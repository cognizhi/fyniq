// Package projectbilling is what a project can bill its customer (PRD-001 FR-PRJ-10; design D9): its approved, billable, not yet billed
// time at billing rates (RND-TIME per entry), and the posted billable costs of its bills and claims at cost plus markup (RND-MARKUP).
// It reads; the invoice made from it is the ordinary invoice command, and the database marks the work billed when that invoice is posted.
package projectbilling

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/projectrate"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The error codes (ENG-23).
const (
	CodeInternal = "PRJ-12" // an internal project is never billed
	CodeNothing  = "PRJ-13" // nothing to bill, or a customer the project lacks
	CodeAccount  = "PRJ-14" // a revenue account or GST code the invoice needs and was not given
)

// Time is an approved, billable, unbilled time entry and what it is worth at the billing rate of its day.
type Time struct {
	EntryID uuid.UUID
	UserID  uuid.UUID
	Date    time.Time
	Minutes int
	Rate    *money.UnitPrice // nil when no billing rate covers the day: the entry cannot be invoiced yet
	Value   *money.Money
}

// Expense is a posted bill or claim line billed on to the customer: its cost before GST and the markup on it.
type Expense struct {
	DocumentID    uuid.UUID
	Line          int    // the line's place in its document, from 0
	Source        string // bill or claim
	Number        string
	Date          time.Time
	Label         string
	Cost          money.Money
	MarkupPercent string
	Markup        money.Money
	Billable      money.Money // cost plus markup
}

// Unbilled is what a project can bill now, in its currency.
type Unbilled struct {
	Currency      money.Currency
	Time          []Time    // priced, in date order
	Unpriced      []Time    // no billing rate covers the day
	Expenses      []Expense // in the project's currency
	OtherCurrency []Expense // in another currency: not on the invoice, which is in the project's
	TimeValue     money.Money
	ExpenseValue  money.Money
	Total         money.Money
}

// Of works out what project can bill now. Time is billed on a time-and-materials project only; a retainer draws it down and a fixed fee bills
// by milestone, so its costs alone are billable; an internal project bills nothing.
func Of(ctx context.Context, u storage.UnitOfWork, p storage.Project, base money.Currency) (Unbilled, error) {
	cur, err := money.Lookup(p.Currency)
	if err != nil {
		return Unbilled{}, err
	}
	out := Unbilled{Currency: cur, TimeValue: money.Zero(cur), ExpenseValue: money.Zero(cur), Total: money.Zero(cur)}
	if p.BillingMethod == storage.BillingInternal {
		return out, nil
	}
	done, err := u.Billing().Billed(ctx, p.ID)
	if err != nil {
		return out, err
	}
	billed := map[string]bool{}
	for _, b := range done {
		billed[key(b.Kind, b.RefID, b.Line)] = true
	}
	if p.BillingMethod == storage.BillingTimeAndMaterials {
		if err := timeOf(ctx, u, p, cur, billed, &out); err != nil {
			return out, err
		}
	}
	if err := expensesOf(ctx, u, p, cur, base, billed, &out); err != nil {
		return out, err
	}
	out.Total, err = out.TimeValue.Add(out.ExpenseValue)
	return out, err
}

func key(kind string, id uuid.UUID, line int) string { return fmt.Sprintf("%s/%s/%d", kind, id, line) }

