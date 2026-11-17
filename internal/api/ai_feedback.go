package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Feedback on the assistant's answers (PRD-001 QA-AI-05; p1-ai-assistant task 6.4, design D13): a person rates an
// answer of their own conversation, up or down, with a reason; an Owner or Administrator reviews it, with the question
// and the answer, and only what they mark golden feeds the eval set. Giving it is on the conversations' row; reviewing
// it is eval work, on the AI providers' row, where model qualification runs the evals. Someone else's turn that read
// payroll shows masked to a reviewer who may not see payroll unmasked (FR-PAY-13), as in the AI activity log, and such
// feedback can only be dismissed: what joins the golden set was read by the person who put it there.

// The codes of feedback's refusals.
const (
	CodeAIFeedbackTurn   = "AI-FEEDBACK-01" // the turn has no answer to rate
	CodeAIFeedbackMasked = "AI-FEEDBACK-02" // feedback masked to the reviewer cannot join the golden set
	CodeAIFeedbackDone   = "AI-FEEDBACK-03" // the feedback was reviewed already
)

// AIFeedbackIn is a thumbs up or down on an answer, with why (QA-AI-05).
type AIFeedbackIn struct {
	Rating string `json:"rating" enum:"up,down" doc:"up: the answer helped; down: it did not." example:"down"`
	Reason string `json:"reason" minLength:"1" maxLength:"1000" doc:"Why, in your words. An Owner or Administrator reads it with the question and the answer when they review it." example:"It used June's figures, not July's."`
}

// AIFeedback is feedback on one of the assistant's answers, and its review.
type AIFeedback struct {
	ID           string       `json:"id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a51"`
	Turn         string       `json:"turn_id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2c"`
	Conversation string       `json:"conversation_id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Person       string       `json:"person" doc:"Who gave it: user:<id>." example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	Rating       string       `json:"rating" enum:"up,down" example:"down"`
	Reason       string       `json:"reason,omitempty" doc:"Why; absent when masked." example:"It used June's figures, not July's."`
	Question     string       `json:"question,omitempty" doc:"What was asked; absent when masked." example:"What was July's revenue?"`
	Answer       string       `json:"answer,omitempty" doc:"The answer it rates, as its person read it; absent when masked." example:"July's revenue was SGD 48,200.00."`
	Classes      []RouteClass `json:"classes" nullable:"false" doc:"The classes of data the turn read (AI-83)."`
	GivenAt      time.Time    `json:"given_at" example:"2026-11-15T08:01:00Z"`
	State        string       `json:"state" enum:"waiting,golden,dismissed" doc:"waiting: for an Owner or Administrator to review; golden: it feeds the eval set; dismissed: it does not." example:"waiting"`
	ReviewedBy   string       `json:"reviewed_by,omitempty" doc:"Who reviewed it: user:<id>." example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2e"`
	ReviewedAt   *time.Time   `json:"reviewed_at,omitempty" example:"2026-11-16T02:00:00Z"`
	Masked       bool         `json:"masked" doc:"Its turn read payroll and you may not see payroll unmasked: its question, answer and reason are withheld (FR-PAY-13), and it can only be dismissed." example:"false"`
}

// AIFeedbackPage is a page of feedback, newest first.
type AIFeedbackPage struct {
	Items      []AIFeedback `json:"items" nullable:"false"`
	NextCursor string       `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYtMTEtMTVUMDg6MDE6MDBafDAxOTMifQ"`
}

// AIFeedbackReviewIn is an Owner's or Administrator's decision on feedback.
type AIFeedbackReviewIn struct {
	Decision string `json:"decision" enum:"golden,dismissed" doc:"golden: it feeds the eval set (QA-AI-05); dismissed: it does not." example:"golden"`
}

// AITurnPath names a turn of one of the caller's conversations.
type AITurnPath struct {
	AIConversationPath
	TurnID string `path:"turn_id" format:"uuid" doc:"The turn whose answer it rates." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2c"`
}

// AIFeedbackPath names feedback.
type AIFeedbackPath struct {
	FeedbackID string `path:"feedback_id" format:"uuid" doc:"The feedback." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a51"`
}

type (
	aiFeedbackGiveInput struct {
		AITurnPath
		KeyHeader
		PreferHeader
		Body AIFeedbackIn
	}
	aiFeedbackGiveOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     AIFeedback
	}
	aiFeedbackListInput struct {
		PageQuery
		State  string `query:"state" enum:"waiting,golden,dismissed" doc:"Only feedback in this state; leave it out for all." example:"waiting"`
		Rating string `query:"rating" enum:"up,down" doc:"Only up or only down." example:"down"`
	}
	aiFeedbackListOutput  struct{ Body AIFeedbackPage }
	aiFeedbackReviewInput struct {
		AIFeedbackPath
		Body AIFeedbackReviewIn
	}
	aiFeedbackOutput struct{ Body AIFeedback }
)

