//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/subscription"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-15, FR-KPI-02 through the API: a contract of an active customer with its dated amendments, and the schedule and contracted MRR derived from them.
// An upgrade raises and a downgrade lowers the value in force; a cancellation ends it and a renewal starts it again; amendments only go forward in
// time; the contract's ETag guards each; a month that is closed takes no amendment; the contracted MRR sums the active contracts by currency.
// req: FR-AR-15
// req: FR-KPI-02
// asvs: V2.3.1
func TestASubscriptionContractIsAmendedFromDatedDaysAndItsMRRIsDerived(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	customer := e.customer()
	post := func(body map[string]any) response {
		return e.call(http.MethodPost, "/v1/subscription-contracts", acc, body)
	}
	e.problemIs(post(map[string]any{"contact_id": uuid.NewString(), "name": "Growth", "currency": "SGD", "starts_on": "2026-01-15", "mrr": "100.00"}),
		"createSubscriptionContract", 422, subscription.CodeContact)
	e.problemIs(post(map[string]any{"contact_id": customer, "name": "Growth", "currency": "SGD", "starts_on": "2026-01-15", "ends_on": "2026-01-01", "mrr": "100.00"}),
		"createSubscriptionContract", 422, subscription.CodeTerms)
	made := post(map[string]any{"contact_id": customer, "name": "Growth plan", "currency": "SGD", "starts_on": "2026-01-15", "ends_on": "2026-12-31", "mrr": "100.00"})
	if made.Status != http.StatusCreated || made.str("status") != "active" || made.str("mrr", "amount") != "100.00" || made.Header.Get("ETag") != `"1"` {
		t.Fatalf("createSubscriptionContract = %d: %s", made.Status, made.Raw)
	}
	id := made.str("id")
	amend := func(tag string, body map[string]any) response {
		return e.call(http.MethodPost, "/v1/subscription-contracts/"+id+"/amendments", acc, body, "If-Match", tag)
	}
	if r := e.call(http.MethodPost, "/v1/subscription-contracts/"+id+"/amendments", acc, map[string]any{"kind": "upgrade", "effective_on": "2026-04-01", "mrr": "150.00", "reason": "x"}); r.Status != 428 {
		t.Errorf("an amendment without If-Match: %d %s", r.Status, r.Raw)
	}
	// A downgrade that raises, an upgrade that lowers, and an amendment before the start are refused.
	e.problemIs(amend(`"1"`, map[string]any{"kind": "downgrade", "effective_on": "2026-04-01", "mrr": "150.00", "reason": "Wrong way"}), "amendSubscriptionContract", 422, subscription.CodeAmount)
	e.problemIs(amend(`"1"`, map[string]any{"kind": "upgrade", "effective_on": "2026-01-01", "mrr": "150.00", "reason": "Too early"}), "amendSubscriptionContract", 422, subscription.CodeDate)
	e.problemIs(amend(`"1"`, map[string]any{"kind": "renew", "effective_on": "2026-04-01", "mrr": "150.00", "reason": "Not ended"}), "amendSubscriptionContract", 422, subscription.CodeKind)
	up := amend(`"1"`, map[string]any{"kind": "upgrade", "effective_on": "2026-04-01", "mrr": "150.00", "reason": "The customer moved to the larger plan"})
	if up.Status != http.StatusCreated || up.Header.Get("ETag") != `"2"` {
		t.Fatalf("the upgrade: %d %s", up.Status, up.Raw)
	}
	e.problemIs(amend(`"1"`, map[string]any{"kind": "downgrade", "effective_on": "2026-07-01", "mrr": "120.00", "reason": "Stale"}), "amendSubscriptionContract", 412, CodeStale)
	e.problemIs(amend(`"2"`, map[string]any{"kind": "downgrade", "effective_on": "2026-03-01", "mrr": "120.00", "reason": "Back in time"}), "amendSubscriptionContract", 422, subscription.CodeDate)
	if r := amend(`"2"`, map[string]any{"kind": "downgrade", "effective_on": "2026-07-01", "mrr": "120.00", "reason": "Smaller team"}); r.Status != http.StatusCreated {
		t.Fatalf("the downgrade: %d %s", r.Status, r.Raw)
	}
	if r := amend(`"3"`, map[string]any{"kind": "cancel", "effective_on": "2026-09-10", "reason": "The customer left"}); r.Status != http.StatusCreated {
		t.Fatalf("the cancellation: %d %s", r.Status, r.Raw)
	}
	e.problemIs(amend(`"4"`, map[string]any{"kind": "upgrade", "effective_on": "2026-10-01", "mrr": "200.00", "reason": "It ended"}), "amendSubscriptionContract", 422, subscription.CodeKind)
	ren := amend(`"4"`, map[string]any{"kind": "renew", "effective_on": "2026-11-01", "mrr": "130.00", "reason": "The customer came back"})
	if ren.Status != http.StatusCreated {
		t.Fatalf("the renewal: %d %s", ren.Status, ren.Raw)
	}
	// The schedule is the value in force on each month's last day (worked by hand: 100 x3, 150 x3, 120 x2, ended x2, 130 x2).
	got := e.call(http.MethodGet, "/v1/subscription-contracts/"+id, acc, nil)
	sched := got.Body["schedule"].([]any)
	want := []string{"100.00", "100.00", "100.00", "150.00", "150.00", "150.00", "120.00", "120.00", "0.00", "0.00", "130.00"}
	if got.Status != 200 || len(sched) < len(want) {
		t.Fatalf("getSubscriptionContract = %d: %s", got.Status, got.Raw)
	}
	for i, w := range want {
		if m := sched[i].(map[string]any)["mrr"].(map[string]any)["amount"]; m != w {
			t.Errorf("month %d: %v, want %s", i+1, m, w)
		}
	}
	if len(got.Body["amendments"].([]any)) != 4 {
		t.Errorf("its amendments: %s", got.Raw)
	}
	// Contracted MRR on a day sums the active contracts by currency.
	other := post(map[string]any{"contact_id": customer, "name": "Seats", "currency": "SGD", "starts_on": "2026-06-01", "mrr": "40.50"})
	if other.Status != http.StatusCreated {
		t.Fatalf("a second contract: %d %s", other.Status, other.Raw)
	}
	for _, x := range []struct{ on, mrr string }{{"2026-05-31", "150.00"}, {"2026-08-31", "160.50"}, {"2026-10-15", "40.50"}, {"2026-12-01", "170.50"}} {
		r := e.call(http.MethodGet, "/v1/subscription-mrr?as_at="+x.on, acc, nil)
		tot, _ := r.Body["totals"].([]any)
		if r.Status != 200 || len(tot) != 1 || tot[0].(map[string]any)["mrr"].(map[string]any)["amount"] != x.mrr {
			t.Errorf("contracted MRR on %s: %d %s, want %s", x.on, r.Status, r.Raw, x.mrr)
		}
	}
	list := e.call(http.MethodGet, "/v1/subscription-contracts?contact_id="+customer, acc, nil)
	if list.Status != 200 || len(list.Body["items"].([]any)) != 2 {
		t.Errorf("listSubscriptionContracts: %d %s", list.Status, list.Raw)
	}
	// The converted figure is a separate one: each currency stays as it is, USD at the day's rate becomes base currency once, labelled with the rate and its date; a day with no rate
	// names the currency it left out.
	if r := e.call(http.MethodPost, "/v1/fx/rates", acc, map[string]any{"kind": "day", "currency": "USD", "on": "2026-10-15", "quote": "0.8", "quote_form": "foreign-per-base", "note": "Bank's rate"}); r.Status != http.StatusCreated {
		t.Fatalf("the USD rate: %d %s", r.Status, r.Raw)
	}
	if r := post(map[string]any{"contact_id": customer, "name": "Overseas seats", "currency": "USD", "starts_on": "2026-09-01", "mrr": "20.00"}); r.Status != http.StatusCreated {
		t.Fatalf("a USD contract: %d %s", r.Status, r.Raw)
	}
	mrr := e.call(http.MethodGet, "/v1/subscription-mrr?as_at=2026-10-15", acc, nil)
	inBase, _ := mrr.Body["in_base"].(map[string]any)
	rates, _ := inBase["rates"].([]any)
	if mrr.Status != 200 || len(mrr.Body["totals"].([]any)) != 2 || inBase["mrr"].(map[string]any)["amount"] != "65.50" || len(rates) != 2 {
		t.Fatalf("the converted figure: %d %s", mrr.Status, mrr.Raw)
	}
	var usd map[string]any
	for _, x := range rates {
		if x.(map[string]any)["currency"] == "USD" {
			usd = x.(map[string]any)
		}
	}
	if usd == nil || usd["rate"] != "1.25" || usd["rate_date"] != "2026-10-15" {
		t.Errorf("the USD rate it was converted at: %v", usd)
	}
	later := e.call(http.MethodGet, "/v1/subscription-mrr?as_at=2026-12-01", acc, nil)
	if lb, _ := later.Body["in_base"].(map[string]any); later.Status != 200 || len(lb["missing"].([]any)) != 1 || lb["missing"].([]any)[0] != "USD" || lb["mrr"].(map[string]any)["amount"] != "170.50" {
		t.Errorf("a day with no USD rate: %d %s", later.Status, later.Raw)
	}
	// A closed month takes no amendment.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		periods, err := u.Periods().List(ctx)
		for _, p := range periods {
			if err == nil && p.StartsOn.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
				_, err = u.Periods().Close(ctx, p.ID)
			}
		}
		return err
	})
	e.problemIs(e.call(http.MethodPost, "/v1/subscription-contracts/"+other.str("id")+"/amendments", acc, map[string]any{"kind": "upgrade", "effective_on": "2026-10-20", "mrr": "99.00", "reason": "Closed"},
		"If-Match", `"1"`), "amendSubscriptionContract", 422, subscription.CodeDate)
}

