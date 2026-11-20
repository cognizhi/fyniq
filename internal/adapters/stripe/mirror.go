package stripe

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The typed view of the objects Model B mirrors (PRD-001 FR-PSP-03, FR-PSP-07; p1-payment-providers 4.1): an invoice with its lines and
// collection state, a customer, a subscription. Written from Stripe's API reference, "The Invoice object" and its line item, "The
// Subscription object" and "The Customer object" (docs.stripe.com/api, retrieved 20 Nov 2026) at API version 2025-09-30.clover; every field
// name is [live-verify] against a test-mode account. Amounts are Stripe's integer minor units, converted by the currency's scale; nothing is a
// float (ENG-10).

func objectOf(m map[string]any, k string) map[string]any {
	v, _ := m[k].(map[string]any)
	return v
}

func listOf(m map[string]any, k string) ([]any, bool) {
	l := objectOf(m, k)
	if l == nil {
		return nil, false
	}
	data, _ := l["data"].([]any)
	more, _ := l["has_more"].(bool)
	return data, more
}

func ptrTime(m map[string]any, k string) *time.Time {
	if t := seconds(m, k); !t.IsZero() {
		return &t
	}
	return nil
}

// invoiceData reads an invoice. An invoice the invoice rules cannot take is still mirrored, with why in Unsupported: a person decides it.
func invoiceData(doc map[string]any, cur string) (*ports.InvoiceData, error) {
	inv := &ports.InvoiceData{CustomerID: str(doc, "customer"), CustomerName: str(doc, "customer_name"), CustomerEmail: str(doc, "customer_email"), Number: str(doc, "number"), Status: str(doc, "status"), HostedURL: str(doc, "hosted_invoice_url"),
		NextAttempt: ptrTime(doc, "next_payment_attempt"), Due: ptrTime(doc, "due_date")}
	if n, ok := doc["attempt_count"].(json.Number); ok {
		if v, err := strconv.Atoi(n.String()); err == nil {
			inv.AttemptCount = v
		}
	}
	if st := objectOf(doc, "status_transitions"); st != nil {
		inv.Finalized = ptrTime(st, "finalized_at")
	}
	if p := objectOf(doc, "parent"); p != nil {
		if d := objectOf(p, "subscription_details"); d != nil {
			inv.SubscriptionID = str(d, "subscription")
		}
	}
	if inv.SubscriptionID == "" {
		inv.SubscriptionID = str(doc, "subscription") // before the clover API version
	}
	for field, dst := range map[string]*string{"total": &inv.Total, "amount_paid": &inv.AmountPaid} {
		n, err := minor(doc, field)
		if err != nil {
			return nil, err
		}
		if *dst, err = Decimal(n, cur); err != nil {
			return nil, err
		}
	}
	lines, more := listOf(doc, "lines")
	var sum, tax int64
	var why []string
	for _, x := range lines {
		l, _ := x.(map[string]any)
		n, err := minor(l, "amount")
		if err != nil {
			return nil, err
		}
		amount, err := Decimal(n, cur)
		if err != nil {
			return nil, err
		}
		line := ports.InvoiceLine{ID: str(l, "id"), Description: str(l, "description"), Amount: amount}
		if pr := objectOf(l, "period"); pr != nil {
			line.PeriodStart, line.PeriodEnd = seconds(pr, "start"), seconds(pr, "end")
		}
		if n < 0 {
			why = append(why, "a credit line")
		}
		if d, _ := l["discount_amounts"].([]any); len(d) > 0 {
			why = append(why, "a discount")
		}
		sum += n
		inv.Lines = append(inv.Lines, line)
	}
	if more {
		why = append(why, "more lines than one page")
	}
	if d, _ := doc["total_discount_amounts"].([]any); len(d) > 0 {
		why = append(why, "a discount")
	}
	taxes, _ := doc["total_taxes"].([]any)
	for _, x := range taxes {
		if t, _ := x.(map[string]any); t != nil {
			n, err := minor(t, "amount")
			if err != nil {
				return nil, err
			}
			tax += n
		}
	}
	total, _ := minor(doc, "total")
	if len(lines) == 0 {
		why = append(why, "no lines")
	} else if sum+tax != total && len(why) == 0 {
		why = append(why, "lines and tax that do not add up to its total")
	}
	if len(why) > 0 {
		seen := map[string]bool{}
		var uniq []string
		for _, w := range why {
			if !seen[w] {
				seen[w] = true
				uniq = append(uniq, w)
			}
		}
		inv.Unsupported = strings.Join(uniq, ", ")
	}
	return inv, nil
}

// customerData reads a customer.
func customerData(doc map[string]any) *ports.CustomerData {
	return &ports.CustomerData{Name: str(doc, "name"), Email: str(doc, "email")}
}

// subscriptionData reads a subscription: what its flat-priced items bill each interval (the first item's interval), its standing and period. An item
// with no flat price (tiered or metered) adds nothing: the subscription shows what Fyniq can say of it, and its invoices carry the rest.
func subscriptionData(doc map[string]any, cur string) (*ports.SubscriptionData, error) {
	s := &ports.SubscriptionData{CustomerID: str(doc, "customer"), Status: str(doc, "status"), CanceledAt: ptrTime(doc, "canceled_at")}
	s.CancelAtPeriodEnd, _ = doc["cancel_at_period_end"].(bool)
	s.PeriodStart, s.PeriodEnd = ptrTime(doc, "current_period_start"), ptrTime(doc, "current_period_end")
	items, _ := listOf(doc, "items")
	var total int64
	for _, x := range items {
		it, _ := x.(map[string]any)
		price := objectOf(it, "price")
		rec := objectOf(price, "recurring")
		if price == nil || rec == nil {
			continue
		}
		unit, ok := rec["interval"].(string)
		if !ok {
			continue
		}
		count := 1
		if n, ok := rec["interval_count"].(json.Number); ok {
			if v, err := strconv.Atoi(n.String()); err == nil && v > 0 {
				count = v
			}
		}
		if s.IntervalUnit == "" {
			s.IntervalUnit, s.IntervalCount = unit, count
		}
		if unit != s.IntervalUnit || count != s.IntervalCount {
			continue // another interval: not added to this subscription's amount
		}
		if s.PeriodStart == nil {
			s.PeriodStart, s.PeriodEnd = ptrTime(it, "current_period_start"), ptrTime(it, "current_period_end")
		}
		if price["unit_amount"] == nil {
			continue
		}
		unitAmount, err := minor(price, "unit_amount")
		if err != nil {
			return nil, err
		}
		qty := int64(1)
		if n, ok := it["quantity"].(json.Number); ok {
			if v, err := strconv.ParseInt(n.String(), 10, 64); err == nil && v >= 0 {
				qty = v
			}
		}
		total += unitAmount * qty
	}
	if s.IntervalUnit == "" {
		s.IntervalUnit, s.IntervalCount = "month", 1
	}
	var err error
	if s.Amount, err = Decimal(total, cur); err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	return s, nil
}

var _ = money.Zero
