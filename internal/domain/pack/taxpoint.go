package pack

import (
	"fmt"
	"time"
)

// RuleEarlierOf is the time-of-supply rule of sg-gst@2026.1 (ENG-45): the earlier of the invoice and the
// payment received. A deposit that is part payment is a payment when received; a security deposit held
// pending the sale is a payment only from the day it is applied to the price (IRAS, "Time of supply
// rules"). A rule is named and versioned, so a different ruling is a new rule in a new pack version, not
// a change to this one.
const RuleEarlierOf = "sg-gst/earlier-of@1"

// Rules are the tax-point rules Fyniq knows.
var Rules = []string{RuleEarlierOf}

// Payment is money received for a supply.
type Payment struct {
	Received        time.Time
	SecurityDeposit bool       // held as security pending the sale, not yet part of the price
	Applied         *time.Time // the day a security deposit is applied to the price
}

// Supply is what fixes a supply's tax point: its invoice, if issued, and the payments received for it.
type Supply struct {
	Invoiced *time.Time
	Payments []Payment
}

// PaymentTaxPoint returns the day p counts as payment received under rule: the day received, or for a
// security deposit the day it is applied to the price. ok is false for a security deposit not yet applied.
func PaymentTaxPoint(rule string, p Payment) (time.Time, bool, error) {
	if rule != RuleEarlierOf {
		return time.Time{}, false, unknownRule(rule)
	}
	if p.SecurityDeposit {
		if p.Applied == nil {
			return time.Time{}, false, nil
		}
		return *p.Applied, true, nil
	}
	return p.Received, true, nil
}

// TaxPoint returns the time of supply of s under rule: the earliest of its invoice and the payments that
// count as received. ok is false while neither has happened. The GST rate, the registration status and
// the output-tax return period are then taken at this date (ENG-45); a deposit received before the
// invoice has its own tax point (PaymentTaxPoint).
func TaxPoint(rule string, s Supply) (time.Time, bool, error) {
	if rule != RuleEarlierOf {
		return time.Time{}, false, unknownRule(rule)
	}
	var at time.Time
	found := false
	consider := func(d time.Time) {
		if !found || d.Before(at) {
			at, found = d, true
		}
	}
	if s.Invoiced != nil {
		consider(*s.Invoiced)
	}
	for _, p := range s.Payments {
		if d, ok, _ := PaymentTaxPoint(rule, p); ok {
			consider(d)
		}
	}
	return at, found, nil
}

func unknownRule(rule string) error {
	return Problems{{CodeRule, "tax_point_rule", fmt.Sprintf("%q is not a rule Fyniq knows", rule), "Use a pack version whose tax-point rule Fyniq implements"}}
}
