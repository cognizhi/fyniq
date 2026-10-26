//go:build local

package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// joinedIn is a new person who joined organisation org through an invitation with roles, made in the store as the
// founder bootstrap makes it.
func (e *env) joinedIn(org uuid.UUID, name string, roles ...string) uuid.UUID {
	e.t.Helper()
	ctx := context.Background()
	hash := sha256.Sum256([]byte(name + uuid.NewString()))
	if err := e.store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash[:], DisplayName: name, Roles: roles,
			ExpiresAt: e.clock.Now().Add(time.Hour), CreatedAt: e.clock.Now()})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	j, err := e.store.JoinOrganisation(ctx, storage.NewJoin{CodeHash: hash[:], DisplayName: name, Issuer: "urn:fyniq:test", Subject: uuid.NewString(),
		Now: e.clock.Now()})
	if err != nil {
		e.t.Fatal(err)
	}
	return j.UserID
}

// otherUser is a person of another organisation, who signs a test's request: the database needs only another user, and
// e's own members stay as they are.
func (e *env) otherUser() uuid.UUID {
	e.t.Helper()
	b := porttest.NewBooks(e.t, e.store, "Signer "+uuid.NewString()[:8]+" Pte Ltd")
	return e.joinedIn(b.Org, "Signer", "approver")
}

