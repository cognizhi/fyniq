//go:build local

package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/domain/paymentbatch"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// batchAccount is a new SGD bank account in Banking, which a batch is paid from.
func (e *env) batchAccount() storage.MoneyAccount {
	e.t.Helper()
	acc := e.chartMoneyAccount()
	var m storage.MoneyAccount
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		m, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: acc.ID, Kind: storage.MoneyBank, Name: acc.Name,
			Identifier: "072-" + acc.Code, Currency: "SGD", At: e.clock.Now()})
		return err
	})
	return m
}

// billOf posts a bill of supplier for net plus GST as an Accountant and returns its ID.
func (e *env) billOf(supplier, net, gst string) string {
	e.t.Helper()
	d := e.billDraft(net, gst)
	d["supplier_id"], d["supplier_ref"] = supplier, "REF-"+uuid.NewString()[:8]
	r := e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), d)
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createBill = %d: %s", r.Status, r.Raw)
	}
	p := e.call(http.MethodPost, "/v1/bills/"+r.str("id")+"/post", e.as(authz.Accountant), nil, "If-Match", r.Header.Get("ETag"))
	if p.Status != http.StatusOK {
		e.t.Fatalf("postBill = %d: %s", p.Status, p.Raw)
	}
	return r.str("id")
}

// approveAsAnother approves a request for an object as the approval engine does it: requested by one person, signed by
// another, then executed in the same unit of work.
func (e *env) approveAsAnother(objectType, key, operation string, execute func(ctx context.Context, u storage.UnitOfWork) error) {
	e.t.Helper()
	requester, signer := e.otherUser(), e.otherUser()
	now := e.clock.Now()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: signer.String()}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		envelope := fmt.Sprintf(`{"object":{"id":%q,"type":%q},"nonce":%q}`, key, objectType, uuid.NewString())
		sum := sha256.Sum256([]byte(envelope))
		r, err := u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: objectType, ObjectKey: key, Operation: operation,
			Summary: "A change", Change: []byte(`{"execute":{}}`), Envelope: envelope, EnvelopeHash: sum[:], PolicyVersion: "p",
			PolicySteps: []storage.ApprovalStep{{Quorum: 1}}, RequestedBy: requester, RequestedAt: now, RequesterMFAMethod: "session_step_up",
			RequesterMFAAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour)})
		if err != nil {
			return err
		}
		if _, err := u.Approvals().Sign(ctx, storage.NewApprovalSignature{RequestID: r.ID, Step: 1, Signer: signer, Decision: "approve",
			EnvelopeHash: r.EnvelopeHash, MFAMethod: "session_step_up", MFAAt: now, SignedAt: now}); err != nil {
			return err
		}
		if _, err := u.Approvals().Decide(ctx, r.ID, r.Version, "approved", "", now); err != nil {
			return err
		}
		return execute(ctx, u)
	}); err != nil {
		e.t.Fatal(err)
	}
}

// vendorAccount puts an SGD account at DBS in force for a vendor, approved by another person.
func (e *env) vendorAccount(contact, number string) string {
	e.t.Helper()
	id, _ := e.bankChange(contact, e.as(authz.Accountant), sgAccount(number))
	e.bankLetter(id, e.as(authz.Accountant))
	e.approveAsAnother(approval.ObjectPayeeBankChange, id, approval.OpPayeeBankChange, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := payee.Approve(ctx, u, uuid.MustParse(id), e.clock.Now())
		return err
	})
	return id
}

// personAccount puts the matrix person's own SGD account in force, approved by another person, and returns the person.
func (e *env) personAccount(number string) uuid.UUID {
	e.t.Helper()
	me := steppedUp(e, e.as(authz.Employee))
	id, _ := e.myBankChange(me, myAccount(number))
	e.myScreenshot(id, me)
	e.approveAsAnother(approval.ObjectClaimantBankChange, id, approval.OpClaimantBankChange, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := payee.Approve(ctx, u, uuid.MustParse(id), e.clock.Now())
		return err
	})
	return uuid.MustParse(matrixApprover(e))
}

// batchBody is a batch paid from acc on 31 October by FAST, of the items, each "bill:<id>" or "claim:<id>".
func batchBody(acc storage.MoneyAccount, items ...string) map[string]any {
	list := []any{}
	for _, x := range items {
		kind, id, _ := strings.Cut(x, ":")
		list = append(list, map[string]any{"kind": kind, "document_id": id})
	}
	return map[string]any{"money_account_id": acc.AccountID.String(), "payment_date": "2026-10-31", "method": "fast", "items": list}
}

