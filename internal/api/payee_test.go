//go:build local

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// vendor adds a vendor of its own, after making sure the bills' supplier exists, so a change of its bank details
// freezes nobody else's payments.
func (e *env) vendor(name string) string {
	e.t.Helper()
	e.supplier()
	r := e.call(http.MethodPost, "/v1/contacts", e.as(authz.Accountant), map[string]any{"legal_name": name, "vendor": true, "currency": "SGD"})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createContact = %d: %s", r.Status, r.Raw)
	}
	return r.str("id")
}

// sgAccount is a change of a vendor's SGD account at DBS to number.
func sgAccount(number string) map[string]any {
	return map[string]any{"currency": "SGD", "kind": "singapore", "bic": "DBSSSGSG", "bank_country": "SG",
		"account_name": "Northwind Analytics Pte. Ltd.", "account_number": number,
		"callback_note": "Called Jane Tan on 6123 4567 from our records, 27 Oct 2026 10:15"}
}

// withContact is a new change's body: the vendor's ID and the account.
func withContact(contact string, account map[string]any) map[string]any {
	b := map[string]any{"contact_id": contact}
	for k, v := range account {
		b[k] = v
	}
	return b
}

// bankChange prepares a change of contact's bank details as tok and returns its ID and ETag.
func (e *env) bankChange(contact, tok string, body map[string]any) (id, tag string) {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/bank-changes", tok, withContact(contact, body))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createPayeeBankChange = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// bankLetter attaches the vendor's letter to a change as tok.
func (e *env) bankLetter(id, tok string) string {
	e.t.Helper()
	r := e.upload("/v1/bank-changes/"+id+"/evidence", tok, map[string]string{"evidence_type": "bank_letter"}, "letter.pdf",
		[]byte("%PDF-1.7 Northwind's new bank account"), "If-Match", "*")
	if r.Status != http.StatusCreated {
		e.t.Fatalf("addPayeeBankChangeEvidence = %d: %s", r.Status, r.Raw)
	}
	return r.str("id")
}

// A change of a vendor's bank details (FR-CT-03; PRD-001 rev 0.152) in Founder mode. Ah Kow, as the Accountant, prepares
// the change; asking is refused until the vendor's letter is attached; asked, every founder is told at once, the vendor's
// payments are frozen (planning a date refused, recording one warned) and the account is not in force. Ah Kow cannot
// approve his own; Wei Ling approves it, which puts the account in force and lifts the freeze. A second change retires
// the first account. The number never leaves the API but through a reveal: stepped up, audited, and masked for the
// Auditor's masked view.
// req: FR-CT-03
// req: SOD-1
// req: FM-1
// req: SEC-11
// req: AUD-02
// req: master-data/contacts The freeze before payment batches (rev 0.152)
// req: master-data/contacts Account numbers sealed and masked (rev 0.152)
// asvs: V2.3.5, V8.2.3, V14.2.4
func TestAChangeOfBankDetailsNeedsASecondPerson(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant)
	weiling := f.as(f.weiling)
	drainWork(t, e)
	supplier := e.supplier()
	bill, tag := e.postedBill("1000.00", "90.00")

	id, ctag := e.bankChange(supplier, ahkow, sgAccount("012-345678-9"))
	got := e.call(http.MethodGet, "/v1/bank-changes/"+id, ahkow, nil)
	if got.str("status") != "draft" || got.str("account", "account_number_last4") != "6789" || strings.Contains(got.Raw, "0123456789") ||
		got.Body["in_force"] != false {
		t.Fatalf("the draft: %s", got.Raw)
	}
	if !strings.Contains(got.Raw, payee.CodeEvidence) {
		t.Errorf("the draft's checks do not ask for the letter: %s", got.Raw)
	}
	ask := func(id, tag string) response {
		return e.call(http.MethodPost, "/v1/bank-changes/"+id+"/approval", ahkow, nil, "If-Match", tag)
	}
	e.problemIs(ask(id, ctag), "requestPayeeBankChangeApproval", 422, payee.CodeEvidence)
	e.bankLetter(id, ahkow)
	if d := e.call(http.MethodGet, "/v1/contacts/"+supplier+"/bank-details", ahkow, nil); d.Body["payments_frozen"] != false {
		t.Fatalf("a draft froze payments: %s", d.Raw)
	}

	// Asked: frozen, and every founder told.
	asked := ask(id, ctag)
	if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectPayeeBankChange {
		t.Fatalf("asking = %d: %s", asked.Status, asked.Raw)
	}
	if d := e.call(http.MethodGet, "/v1/contacts/"+supplier+"/bank-details", ahkow, nil); d.Body["payments_frozen"] != true ||
		len(d.Body["in_force"].([]any)) != 0 || d.Body["changes"].([]any)[0].(map[string]any)["status"] != "waiting" {
		t.Fatalf("waiting: %s", d.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/bills/"+bill+"/payment-plan", ahkow, map[string]any{"planned_on": "2026-11-10"}, "If-Match", tag),
		"planBillPayment", 409, payee.CodeFrozen)
	paid := e.call(http.MethodPost, "/v1/bill-payments", ahkow, e.payment(bill, "10.00"))
	if paid.Status != http.StatusCreated || !strings.Contains(paid.Raw, `"code":"`+payee.CodeFrozen+`"`) {
		t.Fatalf("recording a payment while frozen = %d: %s", paid.Status, paid.Raw)
	}
	fanOutAll(t, e)
	for _, who := range []string{weiling, ahkow} {
		n := e.call(http.MethodGet, "/v1/notifications", who, nil)
		if !strings.Contains(n.Raw, `"type":"payee.bank_details"`) {
			t.Errorf("a founder was not told: %s", n.Raw)
		}
	}

	// Ah Kow cannot approve what he asked for; Wei Ling can.
	if r := f.decide(f.as(f.ahkow), asked.str("id"), "approve", hashOf(asked)); r.Status == http.StatusOK {
		t.Fatalf("the requester approved his own change: %s", r.Raw)
	}
	if r := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); r.Status != http.StatusOK || r.str("status") != "approved" {
		t.Fatalf("Wei Ling approving = %d: %s", r.Status, r.Raw)
	}
	d := e.call(http.MethodGet, "/v1/contacts/"+supplier+"/bank-details", ahkow, nil)
	inForce := d.Body["in_force"].([]any)
	if d.Body["payments_frozen"] != false || len(inForce) != 1 || inForce[0].(map[string]any)["id"] != id {
		t.Fatalf("approved: %s", d.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/bills/"+bill+"/payment-plan", ahkow, map[string]any{"planned_on": "2026-11-10"}, "If-Match", "*"); r.Status != http.StatusOK {
		t.Fatalf("planning after the approval = %d: %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/bank-changes/"+id, ahkow, sgAccount("0123456780"), "If-Match", "*"), "replacePayeeBankChange", 409, payee.CodeNotDraft)

	// A second change retires the first account.
	second, stag := e.bankChange(supplier, ahkow, sgAccount("998877665544"))
	g2 := e.call(http.MethodGet, "/v1/bank-changes/"+second, ahkow, nil)
	if g2.str("replaces", "account_number_last4") != "6789" {
		t.Errorf("the second change does not name the account it replaces: %s", g2.Raw)
	}
	e.bankLetter(second, ahkow)
	asked2 := ask(second, stag)
	if r := f.decide(weiling, asked2.str("id"), "approve", hashOf(asked2)); r.Status != http.StatusOK {
		t.Fatalf("approving the second = %d: %s", r.Status, r.Raw)
	}
	d = e.call(http.MethodGet, "/v1/contacts/"+supplier+"/bank-details", ahkow, nil)
	if in := d.Body["in_force"].([]any); len(in) != 1 || in[0].(map[string]any)["id"] != second {
		t.Fatalf("after the second change: %s", d.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/bank-changes/"+id, ahkow, nil); g.Body["in_force"] != false || g.str("status") != "approved" {
		t.Fatalf("the first change after the second: %s", g.Raw)
	}

	// Revealed: stepped up and audited; the Auditor's masked view stays masked; nothing else shows the number.
	reveal := func(tok string) response {
		return e.call(http.MethodPost, "/v1/bank-changes/"+second+"/reveal", tok, nil)
	}
	if r := reveal(weiling); r.Status != http.StatusOK || r.str("account_number") != "998877665544" {
		t.Fatalf("revealing = %d: %s", r.Status, r.Raw)
	}
	e.problemIs(reveal(e.as(authz.Controller)), "revealPayeeBankAccount", 403, CodeStepUpRequired)
	if r := reveal(f.as(f.weiling, authz.Auditor)); r.Status != http.StatusOK || r.str("account_number") != MaskedValue {
		t.Fatalf("the Auditor's reveal = %d: %s", r.Status, r.Raw)
	}
	trail := auditTrail(t, e)
	if strings.Contains(trail, "0123456789") || strings.Contains(trail, "998877665544") {
		t.Error("the audit trail holds a full account number")
	}
	if n := strings.Count(trail, `"action":"document.bank_account_revealed"`); n != 1 {
		t.Errorf("%d reveals audited, want 1 (the masked view opens nothing)", n)
	}
	for _, want := range []string{`"action":"document.bank_account_kept"`, `"action":"document.bank_account_in_force"`, `"action":"document.bank_account_retired"`} {
		if !strings.Contains(trail, want) {
			t.Errorf("no %s in the trail", want)
		}
	}
	if strings.Contains(e.log.String(), "998877665544") {
		t.Error("the log holds a full number")
	}
}

// A change is checked as it is prepared: the bank's code and country agree with the kind, a Singapore number is digits,
// an IBAN passes its check digits; a revision keeps the number unless the bank changes; the same account at another
// vendor is a warning (FR-CT-04); a contact that is not a vendor has no bank details; and with no field keys nothing is
// kept (SEC-11).
// req: FR-CT-03
// req: FR-CT-04
// req: SEC-11
// req: master-data/contacts One approved account per currency (rev 0.152)
// asvs: V2.2.1, V2.3.5, V14.2.4
func TestABankDetailsChangeIsChecked(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	vendor := e.vendor("Checked Vendor Pte Ltd")
	create := func(body map[string]any) response {
		return e.call(http.MethodPost, "/v1/bank-changes", accountant, withContact(vendor, body))
	}
	with := func(changes map[string]any) map[string]any {
		b := sgAccount("0123456789")
		for k, v := range changes {
			b[k] = v
		}
		return b
	}
	e.problemIs(create(with(map[string]any{"bic": "NWBKGB2L"})), "createPayeeBankChange", 422, payee.CodeCountry)
	e.problemIs(create(with(map[string]any{"account_number": "01234X6789"})), "createPayeeBankChange", 422, payee.CodeNumber)
	e.problemIs(create(with(map[string]any{"kind": "overseas", "bic": "NWBKGB2L", "bank_country": "GB", "account_number": "GB82WEST12345698765431"})),
		"createPayeeBankChange", 422, payee.CodeNumber)
	iban := create(with(map[string]any{"kind": "overseas", "currency": "GBP", "bic": "NWBKGB2L", "bank_country": "GB", "account_number": "GB82 WEST 1234 5698 7654 32"}))
	if iban.Status != http.StatusCreated || iban.str("account", "number_kind") != "iban" || iban.str("account", "account_number_last4") != "5432" {
		t.Fatalf("an IBAN = %d: %s", iban.Status, iban.Raw)
	}
	customer := e.call(http.MethodPost, "/v1/contacts", accountant, map[string]any{"legal_name": "Only A Customer", "customer": true}).str("id")
	e.problemIs(e.call(http.MethodPost, "/v1/bank-changes", accountant, withContact(customer, sgAccount("0123456789"))), "createPayeeBankChange", 422, payee.CodeVendor)

	// A revision keeps the number, unless the bank changes.
	id, tag := e.bankChange(vendor, accountant, sgAccount("0123456789"))
	keep := sgAccount("")
	keep["account_name"] = "Northwind Analytics Pte Ltd"
	r := e.call(http.MethodPut, "/v1/bank-changes/"+id, accountant, keep, "If-Match", tag)
	if r.Status != http.StatusOK || r.str("account", "account_number_last4") != "6789" || r.Body["version"] != float64(2) {
		t.Fatalf("a revision keeping the number = %d: %s", r.Status, r.Raw)
	}
	other := sgAccount("")
	other["bic"] = "OCBCSGSG"
	e.problemIs(e.call(http.MethodPut, "/v1/bank-changes/"+id, accountant, other, "If-Match", r.Header.Get("ETag")), "replacePayeeBankChange", 422, payee.CodeNumber)
	stepped := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: e.books.Org, Roles: []authz.Role{authz.Accountant},
		UserID: uuid.New(), StepUpAt: e.clock.Now()})
	if rv := e.call(http.MethodPost, "/v1/bank-changes/"+id+"/reveal", stepped, nil); rv.str("account_number") != "0123456789" {
		t.Fatalf("the number kept through the revision: %s", rv.Raw)
	}

	// The same account in force at another vendor is a warning on the second.
	f := newFounders(t)
	ahkow, weiling := f.as(f.ahkow, authz.Accountant), f.as(f.weiling)
	first, second := f.e.vendor("First Vendor Pte Ltd"), f.e.vendor("Second Vendor Pte Ltd")
	a, atag := f.e.bankChange(first, ahkow, sgAccount("5550001234"))
	f.e.bankLetter(a, ahkow)
	asked := f.e.call(http.MethodPost, "/v1/bank-changes/"+a+"/approval", ahkow, nil, "If-Match", atag)
	if d := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("approving the first vendor's account = %d: %s", d.Status, d.Raw)
	}
	b, _ := f.e.bankChange(second, ahkow, sgAccount("555-000-1234"))
	if g := f.e.call(http.MethodGet, "/v1/bank-changes/"+b, ahkow, nil); !strings.Contains(g.Raw, payee.CodeDuplicate) || !strings.Contains(g.Raw, "First Vendor Pte Ltd") {
		t.Errorf("the same account at another vendor is not flagged: %s", g.Raw)
	}

	// SOD-1: Wei Ling asks for the change Ah Kow prepared; Ah Kow, who prepared it, does not approve it.
	f.e.bankLetter(b, ahkow)
	asked2 := f.e.call(http.MethodPost, "/v1/bank-changes/"+b+"/approval", f.as(f.weiling, authz.Accountant), nil, "If-Match", `"1-draft"`)
	if asked2.Status != http.StatusAccepted {
		t.Fatalf("Wei Ling asking = %d: %s", asked2.Status, asked2.Raw)
	}
	f.e.problemIs(f.decide(f.as(f.ahkow), asked2.str("id"), "approve", hashOf(asked2)), "approveApproval", 403, payee.CodeOwnChange)

	// No field keys: nothing kept.
	e.api.deps.Fields = nil
	e.problemIs(create(sgAccount("0123456789")), "createPayeeBankChange", 503, payee.CodeNoKeys)
}