// matrixSubscriptions are the authorisation matrix's rows for subscription contracts.
var matrixSubscriptions = map[string]func(e *env, tok string) (response, func() bool){
	"addSubscriptionContractEvidence": func(e *env, tok string) (response, func() bool) {
		return e.upload("/v1/subscription-contracts/"+uuid.NewString()+"/evidence", tok, map[string]string{"evidence_type": "order form"}, "o.pdf", []byte("%PDF-1.7 matrix "+uuid.NewString()), "If-Match", `"1"`), nil
	},
	"listSubscriptionContractEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/subscription-contracts/"+uuid.NewString()+"/evidence", tok, nil), nil
	},
	"createSubscriptionContract": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/subscription-contracts", tok, map[string]any{"contact_id": uuid.NewString(), "name": "Plan", "currency": "SGD", "starts_on": "2026-01-15", "mrr": "10.00"}), nil
	},
	"listSubscriptionContracts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/subscription-contracts", tok, nil), nil
	},
	"getSubscriptionContract": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/subscription-contracts/"+uuid.NewString(), tok, nil), nil
	},
	"amendSubscriptionContract": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/subscription-contracts/"+uuid.NewString()+"/amendments", tok, map[string]any{"kind": "cancel", "effective_on": "2026-10-30", "reason": "x"}, "If-Match", `"1"`), nil
	},
	"getContractedMRR": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/subscription-mrr", tok, nil), nil
	},
}