// checkCodes are the codes of a batch's checks, and of each item's.
func checkCodes(r response) string {
	var codes []string
	for _, c := range r.Body["checks"].([]any) {
		codes = append(codes, c.(map[string]any)["code"].(string))
	}
	return strings.Join(codes, ",")
}

// A batch is prepared from approved bills and claims (FR-BNK-12; PRD-001 rev 0.154): a supplier's three bills are one
// payment into its account in force, a claimant's claim one reimbursement; and every hold that stops an item is shown at
// once, each triggered here (QA-RPT-02): a payee with no approved account (PB-12), one whose change waits (PAYEE-12), a
// paid bill (PB-10), a bill waiting for withholding (PB-13), a bill in USD (PB-09), an item in another approved batch
// (PB-11), and a draft that is not a posted bill (PB-08). Saving checks the form only (PB-06, PB-07).
// req: FR-BNK-12
// req: FR-AP-19
// req: FR-AP-22
// req: QA-RPT-02
// req: banking/bank-transactions SGD to Singapore accounts (rev 0.154)
// req: banking/bank-transactions Bills and expense claims (rev 0.154)
func TestAPaymentBatchChecksEveryItem(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	acc := e.batchAccount()
	northwind := e.vendor("Northwind Analytics Pte Ltd")
	e.vendorAccount(northwind, "0123456789")
	b1, b2, b3 := e.billOf(northwind, "100.00", "9.00"), e.billOf(northwind, "200.00", "18.00"), e.billOf(northwind, "50.00", "4.50")
	person := e.personAccount("5550001234")
	claim := e.postedClaim(person)

	r := e.call(http.MethodPost, "/v1/payment-batches", accountant, batchBody(acc, "bill:"+b1, "bill:"+b2, "bill:"+b3, "claim:"+claim))
	if r.Status != http.StatusCreated || r.str("status") != "draft" || r.Header.Get("ETag") != `"1-draft"` {
		t.Fatalf("createPaymentBatch = %d: %s", r.Status, r.Raw)
	}
	if c := checkCodes(r); c != "" {
		t.Fatalf("a batch of payable items has checks: %s", c)
	}
	pays := r.Body["payments"].([]any)
	if len(pays) != 2 || r.str("total", "amount") != "408.80" {
		t.Fatalf("the batch's payments: %s", r.Raw)
	}
	nw := pays[0].(map[string]any)
	if nw["payee"].(map[string]any)["name"] != "Northwind Analytics Pte Ltd" || len(nw["items"].([]any)) != 3 ||
		nw["amount"].(map[string]any)["amount"] != "381.50" || nw["account"].(map[string]any)["account_number_last4"] != "6789" {
		t.Errorf("a supplier's three bills are not one payment: %v", nw)
	}
	if p := pays[1].(map[string]any); p["payee"].(map[string]any)["kind"] != "person" || p["amount"].(map[string]any)["amount"] != "27.30" {
		t.Errorf("the claim is not one reimbursement: %v", p)
	}

	// Each hold, triggered.
	noAccount := e.vendor("No Account Pte Ltd")
	frozen := e.vendor("Frozen Pte Ltd")
	e.vendorAccount(frozen, "1112223334")
	change, ctag := e.bankChange(frozen, accountant, sgAccount("9998887776"))
	e.bankLetter(change, accountant)
	if a := e.call(http.MethodPost, "/v1/bank-changes/"+change+"/approval", steppedUp(e, accountant), nil, "If-Match", ctag); a.Status != http.StatusAccepted {
		t.Fatalf("asking for the frozen vendor's change = %d: %s", a.Status, a.Raw)
	}
	paid := e.billOf(northwind, "10.00", "0.90")
	if p := e.call(http.MethodPost, "/v1/bill-payments", accountant, e.payment(paid, "10.90")); p.Status != http.StatusCreated {
		t.Fatalf("paying a bill by hand = %d: %s", p.Status, p.Raw)
	}
	foreign := e.foreignSupplier()
	e.call(http.MethodPatch, "/v1/contacts/"+foreign, accountant, map[string]any{"tax_residence": "AU"}, "If-Match", "*")
	wht := e.billDraft("500.00", "0.00")
	wht["supplier_id"], wht["supplier_ref"], wht["nature_of_payment"] = foreign, "AVA-9", "services_in_singapore"
	wht["lines"].([]map[string]any)[0]["gst_code"] = "NR"
	w := e.call(http.MethodPost, "/v1/bills", accountant, wht)
	if p := e.call(http.MethodPost, "/v1/bills/"+w.str("id")+"/post", accountant, nil, "If-Match", w.Header.Get("ETag")); p.Status != http.StatusOK {
		t.Fatalf("posting the bill waiting for withholding = %d: %s", p.Status, p.Raw)
	}
	usd := e.billDraft("100.00", "0.00")
	usd["supplier_id"], usd["supplier_ref"], usd["currency"], usd["fx_rate"] = northwind, "US-1", "USD", "1.34"
	usd["lines"].([]map[string]any)[0]["gst_code"] = "NR"
	usd["lines"].([]map[string]any)[0]["net"] = map[string]any{"amount": "100.00", "currency": "USD"}
	usd["lines"].([]map[string]any)[0]["gst"] = map[string]any{"amount": "0.00", "currency": "USD"}
	u := e.call(http.MethodPost, "/v1/bills", accountant, usd)
	if p := e.call(http.MethodPost, "/v1/bills/"+u.str("id")+"/post", accountant, nil, "If-Match", u.Header.Get("ETag")); p.Status != http.StatusOK {
		t.Fatalf("posting the USD bill = %d: %s", p.Status, p.Raw)
	}
	draftBill, _ := e.createBill("1.00", "0.09")
	held := e.call(http.MethodPost, "/v1/payment-batches", accountant, batchBody(acc, "bill:"+e.billOf(noAccount, "20.00", "1.80"),
		"bill:"+e.billOf(frozen, "30.00", "2.70"), "bill:"+paid, "bill:"+w.str("id"), "bill:"+u.str("id"), "bill:"+draftBill))
	if held.Status != http.StatusCreated {
		t.Fatalf("saving the held batch = %d: %s", held.Status, held.Raw)
	}
	codes := checkCodes(held)
	for _, want := range []string{paymentbatch.CodeNoAccount, payee.CodeFrozen, paymentbatch.CodeNothingOpen, paymentbatch.CodeWithholding,
		paymentbatch.CodeCurrency, paymentbatch.CodeItem} {
		if !strings.Contains(codes, want) {
			t.Errorf("no %s among the held batch's checks: %s", want, codes)
		}
	}

	// In another approved batch: PB-11 on the second.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, uuid.MustParse(r.str("id")))
		if err != nil {
			return err
		}
		v, err := e.api.batchView(ctx, u)
		if err != nil {
			return err
		}
		other := uuid.New()
		v.approved = map[uuid.UUID]uuid.UUID{uuid.MustParse(b1): other}
		b, err := paymentbatch.Evaluate(ctx, u, doc, v.settings, v.approved, v.now)
		if err != nil {
			return err
		}
		if !strings.Contains(paymentbatch.Problems(b.Errors).Error(), paymentbatch.CodeInAnother) {
			t.Errorf("a bill in another approved batch: %v", b.Errors)
		}
		return nil
	})

	// Saving checks the form: an item twice, and nothing to pay.
	e.problemIs(e.call(http.MethodPost, "/v1/payment-batches", accountant, batchBody(acc, "bill:"+b1, "bill:"+b1)), "createPaymentBatch", 422, paymentbatch.CodeTwice)
	e.problemIs(e.call(http.MethodPost, "/v1/payment-batches", accountant, batchBody(acc)), "createPaymentBatch", 422, paymentbatch.CodeEmpty)

	// A draft changes at its version; the list shows both batches.
	again := batchBody(acc, "bill:"+b1)
	c := e.call(http.MethodPut, "/v1/payment-batches/"+r.str("id"), accountant, again, "If-Match", r.Header.Get("ETag"))
	if c.Status != http.StatusOK || c.Body["version"] != float64(2) || c.str("total", "amount") != "109.00" {
		t.Fatalf("replacePaymentBatch = %d: %s", c.Status, c.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/payment-batches/"+r.str("id"), accountant, again, "If-Match", `"1-draft"`), "replacePaymentBatch", 412, CodeStale)
	if l := e.call(http.MethodGet, "/v1/payment-batches?status=draft", accountant, nil); len(l.Body["items"].([]any)) != 2 {
		t.Errorf("the list of drafts: %s", l.Raw)
	}
}

// A new batch for a date starts with the bills planned on or before it and the claims owed (FR-AP-13), none in an
// approved batch; a bill planned later, or not planned, is left out.
// req: FR-AP-13
// req: FR-BNK-12
func TestAPaymentBatchIsSuggestedFromPlannedBills(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	vendor := e.vendor("Planned Pte Ltd")
	early, late, unplanned := e.billOf(vendor, "100.00", "9.00"), e.billOf(vendor, "200.00", "18.00"), e.billOf(vendor, "300.00", "27.00")
	plan := func(id, on string) {
		if r := e.call(http.MethodPost, "/v1/bills/"+id+"/payment-plan", accountant, map[string]any{"planned_on": on}, "If-Match", "*"); r.Status != http.StatusOK {
			t.Fatalf("planning %s = %d: %s", id, r.Status, r.Raw)
		}
	}
	plan(early, "2026-10-30")
	plan(late, "2026-11-15")
	claim := e.postedClaim(uuid.MustParse(matrixApprover(e)))
	s := e.call(http.MethodGet, "/v1/payment-batches/suggestion?payment_date=2026-10-31", accountant, nil)
	got := s.Raw
	if s.Status != http.StatusOK || !strings.Contains(got, early) || !strings.Contains(got, claim) || strings.Contains(got, late) || strings.Contains(got, unplanned) {
		t.Fatalf("the suggestion: %s", got)
	}
}

// The database keeps a batch's approval and release as the rules say (FY239, FY240, FY241, FY242): a batch approved
// without a second person, a draft released, a second release, a release changed or deleted, a link to a payment of
// another account, and a bill paid by hand while an approved batch holds it are each refused.
// req: FR-BNK-12
// req: QA-ENG-05
// req: FM-1
// asvs: V2.3.5, V8.2.2
func TestAttackPaymentBatchesKeepTheirRelease(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	acc := e.batchAccount()
	vendor := e.vendor("Attacked Pte Ltd")
	e.vendorAccount(vendor, "0123456789")
	bill := e.billOf(vendor, "100.00", "9.00")
	r := e.call(http.MethodPost, "/v1/payment-batches", accountant, batchBody(acc, "bill:"+bill))
	batch := uuid.MustParse(r.str("id"))
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "attacker"}})
	attack := func(name, code string, fn func(ctx context.Context, u storage.UnitOfWork) error) {
		t.Helper()
		err := e.store.Do(ctx, e.books.Org, fn)
		var se *storage.Error
		if !errors.As(err, &se) || se.Code != code {
			t.Errorf("%s: want %s, got %v", name, code, err)
		}
	}
	someone := e.otherUser()
	release := func(version int, approvalID uuid.UUID) storage.PaymentBatchRelease {
		return storage.PaymentBatchRelease{BatchID: batch, BatchVersion: version, ApprovalID: approvalID, ReleasedBy: someone, ReleasedAt: e.clock.Now(),
			Writer: "payment-list@1", FileName: "PB-0001.csv", MediaType: "text/csv", Sealed: make([]byte, 64), SHA256: make([]byte, 32), Payments: 1,
			Currency: "SGD", Total: "109.00"}
	}
	attack("a batch approved without a request", storage.CodeBatch, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Documents().SetStatus(ctx, batch, storage.DocumentPosted)
	})
	var approvalID uuid.UUID
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		rs, err := u.Approvals().List(ctx, "")
		for _, x := range rs {
			approvalID = x.ID
		}
		return err
	})
	if approvalID == uuid.Nil { // a request of any kind, to name in a release the guard refuses before it looks at it
		e.vendorAccount(e.vendor("Approval Holder Pte Ltd"), "4443332221")
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			rs, err := u.Approvals().List(ctx, "")
			for _, x := range rs {
				approvalID = x.ID
			}
			return err
		})
	}
	attack("a draft batch released", storage.CodeBatch, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PaymentBatches().Release(ctx, release(1, approvalID))
		return err
	})

	// Approved by another person: it posts, and a payment of its bill by hand is refused while it is unreleased.
	e.approveAsAnother(approval.ObjectPaymentBatch, batch.String(), approval.OpBatchApprove, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Sequences().Number(ctx, batch, storage.DocumentPaymentBatch); err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, batch, storage.DocumentPosted)
	})
	if p := e.call(http.MethodPost, "/v1/bill-payments", accountant, e.payment(bill, "109.00")); p.Status == http.StatusCreated ||
		!strings.Contains(p.Raw, storage.CodeBatchHeld) {
		t.Errorf("a bill in an approved batch paid by hand = %d: %s", p.Status, p.Raw)
	}
	var rel storage.PaymentBatchRelease
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		rs, err := u.Approvals().List(ctx, "")
		for _, x := range rs {
			if x.ObjectType == approval.ObjectPaymentBatch {
				approvalID = x.ID
			}
		}
		if err != nil {
			return err
		}
		rel, err = u.PaymentBatches().Release(ctx, release(1, approvalID))
		return err
	})
	attack("a second release", storage.CodeBatch, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PaymentBatches().Release(ctx, release(1, approvalID))
		return err
	})
	other := e.call(http.MethodPost, "/v1/bill-payments", accountant, e.payment(e.billOf(vendor, "1.00", "0.09"), "1.09"))
	if other.Status != http.StatusCreated {
		t.Fatalf("paying another bill = %d: %s", other.Status, other.Raw)
	}
	attack("a release's payment of another account", storage.CodeBatch, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.PaymentBatches().Link(ctx, rel.ID, uuid.MustParse(other.str("payment", "id")))
	})

	// The release never changes and never goes, even for the table's owner.
	owner, err := pgx.Connect(context.Background(), db.URL("fyniq"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(context.Background())
	for _, stmt := range []string{
		"UPDATE fyniq.payment_batch_release SET file_sealed = '\\x00'::bytea || file_sealed",
		"UPDATE fyniq.payment_batch_release SET total = total + 1",
		"DELETE FROM fyniq.payment_batch_release",
		"TRUNCATE fyniq.payment_batch_release CASCADE",
	} {
		tx, err := owner.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(context.Background(), "SELECT set_config('app.org_id', $1, true)", e.books.Org.String())
		if err == nil {
			_, err = tx.Exec(context.Background(), stmt)
		}
		_ = tx.Rollback(context.Background())
		if err == nil || !strings.Contains(err.Error(), "FY239") {
			t.Errorf("the owner: %s: %v", stmt, err)
		}
	}
	attack("a released batch cancelled", storage.CodeBatch, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Documents().SetStatus(ctx, batch, storage.DocumentReversed)
	})
}

