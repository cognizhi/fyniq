// Package taxfilings works the corporate income tax figures the tax agent needs from the books (PRD-001 FR-TAX-06, p2-tax-filings design D1): the tax-adjusted
// profit, the chargeable income, the start-up or partial exemption, the tax and the rebate of the Year of Assessment, each from the sg-cit pack and exact. It posts
// nothing and files nothing: the paper is the tax agent's working, with the reason of every adjustment.
package taxfilings

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// The kinds of adjustment to the profit before tax, which the tax agent enters with a reason and evidence (no account carries an automatic tax tag).
const (
	NonDeductible    = "non_deductible"               // an expense the Act does not allow: added back
	CapitalAllowance = "capital_allowance_difference" // the capital allowances less the depreciation: a negative amount takes the allowances off
	OtherAdjustment  = "other"
)

var work = apd.BaseContext.WithPrecision(60)

// Adjustment is a change to the profit before tax: an amount to add (positive) or take off (negative), with the reason and whether evidence is attached.
type Adjustment struct {
	Kind     string
	Amount   string // a decimal in Singapore dollars
	Reason   string
	Evidence bool
}

// Input is a financial year's figures.
type Input struct {
	YA                int    // the Year of Assessment
	ProfitBeforeTax   string // the year's profit (negative for a loss), from the ledger
	Adjustments       []Adjustment
	Scheme            string // pack.SchemeStartUp or pack.SchemePartial
	CashGrantEligible bool   // the company met the cash grant's local employee condition
}

// Paper is the working, in exact decimals; nothing is rounded but TaxPayable, once, to the cent.
type Paper struct {
	YA                int
	ProfitBeforeTax   string
	Adjustments       string // their net
	ChargeableIncome  string // never below zero
	LossForward       string // the part of a loss the year leaves, when the adjusted profit is negative
	Exempt            string
	NormalChargeable  string
	Rate              string
	TaxBeforeRebate   string // exact
	Rebate            string
	TaxPayable        string // exact, before the cash grant is paid
	TaxPayableRounded string // to the cent, half away from zero [accountant to confirm]
	PackVersion       string
}

// norm writes an exact decimal without trailing zeros but with at least the cents: 16787.5 is 16787.50 and 2098.5639 stays as it is.
func norm(d *apd.Decimal) string {
	var r apd.Decimal
	if _, _, err := apd.BaseContext.WithPrecision(60).Reduce(&r, d); err != nil {
		return d.Text('f')
	}
	if r.Exponent > -2 {
		var q apd.Decimal
		if _, err := apd.BaseContext.WithPrecision(60).Quantize(&q, &r, -2); err == nil {
			return q.Text('f')
		}
	}
	return r.Text('f')
}

// Problem is an adjustment or an input the paper refuses, in plain words (ENG-23).
type Problem struct{ Field, Message string }

func (p Problem) Error() string { return p.Field + ": " + p.Message }

// Work is the tax-adjusted profit working paper of a year (FR-TAX-06). cit is the sg-cit version in force for the YA. An adjustment with no reason, an unknown kind or an
// amount that is not a decimal is refused.
func Work(cit pack.Version, in Input) (Paper, []Problem) {
	var bad []Problem
	dec := func(field, s string) *apd.Decimal {
		d, _, err := apd.NewFromString(strings.TrimSpace(s))
		if err != nil || d.Form != apd.Finite {
			bad = append(bad, Problem{field, fmt.Sprintf("%q is not an amount in dollars", s)})
			return apd.New(0, 0)
		}
		return d
	}
	profit := dec("profit_before_tax", in.ProfitBeforeTax)
	net := apd.New(0, 0)
	for i, a := range in.Adjustments {
		f := fmt.Sprintf("adjustments[%d]", i)
		if !slices.Contains([]string{NonDeductible, CapitalAllowance, OtherAdjustment}, a.Kind) {
			bad = append(bad, Problem{f + ".kind", fmt.Sprintf("%q is not a kind of adjustment", a.Kind)})
		}
		if strings.TrimSpace(a.Reason) == "" {
			bad = append(bad, Problem{f + ".reason", "an adjustment says why: the tax agent signs the working"})
		}
		if _, err := work.Add(net, net, dec(f+".amount", a.Amount)); err != nil {
			bad = append(bad, Problem{f + ".amount", err.Error()})
		}
	}
	if in.Scheme != pack.SchemeStartUp && in.Scheme != pack.SchemePartial {
		bad = append(bad, Problem{"scheme", "the start-up exemption (sute) or the partial exemption (pte)"})
	}
	if len(bad) > 0 {
		return Paper{}, bad
	}
	p := Paper{YA: in.YA, ProfitBeforeTax: norm(profit), Adjustments: norm(net), PackVersion: cit.ID}
	adjusted := new(apd.Decimal)
	if _, err := work.Add(adjusted, profit, net); err != nil {
		return Paper{}, []Problem{{"profit_before_tax", err.Error()}}
	}
	chargeable, loss := new(apd.Decimal).Set(adjusted), apd.New(0, 0)
	if adjusted.Sign() < 0 {
		loss.Abs(adjusted)
		chargeable = apd.New(0, 0)
	}
	p.ChargeableIncome, p.LossForward = norm(chargeable), norm(loss)
	rate, err := pack.CITRate(cit, in.YA)
	if err != nil {
		return p, []Problem{{"ya", fmt.Sprintf("%s holds no rate for YA %d", cit.ID, in.YA)}}
	}
	exempt, err := pack.CITExemption(cit, in.Scheme, in.YA, chargeable)
	if err != nil {
		return p, []Problem{{"scheme", fmt.Sprintf("%s holds no %s exemption for YA %d", cit.ID, in.Scheme, in.YA)}}
	}
	normal := new(apd.Decimal)
	if _, err := work.Sub(normal, chargeable, exempt); err != nil {
		return p, []Problem{{"exempt", err.Error()}}
	}
	tax := new(apd.Decimal)
	if _, err := work.Mul(tax, normal, rate); err != nil {
		return p, []Problem{{"rate", err.Error()}}
	}
	rebate, err := pack.CITRebate(cit, in.YA, tax, in.CashGrantEligible)
	if err != nil && err != pack.ErrNoYA {
		return p, []Problem{{"rebate", err.Error()}}
	}
	if err == pack.ErrNoYA {
		rebate = apd.New(0, 0) // a YA without an announced rebate has none
	}
	payable := new(apd.Decimal)
	if _, err := work.Sub(payable, tax, rebate); err != nil {
		return p, []Problem{{"rebate", err.Error()}}
	}
	rounded := new(apd.Decimal)
	if _, err := apd.BaseContext.WithPrecision(30).Quantize(rounded, payable, -2); err != nil {
		return p, []Problem{{"tax", err.Error()}}
	}
	p.Exempt, p.NormalChargeable, p.Rate = norm(exempt), norm(normal), rate.Text('f')
	p.TaxBeforeRebate, p.Rebate, p.TaxPayable, p.TaxPayableRounded = norm(tax), norm(rebate), norm(payable), rounded.Text('f')
	return p, nil
}
