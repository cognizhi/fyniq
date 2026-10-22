//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The month-end revaluation (FR-GL-04; rev 0.73), G4-039 at account level, worked by hand: a USD 10,000.00 receivable
// booked on 5 October at 1.35 (S$13,500.00) is S$13,000.00 at 31 October's 1.30, so the run posts Cr 1100 S$500.00 as a
// base-only line and Dr 7040 S$500.00, reversed on 1 November. A receivable of USD 1,000.00 at 1.35 back-dated to 20
// October makes the run stale; the next run takes USD 11,000.00 carried at S$14,350.00 to S$14,300.00: S$50.00 more. A
// USD prepayment is listed as not revalued. November has an EUR balance with no rate: the run is refused with FX-01,
// and once the rate is there its reversal waits for December, which the scheduler posts once December opens.
// req: FR-GL-04
// req: ENG-41
// req: FR-GL-07
// req: FR-AUT-01
// req: FR-AUT-02
func TestTheMonthEndRevaluation(t *testing.T) {
	e := newEnv(t)
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	accountant, controller := e.as(authz.Accountant), e.as(authz.Controller)
	oct, nov := e.books.Oct.ID.String(), e.books.Nov.ID.String()
	var ar, fxAcc, prepaid storage.Account
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if ar, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1100", Name: "Accounts receivable", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{Revalue: true}}); err != nil {
			return err
		}
		if fxAcc, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "7040", Name: "Unrealised FX gain or loss", Type: storage.Revenue,
			Role: storage.RoleUnrealisedFX}); err != nil {
			return err
		}
		prepaid, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1210", Name: "Prepayments", Type: storage.Asset})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// book posts Dr account in cur at rate, Cr sales in SGD.
	book := func(acc storage.Account, date time.Time, cur, amount, rate, base string) {
		t.Helper()
		id := uuid.NewString()
		entry := storage.NewEntry{PostingDate: date, PostingIdentity: id, RuleVersion: "test@1", Source: storage.Source{Type: "manual_journal", ID: id, Version: 1},
			Lines: []storage.NewLine{
				{AccountID: acc.ID, Currency: cur, Amount: storage.Decimal(amount), FXRate: storage.Decimal(rate), FXRateSource: "document", BaseAmount: storage.Decimal(base)},
				{AccountID: e.books.Sales.ID, Currency: "SGD", Amount: storage.Decimal("-" + base), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal("-" + base)},
			}}
		if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, entry)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	book(ar, porttest.Day(10, 5), "USD", "10000.00", "1.35", "13500.00")
	book(prepaid, porttest.Day(10, 6), "USD", "120.00", "1.35", "162.00")
	if r := e.call(http.MethodPost, "/v1/fx/rates", accountant, map[string]any{"kind": "day", "currency": "USD", "on": "2026-10-31", "quote": "1.30"}); r.Status != 201 {
		t.Fatalf("the October rate: %d %s", r.Status, r.Raw)
	}
	get := func(period string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/periods/"+period+"/fx-revaluation", accountant, nil)
		if r.Status != 200 {
			t.Fatalf("getFxRevaluation: %d %s", r.Status, r.Raw)
		}
		return r
	}
	lines := func(r response) []string {
		var out []string
		for _, x := range r.Body["lines"].([]any) {
			l := x.(map[string]any)
			out = append(out, l["code"].(string)+" "+l["amount"].(map[string]any)["amount"].(string)+" "+l["base"].(map[string]any)["amount"].(string))
		}
		return out
	}
	check := func(period string) map[string]any {
		t.Helper()
		s := e.call(http.MethodGet, "/v1/periods/"+period+"/close", controller, nil)
		r := e.call(http.MethodPost, "/v1/periods/"+period+"/close-checks", controller, nil, "If-Match", s.Header.Get("ETag"))
		if r.Status != 200 {
			t.Fatalf("runCloseChecks: %d %s", r.Status, r.Raw)
		}
		for _, c := range r.Body["checks"].(map[string]any)["checks"].([]any) {
			if c.(map[string]any)["key"] == "revaluation" {
				return c.(map[string]any)
			}
		}
		t.Fatalf("no revaluation check: %s", r.Raw)
		return nil
	}

	// October before its run: due, and the close refuses it.
	r := get(oct)
	item := r.Body["balances"].([]any)[0].(map[string]any)
	if r.str("state") != "due" || r.str("date") != "2026-10-31" || r.str("reverse_on") != "2026-11-01" || item["code"] != "1100" ||
		item["open"].(map[string]any)["amount"] != "10000.00" || item["carrying"].(map[string]any)["amount"] != "13500.00" || item["rate"] != "1.3" ||
		item["rate_source"] != "organisation rate of 2026-10-31" || item["change"].(map[string]any)["amount"] != "-500.00" {
		t.Fatalf("October's revaluation: %s", r.Raw)
	}
	if got := lines(r); len(got) != 2 || got[0] != "1100 0.00 -500.00" || got[1] != "7040 500.00 500.00" {
		t.Errorf("October's lines: %v", got)
	}
	if nr := r.Body["not_revalued"].([]any); len(nr) != 1 || nr[0].(map[string]any)["code"] != "1210" {
		t.Errorf("not revalued: %v", nr)
	}
	if c := check(oct); c["severity"] != "error" || c["title"] != "Foreign-currency balances are not revalued" {
		t.Errorf("the close's check before the run: %v", c)
	}

	// Not before the period's last day; then the run posts and reverses at once, as November is open.
	post := func(period string) response {
		return e.call(http.MethodPost, "/v1/periods/"+period+"/fx-revaluation", accountant, nil, "If-Match", get(period).Header.Get("ETag"))
	}
	e.problemIs(e.call(http.MethodPost, "/v1/periods/"+oct+"/fx-revaluation", accountant, nil), "postFxRevaluation", 428, CodeIfMatchRequired)
	e.problemIs(post(oct), "postFxRevaluation", 409, fx.CodeNotEnded)
	e.clock.Set(time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC))
	p := post(oct)
	latest, _ := p.Body["latest"].(map[string]any)
	if p.Status != 201 || p.str("state") != "revalued" || latest == nil || latest["entry_id"] == nil || latest["reversal_id"] == nil ||
		latest["current"] != true || len(p.Body["lines"].([]any)) != 0 || p.str("latest", "posted_by", "kind") != "user" {
		t.Fatalf("postFxRevaluation: %d %s", p.Status, p.Raw)
	}
	if c := check(oct); c["severity"] != "ok" {
		t.Errorf("the close's check after the run: %v", c)
	}

	// A posting back-dated into October makes it stale; the next run posts what changed since.
	book(ar, porttest.Day(10, 20), "USD", "1000.00", "1.35", "1350.00")
	r = get(oct)
	item = r.Body["balances"].([]any)[0].(map[string]any)
	if r.str("state") != "due" || item["open"].(map[string]any)["amount"] != "11000.00" || item["carrying"].(map[string]any)["amount"] != "14350.00" ||
		item["change"].(map[string]any)["amount"] != "-50.00" || r.Body["latest"].(map[string]any)["current"] != false {
		t.Fatalf("October after a back-dated posting: %s", r.Raw)
	}
	if c := check(oct); c["severity"] != "error" || c["detail"] != "A posting dated in or before the period came after the revaluation: revalue it again in Accounting › Currencies" {
		t.Errorf("the stale run's check: %v", c)
	}
	if p := post(oct); p.Status != 201 || p.str("state") != "revalued" {
		t.Fatalf("the second run: %d %s", p.Status, p.Raw)
	}
	if r := get(oct); r.Body["balances"].([]any)[0].(map[string]any)["change"].(map[string]any)["amount"] != "0.00" {
		t.Errorf("October at the rate: %s", r.Raw)
	}

	// November: an EUR receivable with no rate is refused; with the rate, its reversal waits for December.
	book(ar, porttest.Day(11, 3), "EUR", "100.00", "1.45", "145.00")
	r = get(nov)
	if m := r.Body["missing"].([]any); len(m) != 2 || r.str("state") != "due" {
		t.Fatalf("November without rates: %s", r.Raw)
	}
	e.clock.Set(time.Date(2026, 12, 1, 2, 0, 0, 0, time.UTC))
	e.problemIs(post(nov), "postFxRevaluation", 422, fx.CodeNoRate)
	for _, cur := range []string{"USD", "EUR"} {
		if r := e.call(http.MethodPost, "/v1/fx/rates", accountant, map[string]any{"kind": "month", "currency": cur, "on": "2026-11-01", "quote": "1.40"}); r.Status != 201 {
			t.Fatalf("November's %s rate: %d %s", cur, r.Status, r.Raw)
		}
	}
	p = post(nov)
	if p.Status != 201 || p.Body["latest"].(map[string]any)["entry_id"] == nil || p.Body["latest"].(map[string]any)["reversal_id"] != nil {
		t.Fatalf("November's run, its reversal waiting: %d %s", p.Status, p.Raw)
	}
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	if run, err := journal.ReverseRevaluations(context.Background(), worker, e.books.Org, porttest.Day(12, 1)); err != nil || run.Waiting != 1 || run.Reversed != 0 {
		t.Errorf("before December opens: %+v %v", run, err)
	}
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: porttest.Day(12, 1), EndsOn: porttest.Day(12, 31)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if run, err := journal.ReverseRevaluations(context.Background(), worker, e.books.Org, porttest.Day(12, 1)); err != nil || run.Reversed != 1 {
		t.Errorf("once December is open: %+v %v", run, err)
	}
	if r := get(nov); r.Body["latest"].(map[string]any)["reversal_id"] == nil {
		t.Errorf("November's reversal: %s", r.Raw)
	}

	// Every revaluation entry and reversal replays from what its run recorded (ENG-06); the fixture's own entries have no
	// documents behind them, so they are left out.
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rp, err := journal.Replay(ctx, u)
		for _, m := range rp.Mismatches {
			if strings.HasPrefix(m.PostingIdentity, "fx_revaluation:") {
				t.Errorf("replay of %s: %s", m.PostingIdentity, m.Why)
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Close work: the Auditor previews it, an AR specialist does not see it, only those who run the checks post it.
	if r := e.call(http.MethodGet, "/v1/periods/"+oct+"/fx-revaluation", e.as(authz.Auditor), nil); r.Status != 200 {
		t.Errorf("the Auditor's preview: %d", r.Status)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/periods/"+oct+"/fx-revaluation", e.as(authz.ARSpecialist), nil), "getFxRevaluation", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/periods/"+oct+"/fx-revaluation", e.as(authz.Auditor), nil, "If-Match", get(oct).Header.Get("ETag")),
		"postFxRevaluation", 403, CodeForbidden)
	_ = fxAcc
}