// matrixBatch prepares a draft batch of one posted bill as an Accountant and returns its ID.
func matrixBatch(e *env) string {
	bill, _ := e.postedBill("10.00", "0.90")
	r := e.call(http.MethodPost, "/v1/payment-batches", e.as(authz.Accountant), batchBody(e.batchAccount(), "bill:"+bill))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createPaymentBatch = %d: %s", r.Status, r.Raw)
	}
	return r.str("id")
}

// batchCount is how many batches the organisation has.
func batchCount(e *env) int {
	return len(e.call(http.MethodGet, "/v1/payment-batches?page_size=500", e.as(authz.Owner), nil).Body["items"].([]any))
}

// matrixPaymentBatches has one request per operation on payment batches.
var matrixPaymentBatches = map[string]func(e *env, tok string) (response, func() bool){
	"listPaymentBatches": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/payment-batches", tok, nil), nil
	},
	"getPaymentBatchSuggestion": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/payment-batches/suggestion?payment_date=2026-10-31", tok, nil), nil
	},
	"createPaymentBatch": func(e *env, tok string) (response, func() bool) {
		bill, _ := e.postedBill("10.00", "0.90")
		body := batchBody(e.batchAccount(), "bill:"+bill)
		was := batchCount(e)
		r := e.call(http.MethodPost, "/v1/payment-batches", tok, body)
		return r, func() bool { return batchCount(e) == was }
	},
	"getPaymentBatch": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/payment-batches/"+matrixBatch(e), tok, nil), nil
	},
	"replacePaymentBatch": func(e *env, tok string) (response, func() bool) {
		id := matrixBatch(e)
		bill, _ := e.postedBill("20.00", "1.80")
		r := e.call(http.MethodPut, "/v1/payment-batches/"+id, tok, batchBody(e.batchAccount(), "bill:"+bill), "If-Match", "*")
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/payment-batches/"+id, e.as(authz.Accountant), nil).Body["version"] == float64(1)
		}
	},
}
