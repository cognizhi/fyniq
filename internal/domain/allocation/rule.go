package allocation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// More stable codes (ENG-23).
const (
	CodeRule    = "ALLOC-04" // the rule's name, source accounts, key or targets are not usable
	CodePeriod  = "ALLOC-05" // the months asked for are not whole months of the books
	CodeUsage   = "ALLOC-06" // the usage file is missing, names no target, or its billed total is not the supplier's bill
	CodeRun     = "ALLOC-07" // the months were allocated by this rule already
	CodeNothing = "ALLOC-08" // the source accounts hold nothing without a dimension value in the months, so there is nothing to split
)

// The keys of a rule.
const (
	KeyFixed     = "fixed"
	KeyHeadcount = "headcount"
	KeyUsage     = "usage"
)

// Target is where a part of the cost goes: the dimension values the allocated lines carry (at most one of each dimension), and the weight the key gives it.
type Target struct {
	Label   string      `json:"label"`
	Values  []uuid.UUID `json:"values"`
	Percent string      `json:"percent,omitempty"` // fixed percentages: above nothing, at most two decimals
	Code    string      `json:"code,omitempty"`    // usage: the code the usage file names it by
}

// Rule is an allocation rule (FR-DIM-03): the source expense accounts whose cost with no dimension value is split over the targets by the key. It is saved as a document
// of type allocation_rule, a new version at each change; a rule archived is not run again, and its runs stay.
type Rule struct {
	Name     string      `json:"name"`
	Accounts []uuid.UUID `json:"accounts"`
	Key      string      `json:"key"`
	Targets  []Target    `json:"targets"`
	Archived bool        `json:"archived,omitempty"`
}

// Usage is an imported usage file (a cloud or AI-model provider's export, reduced to a code, a quantity and what the row was billed): its billed total must equal the
// supplier's bill it explains, or it is refused. A row whose code names no target is what the key cannot place.
type Usage struct {
	BillID      uuid.UUID `json:"bill_id,omitempty"`
	BilledTotal string    `json:"billed_total"`
	Rows        []Row     `json:"rows"`
}

// Row is one line of a usage file.
type Row struct {
	Code     string `json:"code"`
	Quantity string `json:"quantity"`
	Billed   string `json:"billed"`
}

// Encode is the rule as saved.
func Encode(r Rule) ([]byte, error) { return json.Marshal(r) }

// Read is the rule a document holds, with its version.
func Read(doc storage.Document) (Rule, error) {
	if doc.Type != storage.DocumentAllocationRule {
		return Rule{}, ports.ErrNotFound
	}
	var r Rule
	if err := json.Unmarshal(doc.Payload, &r); err != nil {
		return Rule{}, fmt.Errorf("the saved allocation rule cannot be read: %w", err)
	}
	return r, nil
}

func problem(field, message, fix string) ledger.Problem {
	return ledger.Problem{Code: CodeRule, Field: field, Message: message, Fix: fix}
}

