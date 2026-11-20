package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/allocation"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ allocation rules (FR-DIM-03; p2-reporting-planning task 8.2, design D15)
//
// A rule names the expense accounts whose cost carries no dimension value, a key (fixed percentages, headcount or usage) and the targets: the dimension values the cost is moved to. Running it
// over whole months works the split out by ALLOC-PRORATA, the part the key cannot place staying on the account, and proposes the journal as a draft that an approver posts like any other.

// AllocationTarget is where part of a shared cost goes.
type AllocationTarget struct {
	Label   string   `json:"label" minLength:"1" maxLength:"80" doc:"What the target is called." example:"Engineering"`
	Values  []string `json:"values" nullable:"false" minItems:"1" maxItems:"8" doc:"The dimension values the allocated lines carry, one of each dimension at most (the department, the project, a customer or a product)."`
	Percent string   `json:"percent,omitempty" pattern:"^[0-9]+(\\.[0-9]{1,2})?$" doc:"With the fixed key: this target's percentage, above nothing, at most two decimals; together at most 100, the rest stays unallocated." example:"40"`
	Code    string   `json:"code,omitempty" maxLength:"80" doc:"With the usage key: the code the usage file names this target by." example:"customer-acme"`
}

// AllocationRuleInput is what a person saves of a rule.
type AllocationRuleInput struct {
	Name     string             `json:"name" minLength:"1" maxLength:"80" doc:"The rule's name." example:"Cloud hosting by department"`
	Accounts []string           `json:"accounts" nullable:"false" minItems:"1" maxItems:"20" doc:"The expense accounts whose cost, with no dimension value, is split (IDs of accounts)."`
	Key      string             `json:"key" enum:"fixed,headcount,usage" doc:"fixed: percentages on the targets; headcount: the people of each target's department in the people projection, those with no department being what it cannot place; usage: a usage file's quantities per code, the rows naming no target being what it cannot place." example:"fixed"`
	Targets  []AllocationTarget `json:"targets" nullable:"false" minItems:"1" maxItems:"50" doc:"Where the cost goes."`
	Archived bool               `json:"archived,omitempty" doc:"true to put the rule out of use: it is not run again and its runs stay." example:"false"`
}

// AllocationRule is a saved rule.
type AllocationRule struct {
	ID      string `json:"id" format:"uuid" doc:"The rule's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7a01"`
	Version int    `json:"version" doc:"The version of its content; it makes the ETag." example:"1"`
	AllocationRuleInput
}