// FR-AR-15, FR-DOC-05 through the API: a contract's files (the signed order form, an amendment letter) are evidence on a document of the contract's own, committed at once and append-only, a file
// may name the amendment it supports, and only that contract's own amendments can be named; a stale contract version, a reader, another contract's amendment and a file that is no document are refused.
// req: FR-AR-15
// req: FR-DOC-05
// asvs: V8.2.2
func TestAContractsFilesAreEvidenceOnItsOwnDocumentAndNameOnlyItsOwnAmendments(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	customer := e.customer()
	mk := func(name string) response {
		r := e.call(http.MethodPost, "/v1/subscription-contracts", acc, map[string]any{"contact_id": customer, "name": name, "currency": "SGD", "starts_on": "2026-01-15", "mrr": "100.00"})
		if r.Status != http.StatusCreated {
			t.Fatalf("a contract: %d %s", r.Status, r.Raw)
		}
		return r
	}
	one, two := mk("One"), mk("Two")
	up := e.call(http.MethodPost, "/v1/subscription-contracts/"+one.str("id")+"/amendments", acc, map[string]any{"kind": "upgrade", "effective_on": "2026-04-01", "mrr": "150.00", "reason": "Bigger"}, "If-Match", one.Header.Get("ETag"))
	if up.Status != http.StatusCreated {
		t.Fatalf("the upgrade: %d %s", up.Status, up.Raw)
	}
	other := e.call(http.MethodPost, "/v1/subscription-contracts/"+two.str("id")+"/amendments", acc, map[string]any{"kind": "upgrade", "effective_on": "2026-04-01", "mrr": "150.00", "reason": "Bigger"}, "If-Match", two.Header.Get("ETag"))
	amendmentOf := func(r response) string { return r.Body["amendments"].([]any)[0].(map[string]any)["id"].(string) }
	url := "/v1/subscription-contracts/" + one.str("id") + "/evidence"
	pdf := func(label string) []byte { return []byte("%PDF-1.7 " + label + " " + uuid.NewString()) }
	attach := func(who, path string, fields map[string]string, tag string) response {
		return e.upload(path, who, fields, "order.pdf", pdf("order"), "If-Match", tag)
	}
	tag := e.call(http.MethodGet, "/v1/subscription-contracts/"+one.str("id"), acc, nil).Header.Get("ETag")
	if r := attach(e.as(authz.Auditor), url, map[string]string{"evidence_type": "order form"}, tag); r.Status != http.StatusForbidden {
		t.Errorf("a reader attaching: %d %s", r.Status, r.Raw)
	}
	e.problemIs(attach(acc, url, map[string]string{"evidence_type": "order form"}, `"1"`), "addSubscriptionContractEvidence", 412, CodeStale)
	first := attach(acc, url, map[string]string{"evidence_type": "order form", "note": "Signed 14 Jan"}, tag)
	if first.Status != http.StatusCreated || first.str("file_name") != "order.pdf" {
		t.Fatalf("the contract's file: %d %s", first.Status, first.Raw)
	}
	// A letter for the amendment names it; another contract's amendment, and one that does not exist, cannot be named.
	letter := attach(acc, url+"?amendment_id="+amendmentOf(up), map[string]string{"evidence_type": "amendment letter"}, tag)
	if letter.Status != http.StatusCreated || !strings.Contains(letter.str("note"), "Supports the upgrade from 2026-04-01") {
		t.Fatalf("the amendment's file: %d %s", letter.Status, letter.Raw)
	}
	for _, bad := range []string{amendmentOf(other), uuid.NewString()} {
		if r := attach(acc, url+"?amendment_id="+bad, map[string]string{"evidence_type": "amendment letter"}, tag); r.Status != http.StatusUnprocessableEntity {
			t.Errorf("an amendment that is not this contract's: %d %s", r.Status, r.Raw)
		}
	}
	// A file that is no document is refused, and the list shows exactly the two, on this contract only.
	if r := e.upload(url, acc, map[string]string{"evidence_type": "order form"}, "x.exe", []byte("MZ\x90\x00"), "If-Match", tag); r.Status < 400 {
		t.Errorf("an executable attached: %d %s", r.Status, r.Raw)
	}
	if l := e.call(http.MethodGet, url, e.as(authz.Auditor), nil); l.Status != 200 || len(l.Body["items"].([]any)) != 2 {
		t.Errorf("listSubscriptionContractEvidence: %d %s", l.Status, l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/subscription-contracts/"+two.str("id")+"/evidence", acc, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 0 {
		t.Errorf("another contract's files: %d %s", l.Status, l.Raw)
	}
}
