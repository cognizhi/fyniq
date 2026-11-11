package wiring

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/bankfeed"
	"github.com/cognizhi/fyniq/internal/bankingest"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/backup"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/checks"
	controls "github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/retention"
	"github.com/cognizhi/fyniq/internal/domain/security"
	"github.com/cognizhi/fyniq/internal/domain/superadmin"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/logging"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/notify/reminders"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/remitmail"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/reportexport"
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
			_, reports, err := audit.Nightly(ctx, k.Store, k.Blobs, k.Signer, k.Keyed(), k.TellRecovery(clock), clock) // the recovery step first (D8)
			return errors.Join(err, k.alertChains(ctx, reports, clock))
		},
		"audit.erase": func(ctx context.Context) error { // AUD-06: security events' details after their retention
			_, err := audit.EraseAll(ctx, k.Store, k.Blobs, k.Signer, clock)
			return err
		},
		"retention.purge": func(ctx context.Context) error { // AUD-09, AUD-10: the purge after retention, where it is switched on
			if err := retention.PurgeLedgers(ctx, k.Store, k.Blobs, k.Signer, clock); err != nil { // the ledger's year first (D7b)
				return err
			}
			_, err := audit.PurgeAll(ctx, k.Store, k.Blobs, k.Signer, clock)
			return err
		},
		"pdpa.shred": func(ctx context.Context) error { // §11.8: years past their retention listed, approved runs shredded
			_, err := audit.ShredAll(ctx, k.Store, k.Blobs, k.Signer, k.TellShredding(clock), clock)
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
		backup.Job: func(ctx context.Context) error { // DR-02, DR-04: the weekly export, and the month's in its first week
			if k.Backup == nil {
				return fmt.Errorf("no database export is configured (FYNIQ_GCP exports)")
			}
			done, err := backup.Export(ctx, k.Backup, k.ExportBuckets, clock.Now())
			for _, d := range done {
				fmt.Fprintln(os.Stderr, "db.export:", d)
			}
			return err
		},
		"ledger.verify": func(ctx context.Context) error {
			_, err := balance.VerifyAll(ctx, k.Store)
			return err
		},
		checks.Job: func(ctx context.Context) error { // FR-CTL-01, FR-CTL-05: every check of every organisation
			_, err := k.Checks(clock).Nightly(ctx)
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
		reminders.DeadlinesJob: func(ctx context.Context) error {
			_, err := reminders.DeadlinesDue(ctx, k.Store, k.dispatcher(clock))
			return err
		},
		reminders.CashBufferJob: func(ctx context.Context) error {
			_, err := reminders.CashBuffer(ctx, k.Store, k.dispatcher(clock))
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
		controls.RunJob: func(ctx context.Context) error { // CMP-01: each organisation's verifications
			registry, err := compliance.Load()
			if err != nil {
				return err
			}
			assets, err := compliance.LoadReleaseAssets()
			if err != nil {
				return err
			}
			_, err = controls.Runner{Store: k.Store, Clock: clock, Controls: registry, Platform: k.Store, Release: k.Release, Assets: assets,
				Tell: k.TellControl(clock), Installation: k.Store, Posture: k.Posture, Logs: k.Logs, Alert: k.alertPlatform(clock)}.Nightly(ctx)
			return err
		},
		controls.SchemaJob: func(ctx context.Context) error { // CTL-FIN-02, CTL-AUD-01, CTL-AI-01: the guards as released
			want, err := postgres.ReleaseGuards()
			if err != nil {
				return err
			}
			_, err = controls.VerifySchema(ctx, k.Store, want)
			return err
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
		"reports.expire": func(ctx context.Context) error {
			_, err := k.Exports(clock).Expire(ctx, clock.Now())
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
// TellRecovery tells the Owner and the DPO that a restore landed behind the audit trail's anchors (AUD-09, design D8).
func (k *Kit) TellRecovery(clock ports.Clock) audit.Tell {
	d := k.dispatcher(clock)
	return func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, r storage.AuditRecovery) error {
		members, err := u.Members().List(ctx, clock.Now())
		if err != nil {
			return err
		}
		var users []uuid.UUID
		for _, m := range members {
			if slices.ContainsFunc(m.Roles, func(a storage.RoleAssignment) bool {
				return a.Role == string(authz.Owner) || a.Role == string(authz.DPO)
			}) {
				users = append(users, m.UserID)
			}
		}
		_, err = notify.Emit(ctx, d, u, org, notify.Event{Type: notify.AuditRecoveredType, RecordType: "audit_recovery", RecordID: &r.ID,
			Title: fmt.Sprintf("A restore landed behind the audit trail's anchors: events %d to %d are no longer proven. Follow the restore runbook, then acknowledge it",
				r.MissingFrom, r.MissingTo), Link: "/compliance/audit-trail", Audience: notify.Audience{Users: users}, At: clock.Now()})
		return err
	}
}

// TellShredding tells those who approve on "Data protection (PDPA)", the DPO, that a year's person keys wait for their
// approval to be shredded (§11.8, design D9).
func (k *Kit) TellShredding(clock ports.Clock) audit.TellShredding {
	d := k.dispatcher(clock)
	return func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, r storage.ShreddingRun) error {
		_, err := notify.Emit(ctx, d, u, org, notify.Event{Type: notify.ShreddingDueType, RecordType: "pd_shredding_run", RecordID: &r.ID,
			Title: fmt.Sprintf("The personal data of the year ending %s has passed its retention: approve its shredding", r.Cohort.Format("2 Jan 2006")),
			Link:  "/compliance/data-protection", Audience: notify.Audience{Capability: string(authz.DataProtection), Need: "A"}, At: clock.Now()})
		return err
	}
}

// TellControl tells the members holding a control's owner role that it newly failed or fell overdue (CMP-01, design D5):
// critical, emailed at once, once per failure.
func (k *Kit) TellControl(clock ports.Clock) controls.Tell {
	d := k.dispatcher(clock)
	return func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, c compliance.Control, s storage.ControlState) error {
		members, err := u.Members().List(ctx, clock.Now())
		if err != nil {
			return err
		}
		role := strings.ToLower(c.Owner) // the registry's owners are the roles of §7.1: Owner, Controller, DPO…
		var users []uuid.UUID
		for _, m := range members {
			if slices.ContainsFunc(m.Roles, func(a storage.RoleAssignment) bool { return a.Role == role }) {
				users = append(users, m.UserID)
			}
		}
		word := "failed"
		if s.Status == string(controls.Overdue) {
			word = "is overdue"
		}
		_, err = notify.Emit(ctx, d, u, org, notify.Event{Type: notify.ControlFailedType, RecordType: "control_state", RecordID: &s.ID,
			Title: fmt.Sprintf("%s %s: %s. See it on Compliance › Controls", c.ID, word, strings.TrimSuffix(s.Detail, ".")),
			Link:  "/compliance/controls?control=" + c.ID, Audience: notify.Audience{Users: users}, At: clock.Now()})
		return err
	}
}

func (k *Kit) dispatcher(clock ports.Clock) outbox.Dispatcher {
	return outbox.Dispatcher{Store: k.Store, Queue: k.Queue, Clock: clock}
}

// SalesSender emails the invoices the scheduler issues (FR-AR-06, FR-AR-07): rendered by the render service, staged in
// the staging store, with the web app's origin for their links. Without a render service it sends nothing.
func (k *Kit) SalesSender(clock ports.Clock) salesmail.Sender {
	out := salesmail.Sender{Store: k.Store, Staging: k.Staging, Outbox: k.dispatcher(clock), Origin: k.Origin, Clock: clock}
	if k.Render != nil {
		out.Render = func(ctx context.Context, documentType string, c doctemplate.Content, d render.Data, created time.Time, logo []byte) (render.Response, error) {
			return k.Render.Render(ctx, render.Request{DocumentType: documentType, Content: c, Data: d, Created: created, Logo: logo, Check: true})
		}
	}
	return out
}

// Remittances send suppliers their remittance advice when a batch's payment is matched (FR-BNK-12): rendered by the render
// service and staged in the staging store. Without a render service the job waits for one.
func (k *Kit) Remittances(clock ports.Clock) remitmail.Sender {
	out := remitmail.Sender{Store: k.Store, Staging: k.Staging, Outbox: k.dispatcher(clock)}
	if k.Render != nil {
		out.Render = func(ctx context.Context, documentType string, c doctemplate.Content, d render.Data, created time.Time, logo []byte) (render.Response, error) {
			return k.Render.Render(ctx, render.Request{DocumentType: documentType, Content: c, Data: d, Created: created, Logo: logo, Check: true})
		}
	}
	return out
}

// Exports prepare the exports that run in the background in the staging store, and expire them (FR-RPT-09).
func (k *Kit) Exports(clock ports.Clock) reportexport.Preparer {
	return reportexport.Preparer{Store: k.Store, Files: k.Staging, Outbox: k.dispatcher(clock), Clock: clock, Stamp: k.Stamp}
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

// PurgeRateLimitWindows deletes every organisation's rate-limit windows, and the per-IP windows, that started more
// than rateLimitKept before now (FR-INT-16, SEC-23) and returns how many. A window is only a count for its minute.
func PurgeRateLimitWindows(ctx context.Context, store storage.Store, now time.Time) (int64, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	if ip, ok := store.(storage.IPLimits); ok {
		n, err := ip.PurgeIP(ctx, now.Add(-rateLimitKept))
		if err != nil {
			return 0, fmt.Errorf("the per-IP windows: %w", err)
		}
		total += n
	}
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

// Keyed is the deployment's keyed hash, which proves sensitive values and security events' details in the trail
// (AUD-02, AUD-06): the fields' fingerprint; nil where this service holds no key, and verification then reports the
// details unchecked.
func (k *Kit) Keyed() audit.Keyed {
	if k.Fields == nil {
		return nil
	}
	return k.Fields.Fingerprint
}

// Alerts is the raiser of SEC-31's alerts, with the deployment's thresholds (FYNIQ_ALERT_THRESHOLDS; the defaults when
// they cannot be read, which is logged).
func (k *Kit) Alerts(clock ports.Clock) security.Raiser {
	log := logging.New(os.Stderr, slog.LevelInfo)
	t, err := security.ReadThresholds(os.Getenv)
	if err != nil {
		log.Warn("alerts.thresholds", "error", err.Error())
	}
	return security.Raiser{Dispatcher: outbox.Dispatcher{Store: k.Store, Queue: k.Queue, Clock: clock}, Clock: clock, Thresholds: t, Log: log}
}

// alertPlatform raises the alert of a platform verification's findings in the installation's organisation, to the Super
// admins (SEC-31): one per control and window.
func (k *Kit) alertPlatform(clock ports.Clock) controls.Alert {
	r := k.Alerts(clock)
	r.Installation = true
	return func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, control string, found []controls.Finding) error {
		details := make([]string, len(found))
		for i, f := range found {
			details[i] = f.Detail
		}
		_, err := r.Raise(ctx, u, org, security.Alert{Rule: security.RuleLogLeak, Subject: uuid.NewSHA1(uuid.NameSpaceURL, []byte("fyniq:control:"+control)),
			Link: "/compliance/controls?control=" + control, Title: "The platform's logs hold what they must not: " + strings.Join(details, "; "),
			Facts: map[string]any{"control": control, "kinds": len(found)}})
		return err
	}
}

// alertChains raises an audit-chain alert in each organisation whose trail failed its verification (SEC-31).
func (k *Kit) alertChains(ctx context.Context, reports []audit.Report, clock ports.Clock) error {
	r := k.Alerts(clock)
	var errs []error
	for _, rep := range reports {
		if rep.OK() {
			continue
		}
		errs = append(errs, k.Store.Do(ctx, rep.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := r.Raise(ctx, u, rep.Organisation, security.Alert{Rule: security.RuleAuditChain, Subject: rep.Organisation,
				Link: "/compliance/audit-trail", Title: "The audit trail failed its nightly verification: " + rep.Problem,
				Facts: map[string]any{"events": rep.Events}})
			return err
		}))
	}
	return errors.Join(errs...)
}