// AllocationRulePage is a page of rules.
type AllocationRulePage struct {
	Items      []AllocationRule `json:"items" nullable:"false" doc:"The rules, latest first."`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

// AllocationUsageRow is one row of a usage file.
type AllocationUsageRow struct {
	Code     string `json:"code" maxLength:"80" doc:"The target code, or any other code the key cannot place." example:"customer-acme"`
	Quantity string `json:"quantity" pattern:"^[0-9]+(\\.[0-9]{1,6})?$" doc:"The usage, zero or more with at most six decimals." example:"1250.5"`
	Billed   string `json:"billed" doc:"What the row was billed, in the base currency." example:"312.40"`
}

// AllocationUsage is a usage file, as rows.
type AllocationUsage struct {
	BillID      string               `json:"bill_id" format:"uuid" doc:"The supplier's bill the file explains." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b01"`
	BilledTotal string               `json:"billed_total" doc:"What the file says was billed in all: it must equal the bill's total and the rows' sum, or the file is refused." example:"1250.00"`
	Rows        []AllocationUsageRow `json:"rows" nullable:"false" minItems:"1" maxItems:"5000" doc:"The file's rows."`
}

// AllocationRunInput asks for a rule over whole months.
type AllocationRunInput struct {
	RuleID string           `json:"rule_id" format:"uuid" doc:"The rule to run." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7a01"`
	From   string           `json:"from" format:"date" doc:"The first day of the first month." example:"2026-03-01"`
	To     string           `json:"to" format:"date" doc:"The last day of the last month (the same month or a later one, at most a year in all)." example:"2026-03-31"`
	Usage  *AllocationUsage `json:"usage,omitempty" doc:"With the usage key: the usage file."`
}

// AllocationPart is one target's part of one account's cost.
type AllocationPart struct {
	Target string `json:"target" doc:"The target's label." example:"Engineering"`
	Amount string `json:"amount" doc:"Its part, in the base currency." example:"400.00"`
}

// AllocationSplit is one source account's cost and how it splits.
type AllocationSplit struct {
	AccountID   string           `json:"account_id" format:"uuid" doc:"The account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c1a01"`
	AccountCode string           `json:"account_code" doc:"Its code." example:"6200"`
	AccountName string           `json:"account_name" doc:"Its name." example:"Cloud hosting"`
	Source      string           `json:"source" doc:"Its cost in the months with no dimension value, in the base currency." example:"1000.00"`
	Parts       []AllocationPart `json:"parts" nullable:"false" doc:"Each target's part, by ALLOC-PRORATA."`
	Unallocated string           `json:"unallocated" doc:"What the key could not place: it stays on the account with no dimension value." example:"250.00"`
}

// AllocationWeight is the weight the key gave a target.
type AllocationWeight struct {
	Target string `json:"target" doc:"The target's label." example:"Engineering"`
	Weight int64  `json:"weight" doc:"In the key's unit: hundredths of a percent, people, or millionths of a unit of usage." example:"4000"`
}

// AllocationLine is a line of the journal the run makes.
type AllocationLine struct {
	AccountID   string   `json:"account_id" format:"uuid" doc:"The account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c1a01"`
	Amount      string   `json:"amount" doc:"Debit positive, credit negative, in the base currency." example:"400.00"`
	Description string   `json:"description" doc:"What the line says." example:"Engineering"`
	Values      []string `json:"values" nullable:"false" doc:"The dimension values the line carries."`
}

// AllocationReport is a rule run over whole months.
type AllocationReport struct {
	RuleID     string             `json:"rule_id" format:"uuid" doc:"The rule." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7a01"`
	Key        string             `json:"key" enum:"fixed,headcount,usage" doc:"The key it ran with." example:"fixed"`
	From       string             `json:"from" format:"date" doc:"The first day." example:"2026-03-01"`
	To         string             `json:"to" format:"date" doc:"The last day." example:"2026-03-31"`
	Weights    []AllocationWeight `json:"weights" nullable:"false" doc:"What the key weighed each target at."`
	Unplaced   int64              `json:"unplaced_weight" doc:"The weight the key could not place, in the same unit." example:"2500"`
	Splits     []AllocationSplit  `json:"splits" nullable:"false" doc:"Each source account that held cost."`
	Lines      []AllocationLine   `json:"lines" nullable:"false" doc:"The journal's lines: each part debited with its target's values, what was placed credited with none."`
	JournalID  string             `json:"journal_id,omitempty" format:"uuid" doc:"The draft journal, once proposed." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c2a01"`
	RunID      string             `json:"run_id,omitempty" format:"uuid" doc:"The recorded run, once proposed." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c2a02"`
	NotPosted  string             `json:"note" doc:"What it is." example:"Nothing is posted: the journal is a draft for an approver."`
	Allocation bool               `json:"proposed" doc:"true once the draft journal is made." example:"false"`
}

// AllocationRunRecord is a recorded run.
type AllocationRunRecord struct {
	ID          string `json:"id" format:"uuid" doc:"The run." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c2a02"`
	RuleVersion int    `json:"rule_version" doc:"The version of the rule it ran." example:"1"`
	From        string `json:"from" format:"date" doc:"The first day." example:"2026-03-01"`
	To          string `json:"to" format:"date" doc:"The last day." example:"2026-03-31"`
	JournalID   string `json:"journal_id" format:"uuid" doc:"The journal it proposed." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c2a01"`
	Key         string `json:"key" enum:"fixed,headcount,usage" doc:"The key." example:"fixed"`
}

// AllocationRunPage is a rule's runs.
type AllocationRunPage struct {
	Items      []AllocationRunRecord `json:"items" nullable:"false" doc:"The runs, earliest first."`
	NextCursor string                `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

type (
	// AllocationRulePath names a rule.
	AllocationRulePath struct {
		RuleID string `path:"rule_id" format:"uuid" doc:"The rule." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7a01"`
	}
	allocationListInput  struct{ PageQuery }
	allocationListOutput struct{ Body AllocationRulePage }
	allocationSaveInput  struct {
		KeyHeader
		PreferHeader
		Body AllocationRuleInput
	}
	allocationReviseInput struct {
		AllocationRulePath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body AllocationRuleInput
	}
	allocationWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The rule's version, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     AllocationRule
	}
	allocationReadOutput struct {
		ETag string `header:"ETag" doc:"The rule's version, for If-Match."`
		Body AllocationRule
	}
	allocationRunInput struct {
		Body AllocationRunInput
	}
	allocationProposeInput struct {
		KeyHeader
		PreferHeader
		Body AllocationRunInput
	}
	allocationReportOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     AllocationReport
	}
	allocationRunsInput struct {
		RuleID string `query:"rule_id" format:"uuid" required:"true" doc:"The rule whose runs are listed." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7a01"`
		PageQuery
	}
	allocationRunsOutput struct{ Body AllocationRunPage }
)

