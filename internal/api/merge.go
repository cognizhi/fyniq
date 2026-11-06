package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ account merges (FR-COA-04, Appendix B.8)

// AccountMergeRequest is a merge of an account into another to ask for (FR-COA-04, PRD-001 rev 0.174).
type AccountMergeRequest struct {
	IntoAccountID string `json:"into_account_id" format:"uuid" doc:"The account it is merged into: an active account of the same type, held in the same currency (or in none, as it is), that is not a group, a system account, a money account or a control account; one marked revalue when it is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d62"`
	Date          string `json:"date" format:"date" doc:"The day its balance moves: in an open period, on or after its last posting." example:"2026-11-30"`
}

type mergeAccountInput struct {
	AccountPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body AccountMergeRequest
}

// MergeCodes are the codes a merge, or its approval, may answer (FR-COA-04).
func MergeCodes() []string {
	return []string{journal.CodeMergeAccount, journal.CodeMergeDate, journal.CodeMergeScheduled, ledger.CodeAccountMerge}
}

func registerAccountMerges(a *API) {
	codes := append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeRetry, storage.CodeLockTimeout}, MergeCodes()...)
	register(a, authz.RequestAccountMerge, huma.Operation{
		OperationID: "requestAccountMerge", Method: http.MethodPost, Path: "/v1/accounts/{account_id}/merge-approval",
		Tags: []string{"Chart of accounts"}, Summary: "Ask for approval to merge an account into another", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to merge the account into another (FR-COA-04, FM-1; PRD-001 rev 0.174): the answer is " +
			"202 with an approval request, which a person who holds A on \"Chart of accounts, dimensions\" (the Owner or the Controller) and did " +
			"not ask approves, stepped up. On the approval one entry (account_merge@1, Appendix B.8) moves what the account holds on the date into " +
			"the other account (a balance-sheet account's whole balance, an income or expense account's movement in the financial year), each " +
			"currency and dimension set at its recorded base value, and the account becomes inactive for good, merged into the other; earlier " +
			"postings, closed periods and their snapshots are unchanged. Refused while something scheduled still posts to it (a spread, an asset, " +
			"an automatic reversal). A change to either account or to what it holds before the approval voids it (FR-APR-05). Needs a person " +
			"signed in who passed step-up, and If-Match with the account's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks " +
			"it and asks nothing.",
	}, codes, func(ctx context.Context, in *mergeAccountInput) (*billRequestOutput, error) {
		id, err := parseID("account_id", in.AccountID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		var bad []FieldProblem
		into, err := uuid.Parse(strings.TrimSpace(in.Body.IntoAccountID))
		if err != nil {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.into_account_id", Message: "is not an account ID", Fix: "Send the ID of the account to merge it into"})
		}
		date, err := time.Parse(time.DateOnly, strings.TrimSpace(in.Body.Date))
		if err != nil {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.date", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"})
		}
		if len(bad) > 0 {
			return nil, problem(CodeInvalid, "The merge cannot be asked for: each problem is listed in errors.", "Correct them and ask again.", bad...)
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			cur, err := u.Accounts().Get(ctx, id)
			if err != nil {
				return Approval{}, err
			}
			if err := checkIfMatch(in.IfMatch, etag(cur.Version)); err != nil {
				return Approval{}, err
			}
			plan, err := a.merges.Plan(ctx, u, journal.Merge{From: id, Into: into, Date: date})
			if err != nil {
				return Approval{}, err
			}
			day := date.Format(time.DateOnly)
			c := pendingChange{ObjectType: approval.ObjectAccount, ObjectKey: id.String(), Operation: approval.OpAccountMerge,
				Summary: fmt.Sprintf("%s on %s", plan.Memo(), day),
				Execute: map[string]any{"into_account_id": into.String(), "date": day},
				Diff: []ApprovalDiff{{Field: "active", Before: "true", After: "false"},
					{Field: "merged_into", After: plan.IntoAccount.Code + " " + plan.IntoAccount.Name}}}
			for _, h := range plan.Holdings {
				c.Amounts = append(c.Amounts, approval.Amount{Amount: h.Amount.String(), Currency: h.Amount.Currency().Code})
			}
			return a.requestApproval(ctx, u, c)
		})
		if err != nil {
			return nil, err
		}
		return &billRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}

// mergeOf is the merge a request holds: its account, the account it goes into and the date.
func mergeOf(key string, execute map[string]any) (journal.Merge, error) {
	from, err := uuid.Parse(key)
	if err != nil {
		return journal.Merge{}, err
	}
	into, err := uuid.Parse(fmt.Sprint(execute["into_account_id"]))
	if err != nil {
		return journal.Merge{}, err
	}
	date, err := time.Parse(time.DateOnly, fmt.Sprint(execute["date"]))
	if err != nil {
		return journal.Merge{}, err
	}
	return journal.Merge{From: from, Into: into, Date: date}, nil
}

// mergeDependencies are what a merge's envelope binds (FR-APR-05): the two accounts' versions and what the account
// merged holds now, each currency and dimension set, which is what moves.
func (a *API) mergeDependencies(ctx context.Context, u storage.UnitOfWork, key string, execute map[string]any) (int, map[string]any, error) {
	m, err := mergeOf(key, execute)
	if err != nil {
		return 0, nil, err
	}
	plan, err := a.merges.Plan(ctx, u, m)
	if err != nil {
		return 0, nil, err
	}
	held := make([]string, 0, len(plan.Holdings))
	for _, h := range plan.Holdings {
		held = append(held, fmt.Sprintf("%s %s base %s %v", h.Amount.Currency().Code, h.Amount, h.Base, h.Values))
	}
	return plan.FromAccount.Version, map[string]any{"account_version": strconv.Itoa(plan.FromAccount.Version),
		"into_version": strconv.Itoa(plan.IntoAccount.Version), "holdings": strings.Join(held, "; ")}, nil
}

// executeAccountMerge merges the account as asked, on the approval: its entry posted and the account merged for good.
func (a *API) executeAccountMerge(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	m, err := mergeOf(r.ObjectKey, s.Execute)
	if err != nil {
		return err
	}
	_, _, err = a.merges.PostIn(ctx, u, m)
	return err
}
