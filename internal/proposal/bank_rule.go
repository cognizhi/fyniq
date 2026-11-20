package proposal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// bank.rule_upsert (PRD-001 Appendix E; p1-ai-proposals task 3.2, design D8; FR-BNK-05): a bank rule created or changed through the functions a person's
// Create and Change a bank rule run (banking.CreateRule, ChangeRule), as the approving person. The payload has NO auto-apply field, so a rule the AI
// proposes can never apply by itself: that is another person's approval after the rule's back-test (FR-BNK-14), and a change to a rule's conditions
// or coding stops it applying meanwhile, in the database. The dry run runs the back-test and shows how many held lines the conditions meet, by
// status. The rule's coding goes to an ordinary account (never a money, receivables or payables account: the domain's own check). A rule an AI
// proposal made says so (from_ai_proposal). The undo switches a created rule off, or puts a changed rule's name, place, conditions and coding back, as the
// person, while it is at the version the execution left it.

// BankRulePayload is bank.rule_upsert's payload: a rule_id to change one, none to create.
type BankRulePayload struct {
	RuleID   string             `json:"rule_id"`
	Name     string             `json:"name"`
	Position int                `json:"position"`
	Active   *bool              `json:"active"`
	When     banking.Conditions `json:"when"`
	Then     bankRuleThen       `json:"then"`
}

type bankRuleThen struct {
	Account   string `json:"account"`
	ContactID string `json:"contact_id"`
	GSTCode   string `json:"gst_code"`
	Reference string `json:"reference"`
}

type bankRulePlan struct {
	id          uuid.UUID // the rule changed; Nil for a new one
	req         banking.RuleRequest
	was         *storage.BankRule
	fingerprint int64
}

func (p BankRulePayload) plan(ctx context.Context, u storage.UnitOfWork) (bankRulePlan, error) {
	var out bankRulePlan
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return out, err
	}
	i := slices.IndexFunc(accounts, func(a storage.Account) bool { return a.Code == strings.TrimSpace(p.Then.Account) })
	if i < 0 {
		return out, refusal("payload.then.account is not the code of an account of this organisation", "Use the code of the account the rule codes lines to, as records_search returned it")
	}
	act := banking.Action{AccountID: accounts[i].ID, GSTCode: strings.TrimSpace(p.Then.GSTCode), Reference: strings.TrimSpace(p.Then.Reference)}
	if p.Then.ContactID != "" {
		id, err := uuid.Parse(p.Then.ContactID)
		if err != nil {
			return out, refusal("payload.then.contact_id is not an ID", "Use the contact's ID as records_search returned it")
		}
		act.ContactID = &id
	}
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	out.req = banking.RuleRequest{Name: p.Name, Position: p.Position, When: p.When, Then: act, Active: active, FromAIProposal: true}
	if p.RuleID != "" {
		id, err := uuid.Parse(p.RuleID)
		if err != nil {
			return out, refusal("payload.rule_id is not an ID", "Use the rule's ID, or leave it out to make a new rule")
		}
		r, err := u.BankMatching().Rule(ctx, id)
		if err != nil {
			return out, refusal("payload.rule_id is not a bank rule of this organisation", "Use the ID of a rule as the rules' list returned it")
		}
		out.id, out.was = id, &r
	}
	h := fnv.New64a()
	fmt.Fprintf(h, "%s:%d:%t;", accounts[i].ID, accounts[i].Version, accounts[i].Active)
	out.fingerprint = int64(h.Sum64() >> 1) // #nosec G115 -- 63 bits
	return out, nil
}

func bankRuleRefusal(err error) error {
	var be *banking.Error
	if errors.As(err, &be) {
		return refusal(be.Message, be.Fix)
	}
	return err
}

func ruleKey(id uuid.UUID) string { return "bank_rule:" + id.String() }

// UpsertBankRule is bank.rule_upsert's command.
type UpsertBankRule struct{}

// Policy implements Command: the right to create a bank rule.
func (UpsertBankRule) Policy() authz.Policy { return authz.CreateBankRule }

// Measure implements Command: one rule, no amount.
func (UpsertBankRule) Measure(payload json.RawMessage) (Measure, error) {
	var p BankRulePayload
	if err := decodeStrict(payload, &p); err != nil {
		return Measure{}, err
	}
	return Measure{Items: 1}, nil
}

