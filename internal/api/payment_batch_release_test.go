//go:build local

package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// personAs is a session of user, a member, stepped up now, holding roles.
func (e *env) personAs(user uuid.UUID, roles ...authz.Role) string {
	return e.principal(authn.Principal{Subject: "person-" + user.String(), UserID: user, Client: "web", Class: authn.ClassUser,
		Organisation: e.books.Org, Roles: roles, StepUpAt: e.clock.Now()})
}

// askBatch asks for approval (or, with again, approval again) of a batch as tok, and returns the request.
func (e *env) askBatch(id, tok, tag string, again bool) response {
	e.t.Helper()
	path := "/approval"
	if again {
		path = "/reapproval"
	}
	r := e.call(http.MethodPost, "/v1/payment-batches/"+id+path, tok, nil, "If-Match", tag)
	if r.Status != http.StatusAccepted || r.str("object_type") != approval.ObjectPaymentBatch {
		e.t.Fatalf("asking for the batch's approval = %d: %s", r.Status, r.Raw)
	}
	return r
}

// A batch is approved, then released, in Founder mode (FR-BNK-12; PRD-001 rev 0.154 and rev 0.156). Ah Kow prepares a
// batch of a supplier's two bills and a claim and asks for it; he cannot approve it; Wei Ling approves it, which numbers
// it; the bills it holds are no longer paid by hand (FY241). Then the supplier's account changes: the release is refused
// until the batch is approved again, and the re-approval shows the new account (G4-055). Ah Kow, holding the Payment
// releaser role, releases it although he prepared it, which Founder mode allows and logs for the weekly review (FM-3):
// one bill payment and one reimbursement post on the payment date as recording them by hand does, the bills and the
// claim are paid, and the payment list is written, sealed, with its SHA-256; a second release is refused. The list is
// downloaded only after step-up, as written, and each download is audited; neither the trail nor the log holds a number.
// req: FR-BNK-12
// req: FR-APR-05
// req: FR-APR-10
// req: G4-055
// req: FM-1
// req: FM-3
// req: SEC-11
// req: AUD-02
// req: banking/bank-transactions Posted on release, matched later (rev 0.154)
// req: banking/bank-transactions Who releases (rev 0.154)
// req: banking/bank-transactions A payment list until the bank's file (rev 0.156)
// asvs: V2.3.5, V8.2.2, V14.2.4
func TestAPaymentBatchIsApprovedThenReleased(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant)
	weiling := f.as(f.weiling)
	acc := e.batchAccount()
	vendor := e.vendor("Northwind Analytics Pte Ltd")
	e.vendorAccount(vendor, "0123456789")
	b1, b2 := e.billOf(vendor, "100.00", "9.00"), e.billOf(vendor, "200.00", "18.00")
	person := e.personAccount("5550001234")
	claim := e.postedClaim(person)

	r := e.call(http.MethodPost, "/v1/payment-batches", ahkow, batchBody(acc, "bill:"+b1, "bill:"+b2, "claim:"+claim))
	if r.Status != http.StatusCreated {
		t.Fatalf("preparing the batch = %d: %s", r.Status, r.Raw)
	}
	id := r.str("id")
	asked := e.askBatch(id, ahkow, r.Header.Get("ETag"), false)
	if g := e.call(http.MethodGet, "/v1/payment-batches/"+id, ahkow, nil); g.str("status") != "waiting" || g.str("approval_id") != asked.str("id") {
		t.Fatalf("waiting: %s", g.Raw)
	}
	if d := f.decide(f.as(f.ahkow), asked.str("id"), "approve", hashOf(asked)); d.Status == http.StatusOK {
		t.Fatalf("Ah Kow approved his own batch: %s", d.Raw)
	}
	if d := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("Wei Ling approving = %d: %s", d.Status, d.Raw)
	}
	g := e.call(http.MethodGet, "/v1/payment-batches/"+id, ahkow, nil)
	if g.str("status") != "approved" || !strings.HasPrefix(g.str("number"), "PB-") {
		t.Fatalf("approved: %s", g.Raw)
	}
	if p := e.call(http.MethodPost, "/v1/bill-payments", ahkow, e.payment(b1, "109.00")); p.Status == http.StatusCreated || !strings.Contains(p.Raw, storage.CodeBatchHeld) {
		t.Errorf("a bill in the approved batch paid by hand = %d: %s", p.Status, p.Raw)
	}

	// The supplier's account changes: the release waits for the batch's approval again, which shows the new account.
	releaser := f.as(f.ahkow, authz.PaymentReleaser)
	e.vendorAccount(vendor, "998877665544")
	e.problemIs(e.call(http.MethodPost, "/v1/payment-batches/"+id+"/release", releaser, nil, "If-Match", g.Header.Get("ETag")),
		"releasePaymentBatch", 409, CodeBatchReapprove)
	again := e.askBatch(id, ahkow, g.Header.Get("ETag"), true)
	if !strings.Contains(again.Raw, "5544") {
		t.Errorf("the re-approval does not show the new account: %s", again.Raw)
	}
	if d := f.decide(weiling, again.str("id"), "approve", hashOf(again)); d.Status != http.StatusOK {
		t.Fatalf("approving again = %d: %s", d.Status, d.Raw)
	}

	// Released by Ah Kow, who prepared it: allowed in Founder mode, and logged.
	e.problemIs(e.call(http.MethodPost, "/v1/payment-batches/"+id+"/release", f.as(f.ahkow, authz.Accountant), nil, "If-Match", g.Header.Get("ETag")),
		"releasePaymentBatch", 403, CodeForbidden)
	rel := e.call(http.MethodPost, "/v1/payment-batches/"+id+"/release", releaser, nil, "If-Match", g.Header.Get("ETag"))
	if rel.Status != http.StatusOK || rel.str("status") != "released" || rel.str("release", "writer") != "payment-list@1" ||
		len(rel.Body["release"].(map[string]any)["payments"].([]any)) != 2 {
		t.Fatalf("releasing = %d: %s", rel.Status, rel.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/payment-batches/"+id+"/release", releaser, nil, "If-Match", rel.Header.Get("ETag")),
		"releasePaymentBatch", 409, CodeBatchReleased)
	if l := e.call(http.MethodGet, "/v1/review-log", weiling, nil); !strings.Contains(l.Raw, storage.ReviewReleaseConflict) || !strings.Contains(l.Raw, "SOD-2") {
		t.Errorf("the release by its preparer is not logged: %s", l.Raw)
	}
	pay := e.call(http.MethodGet, "/v1/bill-payments/"+rel.Body["release"].(map[string]any)["payments"].([]any)[0].(map[string]any)["payment_id"].(string), ahkow, nil)
	if pay.Status != http.StatusOK || pay.str("date") != "2026-10-31" || !strings.Contains(pay.Raw, `"amount":"327.00"`) || pay.str("reference") != g.str("number") {
		t.Errorf("the bill payment the release posted: %s", pay.Raw)
	}
	if after := e.call(http.MethodGet, "/v1/payment-batches/suggestion?payment_date=2026-12-31", ahkow, nil); strings.Contains(after.Raw, b1) || strings.Contains(after.Raw, claim) {
		t.Errorf("a paid bill or claim is still suggested: %s", after.Raw)
	}

	// The list: only after step-up, as written, and audited.
	e.problemIs(e.call(http.MethodGet, "/v1/payment-batches/"+id+"/file", e.as(authz.PaymentReleaser), nil), "getPaymentBatchFile", 403, CodeStepUpRequired)
	file := e.call(http.MethodGet, "/v1/payment-batches/"+id+"/file", releaser, nil)
	data, err := base64.StdEncoding.DecodeString(file.str("content_base64"))
	sum := sha256.Sum256(data)
	if file.Status != http.StatusOK || err != nil || hex.EncodeToString(sum[:]) != rel.str("release", "sha256") || file.str("sha256") != rel.str("release", "sha256") {
		t.Fatalf("downloading the list = %d: %s", file.Status, file.Raw)
	}
	list := string(data)
	for _, want := range []string{"998877665544,327.00,SGD,2026-10-31,FAST," + g.str("number"), "5550001234,27.30,SGD", "Total,,,,,354.30,SGD"} {
		if !strings.Contains(list, want) {
			t.Errorf("the list lacks %q:\n%s", want, list)
		}
	}
	trail := auditTrail(t, e)
	for _, want := range []string{`"action":"payment_batch.released"`, `"action":"payment_batch.file_downloaded"`, `"action":"document.paid_in_batch"`} {
		if !strings.Contains(trail, want) {
			t.Errorf("no %s in the trail", want)
		}
	}
	if strings.Contains(trail, "998877665544") || strings.Contains(trail, "5550001234") || strings.Contains(e.log.String(), "998877665544") {
		t.Error("the trail or the log holds a full account number")
	}
}