// The database refuses an account out of step with its change: one made in force, a sealed number changed, an account
// deleted, and a change approved without a second person's approval (FY237, FY238).
// req: FR-CT-03
// req: QA-ENG-05
// req: SOD-1
// asvs: V2.3.5, V8.2.2
func TestAttackBankDetailsChangeOnlyByApproval(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	vendor := e.vendor("Attacked Vendor Pte Ltd")
	id, _ := e.bankChange(vendor, accountant, sgAccount("0123456789"))
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
	attack("an account put in force without approval", func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.PayeeBankAccounts().OfChange(ctx, change, 1)
		if err != nil {
			return err
		}
		return u.PayeeBankAccounts().Move(ctx, a, storage.AccountInForce)
	})
	attack("the change approved without a request", func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Documents().SetStatus(ctx, change, storage.DocumentPosted)
	})
	attack("an account retired that was never in force", func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.PayeeBankAccounts().OfChange(ctx, change, 1)
		if err != nil {
			return err
		}
		return u.PayeeBankAccounts().Move(ctx, a, storage.AccountRetired)
	})
}

// matrixBankChange prepares a change of a vendor of its own with the vendor's letter, as an Accountant, and returns the
// change's and the letter's IDs.
func matrixBankChange(e *env) (id, letter string) {
	vendor := e.vendor("Matrix vendor " + uuid.NewString()[:8])
	id, _ = e.bankChange(vendor, e.as(authz.Accountant), sgAccount("0123456789"))
	return id, e.bankLetter(id, e.as(authz.Accountant))
}

