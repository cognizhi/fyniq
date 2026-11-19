package api

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/assistant"
)

// Codes of the assistant's refusals (NM-AI-04).
const (
	CodeAINever    = assistant.CodeNever
	CodeAIInvalid  = assistant.CodeInvalid
	CodeAIOff      = assistant.CodeOff
	CodeAIConflict = assistant.CodeConflict
)

// AITask is a task type of the assistant's catalogue as the settings page lists it.
type AITask struct {
	ID            string   `json:"id" doc:"The task type." example:"people.draft_letter"`
	What          string   `json:"what" doc:"What it proposes." example:"A letter from a template for a request, with the fields filled from the record"`
	Tier          int      `json:"tier" doc:"Tier 1: routine and reversible." example:"1"`
	ApproverRoles []string `json:"approver_roles" doc:"The roles that decide a proposal of it."`
	ExpiryHours   int      `json:"expiry_hours" doc:"How long a proposal waits for a human." example:"72"`
}

// AISettings are an organisation's assistant settings.
type AISettings struct {
	Enabled    bool     `json:"enabled" doc:"Whether the assistant is on: off until the Owner turns it on." example:"false"`
	Route      string   `json:"route" enum:"none,scripted" doc:"The model route: none, or local mode's scripted model. A real route needs processing evidenced in Singapore." example:"none"`
	TaskTypes  []string `json:"task_types" doc:"The task types the Owner turned on; each is off until then."`
	OwnPay     bool     `json:"own_pay" doc:"Whether own-pay explanations are on. They need the Owner and a second approver, which arrives with the Fyniq link: always false until then." example:"false"`
	OwnPayNote string   `json:"own_pay_note" doc:"Why own pay is off." example:"Needs the Owner and a second approver."`
	Catalogue  []AITask `json:"catalogue" doc:"Every task type the assistant may propose."`
	UpdatedAt  string   `json:"updated_at,omitempty" format:"date-time" doc:"When the Owner last changed them." example:"2026-11-20T10:00:00Z"`
}

// AISettingsInput changes the settings.
type AISettingsInput struct {
	Enabled   bool     `json:"enabled" doc:"Turn the assistant on or off." example:"true"`
	Route     string   `json:"route" enum:"none,scripted" doc:"The model route." example:"scripted"`
	TaskTypes []string `json:"task_types" doc:"The task types to turn on." example:"people.draft_letter"`
	OwnPay    bool     `json:"own_pay,omitempty" doc:"Refused until a second approver exists." example:"false"`
}

// AIProposal is a proposal waiting for, or decided by, a human.
type AIProposal struct {
	ID             string         `json:"id" format:"uuid" doc:"The proposal." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f80"`
	Task           string         `json:"task_type" doc:"What kind of draft." example:"people.draft_letter"`
	What           string         `json:"what" doc:"What it proposes." example:"A letter from a template for a request, with the fields filled from the record"`
	Asker          string         `json:"asker" doc:"The person the assistant acted for." example:"Ana Tan"`
	AskerID        string         `json:"asker_id" format:"uuid" doc:"Their login, so a page does not offer them a decision they cannot make." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45"`
	Payload        map[string]any `json:"payload" doc:"What the assistant proposed."`
	Final          map[string]any `json:"final_payload,omitempty" doc:"What the human approved, after editing."`
	Rationale      string         `json:"rationale,omitempty" doc:"Why, in the assistant's words." example:"Filled from the record."`
	ModelID        string         `json:"model_id" doc:"The route that made it." example:"scripted"`
	State          string         `json:"state" enum:"pending,approved,rejected,applied" doc:"Where it is." example:"pending"`
	Expired        bool           `json:"expired" doc:"True when it waited more than 72 hours without a decision: it cannot be approved." example:"false"`
	CreatedAt      string         `json:"created_at" format:"date-time" doc:"When it was proposed." example:"2026-11-20T10:00:00Z"`
	ExpiresAt      string         `json:"expires_at" format:"date-time" doc:"When it expires." example:"2026-11-23T10:00:00Z"`
	DecidedBy      string         `json:"decided_by,omitempty" doc:"The human who decided it." example:"Mei Tan"`
	DecisionReason string         `json:"decision_reason,omitempty" doc:"Their reason." example:"Not needed."`
}