// Check is every problem with a rule (ENG-22), in plain words: the name, the source accounts (active expense accounts that take postings), the key and the targets (their dimension
// values in use, at most one of each dimension, the percentages or usage codes the key needs, and the departments the headcount key reads).
func Check(ctx context.Context, u storage.UnitOfWork, r Rule) (ledger.Problems, error) {
	var out ledger.Problems
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" || len(r.Name) > 80 {
		out = append(out, problem("name", "the name is empty or longer than 80 characters", "Name the rule, such as Cloud hosting by department"))
	}
	if len(r.Accounts) == 0 || len(r.Accounts) > 20 {
		out = append(out, problem("accounts", "a rule splits one to twenty accounts", "Choose the expense accounts whose cost is shared"))
	}
	for i, id := range r.Accounts {
		a, err := u.Accounts().Get(ctx, id)
		switch {
		case err != nil && !isNotFound(err):
			return nil, err
		case err != nil || a.Type != storage.Expense || a.Group || !a.Active:
			out = append(out, problem(fmt.Sprintf("accounts[%d]", i), "that is not an active expense account that takes postings", "Choose an expense account in use"))
		}
		if slices.Index(r.Accounts, id) != i {
			out = append(out, problem(fmt.Sprintf("accounts[%d]", i), "that account is named twice", "Name each account once"))
		}
	}
	if r.Key != KeyFixed && r.Key != KeyHeadcount && r.Key != KeyUsage {
		out = append(out, problem("key", "the key is fixed percentages, headcount or usage", "Choose one of them"))
	}
	if len(r.Targets) == 0 || len(r.Targets) > 50 {
		out = append(out, problem("targets", "a rule has one to fifty targets", "Add the departments, projects or other values the cost goes to"))
	}
	values, err := u.Dimensions().Values(ctx, nil)
	if err != nil {
		return nil, err
	}
	dims, err := u.Dimensions().List(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]storage.DimensionValue{}
	for _, v := range values {
		byID[v.ID] = v
	}
	kindOf := map[uuid.UUID]string{}
	for _, d := range dims {
		kindOf[d.ID] = d.Kind
	}
	var percents []string
	codes := map[string]bool{}
	for i, t := range r.Targets {
		f := fmt.Sprintf("targets[%d]", i)
		if strings.TrimSpace(t.Label) == "" || len(t.Label) > 80 {
			out = append(out, problem(f+".label", "the target's label is empty or longer than 80 characters", "Label it, such as Engineering"))
		}
		if len(t.Values) == 0 || len(t.Values) > 8 {
			out = append(out, problem(f+".values", "a target names one to eight dimension values", "Choose the department, project or other value the cost goes to"))
		}
		seen := map[uuid.UUID]bool{}
		for _, id := range t.Values {
			v, ok := byID[id]
			switch {
			case !ok || !v.Active:
				out = append(out, problem(f+".values", "a dimension value is not in use", "Choose a value in use, or put it back in use under dimensions"))
			case seen[v.DimensionID]:
				out = append(out, problem(f+".values", "a target names two values of one dimension", "A line carries one value of each dimension"))
			}
			if ok {
				seen[v.DimensionID] = true
			}
		}
		switch r.Key {
		case KeyFixed:
			percents = append(percents, t.Percent)
		case KeyHeadcount:
			department := false
			for _, id := range t.Values {
				if kindOf[byID[id].DimensionID] == storage.DimensionDepartment {
					department = true
				}
			}
			if !department {
				out = append(out, problem(f+".values", "the headcount key counts people by department, so each target names a department", "Add the target's department value"))
			}
		case KeyUsage:
			c := strings.TrimSpace(t.Code)
			if c == "" || len(c) > 80 || codes[c] {
				out = append(out, problem(f+".code", "a usage target has a code, which the file names it by, and no other target has it", "Give the target the code the usage file uses for it"))
			}
			codes[c] = true
		}
	}
	if r.Key == KeyFixed && len(r.Targets) > 0 {
		if _, _, err := PercentUnits(percents); err != nil {
			out = append(out, problem("targets", err.Error(), "Give each target a percentage; together at most 100"))
		}
	}
	return out, nil
}

func isNotFound(err error) bool { return errors.Is(err, ports.ErrNotFound) }

// Create saves a rule as a document, its first version.
func Create(ctx context.Context, u storage.UnitOfWork, r Rule) (storage.Document, error) {
	r = tidy(r)
	if probs, err := Check(ctx, u, r); err != nil || len(probs) > 0 {
		if err != nil {
			return storage.Document{}, err
		}
		return storage.Document{}, probs
	}
	b, err := Encode(r)
	if err != nil {
		return storage.Document{}, err
	}
	return u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentAllocationRule, Payload: b})
}

// Revise saves the next version of a rule.
func Revise(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, r Rule) (storage.Document, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return doc, err
	}
	if _, err := Read(doc); err != nil {
		return doc, err
	}
	r = tidy(r)
	if !r.Archived {
		if probs, err := Check(ctx, u, r); err != nil || len(probs) > 0 {
			if err != nil {
				return doc, err
			}
			return doc, probs
		}
	}
	b, err := Encode(r)
	if err != nil {
		return doc, err
	}
	return u.Documents().Revise(ctx, id, b)
}

func tidy(r Rule) Rule {
	r.Name = strings.TrimSpace(r.Name)
	for i := range r.Targets {
		r.Targets[i].Label = strings.TrimSpace(r.Targets[i].Label)
		r.Targets[i].Percent = strings.TrimSpace(r.Targets[i].Percent)
		r.Targets[i].Code = strings.TrimSpace(r.Targets[i].Code)
	}
	return r
}

// AccountSplit is one source account's cost in the months and how it splits.
type AccountSplit struct {
	Account     storage.Account
	Source      money.Money
	Parts       []money.Money // each target's, in the rule's order
	Unallocated money.Money
}