// In Standard mode the releaser is a third person (SOD-2), and never who prepared or approved the account change of a payee
// the batch pays (SOD-1): the preparer's release and the approver's are refused, and so is the release by whoever prepared
// the supplier's bank change; a third person releases it. An approved batch, unreleased, is cancelled with a reason, which
// frees its bills; a released one is never cancelled.
// req: FR-BNK-12
// req: SOD-1
// req: SOD-2
// req: banking/bank-transactions Who releases (rev 0.154)
// asvs: V8.2.2
func TestAStandardModeReleaseNeedsAThirdPerson(t *testing.T) {
	s := inStandardMode(newEnv(t))
	prep, appr, rel, changer := s.joinedIn(s.books.Org, "Prep", "accountant"), s.joinedIn(s.books.Org, "Appr", "controller"),
		s.joinedIn(s.books.Org, "Rel", "payment_releaser"), s.joinedIn(s.books.Org, "Changer", "accountant", "payment_releaser")
	acc := s.batchAccount()
	vendor := s.vendor("Standard Vendor Pte Ltd")
	change, ctag := s.bankChange(vendor, s.personAs(changer, authz.Accountant), sgAccount("0123456789"))
	s.bankLetter(change, s.personAs(changer, authz.Accountant))
	askChange := s.call(http.MethodPost, "/v1/bank-changes/"+change+"/approval", s.personAs(changer, authz.Accountant), nil, "If-Match", ctag)
	if d := s.call(http.MethodPost, "/v1/approvals/"+askChange.str("id")+"/approve", s.personAs(appr, authz.Controller), hashOf(askChange), "If-Match", `"1"`); d.Status != http.StatusOK {
		t.Fatalf("approving the supplier's account = %d: %s", d.Status, d.Raw)
	}
	bill := s.billOf(vendor, "100.00", "9.00")

	batch := func() (string, string) {
		r := s.call(http.MethodPost, "/v1/payment-batches", s.personAs(prep, authz.Accountant), batchBody(acc, "bill:"+bill))
		asked := s.askBatch(r.str("id"), s.personAs(prep, authz.Accountant), r.Header.Get("ETag"), false)
		if d := s.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", s.personAs(appr, authz.Controller), hashOf(asked), "If-Match", `"1"`); d.Status != http.StatusOK {
			t.Fatalf("approving the batch = %d: %s", d.Status, d.Raw)
		}
		g := s.call(http.MethodGet, "/v1/payment-batches/"+r.str("id"), s.as(authz.Accountant), nil)
		return r.str("id"), g.Header.Get("ETag")
	}

	// An approved batch is cancelled with a reason, which frees its bill for the next.
	first, ftag := batch()
	s.problemIs(s.call(http.MethodPost, "/v1/payment-batches/"+first+"/cancel", s.as(authz.Accountant), map[string]any{"reason": " "}, "If-Match", ftag),
		"cancelPaymentBatch", 422, CodeBatchReason)
	if c := s.call(http.MethodPost, "/v1/payment-batches/"+first+"/cancel", s.as(authz.Accountant), map[string]any{"reason": "Paid by cheque"}, "If-Match", ftag); c.Status != http.StatusOK ||
		c.str("status") != "cancelled" {
		t.Fatalf("cancelling = %d: %s", c.Status, c.Raw)
	}

	id, tag := batch()
	release := func(who uuid.UUID) response {
		return s.call(http.MethodPost, "/v1/payment-batches/"+id+"/release", s.personAs(who, authz.PaymentReleaser), nil, "If-Match", tag)
	}
	for name, who := range map[string]uuid.UUID{"its preparer": prep, "its approver": appr, "who prepared the supplier's bank change": changer} {
		if r := release(who); r.Status != http.StatusForbidden || r.problemCode() != CodeBatchSeparation {
			t.Errorf("released by %s = %d: %s", name, r.Status, r.Raw)
		}
	}
	if r := release(rel); r.Status != http.StatusOK || r.str("status") != "released" {
		t.Fatalf("released by a third person = %d: %s", r.Status, r.Raw)
	}
	s.problemIs(s.call(http.MethodPost, "/v1/payment-batches/"+id+"/cancel", s.as(authz.Accountant), map[string]any{"reason": "Too late"}, "If-Match", tag),
		"cancelPaymentBatch", 409, CodeBatchReleased)
	if l := s.call(http.MethodGet, "/v1/review-log", s.as(authz.Owner), nil); strings.Contains(l.Raw, storage.ReviewReleaseConflict) {
		t.Errorf("a Standard-mode release was logged as a conflict: %s", l.Raw)
	}
}

