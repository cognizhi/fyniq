package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the rules of approval policies (FR-APR-01, rev 0.178)
//
// A policy is an ordered list of rules, each with conditions and its own steps, and its default steps: the first rule
// whose conditions all hold decides a request's steps; when none does, the default steps apply (the owner's decision of
// 6 Nov 2026). A change says what the conditions read: its amount in the base currency, the accounts and dimension
// values of its lines, and its vendor.

// RecordRef names a record by its ID.
type RecordRef string

// Schema implements huma.SchemaProvider: a UUID string, with an example.
func (RecordRef) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "uuid", Description: "A record's ID.", Examples: []any{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"}}
}

// ApprovalRule is a rule of a policy: its conditions and the steps a request that matches them takes.
type ApprovalRule struct {
	Conditions RuleConditions `json:"conditions" doc:"What the rule matches: every condition set must hold."`
	Steps      []ApprovalStep `json:"steps" doc:"The ordered steps a request that matches it takes, instead of the policy's default steps."`
}

// RuleConditions are a rule's conditions.
type RuleConditions struct {
	AmountAtLeast *Money      `json:"amount_at_least,omitempty" doc:"Holds when the change's amount is at or above it, in the base currency; absent for none."`
	Departments   []RecordRef `json:"departments" doc:"Holds when a line carries one of these dimension values (Department's); empty for none."`
	Accounts      []RecordRef `json:"accounts" doc:"Holds when a line posts to one of these accounts; empty for none."`
	Vendors       []RecordRef `json:"vendors" doc:"Holds when the change's vendor is one of these contacts; empty for none."`
}

// ApprovalRuleChange is a rule of a policy you ask for.
type ApprovalRuleChange struct {
	Conditions RuleConditionsChange `json:"conditions" doc:"What the rule matches: set at least one; every one set must hold."`
	Steps      []ApprovalStepChange `json:"steps" minItems:"1" maxItems:"5" doc:"The ordered steps a request that matches it takes: 1 to 5."`
}

// RuleConditionsChange are a rule's conditions you ask for.
type RuleConditionsChange struct {
	AmountAtLeast string      `json:"amount_at_least,omitempty" pattern:"^(0|[1-9][0-9]{0,15})(\\.[0-9]{1,4})?$" doc:"Holds when the change's amount is at or above it: a decimal in the base currency." example:"5000.00"`
	Departments   []RecordRef `json:"departments,omitempty" maxItems:"50" doc:"Holds when a line carries one of these values of the Department dimension."`
	Accounts      []RecordRef `json:"accounts,omitempty" maxItems:"50" doc:"Holds when a line posts to one of these accounts."`
	Vendors       []RecordRef `json:"vendors,omitempty" maxItems:"50" doc:"Holds when the change's vendor is one of these vendor contacts."`
}

// domainRules are stored rules as the policy reads them.
func domainRules(rules []storage.ApprovalRule) []approval.Rule {
	out := make([]approval.Rule, len(rules))
	for i, r := range rules {
		c := r.Conditions
		out[i] = approval.Rule{Steps: domainSteps(r.Steps), Conditions: approval.Conditions{AmountAtLeast: c.AmountAtLeast,
			Departments: slices.Clone(c.Departments), Accounts: slices.Clone(c.Accounts), Vendors: slices.Clone(c.Vendors)}}
	}
	return out
}

