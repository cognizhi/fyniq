package wiring

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/bankfeed"
	"github.com/cognizhi/fyniq/internal/bankingest"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/superadmin"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/notify/reminders"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/remitmail"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/salesmail"
)

// PlatformJobs are the functions behind the platform's schedules (outbox.PlatformSchedules), run by
// the worker locally and by the jobs image as Cloud Run jobs in production.
func (k *Kit) PlatformJobs(clock ports.Clock) map[string]func(ctx context.Context) error {
	return map[string]func(ctx context.Context) error{
		"audit.anchor": func(ctx context.Context) error {
			_, err := audit.AnchorAll(ctx, k.Store, k.Blobs, k.Signer, clock)
			return err
		},
		"audit.verify": func(ctx context.Context) error {
			_, err := audit.VerifyAll(ctx, k.Store, k.Blobs, k.Signer)
			return err
		},
		"idempotency.purge": func(ctx context.Context) error {
			_, err := PurgeIdempotencyKeys(ctx, k.Store, clock.Now())
			return err
		},
		"ratelimit.purge": func(ctx context.Context) error {
			_, err := PurgeRateLimitWindows(ctx, k.Store, clock.Now())
			return err
		},
		"ledger.verify": func(ctx context.Context) error {
			_, err := balance.VerifyAll(ctx, k.Store)
			return err
		},
		"ledger.replay": func(ctx context.Context) error {
			_, err := journal.ReplayAll(ctx, k.Store)
			return err
		},
		connector.CleanupJob: k.CleanupJob(clock),
		"superadmins.verify": func(ctx context.Context) error {
			_, err := superadmin.Verify(ctx, k.Store, k.Store)
			return err
		},
		reminders.Job: func(ctx context.Context) error {
			_, err := reminders.BillsDue(ctx, k.Store, k.dispatcher(clock))
			return err
		},
		notify.DigestJob: func(ctx context.Context) error {
			_, err := k.Fanout(clock).Digest(ctx)
			return err
		},
		email.ExpireJob: func(ctx context.Context) error {
			if k.Staging == nil {
				return nil // no staging store: no received mail was kept
			}
			_, err := email.ExpireAll(ctx, k.Store, k.Staging, clock.Now())
			return err
		},
		email.DomainsJob: func(ctx context.Context) error {
			results, err := email.CheckDomains(ctx, k.Store, k.Store, net.DefaultResolver, k.Mail.Domains, k.MailDNS(ctx))
			if err != nil {
				return err
			}
			return notify.DomainChecks(ctx, k.Store, k.Store, k.dispatcher(clock), results, clock.Now()) // FR-NTF-01
		},
		"compliance.evidence": func(ctx context.Context) error {
			_, err := compliance.RecordEvidence(ctx, k.Store, k.Blobs, clock.Now())
			return err
		},
		"evidence.commit": func(ctx context.Context) error {
			c, ok := k.Committer(clock)
			if !ok {
				return nil // no evidence stores: nothing can have been attached
			}
			_, err := c.CommitAll(ctx)
			return err
		},
		"journals.auto-reverse": func(ctx context.Context) error {
			_, err := journal.ReverseDueAll(ctx, k.Store, clock.Now())
			return err
		},
		"journals.recurring": func(ctx context.Context) error {
			_, err := journal.Service{Store: k.Store, AfterMaking: notify.RecurringMade(k.dispatcher(clock))}.MakeDueAll(ctx, clock.Now()) // FR-NTF-01
			return err
		},
		"fx.feed": func(ctx context.Context) error {
			if k.Rates == nil {
				return nil // no feed configured: organisations enter their own rates
			}
			_, err := fx.RecordFeed(ctx, k.Store, k.Rates, clock.Now())
			return err
		},
		"fx.revalue": func(ctx context.Context) error {
			_, err := journal.RevalueDueAll(ctx, k.Store, clock.Now())
			return err
		},
		"fx.reversals": func(ctx context.Context) error {
			_, err := journal.ReverseRevaluationsAll(ctx, k.Store, clock.Now())
			return err
		},
		"bills.recurring": func(ctx context.Context) error {
			_, err := journal.Bills{Store: k.Store, AfterMaking: notify.RecurringMade(k.dispatcher(clock))}.MakeDueAll(ctx, clock.Now()) // FR-NTF-01
			return err
		},
		"invoices.recurring": func(ctx context.Context) error { // FR-AR-07: drafts, and the approved ones issued and sent
			return salesmail.Scheduled{Invoices: journal.Invoices{Store: k.Store, AfterMaking: notify.RecurringMade(k.dispatcher(clock)), Clock: clock},
				Sender: k.SalesSender(clock)}.RunAll(ctx, clock.Now())
		},
		"bills.releases": func(ctx context.Context) error {
			_, err := journal.ReleaseDueAll(ctx, k.Store, clock.Now())
			return err
		},
		"invoices.releases": func(ctx context.Context) error {
			_, err := journal.ReleaseInvoicesDueAll(ctx, k.Store, clock.Now())
			return err
		},
		"bank.releases": func(ctx context.Context) error {
			_, err := journal.ReleaseBankTransactionsDueAll(ctx, k.Store, clock.Now())
			return err
		},
		"assets.depreciation": func(ctx context.Context) error { // FR-FA-02: each asset's months due by today
			_, err := journal.DepreciateDueAll(ctx, k.Store, clock.Now())
			return err
		},
		"bank.sync": func(ctx context.Context) error { // FR-BNK-15, FR-BNK-16: the bank-data connectors' pulls
			return k.BankFeeds(clock).SyncAll(ctx)
		},
		"bank.review-sample": func(ctx context.Context) error { // FR-BNK-14, FM-3: the week's sample into the review
			return banking.SampleAll(ctx, k.Store, clock.Now())
		},
		"bank.signals-expiry": func(ctx context.Context) error { // FR-BNK-16: alerts no statement line closed in time
			_, err := banking.ExpireSignalsAll(ctx, k.Store, clock.Now())
			return err
		},
		"quotes.expiry": func(ctx context.Context) error {
			_, err := journal.ExpireQuotesDueAll(ctx, k.Store, clock.Now())
			return err
		},
		"evidence.verify": func(ctx context.Context) error {
			if k.Evidence == nil {
				return nil
			}
			_, err := evidence.Verify(ctx, k.Store, k.Evidence)
			return err
		},
		"evidence.sweep": func(ctx context.Context) error {
			if k.Staging == nil || k.Evidence == nil {
				return nil
			}
			_, err := evidence.Sweep(ctx, k.Store, k.Staging, k.Evidence, clock.Now())
			return err
		},
	}
}

