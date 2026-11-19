package payroll

import (
	"sort"
	"strings"

	"github.com/cockroachdb/apd/v3"
)

// ItemFacts is a pay item of a person's month as the comparison reads it.
type ItemFacts struct {
	Key, SourceType string
	Class           string
	Amount          *apd.Decimal
}

// LineFacts is a person's month as the comparison reads it.
type LineFacts struct {
	Person, Name string
	Gross, Net   *apd.Decimal
	CPF          *apd.Decimal // employee and employer CPF together
	Cost         *apd.Decimal
	Items        []ItemFacts
}

// Cause kinds of a change since last month (NM-PAY-11).
const (
	CauseJoiner      = "joiner"
	CauseLeaver      = "leaver"
	CausePayChange   = "pay_change"
	CauseUnpaidLeave = "unpaid_leave"
	CauseOvertime    = "overtime"
	CauseOneOff      = "one_off"
	CauseAllowance   = "allowance"
	CauseDeduction   = "deduction"
	CauseCPF         = "cpf"
)

// Change is one explained difference from last month, linked to the item it comes from.
type Change struct {
	Person, Name string
	Cause        string
	Key          string
	SourceType   string
	Amount       *apd.Decimal // more or less than last month; for a joiner or a leaver, the whole gross pay
}

// Unexplained is a difference with no input behind it: it is listed for the approver.
type Unexplained struct {
	Person, Name, Why string
}

// Comparison is a run against the previous month's: totals and every change with its cause (NM-PAY-11).
type Comparison struct {
	GrossNow, GrossPrev, NetNow, NetPrev, CostNow, CostPrev *apd.Decimal
	Changes                                                 []Change
	Unexplained                                             []Unexplained
}

func total(ls []LineFacts, f func(LineFacts) *apd.Decimal) *apd.Decimal {
	sum := zero()
	for _, l := range ls {
		sum = add(sum, f(l))
	}
	return sum
}

func causeOf(it ItemFacts) string {
	switch {
	case it.Key == "basic":
		return CausePayChange
	case it.Class == "deduction":
		return CauseDeduction
	case strings.HasPrefix(it.Key, "unpaid-"):
		return CauseUnpaidLeave
	case strings.HasPrefix(it.Key, "ot-"):
		return CauseOvertime
	case it.SourceType == "pay_one_off":
		return CauseOneOff
	}
	return CauseAllowance
}

// Compare sets a run against the previous month's: the totals of gross pay, net pay and employer cost, and every difference with its cause and the item it comes from: a joiner, a leaver, a pay
// change, unpaid leave, overtime, a one-off item, an allowance or deduction that changed. A person whose CPF changed with no change in their wages is listed as unexplained (an age band or rate
// change, or an override), so the approver is told rather than left to find it.
func Compare(now, prev []LineFacts) Comparison {
	c := Comparison{
		GrossNow: total(now, func(l LineFacts) *apd.Decimal { return l.Gross }), GrossPrev: total(prev, func(l LineFacts) *apd.Decimal { return l.Gross }),
		NetNow: total(now, func(l LineFacts) *apd.Decimal { return l.Net }), NetPrev: total(prev, func(l LineFacts) *apd.Decimal { return l.Net }),
		CostNow: total(now, func(l LineFacts) *apd.Decimal { return l.Cost }), CostPrev: total(prev, func(l LineFacts) *apd.Decimal { return l.Cost }),
	}
	before := map[string]LineFacts{}
	for _, l := range prev {
		before[l.Person] = l
	}
	seen := map[string]bool{}
	for _, l := range now {
		seen[l.Person] = true
		p, had := before[l.Person]
		if !had {
			c.Changes = append(c.Changes, Change{Person: l.Person, Name: l.Name, Cause: CauseJoiner, Amount: l.Gross})
			continue
		}
		prevItems := map[string]ItemFacts{}
		for _, it := range p.Items {
			prevItems[it.Key] = it
		}
		wages := false
		nowItems := map[string]bool{}
		for _, it := range l.Items {
			nowItems[it.Key] = true
			was := prevItems[it.Key]
			delta := it.Amount
			if was.Amount != nil {
				delta = sub(it.Amount, was.Amount)
			}
			if delta.Sign() != 0 {
				wages = true
				c.Changes = append(c.Changes, Change{Person: l.Person, Name: l.Name, Cause: causeOf(it), Key: it.Key, SourceType: it.SourceType, Amount: delta})
			}
		}
		for _, it := range p.Items {
			if !nowItems[it.Key] && it.Amount.Sign() != 0 {
				wages = true
				c.Changes = append(c.Changes, Change{Person: l.Person, Name: l.Name, Cause: causeOf(it), Key: it.Key, SourceType: it.SourceType, Amount: sub(zero(), it.Amount)})
			}
		}
		if !wages && l.CPF.Cmp(p.CPF) != 0 {
			c.Changes = append(c.Changes, Change{Person: l.Person, Name: l.Name, Cause: CauseCPF, Amount: sub(l.CPF, p.CPF)})
			c.Unexplained = append(c.Unexplained, Unexplained{l.Person, l.Name, "CPF changed with no change in wages: the age band or a rate changed, or an override applies. Check it before approving."})
		}
	}
	for _, p := range prev {
		if !seen[p.Person] {
			c.Changes = append(c.Changes, Change{Person: p.Person, Name: p.Name, Cause: CauseLeaver, Amount: sub(zero(), p.Gross)})
		}
	}
	sort.SliceStable(c.Changes, func(i, j int) bool {
		if c.Changes[i].Name != c.Changes[j].Name {
			return c.Changes[i].Name < c.Changes[j].Name
		}
		return c.Changes[i].Key < c.Changes[j].Key
	})
	return c
}