// storageRules are rules as a version keeps them: every list present, each sorted, so the database compares them exactly.
func storageRules(rules []approval.Rule) []storage.ApprovalRule {
	out := make([]storage.ApprovalRule, len(rules))
	sorted := func(ids []uuid.UUID) []uuid.UUID {
		x := slices.Clone(ids)
		if x == nil {
			x = []uuid.UUID{}
		}
		slices.SortFunc(x, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		return slices.Compact(x)
	}
	for i, r := range rules {
		c := r.Conditions
		out[i] = storage.ApprovalRule{Steps: storageSteps(r.Steps), Conditions: storage.ApprovalConditions{AmountAtLeast: c.AmountAtLeast,
			Departments: sorted(c.Departments), Accounts: sorted(c.Accounts), Vendors: sorted(c.Vendors)}}
	}
	return out
}

// rulesValue is rules as an envelope's change holds them (canonical JSON, FR-APR-10), which the database compares with
// the version's rules (FY105).
func rulesValue(rules []storage.ApprovalRule) []any {
	out := make([]any, len(rules))
	ids := func(xs []uuid.UUID) []any {
		v := make([]any, len(xs))
		for i, x := range xs {
			v[i] = x.String()
		}
		return v
	}
	for i, r := range rules {
		c := r.Conditions
		out[i] = map[string]any{"conditions": map[string]any{"amount_at_least": c.AmountAtLeast, "departments": ids(c.Departments),
			"accounts": ids(c.Accounts), "vendors": ids(c.Vendors)}, "steps": stepsValue(r.Steps)}
	}
	return out
}

// apiRule is a stored rule as the API shows it, its amount in base.
func apiRule(r storage.ApprovalRule, base string) ApprovalRule {
	out := ApprovalRule{Steps: []ApprovalStep{}, Conditions: RuleConditions{Departments: []RecordRef{}, Accounts: []RecordRef{}, Vendors: []RecordRef{}}}
	for _, s := range r.Steps {
		out.Steps = append(out.Steps, apiStep(s))
	}
	c := r.Conditions
	if c.AmountAtLeast != "" {
		out.Conditions.AmountAtLeast = &Money{Amount: c.AmountAtLeast, Currency: base}
	}
	for _, x := range c.Departments {
		out.Conditions.Departments = append(out.Conditions.Departments, RecordRef(x.String()))
	}
	for _, x := range c.Accounts {
		out.Conditions.Accounts = append(out.Conditions.Accounts, RecordRef(x.String()))
	}
	for _, x := range c.Vendors {
		out.Conditions.Vendors = append(out.Conditions.Vendors, RecordRef(x.String()))
	}
	return out
}

// describeRules says what rules do, for a request's summary and diff.
func describeRules(rules []storage.ApprovalRule) string {
	if len(rules) == 0 {
		return "none"
	}
	parts := make([]string, len(rules))
	for i, r := range rules {
		var when []string
		c := r.Conditions
		if c.AmountAtLeast != "" {
			when = append(when, "amount at least "+c.AmountAtLeast)
		}
		for name, ids := range map[string][]uuid.UUID{"departments": c.Departments, "accounts": c.Accounts, "vendors": c.Vendors} {
			if len(ids) > 0 {
				when = append(when, fmt.Sprintf("%d of its %s", len(ids), name))
			}
		}
		slices.Sort(when)
		parts[i] = fmt.Sprintf("rule %d (%s): %s", i+1, strings.Join(when, ", "), describeSteps(r.Steps))
	}
	return strings.Join(parts, "; ")
}

// parseRules reads the rules asked for: their amounts at the base currency's scale and their records' IDs, every
// problem at once; whether the records exist is rulesProblems'.
func parseRules(in []ApprovalRuleChange, base money.Currency) ([]approval.Rule, []FieldProblem) {
	var bad []FieldProblem
	out := make([]approval.Rule, len(in))
	for i, r := range in {
		at := fmt.Sprintf("body.rules[%d]", i)
		steps, fps := parseSteps(r.Steps, at+".steps")
		bad = append(bad, fps...)
		c := approval.Conditions{}
		if a := strings.TrimSpace(r.Conditions.AmountAtLeast); a != "" {
			m, err := money.Parse(a, base)
			if err != nil {
				bad = append(bad, FieldProblem{Code: approval.CodeRules, Field: at + ".conditions.amount_at_least",
					Message: "is not an amount of " + base.Code, Fix: "Send a decimal with at most the currency's decimals, such as 5000.00"})
			} else {
				c.AmountAtLeast = m.String()
			}
		}
		read := func(name string, refs []RecordRef) []uuid.UUID {
			var ids []uuid.UUID
			for j, ref := range refs {
				id, err := uuid.Parse(string(ref))
				if err != nil {
					bad = append(bad, FieldProblem{Code: approval.CodeRules, Field: fmt.Sprintf("%s.conditions.%s[%d]", at, name, j),
						Message: "is not an ID", Fix: "Send the record's id"})
					continue
				}
				ids = append(ids, id)
			}
			return ids
		}
		c.Departments, c.Accounts, c.Vendors = read("departments", r.Conditions.Departments), read("accounts", r.Conditions.Accounts),
			read("vendors", r.Conditions.Vendors)
		out[i] = approval.Rule{Conditions: c, Steps: steps}
	}
	return out, bad
}

// rulesProblems are what is wrong with rules as the next policy for objectType: CheckRules, and records that are not
// a department's values, accounts of the chart or vendors.
func (a *API) rulesProblems(ctx context.Context, u storage.UnitOfWork, objectType string, rules []approval.Rule, base money.Currency) ([]FieldProblem, error) {
	ms, err := u.Members().List(ctx, a.deps.Clock.Now())
	if err != nil {
		return nil, err
	}
	members := make([]approval.Member, len(ms))
	for i, m := range ms {
		roles := memberRoles(m)
		members[i] = approval.Member{UserID: m.UserID, Roles: roles, Approves: approves(roles, objectType)}
	}
	var out []FieldProblem
	for _, x := range approval.CheckRules(approval.Rows[objectType], rules, base, func(role string) bool {
		return approves([]string{role}, objectType)
	}, members) {
		out = append(out, FieldProblem{Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
	}
	departments, err := departmentValues(ctx, u)
	if err != nil {
		return nil, err
	}
	for i, r := range rules {
		at := fmt.Sprintf("body.rules[%d].conditions", i)
		for j, id := range r.Conditions.Departments {
			if !slices.Contains(departments, id) {
				out = append(out, FieldProblem{Code: approval.CodeRules, Field: fmt.Sprintf("%s.departments[%d]", at, j),
					Message: "is not a value of the Department dimension", Fix: "Name a department from Settings › Dimensions"})
			}
		}
		for j, id := range r.Conditions.Accounts {
			if _, err := u.Accounts().Get(ctx, id); errors.Is(err, ports.ErrNotFound) {
				out = append(out, FieldProblem{Code: approval.CodeRules, Field: fmt.Sprintf("%s.accounts[%d]", at, j),
					Message: "is not an account of the chart", Fix: "Name an account from the chart of accounts"})
			} else if err != nil {
				return nil, err
			}
		}
		for j, id := range r.Conditions.Vendors {
			c, err := u.Contacts().Get(ctx, id)
			if err != nil && !errors.Is(err, ports.ErrNotFound) {
				return nil, err
			}
			if err != nil || !c.Vendor {
				out = append(out, FieldProblem{Code: approval.CodeRules, Field: fmt.Sprintf("%s.vendors[%d]", at, j),
					Message: "is not a vendor", Fix: "Name a contact marked as a vendor"})
			}
		}
	}
	return out, nil
}

// departmentValues are the values of the organisation's Department dimension.
func departmentValues(ctx context.Context, u storage.UnitOfWork) ([]uuid.UUID, error) {
	ds, err := u.Dimensions().List(ctx)
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, d := range ds {
		if d.Kind != storage.DimensionDepartment {
			continue
		}
		id := d.ID
		vs, err := u.Dimensions().Values(ctx, &id)
		if err != nil {
			return nil, err
		}
		for _, v := range vs {
			out = append(out, v.ID)
		}
	}
	return out, nil
}

// factsOf are what a change says about itself for the conditions: those it was given, and its amount from its one
// amount in the base currency when it was given none.
func factsOf(c pendingChange, base money.Currency) (approval.Facts, error) {
	f := c.Facts
	if f.Amount == nil && len(c.Amounts) == 1 && c.Amounts[0].Currency == base.Code {
		m, err := money.Parse(c.Amounts[0].Amount, base)
		if err != nil {
			return f, err
		}
		f.Amount = &m
	}
	return f, nil
}

// lineFacts are the accounts and dimension values of lines, each named once, for the policy's rules.
func lineFacts(accounts []string, values [][]DimensionValueRef, vendor string) approval.Facts {
	var f approval.Facts
	add := func(to *[]uuid.UUID, raw string) {
		if id, err := uuid.Parse(raw); err == nil && !slices.Contains(*to, id) {
			*to = append(*to, id)
		}
	}
	for _, a := range accounts {
		add(&f.Accounts, a)
	}
	for _, vs := range values {
		for _, v := range vs {
			add(&f.Values, string(v))
		}
	}
	if vendor != "" {
		add(&f.Vendors, vendor)
	}
	return f
}

// journalFacts are a journal's facts: its debits in the base currency, and its lines' accounts and values.
func journalFacts(ctx context.Context, u storage.UnitOfWork, j Journal, debits string) (approval.Facts, error) {
	var accounts []string
	var values [][]DimensionValueRef
	for _, l := range j.Lines {
		accounts, values = append(accounts, l.AccountID), append(values, l.DimensionValueIDs)
	}
	return withBase(ctx, u, lineFacts(accounts, values, ""), debits)
}

// billFacts are a bill's facts: its total in the base currency, its lines' accounts and values, and its supplier.
func billFacts(ctx context.Context, u storage.UnitOfWork, b Bill, payable string) (approval.Facts, error) {
	var accounts []string
	var values [][]DimensionValueRef
	for _, l := range b.Lines {
		accounts, values = append(accounts, l.AccountID), append(values, l.DimensionValueIDs)
	}
	vendor := ""
	if b.Supplier != nil {
		vendor = b.Supplier.ID
	}
	return withBase(ctx, u, lineFacts(accounts, values, vendor), payable)
}

// invoiceFacts are an invoice's facts: its total in the base currency, and its lines' accounts and values.
func invoiceFacts(ctx context.Context, u storage.UnitOfWork, i Invoice, receivable string) (approval.Facts, error) {
	var accounts []string
	var values [][]DimensionValueRef
	for _, l := range i.Lines {
		accounts, values = append(accounts, l.AccountID), append(values, l.DimensionValueIDs)
	}
	return withBase(ctx, u, lineFacts(accounts, values, ""), receivable)
}

// withBase is f with its amount, a decimal in the base currency; one that does not read leaves it without.
func withBase(ctx context.Context, u storage.UnitOfWork, f approval.Facts, amount string) (approval.Facts, error) {
	code, err := baseCode(ctx, u)
	if err != nil {
		return f, err
	}
	base, err := money.Lookup(code)
	if err != nil {
		return f, err
	}
	if m, err := money.Parse(amount, base); err == nil {
		f.Amount = &m
	}
	return f, nil
}

// orderFacts are a purchase order's facts: its total in the base currency, its lines' accounts and its supplier.
func orderFacts(o PurchaseOrder) (approval.Facts, error) {
	var accounts []string
	for _, l := range o.Lines {
		accounts = append(accounts, l.AccountID)
	}
	vendor := ""
	if o.Supplier != nil {
		vendor = o.Supplier.ID
	}
	f := lineFacts(accounts, nil, vendor)
	if o.TotalBase != nil {
		cur, err := money.Lookup(o.TotalBase.Currency)
		if err != nil {
			return f, err
		}
		m, err := money.Parse(o.TotalBase.Amount, cur)
		if err != nil {
			return f, err
		}
		f.Amount = &m
	}
	return f, nil
}