// AIDecision is a human's decision on a proposal.
type AIDecision struct {
	Approve bool           `json:"approve" doc:"Approve the draft, or not." example:"true"`
	Reason  string         `json:"reason,omitempty" maxLength:"500" doc:"Required to reject." example:"Not needed."`
	Final   map[string]any `json:"final_payload,omitempty" doc:"The text to approve, after editing: it must fit the task's shape. Left out, the assistant's own text is approved."`
}

// AISegment is a sentence of an answer with the sources it rests on.
type AISegment struct {
	Text    string   `json:"text" doc:"A sentence. Every figure in it came from a tool in this turn; one that did not is replaced by 'an unverified figure'." example:"You have 8.50 days left."`
	Sources []string `json:"sources" doc:"Where it comes from: your own record, a pack version and its official source, or a rule."`
}

// AIAnswer is the assistant's answer to a question.
type AIAnswer struct {
	Segments []AISegment `json:"segments" doc:"The answer, sentence by sentence."`
	Tools    []string    `json:"tools" doc:"The read tools that ran, all as the person asking."`
	Refused  bool        `json:"refused" doc:"True when the question asks for something the assistant never does: it approves, changes, issues, pays and reads another person's records never." example:"false"`
	Dropped  int         `json:"dropped" doc:"How many tools the model named that were refused." example:"0"`
}

// Asker answers a person's question with the assistant's read tools, as that person.
type Asker interface {
	Ask(ctx context.Context, s Session, question string) (AIAnswer, error)
}

// AIDraftInput asks the assistant to draft something for a request.
type AIDraftInput struct {
	Task      string `json:"task_type" enum:"people.draft_letter,people.draft_fwa_reply" doc:"What to draft: a letter for a letter request, or a reply to a flexible-work request." example:"people.draft_letter"`
	RequestID string `json:"request_id" format:"uuid" doc:"The request the draft is for: still open." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f90"`
}

// AIDraftResult is the proposal a draft request made.
type AIDraftResult struct {
	ProposalID string `json:"proposal_id" format:"uuid" doc:"The proposal, waiting for a person to decide it." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f80"`
}

// AIDraft is a draft attached to a request after its proposal was approved.
type AIDraft struct {
	ID         string         `json:"id" format:"uuid" doc:"The draft." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1fa0"`
	ProposalID string         `json:"proposal_id" format:"uuid" doc:"The proposal it came from." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f80"`
	Kind       string         `json:"kind" enum:"letter,flex_reply" doc:"A letter's fields or a reply's words." example:"letter"`
	Payload    map[string]any `json:"payload" doc:"The approved text, exactly as approved."`
	AppliedAt  string         `json:"applied_at" format:"date-time" doc:"When it was attached." example:"2026-11-20T10:00:00Z"`
}

// AssistantAdmin is the assistant's settings and proposals behind the API.
type AssistantAdmin interface {
	Settings(ctx context.Context, s Session) (AISettings, error)
	SetSettings(ctx context.Context, s Session, in AISettingsInput) error
	Proposals(ctx context.Context, s Session, state string) ([]AIProposal, error)
	Proposal(ctx context.Context, s Session, id uuid.UUID) (AIProposal, error)
	Decide(ctx context.Context, s Session, id uuid.UUID, in AIDecision) error
	Draft(ctx context.Context, s Session, in AIDraftInput) (AIDraftResult, error)
	Drafts(ctx context.Context, s Session, request uuid.UUID) ([]AIDraft, error)
}

func assistantError(err error) error {
	var r *assistant.Refusal
	if errors.As(err, &r) {
		return Err(r.Code, r.Message)
	}
	return huma.Error500InternalServerError("The assistant could not be handled.")
}

type aiSettingsOutput struct{ Body AISettings }

type aiAskInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Question string `json:"question" minLength:"1" maxLength:"1000" doc:"What to ask: about your own leave balance, your pay day, a rule of the Singapore pack or a line of your payslip." example:"How much leave do I have left?"`
	}
}

