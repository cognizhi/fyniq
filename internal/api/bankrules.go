package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Bank rules' auto-apply (FR-BNK-14, FR-APR-01; p1-bank-import-reconcile task 5.2, design D8): a rule reconciles lines
// by itself only once a person holding A on "Bank transactions, categorise" approves it, after its back-test, and no
// one who asked (FM-1). The rules themselves, their back-test and the rules offered from repeated codings are below
// (task 9.1).

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

// BankRuleConditions are what a line must meet for a rule, all of them (FR-BNK-05). Texts compare without regard to
// case or Unicode form.
type BankRuleConditions struct {
	DescriptionContains  string `json:"description_contains,omitempty" maxLength:"200" doc:"Text the description contains." example:"aws"`
	DescriptionRegex     string `json:"description_regex,omitempty" maxLength:"200" doc:"A regular expression the description matches (RE2)." example:"^GRAB\\*"`
	Direction            string `json:"direction,omitempty" enum:"in,out" doc:"Money in or money out; left out, either." example:"out"`
	AmountMin            string `json:"amount_min,omitempty" pattern:"^[0-9]{1,16}(\\.[0-9]{1,4})?$" doc:"The least size of the line's amount, in its currency." example:"10.00"`
	AmountMax            string `json:"amount_max,omitempty" pattern:"^[0-9]{1,16}(\\.[0-9]{1,4})?$" doc:"The greatest size of the line's amount." example:"500.00"`
	Currency             string `json:"currency,omitempty" pattern:"^[A-Z]{3}$" doc:"The line's currency." example:"SGD"`
	MoneyAccountID       string `json:"money_account_id,omitempty" format:"uuid" doc:"The money account the line is on." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b10"`
	CounterpartyContains string `json:"counterparty_contains,omitempty" maxLength:"200" doc:"Text the counterparty contains." example:"amazon web services"`
}

// BankRuleAction is how a rule codes the lines it meets.
type BankRuleAction struct {
	AccountID string `json:"account_id" format:"uuid" doc:"The account the line goes to." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	ContactID string `json:"contact_id,omitempty" format:"uuid" doc:"The contact it names." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	GSTCode   string `json:"gst_code,omitempty" maxLength:"16" doc:"Its GST code; left out, the account's default." example:"TX"`
	Reference string `json:"reference,omitempty" maxLength:"140" doc:"A reference kept with it." example:"AWS"`
}

// BankRuleDraft is a rule as created or changed.
type BankRuleDraft struct {
	Name     string             `json:"name" minLength:"1" maxLength:"100" doc:"The rule's name." example:"AWS to hosting"`
	Position int                `json:"position" minimum:"1" maximum:"10000" doc:"Its place in the order: the first rule a line meets codes it." example:"1"`
	Active   bool               `json:"active" doc:"Whether it suggests at all." example:"true"`
	When     BankRuleConditions `json:"when" doc:"What a line must meet."`
	Then     BankRuleAction     `json:"then" doc:"How it codes the line."`
}

// BankRule is a rule and whether it applies by itself.
type BankRule struct {
	ID                  string             `json:"id" format:"uuid" doc:"The rule's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fb1"`
	Name                string             `json:"name" doc:"Its name." example:"AWS to hosting"`
	Position            int                `json:"position" doc:"Its place in the order." example:"1"`
	Active              bool               `json:"active" doc:"Whether it suggests at all." example:"true"`
	When                BankRuleConditions `json:"when" doc:"What a line must meet."`
	Then                BankRuleAction     `json:"then" doc:"How it codes the line."`
	AutoApply           bool               `json:"auto_apply" doc:"Whether it reconciles lines by itself, under an approved request (FR-BNK-14)." example:"false"`
	AutoApplyApprovalID string             `json:"auto_apply_approval_id,omitempty" format:"uuid" doc:"The approval that let it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fc1"`
	FromAIProposal      bool               `json:"from_ai_proposal" doc:"Whether an AI proposal made it." example:"false"`
	Problem             string             `json:"problem,omitempty" doc:"Why it no longer reads, when it does not: it matches nothing until it is changed." example:"the account is not active"`
	CreatedBy           string             `json:"created_by" doc:"Who made it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4a01"`
	Version             int                `json:"version" doc:"Its version." example:"2"`
}