func feedbackOf(e storage.AIFeedbackEntry) AIFeedback {
	out := AIFeedback{ID: e.ID.String(), Turn: e.Turn.String(), Conversation: e.Conversation.String(), Person: "user:" + e.UserID.String(),
		Rating: e.Rating, Reason: e.Reason, Question: e.Question, Answer: e.Answer, Classes: []RouteClass{}, GivenAt: e.GivenAt.UTC(),
		State: storage.FeedbackWaiting, ReviewedAt: e.ReviewedAt}
	for _, c := range e.Classes {
		out.Classes = append(out.Classes, RouteClass(c))
	}
	if e.Decision != "" {
		out.State = e.Decision
	}
	if e.ReviewedBy != nil {
		out.ReviewedBy = "user:" + e.ReviewedBy.String()
	}
	return out
}

// masksFeedback is whether feedback shows masked to the caller: someone else's turn that read payroll, to a reader who
// may not see payroll unmasked (FR-PAY-13).
func masksFeedback(p authn.Principal, e storage.AIFeedbackEntry) bool {
	return e.Asker != p.UserID && slices.Contains(e.Classes, aipolicy.Payroll) && !seesPayroll(p)
}

// readsPayrollOf is whether showing feedback to the caller is a read of someone else's payroll (AUD-05).
func readsPayrollOf(p authn.Principal, e storage.AIFeedbackEntry) bool {
	return e.Asker != p.UserID && slices.Contains(e.Classes, aipolicy.Payroll) && !masksFeedback(p, e)
}

