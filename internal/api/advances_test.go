//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// advancesAccount gives e's organisation 1250 Staff & director advances, the account the setting claims.advances_account
// names by default, once.
func (e *env) advancesAccount() string {
	e.t.Helper()
	var id string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		if a, err := u.Accounts().ByCode(ctx, "1250"); err == nil {
			id = a.ID.String()
			return nil
		}
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1250", Name: "Staff & director advances", Type: storage.Asset})
		id = a.ID.String()
		return err
	})
	return id
}

// idrRate enters the day rate of the rupiah for on: S$0.0000850 a rupiah.
func (e *env) idrRate(on time.Time) {
	e.t.Helper()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.FXRates().Enter(ctx, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "IDR", On: on, Rate: "0.0000850", Quote: "0.0000850",
			QuoteForm: "base-per-unit"})
		return err
	})
}

// recordedAdvance advances amount to person from a new money account, and returns the advance's ID and ETag.
func (e *env) recordedAdvance(person, amount string) (id, tag string) {
	e.t.Helper()
	e.claimSetup()
	e.advancesAccount()
	acc := e.chartMoneyAccount()
	r := e.call(http.MethodPost, "/v1/cash-advances", e.as(authz.Accountant), map[string]any{"person_id": person, "date": "2026-10-13",
		"money_account_id": acc.ID.String(), "amount": sgd(amount)})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("recordCashAdvance = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// rupiah is a receipt line in IDR on a day of the Jakarta trip, at the rate cascade's rate for that day.
func rupiah(category, date, merchant, amount string) map[string]any {
	return map[string]any{"date": date, "merchant": merchant, "category_id": category, "description": "Jakarta trip",
		"amount": map[string]any{"amount": amount, "currency": "IDR"}, "gst_code": "NR"}
}

// impactHas says whether a ledger impact credits or debits an account with a base amount.
func impactHas(r response, account, base string) bool {
	lines, _ := r.Body["ledger_impact"].([]any)
	for _, l := range lines {
		m := l.(map[string]any)
		if m["account_id"] == account && m["base_amount"].(map[string]any)["amount"] == base {
			return true
		}
	}
	return false
}

// hasCheck says whether a claim's or an advance's checks name code.
func hasCheck(r response, code string) bool {
	checks, _ := r.Body["checks"].([]any)
	for _, x := range checks {
		if x.(map[string]any)["code"] == code {
			return true
		}
	}
	return false
}

// UC-EXP-03 (p1-expense-claims task 6.1; FR-AP-17; the owner's decision of 26 Oct 2026, rev 0.148): finance advances
// S$800.00 for a trip to Jakarta, recorded under "Payment batches" as Dr 1250 · Cr the bank, and matched to the bank
// line. The member claims three receipts in rupiah at S$0.0000850 a rupiah, worked by hand under RND-FX:
// IDR 6,000,000.00 = S$510.00; IDR 1,500,000.00 = S$127.50; IDR 1,231,176.00 = S$104.64996, which rounds to S$104.65;
// S$742.15 in all. The claim marks the advance, so it posts Dr the expenses 742.15 · Cr 1250 742.15, and nothing to 2150;
// S$57.85 stays open, is refunded (Dr the bank 57.85 · Cr 1250) and matched, and the advance is settled. A claim above
// an advance settles what is open and the excess is owed on 2150 (S$54.60 against S$50.00: S$4.60 reimbursed). An advance
// open past the claim deadline shows ⚠. Voiding needs a second person in Founder mode; an advance claims settled is not
// voided; a voided refund reopens the advance. Every entry replays as it posted.
// req: FR-AP-17
// req: ledger/posting-rules Cash advance to an employee
// req: purchases/expense-claims Cash advance settled by foreign-currency claims
// asvs: V8.2.2
func TestCashAdvancesSettledByClaims(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	travel, _ := e.claimSetup()
	advances := e.advancesAccount()
	e.idrRate(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	e.idrRate(time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	ahkow := c.as(c.ahkow, authz.Employee)
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	mei := c.as(c.mei, authz.Accountant)

	// Finance advances S$800.00 from DBS, and the bank line is matched to it.
	m, out := e.bankLine("-800.00")
	advance := func(who, person, amount string) response {
		return e.call(http.MethodPost, "/v1/cash-advances", who, map[string]any{"person_id": person, "date": "2026-10-13",
			"money_account_id": m.AccountID.String(), "amount": sgd(amount), "reference": "FAST 77120", "notes": "Jakarta trip"})
	}
	e.problemIs(advance(ahkow, c.ahkow, "800.00"), "recordCashAdvance", 403, CodeForbidden)
	e.problemIs(advance(mei, uuid.NewString(), "800.00"), "recordCashAdvance", 422, claim.CodeAdvancePerson)
	e.problemIs(advance(mei, c.ahkow, "0.00"), "recordCashAdvance", 422, ledger.CodeAdvance)
	adv := advance(mei, c.ahkow, "800.00")
	if adv.Status != 201 || adv.str("number") != "ADV-0001" || adv.str("kind") != "advance" || adv.str("open", "amount") != "800.00" ||
		adv.str("person", "name") != "Ah Kow" || !impactHas(adv, advances, "800.00") || !impactHas(adv, m.AccountID.String(), "-800.00") {
		t.Fatalf("the advance: %d %s", adv.Status, adv.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/bank-lines/"+out.ID.String()+"/match-recorded", e.as(authz.Accountant),
		map[string]any{"document_id": adv.str("id")}, "If-Match", etag(out.Version)); r.Status != 200 {
		t.Fatalf("matching the advance's bank line: %d %s", r.Status, r.Raw)
	}

	// Ah Kow sees his open advance, and only his.
	if l := e.call(http.MethodGet, "/v1/cash-advances?open=true&person_id="+c.weiling, ahkow, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 1 {
		t.Fatalf("his open advances: %d %s", l.Status, l.Raw)
	}
	other, _ := e.recordedAdvance(c.mei, "20.00")
	if g := e.call(http.MethodGet, "/v1/cash-advances/"+other, ahkow, nil); g.Status != 404 {
		t.Errorf("another's advance: %d %s", g.Status, g.Raw)
	}

	// His claim in rupiah settles it: S$742.15, all of it against the advance.
	lines := []any{rupiah(travel, "2026-10-13", "Hotel Indonesia Kempinski", "6000000.00"), rupiah(travel, "2026-10-14", "Sate Khas Senayan", "1500000.00"),
		rupiah(travel, "2026-10-14", "Blue Bird", "1231176.00")}
	draft := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "title": "Jakarta", "advance_id": adv.str("id"), "lines": lines})
	if draft.Status != 201 {
		t.Fatalf("drafting: %d %s", draft.Status, draft.Raw)
	}
	g := e.call(http.MethodGet, "/v1/expense-claims/"+draft.str("id"), ahkow, nil)
	if g.str("total", "amount") != "742.15" || g.str("settles", "amount") != "742.15" || g.str("advance_id") != adv.str("id") ||
		!impactHas(g, advances, "-742.15") || !impactHas(g, e.books.Rent.ID.String(), "104.65") || len(g.Body["ledger_impact"].([]any)) != 4 {
		t.Fatalf("the draft against the advance: %s", g.Raw)
	}
	s := e.call(http.MethodPost, "/v1/expense-claims/"+draft.str("id")+"/submit", ahkow, nil, "If-Match", draft.Header.Get("ETag"))
	if d := e.call(http.MethodPost, "/v1/approvals/"+s.str("id")+"/approve", weiling, hashOf(s), "If-Match", `"1"`); d.Status != 200 {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	posted := e.call(http.MethodGet, "/v1/expense-claims/"+draft.str("id"), ahkow, nil)
	if posted.str("status") != "posted" || posted.str("stage") != "paid" || posted.str("total", "amount") != "742.15" ||
		posted.str("settles", "amount") != "742.15" || !impactHas(posted, advances, "-742.15") {
		t.Errorf("the posted claim: %s", posted.Raw)
	}
	if o := e.call(http.MethodGet, "/v1/expense-claims/owed", ahkow, nil); o.str("total", "amount") != "0.00" {
		t.Errorf("nothing owed on 2150: %s", o.Raw)
	}
	a := e.call(http.MethodGet, "/v1/cash-advances/"+adv.str("id"), ahkow, nil)
	settled, _ := a.Body["settled_by"].([]any)
	if a.str("settled", "amount") != "742.15" || a.str("open", "amount") != "57.85" || len(settled) != 1 ||
		settled[0].(map[string]any)["type"] != "expense_claim" {
		t.Fatalf("the advance after the claim: %s", a.Raw)
	}

	// A claim cannot settle an advance with nothing open, another's, or what is not an advance.
	settle := func(id string) response {
		r := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "advance_id": id, "lines": []any{taxi(travel)}})
		return e.call(http.MethodGet, "/v1/expense-claims/"+r.str("id"), ahkow, nil)
	}
	for _, id := range []string{other, uuid.NewString()} {
		if r := settle(id); !hasCheck(r, claim.CodeClaimAdvance) {
			t.Errorf("settling %s: %s", id, r.Raw)
		}
	}

	// He refunds the S$57.85 left; not more; the bank line is matched to it.
	back, in := e.bankLine("57.85")
	refund := func(amount string) response {
		return e.call(http.MethodPost, "/v1/cash-advances/"+adv.str("id")+"/refunds", mei, map[string]any{"date": "2026-10-20",
			"money_account_id": back.AccountID.String(), "amount": sgd(amount), "reference": "FAST 80211"}, "If-Match", adv.Header.Get("ETag"))
	}
	e.problemIs(refund("57.86"), "recordCashAdvanceRefund", 422, claim.CodeAdvanceRefund)
	e.problemIs(e.call(http.MethodPost, "/v1/cash-advances/"+adv.str("id")+"/refunds", mei, map[string]any{"date": "2026-10-20",
		"money_account_id": back.AccountID.String(), "amount": sgd("57.85")}, "If-Match", `"9-posted"`), "recordCashAdvanceRefund", 412, CodeStale)
	rf := refund("57.85")
	if rf.Status != 201 || rf.str("kind") != "refund" || rf.str("refunds", "number") != "ADV-0001" || rf.str("person", "name") != "Ah Kow" ||
		!impactHas(rf, back.AccountID.String(), "57.85") || !impactHas(rf, advances, "-57.85") {
		t.Fatalf("the refund: %d %s", rf.Status, rf.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/bank-lines/"+in.ID.String()+"/match-recorded", e.as(authz.Accountant),
		map[string]any{"document_id": rf.str("id")}, "If-Match", etag(in.Version)); r.Status != 200 {
		t.Fatalf("matching the refund's bank line: %d %s", r.Status, r.Raw)
	}
	if a := e.call(http.MethodGet, "/v1/cash-advances/"+adv.str("id"), mei, nil); a.str("open", "amount") != "0.00" || a.str("refunded", "amount") != "57.85" {
		t.Errorf("the advance settled: %s", a.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/cash-advances?open=true", ahkow, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("no advance open: %s", l.Raw)
	}

	// Claims above an advance: S$54.60 against S$50.00 settles S$50.00, and S$4.60 is owed and reimbursed.
	m2, _ := e.bankLine("-50.00")
	small := e.call(http.MethodPost, "/v1/cash-advances", mei, map[string]any{"person_id": c.ahkow, "date": "2026-10-14",
		"money_account_id": m2.AccountID.String(), "amount": sgd("50.00")})
	over := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "advance_id": small.str("id"),
		"lines": []any{taxi(travel), taxi(travel)}})
	s = e.call(http.MethodPost, "/v1/expense-claims/"+over.str("id")+"/submit", ahkow, nil, "If-Match", over.Header.Get("ETag"))
	if d := e.call(http.MethodPost, "/v1/approvals/"+s.str("id")+"/approve", weiling, hashOf(s), "If-Match", `"1"`); d.Status != 200 {
		t.Fatalf("approving the claim above the advance: %d %s", d.Status, d.Raw)
	}
	if p := e.call(http.MethodGet, "/v1/expense-claims/"+over.str("id"), ahkow, nil); p.str("total", "amount") != "54.60" || p.str("settles", "amount") != "50.00" ||
		p.str("stage") != "approved" || !impactHas(p, advances, "-50.00") {
		t.Errorf("the claim above the advance: %s", p.Raw)
	}
	if o := e.call(http.MethodGet, "/v1/expense-claims/owed", weiling, nil); o.str("total", "amount") != "4.60" || o.str("difference", "amount") != "0.00" {
		t.Errorf("the excess owed: %s", o.Raw)
	}

	// An advance open past the claim deadline (60 days) shows ⚠.
	e.clock.Set(time.Date(2026, 12, 20, 9, 0, 0, 0, time.UTC))
	if a := e.call(http.MethodGet, "/v1/cash-advances/"+other, mei, nil); !hasCheck(a, claim.WarnAdvanceOpen) {
		t.Errorf("an old open advance: %s", a.Raw)
	}
	e.clock.Set(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))

	// The refund bounced: its bank line's match is undone first (FY229), then in Founder mode the refund is voided only on
	// the Controller's approval, which reopens the advance; the advance its claim settled is not voided.
	line := e.bankLineNow(in.ID)
	if r := e.call(http.MethodPost, "/v1/bank-lines/"+in.ID.String()+"/unreconcile", e.as(authz.Accountant), map[string]any{"reason": "Refund bounced"},
		"If-Match", etag(line.Version)); r.Status != 200 {
		t.Fatalf("undoing the refund's match: %d %s", r.Status, r.Raw)
	}
	void := map[string]any{"date": "2026-10-21", "reason": "Refund bounced"}
	path := "/v1/cash-advances/" + rf.str("id")
	e.problemIs(e.call(http.MethodPost, path+"/void", mei, void, "If-Match", rf.Header.Get("ETag")), "voidCashAdvance", 409, CodeAdvanceVoidApproval)
	asked := e.call(http.MethodPost, path+"/void-approval", mei, void, "If-Match", rf.Header.Get("ETag"))
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectCashAdvance {
		t.Fatalf("asking to void the refund: %d %s", asked.Status, asked.Raw)
	}
	if d := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", weiling, hashOf(asked), "If-Match", `"1"`); d.Status != 200 || d.str("status") != "approved" {
		t.Fatalf("approving the void: %d %s", d.Status, d.Raw)
	}
	if a := e.call(http.MethodGet, "/v1/cash-advances/"+adv.str("id"), mei, nil); a.str("open", "amount") != "57.85" || a.str("refunded", "amount") != "0.00" {
		t.Errorf("the advance reopened: %s", a.Raw)
	}
	tag := e.call(http.MethodGet, "/v1/cash-advances/"+adv.str("id"), mei, nil).Header.Get("ETag")
	asked = e.call(http.MethodPost, "/v1/cash-advances/"+adv.str("id")+"/void-approval", mei, void, "If-Match", tag)
	e.problemIs(e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", weiling, hashOf(asked), "If-Match", `"1"`), "approveApproval", 409,
		claim.CodeAdvanceSettled)

	// Every entry replays as it posted (ENG-06).
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) != 0 {
			t.Errorf("replay: %+v", r.Mismatches)
		}
		return err
	})
}

