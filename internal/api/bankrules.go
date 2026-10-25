package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Bank rules' auto-apply (FR-BNK-14, FR-APR-01; p1-bank-import-reconcile task 5.2, design D8): a rule reconciles lines
// by itself only once a person holding A on "Bank transactions, categorise" approves it, after its back-test, and no
// one who asked (FM-1). The rest of the banking API comes with task 9.1.

// BankRulePath names a bank rule.
type BankRulePath struct {
	RuleID string `path:"rule_id" format:"uuid" doc:"The rule's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fb1"`
}

type ruleAutoApplyInput struct {
	BankRulePath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

// ruleETag is a rule's version, for If-Match.
func ruleETag(r storage.BankRule) string { return etag(r.Version, r.AutoApply) }

// ruleHash is the SHA-256 of a rule's conditions and action: what its back-test ran on, and what its approver signs.
func ruleHash(r storage.BankRule) string {
	h := sha256.New()
	h.Write(r.Conditions)
	h.Write([]byte{0})
	h.Write(r.Action)
	return hex.EncodeToString(h.Sum(nil))
}

// ruleDependencies are what a bank rule's envelope depends on: its version, and its conditions and action.
func ruleDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]string, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	r, err := u.BankMatching().Rule(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	return r.Version, map[string]string{"rule_version": fmt.Sprint(r.Version), "rule": ruleHash(r)}, nil
}

// executeRuleAutoApply marks the rule auto-apply under the approved request, at the version it was asked for (FY222
// refuses any other request).
func (a *API) executeRuleAutoApply(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	version, err := intIn(s.Execute, "rule_version")
	if err != nil {
		return err
	}
	_, err = u.BankMatching().SetRuleAutoApply(ctx, id, int(version), r.ID, a.deps.Clock.Now())
	return err
}

func registerBankRules(a *API) {
	codes := []string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, banking.CodeAutoOn, banking.CodeRuleOff, banking.CodeRule,
		storage.CodeRetry, storage.CodeLockTimeout, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}
	register(a, authz.RequestBankRuleAutoApply, huma.Operation{
		OperationID: "requestBankRuleAutoApply", Method: http.MethodPost, Path: "/v1/bank-rules/{rule_id}/auto-apply-approval", Tags: []string{"Banking"},
		Summary: "Ask for approval to let a bank rule apply by itself", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to mark a bank rule auto-apply (FR-BNK-14, FR-APR-01): the auto-reconcile job then " +
			"categorises the lines it meets without a click, within the organisation's caps. The request shows the rule's back-test, how many " +
			"of the lines held it would have matched, and how many of those were booked since the rule was made. A person who holds A on " +
			"\"Bank transactions, categorise\" and did not ask approves it, stepped up; a change to the rule meanwhile stops it. Needs a person " +
			"signed in who passed step-up, and If-Match with the rule's ETag. Idempotent with Idempotency-Key.",
	}, codes, func(ctx context.Context, in *ruleAutoApplyInput) (*billRequestOutput, error) {
		id, err := parseID("rule_id", in.RuleID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			stored, err := u.BankMatching().Rule(ctx, id)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, ruleETag(stored)); err != nil {
				return Approval{}, err
			}
			switch {
			case stored.AutoApply:
				return Approval{}, problem(banking.CodeAutoOn, fmt.Sprintf("Rule %q applies by itself already.", stored.Name), "Nothing to ask for")
			case !stored.Active:
				return Approval{}, problem(banking.CodeRuleOff, fmt.Sprintf("Rule %q is switched off.", stored.Name), "Switch the rule on first")
			}
			rule, err := banking.ReadRule(stored)
			if err != nil {
				return Approval{}, err
			}
			bt, err := banking.RunBackTest(ctx, u, rule.When)
			if err != nil {
				return Approval{}, err
			}
			since := bt.Since(stored.CreatedAt)
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectBankRule, ObjectKey: id.String(), Operation: approval.OpRuleAutoApply,
				Summary: fmt.Sprintf("Let bank rule %q apply by itself: it would have matched %d of %d lines, %d since it was made", stored.Name, bt.Matched,
					bt.Examined, since),
				Execute: map[string]any{"rule_id": id.String(), "rule_version": stored.Version},
				Diff: []ApprovalDiff{{Field: "auto_apply", Before: "false", After: "true"},
					{Field: "back_test", After: fmt.Sprintf("%d of %d lines", bt.Matched, bt.Examined)},
					{Field: "matched_since_made", After: fmt.Sprint(since)}}})
		})
		if err != nil {
			return nil, err
		}
		return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