// approveBatch approves batch id as the approval engine does, by two people of another organisation (so the
// organisation's members stay as they are), its envelope holding the batch's dependencies; the approval numbers it.
func (e *env) approveBatch(id string) {
	e.t.Helper()
	requester, signer := e.otherUser(), e.otherUser()
	now := e.clock.Now()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: signer.String()}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, deps, err := e.api.batchDependencies(ctx, u, id)
		if err != nil {
			return err
		}
		envelope, err := json.Marshal(map[string]any{"object": map[string]any{"id": id, "type": approval.ObjectPaymentBatch}, "dependencies": deps,
			"nonce": uuid.NewString()})
		if err != nil {
			return err
		}
		sum := sha256.Sum256(envelope)
		req, err := u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: approval.ObjectPaymentBatch, ObjectKey: id, Operation: approval.OpBatchApprove,
			Summary: "A batch", Change: []byte(`{"execute":{}}`), Envelope: string(envelope), EnvelopeHash: sum[:], PolicyVersion: "p",
			PolicySteps: []storage.ApprovalStep{{Quorum: 1}}, RequestedBy: requester, RequestedAt: now, RequesterMFAMethod: "session_step_up",
			RequesterMFAAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour)})
		if err != nil {
			return err
		}
		if _, err := u.Approvals().Sign(ctx, storage.NewApprovalSignature{RequestID: req.ID, Step: 1, Signer: signer, Decision: "approve",
			EnvelopeHash: req.EnvelopeHash, MFAMethod: "session_step_up", MFAAt: now, SignedAt: now}); err != nil {
			return err
		}
		if req, err = u.Approvals().Decide(ctx, req.ID, req.Version, "approved", "", now); err != nil {
			return err
		}
		return e.api.executeBatchApproval(ctx, u, req)
	}); err != nil {
		e.t.Fatal(err)
	}
}

