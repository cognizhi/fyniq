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
	"github.com/cognizhi/fyniq/internal/ports"
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
