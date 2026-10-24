//go:build local

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// spreadInvoice is an invoice of 14 October, not GST-registered: a line of price for four months of the analytics
// platform from September, spread, beside an unspread set-up fee of 50.00.
func spreadInvoice(t *testing.T, b salesBooks, ref, price string) invoice.Draft {
	spread := invoiceLine(t, b, "1", price, "", "SR", "SGD")
	spread.Description, spread.SpreadMonths, spread.SpreadFrom = "Analytics, four months", 4, porttest.Day(9, 1)
	setup := invoiceLine(t, b, "1", "50.00", "", "SR", "SGD")
	setup.Description = "Set-up"
	return invoice.Draft{CustomerID: b.Customer.ID, Reference: ref, Date: porttest.Day(10, 14), Currency: money.MustLookup("SGD"),
		Lines: []invoice.Line{spread, setup}}
}

// invoiceReleaseOf is the entry an invoice version's first line released for a month ("2026-09"), or
// ports.ErrNotFound.
func invoiceReleaseOf(t *testing.T, s storage.Store, org, id uuid.UUID, version int, month string) (storage.Entry, error) {
	t.Helper()
	m, err := time.Parse("2006-01", month)
	if err != nil {
		t.Fatal(err)
	}
	var e storage.Entry
	err = s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		e, err = u.Ledger().ByIdentity(ctx, ledger.InvoiceReleaseIdentity(id, version, 1, m, ledger.RuleInvoiceRelease))
		return err
	})
	return e, err
}