type aiAskOutput struct{ Body AIAnswer }

type aiSettingsPut struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   AISettingsInput
}

type aiProposalsInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	State  string `query:"state" required:"false" enum:"pending,approved,rejected,applied" doc:"Only proposals in this state; all when left out." example:"pending"`
}

type aiProposalsOutput struct {
	Body struct {
		Proposals []AIProposal `json:"proposals" doc:"Newest first, up to 200."`
	}
}

type aiProposalInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The proposal." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f80"`
}

type aiProposalOutput struct{ Body AIProposal }

type aiDraftPost struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   AIDraftInput
}

type aiDraftOutput struct{ Body AIDraftResult }

type aiDraftsInput struct {
	Cookie    string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	RequestID string `query:"request_id" format:"uuid" doc:"The request." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f90"`
}

type aiDraftsOutput struct {
	Body struct {
		Drafts []AIDraft `json:"drafts" doc:"The drafts attached to the request, newest first."`
	}
}

type aiDecisionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The proposal." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f80"`
	Body   AIDecision
}

func (a *API) registerAssistant() {
	IdempotencyExempt["POST /v1/assistant/proposals/{id}/decision"] = "a proposal is decided once: a repeat is refused (NM_AI_CONFLICT)"
	IdempotencyExempt["POST /v1/assistant/drafts"] = "each request for a draft is a proposal of its own, which a person decides or lets expire"
	IdempotencyExempt["POST /v1/assistant/ask"] = "a question changes nothing"
	IdempotencyExempt["PUT /v1/assistant/settings"] = "a put of the same settings is the same settings"
	team := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return s, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return s, err
		}
		if !p.Team {
			return s, Err(CodeRoleRequired, "The HR officer, the HR administrator and the Owner see and decide the assistant's drafts.")
		}
		return s, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "askAssistant", Method: "POST", Path: "/v1/assistant/ask", Tags: []string{"Assistant"}, Summary: "Ask the people assistant",
		Description: "Answers a question about the signed-in person's own leave balance, their pay day, a rule of the Singapore pack (cited, with a note to confirm it with HR when it has not been verified a second time) or what a line of their payslip is, as a rule with no figure. The read tools run as the person asking, so the assistant reads what they can and nothing else; no pay figure and no sealed field reaches a model, and every figure of the answer came from a tool in this turn. A question that asks it to approve, change, issue, pay or read another person's records is refused in words. The assistant must be on. The question is not stored; the audit event names the tools that ran. It changes nothing.", Errors: []int{401, 409, 422, 503},
	}, func(ctx context.Context, in *aiAskInput) (*aiAskOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		ans, err := a.d.Asker.Ask(ctx, s, in.Body.Question)
		if err != nil {
			if errors.Is(err, ErrUnavailable) {
				return nil, Err(CodeUnavailable, "The assistant is not available in this deployment yet.")
			}
			return nil, assistantError(err)
		}
		return &aiAskOutput{Body: ans}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "draftWithAssistant", Method: "POST", Path: "/v1/assistant/drafts", Tags: []string{"Assistant"}, Summary: "Ask the assistant to draft a letter or a reply",
		Description: "Makes a proposal: a draft letter for an open letter request (its fields filled from the values the request holds from the person's record, its purpose quoted) or a draft reply to an open flexible-work request (an acknowledgement that states no decision and no reason). The request's own words are data, never instructions. The assistant and the task type must be on. The proposal waits for a person to approve it within 72 hours; nothing is checked, issued, signed or replied to. The HR officer, the HR administrator and the Owner. It is audited.", Errors: []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *aiDraftPost) (*aiDraftOutput, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		r, err := a.d.Assistant.Draft(ctx, s, in.Body)
		if err != nil {
			return nil, assistantError(err)
		}
		return &aiDraftOutput{Body: r}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listAssistantDrafts", Method: "GET", Path: "/v1/assistant/drafts", Tags: []string{"Assistant"}, Summary: "The drafts attached to a request",
		Description: "The approved drafts the executor attached to a letter request or a flexible-work request, for the person who decides it to use. The HR officer, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 422},
	}, func(ctx context.Context, in *aiDraftsInput) (*aiDraftsOutput, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.RequestID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such request.")
		}
		list, err := a.d.Assistant.Drafts(ctx, s, id)
		if err != nil {
			return nil, assistantError(err)
		}
		out := &aiDraftsOutput{}
		out.Body.Drafts = list
		if list == nil {
			out.Body.Drafts = []AIDraft{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getAssistantSettings", Method: "GET", Path: "/v1/assistant/settings", Tags: []string{"Assistant"}, Summary: "The people assistant's settings",
		Description: "Whether the assistant is on, its model route, the task types turned on and the catalogue of what it may propose. The assistant is off, with no route and no task type, until the Owner turns it on. Own-pay explanations are always off until a second approver exists. The HR officer, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403},
	}, func(ctx context.Context, in *letterListInput) (*aiSettingsOutput, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		v, err := a.d.Assistant.Settings(ctx, s)
		if err != nil {
			return nil, assistantError(err)
		}
		return &aiSettingsOutput{Body: v}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "putAssistantSettings", Method: "PUT", Path: "/v1/assistant/settings", Tags: []string{"Assistant"}, Summary: "Turn the people assistant on or off",
		Description: "Saves the settings. Only the Owner, after a passkey confirmation in the last five minutes. A task type outside the catalogue, own-pay explanations (they need a second approver) and a route this build does not have are refused; turning the assistant on needs a route. It is audited.", Errors: []int{401, 403, 422}, DefaultStatus: 204,
	}, func(ctx context.Context, in *aiSettingsPut) (*struct{}, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return nil, err
		}
		if !p.Sign {
			return nil, Err(CodeRoleRequired, "Only the Owner turns the assistant on or off.")
		}
		if !s.Fresh(a.d.Clock.Now()) {
			return nil, Err(CodeStepUp, "Confirm it is you with your passkey, then try again: the assistant reads personal data.")
		}
		if err := a.d.Assistant.SetSettings(ctx, s, in.Body); err != nil {
			return nil, assistantError(err)
		}
		return nil, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listAssistantProposals", Method: "GET", Path: "/v1/assistant/proposals", Tags: []string{"Assistant"}, Summary: "The assistant's proposals",
		Description: "The drafts the assistant proposed, newest first, with what it proposed, who it acted for and where each is. The HR officer, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403},
	}, func(ctx context.Context, in *aiProposalsInput) (*aiProposalsOutput, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Assistant.Proposals(ctx, s, in.State)
		if err != nil {
			return nil, assistantError(err)
		}
		out := &aiProposalsOutput{}
		out.Body.Proposals = list
		if list == nil {
			out.Body.Proposals = []AIProposal{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getAssistantProposal", Method: "GET", Path: "/v1/assistant/proposals/{id}", Tags: []string{"Assistant"}, Summary: "One proposal",
		Description: "One draft with its text. The HR officer, the HR administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 404},
	}, func(ctx context.Context, in *aiProposalInput) (*aiProposalOutput, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such proposal.")
		}
		p, err := a.d.Assistant.Proposal(ctx, s, id)
		if err != nil {
			return nil, assistantError(err)
		}
		return &aiProposalOutput{Body: p}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decideAssistantProposal", Method: "POST", Path: "/v1/assistant/proposals/{id}/decision", Tags: []string{"Assistant"}, Summary: "Approve or reject a proposal",
		Description: "A named human approves or rejects a pending proposal; a tier 1 draft may be decided by the person who asked for it. An approval stores the final text the human saw and edited, checked against the task's shape, with its SHA-256; only that text is applied, and only after approval. A proposal that waited more than 72 hours cannot be approved. A rejection needs a reason. It is audited with the hashes, never the text.", Errors: []int{401, 403, 404, 409, 422}, DefaultStatus: 204,
	}, func(ctx context.Context, in *aiDecisionInput) (*struct{}, error) {
		s, err := team(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such proposal.")
		}
		if err := a.d.Assistant.Decide(ctx, s, id, in.Body); err != nil {
			return nil, assistantError(err)
		}
		return nil, nil
	})
}