func timeOf(ctx context.Context, u storage.UnitOfWork, p storage.Project, cur money.Currency, billed map[string]bool, out *Unbilled) error {
	es, err := u.Timesheets().Entries(ctx, storage.TimeFilter{ProjectID: p.ID, Status: storage.TimeApproved, Limit: 20000})
	if err != nil {
		return err
	}
	rates, err := u.Rates().BillingRates(ctx, p.ID, p.CustomerID)
	if err != nil {
		return err
	}
	costRates, err := u.Rates().CostRates(ctx)
	if err != nil {
		return err
	}
	roleOf := func(user uuid.UUID, day time.Time) string {
		best, found := storage.CostRate{}, false
		for _, r := range costRates {
			if r.UserID != nil && *r.UserID == user && !r.From.After(day) && (!found || r.From.After(best.From)) {
				best, found = r, true
			}
		}
		return best.Role
	}
	slices.SortFunc(es, func(a, b storage.TimeEntry) int {
		if c := a.WorkDate.Compare(b.WorkDate); c != 0 {
			return c
		}
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	for _, e := range es {
		if !e.Billable || billed[key(storage.BilledTime, e.ID, 0)] {
			continue
		}
		t := Time{EntryID: e.ID, UserID: e.UserID, Date: e.WorkDate, Minutes: e.Minutes}
		if br, ok := projectrate.Billing(rates, p.ID, p.CustomerID, e.UserID, roleOf(e.UserID, e.WorkDate), e.WorkDate); ok {
			price, err := money.ParseUnitPrice(string(br.Hourly), cur, "billing_rate")
			if err != nil {
				return err
			}
			v, err := rounding.Time(e.Minutes, price)
			if err != nil {
				return err
			}
			t.Rate, t.Value = &price, &v
			if out.TimeValue, err = out.TimeValue.Add(v); err != nil {
				return err
			}
			out.Time = append(out.Time, t)
			continue
		}
		out.Unpriced = append(out.Unpriced, t)
	}
	return nil
}

func expensesOf(ctx context.Context, u storage.UnitOfWork, p storage.Project, cur, base money.Currency, billed map[string]bool, out *Unbilled) error {
	var after *uuid.UUID
	for {
		ids, err := u.Billing().BillableDocuments(ctx, p.ID, after, 200)
		if err != nil {
			return err
		}
		for _, id := range ids {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			items, err := linesOf(ctx, u, doc, p.ID, base)
			if err != nil {
				return err
			}
			for _, x := range items {
				if billed[key(storage.BilledExpense, x.DocumentID, x.Line)] {
					continue
				}
				if x.Cost.Currency() != cur {
					out.OtherCurrency = append(out.OtherCurrency, x)
					continue
				}
				out.Expenses = append(out.Expenses, x)
				if out.ExpenseValue, err = out.ExpenseValue.Add(x.Billable); err != nil {
					return err
				}
			}
		}
		if len(ids) < 200 {
			return nil
		}
		after = &ids[len(ids)-1]
	}
}

// linesOf are a posted bill's or claim's billable lines that carry the project, with their cost before GST and markup.
func linesOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, project uuid.UUID, base money.Currency) ([]Expense, error) {
	var out []Expense
	add := func(line int, label string, date time.Time, cost money.Money, markup string, values []uuid.UUID) error {
		if !slices.Contains(values, project) {
			return nil
		}
		m := money.Zero(cost.Currency())
		if markup != "" {
			var err error
			if m, err = rounding.Markup(cost, markup); err != nil {
				return err
			}
		}
		total, err := cost.Add(m)
		if err != nil {
			return err
		}
		out = append(out, Expense{DocumentID: doc.ID, Line: line, Number: doc.Number, Date: date, Label: label, Cost: cost, MarkupPercent: markup, Markup: m, Billable: total})
		return nil
	}
	switch doc.Type {
	case storage.DocumentBill:
		d, err := bill.Read(doc, base)
		if err != nil {
			return nil, err
		}
		for i, l := range d.Lines {
			if !l.Billable {
				continue
			}
			if err := add(i, l.Description, d.Date, l.Net, l.Markup, l.Values); err != nil {
				return nil, err
			}
		}
		for i := range out {
			out[i].Source = "bill"
		}
	case storage.DocumentExpenseClaim:
		p, _, err := claim.Prepare(ctx, u, doc, base)
		if err != nil {
			return nil, err
		}
		for i, l := range p.Draft.Lines {
			if !l.Billable {
				continue
			}
			var cost money.Money
			label := l.Description
			if l.Kind == claim.LineMileage {
				t, ok := p.Trips[i]
				if !ok {
					continue
				}
				cost = t.Amount
				if label == "" {
					label = l.From + " to " + l.To
				}
			} else {
				c, err := l.Amount.Sub(l.GST) // the cost before GST; the invoice's own GST code decides the GST on the sale
				if err != nil {
					return nil, err
				}
				cost = c
				if label == "" {
					label = l.Merchant
				}
			}
			if err := add(i, label, l.Date, cost, l.Markup, l.Values); err != nil {
				return nil, err
			}
		}
		for i := range out {
			out[i].Source = "claim"
		}
	}
	return out, nil
}
