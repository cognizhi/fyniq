//go:build local

package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// cardOf sets up a company card in Banking, in SGD, held by holder, and returns its money account.
func (e *env) cardOf(holder string) storage.MoneyAccount {
	e.t.Helper()
	e.bankBooks()
	e.claimSetup()
	e.advancesAccount()
	acc := e.chartMoneyAccount()
	r := e.call(http.MethodPost, "/v1/money-accounts", e.as(authz.Accountant), map[string]any{"account_id": acc.ID.String(), "kind": "card",
		"name": "Corporate Visa " + acc.Code, "cardholder_id": holder})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createMoneyAccount = %d: %s", r.Status, r.Raw)
	}
	var m storage.MoneyAccount
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		m, err = u.Banking().MoneyAccount(ctx, uuid.MustParse(r.str("id")))
		return err
	})
	return m
}

// FR-AP-18 (p1-expense-claims task 7.1, design D9): a card money account names its holder, an active member; the
// holder's My claims lists the card's lines, and they complete each: spent for the company with the expense policy's
// category, which categorises the line as Banking would (Dr the category's account · Cr the card), then its receipt;
// or personal spend, recorded as an advance to them from the card and matched to the line, which they owe, which no
// claim settles, and which they refund. Nobody else completes the card's lines or sees them here.
// req: FR-AP-18
// req: purchases/expense-claims Cardholder completes a card line
// req: purchases/expense-claims Personal spend on a company card
// asvs: V8.2.2
func TestCardLinesCompletedByTheirHolder(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	travel, _ := e.claimSetup()
	advances := e.advancesAccount()
	ahkow := c.as(c.ahkow, authz.Employee)
	mei := c.as(c.mei, authz.Employee)

	// Only a card has a holder, and only an active member holds one.
	acc := e.chartMoneyAccount()
	for _, x := range []struct{ kind, holder string }{{"bank", c.ahkow}, {"card", uuid.NewString()}} {
		e.problemIs(e.call(http.MethodPost, "/v1/money-accounts", e.as(authz.Accountant), map[string]any{"account_id": acc.ID.String(), "kind": x.kind,
			"name": "Card", "cardholder_id": x.holder}), "createMoneyAccount", 422, banking.CodeCardholder)
	}
	card := e.cardOf(c.ahkow)
	if g := e.call(http.MethodGet, "/v1/money-accounts/"+card.ID.String(), e.as(authz.Accountant), nil); g.str("cardholder", "name") != "Ah Kow" {
		t.Errorf("the card's holder: %s", g.Raw)
	}
	taxiLine, personal := e.lineOn(card, "-27.30", nil), e.lineOn(card, "-45.00", nil)

	// Ah Kow sees his card's two lines waiting; Mei sees none and completes none of them.
	if l := e.call(http.MethodGet, "/v1/card-lines?waiting=true", ahkow, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 2 {
		t.Fatalf("his card lines: %d %s", l.Status, l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/card-lines", mei, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("another member's card lines: %s", l.Raw)
	}
	complete := func(who string, line storage.BankLine, body map[string]any) response {
		return e.call(http.MethodPost, "/v1/card-lines/"+line.ID.String()+"/complete", who, body, "If-Match", etag(line.Version))
	}
	spend := map[string]any{"category_id": travel, "gst_code": "NR", "description": "Taxi to Acme"}
	if r := complete(mei, taxiLine, spend); r.Status != 404 || e.bankLineNow(taxiLine.ID).Status != storage.LineUnreconciled {
		t.Errorf("another member completing it: %d %s", r.Status, r.Raw)
	}
	e.problemIs(complete(e.as(authz.Accountant), taxiLine, spend), "completeCardLine", 403, CodeForbidden)
	e.problemIs(complete(ahkow, taxiLine, map[string]any{"description": "Taxi"}), "completeCardLine", 422, CodeInvalid)
	e.problemIs(complete(ahkow, taxiLine, map[string]any{"category_id": uuid.NewString()}), "completeCardLine", 422, claim.CodeCategory)

	// The receipt waits for the line to be completed.
	receipt := []byte("%PDF-1.7 ComfortDelGro, 14 Oct 2026, S$27.30")
	e.problemIs(e.upload("/v1/card-lines/"+taxiLine.ID.String()+"/evidence", ahkow, map[string]string{"evidence_type": "receipt"}, "taxi.pdf",
		receipt, "If-Match", etag(taxiLine.Version)), "addCardLineEvidence", 409, CodeCardNotDone)

	// Spent for the company: categorised to the category's account, Dr Rent 27.30 · Cr the card.
	done := complete(ahkow, taxiLine, spend)
	if done.Status != 200 || done.str("completed") != "spend" || done.str("status") != storage.LineCategorised || done.str("document_id") == "" {
		t.Fatalf("completing the taxi: %d %s", done.Status, done.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, uuid.MustParse(done.str("document_id")))
		if err != nil || doc.Type != storage.DocumentBankTransaction || doc.Status != storage.DocumentPosted {
			t.Errorf("the bank transaction: %+v %v", doc, err)
		}
		return err
	})
	if r := e.upload("/v1/card-lines/"+taxiLine.ID.String()+"/evidence", ahkow, map[string]string{"evidence_type": "receipt"}, "taxi.pdf",
		receipt, "If-Match", done.Header.Get("ETag")); r.Status != 201 || r.str("document_id") != done.str("document_id") || r.str("line_id") != taxiLine.ID.String() {
		t.Fatalf("attaching the receipt: %d %s", r.Status, r.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/card-lines/"+taxiLine.ID.String()+"/evidence", ahkow, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("its receipt: %s", l.Raw)
	}

	// Personal spend: an advance to him from the card, matched to the line; he owes it, no claim settles it, he refunds it.
	p := complete(ahkow, personal, map[string]any{"personal": true, "description": "Dinner with family"})
	if p.Status != 200 || p.str("completed") != "personal" || p.str("status") != storage.LineMatched {
		t.Fatalf("personal spend: %d %s", p.Status, p.Raw)
	}
	adv := e.call(http.MethodGet, "/v1/cash-advances/"+p.str("document_id"), ahkow, nil)
	if adv.str("open", "amount") != "45.00" || !impactHas(adv, advances, "45.00") || !impactHas(adv, card.AccountID.String(), "-45.00") {
		t.Fatalf("what he owes: %s", adv.Raw)
	}
	claimed := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "advance_id": p.str("document_id"),
		"lines": []any{taxi(travel)}})
	if g := e.call(http.MethodGet, "/v1/expense-claims/"+claimed.str("id"), ahkow, nil); !hasCheck(g, claim.CodeClaimAdvance) {
		t.Errorf("a claim settling personal spend: %s", g.Raw)
	}
	bank, _ := e.bankLine("45.00")
	if r := e.call(http.MethodPost, "/v1/cash-advances/"+p.str("document_id")+"/refunds", e.as(authz.Accountant), map[string]any{"date": "2026-10-20",
		"money_account_id": bank.AccountID.String(), "amount": sgd("45.00")}, "If-Match", adv.Header.Get("ETag")); r.Status != 201 {
		t.Fatalf("his refund: %d %s", r.Status, r.Raw)
	}
	if a := e.call(http.MethodGet, "/v1/cash-advances/"+p.str("document_id"), ahkow, nil); a.str("open", "amount") != "0.00" {
		t.Errorf("repaid: %s", a.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/card-lines?waiting=true", ahkow, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("nothing waiting: %s", l.Raw)
	}

	// A card holder's line is completed once: again, it is no longer waiting.
	e.problemIs(complete(ahkow, e.bankLineNow(taxiLine.ID), spend), "completeCardLine", 409, journal.CodeLineDone)

	// Every entry replays as it posted (ENG-06).
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) != 0 {
			t.Errorf("replay: %+v", r.Mismatches)
		}
		return err
	})
}

