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
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A bad debt that bore GST is in the relief queue with the GST in the part written off and the dates of the pack: an invoice of 14 Oct 2026 with 0.90 of GST on 10.90, written
// off for 10.00 on 31 Oct 2026, carries 0.83 of GST (10.00 x 0.90 / 10.90 = 0.8257, to the cent), may be claimed from 14 Oct 2027 and until 13 Oct 2031, and on 15 Oct 2026 is not yet
// eligible (FR-AR-11).
// req: FR-AR-11
func TestABadDebtIsInTheReliefQueueWithItsDates(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady() // GST-registered from 1 Sep 2026
	if r := e.call(http.MethodGet, "/v1/tax/bad-debt-relief", e.as(authz.TaxAgent), nil); r.Status != 200 || strings.Contains(r.Raw, `"write_off_id"`) {
		t.Fatalf("an empty queue: %d %s", r.Status, r.Raw)
	}
	e.postedWriteOff()
	r := e.call(http.MethodGet, "/v1/tax/bad-debt-relief", e.as(authz.TaxAgent), nil)
	items, _ := r.Body["items"].([]any)
	if r.Status != 200 || len(items) != 1 {
		t.Fatalf("the queue: %d %s", r.Status, r.Raw)
	}
	it := items[0].(map[string]any)
	gst, _ := it["gst"].(map[string]any)
	if it["supply_date"] != "2026-10-14" || it["written_off_on"] != "2026-10-31" || it["eligible_from"] != "2027-10-14" || it["claim_by"] != "2031-10-13" || it["status"] != "not_yet" ||
		gst["amount"] != "0.83" || it["pack"] != "sg-tax-filings@2026.1" {
		t.Errorf("the item is %v", it)
	}
}

