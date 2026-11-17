package api

import (
	"context"
	"encoding/json"
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

// The AI's proposals as people review them (PRD-001 §9.5, AI-07; p1-ai-proposals-core design D5) and the catalogue as
// the AI tasks page lists it (§9.6). Each member sees the proposals made for them and those they may sign; the Owner and
// the DPO see every one. The summary is the platform's, from the payload and the dry run, never the model's; the AI's
// rationale is shown apart, labelled as AI text.

// AIText is text an AI wrote, shown apart and labelled as such (AI-07, FR-UX-17).
type AIText struct {
	Text   string `json:"text" doc:"What the AI wrote: its own words, never the platform's." example:"The AGM is due by 30 June 2027 and has no task yet."`
	Origin string `json:"origin" enum:"ai" doc:"Who wrote it: always the AI, to show with the AI label." example:"ai"`
}

// AIProposalItem is an item of a batch proposal.
type AIProposalItem struct {
	N    int `json:"n" doc:"Its number, from 1, as an approval names it." example:"1"`
	Item any `json:"item" doc:"The item, as the task type's schema describes it." example:"{\"note\":\"Book the room\"}"`
}

// AIProposalSummary is a proposal in a list.
type AIProposalSummary struct {
	ID          string `json:"id" format:"uuid" example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0d"`
	TaskType    string `json:"task_type" example:"sec.create_task"`
	TaskName    string `json:"task_name" doc:"The task type's name in the catalogue." example:"Create a compliance task"`
	Tier        int    `json:"tier" minimum:"1" maximum:"3" doc:"Its risk tier (§9.6)." example:"1"`
	Status      string `json:"status" enum:"draft,invalid,pending,approved,rejected,expired,superseded,executing,executed,stale,failed" example:"pending"`
	Summary     string `json:"summary,omitempty" doc:"What it does, in the platform's words (AI-07)." example:"Creates the task Hold the AGM, due 30 June 2027"`
	Reason      string `json:"reason,omitempty" doc:"Why it is invalid, rejected, stale or failed." example:"Not this year"`
	RequestedBy string `json:"requested_by" format:"uuid" doc:"The person it was made for." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0e"`
	Items       int    `json:"items" doc:"A batch's items; 0 when it is not a batch." example:"0"`
	ApprovalID  string `json:"approval_id,omitempty" format:"uuid" doc:"Its approval request, where a person decides it." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0f"`
	ProposedAt  string `json:"proposed_at" format:"date-time" example:"2026-11-15T09:00:00Z"`
	ExpiresAt   string `json:"expires_at" format:"date-time" example:"2026-11-18T09:00:00Z"`
}

// AIProposalPage is a page of proposals, newest first.
type AIProposalPage struct {
	Items      []AIProposalSummary `json:"items" nullable:"false"`
	NextCursor string              `json:"next_cursor,omitempty" doc:"The cursor of the next page; absent on the last." example:"MDE5M2EzYzItNmQwZS03YTFiLTljNGQtNWU2ZjdhOGI5YzBk"`
}

// AIProposalDetail is a proposal as its approver reviews it (AI-07): the platform's summary, the items, the impact and
// the evidence, the AI's rationale apart, what it was made from and what became of it.
type AIProposalDetail struct {
	AIProposalSummary
	TaskVersion   int              `json:"task_version" example:"1"`
	Description   string           `json:"description" doc:"The task type's description, as the model and the approver read it." example:"Create a compliance task with an owner and a due date from a finding."`
	Payload       any              `json:"payload" doc:"Exactly what executes once approved." example:"{\"title\":\"Hold the AGM\",\"due_on\":\"2027-06-30\"}"`
	PayloadSHA256 string           `json:"payload_sha256" doc:"Its SHA-256, which the approval signs (AI-P5)." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	ItemList      []AIProposalItem `json:"item_list" nullable:"false" doc:"A batch's items, each selectable by its number (AI-32)."`
	Impact        any              `json:"impact,omitempty" doc:"The dry run's impact: what it would change, as its task type reports it (ledger lines, balances, checks, documents)." example:"{\"tasks\":[{\"title\":\"Hold the AGM\",\"change\":\"create\"}]}"`
	Evidence      []AIEvidenceRef  `json:"evidence" nullable:"false" doc:"The records and documents the AI relied on."`
	Rationale     *AIText          `json:"rationale,omitempty" doc:"Why the AI proposes it, in its words, apart from the platform's summary (AI-07)."`
	Agent         string           `json:"agent" doc:"The AI that proposed it." example:"chat"`
	Model         string           `json:"model" example:"claude-opus-5-5"`
	Prompt        string           `json:"prompt" example:"chat@3"`
	DecidedBy     string           `json:"decided_by,omitempty" format:"uuid" doc:"Who approved or rejected it." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0e"`
	DecidedAt     string           `json:"decided_at,omitempty" format:"date-time" example:"2026-11-15T10:00:00Z"`
	ApprovedItems []ProposalItem   `json:"approved_items,omitempty" doc:"The items approved, when an approval chose some."`
	Execution     any              `json:"execution,omitempty" doc:"What the executor did: its run, the records and entries it made, or the error." example:"{\"records\":[\"task:0193a3c2\"]}"`
	YouMayApprove bool             `json:"you_may_approve" doc:"Whether you may sign its approval request now." example:"true"`
}

// AITaskTypeView is a task type of the release's catalogue, as the AI tasks page lists it (§9.6).
type AITaskTypeView struct {
	ID          string           `json:"id" example:"sec.create_task"`
	Version     int              `json:"version" example:"1"`
	Name        string           `json:"name" example:"Create a compliance task"`
	Description string           `json:"description" example:"Create a compliance task with an owner and a due date from a finding."`
	Tier        int              `json:"tier" minimum:"1" maximum:"3" example:"1"`
	Items       int              `json:"max_items" doc:"The most items one proposal holds." example:"5"`
	PerItem     string           `json:"max_amount_per_item,omitempty" doc:"The most one item may move, in currency." example:"5000.00"`
	Total       string           `json:"max_amount_total,omitempty" doc:"The most one proposal may move, in currency." example:"20000.00"`
	Currency    string           `json:"currency,omitempty" example:"SGD"`
	Approvers   []AIApproverRole `json:"approvers" nullable:"false" doc:"The roles that approve it."`
	ExpiryHours int              `json:"expiry_hours" example:"72"`
	On          bool             `json:"on" doc:"Whether the organisation has turned it on (ai.task_types)." example:"false"`
	Built       bool             `json:"built" doc:"Whether this release can run it: its command is built." example:"true"`
}

type (
	aiProposalListInput struct {
		PageQuery
		Status string `query:"status" enum:"draft,invalid,pending,approved,rejected,expired,superseded,executing,executed,stale,failed" doc:"Only proposals in this state; leave it out for all." example:"pending"`
	}
	aiProposalListOutput struct{ Body AIProposalPage }
	// AIProposalPath names a proposal.
	AIProposalPath struct {
		ProposalID string `path:"proposal_id" format:"uuid" doc:"The proposal's ID." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0d"`
	}
	aiProposalGetInput  struct{ AIProposalPath }
	aiProposalGetOutput struct{ Body AIProposalDetail }
	aiTaskTypesOutput   struct{ Body AITaskTypeList }
	aiTaskTypesInput    struct{}
)

// AIApproverRole is a role that approves a task type's proposals.
type AIApproverRole string

// Schema implements huma.SchemaProvider.
func (AIApproverRole) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A role, as Appendix D names it.", Examples: []any{"owner"}}
}