// BankRuleBackTest is what conditions would have matched among the lines held.
type BankRuleBackTest struct {
	Examined int            `json:"examined" doc:"The lines looked at." example:"420"`
	Matched  int            `json:"matched" doc:"Those the conditions meet." example:"12"`
	ByStatus map[string]int `json:"by_status" doc:"The matched lines by their status now." example:"{\"categorised\":11,\"unreconciled\":1}"`
	Sample   []BankLine     `json:"sample" nullable:"false" doc:"The first few matched lines, latest first."`
}

// BankRuleOffer is a rule offered from lines a person coded the same way several times.
type BankRuleOffer struct {
	When  BankRuleConditions `json:"when" doc:"The conditions the lines share."`
	Then  BankRuleAction     `json:"then" doc:"How they were coded."`
	Lines int                `json:"lines" doc:"How many lines were coded so." example:"5"`
}

type ruleInput struct {
	KeyHeader
	PreferHeader
	Body BankRuleDraft
}

type replaceRuleInput struct {
	BankRulePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body BankRuleDraft
}

type ruleOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The rule's version, for If-Match."`
	Location string `header:"Location" doc:"The rule's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     BankRule
}

type rulesInput struct {
	PageQuery
}

type rulesOutput struct {
	Body struct {
		Items      []BankRule `json:"items" nullable:"false" doc:"The rules in their order."`
		NextCursor string     `json:"next_cursor,omitempty" doc:"The next page's cursor; absent on the last page." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4fb1"`
	}
}

type backTestInput struct {
	Body BankRuleConditions
}

type backTestOutput struct {
	Body BankRuleBackTest
}

type offersOutput struct {
	Body struct {
		Items      []BankRuleOffer `json:"items" nullable:"false" doc:"The rules offered, most lines first."`
		NextCursor string          `json:"next_cursor,omitempty" doc:"The next page's cursor; absent on the last page." example:"MDE5MjhmNmU"`
	}
}

func conditionsOf(c banking.Conditions) BankRuleConditions {
	return BankRuleConditions{DescriptionContains: c.DescriptionContains, DescriptionRegex: c.DescriptionRegex, Direction: c.Direction,
		AmountMin: c.AmountMin, AmountMax: c.AmountMax, Currency: c.Currency, MoneyAccountID: idText(c.MoneyAccountID), CounterpartyContains: c.CounterpartyContains}
}

func actionOf(x banking.Action) BankRuleAction {
	out := BankRuleAction{AccountID: x.AccountID.String(), ContactID: idText(x.ContactID), GSTCode: x.GSTCode, Reference: x.Reference}
	if x.AccountID == uuid.Nil {
		out.AccountID = ""
	}
	return out
}

// conditionsIn reads conditions from the request, naming their fields under path.
func conditionsIn(c BankRuleConditions, path string) (banking.Conditions, []FieldProblem) {
	out := banking.Conditions{DescriptionContains: c.DescriptionContains, DescriptionRegex: c.DescriptionRegex, Direction: c.Direction,
		AmountMin: c.AmountMin, AmountMax: c.AmountMax, Currency: c.Currency, CounterpartyContains: c.CounterpartyContains}
	if c.MoneyAccountID == "" {
		return out, nil
	}
	id, err := uuid.Parse(c.MoneyAccountID)
	if err != nil {
		return out, []FieldProblem{{Code: CodeInvalid, Field: path + ".money_account_id", Message: "is not an ID", Fix: "Send the money account's ID"}}
	}
	out.MoneyAccountID = &id
	return out, nil
}

func ruleRequestOf(d BankRuleDraft, at time.Time) (banking.RuleRequest, error) {
	when, fields := conditionsIn(d.When, "body.when")
	then := banking.Action{GSTCode: d.Then.GSTCode, Reference: d.Then.Reference}
	for _, x := range []struct {
		text, field string
		to          func(uuid.UUID)
	}{{d.Then.AccountID, "body.then.account_id", func(id uuid.UUID) { then.AccountID = id }},
		{d.Then.ContactID, "body.then.contact_id", func(id uuid.UUID) { then.ContactID = &id }}} {
		if x.text == "" {
			continue
		}
		id, err := uuid.Parse(x.text)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: x.field, Message: "is not an ID", Fix: "Send its ID"})
			continue
		}
		x.to(id)
	}
	if len(fields) > 0 {
		return banking.RuleRequest{}, invalid(fields)
	}
	return banking.RuleRequest{Name: d.Name, Position: d.Position, Active: d.Active, When: when, Then: then, At: at}, nil
}

