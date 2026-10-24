//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/statement"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// statementMonth gives the customer a month of documents in SGD, GST-registered, and moves the clock to 5 November:
// INV A (109.00) on 1 October, before the period; INV C (109.00) on 12 October, voided on 20 October; INV B (218.00) on
// 14 October, due on 20 October; a receipt of A on 15 October; a credit note (10.90) on 21 October, refunded on 22
// October; and a write-off of 18.00 of B on 31 October. From 10 to 31 October the statement brings 109.00 forward and
// comes to 200.00, all of it B, 11 days overdue. It returns B's ID.
func (e *env) statementMonth() string {
	e.t.Helper()
	acc := e.as(authz.Accountant)
	e.readyToEmail()
	a, _ := e.invoiceDue("2026-10-01", "2026-10-31")
	c, _ := e.invoiceDue("2026-10-12", "2026-10-31")
	d := e.invoiceDraft("1", "200.00")
	d["date"], d["due_date"] = "2026-10-14", "2026-10-20"
	bc := e.call(http.MethodPost, "/v1/invoices", acc, d)
	if r := e.call(http.MethodPost, "/v1/invoices/"+bc.str("id")+"/issue", acc, nil, "If-Match", bc.Header.Get("ETag")); r.Status != http.StatusOK {
		e.t.Fatalf("issueInvoice: %d %s", r.Status, r.Raw)
	}
	b := bc.str("id")
	receipt := e.receiptOf(a, "109.00")
	receipt["date"] = "2026-10-15"
	if r := e.call(http.MethodPost, "/v1/customer-receipts", acc, receipt); r.Status != http.StatusCreated {
		e.t.Fatalf("recordCustomerReceipt: %d %s", r.Status, r.Raw)
	}
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture-approver"}})
	if _, err := (journal.Invoices{Store: e.store}).Void(ctx, e.books.Org, uuid.MustParse(c), time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), "Sent twice"); err != nil {
		e.t.Fatal(err)
	}
	cn := e.issuedCreditNote("1", "10.00")
	if r := e.call(http.MethodPost, "/v1/customer-refunds", acc, e.refundingCredit(cn, "10.90")); r.Status != http.StatusCreated {
		e.t.Fatalf("recordCustomerRefund: %d %s", r.Status, r.Raw)
	}
	w := e.call(http.MethodPost, "/v1/write-offs", acc, e.writeOffDraft(b, "18.00"))
	if w.Status != http.StatusCreated {
		e.t.Fatalf("createWriteOff: %d %s", w.Status, w.Raw)
	}
	if _, err := (journal.WriteOffs{Store: e.store}).Post(ctx, e.books.Org, uuid.MustParse(w.str("id"))); err != nil {
		e.t.Fatal(err)
	}
	e.clock.Set(time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC))
	return b
}

