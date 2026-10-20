package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the review log (FM-3, rev 0.46)

// ReviewLogItem is a high-risk action that executed, with its review once it has one.
type ReviewLogItem struct {
	ID             string     `json:"id" format:"uuid" doc:"The entry's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f50"`
	Kind           string     `json:"kind" enum:"approved_change,founder_bootstrap" doc:"approved_change: a change a second person approved (FM-1); founder_bootstrap: a role change or invitation the Owner made alone before a second member joined (FR-ADM-01)." example:"approved_change"`
	ObjectType     string     `json:"object_type" doc:"What it changed." example:"organisation_setting"`
	ObjectKey      string     `json:"object_key" doc:"Which one." example:"bank.tolerance_base"`
	Summary        string     `json:"summary" doc:"What it did, in a sentence." example:"Change Tolerance for base-currency differences from 1.00 to 5.00 from 2026-10-20"`
	Actor          string     `json:"actor,omitempty" format:"uuid" doc:"Who acted: the requester, or the Owner in the bootstrap." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	ActorName      string     `json:"actor_name,omitempty" doc:"Their name." example:"Mei Ling Tan"`
	ApprovalID     string     `json:"approval_id,omitempty" format:"uuid" doc:"The approved request, for an approved change." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	LoggedAt       time.Time  `json:"logged_at" doc:"When it executed." example:"2026-10-20T09:30:00Z"`
	Overdue        bool       `json:"overdue" doc:"true when it is still unreviewed 14 days after it executed, in Founder mode: Checks shows it as a warning (FM-3)." example:"false"`
	ReviewedBy     string     `json:"reviewed_by,omitempty" format:"uuid" doc:"Who reviewed it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"`
	ReviewedByName string     `json:"reviewed_by_name,omitempty" doc:"Their name." example:"Wei Jie Lim"`
	ReviewedAt     *time.Time `json:"reviewed_at,omitempty" doc:"When." example:"2026-10-24T09:00:00Z"`
	Note           string     `json:"note,omitempty" doc:"What the reviewer noted." example:"Agreed at Monday's meeting"`
	YouMayReview   bool       `json:"you_may_review" doc:"true when you may review it now: it is unreviewed, you did not act on it and you hold the approval right on what it changed." example:"true"`
}

// ReviewLogPage is a page of the review log, oldest first, with counts over the whole log.
type ReviewLogPage struct {
	Items      []ReviewLogItem `json:"items" doc:"The entries on this page."`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
	Unreviewed int             `json:"unreviewed" doc:"How many entries you may see are unreviewed." example:"3"`
	Overdue    int             `json:"overdue" doc:"How many of them are overdue (FM-3)." example:"0"`
}

// EntryRef names an entry of the review log.
type EntryRef string

// Schema implements huma.SchemaProvider: a UUID string, with an example.
func (EntryRef) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "uuid", Description: "An entry's ID.", Examples: []any{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f50"}}
}

// ReviewLogReview reviews entries of the log.
type ReviewLogReview struct {
	EntryIDs []EntryRef `json:"entry_ids" minItems:"1" maxItems:"100" doc:"The entries you reviewed: none you acted on."`
	Note     string     `json:"note,omitempty" maxLength:"500" doc:"What you noted, recorded with each." example:"Agreed at Monday's meeting"`
}

// ReviewLogReviewed is what a review recorded.
type ReviewLogReviewed struct {
	Reviewed []ReviewLogItem `json:"reviewed" doc:"The entries, now reviewed."`
}

type reviewLogListInput struct {
	PageQuery
	Status string `query:"status" enum:"unreviewed,reviewed,all" default:"unreviewed" doc:"The entries to list: unreviewed (the default), reviewed, or all." example:"unreviewed"`
}

type reviewLogReviewInput struct {
	KeyHeader
	PreferHeader
	Body ReviewLogReview
}

type reviewLogListOutput struct{ Body ReviewLogPage }

type reviewLogReviewOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ReviewLogReviewed
}

// ReviewCodes are the review log's rules.
var ReviewCodes = []string{approval.CodeReviewOwn, approval.CodeReviewed}

