//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// feedOf is a daily feed that published the quotes it holds, by date.
type feedOf map[string][]ports.RateQuote

func (feedOf) Name() string { return "Test feed" }

func (f feedOf) Fetch(_ context.Context, day time.Time) ([]ports.RateQuote, error) {
	return f[day.Format(time.DateOnly)], nil
}

// Exchange rates and the cascade (FR-GL-05, ENG-40, RND-RATE; rev 0.70). The feed publishes USD and JPY on Friday 16
// October 2026 and USD on Monday 19 October; a posting on Sunday the 18th takes Friday's rate. The organisation's own
// rate for the 18th overrides the feed, and a second one replaces the first. EUR has no feed rate: two rates in
// October's monthly table give their average, with ⚠, until one is withdrawn (G4-025). GBP has no rate anywhere. The
// expected rates are worked by hand: 1 ÷ 0.77 = 1.2987012987…, (1.48 + 1.49) ÷ 2 = 1.485, 0.8521 ÷ 100 = 0.008521.
// req: FR-GL-05
// req: ENG-40
// req: RND-RATE
func TestExchangeRatesAndTheCascade(t *testing.T) {
	e := newEnv(t)
	accountant, auditor := e.as(authz.Accountant), e.as(authz.Auditor)
	lookup := func(cur, date string) response {
		t.Helper()
		return e.call(http.MethodGet, "/v1/fx/rate?currency="+cur+"&date="+date, accountant, nil)
	}
	found := func(cur, date, rate, source, step string, entries float64) {
		t.Helper()
		r := lookup(cur, date)
		if r.Status != 200 || r.str("rate") != rate || r.str("source") != source || r.str("step") != step || r.Body["entries"] != entries ||
			r.str("base_currency") != "SGD" {
			t.Errorf("%s on %s: want %s from %q (%s, %v entries), got %d %s", cur, date, rate, source, step, entries, r.Status, r.Raw)
		}
	}

	// Nothing yet: no rate anywhere.
	e.problemIs(lookup("USD", "2026-10-18"), "lookupFxRate", 422, fx.CodeNoRate)

	// The feed: the job stores the last week's days that have no rate yet, once.
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	feed := feedOf{
		"2026-10-16": {{Base: "SGD", Currency: "USD", Quote: "1.2953", Form: "base-per-unit"}, {Base: "SGD", Currency: "JPY", Quote: "0.8521", Form: "base-per-100"}},
		"2026-10-19": {{Base: "SGD", Currency: "USD", Quote: "1.2960", Form: "base-per-unit"}},
	}
	now := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC) // 10 am on Tuesday 20 October in Singapore
	run, err := fx.RecordFeed(context.Background(), worker, feed, now)
	if err != nil || run.Stored != 3 || len(run.Days) != 7 {
		t.Fatalf("the feed's first run: %+v, %v", run, err)
	}
	if run, err := fx.RecordFeed(context.Background(), worker, feed, now); err != nil || run.Stored != 0 || len(run.Days) != 5 {
		t.Errorf("the feed's second run fetches only the days still without a rate: %+v, %v", run, err)
	}
	if r := e.call(http.MethodGet, "/v1/fx/feed?currency=USD&from=2026-10-14&to=2026-10-20", accountant, nil); r.Status != 200 ||
		len(r.Body["items"].([]any)) != 2 {
		t.Errorf("listFeedRates: %d %s", r.Status, r.Raw)
	}
	found("USD", "2026-10-16", "1.2953", "Test feed daily feed of 2026-10-16", "feed", 1)
	found("USD", "2026-10-18", "1.2953", "Test feed daily feed of 2026-10-16", "feed", 1) // Sunday: Friday's rate
	found("USD", "2026-10-19", "1.296", "Test feed daily feed of 2026-10-19", "feed", 1)
	found("JPY", "2026-10-18", "0.008521", "Test feed daily feed of 2026-10-16", "feed", 1)
	found("USD", "2026-10-26", "1.296", "Test feed daily feed of 2026-10-19", "feed", 1) // the seventh day before still counts
	e.problemIs(lookup("USD", "2026-10-27"), "lookupFxRate", 422, fx.CodeNoRate)         // the eighth does not

	// The organisation's rate for a day overrides the feed; a second one replaces the first.
	enter := func(body map[string]any) response {
		t.Helper()
		return e.call(http.MethodPost, "/v1/fx/rates", accountant, body)
	}
	first := enter(map[string]any{"kind": "day", "currency": "USD", "on": "2026-10-18", "quote": "0.77", "quote_form": "foreign-per-base", "note": "Bank's rate"})
	if first.Status != 201 || first.str("rate") != "1.2987012987" || first.str("quote") != "0.77" || first.Header.Get("Location") != "/v1/fx/rates/"+first.str("id") {
		t.Fatalf("createFxRate: %d %s", first.Status, first.Raw)
	}
	found("USD", "2026-10-18", "1.2987012987", "organisation rate of 2026-10-18", "organisation", 1)
	if r := enter(map[string]any{"kind": "day", "currency": "USD", "on": "2026-10-18", "quote": "1.3"}); r.Status != 201 || r.str("quote_form") != "base-per-unit" {
		t.Fatalf("a second rate for the day: %d %s", r.Status, r.Raw)
	}
	found("USD", "2026-10-18", "1.3", "organisation rate of 2026-10-18", "organisation", 1)
	if r := e.call(http.MethodGet, "/v1/fx/rates/"+first.str("id"), auditor, nil); r.Status != 200 || r.Body["withdrawn"] != true ||
		r.str("withdrawn_reason") != fx.ReplacedReason {
		t.Errorf("the replaced rate: %d %s", r.Status, r.Raw)
	}

	// The monthly table: duplicates are averaged and shown until one is withdrawn (G4-025).
	eur := e.call(http.MethodPost, "/v1/fx/rates:bulk", accountant, map[string]any{"items": []map[string]any{
		{"kind": "month", "currency": "EUR", "on": "2026-10-01", "quote": "1.48"},
		{"kind": "month", "currency": "SGD", "on": "2026-10-01", "quote": "1"},
		{"kind": "month", "currency": "EUR", "on": "2026-10-01", "quote": "1.49"},
	}})
	results, _ := eur.Body["results"].([]any)
	if eur.Status != 200 || len(results) != 3 || eur.Body["saved"] != 2.0 || results[1].(map[string]any)["status"] != 422.0 {
		t.Fatalf("bulkCreateFxRates: %d %s", eur.Status, eur.Raw)
	}
	found("EUR", "2026-10-18", "1.485", "monthly table 2026-10, average of 2 entries", "month", 2)
	l := e.call(http.MethodGet, "/v1/fx/rates?currency=EUR", auditor, nil)
	if dups, _ := l.Body["duplicates"].([]any); l.Status != 200 || len(dups) != 1 || dups[0].(map[string]any)["month"] != "2026-10-01" {
		t.Errorf("the duplicates: %d %s", l.Status, l.Raw)
	}
	firstEUR := results[0].(map[string]any)["id"].(string)
	e.problemIs(e.call(http.MethodPost, "/v1/fx/rates/"+firstEUR+"/withdraw", accountant, map[string]any{"reason": "No If-Match"}), "withdrawFxRate", 428, CodeIfMatchRequired)
	if r := e.call(http.MethodPost, "/v1/fx/rates/"+firstEUR+"/withdraw", accountant, map[string]any{"reason": "Duplicate of the bank's rate"}, "If-Match", `"1"`); r.Status != 200 ||
		r.Body["withdrawn"] != true || r.str("withdrawn_reason") != "Duplicate of the bank's rate" || r.Header.Get("ETag") != `"2"` {
		t.Fatalf("withdrawFxRate: %d %s", r.Status, r.Raw)
	}
	found("EUR", "2026-10-18", "1.49", "monthly table 2026-10", "month", 1)
	if l := e.call(http.MethodGet, "/v1/fx/rates?currency=EUR", auditor, nil); len(l.Body["duplicates"].([]any)) != 0 || len(l.Body["items"].([]any)) != 1 {
		t.Errorf("after the withdrawal: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/fx/rates?currency=EUR&withdrawn=true", auditor, nil); len(l.Body["items"].([]any)) != 2 {
		t.Errorf("withdrawn rates stay listed: %s", l.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/fx/rates/"+firstEUR+"/withdraw", accountant, map[string]any{"reason": "Again"}, "If-Match", `"1"`), "withdrawFxRate", 412, CodeStale)
	e.problemIs(e.call(http.MethodPost, "/v1/fx/rates/"+firstEUR+"/withdraw", accountant, map[string]any{"reason": "Again"}, "If-Match", `"2"`), "withdrawFxRate", 409, fx.CodeWithdrawn)
	second := results[2].(map[string]any)["id"].(string)
	e.problemIs(e.call(http.MethodPost, "/v1/fx/rates/"+second+"/withdraw", accountant, map[string]any{"reason": "  "}, "If-Match", `"1"`), "withdrawFxRate", 422, fx.CodeReason)

	// No rate anywhere, and the refusals of an entry.
	r := lookup("GBP", "2026-10-18")
	e.problemIs(r, "lookupFxRate", 422, fx.CodeNoRate)
	if d := r.str("detail"); d != "✖ No exchange rate for GBP on 18 Oct 2026: none of the organisation's for the day, none from the daily feed in the 7 days before, none in the monthly table for October 2026." {
		t.Errorf("FX-01's words: %q", d)
	}
	e.problemIs(lookup("SGD", "2026-10-18"), "lookupFxRate", 422, CodeInvalid)
	for field, body := range map[string]map[string]any{
		"body.on":       {"kind": "month", "currency": "EUR", "on": "2026-10-15", "quote": "1.5"},
		"body.quote":    {"kind": "day", "currency": "EUR", "on": "2026-10-15", "quote": "1.12345678901"},
		"body.currency": {"kind": "day", "currency": "XXX", "on": "2026-10-15", "quote": "1.5"},
	} {
		r := enter(body)
		e.problemIs(r, "createFxRate", 422, CodeInvalid)
		if errs, _ := r.Body["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["code"] != fx.CodeEntry || errs[0].(map[string]any)["field"] != field {
			t.Errorf("createFxRate: want %s refused as %s, got %s", field, fx.CodeEntry, r.Raw)
		}
	}

	// The rates are month-end work: the Auditor reads them, an AR specialist does not see them, and only those who run the
	// close's checks enter them.
	e.problemIs(e.call(http.MethodGet, "/v1/fx/rates", e.as(authz.ARSpecialist), nil), "listFxRates", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/fx/rates", auditor, map[string]any{"kind": "day", "currency": "EUR", "on": "2026-10-15", "quote": "1.5"}),
		"createFxRate", 403, CodeForbidden)
}

// A manual journal's foreign-currency line without a rate of its own takes the cascade's rate for the journal's date
// when its checks run and when it posts, with that rate's source (ENG-40, FR-GL-05; rev 0.71); a typed rate stays the
// document's. A line with no rate anywhere has FX-01 in its checks, and posting is refused with it. Replay reads the rate
// from the entry, so a rate withdrawn later changes nothing.
// req: ENG-40
// req: FR-GL-05
func TestForeignCurrencyJournalsTakeTheCascadesRate(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	rate := e.call(http.MethodPost, "/v1/fx/rates", accountant, map[string]any{"kind": "day", "currency": "USD", "on": "2026-10-14", "quote": "1.35"})
	if rate.Status != 201 {
		t.Fatalf("createFxRate: %d %s", rate.Status, rate.Raw)
	}
	usd := func(date, cur, rate string) map[string]any {
		line := func(acc storage.Account, amount string) map[string]any {
			l := map[string]any{"account_id": acc.ID.String(), "amount": map[string]any{"amount": amount, "currency": cur}}
			if rate != "" {
				l["fx_rate"] = rate
			}
			return l
		}
		return map[string]any{"date": date, "memo": "Client paid in " + cur, "lines": []map[string]any{line(e.books.Bank, "100.00"), line(e.books.Sales, "-100.00")}}
	}
	create := func(body map[string]any) response {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/journals", accountant, body)
		if r.Status != 201 {
			t.Fatalf("createJournal: %d %s", r.Status, r.Raw)
		}
		return e.call(http.MethodGet, "/v1/journals/"+r.str("id"), accountant, nil)
	}
	firstCheck := func(j response) map[string]any {
		checks, _ := j.Body["checks"].([]any)
		if len(checks) == 0 {
			t.Fatalf("no checks: %s", j.Raw)
		}
		return checks[0].(map[string]any)
	}

	// The cascade's rate: the draft is OK, and the entry records the rate and where it came from.
	j := create(usd("2026-10-14", "USD", ""))
	if c := firstCheck(j); c["code"] != journal.StatusReady {
		t.Errorf("a USD draft with the day's rate: %v", c)
	}
	p := e.call(http.MethodPost, "/v1/journals/"+j.str("id")+"/post", accountant, nil, "If-Match", j.Header.Get("ETag"))
	entry, _ := p.Body["entry"].(map[string]any)
	lines, _ := entry["lines"].([]any)
	if p.Status != 200 || len(lines) != 2 {
		t.Fatalf("postJournal: %d %s", p.Status, p.Raw)
	}
	if l := lines[0].(map[string]any); l["fx_rate"] != "1.35" || l["fx_rate_source"] != "organisation rate of 2026-10-14" ||
		l["base_amount"].(map[string]any)["amount"] != "135.00" {
		t.Errorf("the line took %v", l)
	}

	// A typed rate is the document's own.
	typed := create(usd("2026-10-14", "USD", "1.36"))
	p = e.call(http.MethodPost, "/v1/journals/"+typed.str("id")+"/post", accountant, nil, "If-Match", typed.Header.Get("ETag"))
	if l := p.Body["entry"].(map[string]any)["lines"].([]any)[0].(map[string]any); l["fx_rate"] != "1.36" || l["fx_rate_source"] != "document" {
		t.Errorf("a typed rate: %v", l)
	}

	// No rate anywhere: FX-01 on each line, in its checks and when posting.
	eur := create(usd("2026-10-14", "EUR", ""))
	if c := firstCheck(eur); c["code"] != fx.CodeNoRate || c["field"] != "lines[1].fx_rate" || c["title"] != "No exchange rate for this currency" {
		t.Errorf("a EUR draft without a rate: %v", c)
	}
	refused := e.call(http.MethodPost, "/v1/journals/"+eur.str("id")+"/post", accountant, nil, "If-Match", eur.Header.Get("ETag"))
	e.problemIs(refused, "postJournal", 422, fx.CodeNoRate)
	if errs, _ := refused.Body["errors"].([]any); len(errs) != 2 || errs[1].(map[string]any)["field"] != "lines[1].fx_rate" {
		t.Errorf("FX-01 per line: %s", refused.Raw)
	}

	// Withdrawing the rate later changes nothing posted: replay reads the rate the entry kept.
	if w := e.call(http.MethodPost, "/v1/fx/rates/"+rate.str("id")+"/withdraw", accountant, map[string]any{"reason": "Test"}, "If-Match", `"1"`); w.Status != 200 {
		t.Fatalf("withdrawFxRate: %d %s", w.Status, w.Raw)
	}
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) != 0 {
			t.Errorf("replay after the rate was withdrawn: %+v", r.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