// postedClaim is a S$27.30 claim of claimant's, approved and posted as the approval engine does it: the claimant asks,
// and another person signs (FY233).
func (e *env) postedClaim(claimant uuid.UUID) string {
	e.t.Helper()
	id, _ := e.ownClaimOf(claimant)
	signer := e.otherUser()
	now := e.clock.Now()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: signer.String()}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		envelope := fmt.Sprintf(`{"object":{"id":%q,"type":"expense_claim"},"nonce":%q}`, id, uuid.NewString())
		sum := sha256.Sum256([]byte(envelope))
		r, err := u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: approval.ObjectClaim, ObjectKey: id, Operation: approval.OpClaimPost,
			Summary: "A claim", Change: []byte(`{"execute":{}}`), Envelope: envelope, EnvelopeHash: sum[:], PolicyVersion: "p",
			PolicySteps: []storage.ApprovalStep{{Quorum: 1}}, RequestedBy: claimant, RequestedAt: now, RequesterMFAMethod: "session_step_up",
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
		_, err = e.api.claims.Post(ctx, e.books.Org, uuid.MustParse(id))
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// recordedReimbursement pays back a posted claim of claimant's in full from a new money account, and returns the
// reimbursement's ID and ETag.
func (e *env) recordedReimbursement(claimant uuid.UUID) (id, tag string) {
	e.t.Helper()
	c := e.postedClaim(claimant)
	acc := e.chartMoneyAccount()
	r := e.call(http.MethodPost, "/v1/claim-reimbursements", e.as(authz.Accountant), map[string]any{"claimant_id": claimant.String(),
		"date": "2026-10-15", "money_account_id": acc.ID.String(), "claims": []any{map[string]any{"claim_id": c, "amount": sgd("27.30")}}})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("recordClaimReimbursement = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// FR-AP-07 and FR-AP-14 (p1-expense-claims task 3.1; the owner's decisions of 26 Oct 2026, rev 0.147 and 0.148): until
// payment batches, the Accountant records what was paid back to a claimant by hand, under "Payment batches", and it
// posts at once, Dr 2150 · Cr the bank, settling the claims, which are then paid; bank import matches the bank line to
// it as money recorded by hand. In Founder mode a void needs the Controller's approval, and the claims are owed again.
// What the company owes each claimant equals 2150's balance, and an Employee sees only what they are owed.
// req: FR-AP-07
// req: FR-AP-14
// req: purchases/expense-claims Out-of-pocket receipt claim
// asvs: V8.2.2
func TestReimbursementsPayClaimsBack(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	travel, _ := e.claimSetup()
	ahkow := c.as(c.ahkow, authz.Employee)
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	mei := c.as(c.mei, authz.Accountant)
	approved := func(lines ...any) string {
		r := e.call(http.MethodPost, "/v1/expense-claims", ahkow, map[string]any{"date": "2026-10-15", "lines": lines})
		s := e.call(http.MethodPost, "/v1/expense-claims/"+r.str("id")+"/submit", ahkow, nil, "If-Match", r.Header.Get("ETag"))
		if d := e.call(http.MethodPost, "/v1/approvals/"+s.str("id")+"/approve", weiling, hashOf(s), "If-Match", `"1"`); d.Status != 200 {
			t.Fatalf("approving: %d %s", d.Status, d.Raw)
		}
		return r.str("id")
	}
	one, two, three := approved(taxi(travel)), approved(taxi(travel), taxi(travel)), approved(taxi(travel))

	// What the company owes Ah Kow: S$109.20, which is 2150's balance.
	owed := e.call(http.MethodGet, "/v1/expense-claims/owed", ahkow, nil)
	if owed.Status != 200 || owed.str("total", "amount") != "109.20" || len(owed.Body["people"].([]any)) != 1 || owed.Body["account_balance"] != nil {
		t.Fatalf("what he is owed: %d %s", owed.Status, owed.Raw)
	}
	if all := e.call(http.MethodGet, "/v1/expense-claims/owed", weiling, nil); all.str("account_balance", "amount") != "109.20" ||
		all.str("difference", "amount") != "0.00" || !strings.Contains(all.Raw, `"name":"Ah Kow"`) {
		t.Errorf("everyone's: %s", all.Raw)
	}

	// The Accountant pays the first two back from DBS on one reimbursement, S$81.90, and the third on another.
	m, line := e.bankLine("-81.90")
	pay := func(parts ...string) response {
		var claims []any
		for i := 0; i < len(parts); i += 2 {
			claims = append(claims, map[string]any{"claim_id": parts[i], "amount": sgd(parts[i+1])})
		}
		return e.call(http.MethodPost, "/v1/claim-reimbursements", mei, map[string]any{"claimant_id": c.ahkow, "date": "2026-10-20",
			"money_account_id": m.AccountID.String(), "reference": "FAST 1", "claims": claims})
	}
	e.problemIs(e.call(http.MethodPost, "/v1/claim-reimbursements", weiling, map[string]any{"claimant_id": c.ahkow, "date": "2026-10-20",
		"money_account_id": m.AccountID.String(), "claims": []any{map[string]any{"claim_id": one, "amount": sgd("27.30")}}}),
		"recordClaimReimbursement", 403, CodeForbidden)
	e.problemIs(pay(one, "27.31"), "recordClaimReimbursement", 422, claim.CodeOverpaid)
	e.problemIs(pay(uuid.NewString(), "1.00"), "recordClaimReimbursement", 422, claim.CodeNotOwed)
	e.problemIs(pay(one, "27.30", one, "1.00"), "recordClaimReimbursement", 422, claim.CodeNotOwed)
	first := pay(one, "27.30", two, "54.60")
	if first.Status != 201 || first.str("number") != "RB-0001" || first.str("total", "amount") != "81.90" || len(first.Body["claims"].([]any)) != 2 ||
		first.str("claimant", "name") != "Ah Kow" || len(first.Body["ledger_impact"].([]any)) != 2 {
		t.Fatalf("paying the first back: %d %s", first.Status, first.Raw)
	}
	second := pay(three, "27.30")
	if second.Status != 201 {
		t.Fatalf("paying the second back: %d %s", second.Status, second.Raw)
	}
	for _, x := range []string{one, two, three} {
		if g := e.call(http.MethodGet, "/v1/expense-claims/"+x, ahkow, nil); g.str("stage") != "paid" {
			t.Errorf("a claim paid back: %s", g.Raw)
		}
	}
	if o := e.call(http.MethodGet, "/v1/expense-claims/owed?as_at=2026-10-20", weiling, nil); o.str("total", "amount") != "0.00" || o.str("account_balance", "amount") != "0.00" ||
		len(o.Body["people"].([]any)) != 0 {
		t.Errorf("nothing owed: %s", o.Raw)
	}

	// The bank line of S$81.90 is matched to the first, as money recorded by hand.
	if r := e.call(http.MethodPost, "/v1/bank-lines/"+line.ID.String()+"/match-recorded", e.as(authz.Accountant),
		map[string]any{"document_id": first.str("id")}, "If-Match", etag(line.Version)); r.Status != 200 {
		t.Fatalf("matching the bank line: %d %s", r.Status, r.Raw)
	}

	// In Founder mode the second is voided only on the Controller's approval: the third claim is owed again.
	void := map[string]any{"date": "2026-10-21", "reason": "Paid to the wrong account"}
	path := "/v1/claim-reimbursements/" + second.str("id")
	e.problemIs(e.call(http.MethodPost, path+"/void", mei, void, "If-Match", second.Header.Get("ETag")), "voidClaimReimbursement", 409, CodeReimbursementVoidApproval)
	asked := e.call(http.MethodPost, path+"/void-approval", mei, void, "If-Match", second.Header.Get("ETag"))
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectReimbursement {
		t.Fatalf("asking to void: %d %s", asked.Status, asked.Raw)
	}
	if d := e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/approve", weiling, hashOf(asked), "If-Match", `"1"`); d.Status != 200 || d.str("status") != "approved" {
		t.Fatalf("approving the void: %d %s", d.Status, d.Raw)
	}
	if g := e.call(http.MethodGet, path, mei, nil); g.str("status") != "reversed" {
		t.Errorf("the voided reimbursement: %s", g.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/expense-claims/"+three, ahkow, nil); g.str("stage") != "approved" {
		t.Errorf("the second claim after the void: %s", g.Raw)
	}
	if o := e.call(http.MethodGet, "/v1/expense-claims/owed?as_at=2026-10-31", ahkow, nil); o.str("total", "amount") != "27.30" {
		t.Errorf("owed again: %s", o.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/claim-reimbursements?claimant_id="+c.ahkow, mei, nil); len(l.Body["items"].([]any)) != 2 {
		t.Errorf("his reimbursements: %s", l.Raw)
	}
}

// matrixReimbursements are the authorisation matrix's rows for reimbursements of claims and what the company owes.
var matrixReimbursements = map[string]func(e *env, tok string) (response, func() bool){
	"listClaimReimbursements": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/claim-reimbursements", tok, nil), nil
	},
	"getClaimReimbursement": func(e *env, tok string) (response, func() bool) {
		id, _ := e.recordedReimbursement(uuid.MustParse(matrixApprover(e)))
		return e.call(http.MethodGet, "/v1/claim-reimbursements/"+id, tok, nil), nil
	},
	"recordClaimReimbursement": func(e *env, tok string) (response, func() bool) {
		claimant := uuid.MustParse(matrixApprover(e))
		c := e.postedClaim(claimant)
		acc := e.chartMoneyAccount()
		r := e.call(http.MethodPost, "/v1/claim-reimbursements", tok, map[string]any{"claimant_id": claimant.String(), "date": "2026-10-15",
			"money_account_id": acc.ID.String(), "claims": []any{map[string]any{"claim_id": c, "amount": sgd("27.30")}}})
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/expense-claims/"+c, e.as(authz.Owner), nil).str("stage") == "approved"
		}
	},
	// A void in Founder mode is asked for, so the row voids in an organisation in Standard mode.
	"voidClaimReimbursement": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.recordedReimbursement(s.joinedIn(s.books.Org, "Standard claimant", "employee"))
		r := s.call(http.MethodPost, "/v1/claim-reimbursements/"+id+"/void", tok, map[string]any{"date": "2026-10-28", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/claim-reimbursements/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	"requestClaimReimbursementVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, approval.ObjectReimbursement)
		id, tag := e.recordedReimbursement(uuid.MustParse(matrixApprover(e)))
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/claim-reimbursements/"+id+"/void-approval", person, map[string]any{"date": "2026-10-28", "reason": "Matrix test"},
			"If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, approval.ObjectReimbursement) == was }
	},
	"getExpenseClaimsOwed": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/expense-claims/owed", tok, nil), nil
	},
}