func bankRuleOf(r storage.BankRule) BankRule {
	out := BankRule{ID: r.ID.String(), Name: r.Name, Position: r.Position, Active: r.Active, AutoApply: r.AutoApply,
		AutoApplyApprovalID: idText(r.AutoApplyApprovalID), FromAIProposal: r.FromAIProposal, CreatedBy: r.CreatedBy, Version: r.Version}
	rule, err := banking.ReadRule(r)
	if err != nil {
		out.Problem = err.Error()
		return out
	}
	out.When, out.Then = conditionsOf(rule.When), actionOf(rule.Then)
	return out
}

func registerBankRuleBook(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		banking.CodeRule, storage.CodeDuplicate}, locks...)

	register(a, authz.ListBankRules, huma.Operation{
		OperationID: "listBankRules", Method: http.MethodGet, Path: "/v1/bank-rules", Tags: []string{"Banking"},
		Summary: "List the bank rules",
		Description: "The organisation's bank rules in their order, a page at a time (FR-BNK-05): the first rule a line meets suggests its " +
			"coding, and one that auto-applies reconciles it within the caps (FR-BNK-14). A rule that no longer reads says why. Read-only.",
	}, []string{CodeCursor}, func(ctx context.Context, in *rulesInput) (*rulesOutput, error) {
		p, _ := authn.From(ctx)
		out := &rulesOutput{}
		out.Body.Items = []BankRule{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.BankMatching().Rules(ctx)
			if err != nil {
				return err
			}
			if all, out.Body.NextCursor, err = page(all, func(r storage.BankRule) string { return r.ID.String() }, in.PageQuery); err != nil {
				return err
			}
			for _, r := range all {
				out.Body.Items = append(out.Body.Items, bankRuleOf(r))
			}
			return nil
		})
		return out, err
	})

	register(a, authz.CreateBankRule, huma.Operation{
		OperationID: "createBankRule", Method: http.MethodPost, Path: "/v1/bank-rules", Tags: []string{"Banking"},
		Summary: "Create a bank rule", DefaultStatus: http.StatusCreated,
		Description: "Saves a bank rule: its conditions, the coding it suggests and its place in the order (FR-BNK-05). Every problem comes " +
			"back together (BANK-06): an account a line reaches by another path, a GST code the pack does not have, a regular expression " +
			"that does not compile. A new rule never applies by itself: that needs another person's approval after its back-test " +
			"(requestBankRuleAutoApply). Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *ruleInput) (*ruleOutput, error) {
		req, err := ruleRequestOf(in.Body, a.deps.Clock.Now())
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (BankRule, error) {
			r, err := banking.CreateRule(ctx, u, req)
			if err != nil {
				return BankRule{}, err
			}
			return bankRuleOf(r.BankRule), nil
		})
		if err != nil {
			return nil, err
		}
		out := &ruleOutput{Status: w.Status, Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}
		if !w.Previewed {
			out.ETag, out.Location = etag(w.Body.Version, w.Body.AutoApply), "/v1/bank-rules/"+w.Body.ID
		}
		return out, nil
	})

	register(a, authz.GetBankRule, huma.Operation{
		OperationID: "getBankRule", Method: http.MethodGet, Path: "/v1/bank-rules/{rule_id}", Tags: []string{"Banking"},
		Summary: "Get a bank rule", Description: "Returns a bank rule with its ETag. Read-only.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *BankRulePath) (*ruleOutput, error) {
		id, err := parseID("rule_id", in.RuleID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var r storage.BankRule
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			r, err = u.BankMatching().Rule(ctx, id)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &ruleOutput{Status: http.StatusOK, ETag: ruleETag(r), Body: bankRuleOf(r)}, nil
	})

	register(a, authz.ReplaceBankRule, huma.Operation{
		OperationID: "replaceBankRule", Method: http.MethodPut, Path: "/v1/bank-rules/{rule_id}", Tags: []string{"Banking"},
		Summary: "Change a bank rule",
		Description: "Changes a rule's name, place, conditions, coding or whether it is on (FR-BNK-05). A rule that applies by itself stops " +
			"doing so when its conditions or coding change, until a new request is approved; an approval asked for meanwhile is stopped. " +
			"Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, storage.CodeVersion}, writes...), func(ctx context.Context, in *replaceRuleInput) (*ruleOutput, error) {
		id, err := parseID("rule_id", in.RuleID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		req, err := ruleRequestOf(in.Body, a.deps.Clock.Now())
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (BankRule, error) {
			r, err := u.BankMatching().Rule(ctx, id)
			if err != nil {
				return BankRule{}, err
			}
			if err := checkIfMatch(in.IfMatch, ruleETag(r)); err != nil {
				return BankRule{}, err
			}
			changed, err := banking.ChangeRule(ctx, u, id, r.Version, req)
			if err != nil {
				return BankRule{}, err
			}
			return bankRuleOf(changed.BankRule), nil
		})
		if err != nil {
			return nil, err
		}
		return &ruleOutput{Status: w.Status, ETag: etag(w.Body.Version, w.Body.AutoApply), Applied: applied(w), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})

	// A back-test saves nothing, so a repeat is harmless and return=preview would answer the same.
	register(a, authz.BackTestBankRule, exempt(exempt(huma.Operation{
		OperationID: "backTestBankRule", Method: http.MethodPost, Path: "/v1/bank-rules:back-test", Tags: []string{"Banking"},
		Summary: "Back-test bank rule conditions",
		Description: "Runs conditions over the lines held and says what they would have matched: how many of how many, by status now, and the " +
			"first few, latest first (FR-BNK-05, FR-BNK-14). Saves nothing.",
	}, "Idempotency-Key", "A back-test saves nothing, so a repeat is harmless and answers the same (FR-BNK-05)."),
		"Prefer", "A back-test saves nothing: return=preview would answer the same as the request itself (FR-INT-14)."), []string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, banking.CodeRule}, func(ctx context.Context, in *backTestInput) (*backTestOutput, error) {
		c, fields := conditionsIn(in.Body, "body")
		if len(fields) > 0 {
			return nil, invalid(fields)
		}
		p, _ := authn.From(ctx)
		out := &backTestOutput{Body: BankRuleBackTest{ByStatus: map[string]int{}, Sample: []BankLine{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			bt, err := banking.RunBackTest(ctx, u, c)
			if err != nil {
				return err
			}
			out.Body.Examined, out.Body.Matched = bt.Examined, bt.Matched
			for k, v := range bt.ByStatus {
				out.Body.ByStatus[k] = v
			}
			for _, l := range bt.Sample {
				out.Body.Sample = append(out.Body.Sample, bankLineOf(l))
			}
			return nil
		})
		return out, err
	})

	register(a, authz.ListBankRuleOffers, huma.Operation{
		OperationID: "listBankRuleOffers", Method: http.MethodGet, Path: "/v1/bank-rule-offers", Tags: []string{"Banking"},
		Summary: "List the rules offered from repeated codings",
		Description: fmt.Sprintf("Rules a person may want, from lines they coded the same way at least %d times that no rule meets yet: the "+
			"counterparty or the description's leading words, the direction, the account and the GST code (FR-BNK-05). Saving one is "+
			"createBankRule. Read-only.", banking.RepeatedCodings),
	}, []string{banking.CodeRule, CodeCursor}, func(ctx context.Context, in *rulesInput) (*offersOutput, error) {
		p, _ := authn.From(ctx)
		out := &offersOutput{}
		out.Body.Items = []BankRuleOffer{}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			rules, _, err := banking.Rules(ctx, u)
			if err != nil {
				return err
			}
			offers, err := banking.Repeated(ctx, u, rules, banking.RepeatedCodings)
			if err != nil {
				return err
			}
			key := func(o banking.Offer) string {
				return fmt.Sprintf("%s|%s|%s|%s|%s", o.Then.AccountID, o.Then.GSTCode, o.When.Direction, o.When.CounterpartyContains, o.When.DescriptionContains)
			}
			if offers, out.Body.NextCursor, err = page(offers, key, in.PageQuery); err != nil {
				return err
			}
			for _, o := range offers {
				out.Body.Items = append(out.Body.Items, BankRuleOffer{When: conditionsOf(o.When), Then: actionOf(o.Then), Lines: len(o.Lines)})
			}
			return err
		})
		return out, err
	})
}
