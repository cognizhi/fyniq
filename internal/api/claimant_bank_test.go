//go:build local

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// myAccount is a change of a person's own account at DBS to number.
func myAccount(number string) map[string]any {
	return map[string]any{"bic": "DBSSSGSG", "account_name": "Tan Ah Kow", "account_number": number}
}

// myBankChange prepares a change of the caller's own bank account as tok and returns its ID and ETag.
func (e *env) myBankChange(tok string, body map[string]any) (id, tag string) {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/me/bank-changes", tok, body)
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createMyBankChange = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// myScreenshot attaches a screenshot of the person's banking app to their own change as tok.
func (e *env) myScreenshot(id, tok string) string {
	e.t.Helper()
	r := e.upload("/v1/me/bank-changes/"+id+"/evidence", tok, map[string]string{"evidence_type": "bank_app_screenshot"}, "account.pdf",
		[]byte("%PDF-1.7 my bank app"), "If-Match", "*")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("addMyBankChangeEvidence = %d: %s", r.Status, r.Raw)
	}
	return r.str("id")
}

// A person's own bank account for reimbursements (FR-AP-22; PRD-001 rev 0.154) in Founder mode. Ah Kow, as a claimant,
// enters his own account; asking is refused until a screenshot of his banking app is attached; asked, payments to him
// are frozen (a reimbursement recorded by hand is flagged) and the account is not in force. Wei Ling cannot see Ah Kow's
// change through her own profile. Ah Kow cannot approve his own, in any role; Wei Ling, who holds A on "Payment
// batches", reads it, compares the number with its screenshot and approves it, which puts it in force and lifts the
// freeze. A second change retires the first. The number never leaves the API but through a reveal: stepped up, audited.
// req: FR-AP-22
// req: FM-1
// req: SEC-11
// req: AUD-02
// req: purchases/payables Entered by the person
// req: purchases/payables Approved by another person
// req: purchases/payables The full number on request
// asvs: V2.3.5, V8.2.2, V8.2.3, V14.2.4
func TestAPersonsBankAccountNeedsAnotherPerson(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Employee)
	weiling := f.as(f.weiling, authz.Employee)
	controller := f.as(f.weiling)

	id, tag := e.myBankChange(ahkow, myAccount("012-345678-9"))
	got := e.call(http.MethodGet, "/v1/me/bank-changes/"+id, ahkow, nil)
	if got.str("status") != "draft" || got.str("account", "account_number_last4") != "6789" || got.str("account", "currency") != "SGD" ||
		got.str("account", "kind") != "singapore" || got.str("member_user_id") != f.ahkow || strings.Contains(got.Raw, "0123456789") {
		t.Fatalf("the draft: %s", got.Raw)
	}
	if !strings.Contains(got.Raw, payee.CodeEvidence) {
		t.Errorf("the draft's checks do not ask for the screenshot: %s", got.Raw)
	}
	ask := func(id, tag string) response {
		return e.call(http.MethodPost, "/v1/me/bank-changes/"+id+"/approval", ahkow, nil, "If-Match", tag)
	}
	e.problemIs(ask(id, tag), "requestMyBankChangeApproval", 422, payee.CodeEvidence)
	e.myScreenshot(id, ahkow)

	// Another person's change is not found through one's own profile, for reading, changing, asking, revealing or its files.
	e.problemIs(e.call(http.MethodGet, "/v1/me/bank-changes/"+id, weiling, nil), "getMyBankChange", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodPut, "/v1/me/bank-changes/"+id, weiling, myAccount("999999999"), "If-Match", tag), "replaceMyBankChange", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodPost, "/v1/me/bank-changes/"+id+"/approval", weiling, nil, "If-Match", tag), "requestMyBankChangeApproval", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodPost, "/v1/me/bank-changes/"+id+"/reveal", weiling, nil), "revealMyBankAccount", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodGet, "/v1/me/bank-changes/"+id+"/evidence", weiling, nil), "listMyBankChangeEvidence", 404, CodeNotFound)
	if d := e.call(http.MethodGet, "/v1/me/bank-details", weiling, nil); len(d.Body["changes"].([]any)) != 0 {
		t.Fatalf("Wei Ling's own details show Ah Kow's change: %s", d.Raw)
	}

	// Asked: frozen, and a reimbursement recorded by hand is flagged.
	asked := ask(id, tag)
	if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectClaimantBankChange {
		t.Fatalf("asking = %d: %s", asked.Status, asked.Raw)
	}
	if d := e.call(http.MethodGet, "/v1/me/bank-details", ahkow, nil); d.Body["payments_frozen"] != true || d.Body["in_force"] != nil ||
		d.Body["changes"].([]any)[0].(map[string]any)["status"] != "waiting" {
		t.Fatalf("waiting: %s", d.Raw)
	}
	claim := e.postedClaim(uuid.MustParse(f.ahkow))
	acc := e.chartMoneyAccount()
	paid := e.call(http.MethodPost, "/v1/claim-reimbursements", e.as(authz.Accountant), map[string]any{"claimant_id": f.ahkow,
		"date": "2026-10-15", "money_account_id": acc.ID.String(), "claims": []any{map[string]any{"claim_id": claim, "amount": sgd("27.30")}}})
	if paid.Status != http.StatusCreated || !strings.Contains(paid.Raw, `"code":"`+payee.CodeFrozen+`"`) {
		t.Fatalf("recording a reimbursement while frozen = %d: %s", paid.Status, paid.Raw)
	}

	// Ah Kow approves his own in no role; Wei Ling reads it, compares the number with the screenshot and approves it.
	for _, role := range []authz.Role{authz.Owner, authz.Controller, authz.Approver, authz.Employee, authz.PaymentReleaser} {
		if r := f.decide(f.as(f.ahkow, role), asked.str("id"), "approve", hashOf(asked)); r.Status == http.StatusOK {
			t.Fatalf("Ah Kow as %s approved his own bank account: %s", role, r.Raw)
		}
	}
	seen := e.call(http.MethodGet, "/v1/claimant-bank-changes/"+id, controller, nil)
	if seen.Status != http.StatusOK || seen.str("status") != "waiting" || seen.str("account", "account_number_last4") != "6789" {
		t.Fatalf("the approver reading the change = %d: %s", seen.Status, seen.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/claimant-bank-changes/"+id+"/evidence", controller, nil); len(l.Body["items"].([]any)) != 1 {
		t.Fatalf("the approver listing its evidence: %s", l.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/claimant-bank-changes/"+id+"/reveal", controller, nil); r.Status != http.StatusOK || r.str("account_number") != "0123456789" {
		t.Fatalf("the approver revealing = %d: %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/claimant-bank-changes/"+id+"/reveal", e.as(authz.Controller), nil), "revealClaimantBankAccount", 403, CodeStepUpRequired)
	if r := f.decide(controller, asked.str("id"), "approve", hashOf(asked)); r.Status != http.StatusOK || r.str("status") != "approved" {
		t.Fatalf("Wei Ling approving = %d: %s", r.Status, r.Raw)
	}
	d := e.call(http.MethodGet, "/v1/me/bank-details", ahkow, nil)
	if d.Body["payments_frozen"] != false || d.str("in_force", "id") != id {
		t.Fatalf("approved: %s", d.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/me/bank-changes/"+id, ahkow, myAccount("0123456780"), "If-Match", "*"), "replaceMyBankChange", 409, payee.CodeNotDraft)

	// A second change retires the first.
	second, stag := e.myBankChange(ahkow, myAccount("998877665544"))
	if g := e.call(http.MethodGet, "/v1/me/bank-changes/"+second, ahkow, nil); g.str("replaces", "account_number_last4") != "6789" {
		t.Errorf("the second change does not name the account it replaces: %s", g.Raw)
	}
	e.myScreenshot(second, ahkow)
	// Asked, then withdrawn by Ah Kow, which lifts the freeze; Wei Ling cannot withdraw it for him; asked again, approved.
	if r := ask(second, stag); r.Status != http.StatusAccepted {
		t.Fatalf("asking for the second = %d: %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/me/bank-changes/"+second+"/withdraw", weiling, nil, "If-Match", stag), "withdrawMyBankChange", 404, CodeNotFound)
	if r := e.call(http.MethodPost, "/v1/me/bank-changes/"+second+"/withdraw", ahkow, nil, "If-Match", stag); r.Status != http.StatusOK || r.str("status") != "draft" {
		t.Fatalf("withdrawing = %d: %s", r.Status, r.Raw)
	}
	if d := e.call(http.MethodGet, "/v1/me/bank-details", ahkow, nil); d.Body["payments_frozen"] != false || d.str("in_force", "id") != id {
		t.Fatalf("after withdrawing: %s", d.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/me/bank-changes/"+second+"/withdraw", ahkow, nil, "If-Match", stag), "withdrawMyBankChange", 409, approval.CodeNotPending)
	asked2 := ask(second, stag)
	if r := f.decide(controller, asked2.str("id"), "approve", hashOf(asked2)); r.Status != http.StatusOK {
		t.Fatalf("approving the second = %d: %s", r.Status, r.Raw)
	}
	if d := e.call(http.MethodGet, "/v1/me/bank-details", ahkow, nil); d.str("in_force", "id") != second {
		t.Fatalf("after the second change: %s", d.Raw)
	}

	// Revealed by the person, stepped up and audited; nothing else shows the number.
	if r := e.call(http.MethodPost, "/v1/me/bank-changes/"+second+"/reveal", ahkow, nil); r.Status != http.StatusOK || r.str("account_number") != "998877665544" {
		t.Fatalf("revealing his own = %d: %s", r.Status, r.Raw)
	}
	trail := auditTrail(t, e)
	if strings.Contains(trail, "0123456789") || strings.Contains(trail, "998877665544") {
		t.Error("the audit trail holds a full account number")
	}
	if n := strings.Count(trail, `"action":"document.bank_account_revealed"`); n != 2 {
		t.Errorf("%d reveals audited, want 2", n)
	}
	for _, want := range []string{`"action":"document.bank_account_kept"`, `"action":"document.bank_account_in_force"`, `"action":"document.bank_account_retired"`} {
		if !strings.Contains(trail, want) {
			t.Errorf("no %s in the trail", want)
		}
	}
	if strings.Contains(e.log.String(), "998877665544") || strings.Contains(e.log.String(), "0123456789") {
		t.Error("the log holds a full number")
	}
}

// A person's account is checked as it is entered: a bank outside Singapore is refused, a Singapore number is digits, a
// revision keeps the number unless the bank changes, someone signed in as no person keeps none, the same account in
// force for a vendor is a warning (FR-CT-04), and with no field keys nothing is kept (SEC-11).
// req: FR-AP-22
// req: FR-CT-04
// req: SEC-11
// req: purchases/payables Entered by the person
// asvs: V2.2.1, V2.3.5
func TestAPersonsBankAccountIsChecked(t *testing.T) {
	e := newEnv(t)
	me := steppedUp(e, e.as(authz.Employee))
	create := func(body map[string]any) response { return e.call(http.MethodPost, "/v1/me/bank-changes", me, body) }
	overseas := myAccount("12345678")
	overseas["bic"] = "NWBKGB2L"
	e.problemIs(create(overseas), "createMyBankChange", 422, payee.CodeCountry)
	e.problemIs(create(myAccount("01234X6789")), "createMyBankChange", 422, payee.CodeNumber)
	e.problemIs(e.call(http.MethodPost, "/v1/me/bank-changes", e.as(authz.Employee), myAccount("0123456789")), "createMyBankChange", 403, payee.CodeOwnOnly)

	id, tag := e.myBankChange(me, myAccount("0123456789"))
	keep := myAccount("")
	keep["account_name"] = "Tan Ah Kow Ah Beng"
	r := e.call(http.MethodPut, "/v1/me/bank-changes/"+id, me, keep, "If-Match", tag)
	if r.Status != http.StatusOK || r.str("account", "account_number_last4") != "6789" || r.Body["version"] != float64(2) {
		t.Fatalf("a revision keeping the number = %d: %s", r.Status, r.Raw)
	}
	other := myAccount("")
	other["bic"] = "OCBCSGSG"
	e.problemIs(e.call(http.MethodPut, "/v1/me/bank-changes/"+id, me, other, "If-Match", r.Header.Get("ETag")), "replaceMyBankChange", 422, payee.CodeNumber)

	// The same account in force for a vendor is a warning on the person's change.
	f := newFounders(t)
	vendor := f.e.vendor("Diverted Vendor Pte Ltd")
	v, vtag := f.e.bankChange(vendor, f.as(f.ahkow, authz.Accountant), sgAccount("5550001234"))
	f.e.bankLetter(v, f.as(f.ahkow, authz.Accountant))
	asked := f.e.call(http.MethodPost, "/v1/bank-changes/"+v+"/approval", f.as(f.ahkow, authz.Accountant), nil, "If-Match", vtag)
	if d := f.decide(f.as(f.weiling), asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("approving the vendor's account = %d: %s", d.Status, d.Raw)
	}
	mine, _ := f.e.myBankChange(f.as(f.weiling, authz.Employee), myAccount("555-000-1234"))
	if g := f.e.call(http.MethodGet, "/v1/claimant-bank-changes/"+mine, f.as(f.ahkow), nil); !strings.Contains(g.Raw, payee.CodeDuplicate) ||
		!strings.Contains(g.Raw, "Diverted Vendor Pte Ltd") {
		t.Errorf("a person's account that a vendor has in force is not flagged: %s", g.Raw)
	}

	// No field keys: nothing kept.
	e.api.deps.Fields = nil
	e.problemIs(create(myAccount("0123456789")), "createMyBankChange", 503, payee.CodeNoKeys)
}

// The database refuses a person's account out of step with its change: one put in force without approval, a second one
// put in force beside it, one for someone who is not a member, one that is not a Singapore account, and a change approved
// without a request (FY237, FY238).
// req: FR-AP-22
// req: QA-ENG-05
// req: FM-1
// asvs: V2.3.5, V8.2.2
func TestAttackPersonsBankAccountOnlyByApproval(t *testing.T) {
	e := newEnv(t)
	me := steppedUp(e, e.as(authz.Employee))
	person := uuid.MustParse(matrixApprover(e))
	id, _ := e.myBankChange(me, myAccount("0123456789"))
	change := uuid.MustParse(id)
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "attacker"}})
	attack := func(name string, fn func(ctx context.Context, u storage.UnitOfWork) error) {
		t.Helper()
		err := e.store.Do(ctx, e.books.Org, fn)
		var se *storage.Error
		if !errors.As(err, &se) || se.Code != storage.CodePayee {
			t.Errorf("%s: want %s, got %v", name, storage.CodePayee, err)
		}
	}
	attack("a person's account put in force without approval", func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.PayeeBankAccounts().OfChange(ctx, change, 1)
		if err != nil {
			return err
		}
		return u.PayeeBankAccounts().Move(ctx, a, storage.AccountInForce)
	})
	attack("a person's change approved without a request", func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Documents().SetStatus(ctx, change, storage.DocumentPosted)
	})
	second, _ := e.myBankChange(me, myAccount("998877665544"))
	attack("a second account put in force for the person", func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.PayeeBankAccounts().OfChange(ctx, uuid.MustParse(second), 1)
		if err != nil {
			return err
		}
		return u.PayeeBankAccounts().Move(ctx, a, storage.AccountInForce)
	})
	attack("an account kept for someone who is not a member", func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.PayeeBankAccounts().OfChange(ctx, change, 1)
		if err != nil {
			return err
		}
		stranger := payee.Person(uuid.New())
		b, err := payee.EncodeFor(stranger, payee.Draft{Currency: "SGD", Kind: payee.Singapore, BIC: "DBSSSGSG", BankCountry: "SG", AccountName: "Stranger"},
			payee.Number{Last4: a.Last4, Fingerprint: a.Fingerprint, Kind: "account"})
		if err != nil {
			return err
		}
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentClaimantBankChange, Payload: b})
		if err != nil {
			return err
		}
		a.ChangeID, a.ChangeVersion, a.MemberUserID = doc.ID, doc.Version, stranger.PersonID
		_, err = u.PayeeBankAccounts().Add(ctx, a)
		return err
	})
	attack("a person's account kept for a vendor's change", func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.PayeeBankAccounts().OfChange(ctx, change, 1)
		if err != nil {
			return err
		}
		a.ContactID, a.MemberUserID = person, uuid.Nil
		_, err = u.PayeeBankAccounts().Add(ctx, a)
		return err
	})
}