// FR-AR-08 through the API (p1-sales task 7.2): a customer's statement from 10 to 31 October brings 109.00 forward,
// lists the voided invoice and its void, the second invoice, the receipt, the credit note and its refund, and the
// write-off with a running balance to 200.00, the balance due the receivables give, all of it 1–30 days overdue: it adds
// up. A period backwards or ending after today, and a contact that is not a customer, are refused. Made, it is ST-0001,
// kept as it said; emailed, its PDF is its evidence and the delivery log has it from the invoices sender's purpose; it is
// listed for its customer; a currency the customer has nothing in is refused.
// req: FR-AR-08
// req: FR-INT-01
func TestAStatementAddsUpAndIsSentAsMade(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	b := e.statementMonth()
	path := "/v1/contacts/" + e.customer() + "/statement"

	s := e.call(http.MethodGet, path+"?from=2026-10-10&to=2026-10-31", acc, nil)
	if s.Status != http.StatusOK || s.Body["adds_up"] != true || s.str("opening_balance", "amount") != "109.00" ||
		s.str("balance_due", "amount") != "200.00" || s.str("overdue", "amount") != "200.00" || s.str("ageing", "overdue_1_30", "amount") != "200.00" ||
		s.str("ageing", "not_yet_due", "amount") != "0.00" || s.str("currency") != "SGD" {
		t.Fatalf("getCustomerStatement: %d %s", s.Status, s.Raw)
	}
	var kinds, amounts []string
	for _, x := range s.Body["lines"].([]any) {
		l := x.(map[string]any)
		kinds = append(kinds, l["kind"].(string))
		amounts = append(amounts, l["amount"].(map[string]any)["amount"].(string))
	}
	wantKinds := []string{"invoice", "invoice", "receipt", "void", "credit_note", "refund", "write_off"}
	wantAmounts := []string{"109.00", "218.00", "-109.00", "-109.00", "-10.90", "10.90", "-18.00"}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("the lines: %v %v", kinds, amounts)
	}
	for i := range kinds {
		if kinds[i] != wantKinds[i] || amounts[i] != wantAmounts[i] {
			t.Errorf("line %d: %s %s, want %s %s", i, kinds[i], amounts[i], wantKinds[i], wantAmounts[i])
		}
	}
	last := s.Body["lines"].([]any)[len(kinds)-1].(map[string]any)
	if last["balance"].(map[string]any)["amount"] != "200.00" || last["document_id"] == b {
		t.Errorf("the last line: %v", last)
	}

	e.problemIs(e.call(http.MethodGet, path+"?from=2026-10-31&to=2026-10-10", acc, nil), "getCustomerStatement", 422, statement.CodeRange)
	e.problemIs(e.call(http.MethodGet, path+"?from=2026-10-10&to=2026-11-30", acc, nil), "getCustomerStatement", 422, statement.CodeRange)
	vendor := e.call(http.MethodPost, "/v1/contacts", acc, map[string]any{"legal_name": "Supplies Pte Ltd", "vendor": true})
	e.problemIs(e.call(http.MethodGet, "/v1/contacts/"+vendor.str("id")+"/statement?from=2026-10-10&to=2026-10-31", acc, nil), "getCustomerStatement",
		422, statement.CodeCustomer)

	// Made and emailed.
	body := map[string]any{"customer_id": e.customer(), "from": "2026-10-10", "to": "2026-10-31"}
	e.problemIs(e.call(http.MethodPost, "/v1/statements", acc, map[string]any{"customer_id": e.customer(), "currency": "USD", "from": "2026-10-10",
		"to": "2026-10-31"}), "createStatement", 422, statement.CodeCurrency)
	if pv := e.call(http.MethodPost, "/v1/statements", acc, body, "Prefer", "return=preview"); pv.Status != http.StatusOK ||
		pv.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview: %d %s", pv.Status, pv.Raw)
	}
	made := e.call(http.MethodPost, "/v1/statements", acc, body, "Idempotency-Key", uuid.NewString())
	if made.Status != http.StatusCreated || made.str("number") != "ST-0001" || made.str("date") != "2026-11-05" || made.str("balance_due", "amount") != "200.00" ||
		len(made.Body["lines"].([]any)) != 7 || made.Header.Get("ETag") == "" {
		t.Fatalf("createStatement: %d %s", made.Status, made.Raw)
	}
	id := made.str("id")
	sent := e.call(http.MethodPost, "/v1/statements/"+id+"/email", acc, nil, "If-Match", made.Header.Get("ETag"))
	sends, _ := sent.Body["sends"].([]any)
	if sent.Status != http.StatusOK || len(sends) != 1 || sends[0].(map[string]any)["to"] != "ap@acme.example" {
		t.Fatalf("emailStatement: %d %s", sent.Status, sent.Raw)
	}
	log := e.call(http.MethodGet, "/v1/organisation/email/messages?purpose=statement", e.as(authz.Administrator), nil)
	if items, _ := log.Body["items"].([]any); log.Status != 200 || len(items) != 1 {
		t.Errorf("the delivery log: %d %s", log.Status, log.Raw)
	}
	list := e.call(http.MethodGet, "/v1/statements?customer_id="+e.customer(), acc, nil)
	if items, _ := list.Body["items"].([]any); list.Status != 200 || len(items) != 1 || items[0].(map[string]any)["number"] != "ST-0001" {
		t.Errorf("listStatements: %d %s", list.Status, list.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/statements/"+id, acc, nil); g.Status != 200 || g.str("number") != "ST-0001" || len(g.Body["sends"].([]any)) != 1 {
		t.Errorf("getStatement: %d %s", g.Status, g.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/statements/"+b, acc, nil), "getStatement", 404, statement.CodeNotStatement)
}

// matrixStatement is a statement made of the customer's October, for the matrix's reads and emails.
func matrixStatement(e *env) (id, etag string) {
	e.t.Helper()
	e.readyToEmail()
	e.invoiceDue("2026-10-01", "2026-10-31")
	r := e.call(http.MethodPost, "/v1/statements", e.as(authz.Accountant), map[string]any{"customer_id": e.customer(), "from": "2026-10-01", "to": "2026-10-15"})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createStatement = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}
