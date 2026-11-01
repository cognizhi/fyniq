//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// fixture runs fn in a unit of work of e's organisation, as the fixture.
func (e *env) fixture(fn func(ctx context.Context, u storage.UnitOfWork) error) {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, fn); err != nil {
		e.t.Fatal(err)
	}
}

// checked runs every check and returns one as the Controller reads it.
func (e *env) checked(key string) response {
	e.t.Helper()
	e.runCatalogue()
	r := e.call(http.MethodGet, "/v1/checks/"+key, e.as(authz.Controller), nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("getCheck %s = %d: %s", key, r.Status, r.Raw)
	}
	return r
}

// itemsOf are a check's items as the API lists them.
func itemsOf(r response) []map[string]any {
	var out []map[string]any
	for _, x := range r.Body["items"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

// named finds the item of id among a check's items.
func itemNamed(r response, id string) map[string]any {
	for _, it := range itemsOf(r) {
		if it["id"] == id {
			return it
		}
	}
	return nil
}

// wantStatus fails unless the check's status is want.
func wantStatus(t *testing.T, r response, want string) {
	t.Helper()
	if r.Body["status"] != want {
		t.Errorf("%s: %v (%v), want %s: %s", r.Body["key"], r.Body["status"], r.Body["detail"], want, r.Raw)
	}
}

// The checks of the sales documents: a draft that cannot be issued (C.1 #13), an invoice issued late (C.1 #14), one
// overdue by more than 90 days (C.1 #15), a line worth nothing (C.1 #24) and a quote to follow up (C.1 #25); each item
// marked reviewed leaves its check until the item changes (rev 0.164).
// req: C.1 #13
// req: C.1 #14
// req: C.1 #15
// req: C.1 #24
// req: C.1 #25
// req: FR-CTL-01
func TestTheSalesChecks(t *testing.T) {
	t.Run("a draft invoice that cannot be issued, and a line worth nothing", func(t *testing.T) {
		e := newEnv(t)
		for _, key := range []string{"sales.invoices-cannot-post", "sales.document-lines"} {
			wantStatus(t, e.checked(key), "ok")
		}
		d := e.invoiceDraft("1", "100.00")
		d["lines"].([]map[string]any)[0]["account_id"] = e.books.Expenses.ID.String() // a group: it takes no postings
		bad := e.call(http.MethodPost, "/v1/invoices", e.as(authz.Accountant), d)
		free := e.call(http.MethodPost, "/v1/invoices", e.as(authz.Accountant), e.invoiceDraft("1", "0.00"))
		if bad.Status != http.StatusCreated || free.Status != http.StatusCreated {
			t.Fatalf("drafts = %d, %d: %s %s", bad.Status, free.Status, bad.Raw, free.Raw)
		}
		cannot := e.checked("sales.invoices-cannot-post")
		wantStatus(t, cannot, "error")
		if it := itemNamed(cannot, bad.str("id")); it == nil || it["kind"] != "invoice" || !strings.HasPrefix(it["label"].(string), "Draft of 2026-10-14") {
			t.Errorf("the draft that cannot be issued: %s", cannot.Raw)
		}
		lines := e.checked("sales.document-lines")
		wantStatus(t, lines, "warning")
		if it := itemNamed(lines, free.str("id")); it == nil || it["detail"] != "line 1 is worth nothing" {
			t.Errorf("the line worth nothing: %s", lines.Raw)
		}
	})

	t.Run("an invoice issued more than 30 days after its date, reviewed", func(t *testing.T) {
		e := newEnv(t)
		if r := e.call(http.MethodPost, "/v1/organisation/gst-registrations", e.as(authz.Controller),
			map[string]any{"registered_from": "2026-01-01", "gst_number": "202312345K"}); r.Status != http.StatusCreated {
			t.Fatalf("registering for GST = %d: %s", r.Status, r.Raw)
		}
		d := e.invoiceDraft("1", "100.00")
		d["date"] = "2026-10-01"
		c := e.call(http.MethodPost, "/v1/invoices", e.as(authz.Accountant), d)
		e.clock.Set(time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC))
		if r := e.call(http.MethodPost, "/v1/invoices/"+c.str("id")+"/issue", e.as(authz.Accountant), nil, "If-Match", c.Header.Get("ETag")); r.Status != http.StatusOK {
			t.Fatalf("issuing = %d: %s", r.Status, r.Raw)
		}
		late := e.checked("sales.invoices-to-review")
		wantStatus(t, late, "warning")
		it := itemNamed(late, c.str("id"))
		if it == nil || !strings.Contains(it["detail"].(string), "more than 30 days after its date") || it["version"] == "" {
			t.Fatalf("the invoice issued late: %s", late.Raw)
		}
		// Marked reviewed, it leaves the check at once and stays out when the checks run again.
		r := e.call(http.MethodPost, "/v1/checks/sales.invoices-to-review/reviews", e.as(authz.Accountant),
			map[string]any{"kind": "invoice", "id": c.str("id"), "note": "Agreed with the customer"})
		if r.Status != http.StatusCreated {
			t.Fatalf("reviewCheckItem = %d: %s", r.Status, r.Raw)
		}
		wantStatus(t, r, "ok")
		if again := e.checked("sales.invoices-to-review"); again.Body["status"] != "ok" || !strings.Contains(again.Body["detail"].(string), "1 item(s) marked reviewed") {
			t.Errorf("after the review and a run: %s", again.Raw)
		}
		if r := e.call(http.MethodPost, "/v1/checks/sales.invoices-to-review/reviews", e.as(authz.Bookkeeper),
			map[string]any{"kind": "invoice", "id": c.str("id")}); r.Status != http.StatusCreated {
			t.Errorf("marking it again = %d: %s", r.Status, r.Raw)
		}
		if r := e.call(http.MethodPost, "/v1/checks/sales.invoices-to-review/reviews", e.as(authz.Auditor),
			map[string]any{"kind": "invoice", "id": c.str("id")}); r.Status != http.StatusForbidden {
			t.Errorf("an Auditor marks it reviewed: %d", r.Status)
		}
	})

	t.Run("an invoice overdue by more than 90 days", func(t *testing.T) {
		e := newEnv(t)
		d := e.invoiceDraft("1", "100.00")
		d["date"], d["due_date"] = "2026-10-01", "2026-10-01"
		c := e.call(http.MethodPost, "/v1/invoices", e.as(authz.Accountant), d)
		if r := e.call(http.MethodPost, "/v1/invoices/"+c.str("id")+"/issue", e.as(authz.Accountant), nil, "If-Match", c.Header.Get("ETag")); r.Status != http.StatusOK {
			t.Fatalf("issuing = %d: %s", r.Status, r.Raw)
		}
		e.clock.Set(time.Date(2026, 12, 30, 9, 0, 0, 0, time.UTC)) // 90 days overdue: not yet
		wantStatus(t, e.checked("sales.invoices-overdue"), "ok")
		e.clock.Set(time.Date(2027, 1, 15, 9, 0, 0, 0, time.UTC))
		overdue := e.checked("sales.invoices-overdue")
		wantStatus(t, overdue, "warning")
		if it := itemNamed(overdue, c.str("id")); it == nil || !strings.Contains(it["detail"].(string), "106 days overdue") {
			t.Errorf("the invoice overdue: %s", overdue.Raw)
		}
	})

	t.Run("quotes to follow up", func(t *testing.T) {
		e := newEnv(t)
		sent, _ := e.sentQuote() // valid until 13 November
		wantStatus(t, e.checked("sales.quotes-to-follow-up"), "ok")
		accepted, tag := e.sentQuote()
		if r := e.call(http.MethodPost, "/v1/quotes/"+accepted+"/accept", e.as(authz.Accountant), map[string]any{}, "If-Match", tag); r.Status != http.StatusOK {
			t.Fatalf("accepting = %d: %s", r.Status, r.Raw)
		}
		todo := e.checked("sales.quotes-to-follow-up")
		wantStatus(t, todo, "warning")
		if it := itemNamed(todo, accepted); it == nil || !strings.Contains(it["detail"].(string), "not invoiced in full") {
			t.Errorf("the quote accepted and not invoiced: %s", todo.Raw)
		}
		if itemNamed(todo, sent) != nil {
			t.Errorf("a quote valid for weeks yet is listed: %s", todo.Raw)
		}
		e.clock.Set(time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC))
		if it := itemNamed(e.checked("sales.quotes-to-follow-up"), sent); it == nil || !strings.Contains(it["detail"].(string), "its validity ends 2026-11-13") {
			t.Errorf("the quote whose validity ends within the week is not listed")
		}
	})
}

