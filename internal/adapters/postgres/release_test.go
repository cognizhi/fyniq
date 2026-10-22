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
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// spreadBooks are purchasesBooks with 1210 Prepayments.
type spreadBooks struct {
	purchasesBooks
	Prepaid storage.Account
}

func newSpreadBooks(t *testing.T, ctx context.Context, s storage.Store, name string) spreadBooks {
	t.Helper()
	b := spreadBooks{purchasesBooks: newPurchasesBooks(t, ctx, s, name)}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		b.Prepaid, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1210", Name: "Prepayments", Type: storage.Asset,
			Role: storage.AccountRole(ledger.RolePrepayments)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return b
}

// spreadDraft is a bill of 14 October: a line of 1,200.00 + 108.00 GST for Jira, spread over four months from
// September, beside an unspread line of 50.00 + 4.50.
func spreadDraft(t *testing.T, b spreadBooks, ref, net, gst string) bill.Draft {
	return bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: ref, Date: porttest.Day(10, 14), Currency: money.MustLookup("SGD"), Lines: []bill.Line{
		{AccountID: b.Rent.ID, Description: "Jira, four months", Net: sgdOf(t, net), GSTCode: "TX", GST: sgdOf(t, gst), SpreadMonths: 4, SpreadFrom: porttest.Day(9, 1)},
		{AccountID: b.Rent.ID, Description: "Seats", Net: sgdOf(t, "50.00"), GSTCode: "TX", GST: sgdOf(t, "4.50")},
	}}
}

// releaseOf is the entry a bill version's first line released for a month ("2026-09"), or ports.ErrNotFound.
func releaseOf(t *testing.T, s storage.Store, org, id uuid.UUID, version int, month string) (storage.Entry, error) {
	t.Helper()
	m, err := time.Parse("2006-01", month)
	if err != nil {
		t.Fatal(err)
	}
	var e storage.Entry
	err = s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		e, err = u.Ledger().ByIdentity(ctx, ledger.BillReleaseIdentity(id, version, 1, m, ledger.RuleBillRelease))
		return err
	})
	return e, err
}