// The working paper takes the year's profit and revenue from the ledger and the rest from the packs: an invoice of S$1,000.00 is S$1,000.00 of profit and revenue, so under the partial
// exemption 750.00 is exempt (75% of the first 10,000), 250.00 is taxed at 17% (42.50), YA 2027 has no announced rebate in sg-cit (so none is given) and 42.50 is payable; the form is C-S (Lite), with C-S as the
// option; an adjustment of 400.00 added back makes the profit 1,400.00 (exempt 1,050.00, taxed 350.00, tax 59.50, no rebate). A reader of the books cannot ask for it, and what it
// cannot work it refuses with the field.
// req: FR-TAX-06
func TestTheTaxWorkingPaperIsWorkedFromTheLedger(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	e.issuedInvoice("1", "1000.00")
	ctl := e.as(authz.Controller)
	tag := e.call(http.MethodGet, "/v1/organisation/profile", ctl, nil).Header.Get("ETag")
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", ctl, map[string]any{"fy_start_month": 10, "books_start": "2026-10-01", "first_year_end": "2027-09-30"}, "If-Match", tag); p.Status != 200 {
		t.Fatalf("the financial year: %d %s", p.Status, p.Raw)
	}
	tax := e.as(authz.TaxAgent)
	r := e.call(http.MethodPost, "/v1/tax/working-paper", tax, map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte"})
	if r.Status != 200 {
		t.Fatalf("the paper: %d %s", r.Status, r.Raw)
	}
	if r.Body["ya"] != 2027.0 || r.str("revenue") != "1000.00" || r.str("profit_before_tax") != "1000.00" || r.str("exempt") != "750.00" || r.str("normal_chargeable") != "250.00" ||
		r.str("tax_before_rebate") != "42.50" || r.str("rebate") != "0.00" || r.str("tax_payable_rounded") != "42.50" || r.str("form") != "C-S (Lite)" || r.str("optional_form") != "C-S" ||
		r.str("cit_pack") != "sg-cit@2026.1" || r.str("forms_pack") != "sg-tax-filings@2026.1" {
		t.Errorf("the paper is %s", r.Raw)
	}
	r = e.call(http.MethodPost, "/v1/tax/working-paper", tax, map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte",
		"adjustments": []map[string]any{{"kind": "non_deductible", "amount": "400.00", "reason": "Fines", "evidence": true}}})
	if r.Status != 200 || r.str("profit_before_tax") != "1000.00" || r.str("adjustments") != "400.00" || r.str("chargeable_income") != "1400.00" || r.str("exempt") != "1050.00" ||
		r.str("tax_before_rebate") != "59.50" || r.str("rebate") != "0.00" || r.str("tax_payable_rounded") != "59.50" {
		t.Errorf("with the adjustment: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/tax/working-paper", tax, map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte",
		"adjustments": []map[string]any{{"kind": "non_deductible", "amount": "1.00", "reason": " "}}}); r.Status != 422 || !strings.Contains(r.Raw, "adjustments[0].reason") {
		t.Errorf("an adjustment with no reason: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/tax/working-paper", e.as(authz.Auditor), map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte"}), "workTaxPaper", 403, CodeForbidden)
}

// A year with no revenue and no expense line is a nil working paper, not a failure: the authorisation matrix asks it of an organisation whose books are empty (FR-TAX-06).
// req: FR-TAX-06
func TestAYearWithNoLinesIsANilWorkingPaper(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	r := e.call(http.MethodPost, "/v1/tax/working-paper", e.as(authz.Accountant), map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte"})
	if r.Status != 200 || r.str("revenue") != "0.00" || r.str("tax_payable_rounded") != "0.00" || r.str("form") != "C-S (Lite)" {
		t.Fatalf("an empty year: %d %s", r.Status, r.Raw)
	}
}

// With no pack activated the organisation has no update to accept, and the answer says so.
// req: ENG-33
func TestNoPackUpdateIsOfferedWhileNothingIsActivated(t *testing.T) {
	e := newEnv(t)
	r := e.call(http.MethodGet, "/v1/statutory-packs/updates", e.as(authz.Controller), nil)
	if r.Status != 200 || strings.Contains(r.Raw, `"available"`) {
		t.Fatalf("the updates: %d %s", r.Status, r.Raw)
	}
}

func init() {
	matrixTaxFilings["getBadDebtRelief"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/tax/bad-debt-relief", tok, nil), nil
	}
	matrixTaxFilings["workTaxPaper"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/tax/working-paper", tok, map[string]any{"financial_year_end": "2026-11-30", "scheme": "pte"}, "Prefer", "return=preview"), nil
	}
	matrixTaxFilings["listStatutoryPackUpdates"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/statutory-packs/updates", tok, nil), nil
	}
}

// matrixTaxFilings are the authorisation matrix's rows for the tax filings (FR-AR-11, FR-TAX-06, ENG-33), filled by the init above.
var matrixTaxFilings = map[string]func(e *env, tok string) (response, func() bool){}

// GST bad debt relief is claimed through the API (FR-AR-11, RND-BDR [accountant to confirm]): the invoice of 14 Oct 2026 with 0.90 of GST on 10.90, written off for 10.00 on 31 Oct 2026, claims
// 0.83 (10.00 x 0.90 / 10.90 = 0.8257). It cannot be claimed before 14 Oct 2027 (twelve months from the supply) or after today, once only while a claim of it lives, and not for a write-off that
// is not posted; a claim posts Dr the GST control account and Cr the account the write-off took the debt to, shows on the queue's row, and a void reverses it, after which the write-off can be claimed
// again. A write-off with a live claim cannot be voided: the database refuses (FYT02).
// req: FR-AR-11
// req: RND-BDR
// req: QA-SEC-03
// asvs: V4.1.3, V8.2.1
// nested reads a string nested in a response's body.
func nested(r response, path ...string) string {
	var cur any = r.Body
	for _, k := range path {
		m, _ := cur.(map[string]any)
		cur = m[k]
	}
	s, _ := cur.(string)
	return s
}

func TestGSTBadDebtReliefIsClaimedOnceAndVoided(t *testing.T) {
	e := newEnv(t)
	e.invoiceNowReady()
	woID, _ := e.postedWriteOff()
	tax := e.as(authz.TaxAgent)
	claim := func(date string, extra ...string) response {
		return e.call(http.MethodPost, "/v1/tax/bad-debt-relief/claims", tax, map[string]any{"write_off_id": woID, "date": date, "f5_period_end": "2027-12-31", "note": "Three reminders"}, extra...)
	}
	e.problemIs(claim("2026-11-30"), "claimBadDebtRelief", 422, "RELIEF-09") // after today, 15 Oct 2026
	e.clock.Set(time.Date(2027, 10, 20, 9, 0, 0, 0, time.UTC))
	if err := e.store.Do(storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}}), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: time.Date(2027, 10, 1, 0, 0, 0, 0, time.UTC), EndsOn: time.Date(2027, 10, 31, 0, 0, 0, 0, time.UTC)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.problemIs(claim("2027-10-13"), "claimBadDebtRelief", 422, "RELIEF-07") // the day before the twelve months are over
	e.problemIs(e.call(http.MethodPost, "/v1/tax/bad-debt-relief/claims", e.as(authz.Auditor), map[string]any{"write_off_id": woID, "date": "2027-10-14"}), "claimBadDebtRelief", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPost, "/v1/tax/bad-debt-relief/claims", tax, map[string]any{"write_off_id": "01928f6e-7a3b-7c41-9d2e-5f8a1b3c0000", "date": "2027-10-14"}), "claimBadDebtRelief", 404, CodeNotFound)
	if p := claim("2027-10-14", "Prefer", "return=preview"); p.Status != 200 || nested(p, "claim", "gst", "amount") != "0.83" {
		t.Fatalf("the preview: %d %s", p.Status, p.Raw)
	}
	if q := e.call(http.MethodGet, "/v1/tax/bad-debt-relief", tax, nil); strings.Contains(q.Raw, `"claim"`) {
		t.Fatalf("a preview saved a claim: %s", q.Raw)
	}
	r := claim("2027-10-14")
	if r.Status != http.StatusCreated || nested(r, "claim", "gst", "amount") != "0.83" || nested(r, "claim", "status") != "posted" || nested(r, "claim", "pack") != "sg-tax-filings@2026.1" || nested(r, "claim", "f5_period_end") != "2027-12-31" ||
		nested(r, "claim", "eligible_from") != "2027-10-14" || nested(r, "claim", "claim_by") != "2031-10-13" || nested(r, "entry", "rule_version") != "bad_debt_relief@1" {
		t.Fatalf("the claim: %d %s", r.Status, r.Raw)
	}
	lines, _ := r.Body["entry"].(map[string]any)["lines"].([]any)
	if len(lines) != 2 {
		t.Fatalf("the entry's lines: %s", r.Raw)
	}
	var dr, cr string
	for _, l := range lines {
		amount := l.(map[string]any)["amount"].(map[string]any)["amount"].(string)
		if strings.HasPrefix(amount, "-") {
			cr = amount
		} else {
			dr = amount
		}
	}
	if dr != "0.83" || cr != "-0.83" {
		t.Errorf("Dr %s, Cr %s", dr, cr)
	}
	id, tag := nested(r, "claim", "id"), r.Header.Get("ETag")
	e.problemIs(claim("2027-10-15"), "claimBadDebtRelief", 409, "RELIEF-04")
	if g := e.call(http.MethodGet, "/v1/tax/bad-debt-relief/claims/"+id, tax, nil); g.Status != 200 || nested(g, "gst", "amount") != "0.83" || g.Header.Get("ETag") != tag {
		t.Errorf("the claim: %d %s", g.Status, g.Raw)
	}
	if q := e.call(http.MethodGet, "/v1/tax/bad-debt-relief", tax, nil); !strings.Contains(q.Raw, `"claim":{"date":"2027-10-14"`) && !strings.Contains(q.Raw, id) {
		t.Errorf("the queue does not show the claim: %s", q.Raw)
	}
	// the database refuses a write-off with a live claim being reversed
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if _, err := (journal.WriteOffs{Store: e.store}).Void(ctx, e.books.Org, uuid.MustParse(woID), time.Date(2027, 10, 20, 0, 0, 0, 0, time.UTC), "Reinstated"); err == nil || !strings.Contains(err.Error(), "claimed") {
		t.Errorf("voiding a write-off with a live claim: %v", err)
	}
	// the void: before the claim is refused, a missing If-Match is refused, then it reverses
	e.problemIs(e.call(http.MethodPost, "/v1/tax/bad-debt-relief/claims/"+id+"/void", tax, map[string]any{"date": "2027-10-13", "reason": "Wrong"}, "If-Match", tag), "voidBadDebtReliefClaim", 422, "RELIEF-13")
	v := e.call(http.MethodPost, "/v1/tax/bad-debt-relief/claims/"+id+"/void", tax, map[string]any{"date": "2027-10-20", "reason": "The debtor paid"}, "If-Match", tag)
	if v.Status != 200 || nested(v, "claim", "status") != "reversed" || nested(v, "entry", "reverses_entry_id") == "" {
		t.Fatalf("the void: %d %s", v.Status, v.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/tax/bad-debt-relief/claims/"+id+"/void", tax, map[string]any{"date": "2027-10-20", "reason": "Again"}, "If-Match", v.Header.Get("ETag")), "voidBadDebtReliefClaim", 409, "RELIEF-12")
	if again := claim("2027-10-20"); again.Status != http.StatusCreated || nested(again, "claim", "gst", "amount") != "0.83" {
		t.Errorf("claimed again after the void: %d %s", again.Status, again.Raw)
	}
}

func init() {
	matrixTaxFilings["claimBadDebtRelief"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/tax/bad-debt-relief/claims", tok, map[string]any{"write_off_id": "01928f6e-7a3b-7c41-9d2e-5f8a1b3c0000", "date": "2026-10-14"}, "Prefer", "return=preview"), nil
	}
	matrixTaxFilings["getBadDebtReliefClaim"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/tax/bad-debt-relief/claims/01928f6e-7a3b-7c41-9d2e-5f8a1b3c0000", tok, nil), nil
	}
	matrixTaxFilings["voidBadDebtReliefClaim"] = func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/tax/bad-debt-relief/claims/01928f6e-7a3b-7c41-9d2e-5f8a1b3c0000/void", tok, map[string]any{"date": "2026-10-14", "reason": "Test"}, "If-Match", `"1-posted"`, "Prefer", "return=preview"), nil
	}
}
