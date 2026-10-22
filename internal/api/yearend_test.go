//go:build local

package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Year end on the close (FR-GL-10; rev 0.69): the first financial year is October 2026 alone, the second starts on 1
// November. October books S$1,000.00 of sales and S$300.00 straight to retained earnings (as opening balances would);
// November S$200.00 of sales and S$50.00 of rent. The first year's result, a S$1,000.00 profit, is what retained earnings
// carries into the second year on top of the S$300.00; nothing is posted for it. Closing October closes the first year.
// req: FR-GL-10
func TestTheYearEndOfAPeriod(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow, authz.Controller), f.as(f.weiling, authz.Owner, authz.Controller)
	oct, nov := e.books.Oct.ID.String(), e.books.Nov.ID.String()
	yearEnd := func(period string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/periods/"+period+"/year-end", ahkow, nil)
		if r.Status != 200 {
			t.Fatalf("getYearEnd: %d %s", r.Status, r.Raw)
		}
		return r
	}

	// Without the financial years there is no year end.
	e.problemIs(e.call(http.MethodGet, "/v1/periods/"+oct+"/year-end", ahkow, nil), "getYearEnd", 409, period.CodeNoYear)
	g := e.call(http.MethodGet, "/v1/organisation/profile", ahkow, nil)
	if r := e.call(http.MethodPatch, "/v1/organisation/profile", ahkow, map[string]any{"fy_start_month": 11, "books_start": "2026-10-01",
		"first_year_end": "2026-10-31"}, "If-Match", g.Header.Get("ETag")); r.Status != 200 {
		t.Fatalf("setting the years: %d %s", r.Status, r.Raw)
	}
	e.book(e.books.Sales, porttest.Day(10, 5), "-1000.00")
	e.book(e.books.Sales, porttest.Day(11, 3), "-200.00")
	e.book(e.books.Rent, porttest.Day(11, 4), "50.00")

	// Without a retained-earnings account nothing folds, and the answer says so by leaving it out.
	if r := yearEnd(oct); r.Body["retained_earnings"] != nil || r.str("result", "amount") != "-1000.00" {
		t.Fatalf("the year end without a retained-earnings account: %s", r.Raw)
	}
	var re storage.Account
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		re, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "3100", Name: "Retained earnings", Type: storage.Equity, Role: storage.RoleRetainedEarnings})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.book(re, porttest.Day(10, 1), "-300.00")

	// want compares fields by their dotted paths in the answer, numbers and booleans as written.
	want := func(r response, fields map[string]string) {
		t.Helper()
		for path, v := range fields {
			var at any = r.Body
			for _, k := range strings.Split(path, ".") {
				m, _ := at.(map[string]any)
				at = m[k]
			}
			if got := fmt.Sprint(at); got != v {
				t.Errorf("year %s: %s is %s, want %s", r.str("starts_on"), path, got, v)
			}
		}
	}
	want(yearEnd(oct), map[string]string{"number": "1", "starts_on": "2026-10-01", "ends_on": "2026-10-31", "periods": "1", "periods_closed": "0",
		"closed": "false", "last_period.id": oct, "income.amount": "-1000.00", "expenses.amount": "0.00", "result.amount": "-1000.00",
		"retained_earnings.code": "3100", "retained_earnings.start.amount": "0.00", "retained_earnings.posted.amount": "-300.00", "retained_earnings.end.amount": "-1300.00"})
	want(yearEnd(nov), map[string]string{"number": "2", "starts_on": "2026-11-01", "ends_on": "2027-10-31", "periods": "1", "periods_closed": "0",
		"closed": "false", "last_period.id": nov, "income.amount": "-200.00", "expenses.amount": "50.00", "result.amount": "-150.00",
		"retained_earnings.start.amount": "-1300.00", "retained_earnings.posted.amount": "0.00", "retained_earnings.end.amount": "-1450.00"})

	// Closing October, the first year's only month, closes the year; its figures stay as they were.
	checklistDone(t, e.store, e.books.Org, e.books.Oct.ID)
	state := e.call(http.MethodGet, "/v1/periods/"+oct+"/close", ahkow, nil)
	r := e.call(http.MethodPost, "/v1/periods/"+oct+"/close-checks", ahkow, nil, "If-Match", state.Header.Get("ETag"))
	if r.Status != 200 || r.Body["checks"].(map[string]any)["clean"] != true {
		t.Fatalf("October's checks: %d %s", r.Status, r.Raw)
	}
	state = e.call(http.MethodGet, "/v1/periods/"+oct+"/close", ahkow, nil)
	if c := e.call(http.MethodPost, "/v1/periods/"+oct+"/close", weiling, map[string]any{"checks_id": r.str("checks", "id"), "statement": "Reviewed."},
		"If-Match", state.Header.Get("ETag")); c.Status != 200 {
		t.Fatalf("closing October: %d %s", c.Status, c.Raw)
	}
	want(yearEnd(oct), map[string]string{"periods_closed": "1", "closed": "true", "result.amount": "-1000.00", "retained_earnings.end.amount": "-1300.00"})

	// Reading it is the close's right: an AR specialist does not see it.
	e.problemIs(e.call(http.MethodGet, "/v1/periods/"+oct+"/year-end", e.as(authz.ARSpecialist), nil), "getYearEnd", 403, CodeForbidden)
}