// Fanout fans notification events out and sends their digests (FR-NTF-01, FR-NTF-02, FR-NTF-05).
func (k *Kit) Fanout(clock ports.Clock) notify.Fanout {
	return notify.Fanout{Store: k.Store, Dispatcher: k.dispatcher(clock), Clock: clock, Origin: k.Origin}
}

// Relays emits the installation's notices, such as a connector paused or a change waiting, in the organisations of the
// Super admins they name (FR-ADM-07, FR-INT-26).
func (k *Kit) Relays(clock ports.Clock) notify.Relays {
	return notify.Relays{Store: k.Store, Operators: k.Store, Dispatcher: k.dispatcher(clock)}
}

// JobDead tells those who look after an organisation of its background work that died (FR-NTF-01).
func (k *Kit) JobDead(clock ports.Clock) outbox.Dead {
	return notify.JobDead(k.dispatcher(clock))
}

// dispatcher queues work through the kit's outbox.
func (k *Kit) dispatcher(clock ports.Clock) outbox.Dispatcher {
	return outbox.Dispatcher{Store: k.Store, Queue: k.Queue, Clock: clock}
}

// SalesSender emails the invoices the scheduler issues (FR-AR-06, FR-AR-07): rendered by the render service, staged in
// the staging store, with the web app's origin for their links. Without a render service it sends nothing.
func (k *Kit) SalesSender(clock ports.Clock) salesmail.Sender {
	out := salesmail.Sender{Store: k.Store, Staging: k.Staging, Outbox: k.dispatcher(clock), Origin: k.Origin, Clock: clock}
	if k.Render != nil {
		out.Render = func(ctx context.Context, documentType string, c doctemplate.Content, d render.Data, created time.Time) (render.Response, error) {
			return k.Render.Render(ctx, render.Request{DocumentType: documentType, Content: c, Data: d, Created: created, Check: true})
		}
	}
	return out
}

