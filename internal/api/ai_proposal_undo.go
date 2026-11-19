package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// Undo and approval quality (PRD-001 AI-31, AI-91, AI-92, CTL-AI-07; p1-ai-proposals design D4 and D6).

// The codes of an undo's refusals.
const (
	CodeUndoNotExecuted  = "AI-UNDO-01" // the proposal did not execute, so there is nothing to undo
	CodeUndoNotUndoable  = "AI-UNDO-02" // its task type's effect has no safe reversal: a draft, or a message sent
	CodeUndoAlready      = "AI-UNDO-03" // it was undone already
	CodeUndoMoved        = "AI-UNDO-04" // a record moved since the execution, or its period is closed
	CodeQualityNotReader = "AI-QUALITY-01"
	CodeQualityMonth     = "AI-QUALITY-02"
)

// AIUndoRequest says why an executed proposal is undone.
type AIUndoRequest struct {
	ProposalID string `json:"proposal_id" format:"uuid" doc:"The executed proposal to undo." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0d"`
	Reason     string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is undone: the audit trail and the undo's record keep it." example:"The supplier credited the bill"`
}

// AIUndone is the record of an undo.
type AIUndone struct {
	ProposalID string          `json:"proposal_id" format:"uuid" doc:"The proposal, which stays Executed." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0d"`
	UndoneBy   string          `json:"undone_by" format:"uuid" doc:"Who undid it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	UndoneAt   time.Time       `json:"undone_at" doc:"When." example:"2026-11-20T09:00:00Z"`
	Reason     string          `json:"reason" doc:"Why." example:"The supplier credited the bill"`
	Reversed   json.RawMessage `json:"reversed" doc:"What was reversed: the records and entries the command reports." example:"{\"records\":[\"bill:0193a3c2\"],\"entries\":[]}"`
}

type aiUndoInput struct {
	KeyHeader
	PreferHeader
	Body AIUndoRequest
}

type aiUndoOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     AIUndone
}

// QualityMonth is a month's approval-quality metrics and, when signed, its review.
type QualityMonth struct {
	Month    string           `json:"month" example:"2026-11" doc:"The month, YYYY-MM."`
	Metrics  proposal.Quality `json:"metrics" doc:"Computed from the month's proposals' own records (AI-92)."`
	SignedBy string           `json:"signed_by,omitempty" format:"uuid" doc:"Who signed the month's review." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	SignedAt *time.Time       `json:"signed_at,omitempty" doc:"When." example:"2026-12-01T09:00:00Z"`
	Note     string           `json:"note,omitempty" doc:"The reviewer's note." example:"Two stale proposals followed up"`
}

type (
	// QualityMonthQuery names a month.
	QualityMonthQuery struct {
		Month string `query:"month" required:"true" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month, YYYY-MM." example:"2026-11"`
	}
	qualityGetOutput struct{ Body QualityMonth }
	qualitySignInput struct {
		KeyHeader
		PreferHeader
		Body struct {
			Month string `json:"month" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month reviewed, YYYY-MM." example:"2026-11"`
			Note  string `json:"note,omitempty" maxLength:"1000" doc:"What the reviewer concluded." example:"Two stale proposals followed up"`
		}
	}
	qualitySignOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     QualityMonth
	}
)

func monthOf(s string) (time.Time, error) {
	m, err := time.Parse("2006-01", s)
	if err != nil {
		return time.Time{}, problem(CodeQualityMonth, "The month is not YYYY-MM.", "Send a month such as 2026-11")
	}
	return m, nil
}

// qualityReader is whether p may read approval quality: the Owner and the DPO, as the AI activity log (rev 0.276).
func qualityReader(p authn.Principal) error {
	if p.AI() || p.UserID == uuid.Nil || (!slices.Contains(p.Roles, authz.Owner) && !slices.Contains(p.Roles, authz.DPO)) {
		return problem(CodeQualityNotReader, "Only the Owner and the DPO read the approval-quality metrics.", "Ask the Owner or the DPO")
	}
	return nil
}

func (a *API) qualityOf(ctx context.Context, u storage.UnitOfWork, month time.Time) (QualityMonth, error) {
	rows, err := u.AIProposalTrail().QualityRows(ctx, month, month.AddDate(0, 1, 0))
	if err != nil {
		return QualityMonth{}, err
	}
	out := QualityMonth{Month: month.Format("2006-01"), Metrics: proposal.MeasureQuality(rows)}
	if rv, ok, err := u.AIProposalTrail().ReviewOf(ctx, month); err != nil {
		return out, err
	} else if ok {
		at := rv.SignedAt.UTC()
		out.SignedBy, out.SignedAt, out.Note = rv.SignedBy.String(), &at, rv.Note
	}
	return out, nil
}

