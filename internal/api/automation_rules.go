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
	"github.com/cognizhi/fyniq/internal/domain/automation"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ workflow rules without code (FR-AUT-03; p2-mobile-approvals-automation design D5)
//
// "When a bill over S$5,000 is posted, tell Wei Ling": a trigger from a fixed list, typed conditions and one action from a fixed list, held by the database too. The only action is to tell
// members, each of whom must hold the right to see the document. A rule never posts, approves, pays or files, and nothing of it is on the AI or MCP surface (§9.6). Off until an
// Owner turns on "Workflow rules" with a second approver.

// The refusals of a workflow rule (ENG-23).
var AutomationCodes = []string{automation.CodeRule, automation.CodeOff, automation.CodeNotFound}

// RuleRecipient is a member told by a rule: a user ID.
type RuleRecipient string

// Schema implements huma.SchemaProvider.
func (RuleRecipient) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "uuid", Description: "A member's user ID.", Examples: []any{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"}}
}

// AutomationRuleInput is a rule to make.
type AutomationRuleInput struct {
	Name          string          `json:"name" minLength:"1" maxLength:"100" doc:"The rule's name." example:"Large bills"`
	DocumentType  string          `json:"document_type,omitempty" maxLength:"41" doc:"The kind of document it watches, such as bill, invoice or journal (the kinds whose postings tell people); leave it out for any kind." example:"bill"`
	AmountAtLeast string          `json:"amount_at_least,omitempty" pattern:"^[0-9]+(\\.[0-9]{1,4})?$" doc:"Fires only when the document's debits in the base currency are at least this; leave it out for any amount." example:"5000.00"`
	Recipients    []RuleRecipient `json:"recipients" nullable:"false" minItems:"1" maxItems:"20" doc:"The active members told (user IDs). Each must hold the right to view the document, which is checked every time the rule fires." `
}

// AutomationRule is a saved rule.
type AutomationRule struct {
	ID            string          `json:"id" format:"uuid" doc:"The rule's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a01"`
	Name          string          `json:"name" doc:"The rule's name." example:"Large bills"`
	Trigger       string          `json:"trigger" enum:"document.posted" doc:"What sets it off: a document of a kind is posted." example:"document.posted"`
	DocumentType  string          `json:"document_type,omitempty" doc:"The kind of document it watches; absent for any." example:"bill"`
	AmountAtLeast string          `json:"amount_at_least,omitempty" doc:"The debits in the base currency from which it fires, with four decimals as kept; absent for any amount." example:"5000.0000"`
	Action        string          `json:"action" enum:"notify" doc:"What it does: tells the recipients. It is the only action: no rule posts, approves, pays or files." example:"notify"`
	Recipients    []RuleRecipient `json:"recipients" nullable:"false" doc:"The members told."`
	State         string          `json:"state" enum:"active,paused,archived" doc:"active fires; paused does not and can be resumed; archived is final." example:"active"`
	Version       int             `json:"version" doc:"The rule's version; it makes the ETag." example:"1"`
	CreatedAt     time.Time       `json:"created_at" doc:"When it was made." example:"2026-11-21T09:00:00Z"`
	UpdatedAt     time.Time       `json:"updated_at" doc:"When it last changed." example:"2026-11-21T09:00:00Z"`
}