// mayReview reports whether the person may review e: it is unreviewed, they did not act on it, and they hold the approval
// right on the row of what it changed.
func mayReview(p authn.Principal, e storage.ReviewLogEntry) bool {
	return e.ReviewedBy == nil && p.UserID != uuid.Nil && (e.Actor == nil || *e.Actor != p.UserID) && mayApprove(p, e.ObjectType)
}

// seesEntry reports whether the person may see e: a right on the row of what it changed, or having acted on it.
func seesEntry(p authn.Principal, e storage.ReviewLogEntry) bool {
	if p.AI() {
		return false
	}
	if e.Actor != nil && *e.Actor == p.UserID {
		return true
	}
	return authz.Decide(authz.Policy{Operation: "view", Capability: approvalRow(e.ObjectType), Need: authz.View}, p.Roles, false).Allowed
}

func reviewItemOf(p authn.Principal, e storage.ReviewLogEntry, names map[uuid.UUID]string, founder bool, now time.Time) ReviewLogItem {
	v := ReviewLogItem{ID: e.ID.String(), Kind: e.Kind, ObjectType: e.ObjectType, ObjectKey: e.ObjectKey, Summary: e.Summary,
		LoggedAt: e.LoggedAt.UTC(), Note: e.Note, YouMayReview: mayReview(p, e)}
	if e.Actor != nil {
		v.Actor, v.ActorName = e.Actor.String(), names[*e.Actor]
	}
	if e.ApprovalID != nil {
		v.ApprovalID = e.ApprovalID.String()
	}
	if e.ReviewedBy != nil {
		v.ReviewedBy, v.ReviewedByName = e.ReviewedBy.String(), names[*e.ReviewedBy]
		at := e.ReviewedAt.UTC()
		v.ReviewedAt = &at
	}
	v.Overdue = founder && e.ReviewedBy == nil && now.Sub(e.LoggedAt) > approval.ReviewOverdue
	return v
}