// matrixPayee has one request per operation on vendors' bank details, each on a vendor of its own.
var matrixPayee = map[string]func(e *env, tok string) (response, func() bool){
	"getPayeeBankDetails": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/contacts/"+e.supplier()+"/bank-details", tok, nil), nil
	},
	"createPayeeBankChange": func(e *env, tok string) (response, func() bool) {
		vendor := e.vendor("Matrix vendor " + uuid.NewString()[:8])
		r := e.call(http.MethodPost, "/v1/bank-changes", tok, withContact(vendor, sgAccount("0123456789")))
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/contacts/"+vendor+"/bank-details", e.as(authz.Accountant), nil).Body["changes"].([]any)) == 0
		}
	},
	"getPayeeBankChange": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixBankChange(e)
		return e.call(http.MethodGet, "/v1/bank-changes/"+id, tok, nil), nil
	},
	"replacePayeeBankChange": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixBankChange(e)
		r := e.call(http.MethodPut, "/v1/bank-changes/"+id, tok, sgAccount("9876543210"), "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/bank-changes/"+id, e.as(authz.Accountant), nil).Body["version"] == float64(1)
		}
	},
	"requestPayeeBankChangeApproval": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, approval.ObjectPayeeBankChange)
		id, _ := matrixBankChange(e)
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/bank-changes/"+id+"/approval", person, nil, "If-Match", `"1-draft"`)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, approval.ObjectPayeeBankChange) == was }
	},
	"revealPayeeBankAccount": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixBankChange(e)
		return e.call(http.MethodPost, "/v1/bank-changes/"+id+"/reveal", steppedUp(e, tok), nil), nil
	},
	"addPayeeBankChangeEvidence": func(e *env, tok string) (response, func() bool) {
		vendor := e.vendor("Matrix vendor " + uuid.NewString()[:8])
		id, _ := e.bankChange(vendor, e.as(authz.Accountant), sgAccount("0123456789"))
		r := e.upload("/v1/bank-changes/"+id+"/evidence", tok, map[string]string{"evidence_type": "bank_letter"}, "letter.pdf",
			[]byte("%PDF-1.7 matrix letter"), "If-Match", "*")
		return r, func() bool {
			l := e.call(http.MethodGet, "/v1/bank-changes/"+id+"/evidence", e.as(authz.Accountant), nil)
			items, _ := l.Body["items"].([]any)
			return l.Status == http.StatusOK && len(items) == 0
		}
	},
	"listPayeeBankChangeEvidence": func(e *env, tok string) (response, func() bool) {
		id, _ := matrixBankChange(e)
		return e.call(http.MethodGet, "/v1/bank-changes/"+id+"/evidence", tok, nil), nil
	},
	"getPayeeBankChangeEvidence": func(e *env, tok string) (response, func() bool) {
		id, ev := matrixBankChange(e)
		return e.call(http.MethodGet, "/v1/bank-changes/"+id+"/evidence/"+ev, tok, nil), nil
	},
	"removePayeeBankChangeEvidence": func(e *env, tok string) (response, func() bool) {
		id, ev := matrixBankChange(e)
		r := e.call(http.MethodDelete, "/v1/bank-changes/"+id+"/evidence/"+ev, tok, nil, "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/bank-changes/"+id+"/evidence/"+ev, e.as(authz.Accountant), nil).str("state") == "attached"
		}
	},
}