// AutomationRulePage is a page of rules, oldest first.
type AutomationRulePage struct {
	Items      []AutomationRule `json:"items" nullable:"false" doc:"The rules."`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

// AutomationRuleChange changes some fields of a rule; what is left out stays.
type AutomationRuleChange struct {
	Name          *string          `json:"name,omitempty" minLength:"1" maxLength:"100" doc:"A new name." example:"Large bills"`
	DocumentType  *string          `json:"document_type,omitempty" maxLength:"41" doc:"A new kind of document; an empty string for any kind." example:"bill"`
	AmountAtLeast *string          `json:"amount_at_least,omitempty" pattern:"^([0-9]+(\\.[0-9]{1,4})?)?$" doc:"A new amount; an empty string for any amount." example:"5000.00"`
	Recipients    *[]RuleRecipient `json:"recipients,omitempty" minItems:"1" maxItems:"20" doc:"The new recipients, replacing the old."`
	State         *string          `json:"state,omitempty" enum:"active,paused,archived" doc:"active or paused to turn it on or off; archived ends it for good." example:"paused"`
}

type (
	// AutomationRulePath names a rule.
	AutomationRulePath struct {
		RuleID string `path:"rule_id" format:"uuid" doc:"The rule's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9a01"`
	}
	automationRuleListOutput  struct{ Body AutomationRulePage }
	automationRuleCreateInput struct {
		KeyHeader
		PreferHeader
		Body AutomationRuleInput
	}
	automationRuleChangeInput struct {
		AutomationRulePath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body AutomationRuleChange
	}
	automationRuleOutput struct {
		ETag     string `header:"ETag" doc:"The rule's version, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     AutomationRule
	}
)

func automationRuleOf(r storage.AutomationRule) AutomationRule {
	who := make([]RuleRecipient, len(r.Recipients))
	for i, u := range r.Recipients {
		who[i] = RuleRecipient(u.String())
	}
	return AutomationRule{ID: r.ID.String(), Name: r.Name, Trigger: r.Trigger, DocumentType: r.DocumentType, AmountAtLeast: string(r.AmountAtLeast), Action: r.Action, Recipients: who,
		State: r.State, Version: r.Version, CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC()}
}

func ruleProblem(fp []automation.FieldProblem) *Problem {
	out := make([]FieldProblem, len(fp))
	for i, f := range fp {
		out[i] = FieldProblem{Code: f.Code, Field: f.Field, Message: f.Message, Fix: f.Fix}
	}
	return problem(automation.CodeRule, "The rule cannot be saved: "+fp[0].Field+" "+fp[0].Message+".", fp[0].Fix, out...)
}

func ruleIDs(ss []RuleRecipient) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, len(ss))
	for i, s := range ss {
		id, err := uuid.Parse(string(s))
		if err != nil {
			return nil, problem(automation.CodeRule, "A recipient is not an ID.", "Send the members' user IDs",
				FieldProblem{Code: automation.CodeRule, Field: "body.recipients", Message: string(s) + " is not an ID", Fix: "Send the members' user IDs"})
		}
		out[i] = id
	}
	return out, nil
}

// rulesOn refuses while the organisation's switch is off.
func (a *API) rulesOn(ctx context.Context, u storage.UnitOfWork) error {
	on, err := notify.RulesOn(ctx, u, a.deps.Clock.Now())
	if err != nil {
		return err
	}
	if !on {
		return problem(automation.CodeOff, "Workflow rules are off for this organisation.", "Ask an Owner to turn on \"Workflow rules\" in Settings → Organisation; it needs a second approver.")
	}
	return nil
}

// activeMembers are the user IDs of the organisation's active members.
func activeMembers(ctx context.Context, u storage.UnitOfWork, now time.Time) ([]string, error) {
	ms, err := u.Members().List(ctx, now)
	var out []string
	for _, m := range ms {
		if m.Status == "active" {
			out = append(out, m.UserID.String())
		}
	}
	return out, err
}

func registerAutomationRules(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	people := []string{CodeSessionRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}

	register(a, authz.ListAutomationRules, huma.Operation{
		OperationID: "listAutomationRules", Method: http.MethodGet, Path: "/v1/automation-rules", Tags: []string{"Automation"},
		Summary: "List the organisation's workflow rules",
		Description: "The rules that tell members when a document of a kind is posted (FR-AUT-03), oldest first, a page at a time, each with its conditions, recipients and state. " +
			"Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *PageQuery) (*automationRuleListOutput, error) {
		p, _ := authn.From(ctx)
		out := AutomationRulePage{Items: []AutomationRule{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.AutomationRules().List(ctx)
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(r storage.AutomationRule) string { return r.ID.String() }, *in)
			for _, r := range page {
				out.Items = append(out.Items, automationRuleOf(r))
			}
			out.NextCursor = next
			return err
		})
		return &automationRuleListOutput{Body: out}, err
	})

	register(a, authz.CreateAutomationRule, huma.Operation{
		OperationID: "createAutomationRule", Method: http.MethodPost, Path: "/v1/automation-rules", Tags: []string{"Automation"}, DefaultStatus: http.StatusCreated,
		Summary: "Make a workflow rule",
		Description: "Makes a rule that tells members when a document of a kind, above an amount, is posted (FR-AUT-03): `{ \"name\": \"Large bills\", \"document_type\": \"bill\", \"amount_at_least\": \"5000.00\", " +
			"\"recipients\": [...] }`. The trigger and the one action (notify) are fixed: a rule never posts, approves, pays, files or sends outside Fyniq, and has no expressions. Each recipient must be an " +
			"active member, and holds the right to view the document each time the rule fires; the person who posted it is never told. Refused (AUT-02) while \"Workflow rules\" is off, as it is until an Owner " +
			"turns it on with a second approver; at most 50 rules (AUT-01). Needs a person signed in to Fyniq. Side effects: the rule and its audit event. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and saves nothing. On the never-list for the AI and MCP (§9.6).",
	}, append(append([]string{CodeNotFound}, people...), append(AutomationCodes, locks...)...), func(ctx context.Context, in *automationRuleCreateInput) (*automationRuleOutput, error) {
		p, err := signedIn(ctx, "Making a workflow rule")
		if err != nil {
			return nil, err
		}
		who, err := ruleIDs(in.Body.Recipients)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AutomationRule, error) {
			if err := a.rulesOn(ctx, u); err != nil {
				return AutomationRule{}, err
			}
			now := a.deps.Clock.Now()
			members, err := activeMembers(ctx, u, now)
			if err != nil {
				return AutomationRule{}, err
			}
			if fp := automation.Check(in.Body.Name, in.Body.DocumentType, in.Body.AmountAtLeast, who, notify.DocumentKinds(), members); len(fp) > 0 {
				return AutomationRule{}, ruleProblem(fp)
			}
			all, err := u.AutomationRules().List(ctx)
			if err != nil {
				return AutomationRule{}, err
			}
			if n := len(slices.DeleteFunc(slices.Clone(all), func(r storage.AutomationRule) bool { return r.State == "archived" })); n >= automation.MaxRules {
				return AutomationRule{}, problem(automation.CodeRule, "An organisation has at most 50 rules.", "Archive a rule you no longer need")
			}
			r, err := u.AutomationRules().Create(ctx, storage.NewAutomationRule{Name: strings.TrimSpace(in.Body.Name), DocumentType: in.Body.DocumentType,
				AmountAtLeast: storage.Decimal(in.Body.AmountAtLeast), Recipients: who, By: p.UserID, At: now})
			return automationRuleOf(r), err
		})
		if err != nil {
			return nil, err
		}
		return &automationRuleOutput{ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ChangeAutomationRule, huma.Operation{
		OperationID: "changeAutomationRule", Method: http.MethodPatch, Path: "/v1/automation-rules/{rule_id}", Tags: []string{"Automation"},
		Summary: "Change, pause or archive a workflow rule",
		Description: "Changes a rule's name, kind of document, amount or recipients, or pauses, resumes or archives it, over the version you read (If-Match; FR-AUT-03). What you leave out stays. The trigger and the " +
			"action never change. An archived rule is final. Refused (AUT-02) while \"Workflow rules\" is off. Needs a person signed in to Fyniq. Side effects: the change and its audit event. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{CodeNotFound, CodeIfMatchRequired, CodeStale, storage.CodeVersion}, people...), append(AutomationCodes, locks...)...),
		func(ctx context.Context, in *automationRuleChangeInput) (*automationRuleOutput, error) {
			if _, err := signedIn(ctx, "Changing a workflow rule"); err != nil {
				return nil, err
			}
			id, err := uuid.Parse(in.RuleID)
			if err != nil {
				return nil, problem(automation.CodeNotFound, "No workflow rule with that ID exists.", "List the rules to find their IDs")
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			var who []uuid.UUID
			if in.Body.Recipients != nil {
				if who, err = ruleIDs(*in.Body.Recipients); err != nil {
					return nil, err
				}
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AutomationRule, error) {
				before, err := u.AutomationRules().Lock(ctx, id)
				if errors.Is(err, ports.ErrNotFound) {
					return AutomationRule{}, problem(automation.CodeNotFound, "No workflow rule with that ID exists.", "List the rules to find their IDs")
				}
				if err != nil {
					return AutomationRule{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(before.Version)); err != nil {
					return AutomationRule{}, err
				}
				if err := a.rulesOn(ctx, u); err != nil {
					return AutomationRule{}, err
				}
				c := storage.AutomationRuleChange{Name: before.Name, DocumentType: before.DocumentType, State: before.State, AmountAtLeast: before.AmountAtLeast, Recipients: before.Recipients, At: a.deps.Clock.Now()}
				if in.Body.Name != nil {
					c.Name = strings.TrimSpace(*in.Body.Name)
				}
				if in.Body.DocumentType != nil {
					c.DocumentType = *in.Body.DocumentType
				}
				if in.Body.AmountAtLeast != nil {
					c.AmountAtLeast = storage.Decimal(*in.Body.AmountAtLeast)
				}
				if in.Body.Recipients != nil {
					c.Recipients = who
				}
				if in.Body.State != nil {
					c.State = *in.Body.State
				}
				if before.State == "archived" {
					return AutomationRule{}, problem(automation.CodeRule, "The rule is archived, which is final.", "Make a new rule instead")
				}
				members, err := activeMembers(ctx, u, c.At)
				if err != nil {
					return AutomationRule{}, err
				}
				if fp := automation.Check(c.Name, c.DocumentType, string(c.AmountAtLeast), c.Recipients, notify.DocumentKinds(), members); len(fp) > 0 && c.State != "archived" {
					return AutomationRule{}, ruleProblem(fp)
				}
				r, err := u.AutomationRules().Change(ctx, id, before.Version, c)
				return automationRuleOf(r), err
			})
			if err != nil {
				return nil, err
			}
			return &automationRuleOutput{ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