func registerAIFeedback(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GiveAIFeedback, exempt(huma.Operation{
		OperationID: "giveAIFeedback", Method: http.MethodPost, Path: "/v1/assistant/conversations/{conversation_id}/turns/{turn_id}/feedback",
		Tags: []string{"Assistant"}, DefaultStatus: http.StatusCreated, Summary: "Rate an answer of the assistant",
		Description: "A thumbs up or down on an answer in one of the caller's own conversations, with why (QA-AI-05), once per answer. An " +
			"Owner or Administrator reviews it with the question and the answer; only what they mark golden feeds the eval set. A turn " +
			"still answering, or one that failed, has no answer to rate. Side effects: the feedback and its audit event " +
			"ai_feedback.given. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing. On the never-list " +
			"for MCP and agent clients (§9.6).",
	}, "If-Match", "Feedback is given once per answer, and the database refuses a second: there is no version to match."),
		append([]string{CodeNotFound, CodeAIPersonOnly, CodeAIFeedbackTurn, storage.CodeDuplicate, CodeMalformed, CodeTooLarge, CodeMediaType,
			CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, locks...),
		func(ctx context.Context, in *aiFeedbackGiveInput) (*aiFeedbackGiveOutput, error) {
			p, _ := authn.From(ctx)
			if err := personOnly(p); err != nil {
				return nil, err
			}
			turnID, err := parseID("turn_id", in.TurnID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AIFeedback, error) {
				c, err := ownConversation(ctx, u, p, in.ConversationID)
				if err != nil {
					return AIFeedback{}, err
				}
				turns, err := u.AIConversations().Turns(ctx, c.ID)
				if err != nil {
					return AIFeedback{}, err
				}
				i := slices.IndexFunc(turns, func(t storage.AITurn) bool { return t.ID == turnID })
				if i < 0 {
					return AIFeedback{}, ports.ErrNotFound
				}
				t := turns[i]
				if t.Status != storage.TurnAnswered && t.Status != storage.TurnRefused {
					return AIFeedback{}, problem(CodeAIFeedbackTurn, "This turn has no answer to rate: it is "+t.Status+".",
						"Rate it once it is answered, or ask the question again")
				}
				f, err := u.AIConversations().Give(ctx, storage.AIFeedback{Turn: t.ID, UserID: p.UserID, Rating: in.Body.Rating,
					Reason: strings.TrimSpace(in.Body.Reason), GivenAt: a.deps.Clock.Now()})
				if err != nil {
					return AIFeedback{}, err
				}
				return feedbackOf(storage.AIFeedbackEntry{AIFeedback: f, Conversation: c.ID, Asker: t.UserID, Question: t.Question, Answer: t.Answer,
					Classes: t.Classes}), nil
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &aiFeedbackGiveOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &aiFeedbackGiveOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.ListAIFeedback, huma.Operation{
		OperationID: "listAIFeedback", Method: http.MethodGet, Path: "/v1/ai/feedback", Tags: []string{"AI"},
		Summary: "List feedback on the assistant's answers",
		Description: "Feedback on the assistant's answers for an Owner or Administrator to review (QA-AI-05), newest first, a page at a " +
			"time, with the question, the answer, the reason and the classes of data the turn read; filtered by state and rating. " +
			"Golden feedback is what feeds the eval set. Someone else's turn that read payroll shows masked to a reader who may not see " +
			"payroll unmasked (FR-PAY-13). Side effects: showing such feedback unmasked writes the audit event ai_activity.read " +
			"(AUD-05); otherwise none. On the never-list for the AI and MCP (§9.6).",
	}, append([]string{CodeCursor, CodeAIPersonOnly}, locks...), func(ctx context.Context, in *aiFeedbackListInput) (*aiFeedbackListOutput, error) {
		p, _ := authn.From(ctx)
		if err := personOnly(p); err != nil {
			return nil, err
		}
		q := storage.AIFeedbackQuery{State: in.State, Rating: in.Rating, Limit: in.PageSize + 1}
		after, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		if after != "" {
			at, id, ok := strings.Cut(after, "|")
			t, terr := time.Parse(time.RFC3339Nano, at)
			uid, ierr := uuid.Parse(id)
			if !ok || terr != nil || ierr != nil {
				return nil, badCursor()
			}
			q.BeforeAt, q.BeforeID = &t, uid
		}
		out := AIFeedbackPage{Items: []AIFeedback{}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			list, err := u.AIConversations().Feedback(ctx, q)
			if err != nil {
				return err
			}
			if len(list) > in.PageSize {
				list = list[:in.PageSize]
				last := list[len(list)-1]
				out.NextCursor = encodeCursor(last.GivenAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String())
			}
			var read []string
			for _, e := range list {
				out.Items = append(out.Items, a.shownFeedback(p, e, &read))
			}
			return a.auditPayrollRead(ctx, u, read)
		})
		return &aiFeedbackListOutput{Body: out}, err
	})

	register(a, authz.ReviewAIFeedback, exempt(exempt(huma.Operation{
		OperationID: "reviewAIFeedback", Method: http.MethodPost, Path: "/v1/ai/feedback/{feedback_id}/review", Tags: []string{"AI"},
		Summary: "Review feedback on an answer of the assistant",
		Description: "An Owner or Administrator decides feedback waiting (QA-AI-05): golden feeds the eval set, dismissed does not. Feedback " +
			"masked to the reviewer, on someone else's turn that read payroll, can only be dismissed: what joins the golden set was read " +
			"by the person who put it there. Side effects: the decision and its audit event ai_feedback.reviewed; showing payroll " +
			"unmasked also writes ai_activity.read (AUD-05). On the never-list for the AI and MCP (§9.6).",
	}, "Idempotency-Key", "Feedback is reviewed once: a second review is refused as no longer waiting, so the request is idempotent by itself."),
		"Prefer", "A review decides one feedback, with nothing to preview."),
		append([]string{CodeNotFound, CodeAIPersonOnly, CodeAIFeedbackMasked, CodeAIFeedbackDone}, locks...),
		func(ctx context.Context, in *aiFeedbackReviewInput) (*aiFeedbackOutput, error) {
			p, _ := authn.From(ctx)
			if err := personOnly(p); err != nil {
				return nil, err
			}
			id, err := parseID("feedback_id", in.FeedbackID)
			if err != nil {
				return nil, err
			}
			var out AIFeedback
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				e, err := u.AIConversations().FeedbackEntry(ctx, id)
				if err != nil {
					return err
				}
				if e.Decision != "" {
					return reviewedAlready(e.Decision)
				}
				if in.Body.Decision == storage.FeedbackGolden && masksFeedback(p, e) {
					return problem(CodeAIFeedbackMasked, "This feedback's turn read payroll, which you may not see, so it cannot join the golden set.",
						"Dismiss it, or ask someone who sees payroll to review it")
				}
				now := a.deps.Clock.Now()
				if err := u.AIConversations().Review(ctx, id, p.UserID, in.Body.Decision, now); errors.Is(err, ports.ErrNotFound) {
					return reviewedAlready("decided by someone else just now")
				} else if err != nil {
					return err
				}
				e.Decision, e.ReviewedBy, e.ReviewedAt = in.Body.Decision, &p.UserID, &now
				var read []string
				out = a.shownFeedback(p, e, &read)
				return a.auditPayrollRead(ctx, u, read)
			})
			if err != nil {
				return nil, err
			}
			return &aiFeedbackOutput{Body: out}, nil
		})
}

func reviewedAlready(state string) error {
	return problem(CodeAIFeedbackDone, "This feedback was reviewed already: it is "+state+".", "Leave it as it is; feedback is reviewed once")
}

// shownFeedback is feedback as the caller may see it, noting the feedback whose showing reads someone else's payroll.
func (a *API) shownFeedback(p authn.Principal, e storage.AIFeedbackEntry, read *[]string) AIFeedback {
	f := feedbackOf(e)
	switch {
	case masksFeedback(p, e):
		f.Masked, f.Question, f.Answer, f.Reason = true, "", "", ""
	case readsPayrollOf(p, e):
		*read = append(*read, e.ID.String())
	}
	return f
}

// auditPayrollRead records that the caller was shown someone else's feedback on turns that read payroll (AUD-05).
func (a *API) auditPayrollRead(ctx context.Context, u storage.UnitOfWork, read []string) error {
	if len(read) == 0 {
		return nil
	}
	first, _ := uuid.Parse(read[0])
	return u.Audit().Record(ctx, storage.AuditRecord{Action: "ai_activity.read", ObjectType: "ai_feedback", ObjectID: first,
		Changes: map[string][2]any{"feedback": {nil, read}}})
}