// FR-GL-03 and ENG-05 for invoices (p1-sales task 1.5; rev 0.118): an invoice line spread over months posts its
// revenue to 2400, and its releases move it to the line's account month by month, posted by the scheduler up to each
// organisation's today and never ahead: those already due when the invoice is issued at once, the rest by the hourly
// job invoices.releases under the worker's role and its own principal, a month with no open period waiting with an
// audit event. An edit reverses the old version's releases on their own dates and releases the new version's; a void
// reverses them all, leaving 2400 and the account at nothing. The replay reproduces every release.
// req: FR-GL-03
// req: ENG-05
// req: FR-AUT-01
// req: FR-AUT-02
// req: FR-GL-12
// req: ledger/posting-rules Spreading an invoice or bill
func TestTheSchedulerReleasesASpreadInvoice(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Spread revenue Pte Ltd")
	now := clock.NewFrozen(time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC))
	invoices := journal.Invoices{Store: s, Clock: now}
	jobs := journal.Releases{Store: store(t, "fyniq_worker")}

	// Issued on 20 October: 1,200.00 on 2400, and September's and October's parts, 300.00 each, released at once on the
	// invoice's date; November's is in the future.
	doc, err := invoices.Create(ctx, b.Org, spreadInvoice(t, b, "PO-4001", "1200.00"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := invoices.Post(ctx, b.Org, doc.ID)
	if err != nil || legsOf(t, s, b.Org, e) != "1100 1250 SGD = 1250; 2400 -1200 SGD = -1200; 4000 -50 SGD = -50" {
		t.Fatalf("the invoice: %s %v", legsOf(t, s, b.Org, e), err)
	}
	for _, m := range []string{"2026-09", "2026-10"} {
		r, err := invoiceReleaseOf(t, s, b.Org, doc.ID, 1, m)
		if err != nil || legsOf(t, s, b.Org, r) != "2400 300 SGD = 300; 4000 -300 SGD = -300" || !r.PostingDate.Equal(porttest.Day(10, 14)) ||
			r.Source != (storage.Source{Type: ledger.SourceInvoiceRelease, ID: doc.ID.String(), Version: 1}) || r.RuleVersion != ledger.RuleInvoiceRelease {
			t.Fatalf("the release of %s: %+v %v", m, r, err)
		}
	}
	if _, err := invoiceReleaseOf(t, s, b.Org, doc.ID, 1, "2026-11"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("November's part before November: %v", err)
	}

	// The job on 5 November releases November's part on its first day, once; the bills' job leaves invoices alone; on 5
	// December, with no December period, December's waits.
	if run, err := jobs.ReleaseDue(context.Background(), b.Org, porttest.Day(11, 5)); err != nil || run.Released != 0 {
		t.Fatalf("the bills' run: %+v %v", run, err)
	}
	run, err := jobs.ReleaseInvoicesDue(context.Background(), b.Org, porttest.Day(11, 5))
	if err != nil || run.Released != 1 || run.Waiting != 0 {
		t.Fatalf("the run of 5 November: %+v %v", run, err)
	}
	if r, err := invoiceReleaseOf(t, s, b.Org, doc.ID, 1, "2026-11"); err != nil || !r.PostingDate.Equal(porttest.Day(11, 1)) || r.PeriodID != b.Nov.ID {
		t.Fatalf("November's release: %+v %v", r, err)
	}
	if run, err = jobs.ReleaseInvoicesDue(context.Background(), b.Org, porttest.Day(11, 5)); err != nil || run.Released != 0 {
		t.Fatalf("a second run: %+v %v", run, err)
	}
	if run, err = jobs.ReleaseInvoicesDue(context.Background(), b.Org, time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC)); err != nil || run.Released != 0 || run.Waiting != 1 {
		t.Fatalf("the run of 5 December without a period: %+v %v", run, err)
	}
	if err := store(t, "fyniq_readonly").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		if err != nil {
			return err
		}
		var trail strings.Builder
		for _, ev := range events {
			trail.WriteString(ev.Body + "\n")
		}
		for _, want := range []string{`"actor":{"type":"scheduler","id":"invoices.releases"}`, `"action":"invoice.release_waiting"`,
			`"waiting_for":[null,"STORAGE-PERIOD-01"]`} {
			if !strings.Contains(trail.String(), want) {
				t.Errorf("the audit trail lacks %s", want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Edited on 5 December to 1,600.00: the three releases of version 1 are reversed on their own dates, and version 2's,
	// 400.00 a month, are released at once up to November; December's waits for its period.
	now.Set(time.Date(2026, 12, 5, 4, 0, 0, 0, time.UTC))
	if _, err := invoices.Edit(ctx, b.Org, doc.ID, spreadInvoice(t, b, "PO-4001", "1600.00"), "The agreed price was higher", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for m, on := range map[string]time.Time{"2026-09": porttest.Day(10, 14), "2026-10": porttest.Day(10, 14), "2026-11": porttest.Day(11, 1)} {
			month, _ := time.Parse("2006-01", m)
			r, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(ledger.InvoiceReleaseIdentity(doc.ID, 1, 1, month, ledger.RuleInvoiceRelease)))
			if err != nil || !r.PostingDate.Equal(on) {
				t.Errorf("the reversal of version 1's %s: %+v %v", m, r, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"2026-09", "2026-10", "2026-11"} {
		if r, err := invoiceReleaseOf(t, s, b.Org, doc.ID, 2, m); err != nil || legsOf(t, s, b.Org, r) != "2400 400 SGD = 400; 4000 -400 SGD = -400" {
			t.Errorf("version 2's %s: %s %v", m, legsOf(t, s, b.Org, r), err)
		}
	}
	if _, err := invoiceReleaseOf(t, s, b.Org, doc.ID, 2, "2026-12"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("December's part without its period: %v", err)
	}

	// December opens: the job releases its part, and the spread is earned: 2400 is back at nothing.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: porttest.Day(12, 1), EndsOn: porttest.Day(12, 31)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if run, err = jobs.ReleaseInvoicesDue(context.Background(), b.Org, time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC)); err != nil || run.Released != 1 || run.Waiting != 0 {
		t.Fatalf("the run of 5 December: %+v %v", run, err)
	}
	if got := balances(t, s, b.Org); got["2400"] != "0.00" || got["4000"] != "-1650.00" || got["1100"] != "1650.00" {
		t.Fatalf("after the spread: %v", got)
	}

	// Voided on 10 December: the invoice and every release of version 2 are reversed, the releases on the void's date.
	if _, err := invoices.Void(ctx, b.Org, doc.ID, porttest.Day(12, 10), "Cancelled with the customer"); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"2026-09", "2026-10", "2026-11", "2026-12"} {
		month, _ := time.Parse("2006-01", m)
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(ledger.InvoiceReleaseIdentity(doc.ID, 2, 1, month, ledger.RuleInvoiceRelease)))
			if err != nil || !r.PostingDate.Equal(porttest.Day(12, 10)) {
				t.Errorf("the void's reversal of %s: %+v %v", m, r, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := balances(t, s, b.Org); got["2400"] != "0.00" || got["4000"] != "0.00" || got["1100"] != "0.00" {
		t.Fatalf("after the void: %v", got)
	}
	if run, err = jobs.ReleaseInvoicesDue(context.Background(), b.Org, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)); err != nil || run.Released != 0 || run.Waiting != 0 {
		t.Fatalf("a voided invoice releases nothing: %+v %v", run, err)
	}
	if _, err := journal.ReleaseInvoicesDueAll(context.Background(), store(t, "fyniq_worker"), now.Now()); err != nil {
		t.Errorf("every organisation's run: %v", err)
	}

	// The replay reproduces every invoice entry and release.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err != nil {
			return err
		}
		if len(r.Mismatches) > 0 || r.Entries != 18 {
			t.Errorf("the replay: %d entries, %+v", r.Entries, r.Mismatches)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// The database keeps an invoice's release to an issued invoice's current version (FY172), and an edit or a void to the
// invoice's own entry: reversing a release in its place is refused at commit (FY056).
// req: FR-GL-03
// req: FR-GL-12
// req: QA-ENG-05
// asvs: V2.2.3, V13.2.2
func TestAttackAReleaseIsNotTheInvoicesEntry(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Invoice release attacks Pte Ltd")
	invoices := journal.Invoices{Store: s, Clock: clock.NewFrozen(time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC))}
	doc, err := invoices.Create(ctx, b.Org, spreadInvoice(t, b, "PO-5001", "1200.00"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invoices.Post(ctx, b.Org, doc.ID); err != nil {
		t.Fatal(err)
	}
	release := func(id uuid.UUID, version int, month time.Time) storage.NewEntry {
		return storage.NewEntry{PostingDate: porttest.Day(11, 1), PostingIdentity: ledger.InvoiceReleaseIdentity(id, version, 1, month, ledger.RuleInvoiceRelease),
			RuleVersion: ledger.RuleInvoiceRelease, Source: storage.Source{Type: ledger.SourceInvoiceRelease, ID: id.String(), Version: version},
			Lines: []storage.NewLine{
				{AccountID: b.Deferred.ID, Currency: "SGD", Amount: "1.00", FXRate: "1", FXRateSource: "base", BaseAmount: "1.00"},
				{AccountID: b.Sales.ID, Currency: "SGD", Amount: "-1.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-1.00"},
			}}
	}
	// A release of a version the invoice does not have, or of a draft, is refused.
	draft, err := invoices.Create(ctx, b.Org, spreadInvoice(t, b, "PO-5002", "10.00"))
	if err != nil {
		t.Fatal(err)
	}
	for name, e := range map[string]storage.NewEntry{"another version": release(doc.ID, 2, porttest.Day(11, 1)), "a draft": release(draft.ID, 1, porttest.Day(11, 1))} {
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, e)
			return err
		})
		if storage.CodeOf(err) != storage.CodeBillRelease {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A void that reverses a release instead of the invoice's entry is refused at commit.
	err = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		stored, err := u.Ledger().ByIdentity(ctx, ledger.InvoiceReleaseIdentity(doc.ID, 1, 1, porttest.Day(9, 1), ledger.RuleInvoiceRelease))
		if err != nil {
			return err
		}
		reversal := release(doc.ID, 1, porttest.Day(9, 1))
		reversal.PostingIdentity, reversal.ReversesEntryID = ledger.ReversalIdentity(stored.PostingIdentity), &stored.ID
		reversal.Lines[0].Amount, reversal.Lines[0].BaseAmount = "-300.00", "-300.00"
		reversal.Lines[1].Amount, reversal.Lines[1].BaseAmount = "300.00", "300.00"
		if _, err := u.Ledger().Append(ctx, reversal); err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, doc.ID, storage.DocumentReversed)
	})
	if storage.CodeOf(err) != storage.CodeDocumentEdit {
		t.Errorf("a void by a release's reversal: %v", err)
	}
}