// markViewed records that p opened proposal id's review, the first time only (AI-92).
func (a *API) markViewed(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) error {
	if p.AI() || p.UserID == uuid.Nil {
		return nil
	}
	return u.AIProposalTrail().Viewed(ctx, id, p.UserID, a.deps.Clock.Now())
}

func registerProposalUndoAndQuality(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.UndoAIProposal, huma.Operation{
		OperationID: "undoAIProposal", Method: http.MethodPost, Path: "/v1/ai/proposal-undos", Tags: []string{"AI"}, DefaultStatus: http.StatusCreated,
		Summary: "Undo an executed AI proposal",
		Description: "Undoes an executed proposal in one action (AI-31, AI-91): as the caller, who holds the right its command needs, through the domain's own " +
			"reversal in one transaction. An entry it posted is reversed by a reversing entry, never updated or deleted; master data it changed returns to its " +
			"previous version; each carries its own approval policy as when a person does it, so the undo needs the approval the original did. Refused unless " +
			"the proposal is Executed (AI-UNDO-01), its task type can be undone (a draft posts nothing and is never deleted, a message sent cannot be unsent: " +
			"AI-UNDO-02), it was not undone already (AI-UNDO-03) and its records are as the execution left them with their period open (AI-UNDO-04). The undo " +
			"is kept in an append-only record and audited; the proposal stays Executed. On the never-list for the AI and MCP (§9.6). Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeAIPersonOnly, CodeForbidden, CodeUndoNotExecuted, CodeUndoNotUndoable, CodeUndoAlready, CodeUndoMoved, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid}, locks...), func(ctx context.Context, in *aiUndoInput) (*aiUndoOutput, error) {
		p, _ := authn.From(ctx)
		if err := personOnly(p); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.Body.ProposalID)
		if err != nil {
			return nil, notFoundProposal()
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AIUndone, error) {
			pr, err := u.AIProposals().Get(ctx, id)
			if errors.Is(err, ports.ErrNotFound) {
				return AIUndone{}, notFoundProposal()
			}
			if err != nil {
				return AIUndone{}, err
			}
			if ok, err := a.seesAIProposal(ctx, u, p, pr); err != nil {
				return AIUndone{}, err
			} else if !ok {
				return AIUndone{}, notFoundProposal()
			}
			tt, found := a.catalogue().Get(pr.TaskType)
			cmd, built := a.commands()[tt.Command]
			if !found || !built {
				return AIUndone{}, problem(CodeUndoNotUndoable, pr.TaskType+" is not built in this release.", "Ask for it to be undone by hand")
			}
			// The person must hold the right its command needs, as they would to do it themselves (AI-10, AI-91).
			if !authz.Decide(cmd.Policy(), p.Roles, false).Allowed {
				return AIUndone{}, problem(CodeForbidden, "You do not hold the right "+pr.TaskType+" needs, so you cannot undo it.", "Ask someone who does")
			}
			if pr.Status != storage.ProposalExecuted {
				return AIUndone{}, problem(CodeUndoNotExecuted, "Only an executed proposal is undone, and this one is "+pr.Status+".", "A proposal that did not execute changed nothing")
			}
			undoer, ok := cmd.(proposal.Undoer)
			if !ok {
				return AIUndone{}, problem(CodeUndoNotUndoable, tt.Name+" cannot be undone: it posts nothing, or cannot be reversed safely.",
					"A draft is edited or ignored by a person; correct posted work with the record's own void or edit")
			}
			if _, done, err := u.AIProposalTrail().UndoOf(ctx, id); err != nil {
				return AIUndone{}, err
			} else if done {
				return AIUndone{}, problem(CodeUndoAlready, "It was undone already.", "Nothing more to do")
			}
			var done proposal.Execution
			if err := json.Unmarshal(pr.Execution, &done); err != nil {
				return AIUndone{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return AIUndone{}, err
			}
			reversed, err := undoer.Undo(ctx, u, done, proposal.Actor{User: p.UserID, Requester: pr.RequestedBy, Roles: p.Roles, Today: today, Proposal: pr.ID})
			if errors.Is(err, proposal.ErrStale) {
				return AIUndone{}, problem(CodeUndoMoved, "A record the proposal changed has changed since, so it is not undone.", "Correct it by hand")
			}
			if err != nil {
				return AIUndone{}, err
			}
			raw, err := json.Marshal(reversed)
			if err != nil {
				return AIUndone{}, err
			}
			rec, err := u.AIProposalTrail().Undo(ctx, id, p.UserID, a.deps.Clock.Now(), strings.TrimSpace(in.Body.Reason), raw)
			if err != nil {
				return AIUndone{}, err
			}
			return AIUndone{ProposalID: id.String(), UndoneBy: rec.UndoneBy.String(), UndoneAt: rec.UndoneAt.UTC(), Reason: rec.Reason, Reversed: rec.Reversed}, nil
		})
		if err != nil {
			return nil, err
		}
		return &aiUndoOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.GetAIApprovalQuality, exempt(huma.Operation{
		OperationID: "getAIApprovalQuality", Method: http.MethodGet, Path: "/v1/ai/approval-quality", Tags: []string{"AI"},
		Summary: "Read a month's approval-quality metrics",
		Description: "How the month's AI proposals were decided (CTL-AI-07, AI-92): those made, approved, rejected, expired, invalid and stale, the share approved within " +
			"10 seconds of the decider first opening the review with no item changed (a sign of rubber-stamping), the median time to a decision, the approvals per " +
			"approver, the share of rejections that gave a reason and each task type's share, with the month's signed review when there is one. For the Owner and " +
			"the DPO. Computed from the proposals' own records; no side effects. On the never-list for the AI and MCP (§9.6).",
	}, "cursor", "It is one month's figures, not a list, so there is nothing to page through or resume."), append([]string{CodeAIPersonOnly, CodeQualityNotReader, CodeQualityMonth}, locks...), func(ctx context.Context, in *QualityMonthQuery) (*qualityGetOutput, error) {
		p, _ := authn.From(ctx)
		if err := personOnly(p); err != nil {
			return nil, err
		}
		if err := qualityReader(p); err != nil {
			return nil, err
		}
		month, err := monthOf(in.Month)
		if err != nil {
			return nil, err
		}
		var out QualityMonth
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = a.qualityOf(ctx, u, month)
			return err
		})
		return &qualityGetOutput{Body: out}, err
	})

	register(a, authz.SignAIApprovalReview, huma.Operation{
		OperationID: "signAIApprovalReview", Method: http.MethodPost, Path: "/v1/ai/approval-reviews", Tags: []string{"AI"}, DefaultStatus: http.StatusCreated,
		Summary: "Sign a month's approval-quality review",
		Description: "The monthly review of how AI proposals were decided (CTL-AI-07): the Owner or the DPO signs it after step-up, and the metrics as they stand are kept with " +
			"the signature as evidence. A month is signed once, and only after it has ended. Side effects: the review and its audit event. On the never-list for the AI " +
			"and MCP (§9.6). Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeAIPersonOnly, CodeQualityNotReader, CodeQualityMonth, CodeStepUpRequired, storage.CodeVersion, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, locks...),
		func(ctx context.Context, in *qualitySignInput) (*qualitySignOutput, error) {
			p, _ := authn.From(ctx)
			if err := personOnly(p); err != nil {
				return nil, err
			}
			if err := qualityReader(p); err != nil {
				return nil, err
			}
			if !p.SteppedUp(a.deps.Clock.Now()) {
				return nil, problem(CodeStepUpRequired, "Signing the review needs step-up authentication in the last 5 minutes.", "Confirm it's you with your passkey or authenticator app, then try again.")
			}
			month, err := monthOf(in.Body.Month)
			if err != nil {
				return nil, err
			}
			if a.deps.Clock.Now().Before(month.AddDate(0, 1, 0)) {
				return nil, problem(CodeQualityMonth, "A month is reviewed after it ends.", "Sign it from the first day of the next month")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (QualityMonth, error) {
				cur, err := a.qualityOf(ctx, u, month)
				if err != nil {
					return QualityMonth{}, err
				}
				raw, err := json.Marshal(cur.Metrics)
				if err != nil {
					return QualityMonth{}, err
				}
				if _, err := u.AIProposalTrail().Review(ctx, month, p.UserID, a.deps.Clock.Now(), raw, strings.TrimSpace(in.Body.Note)); err != nil {
					return QualityMonth{}, err
				}
				return a.qualityOf(ctx, u, month)
			})
			if err != nil {
				return nil, err
			}
			return &qualitySignOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
