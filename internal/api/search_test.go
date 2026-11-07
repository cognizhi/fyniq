//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The global search (FR-UX-02; PRD-001 rev 0.190) finds an issued invoice by its customer's name, its reference and its
// total of S$1,000.00 (no GST in these books), a posted rent journal of S$1,900.00 above 1.5k, a draft bill by its
// supplier's reference, an account by its name and a contact by its name; an amount is not looked for in contacts.
// The group filter returns one kind. A claim's line "Taxi to Acme" is found too. An Employee finds their own taxi claim, never a colleague's, and searches no other
// kind; an AR specialist does not find journals or bills (SEC-34).
// req: FR-UX-02
// req: SEC-34
// req: experience/navigation Search by amount
// req: experience/navigation Type filters and keyboard navigation
// req: experience/navigation References and notes
// req: experience/navigation Only what the person may see (rev 0.190)
// asvs: V8.2.2
func TestTheGlobalSearchFindsWhatThePersonMaySee(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	invoice, _ := e.issuedInvoice("2", "500.00")
	journal, _ := e.postedJournal("1900.00")
	bill, _ := e.createBill("200.00", "18.00")
	travel, _ := e.claimSetup()
	ahkow, mei := c.as(c.ahkow, authz.Employee), c.as(c.mei, authz.Employee)
	mine := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "lines": []any{taxi(travel)}})
	theirs := e.call(http.MethodPost, "/v1/expense-claims", mei, map[string]any{"date": "2026-10-15", "lines": []any{taxi(travel)}})
	if mine.Status != http.StatusCreated || theirs.Status != http.StatusCreated {
		t.Fatalf("the claims: %d %s; %d %s", mine.Status, mine.Raw, theirs.Status, theirs.Raw)
	}

	find := func(tok, query string) (map[string][]map[string]any, map[string]map[string]any) {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/search?q="+strings.NewReplacer(" ", "+", ">", "%3E", ",", "%2C").Replace(query), tok, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("%q: %d %s", query, r.Status, r.Raw)
		}
		results, groups := map[string][]map[string]any{}, map[string]map[string]any{}
		for _, x := range r.Body["results"].([]any) {
			m := x.(map[string]any)
			results[m["group"].(string)] = append(results[m["group"].(string)], m)
		}
		for _, x := range r.Body["groups"].([]any) {
			m := x.(map[string]any)
			groups[m["key"].(string)] = m
		}
		return results, groups
	}
	acc := e.as(authz.Accountant)

	r, g := find(acc, "acme")
	if len(r["invoices"]) != 1 || r["invoices"][0]["id"] != invoice || !strings.Contains(r["invoices"][0]["title"].(string), "Acme Analytics Pte Ltd") ||
		r["invoices"][0]["amount"].(map[string]any)["amount"] != "1000.00" || r["invoices"][0]["link"] != "/sales/invoices/"+invoice || len(r["contacts"]) != 1 || g["invoices"]["count"] != float64(1) {
		t.Fatalf("acme: %v %v", r, g)
	}
	if r, _ := find(acc, "po-500"); len(r["invoices"]) != 1 || r["invoices"][0]["detail"] != "PO-500.00" {
		t.Errorf("by its reference: %v", r)
	}
	if r, g := find(acc, "1,000.00"); len(r["invoices"]) != 1 || len(r["contacts"]) != 0 || g["contacts"]["count"] != float64(0) {
		t.Errorf("by its total: %v %v", r, g)
	}
	if r, _ := find(acc, "> 1.5k"); len(r["journals"]) != 1 || r["journals"][0]["id"] != journal || len(r["invoices"]) != 0 {
		t.Errorf("above 1.5k: %v", r)
	}
	if r, _ := find(acc, "AU-200"); len(r["bills"]) != 1 || r["bills"][0]["id"] != bill || r["bills"][0]["status"] != "draft" || r["bills"][0]["amount"] != nil {
		t.Errorf("a draft bill by its supplier's reference: %v", r)
	}
	if r, _ := find(acc, "rent"); len(r["journals"]) != 1 || len(r["accounts"]) != 1 || r["accounts"][0]["title"] != "6100 Rent" {
		t.Errorf("rent: %v", r)
	}
	only := e.call(http.MethodGet, "/v1/search?q=acme&group=contacts", acc, nil)
	if res := only.Body["results"].([]any); len(res) != 1 || res[0].(map[string]any)["group"] != "contacts" {
		t.Errorf("the contacts alone: %s", only.Raw)
	}

	// An Employee finds their own claim alone, and searches nothing else.
	r, g = find(ahkow, "comfortdelgro")
	if len(r["claims"]) != 1 || r["claims"][0]["id"] != mine.str("id") || r["claims"][0]["link"] != "/me/claims/"+mine.str("id") || len(g) != 1 || g["claims"] == nil {
		t.Errorf("the Employee's search: %v %v", r, g)
	}
	// An AR specialist does not find journals or bills.
	if _, g := find(e.as(authz.ARSpecialist), "rent"); g["journals"] != nil || g["bills"] != nil || g["invoices"] == nil {
		t.Errorf("the AR specialist's kinds: %v", g)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/search?q=", acc, nil), "searchRecords", 422, CodeInvalid)
}
