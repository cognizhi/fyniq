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
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// bankRule makes a rule in e's organisation's store, as the rules API of task 9.1 will: lines naming AWS to rent at TX.
func (e *env) bankRule(name string) storage.BankRule {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "bookkeeper"}})
	var r banking.Rule
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		r, err = banking.CreateRule(ctx, u, banking.RuleRequest{Name: name, Position: 1, Active: true, At: e.clock.Now(),
			When: banking.Conditions{DescriptionContains: "aws"}, Then: banking.Action{AccountID: e.books.Rent.ID, GSTCode: "TX"}})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return r.BankRule
}

// rule is a rule as it is stored now.
func (e *env) rule(id uuid.UUID) storage.BankRule {
	e.t.Helper()
	var r storage.BankRule
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		r, err = u.BankMatching().Rule(ctx, id)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return r
}

// FR-BNK-14 and FR-APR-01 (p1-bank-import-reconcile task 5.2, design D8): a bank rule applies by itself only once
// another person holding A on "Bank transactions, categorise" approves the request, which shows the rule's back-test;
// a second request while one waits is refused, the requester never approves their own (FM-1), and a rule changed
// after the request stops it (FR-APR-10).
// req: FR-BNK-14
// req: FR-APR-01
// req: FM-1
// asvs: V2.3.5
func TestABankRuleAppliesByItselfOnlyOnceApproved(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Accountant, authz.Controller)
	weiling := f.as(f.weiling, authz.Accountant, authz.Controller)
	r := e.bankRule("AWS")
	path := "/v1/bank-rules/" + r.ID.String() + "/auto-apply-approval"

	e.problemIs(e.call(http.MethodPost, path, ahkow, nil, "If-Match", `"0"`), "requestBankRuleAutoApply", 412, CodeStale)
	asked := e.call(http.MethodPost, path, ahkow, nil, "If-Match", ruleETag(r))
	if asked.Status != 202 || asked.str("object_type") != approval.ObjectBankRule || asked.str("operation") != approval.OpRuleAutoApply ||
		!strings.Contains(asked.str("summary"), `Let bank rule "AWS" apply by itself: it would have matched 0 of 0 lines, 0 since it was made`) {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	e.problemIs(e.call(http.MethodPost, path, ahkow, nil, "If-Match", ruleETag(r)), "requestBankRuleAutoApply", 409, approval.CodePending)
	if e.rule(r.ID).AutoApply {
		t.Fatal("the rule applies by itself before its approval")
	}
	e.problemIs(f.decide(ahkow, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
	if a := f.decide(weiling, asked.str("id"), "approve", hashOf(asked)); a.Status != 200 || a.str("status") != "approved" {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}
	if got := e.rule(r.ID); !got.AutoApply || got.AutoApplyApprovalID == nil || got.AutoApplyApprovalID.String() != asked.str("id") {
		t.Fatalf("the approved rule: %+v", got)
	}
	e.problemIs(e.call(http.MethodPost, path, ahkow, nil, "If-Match", ruleETag(e.rule(r.ID))), "requestBankRuleAutoApply", 409, banking.CodeAutoOn)

	// A rule changed after its request: the approval executes nothing.
	other := e.bankRule("AWS EMEA")
	asked = e.call(http.MethodPost, "/v1/bank-rules/"+other.ID.String()+"/auto-apply-approval", ahkow, nil, "If-Match", ruleETag(other))
	if asked.Status != 202 {
		t.Fatalf("asking for the second rule: %d %s", asked.Status, asked.Raw)
	}
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "bookkeeper"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := banking.ChangeRule(ctx, u, other.ID, other.Version, banking.RuleRequest{Name: "AWS EMEA", Position: 2, Active: true, At: e.clock.Now(),
			When: banking.Conditions{DescriptionContains: "aws emea"}, Then: banking.Action{AccountID: e.books.Rent.ID, GSTCode: "TX"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.problemIs(f.decide(weiling, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 409, approval.CodeChanged)
	if e.rule(other.ID).AutoApply {
		t.Error("a changed rule applies by itself")
	}
	e.problemIs(e.call(http.MethodPost, "/v1/bank-rules/"+uuid.NewString()+"/auto-apply-approval", ahkow, nil, "If-Match", `"1"`),
		"requestBankRuleAutoApply", 404, CodeNotFound)
}