// DryRun implements Command: the rule is made or changed in the rolled-back unit of work, and its back-test is shown: how many of the lines held its
// conditions meet, by status.
func (UpsertBankRule) DryRun(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, as Actor) (Impact, error) {
	var p BankRulePayload
	if err := decodeStrict(payload, &p); err != nil {
		return Impact{}, err
	}
	plan, err := p.plan(ctx, u)
	if err != nil {
		return Impact{}, err
	}
	plan.req.At = as.Today
	var what string
	if plan.was == nil {
		if _, err := banking.CreateRule(ctx, u, plan.req); err != nil {
			return Impact{}, bankRuleRefusal(err)
		}
		what = "Adds"
	} else {
		if _, err := banking.ChangeRule(ctx, u, plan.id, plan.was.Version, plan.req); err != nil {
			return Impact{}, bankRuleRefusal(err)
		}
		what = "Changes"
	}
	bt, err := banking.RunBackTest(ctx, u, plan.req.When)
	if err != nil {
		return Impact{}, err
	}
	rule := map[string]any{"change": strings.ToLower(what), "name": strings.TrimSpace(p.Name), "position": p.Position, "active": plan.req.Active,
		"when": p.When, "codes_to_account": p.Then.Account, "gst_code": nilIfBlank(p.Then.GSTCode), "reference": nilIfBlank(p.Then.Reference),
		"back_test": map[string]any{"lines_examined": bt.Examined, "lines_matched": bt.Matched, "by_status": bt.ByStatus}}
	if plan.was != nil {
		rule["rule"] = plan.was.Name
	}
	changes, err := json.Marshal(map[string]any{"bank_rules": []map[string]any{rule},
		"ledger": "Nothing is posted or reconciled. The rule only suggests a coding, which a person accepts; it never applies by itself unless another person approves that after its back-test"})
	if err != nil {
		return Impact{}, err
	}
	base := map[string]int64{"accounts": plan.fingerprint}
	if plan.was != nil {
		base[ruleKey(plan.id)] = int64(plan.was.Version)
	}
	return Impact{Summary: fmt.Sprintf("%s the bank rule %q: it would have matched %d of %d lines held; it never applies by itself", what, strings.TrimSpace(p.Name), bt.Matched, bt.Examined),
		Changes: changes, Base: base}, nil
}

// bankRuleUndo is what an undo needs.
type bankRuleUndo struct {
	Rule    string `json:"rule"`
	Version int    `json:"version"`
	Created bool   `json:"created"`
	// The values a change replaced: the rule as it was.
	Name       string          `json:"name,omitempty"`
	Position   int             `json:"position,omitempty"`
	Active     bool            `json:"active,omitempty"`
	Conditions json.RawMessage `json:"conditions,omitempty"`
	Action     json.RawMessage `json:"action,omitempty"`
}

// Execute implements Command: only while the rule it changes, and the account it codes to, are as the dry run read them (AI-P5).
func (UpsertBankRule) Execute(ctx context.Context, u storage.UnitOfWork, payload json.RawMessage, base map[string]int64, as Actor) (Execution, error) {
	var p BankRulePayload
	if err := decodeStrict(payload, &p); err != nil {
		return Execution{}, err
	}
	plan, err := p.plan(ctx, u)
	if err != nil {
		return Execution{}, err
	}
	if want, ok := base["accounts"]; !ok || want != plan.fingerprint {
		return Execution{}, ErrStale
	}
	plan.req.At = as.Today
	st := bankRuleUndo{}
	var saved banking.Rule
	if plan.was == nil {
		if saved, err = banking.CreateRule(ctx, u, plan.req); err != nil {
			return Execution{}, bankRuleRefusal(err)
		}
		st.Created = true
	} else {
		if want, ok := base[ruleKey(plan.id)]; !ok || want != int64(plan.was.Version) {
			return Execution{}, ErrStale
		}
		st.Name, st.Position, st.Active, st.Conditions, st.Action = plan.was.Name, plan.was.Position, plan.was.Active, plan.was.Conditions, plan.was.Action
		if saved, err = banking.ChangeRule(ctx, u, plan.id, plan.was.Version, plan.req); err != nil {
			return Execution{}, bankRuleRefusal(err)
		}
	}
	st.Rule, st.Version = saved.ID.String(), saved.Version
	undo, err := json.Marshal(st)
	if err != nil {
		return Execution{}, err
	}
	return Execution{Records: []string{ruleKey(saved.ID)}, Entries: []string{}, Undo: undo}, nil
}

// Undo implements Undoer: a rule the proposal made is switched off, one it changed is put back as it was, as the person who undoes it, while the rule is at
// the version the execution left it.
func (UpsertBankRule) Undo(ctx context.Context, u storage.UnitOfWork, done Execution, as Actor) (Execution, error) {
	var st bankRuleUndo
	if err := json.Unmarshal(done.Undo, &st); err != nil || st.Rule == "" {
		return Execution{}, refusal("the execution kept nothing to undo it with", "Change the rule by hand")
	}
	id, err := uuid.Parse(st.Rule)
	if err != nil {
		return Execution{}, err
	}
	cur, err := u.BankMatching().Rule(ctx, id)
	if err != nil {
		return Execution{}, err
	}
	if cur.Version != st.Version {
		return Execution{}, ErrStale
	}
	now, err := banking.ReadRule(cur)
	if err != nil {
		return Execution{}, bankRuleRefusal(err)
	}
	req := banking.RuleRequest{Name: now.Name, Position: now.Position, When: now.When, Then: now.Then, Active: false, At: as.Today}
	if !st.Created {
		var was banking.Rule
		if err := json.Unmarshal(st.Conditions, &was.When); err != nil {
			return Execution{}, err
		}
		if err := json.Unmarshal(st.Action, &was.Then); err != nil {
			return Execution{}, err
		}
		req = banking.RuleRequest{Name: st.Name, Position: st.Position, When: was.When, Then: was.Then, Active: st.Active, At: as.Today}
	}
	back, err := banking.ChangeRule(ctx, u, id, cur.Version, req)
	switch {
	case storage.CodeOf(err) == storage.CodeVersion:
		return Execution{}, ErrStale
	case err != nil:
		return Execution{}, bankRuleRefusal(err)
	}
	return Execution{Records: []string{ruleKey(back.ID)}, Entries: []string{}}, nil
}

var _ Undoer = UpsertBankRule{}