func allocationRuleOf(doc storage.Document) (AllocationRule, error) {
	r, err := allocation.Read(doc)
	if err != nil {
		return AllocationRule{}, err
	}
	out := AllocationRule{ID: doc.ID.String(), Version: doc.Version, AllocationRuleInput: AllocationRuleInput{Name: r.Name, Key: r.Key, Archived: r.Archived,
		Accounts: []string{}, Targets: []AllocationTarget{}}}
	for _, a := range r.Accounts {
		out.Accounts = append(out.Accounts, a.String())
	}
	for _, t := range r.Targets {
		values := make([]string, len(t.Values))
		for i, v := range t.Values {
			values[i] = v.String()
		}
		out.Targets = append(out.Targets, AllocationTarget{Label: t.Label, Values: values, Percent: t.Percent, Code: t.Code})
	}
	return out, nil
}

// ruleOf reads a rule from its input, every ID checked.
func ruleFromInput(in AllocationRuleInput) (allocation.Rule, error) {
	var fields []FieldProblem
	r := allocation.Rule{Name: in.Name, Key: in.Key, Archived: in.Archived}
	for _, s := range in.Accounts {
		id, err := uuid.Parse(s)
		if err != nil {
			fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.accounts", Message: "an account ID is not an ID", Fix: "Send the IDs of accounts"})
			continue
		}
		r.Accounts = append(r.Accounts, id)
	}
	for i, t := range in.Targets {
		x := allocation.Target{Label: t.Label, Percent: t.Percent, Code: t.Code}
		for _, s := range t.Values {
			id, err := uuid.Parse(s)
			if err != nil {
				fields = append(fields, FieldProblem{Code: CodeInvalid, Field: "body.targets", Message: "target " + strconv.Itoa(i+1) + " has a dimension value that is not an ID", Fix: "Send the IDs of dimension values"})
				continue
			}
			x.Values = append(x.Values, id)
		}
		r.Targets = append(r.Targets, x)
	}
	if len(fields) > 0 {
		return r, invalid(fields)
	}
	return r, nil
}

func usageFromInput(in *AllocationUsage) (*allocation.Usage, error) {
	if in == nil {
		return nil, nil
	}
	id, err := uuid.Parse(in.BillID)
	if err != nil {
		return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.usage.bill_id", Message: "is not an ID", Fix: "Send the ID of the supplier's bill the file explains"}})
	}
	u := &allocation.Usage{BillID: id, BilledTotal: in.BilledTotal}
	for _, r := range in.Rows {
		u.Rows = append(u.Rows, allocation.Row{Code: r.Code, Quantity: r.Quantity, Billed: r.Billed})
	}
	return u, nil
}

func allocationReportOf(rep allocation.Report, ruleID uuid.UUID) AllocationReport {
	out := AllocationReport{RuleID: ruleID.String(), Key: rep.Rule.Key, From: dateOf(rep.From), To: dateOf(rep.To), Weights: []AllocationWeight{}, Splits: []AllocationSplit{}, Lines: []AllocationLine{},
		Unplaced: rep.Unplaced, NotPosted: "Nothing is posted: the journal is a draft that an approver posts like any other (FR-DIM-03)."}
	for i, w := range rep.Weights {
		out.Weights = append(out.Weights, AllocationWeight{Target: rep.Rule.Targets[i].Label, Weight: w})
	}
	for _, s := range rep.Splits {
		sp := AllocationSplit{AccountID: s.Account.ID.String(), AccountCode: s.Account.Code, AccountName: s.Account.Name, Source: s.Source.String(), Unallocated: s.Unallocated.String(), Parts: []AllocationPart{}}
		for i, p := range s.Parts {
			sp.Parts = append(sp.Parts, AllocationPart{Target: rep.Rule.Targets[i].Label, Amount: p.String()})
		}
		out.Splits = append(out.Splits, sp)
	}
	for _, l := range rep.Lines {
		values := make([]string, len(l.Values))
		for i, v := range l.Values {
			values[i] = v.String()
		}
		out.Lines = append(out.Lines, AllocationLine{AccountID: l.AccountID.String(), Amount: l.Amount.String(), Description: l.Description, Values: values})
	}
	return out
}

// allocationRange reads the two days of a run.
func allocationRange(in AllocationRunInput) (from, to time.Time, err error) {
	var fields []FieldProblem
	f := dayIn("body.from", in.From, &fields)
	t := dayIn("body.to", in.To, &fields)
	if len(fields) > 0 {
		return from, to, invalid(fields)
	}
	return *f, *t, nil
}