// Report is a rule run over whole months: what each source account held with no dimension value, how the key weighed the targets, how each account split, and the journal's lines.
type Report struct {
	Rule     Rule
	From, To time.Time
	Splits   []AccountSplit
	// Weights are each target's weight in the key's own unit (hundredths of a percent, people, millionths of a unit of usage) and Unplaced the weight the key could not place.
	Weights  []int64
	Unplaced int64
	Lines    []ledger.JournalLine
}

// months resolves the first and last day of whole months to their periods.
func months(ctx context.Context, u storage.UnitOfWork, from, to time.Time) ([]uuid.UUID, error) {
	if from.Day() != 1 || to.Day() != time.Date(to.Year(), to.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day() || to.Before(from) || to.Sub(from) > 366*24*time.Hour {
		return nil, &Error{CodePeriod, "dates", "the months run from the first day of one to the last day of the same or a later month, at most a year", "Choose whole months, such as 1 Mar to 31 Mar"}
	}
	all, err := u.Periods().List(ctx)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for m := from; !m.After(to); m = m.AddDate(0, 1, 0) {
		found := false
		for _, p := range all {
			if p.StartsOn.Equal(m) {
				ids, found = append(ids, p.ID), true
			}
		}
		if !found {
			return nil, &Error{CodePeriod, "dates", "the books have no period for " + m.Format("January 2006"), "Choose months the books have periods for"}
		}
	}
	return ids, nil
}

// Compute runs a rule over whole months without saving anything: each source account's cost with no dimension value (read through the balance service, ENG-04), the key's weights, and
// the split by ALLOC-PRORATA with what the key cannot place left unallocated. The usage file is for the usage key alone, and the headcount key reads the people projection as at the last day.
func Compute(ctx context.Context, u storage.UnitOfWork, doc storage.Document, from, to time.Time, usage *Usage) (Report, error) {
	rule, err := Read(doc)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Rule: rule, From: from, To: to}
	if rule.Archived {
		return rep, &Error{CodeRule, "rule", "the rule is archived", "Save it again without archiving it, or make a new rule"}
	}
	if probs, err := Check(ctx, u, rule); err != nil || len(probs) > 0 {
		if err != nil {
			return rep, err
		}
		return rep, probs
	}
	periods, err := months(ctx, u, from, to)
	if err != nil {
		return rep, err
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return rep, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return rep, err
	}
	runs, err := Runs(ctx, u, doc.ID)
	if err != nil {
		return rep, err
	}
	for _, r := range runs {
		if r.From.Equal(from) {
			return rep, &Error{CodeRun, "dates", "this rule allocated " + from.Format("January 2006") + " already", "Open its journal; reverse it if it was wrong"}
		}
	}
	// What the key weighs.
	if rep.Weights, rep.Unplaced, err = weights(ctx, u, rule, to, usage, base); err != nil {
		return rep, err
	}
	// The cost to split: the source accounts' postings with no dimension value, so a line already tagged is never moved again.
	tb, err := balance.TrialAt(ctx, u, base, balance.Current.Only(func(values []uuid.UUID) bool { return len(values) == 0 }), periods...)
	if err != nil {
		return rep, err
	}
	held := map[uuid.UUID]money.Money{}
	for _, row := range tb.Rows {
		held[row.AccountID] = row.Base
	}
	for _, id := range rule.Accounts {
		src, ok := held[id]
		if !ok || src.IsZero() {
			continue
		}
		acct, err := u.Accounts().Get(ctx, id)
		if err != nil {
			return rep, err
		}
		res, err := Split(src, rep.Weights, rep.Unplaced)
		if err != nil {
			return rep, err
		}
		rep.Splits = append(rep.Splits, AccountSplit{Account: acct, Source: src, Parts: res.Parts, Unallocated: res.Unallocated})
	}
	if len(rep.Splits) == 0 {
		return rep, &Error{CodeNothing, "accounts", "the source accounts hold no cost without a dimension value in those months", "Post the shared cost first, or choose other months or accounts"}
	}
	// The journal: each part debited to its account carrying the target's values, the whole of what was placed credited to the account with none, so the unplaced stays where it was.
	for _, s := range rep.Splits {
		placed := money.Zero(base)
		for i, p := range s.Parts {
			if p.IsZero() {
				continue
			}
			rep.Lines = append(rep.Lines, ledger.JournalLine{AccountID: s.Account.ID, Amount: p, Description: rule.Targets[i].Label, Values: slices.Clone(rule.Targets[i].Values)})
			if placed, err = placed.Add(p); err != nil {
				return rep, err
			}
		}
		if !placed.IsZero() {
			rep.Lines = append(rep.Lines, ledger.JournalLine{AccountID: s.Account.ID, Amount: placed.Neg(), Description: "Allocated: " + rule.Name})
		}
	}
	if len(rep.Lines) == 0 {
		return rep, &Error{CodeNothing, "accounts", "the key places none of the cost on a target, so the journal would move nothing", "Check the weights, or the usage file's codes"}
	}
	return rep, nil
}