// The checks of the purchases: a draft bill that cannot post (C.1 #16), a posted bill with a warning (C.1 #17), two
// bills alike (C.2 #10), a posted bill with no document (C.2 #20), and a bill whose input tax is due back (C.2 #48).
// req: C.1 #16
// req: C.1 #17
// req: C.2 #10
// req: C.2 #20
// req: C.2 #48
func TestThePurchasesChecks(t *testing.T) {
	t.Run("a draft bill that cannot post", func(t *testing.T) {
		e := newEnv(t)
		wantStatus(t, e.checked("purchases.bills-cannot-post"), "ok")
		d := e.billDraft("100.00", "9.00")
		d["lines"].([]map[string]any)[0]["account_id"] = e.books.Expenses.ID.String()
		bad := e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), d)
		if bad.Status != http.StatusCreated {
			t.Fatalf("draft = %d: %s", bad.Status, bad.Raw)
		}
		cannot := e.checked("purchases.bills-cannot-post")
		wantStatus(t, cannot, "error")
		if itemNamed(cannot, bad.str("id")) == nil {
			t.Errorf("the bill that cannot post: %s", cannot.Raw)
		}
	})

	t.Run("posted bills: a warning, two alike, no document", func(t *testing.T) {
		e := newEnv(t)
		if r := e.call(http.MethodPost, "/v1/organisation/gst-registrations", e.as(authz.Controller),
			map[string]any{"registered_from": "2026-01-01", "gst_number": "202312345K"}); r.Status != http.StatusCreated {
			t.Fatalf("registering for GST = %d: %s", r.Status, r.Raw)
		}
		for _, key := range []string{"purchases.bills-to-review", "purchases.possible-duplicates", "purchases.without-a-document"} {
			wantStatus(t, e.checked(key), "ok")
		}
		off, _ := e.postedBill("100.00", "8.00") // GST stated other than RND-GST-EXCL's 9.00
		review := e.checked("purchases.bills-to-review")
		wantStatus(t, review, "warning")
		if it := itemNamed(review, off); it == nil || it["detail"] == "" {
			t.Errorf("the bill with a warning: %s", review.Raw)
		}
		first, _ := e.postedBill("250.00", "22.50")
		second, _ := e.postedBill("250.00", "22.50") // the same supplier, number and total
		alike := e.checked("purchases.possible-duplicates")
		wantStatus(t, alike, "warning")
		if itemNamed(alike, second) == nil && itemNamed(alike, first) == nil {
			t.Errorf("the bills alike: %s", alike.Raw)
		}
		none := e.checked("purchases.without-a-document")
		wantStatus(t, none, "warning")
		if it := itemNamed(none, off); it == nil || it["detail"] != "Posted with no receipt or invoice attached" {
			t.Errorf("the bill without its document: %s", none.Raw)
		}
		withFile, _ := matrixBillEvidence(e)
		post := e.call(http.MethodGet, "/v1/bills/"+withFile, e.as(authz.Accountant), nil)
		if r := e.call(http.MethodPost, "/v1/bills/"+withFile+"/post", e.as(authz.Accountant), nil, "If-Match", post.Header.Get("ETag")); r.Status != http.StatusOK {
			t.Fatalf("posting the bill with its file = %d: %s", r.Status, r.Raw)
		}
		if itemNamed(e.checked("purchases.without-a-document"), withFile) != nil {
			t.Error("a bill with its file is listed as without one")
		}
	})

	t.Run("a bill with input tax claimed, unpaid 12 months after its due date", func(t *testing.T) {
		e := newEnv(t)
		if r := e.call(http.MethodPost, "/v1/organisation/gst-registrations", e.as(authz.Controller),
			map[string]any{"registered_from": "2026-01-01", "gst_number": "202312345K"}); r.Status != http.StatusCreated {
			t.Fatalf("registering for GST = %d: %s", r.Status, r.Raw)
		}
		id, _ := e.postedBill("100.00", "9.00") // due 30 days after 14 October: 13 November
		e.clock.Set(time.Date(2027, 11, 13, 9, 0, 0, 0, time.UTC))
		wantStatus(t, e.checked("purchases.input-tax-to-repay"), "ok")
		e.clock.Set(time.Date(2027, 11, 14, 9, 0, 0, 0, time.UTC))
		due := e.checked("purchases.input-tax-to-repay")
		wantStatus(t, due, "warning")
		if it := itemNamed(due, id); it == nil || !strings.Contains(it["detail"].(string), "input tax claimed on it is to be repaid") {
			t.Errorf("the bill whose input tax is due back: %s", due.Raw)
		}
	})
}

