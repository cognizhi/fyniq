//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-GL-04, FR-AUT-01, FR-AUT-02 (the founders' decision of 22 Oct 2026, rev 0.75): the scheduler revalues each ended,
// open period that is due, once its last day's rates are in, as its own principal. The worker's role still holds no
// INSERT on documents and cannot name who posted one: its one way to make a revaluation is
// create_scheduled_revaluation, which saves the run the application worked out as a posted fx_revaluation of an open
// period, by scheduler:fx.revalue, and nothing else. A period without a rate waits, audited; a current one is left
// alone; a back-dated posting makes it due again.
// req: FR-GL-04
// req: FR-AUT-01
// req: FR-AUT-02
// req: SEC-03
// asvs: V13.2.2
func TestTheSchedulerRevaluesAnEndedMonth(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	b := books(t, "Scheduled revaluation Pte Ltd")
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	var wise storage.Account
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if wise, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1030", Name: "Wise (USD)", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "USD", Revalue: true}}); err != nil {
			return err
		}
		_, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "7040", Name: "Unrealised FX gain or loss", Type: storage.Revenue,
			Role: storage.RoleUnrealisedFX})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// book posts Dr Wise in USD at 1.35, Cr sales in SGD.
	book := func(day int, usd, sgd string) {
		t.Helper()
		id := uuid.NewString()
		if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, day), PostingIdentity: id, RuleVersion: "test@1",
				Source: storage.Source{Type: "manual_journal", ID: id, Version: 1}, Lines: []storage.NewLine{
					{AccountID: wise.ID, Currency: "USD", Amount: storage.Decimal(usd), FXRate: "1.35", FXRateSource: "document", BaseAmount: storage.Decimal(sgd)},
					{AccountID: b.Sales.ID, Currency: "SGD", Amount: storage.Decimal("-" + sgd), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal("-" + sgd)},
				}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	book(5, "10000.00", "13500.00")
	nov1 := porttest.Day(11, 1)

	// Before October has ended nothing is due; on 1 November it waits for a USD rate, and says so.
	if r, err := journal.RevalueDue(context.Background(), worker, b.Org, porttest.Day(10, 31)); err != nil || r.Revalued+r.Waiting != 0 {
		t.Fatalf("on the period's last day: %+v %v", r, err)
	}
	if r, err := journal.RevalueDue(context.Background(), worker, b.Org, nov1); err != nil || r.Waiting != 1 || r.Revalued != 0 {
		t.Fatalf("without a rate: %+v %v", r, err)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.FXRates().Enter(ctx, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, 31), Rate: "1.3",
			Quote: "1.30", QuoteForm: "base-per-unit"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if r, err := journal.RevalueDue(context.Background(), worker, b.Org, nov1); err != nil || r.Revalued != 1 || r.Waiting != 0 {
		t.Fatalf("with the rate: %+v %v", r, err)
	}
	if r, err := journal.RevalueDue(context.Background(), worker, b.Org, nov1); err != nil || r.Revalued != 0 {
		t.Fatalf("a second run, current: %+v %v", r, err)
	}

	// The run is the scheduler's: saved and posted by it, its entry the engine's, reversed at once on 1 November.
	runs := func() []fx.Run {
		t.Helper()
		var out []fx.Run
		if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: fx.DocumentFXRevaluation, Limit: 10})
			if err != nil {
				return err
			}
			for _, d := range docs {
				r, err := fx.RunOf(ctx, u, d)
				if err != nil {
					return err
				}
				out = append(out, r)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	got := runs()
	if len(got) != 1 {
		t.Fatalf("want one run, got %d", len(got))
	}
	r := got[0]
	if r.Document.SavedBy != "scheduler:fx.revalue" || r.Document.PostedBy != "scheduler:fx.revalue" || r.Document.Status != storage.DocumentReversed ||
		r.Entry == nil || r.Reversal == nil || !r.Entry.PostingDate.Equal(porttest.Day(10, 31)) || !r.Reversal.PostingDate.Equal(nov1) {
		t.Fatalf("the scheduler's run: %+v", r)
	}
	var lines []string
	for _, l := range r.Entry.Lines {
		lines = append(lines, l.Currency+" "+string(l.Amount)+" "+string(l.BaseAmount))
	}
	if strings.Join(lines, "; ") != "USD 0 -500.0000; SGD 500.0000 500.0000" { // as stored: NUMERIC(20,4)
		t.Errorf("the entry: %v", lines)
	}

	// A posting dated in October after the run makes it due again: the next run posts the difference.
	book(20, "1000.00", "1350.00")
	if r, err := journal.RevalueDue(context.Background(), worker, b.Org, porttest.Day(11, 2)); err != nil || r.Revalued != 1 {
		t.Fatalf("after a back-dated posting: %+v %v", r, err)
	}
	if got := runs(); len(got) != 2 {
		t.Errorf("want two runs, got %d", len(got))
	}

	// Its audit events name the scheduler, and the wait says why.
	if err := store(t, "fyniq_readonly").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		if err != nil {
			return err
		}
		var trail strings.Builder
		for _, ev := range events {
			trail.WriteString(ev.Body + "\n")
		}
		for _, want := range []string{`"actor":{"type":"scheduler","id":"fx.revalue"}`, `"action":"fx_revaluation.waiting"`, `"FX-01"`,
			`"action":"document.posted"`} {
			if !strings.Contains(trail.String(), want) {
				t.Errorf("the audit trail lacks %s", want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	nov := b.Nov.ID.String()
	payload := func(period, date, reverse string) string {
		return `'{"period_id":"` + period + `","date":"` + date + `","reverse_on":"` + reverse + `"}'`
	}
	for _, c := range []struct{ role, stmt, state string }{
		// The worker makes documents only through its functions.
		{"fyniq_worker", "INSERT INTO fyniq.document (type) VALUES ('fx_revaluation')", "42501"},
		// It may set who posted a scheduled recurring invoice since rev 0.136 (FY201 fences it); a poster never changes.
		{"fyniq_worker", "UPDATE fyniq.document SET posted_by = 'user:x' WHERE id = '" + r.Document.ID.String() + "'", "FY053"},
		// Only a revaluation of that period, at its last day, reversed the day after.
		{"fyniq_worker", "SELECT fyniq.create_scheduled_revaluation('" + nov + "', " + payload(b.Oct.ID.String(), "2026-11-30", "2026-12-01") + ", now())", "FY169"},
		{"fyniq_worker", "SELECT fyniq.create_scheduled_revaluation('" + nov + "', " + payload(nov, "2026-11-15", "2026-12-01") + ", now())", "FY169"},
		{"fyniq_worker", "SELECT fyniq.create_scheduled_revaluation('" + nov + "', " + payload(nov, "2026-11-30", "2027-01-01") + ", now())", "FY169"},
		{"fyniq_worker", "SELECT fyniq.create_scheduled_revaluation('" + nov + "', '[]', now())", "FY169"},
		{"fyniq_worker", "SELECT fyniq.create_scheduled_revaluation('" + uuid.NewString() + "', " + payload(nov, "2026-11-30", "2026-12-01") + ", now())", "FY169"},
		{"fyniq_worker", "SELECT fyniq.create_scheduled_revaluation('" + nov + "', " + payload(nov, "2026-11-30", "2026-12-01") + ", NULL)", "FY169"},
		// Nobody else saves the scheduler's runs.
		{"fyniq_api", "SELECT fyniq.create_scheduled_revaluation('" + nov + "', " + payload(nov, "2026-11-30", "2026-12-01") + ", now())", "42501"},
		{"fyniq_executor", "SELECT fyniq.create_scheduled_revaluation('" + nov + "', " + payload(nov, "2026-11-30", "2026-12-01") + ", now())", "42501"},
		{"fyniq_readonly", "SELECT fyniq.create_scheduled_revaluation('" + nov + "', " + payload(nov, "2026-11-30", "2026-12-01") + ", now())", "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}