// matrixCards are the authorisation matrix's rows for a company card's lines, completed by its holder, the matrix
// member (steppedUp).
var matrixCards = map[string]func(e *env, tok string) (response, func() bool){
	"listCardLines": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/card-lines", steppedUp(e, tok), nil), nil
	},
	"completeCardLine": func(e *env, tok string) (response, func() bool) {
		travel, _ := e.claimSetup()
		l := e.lineOn(e.cardOf(matrixApprover(e)), "-27.30", nil)
		r := e.call(http.MethodPost, "/v1/card-lines/"+l.ID.String()+"/complete", steppedUp(e, tok),
			map[string]any{"category_id": travel, "gst_code": "NR"}, "If-Match", etag(l.Version))
		return r, e.lineIs(l.ID, storage.LineUnreconciled)
	},
	"addCardLineEvidence": func(e *env, tok string) (response, func() bool) {
		travel, _ := e.claimSetup()
		l := e.lineOn(e.cardOf(matrixApprover(e)), "-27.30", nil)
		done := e.call(http.MethodPost, "/v1/card-lines/"+l.ID.String()+"/complete", steppedUp(e, e.as(authz.Employee)),
			map[string]any{"category_id": travel, "gst_code": "NR"}, "If-Match", etag(l.Version))
		r := e.upload("/v1/card-lines/"+l.ID.String()+"/evidence", steppedUp(e, tok), map[string]string{"evidence_type": "receipt"}, "r.pdf",
			[]byte("%PDF-1.7 matrix "+uuid.NewString()), "If-Match", done.Header.Get("ETag"))
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/card-lines/"+l.ID.String()+"/evidence", steppedUp(e, e.as(authz.Employee)), nil)
			return len(l.Body["items"].([]any)) == 0
		}
	},
	"getClaimGSTCodes": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/expense-claims/gst-codes?on=2026-10-15", tok, nil), nil
	},
	"listCardLineEvidence": func(e *env, tok string) (response, func() bool) {
		l := e.lineOn(e.cardOf(matrixApprover(e)), "-27.30", nil)
		return e.call(http.MethodGet, "/v1/card-lines/"+l.ID.String()+"/evidence", steppedUp(e, tok), nil), nil
	},
}