func registerAllocations(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	domain := []string{allocation.CodePercent, allocation.CodeQuantity, allocation.CodeWeights, allocation.CodeRule, allocation.CodePeriod, allocation.CodeUsage, allocation.CodeRun, allocation.CodeNothing}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNotFound}, locks...)
	tags := []string{"Journals"}

	register(a, authz.ListAllocationRules, huma.Operation{
		OperationID: "listAllocationRules", Method: http.MethodGet, Path: "/v1/allocation-rules", Tags: tags,
		Summary:     "List the allocation rules",
		Description: "Every allocation rule, the latest first, a page at a time (FR-DIM-03). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *allocationListInput) (*allocationListOutput, error) {
		p, _ := authn.From(ctx)
		out := allocationListOutput{Body: AllocationRulePage{Items: []AllocationRule{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var all []AllocationRule
			f := storage.DocumentFilter{Type: storage.DocumentAllocationRule, Limit: 200}
			for {
				docs, err := u.Documents().Page(ctx, f)
				if err != nil {
					return err
				}
				for _, d := range docs {
					v, err := allocationRuleOf(d)
					if err != nil {
						return err
					}
					all = append(all, v)
				}
				if len(docs) < f.Limit {
					break
				}
				f.AfterDate, f.AfterID = docs[len(docs)-1].Date, &docs[len(docs)-1].ID
			}
			var err error
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x AllocationRule) string { return x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []AllocationRule{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateAllocationRule, huma.Operation{
		OperationID: "createAllocationRule", Method: http.MethodPost, Path: "/v1/allocation-rules", Tags: tags, DefaultStatus: http.StatusCreated,
		Summary: "Save an allocation rule",
		Description: "Saves a rule (FR-DIM-03): the expense accounts whose cost with no dimension value is split, the key and the targets. The rule posts nothing; running it proposes a draft journal. " +
			"Every problem comes back together. Side effects: the rule and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(domain, writes...), func(ctx context.Context, in *allocationSaveInput) (*allocationWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AllocationRule, error) {
			r, err := ruleFromInput(in.Body)
			if err != nil {
				return AllocationRule{}, err
			}
			doc, err := allocation.Create(ctx, u, r)
			if err != nil {
				return AllocationRule{}, equityProblems(err)
			}
			return allocationRuleOf(doc)
		})
		if err != nil {
			return nil, err
		}
		return allocationAnswer(w), nil
	})

	register(a, authz.GetAllocationRule, huma.Operation{
		OperationID: "getAllocationRule", Method: http.MethodGet, Path: "/v1/allocation-rules/{rule_id}", Tags: tags,
		Summary: "Get an allocation rule", Description: "A rule as saved, with its ETag (FR-DIM-03). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *AllocationRulePath) (*allocationReadOutput, error) {
		id, err := parseID("rule_id", in.RuleID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out allocationReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			if out.Body, err = allocationRuleOf(doc); err != nil {
				return err
			}
			out.ETag = recordETag(doc)
			return nil
		})
		return &out, err
	})

	register(a, authz.ReplaceAllocationRule, huma.Operation{
		OperationID: "replaceAllocationRule", Method: http.MethodPut, Path: "/v1/allocation-rules/{rule_id}", Tags: tags,
		Summary: "Change an allocation rule",
		Description: "Saves the rule's next version (FR-DIM-03); runs already recorded keep the version they ran. archived: true puts it out of use. Needs If-Match with its ETag. " +
			"Side effects: the new version and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired}, append(domain, writes...)...), func(ctx context.Context, in *allocationReviseInput) (*allocationWriteOutput, error) {
		id, err := parseID("rule_id", in.RuleID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AllocationRule, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return AllocationRule{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return AllocationRule{}, err
			}
			r, err := ruleFromInput(in.Body)
			if err != nil {
				return AllocationRule{}, err
			}
			if doc, err = allocation.Revise(ctx, u, id, r); err != nil {
				return AllocationRule{}, equityProblems(err)
			}
			return allocationRuleOf(doc)
		})
		if err != nil {
			return nil, err
		}
		return allocationAnswer(w), nil
	})

	register(a, authz.PreviewAllocation, exempt(exempt(huma.Operation{
		OperationID: "previewAllocation", Method: http.MethodPost, Path: "/v1/allocation-previews", Tags: tags,
		Summary: "Work an allocation out without saving it",
		Description: "Runs a rule over whole months and shows what it would do (FR-DIM-03): each source account's cost with no dimension value (read through the balance service), the key's weights, the " +
			"split by ALLOC-PRORATA with what the key cannot place left unallocated, and the journal's lines. A usage file must add up to its stated total and equal the supplier's bill it explains, " +
			"or it is refused (ALLOC-06). Nothing is saved or posted. Read-only; no side effects.",
	}, "Idempotency-Key", "A preview saves nothing, so a repeat is harmless and answers the same (FR-DIM-03)."),
		"Prefer", "A preview saves nothing: return=preview would answer the same as the request itself (FR-INT-14)."), append([]string{CodeNotFound, CodeInvalid}, append(domain, locks...)...), func(ctx context.Context, in *allocationRunInput) (*allocationReportOutput, error) {
		id, err := parseID("body.rule_id", in.Body.RuleID)
		if err != nil {
			return nil, err
		}
		from, to, err := allocationRange(in.Body)
		if err != nil {
			return nil, err
		}
		usage, err := usageFromInput(in.Body.Usage)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out allocationReportOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			rep, err := allocation.Compute(ctx, u, doc, from, to, usage)
			if err != nil {
				return equityProblems(err)
			}
			out.Status, out.Body = http.StatusOK, allocationReportOf(rep, id)
			return nil
		})
		return &out, err
	})

	register(a, authz.ProposeAllocation, huma.Operation{
		OperationID: "proposeAllocation", Method: http.MethodPost, Path: "/v1/allocation-runs", Tags: tags, DefaultStatus: http.StatusCreated,
		Summary: "Run an allocation rule into a draft journal",
		Description: "Runs a rule over whole months (previewAllocation shows what it does) and makes the journal as a draft for an approver (FR-DIM-03): each part debited to its account with the target's " +
			"dimension values, what was placed credited with none, so what the key cannot place stays on the account. A rule runs once for a month (ALLOC-07). The run is recorded, never changed " +
			"and never reversed (FYR14). Side effects: the draft journal, the recorded run and their audit events. Nothing is posted. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound}, append(domain, writes...)...), func(ctx context.Context, in *allocationProposeInput) (*allocationReportOutput, error) {
		id, err := parseID("body.rule_id", in.Body.RuleID)
		if err != nil {
			return nil, err
		}
		from, to, err := allocationRange(in.Body)
		if err != nil {
			return nil, err
		}
		usage, err := usageFromInput(in.Body.Usage)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AllocationReport, error) {
			doc, err := u.Documents().Lock(ctx, id)
			if err != nil {
				return AllocationReport{}, err
			}
			rep, j, err := allocation.Propose(ctx, u, doc, from, to, usage)
			if err != nil {
				return AllocationReport{}, equityProblems(err)
			}
			out := allocationReportOf(rep, id)
			out.JournalID, out.Allocation = j.ID.String(), true
			return out, nil
		})
		if err != nil {
			return nil, err
		}
		return &allocationReportOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.ListAllocationRuns, huma.Operation{
		OperationID: "listAllocationRuns", Method: http.MethodGet, Path: "/v1/allocation-runs", Tags: tags,
		Summary: "List a rule's runs", Description: "The runs recorded for a rule, earliest first, a page at a time (FR-DIM-03). Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeCursor}, locks...), func(ctx context.Context, in *allocationRunsInput) (*allocationRunsOutput, error) {
		id, err := parseID("rule_id", in.RuleID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := allocationRunsOutput{Body: AllocationRunPage{Items: []AllocationRunRecord{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			if _, err := allocation.Read(doc); err != nil {
				return err
			}
			runs, err := allocation.Runs(ctx, u, id)
			if err != nil {
				return err
			}
			var all []AllocationRunRecord
			for _, r := range runs {
				all = append(all, AllocationRunRecord{ID: r.DocumentID.String(), RuleVersion: r.RuleVer, From: dateOf(r.From), To: dateOf(r.To), JournalID: r.JournalID.String(), Key: r.Key})
			}
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x AllocationRunRecord) string { return x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []AllocationRunRecord{}
			}
			return err
		})
		return &out, err
	})
}

func allocationAnswer(w written[AllocationRule]) *allocationWriteOutput {
	if w.Previewed {
		return &allocationWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &allocationWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

// TransformSchema implements huma.SchemaTransformer: an example for each account ID (NFR-API-01).
func (AllocationRuleInput) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"accounts": "01928f6e-7a3b-7c41-9d2e-5f8a1b3c1a01"})
	return s
}

// TransformSchema implements huma.SchemaTransformer: an example for each dimension value ID (NFR-API-01).
func (AllocationTarget) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"values": "01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"})
	return s
}

// TransformSchema implements huma.SchemaTransformer: an example for each dimension value ID (NFR-API-01).
func (AllocationLine) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"values": "01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"})
	return s
}