// matrixApprovedBatch is a batch of one bill of a payee with an approved account, prepared by the matrix person and
// approved as the engine approves it, by two people of another organisation (so the matrix's members stay as they are),
// its envelope holding the batch's dependencies; with its ETag.
func matrixApprovedBatch(e *env) (id, tag string) {
	e.t.Helper()
	vendor := e.vendor("Matrix payee " + uuid.NewString()[:8])
	e.vendorAccount(vendor, "0123456789")
	r := e.call(http.MethodPost, "/v1/payment-batches", steppedUp(e, e.as(authz.Accountant)), batchBody(e.batchAccount(), "bill:"+e.billOf(vendor, "10.00", "0.90")))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("preparing the matrix batch = %d: %s", r.Status, r.Raw)
	}
	id = r.str("id")
	e.approveBatch(id)
	g := e.call(http.MethodGet, "/v1/payment-batches/"+id, e.as(authz.Accountant), nil)
	return id, g.Header.Get("ETag")
}

// matrixPaymentBatchRelease has one request per operation on approving, releasing and cancelling batches.
var matrixPaymentBatchRelease = map[string]func(e *env, tok string) (response, func() bool){
	"requestPaymentBatchApproval": func(e *env, tok string) (response, func() bool) {
		vendor := e.vendor("Matrix payee " + uuid.NewString()[:8])
		e.vendorAccount(vendor, "0123456789")
		r := e.call(http.MethodPost, "/v1/payment-batches", e.as(authz.Accountant), batchBody(e.batchAccount(), "bill:"+e.billOf(vendor, "10.00", "0.90")))
		was := requestsOf(e, approval.ObjectPaymentBatch)
		a := e.call(http.MethodPost, "/v1/payment-batches/"+r.str("id")+"/approval", steppedUp(e, tok), nil, "If-Match", r.Header.Get("ETag"))
		return a, func() bool { return requestsOf(e, approval.ObjectPaymentBatch) == was }
	},
	"requestPaymentBatchReapproval": func(e *env, tok string) (response, func() bool) {
		id, tag := matrixApprovedBatch(e)
		was := requestsOf(e, approval.ObjectPaymentBatch)
		a := e.call(http.MethodPost, "/v1/payment-batches/"+id+"/reapproval", steppedUp(e, tok), nil, "If-Match", tag)
		return a, func() bool { return requestsOf(e, approval.ObjectPaymentBatch) == was }
	},
	"releasePaymentBatch": func(e *env, tok string) (response, func() bool) {
		id, tag := matrixApprovedBatch(e)
		r := e.call(http.MethodPost, "/v1/payment-batches/"+id+"/release", steppedUp(e, tok), nil, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/payment-batches/"+id, e.as(authz.Accountant), nil).str("status") == "approved"
		}
	},
	"getPaymentBatchFile": func(e *env, tok string) (response, func() bool) {
		id, tag := matrixApprovedBatch(e)
		if r := e.call(http.MethodPost, "/v1/payment-batches/"+id+"/release", steppedUp(e, e.as(authz.PaymentReleaser)), nil, "If-Match", tag); r.Status != http.StatusOK {
			e.t.Fatalf("releasing the matrix batch = %d: %s", r.Status, r.Raw)
		}
		return e.call(http.MethodGet, "/v1/payment-batches/"+id+"/file", steppedUp(e, tok), nil), nil
	},
	"cancelPaymentBatch": func(e *env, tok string) (response, func() bool) {
		id, tag := matrixApprovedBatch(e)
		r := e.call(http.MethodPost, "/v1/payment-batches/"+id+"/cancel", tok, map[string]any{"reason": "Matrix"}, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/payment-batches/"+id, e.as(authz.Accountant), nil).str("status") == "approved"
		}
	},
}