// balances are the accounts' balances over the books' periods, from the balance service, by code.
func balances(t *testing.T, s storage.Store, org uuid.UUID) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		periods, err := u.Periods().List(ctx)
		if err != nil {
			return err
		}
		accounts, err := u.Accounts().List(ctx)
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, len(periods))
		for i, p := range periods {
			ids[i] = p.ID
		}
		tb, err := balance.Trial(ctx, u, money.MustLookup("SGD"), ids...)
		if err != nil {
			return err
		}
		for _, r := range tb.Rows {
			for _, a := range accounts {
				if a.ID == r.AccountID {
					out[a.Code] = r.Base.String()
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// FR-GL-03 and ENG-05 (p1-purchases task 6.1; rev 0.83): a bill line spread over months posts its cost to 1210, and its
// releases move it to the line's account month by month, posted by the scheduler up to each organisation's today and
// never ahead: those already due when the bill posts at once, the rest by the hourly job bills.releases under the
// worker's role, a month with no open period waiting with an audit event. An edit reverses the old version's releases
// on their own dates and releases the new version's; a void reverses them all, leaving 1210 and the account at nothing.
// The replay reproduces every release.
// req: FR-GL-03
// req: ENG-05
// req: FR-AUT-01
// req: FR-AUT-02
// req: FR-GL-12
// req: ledger/posting-rules Spreading an invoice or bill
func TestTheSchedulerReleasesASpreadBill(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ap-specialist"}})
	s := store(t, "fyniq_api")
	b := newSpreadBooks(t, ctx, s, "Spreads Pte Ltd")
	now := clock.NewFrozen(time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC))
	bills := journal.Bills{Store: s, Clock: now}
	jobs := journal.Releases{Store: store(t, "fyniq_worker")}

	// Posted on 20 October: 1,308.00 (the GST not claimable) on 1210, and September's and October's parts, 327.00
	// each, released at once on the bill's date; November's is in the future.
	doc, err := bills.Create(ctx, b.Org, spreadDraft(t, b, "AU-2001", "1200.00", "108.00"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := bills.Post(ctx, b.Org, doc.ID)
	if err != nil || legsOf(t, s, b.Org, e) != "1210 1308 SGD = 1308; 6100 54.5 SGD = 54.5; 2010 -1362.5 SGD = -1362.5" {
		t.Fatalf("the bill: %s %v", legsOf(t, s, b.Org, e), err)
	}
	for _, m := range []string{"2026-09", "2026-10"} {
		r, err := releaseOf(t, s, b.Org, doc.ID, 1, m)
		if err != nil || legsOf(t, s, b.Org, r) != "6100 327 SGD = 327; 1210 -327 SGD = -327" || !r.PostingDate.Equal(porttest.Day(10, 14)) ||
			r.Source != (storage.Source{Type: ledger.SourceBillRelease, ID: doc.ID.String(), Version: 1}) || r.RuleVersion != ledger.RuleBillRelease {
			t.Fatalf("the release of %s: %+v %v", m, r, err)
		}
	}
	if _, err := releaseOf(t, s, b.Org, doc.ID, 1, "2026-11"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("November's part before November: %v", err)
	}

	// The job on 5 November releases November's part on its first day, once; on 5 December, with no December period,
	// December's waits.
	run, err := jobs.ReleaseDue(context.Background(), b.Org, porttest.Day(11, 5))
	if err != nil || run.Released != 1 || run.Waiting != 0 {
		t.Fatalf("the run of 5 November: %+v %v", run, err)
	}
	if r, err := releaseOf(t, s, b.Org, doc.ID, 1, "2026-11"); err != nil || !r.PostingDate.Equal(porttest.Day(11, 1)) || r.PeriodID != b.Nov.ID {
		t.Fatalf("November's release: %+v %v", r, err)
	}
	if run, err = jobs.ReleaseDue(context.Background(), b.Org, porttest.Day(11, 5)); err != nil || run.Released != 0 {
		t.Fatalf("a second run: %+v %v", run, err)
	}
	if run, err = jobs.ReleaseDue(context.Background(), b.Org, time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC)); err != nil || run.Released != 0 || run.Waiting != 1 {
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
		for _, want := range []string{`"actor":{"type":"scheduler","id":"bills.releases"}`, `"action":"bill.release_waiting"`,
			`"waiting_for":[null,"STORAGE-PERIOD-01"]`} {
			if !strings.Contains(trail.String(), want) {
				t.Errorf("the audit trail lacks %s", want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Edited on 5 December to 1,600.00 + 144.00: the three releases of version 1 are reversed on their own dates, and
	// version 2's, 436.00 a month, are released at once up to November; December's waits for its period.
	now.Set(time.Date(2026, 12, 5, 4, 0, 0, 0, time.UTC))
	if _, err := bills.Edit(ctx, b.Org, doc.ID, spreadDraft(t, b, "AU-2001", "1600.00", "144.00"), "The supplier's corrected invoice", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for m, on := range map[string]time.Time{"2026-09": porttest.Day(10, 14), "2026-10": porttest.Day(10, 14), "2026-11": porttest.Day(11, 1)} {
			month, _ := time.Parse("2006-01", m)
			r, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(ledger.BillReleaseIdentity(doc.ID, 1, 1, month, ledger.RuleBillRelease)))
			if err != nil || !r.PostingDate.Equal(on) {
				t.Errorf("the reversal of version 1's %s: %+v %v", m, r, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"2026-09", "2026-10", "2026-11"} {
		if r, err := releaseOf(t, s, b.Org, doc.ID, 2, m); err != nil || legsOf(t, s, b.Org, r) != "6100 436 SGD = 436; 1210 -436 SGD = -436" {
			t.Errorf("version 2's %s: %s %v", m, legsOf(t, s, b.Org, r), err)
		}
	}
	if _, err := releaseOf(t, s, b.Org, doc.ID, 2, "2026-12"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("December's part without its period: %v", err)
	}

	// December opens: the job releases its part, and the spread is used up: 1210 is back at nothing.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: porttest.Day(12, 1), EndsOn: porttest.Day(12, 31)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if run, err = jobs.ReleaseDue(context.Background(), b.Org, time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC)); err != nil || run.Released != 1 || run.Waiting != 0 {
		t.Fatalf("the run of 5 December: %+v %v", run, err)
	}
	if got := balances(t, s, b.Org); got["1210"] != "0.00" || got["6100"] != "1798.50" || got["2010"] != "-1798.50" {
		t.Fatalf("after the spread: %v", got)
	}

	// Voided on 10 December: the bill and every release of version 2 are reversed, the releases on the void's date.
	if _, err := bills.Void(ctx, b.Org, doc.ID, porttest.Day(12, 10), "Cancelled with the supplier"); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"2026-09", "2026-10", "2026-11", "2026-12"} {
		month, _ := time.Parse("2006-01", m)
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(ledger.BillReleaseIdentity(doc.ID, 2, 1, month, ledger.RuleBillRelease)))
			if err != nil || !r.PostingDate.Equal(porttest.Day(12, 10)) {
				t.Errorf("the void's reversal of %s: %+v %v", m, r, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := balances(t, s, b.Org); got["1210"] != "0.00" || got["6100"] != "0.00" || got["2010"] != "0.00" {
		t.Fatalf("after the void: %v", got)
	}
	if run, err = jobs.ReleaseDue(context.Background(), b.Org, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)); err != nil || run.Released != 0 || run.Waiting != 0 {
		t.Fatalf("a voided bill releases nothing: %+v %v", run, err)
	}
	if _, err := journal.ReleaseDueAll(context.Background(), store(t, "fyniq_worker"), now.Now()); err != nil {
		t.Errorf("every organisation's run: %v", err)
	}

	// The replay reproduces every bill and release.
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

// The database keeps a release to a posted bill's current version (FY172), and an edit or a void to the bill's own
// entry: reversing a release in its place is refused at commit (FY055, FY056).
// req: FR-GL-03
// req: FR-GL-12
// req: QA-ENG-05
// asvs: V2.2.3, V13.2.2
func TestAttackAReleaseIsNotTheBillsEntry(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ap-specialist"}})
	s := store(t, "fyniq_api")
	b := newSpreadBooks(t, ctx, s, "Release attacks Pte Ltd")
	bills := journal.Bills{Store: s, Clock: clock.NewFrozen(time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC))}
	doc, err := bills.Create(ctx, b.Org, spreadDraft(t, b, "AU-3001", "1200.00", "108.00"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bills.Post(ctx, b.Org, doc.ID); err != nil {
		t.Fatal(err)
	}
	release := func(version int, month time.Time) storage.NewEntry {
		id := ledger.BillReleaseIdentity(doc.ID, version, 1, month, ledger.RuleBillRelease)
		return storage.NewEntry{PostingDate: porttest.Day(11, 1), PostingIdentity: id, RuleVersion: ledger.RuleBillRelease,
			Source: storage.Source{Type: ledger.SourceBillRelease, ID: doc.ID.String(), Version: version},
			Lines: []storage.NewLine{
				{AccountID: b.Rent.ID, Currency: "SGD", Amount: "1.00", FXRate: "1", FXRateSource: "base", BaseAmount: "1.00"},
				{AccountID: b.Prepaid.ID, Currency: "SGD", Amount: "-1.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-1.00"},
			}}
	}
	// A release of a version the bill does not have, or of a draft, is refused.
	draft, err := bills.Create(ctx, b.Org, spreadDraft(t, b, "AU-3002", "10.00", "0.90"))
	if err != nil {
		t.Fatal(err)
	}
	for name, e := range map[string]storage.NewEntry{"another version": release(2, porttest.Day(11, 1)), "a draft": func() storage.NewEntry {
		x := release(1, porttest.Day(11, 1))
		x.Source.ID, x.PostingIdentity = draft.ID.String(), "bill_release:"+draft.ID.String()+":v1:l1:2026-11:bill_release@1"
		return x
	}()} {
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, e)
			return err
		})
		if storage.CodeOf(err) != storage.CodeBillRelease {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A void that reverses a release instead of the bill's entry is refused at commit.
	err = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		stored, err := u.Ledger().ByIdentity(ctx, ledger.BillReleaseIdentity(doc.ID, 1, 1, porttest.Day(9, 1), ledger.RuleBillRelease))
		if err != nil {
			return err
		}
		reversal := release(1, porttest.Day(9, 1))
		reversal.PostingIdentity, reversal.ReversesEntryID = ledger.ReversalIdentity(stored.PostingIdentity), &stored.ID
		reversal.Lines[0].Amount, reversal.Lines[0].BaseAmount = "-327.00", "-327.00"
		reversal.Lines[1].Amount, reversal.Lines[1].BaseAmount = "327.00", "327.00"
		if _, err := u.Ledger().Append(ctx, reversal); err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, doc.ID, storage.DocumentReversed)
	})
	if storage.CodeOf(err) != storage.CodeDocumentEdit {
		t.Errorf("a void by a release's reversal: %v", err)
	}
}