func registerReviewLog(a *API) {
	register(a, authz.ListReviewLog, huma.Operation{
		OperationID: "listReviewLog", Method: http.MethodGet, Path: "/v1/review-log", Tags: []string{"Approvals"},
		Summary: "List the review log",
		Description: "The high-risk actions that executed, oldest first: each approved change (FM-1) and each role change or invitation the " +
			"Owner made alone before a second member joined (FR-ADM-01), with its review once another founder reviewed it (FM-3). In " +
			"Founder mode an entry still unreviewed 14 days after it executed is overdue. Shown to whoever holds a right on what each " +
			"changed, and to the one who acted; never to the AI. Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *reviewLogListInput) (*reviewLogListOutput, error) {
		p, _ := authn.From(ctx)
		now := a.deps.Clock.Now()
		var page ReviewLogPage
		var all []ReviewLogItem
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			mode, _, _, err := teamModeOf(ctx, u)
			if err != nil {
				return err
			}
			es, err := u.ReviewLog().List(ctx)
			if err != nil {
				return err
			}
			names, err := memberNames(ctx, u, now)
			if err != nil {
				return err
			}
			for _, e := range es {
				if !seesEntry(p, e) {
					continue
				}
				v := reviewItemOf(p, e, names, mode == team.Founder, now)
				if v.ReviewedBy == "" {
					page.Unreviewed++
				}
				if v.Overdue {
					page.Overdue++
				}
				if in.Status == "all" || (cmpOr(in.Status, "unreviewed") == "unreviewed") == (v.ReviewedBy == "") {
					all = append(all, v)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if all == nil {
			all = []ReviewLogItem{}
		}
		items, next, err := pageOf(all, func(v ReviewLogItem) string { return v.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		page.Items, page.NextCursor = items, next
		return &reviewLogListOutput{Body: page}, nil
	})

	register(a, authz.ReviewLogEntries, huma.Operation{
		OperationID: "reviewLogEntries", Method: http.MethodPost, Path: "/v1/review-log/reviews", Tags: []string{"Approvals"},
		Summary: "Review high-risk actions",
		Description: "Records your review of entries of the review log (FM-3): each unreviewed, none you acted on, and each one whose " +
			"object you hold the approval right (A) on, such as an Owner. Every entry is checked first, and nothing is recorded unless all " +
			"pass. Needs a person signed in to Fyniq; never the AI. Side effects: a review per entry and its audit event. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeSessionRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, approval.CodeNotApprover, storage.CodeApproval, storage.CodeRetry, storage.CodeLockTimeout}, ReviewCodes...),
		func(ctx context.Context, in *reviewLogReviewInput) (*reviewLogReviewOutput, error) {
			p, _ := authn.From(ctx)
			if p.Via != authn.ViaSession || p.AI() || p.UserID == uuid.Nil {
				return nil, problem(CodeSessionRequired, "Reviewing high-risk actions needs a person with a Fyniq user, signed in to Fyniq.",
					"Sign in to Fyniq in the browser and do it there.")
			}
			var ids []uuid.UUID
			var bad []FieldProblem
			for i, raw := range in.Body.EntryIDs {
				id, err := uuid.Parse(string(raw))
				if err != nil || slices.Contains(ids, id) {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: fmt.Sprintf("body.entry_ids[%d]", i), Message: "is not an entry ID, or is named twice",
						Fix: "Send each entry's id from the review log once"})
					continue
				}
				ids = append(ids, id)
			}
			if len(bad) > 0 {
				return nil, invalid(bad)
			}
			note := strings.TrimSpace(in.Body.Note)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ReviewLogReviewed, error) {
				now := a.deps.Clock.Now()
				es, err := u.ReviewLog().List(ctx)
				if err != nil {
					return ReviewLogReviewed{}, err
				}
				var fps []FieldProblem
				var chosen []storage.ReviewLogEntry
				for i, id := range ids {
					field := fmt.Sprintf("body.entry_ids[%d]", i)
					j := slices.IndexFunc(es, func(e storage.ReviewLogEntry) bool { return e.ID == id })
					switch {
					case j < 0 || !seesEntry(p, es[j]):
						return ReviewLogReviewed{}, problem(CodeNotFound, "No entry of the review log has the ID "+id.String()+".", "List the review log to find the entries' IDs")
					case es[j].ReviewedBy != nil:
						fps = append(fps, FieldProblem{Code: approval.CodeReviewed, Field: field, Message: "was reviewed already", Fix: "Leave it out"})
					case es[j].Actor != nil && *es[j].Actor == p.UserID:
						fps = append(fps, FieldProblem{Code: approval.CodeReviewOwn, Field: field, Message: "is an action you took", Fix: "Ask another founder to review it"})
					case !mayApprove(p, es[j].ObjectType):
						fps = append(fps, FieldProblem{Code: approval.CodeNotApprover, Field: field,
							Message: "changed " + string(approvalRow(es[j].ObjectType)) + ", on which you hold no approval right", Fix: "Ask a member who holds it, such as an Owner"})
					default:
						chosen = append(chosen, es[j])
					}
				}
				if len(fps) > 0 {
					return ReviewLogReviewed{}, problem(fps[0].Code, fmt.Sprintf("%d entry(ies) cannot be reviewed by you; each is listed in errors.", len(fps)),
						"Review only unreviewed entries you did not act on", fps...)
				}
				for _, e := range chosen {
					if err := u.ReviewLog().Review(ctx, e.ID, p.UserID, now, note); err != nil {
						return ReviewLogReviewed{}, err
					}
				}
				es, err = u.ReviewLog().List(ctx)
				if err != nil {
					return ReviewLogReviewed{}, err
				}
				names, err := memberNames(ctx, u, now)
				if err != nil {
					return ReviewLogReviewed{}, err
				}
				mode, _, _, err := teamModeOf(ctx, u)
				if err != nil {
					return ReviewLogReviewed{}, err
				}
				out := ReviewLogReviewed{Reviewed: []ReviewLogItem{}}
				for _, e := range es {
					if slices.Contains(ids, e.ID) {
						out.Reviewed = append(out.Reviewed, reviewItemOf(p, e, names, mode == team.Founder, now))
					}
				}
				return out, nil
			})
			if err != nil {
				return nil, err
			}
			return &reviewLogReviewOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
