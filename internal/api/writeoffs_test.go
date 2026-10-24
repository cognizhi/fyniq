//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/writeoff"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// badDebts adds, once, a bad-debts expense account to e's books and returns its ID.
func (e *env) badDebts() string {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	var id string
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if a, err := u.Accounts().ByCode(ctx, "6470"); err == nil {
			id = a.ID.String()
			return nil
		}
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "6470", Name: "Bad debts", Type: storage.Expense})
		id = a.ID.String()
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// writeOffDraft is a bad-debt write-off of amount of an invoice.
func (e *env) writeOffDraft(invoiceID, amount string) map[string]any {
	return map[string]any{"invoice_id": invoiceID, "date": "2026-10-31", "amount": map[string]any{"amount": amount, "currency": "SGD"},
		"reason": "bad_debt", "account_id": e.badDebts(), "note": "The customer went into liquidation"}
}

// draftWriteOff issues an invoice and saves a draft write-off of all of it as an Accountant; it returns its ID and ETag.
func (e *env) draftWriteOff() (id, etag string) {
	e.t.Helper()
	inv, _ := e.issuedInvoice("1", "10.00")
	r := e.call(http.MethodPost, "/v1/write-offs", e.as(authz.Accountant), e.writeOffDraft(inv, "10.00"))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createWriteOff = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// postedWriteOff saves a draft write-off and posts it through the posting service, as an approval would; it returns its
// ID and ETag.
func (e *env) postedWriteOff() (id, etag string) {
	e.t.Helper()
	id, _ = e.draftWriteOff()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture-approver"}})
	if _, err := (journal.WriteOffs{Store: e.store}).Post(ctx, e.books.Org, uuid.MustParse(id)); err != nil {
		e.t.Fatal(err)
	}
	return id, e.call(http.MethodGet, "/v1/write-offs/"+id, e.as(authz.Accountant), nil).Header.Get("ETag")
}

// FR-AR-17 and SOD-9 through the API (p1-sales task 2.3): a draft write-off shows what it would post; it posts only on
// another person's approval, never one who prepared it or recorded a receipt of the invoice; the invoice is then written
// off; in Founder mode its void is asked for and reopens the invoice.
// req: FR-AR-17
// req: SOD-9
// req: FM-1
func TestWriteOffsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)

	// The happy path: Ah Kow prepares and asks; Wei Ling approves.
	inv, _ := e.issuedInvoice("1", "1000.00")
	d := e.call(http.MethodPost, "/v1/write-offs", ahkow, e.writeOffDraft(inv, "1000.00"))
	if d.Status != 201 || d.str("states", "approval") != "draft" || d.str("carrying", "amount") != "1000.00" || d.str("account", "code") != "6470" ||
		d.Body["relief_review"] != true || d.str("invoice", "open", "amount") != "1000.00" {
		t.Fatalf("the draft: %d %s", d.Status, d.Raw)
	}
	g := e.call(http.MethodGet, "/v1/write-offs/"+d.str("id"), weiling, nil)
	if len(g.Body["ledger_impact"].([]any)) != 2 || len(g.Body["checks"].([]any)) != 0 {
		t.Fatalf("the draft's impact: %s", g.Raw)
	}
	asked := e.call(http.MethodPost, "/v1/write-offs/"+d.str("id")+"/post-approval", ahkow, nil, "If-Match", d.Header.Get("ETag"))
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectWriteOff || !strings.Contains(asked.str("summary"), "Write off 1000.00 SGD of INV-0001 to Acme Analytics Pte Ltd (bad debt): The customer went into liquidation") {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}
	g = e.call(http.MethodGet, "/v1/write-offs/"+d.str("id"), weiling, nil)
	if g.str("status") != "posted" || g.str("number") != "WO-0001" || g.str("posted_by", "id") != f.weiling {
		t.Fatalf("after the approval: %s", g.Raw)
	}
	if i := e.call(http.MethodGet, "/v1/invoices/"+inv, weiling, nil); i.str("states", "settlement") != "written_off" {
		t.Errorf("the invoice: %s", i.Raw)
	}

	// SOD-9: Wei Ling recorded a receipt of the invoice, so she never approves its write-off.
	other, _ := e.issuedInvoice("1", "500.00")
	if r := e.call(http.MethodPost, "/v1/customer-receipts", weiling, e.receiptOf(other, "400.00")); r.Status != 201 {
		t.Fatalf("the receipt: %d %s", r.Status, r.Raw)
	}
	d2 := e.call(http.MethodPost, "/v1/write-offs", ahkow, e.writeOffDraft(other, "100.00"))
	asked = e.call(http.MethodPost, "/v1/write-offs/"+d2.str("id")+"/post-approval", ahkow, nil, "If-Match", d2.Header.Get("ETag"))
	e.problemIs(f.decide(weiling, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, CodeOwnWriteOff)
	e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/withdraw", ahkow, nil, "If-Match", `"1"`)

	// Nor does whoever prepared it: Ah Kow prepares, Wei Ling asks, Ah Kow may not approve.
	third, _ := e.issuedInvoice("1", "50.00")
	d3 := e.call(http.MethodPost, "/v1/write-offs", ahkow, e.writeOffDraft(third, "50.00"))
	asked = e.call(http.MethodPost, "/v1/write-offs/"+d3.str("id")+"/post-approval", weiling, nil, "If-Match", d3.Header.Get("ETag"))
	e.problemIs(f.decide(ahkow, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, CodeOwnWriteOff)

	// Founder mode: the void is asked for and approved; the invoice reopens.
	posted := g.Header.Get("ETag")
	reversal := map[string]any{"date": "2026-10-31", "reason": "Paid after all"}
	e.problemIs(e.call(http.MethodPost, "/v1/write-offs/"+d.str("id")+"/void", ahkow, reversal, "If-Match", posted), "voidWriteOff", 409, CodeWriteOffVoidApproval)
	void := e.call(http.MethodPost, "/v1/write-offs/"+d.str("id")+"/void-approval", ahkow, reversal, "If-Match", posted)
	if a := f.decide(weiling, void.str("id"), "approve", hashOf(void)); a.Status != 200 {
		t.Fatalf("approving the void: %d %s", a.Status, a.Raw)
	}
	if i := e.call(http.MethodGet, "/v1/invoices/"+inv, weiling, nil); i.str("states", "settlement") != "open" || i.str("open", "amount") != "1000.00" {
		t.Errorf("the invoice after the void: %s", i.Raw)
	}

	// What cannot post is shown, and refused when asked.
	bad := e.writeOffDraft(other, "100.00")
	delete(bad, "account_id")
	r := e.call(http.MethodPost, "/v1/write-offs", ahkow, bad)
	if g := e.call(http.MethodGet, "/v1/write-offs/"+r.str("id"), ahkow, nil); !strings.Contains(g.Raw, writeoff.CodeAccount) || g.Body["carrying"] != nil {
		t.Errorf("a bad debt with no account: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/write-offs/"+r.str("id")+"/post-approval", ahkow, nil, "If-Match", r.Header.Get("ETag")),
		"requestWriteOffPost", 422, writeoff.CodeAccount)
}