// AITaskTypeList is the release's catalogue.
type AITaskTypeList struct {
	TaskTypes []AITaskTypeView `json:"task_types" nullable:"false" doc:"Every task type of the release, a small fixed set: not a paged list."`
}

func registerAIProposalReads(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.ListAIProposals, huma.Operation{
		OperationID: "listAIProposals", Method: http.MethodGet, Path: "/v1/ai/proposals", Tags: []string{"AI"},
		Summary: "List the AI's proposals",
		Description: "The AI's proposals you may see (design D5), newest first, a page at a time: those made for you, those you may " +
			"approve now, and, for the Owner and the DPO, every one; filtered by state. The summary is the platform's, never the model's " +
			"(AI-07). No side effects. On the never-list for the AI and MCP (§9.6).",
	}, append([]string{CodeCursor, CodeAIPersonOnly}, locks...), func(ctx context.Context, in *aiProposalListInput) (*aiProposalListOutput, error) {
		p, _ := authn.From(ctx)
		if err := personOnly(p); err != nil {
			return nil, err
		}
		after := uuid.Nil
		if in.Cursor != "" {
			c, err := decodeCursor(in.Cursor)
			if err != nil {
				return nil, err
			}
			if after, err = uuid.Parse(c); err != nil {
				return nil, badCursor()
			}
		}
		var statuses []string
		if in.Status != "" {
			statuses = []string{in.Status}
		}
		out := AIProposalPage{Items: []AIProposalSummary{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var shown []storage.AIProposal
			for round := 0; round < 10 && len(shown) <= in.PageSize; round++ {
				batch, err := u.AIProposals().List(ctx, storage.AIProposalQuery{Statuses: statuses, After: after, Limit: 100})
				if err != nil {
					return err
				}
				for _, pr := range batch {
					ok, err := a.seesAIProposal(ctx, u, p, pr)
					if err != nil {
						return err
					}
					if ok {
						shown = append(shown, pr)
					}
				}
				if len(batch) < 100 {
					break
				}
				after = batch[len(batch)-1].ID
			}
			if len(shown) > in.PageSize {
				shown = shown[:in.PageSize]
				out.NextCursor = encodeCursor(shown[len(shown)-1].ID.String())
			}
			for _, pr := range shown {
				out.Items = append(out.Items, a.proposalSummary(pr))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &aiProposalListOutput{Body: out}, nil
	})

	register(a, authz.GetAIProposal, huma.Operation{
		OperationID: "getAIProposal", Method: http.MethodGet, Path: "/v1/ai/proposals/{proposal_id}", Tags: []string{"AI"},
		Summary: "Read an AI proposal",
		Description: "A proposal as its approver reviews it (AI-07): the platform's summary from the payload and the dry run, the " +
			"items, each selectable by its number, the impact and the evidence, and the AI's rationale apart, labelled as AI text; what it " +
			"was made from, its approval request and what became of it. Not found unless you may see it (design D5). No side effects. On " +
			"the never-list for the AI and MCP (§9.6).",
	}, append([]string{CodeNotFound, CodeAIPersonOnly}, locks...), func(ctx context.Context, in *aiProposalGetInput) (*aiProposalGetOutput, error) {
		p, _ := authn.From(ctx)
		if err := personOnly(p); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ProposalID)
		if err != nil {
			return nil, notFoundProposal()
		}
		var out AIProposalDetail
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			pr, err := u.AIProposals().Get(ctx, id)
			if err == ports.ErrNotFound {
				return notFoundProposal()
			}
			if err != nil {
				return err
			}
			if ok, err := a.seesAIProposal(ctx, u, p, pr); err != nil {
				return err
			} else if !ok {
				return notFoundProposal()
			}
			out, err = a.proposalDetail(ctx, u, p, pr)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &aiProposalGetOutput{Body: out}, nil
	})

	register(a, authz.ListAITaskTypes, exempt(huma.Operation{
		OperationID: "listAITaskTypes", Method: http.MethodGet, Path: "/v1/ai/task-types", Tags: []string{"AI"},
		Summary: "List the AI's task types",
		Description: "The release's catalogue of the changes the AI may propose (§9.6), as the AI tasks page lists it: each task type's " +
			"tier, limits, approver roles and expiry, whether this release can run it, and whether the organisation has turned it on " +
			"(ai.task_types, AI-32). No side effects. On the never-list for the AI and MCP (§9.6).",
	}, "cursor", "the release's catalogue is a small fixed set of task types, never paged"), append([]string{CodeAIPersonOnly}, locks...), func(ctx context.Context, _ *aiTaskTypesInput) (*aiTaskTypesOutput, error) {
		p, _ := authn.From(ctx)
		if err := personOnly(p); err != nil {
			return nil, err
		}
		out := AITaskTypeList{TaskTypes: []AITaskTypeView{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			on, err := settingOn(ctx, u, settingTaskTypes, today)
			if err != nil {
				return err
			}
			for _, tt := range a.catalogue().Types() {
				_, built := a.commands()[tt.Command]
				approvers := make([]AIApproverRole, len(tt.Approvers))
				for i, r := range tt.Approvers {
					approvers[i] = AIApproverRole(r)
				}
				out.TaskTypes = append(out.TaskTypes, AITaskTypeView{ID: tt.ID, Version: tt.Version, Name: tt.Name, Description: tt.Description, Tier: tt.Tier,
					Items: tt.Limits.Items, PerItem: tt.Limits.AmountPerItem, Total: tt.Limits.AmountTotal, Currency: tt.Limits.Currency,
					Approvers: approvers, ExpiryHours: tt.ExpiryHours, On: slices.Contains(strings.Split(on, ","), tt.ID), Built: built})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &aiTaskTypesOutput{Body: out}, nil
	})
}

// seesAIProposal reports whether p may see pr (design D5): it was made for them, they decided it, they are the Owner or
// the DPO, or they may sign its pending request now.
func (a *API) seesAIProposal(ctx context.Context, u storage.UnitOfWork, p authn.Principal, pr storage.AIProposal) (bool, error) {
	if p.AI() {
		return false, nil
	}
	if pr.RequestedBy == p.UserID || (pr.DecidedBy != nil && *pr.DecidedBy == p.UserID) ||
		slices.Contains(p.Roles, authz.Owner) || slices.Contains(p.Roles, authz.DPO) {
		return true, nil
	}
	if pr.Status != storage.ProposalPending || pr.ApprovalRequest == nil {
		return false, nil
	}
	may, err := a.mayDecideProposal(ctx, u, p, *pr.ApprovalRequest)
	return may, err
}

// mayDecideProposal reports whether p may sign the proposal's approval request now.
func (a *API) mayDecideProposal(ctx context.Context, u storage.UnitOfWork, p authn.Principal, request uuid.UUID) (bool, error) {
	r, err := u.Approvals().Get(ctx, request)
	if err == ports.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sigs, err := u.Approvals().Signatures(ctx, r.ID)
	if err != nil {
		return false, err
	}
	return r.Status == "pending" && mayDecide(p, r, sigs), nil
}

func (a *API) proposalSummary(pr storage.AIProposal) AIProposalSummary {
	var impact proposal.Impact
	_ = json.Unmarshal(pr.Impact, &impact)
	name := pr.TaskType
	if tt, ok := a.catalogue().Get(pr.TaskType); ok {
		name = tt.Name
	}
	s := AIProposalSummary{ID: pr.ID.String(), TaskType: pr.TaskType, TaskName: name, Tier: pr.RiskTier, Status: pr.Status, Summary: impact.Summary,
		Reason: pr.Reason, RequestedBy: pr.RequestedBy.String(), Items: pr.ItemCount, ProposedAt: pr.ProposedAt.UTC().Format(time.RFC3339),
		ExpiresAt: pr.ExpiresAt.UTC().Format(time.RFC3339)}
	if pr.ApprovalRequest != nil {
		s.ApprovalID = pr.ApprovalRequest.String()
	}
	return s
}

func (a *API) proposalDetail(ctx context.Context, u storage.UnitOfWork, p authn.Principal, pr storage.AIProposal) (AIProposalDetail, error) {
	d := AIProposalDetail{AIProposalSummary: a.proposalSummary(pr), TaskVersion: pr.TaskVersion, PayloadSHA256: pr.PayloadSHA256,
		ItemList: []AIProposalItem{}, Evidence: []AIEvidenceRef{}, Agent: pr.Agent, Model: pr.Model, Prompt: pr.Prompt}
	for _, n := range pr.ApprovedItems {
		d.ApprovedItems = append(d.ApprovedItems, ProposalItem(n))
	}
	if tt, ok := a.catalogue().Get(pr.TaskType); ok {
		d.Description = tt.Description
	}
	_ = json.Unmarshal([]byte(pr.Payload), &d.Payload)
	var batch struct {
		Items []any `json:"items"`
	}
	if pr.ItemCount > 0 && json.Unmarshal([]byte(pr.Payload), &batch) == nil {
		for i, it := range batch.Items {
			d.ItemList = append(d.ItemList, AIProposalItem{N: i + 1, Item: it})
		}
	}
	if len(pr.Impact) > 0 {
		var impact struct {
			Changes any `json:"changes"`
		}
		_ = json.Unmarshal(pr.Impact, &impact)
		d.Impact = impact.Changes
	}
	for _, e := range pr.Evidence {
		d.Evidence = append(d.Evidence, AIEvidenceRef{Kind: e.Kind, ID: e.ID, Title: e.Title})
	}
	if pr.Rationale != "" {
		d.Rationale = &AIText{Text: pr.Rationale, Origin: "ai"}
	}
	if pr.DecidedBy != nil {
		d.DecidedBy = pr.DecidedBy.String()
	}
	if pr.DecidedAt != nil {
		d.DecidedAt = pr.DecidedAt.UTC().Format(time.RFC3339)
	}
	if len(pr.Execution) > 0 {
		_ = json.Unmarshal(pr.Execution, &d.Execution)
	}
	if pr.Status == storage.ProposalPending && pr.ApprovalRequest != nil {
		may, err := a.mayDecideProposal(ctx, u, p, *pr.ApprovalRequest)
		if err != nil {
			return d, err
		}
		d.YouMayApprove = may
	}
	return d, nil
}

func notFoundProposal() *Problem {
	return problem(CodeNotFound, "No AI proposal with that ID exists in this organisation, or you may not see it.",
		"List the proposals to find their IDs")
}