// Remittances send suppliers their remittance advice when a batch's payment is matched (FR-BNK-12): rendered by the render
// service and staged in the staging store. Without a render service the job waits for one.
func (k *Kit) Remittances(clock ports.Clock) remitmail.Sender {
	out := remitmail.Sender{Store: k.Store, Staging: k.Staging, Outbox: k.dispatcher(clock)}
	if k.Render != nil {
		out.Render = func(ctx context.Context, documentType string, c doctemplate.Content, d render.Data, created time.Time) (render.Response, error) {
			return k.Render.Render(ctx, render.Request{DocumentType: documentType, Content: c, Data: d, Created: created, Check: true})
		}
	}
	return out
}

// Fetcher fetches, decides and keeps received mail (FR-INT-22), telling whoever decides of quarantined mail.
func (k *Kit) Fetcher(clock ports.Clock) email.Fetcher {
	return email.Fetcher{Mail: k.Mail.Inbound, Store: k.Store, Files: k.Staging, Scanner: k.Scanner, Clock: clock,
		Quarantined: notify.Quarantined(k.dispatcher(clock)), Accepted: bankingest.Accepted(k.dispatcher(clock))}
}

// BankFeeds syncs the organisations' bank-data connectors (FR-BNK-15, FR-BNK-16).
func (k *Kit) BankFeeds(clock ports.Clock) bankfeed.Syncer {
	return bankfeed.Syncer{Store: k.Store, Runtime: k.Runtime(clock), Clock: clock}
}

// BankReports imports the banks' emailed reports accepted at the bank-ingest address (FR-BNK-17).
func (k *Kit) BankReports(clock ports.Clock) bankingest.Importer {
	return bankingest.Importer{Store: k.Store, Files: k.Staging, Clock: clock}
}

// Committer commits attached files of records that are no longer drafts to the locked store (FR-DOC-13);
// ok is false when the kit has no evidence stores.
func (k *Kit) Committer(clock ports.Clock) (evidence.Committer, bool) {
	if k.Staging == nil || k.Evidence == nil {
		return evidence.Committer{}, false
	}
	return evidence.Committer{Store: k.Store, Staging: k.Staging, Locked: k.Evidence, Clock: clock, Zone: appclock.Singapore}, true
}

// PurgeIdempotencyKeys deletes every organisation's idempotency keys that expired at or before now
// (FR-INT-08) and returns how many. An expired key that is not yet purged is refused, not replayed.
func PurgeIdempotencyKeys(ctx context.Context, store storage.Store, now time.Time) (int64, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, org := range orgs {
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			n, err := u.Idempotency().Purge(ctx, now)
			total += n
			return err
		})
		if err != nil {
			return total, fmt.Errorf("organisation %s: %w", org, err)
		}
	}
	return total, nil
}

// rateLimitKept is how long an ended rate-limit window is kept: a margin for API instances whose clocks
// differ a little.
const rateLimitKept = 5 * time.Minute

// PurgeRateLimitWindows deletes every organisation's rate-limit windows that started more than
// rateLimitKept before now (FR-INT-16) and returns how many. A window is only a count for its minute.
func PurgeRateLimitWindows(ctx context.Context, store storage.Store, now time.Time) (int64, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, org := range orgs {
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			n, err := u.RateLimits().Purge(ctx, now.Add(-rateLimitKept))
			total += n
			return err
		})
		if err != nil {
			return total, fmt.Errorf("organisation %s: %w", org, err)
		}
	}
	return total, nil
}