// weights reads the key's weights for the targets, and the weight it cannot place.
func weights(ctx context.Context, u storage.UnitOfWork, rule Rule, to time.Time, usage *Usage, base money.Currency) ([]int64, int64, error) {
	switch rule.Key {
	case KeyFixed:
		percents := make([]string, len(rule.Targets))
		for i, t := range rule.Targets {
			percents[i] = t.Percent
		}
		return PercentUnits(percents)
	case KeyHeadcount:
		people, err := u.PeopleLinks().People(ctx)
		if err != nil {
			return nil, 0, err
		}
		units := make([]int64, len(rule.Targets))
		var unplaced int64
		for _, p := range people {
			if p.Removed || (p.StartedOn != nil && p.StartedOn.After(to)) || (p.LeftOn != nil && !p.LeftOn.After(to)) {
				continue
			}
			placed := false
			for i, t := range rule.Targets {
				if p.DepartmentValue != uuid.Nil && slices.Contains(t.Values, p.DepartmentValue) {
					units[i]++
					placed = true
				}
			}
			if !placed && p.DepartmentValue == uuid.Nil {
				unplaced++
			}
		}
		return units, unplaced, nil
	default:
		if usage == nil || len(usage.Rows) == 0 {
			return nil, 0, &Error{CodeUsage, "usage", "the usage key needs the usage file", "Send the file's rows: a code, a quantity and what each was billed"}
		}
		if err := checkBilled(ctx, u, *usage, base); err != nil {
			return nil, 0, err
		}
		byCode := map[string]int{}
		for i, t := range rule.Targets {
			byCode[t.Code] = i
		}
		units := make([]int64, len(rule.Targets))
		var unplaced int64
		for i, row := range usage.Rows {
			q, err := QuantityUnits([]string{row.Quantity})
			if err != nil {
				return nil, 0, &Error{CodeUsage, fmt.Sprintf("usage.rows[%d]", i), fmt.Sprintf("row %d's quantity %q is not zero or more with at most six decimals", i+1, row.Quantity), "Enter the usage as a plain number"}
			}
			if t, ok := byCode[strings.TrimSpace(row.Code)]; ok {
				units[t] += q[0]
			} else {
				unplaced += q[0]
			}
		}
		return units, unplaced, nil
	}
}

// checkBilled refuses a usage file whose rows are not billed what it says, or whose billed total is not the supplier's bill it explains (FR-DIM-03).
func checkBilled(ctx context.Context, u storage.UnitOfWork, usage Usage, base money.Currency) error {
	total, err := money.Parse(strings.TrimSpace(usage.BilledTotal), base)
	if err != nil {
		return &Error{CodeUsage, "usage.billed_total", "the billed total is not an amount in " + base.Code, "Enter what the supplier billed, as the file's total"}
	}
	sum := money.Zero(base)
	for i, row := range usage.Rows {
		b, err := money.Parse(strings.TrimSpace(row.Billed), base)
		if err != nil {
			return &Error{CodeUsage, fmt.Sprintf("usage.rows[%d].billed", i), fmt.Sprintf("row %d's billed amount is not an amount in %s", i+1, base.Code), "Enter what the row was billed"}
		}
		if sum, err = sum.Add(b); err != nil {
			return err
		}
	}
	if !sum.Equal(total) {
		return &Error{CodeUsage, "usage.billed_total", fmt.Sprintf("the rows add up to %s but the file says %s was billed", sum, total), "Fix the rows or the total: the file is refused until they agree"}
	}
	if usage.BillID == uuid.Nil {
		return &Error{CodeUsage, "usage.bill_id", "the file does not say which supplier's bill it explains", "Name the bill the usage file explains"}
	}
	doc, err := u.Documents().Get(ctx, usage.BillID)
	if err != nil {
		if isNotFound(err) {
			return &Error{CodeUsage, "usage.bill_id", "there is no such bill", "Name a bill of the organisation"}
		}
		return err
	}
	d, err := bill.Read(doc, base)
	if err != nil {
		return &Error{CodeUsage, "usage.bill_id", "that is not a bill in " + base.Code, "Name a bill in " + base.Code}
	}
	billTotal, err := d.Total()
	if err != nil {
		return err
	}
	if !billTotal.Equal(total) {
		return &Error{CodeUsage, "usage.billed_total", fmt.Sprintf("the file bills %s but the supplier's bill is %s", total, billTotal), "The file's billed total must equal the bill it explains: check that you have the right file and bill"}
	}
	return nil
}