// matrixAdvances are the authorisation matrix's rows for cash advances and their refunds.
var matrixAdvances = map[string]func(e *env, tok string) (response, func() bool){
	"listCashAdvances": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/cash-advances", tok, nil), nil
	},
	"getCashAdvance": func(e *env, tok string) (response, func() bool) {
		id, _ := e.recordedAdvance(matrixApprover(e), "20.00")
		return e.call(http.MethodGet, "/v1/cash-advances/"+id, steppedUp(e, tok), nil), nil // as the advance's person
	},
	"recordCashAdvance": func(e *env, tok string) (response, func() bool) {
		e.claimSetup()
		e.advancesAccount()
		acc := e.chartMoneyAccount()
		person := matrixApprover(e)
		was := len(e.call(http.MethodGet, "/v1/cash-advances?person_id="+person, e.as(authz.Owner), nil).Body["items"].([]any))
		r := e.call(http.MethodPost, "/v1/cash-advances", tok, map[string]any{"person_id": person, "date": "2026-10-13",
			"money_account_id": acc.ID.String(), "amount": sgd("20.00")})
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/cash-advances?person_id="+person, e.as(authz.Owner), nil).Body["items"].([]any)) == was
		}
	},
	"recordCashAdvanceRefund": func(e *env, tok string) (response, func() bool) {
		id, tag := e.recordedAdvance(matrixApprover(e), "20.00")
		acc := e.chartMoneyAccount()
		r := e.call(http.MethodPost, "/v1/cash-advances/"+id+"/refunds", tok, map[string]any{"date": "2026-10-14", "money_account_id": acc.ID.String(),
			"amount": sgd("20.00")}, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/cash-advances/"+id, e.as(authz.Owner), nil).str("open", "amount") == "20.00"
		}
	},
	// A void in Founder mode is asked for, so the row voids in an organisation in Standard mode.
	"voidCashAdvance": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.recordedAdvance(s.joinedIn(s.books.Org, "Standard member", "employee").String(), "20.00")
		r := s.call(http.MethodPost, "/v1/cash-advances/"+id+"/void", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/cash-advances/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	"requestCashAdvanceVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, approval.ObjectCashAdvance)
		id, tag := e.recordedAdvance(matrixApprover(e), "20.00")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/cash-advances/"+id+"/void-approval", person, map[string]any{"date": "2026-10-28", "reason": "Matrix test"},
			"If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, approval.ObjectCashAdvance) == was }
	},
}