// The checks of the journals, the chart, the contacts and the rates: a draft journal that cannot post and a posted one
// whose reversal waits (C.1 #18); what posts by itself pointing at an inactive account, and accounts no section maps
// (C.1 #8); two contacts alike and two monthly rates (C.1 #9).
// req: C.1 #8
// req: C.1 #9
// req: C.1 #18
func TestTheJournalsSetupAndContactsChecks(t *testing.T) {
	e := newEnv(t)
	wantStatus(t, e.checked("journals.problems"), "ok")
	d := e.draft("100.00")
	d["lines"] = d["lines"].([]map[string]any)[:1] // one line: it does not balance
	bad := e.call(http.MethodPost, "/v1/journals", e.as(authz.Accountant), d)
	if bad.Status != http.StatusCreated {
		t.Fatalf("draft = %d: %s", bad.Status, bad.Raw)
	}
	w := e.draft("40.00")
	w["reverse_on"] = "2027-03-01" // no period covers it: the reversal waits
	waits := e.call(http.MethodPost, "/v1/journals", e.as(authz.Accountant), w)
	if r := e.call(http.MethodPost, "/v1/journals/"+waits.str("id")+"/post", e.as(authz.Accountant), nil, "If-Match", waits.Header.Get("ETag")); r.Status != http.StatusOK {
		t.Fatalf("posting the journal that reverses = %d: %s", r.Status, r.Raw)
	}
	journals := e.checked("journals.problems")
	wantStatus(t, journals, "error")
	if it := itemNamed(journals, bad.str("id")); it == nil || it["severity"] != "error" {
		t.Errorf("the draft that cannot post: %s", journals.Raw)
	}
	if it := itemNamed(journals, waits.str("id")); it == nil || it["severity"] != "warning" {
		t.Errorf("the journal whose reversal waits: %s", journals.Raw)
	}

	// The test books' accounts carry no report group: each is for review; an item pointing at an inactive account cannot post.
	chart := e.checked("setup.chart-problems")
	wantStatus(t, chart, "warning")
	if it := itemNamed(chart, e.books.Bank.ID.String()); it == nil || it["severity"] != "warning" {
		t.Errorf("the account no section maps: %s", chart.Raw)
	}
	var itemID string
	e.fixture(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "4100", Name: "Training", Type: storage.Revenue})
		if err != nil {
			return err
		}
		it, err := u.Items().Create(ctx, "TRAIN", storage.ItemDetails{Name: "Training day", Type: storage.ItemService, RevenueAccountID: &a.ID})
		if err != nil {
			return err
		}
		itemID = it.ID.String()
		_, err = u.Accounts().SetActive(ctx, a.ID, false)
		return err
	})
	chart = e.checked("setup.chart-problems")
	wantStatus(t, chart, "error")
	if it := itemNamed(chart, itemID); it == nil || it["severity"] != "error" || !strings.Contains(it["detail"].(string), "4100 Training, which is inactive") {
		t.Errorf("the item pointing at an inactive account: %s", chart.Raw)
	}

	wantStatus(t, e.checked("setup.contacts-and-rates"), "ok")
	e.customer()
	e.fixture(func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "ACME  Analytics Pte Ltd", Customer: true}); err != nil {
			return err
		}
		for _, rate := range []string{"1.3400000000", "1.3500000000"} {
			if _, err := u.FXRates().Enter(ctx, storage.NewFXRate{Currency: "USD", Kind: storage.FXRateMonth, On: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
				Rate: storage.Decimal(rate), Quote: storage.Decimal(rate), QuoteForm: "base-per-unit"}); err != nil {
				return err
			}
		}
		return nil
	})
	alike := e.checked("setup.contacts-and-rates")
	wantStatus(t, alike, "warning")
	kinds := map[string]bool{}
	for _, it := range itemsOf(alike) {
		kinds[it["kind"].(string)] = true
	}
	if !kinds["contact"] || !kinds["fx_month"] {
		t.Errorf("the contacts alike and the monthly rates: %s", alike.Raw)
	}
}