// matrixMyBankChange prepares a change of the matrix person's own account with its screenshot, as an Employee stepped
// up, and returns the change's and the screenshot's IDs.
func matrixMyBankChange(e *env) (id, shot string) {
	me := steppedUp(e, e.as(authz.Employee))
	id, _ = e.myBankChange(me, myAccount("0123456789"))
	return id, e.myScreenshot(id, me)
}

// matrixClaimantBank has one request per operation on people's own bank accounts, each on a change of its own. The
// person's own are made as the matrix person (steppedUp), so a role allowed them reaches its own change.
var matrixClaimantBank = map[string]func(e *env, tok string) (response, func() bool){
	"getMyBankDetails": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/me/bank-details", steppedUp(e, tok), nil), nil
	},
	"createMyBankChange": func(e *env, tok string) (response, func() bool) {
		count := func() int {
			return len(e.call(http.MethodGet, "/v1/me/bank-details", steppedUp(e, e.as(authz.Employee)), nil).Body["changes"].([]any))
		}
		was := count()
		r := e.call(http.MethodPost, "/v1/me/bank-changes", steppedUp(e, tok), myAccount("0123456789"))
		return r, func() bool { return count() == was }
	},
	"getMyBankChange": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixMyBankChange(e)
		return e.call(http.MethodGet, "/v1/me/bank-changes/"+id, steppedUp(e, tok), nil), nil
	},
	"replaceMyBankChange": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixMyBankChange(e)
		r := e.call(http.MethodPut, "/v1/me/bank-changes/"+id, steppedUp(e, tok), myAccount("9876543210"), "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/claimant-bank-changes/"+id, e.as(authz.Accountant), nil).Body["version"] == float64(1)
		}
	},
	"requestMyBankChangeApproval": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, approval.ObjectClaimantBankChange)
		id, _ := matrixMyBankChange(e)
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/me/bank-changes/"+id+"/approval", person, nil, "If-Match", `"1-draft"`)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/me/bank-changes/"+id+"/withdraw", person, nil, "If-Match", `"1-draft"`)
		}
		return r, func() bool { return requestsOf(e, approval.ObjectClaimantBankChange) == was }
	},
	"withdrawMyBankChange": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixMyBankChange(e)
		me := steppedUp(e, e.as(authz.Employee))
		if r := e.call(http.MethodPost, "/v1/me/bank-changes/"+id+"/approval", me, nil, "If-Match", `"1-draft"`); r.Status != http.StatusAccepted {
			e.t.Fatalf("asking for the matrix change's approval = %d: %s", r.Status, r.Raw)
		}
		r := e.call(http.MethodPost, "/v1/me/bank-changes/"+id+"/withdraw", steppedUp(e, tok), nil, "If-Match", `"1-draft"`)
		frozen := func() bool { return e.call(http.MethodGet, "/v1/me/bank-details", me, nil).Body["payments_frozen"] == true }
		still := frozen()
		if still { // a refused withdrawal leaves it waiting; the next row must find nothing waiting
			e.call(http.MethodPost, "/v1/me/bank-changes/"+id+"/withdraw", me, nil, "If-Match", `"1-draft"`)
		}
		return r, func() bool { return still }
	},
	"revealMyBankAccount": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixMyBankChange(e)
		return e.call(http.MethodPost, "/v1/me/bank-changes/"+id+"/reveal", steppedUp(e, tok), nil), nil
	},
	"getClaimantBankChange": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixMyBankChange(e)
		return e.call(http.MethodGet, "/v1/claimant-bank-changes/"+id, tok, nil), nil
	},
	"revealClaimantBankAccount": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixMyBankChange(e)
		return e.call(http.MethodPost, "/v1/claimant-bank-changes/"+id+"/reveal", steppedUp(e, tok), nil), nil
	},
	"addMyBankChangeEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := e.myBankChange(steppedUp(e, e.as(authz.Employee)), myAccount("0123456789"))
		r := e.upload("/v1/me/bank-changes/"+id+"/evidence", steppedUp(e, tok), map[string]string{"evidence_type": "bank_app_screenshot"}, "account.pdf",
			[]byte("%PDF-1.7 matrix screenshot"), "If-Match", "*")
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/claimant-bank-changes/"+id+"/evidence", e.as(authz.Accountant), nil)
			items, _ := l.Body["items"].([]any)
			return l.Status == http.StatusOK && len(items) == 0
		}
	},
	"listMyBankChangeEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixMyBankChange(e)
		return e.call(http.MethodGet, "/v1/me/bank-changes/"+id+"/evidence", steppedUp(e, tok), nil), nil
	},
	"getMyBankChangeEvidence": func(e *env, tok string) (response, func() bool) {
		id, shot := matrixMyBankChange(e)
		return e.call(http.MethodGet, "/v1/me/bank-changes/"+id+"/evidence/"+shot, steppedUp(e, tok), nil), nil
	},
	"removeMyBankChangeEvidence": func(e *env, tok string) (response, func() bool) {
		id, shot := matrixMyBankChange(e)
		r := e.call(http.MethodDelete, "/v1/me/bank-changes/"+id+"/evidence/"+shot, steppedUp(e, tok), nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/claimant-bank-changes/"+id+"/evidence/"+shot, e.as(authz.Accountant), nil).str("state") == "attached"
		}
	},
	"listClaimantBankChangeEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixMyBankChange(e)
		return e.call(http.MethodGet, "/v1/claimant-bank-changes/"+id+"/evidence", tok, nil), nil
	},
	"getClaimantBankChangeEvidence": func(e *env, tok string) (response, func() bool) {
		id, shot := matrixMyBankChange(e)
		return e.call(http.MethodGet, "/v1/claimant-bank-changes/"+id+"/evidence/"+shot, tok, nil), nil
	},
}