// Run is a rule's recorded run over whole months, saved with its draft journal.
type Run struct {
	DocumentID uuid.UUID
	RuleID     uuid.UUID
	RuleVer    int
	From, To   time.Time
	JournalID  uuid.UUID
	Key        string
}

type runPayload struct {
	Date      string    `json:"date"`
	RuleID    uuid.UUID `json:"rule_id"`
	RuleVer   int       `json:"rule_version"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	JournalID uuid.UUID `json:"journal_id"`
	Key       string    `json:"key"`
	Splits    []struct {
		Account     uuid.UUID `json:"account"`
		Source      string    `json:"source"`
		Parts       []string  `json:"parts"`
		Unallocated string    `json:"unallocated"`
	} `json:"splits"`
	Weights  []int64 `json:"weights"`
	Unplaced int64   `json:"unplaced"`
}

// Runs are a rule's recorded runs, earliest first.
func Runs(ctx context.Context, u storage.UnitOfWork, rule uuid.UUID) ([]Run, error) {
	docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentAllocationRun, Limit: 1000})
	if err != nil {
		return nil, err
	}
	var out []Run
	for _, d := range docs {
		var p runPayload
		if json.Unmarshal(d.Payload, &p) != nil || p.RuleID != rule {
			continue
		}
		from, _ := time.Parse(time.DateOnly, p.From)
		to, _ := time.Parse(time.DateOnly, p.To)
		out = append(out, Run{DocumentID: d.ID, RuleID: p.RuleID, RuleVer: p.RuleVer, From: from, To: to, JournalID: p.JournalID, Key: p.Key})
	}
	slices.SortFunc(out, func(a, b Run) int { return a.From.Compare(b.From) })
	return out, nil
}

// Propose makes the run's journal as a draft for a person to approve and records the run, appended to the rule at once so it is never changed: Dr each account for each target's part with
// the target's values, Cr the account for what was placed. Nothing is posted here; the journal posts like any other, on an approver's say (FR-DIM-03).
func Propose(ctx context.Context, u storage.UnitOfWork, doc storage.Document, from, to time.Time, usage *Usage) (Report, storage.Document, error) {
	rep, err := Compute(ctx, u, doc, from, to, usage)
	if err != nil {
		return rep, storage.Document{}, err
	}
	memo := "Allocation of " + rep.Rule.Name + ", " + from.Format("January 2006")
	if !from.Equal(time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)) {
		memo = "Allocation of " + rep.Rule.Name + ", " + from.Format("Jan 2006") + " to " + to.Format("Jan 2006")
	}
	j, err := journal.CreateIn(ctx, u, journal.Draft{Date: to, Memo: memo + " (" + rep.Rule.Key + " key)", Lines: rep.Lines})
	if err != nil {
		return rep, storage.Document{}, err
	}
	p := runPayload{Date: to.Format(time.DateOnly), RuleID: doc.ID, RuleVer: doc.Version, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly), JournalID: j.ID, Key: rep.Rule.Key,
		Weights: rep.Weights, Unplaced: rep.Unplaced}
	for _, s := range rep.Splits {
		parts := make([]string, len(s.Parts))
		for i, x := range s.Parts {
			parts[i] = x.String()
		}
		p.Splits = append(p.Splits, struct {
			Account     uuid.UUID `json:"account"`
			Source      string    `json:"source"`
			Parts       []string  `json:"parts"`
			Unallocated string    `json:"unallocated"`
		}{s.Account.ID, s.Source.String(), parts, s.Unallocated.String()})
	}
	b, err := json.Marshal(p)
	if err != nil {
		return rep, storage.Document{}, err
	}
	run, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentAllocationRun, Payload: b})
	if err != nil {
		return rep, storage.Document{}, err
	}
	if err := u.Documents().SetStatus(ctx, run.ID, storage.DocumentPosted); err != nil { // appended: a run is never changed
		return rep, storage.Document{}, err
	}
	return rep, j, nil
}
