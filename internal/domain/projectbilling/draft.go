package projectbilling

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Error is a draft the project cannot make, with its stable code and the fix (ENG-23).
type Error struct{ Code, Message, Fix string }

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// ExpenseRef names a line of a posted bill or claim to bill.
type ExpenseRef struct {
	DocumentID uuid.UUID
	Line       int
}

// DraftOptions say what a draft invoice of a project's unbilled work bills, and on what.
type DraftOptions struct {
	AccountID uuid.UUID // the revenue account its lines earn
	GSTCode   string
	Day       time.Time // the invoice's date
	// TimeEntries and Expenses narrow what is billed; none of either means all that can be billed.
	TimeEntries []uuid.UUID
	Expenses    []ExpenseRef
	Names       map[uuid.UUID]string // the people's names, for a line's description
}

// InvoiceDraft is the draft invoice of a time-and-materials project's unbilled work (FR-PRJ-10): time a person and a rate a line, a cost a
// line, each carrying the project, and what it bills for the database to mark when the invoice is posted. The same function makes it for a
// person's request (the API) and for an approved AI proposal (the executor), so the two never differ.
func InvoiceDraft(ctx context.Context, u storage.UnitOfWork, pr storage.Project, base money.Currency, o DraftOptions) (invoice.Draft, error) {
	if pr.BillingMethod == storage.BillingInternal {
		return invoice.Draft{}, &Error{CodeInternal, "An internal project is never billed.", "Bill a customer's project instead"}
	}
	if pr.CustomerID == nil {
		return invoice.Draft{}, &Error{CodeNothing, "The project has no customer to bill.", "Set the project's customer first"}
	}
	un, err := Of(ctx, u, pr, base)
	if err != nil {
		return invoice.Draft{}, err
	}
	cur := un.Currency
	code := o.GSTCode
	if code == "" {
		code = "SR"
	}
	wantTime, wantExp := map[uuid.UUID]bool{}, map[string]bool{}
	for _, t := range o.TimeEntries {
		wantTime[t] = true
	}
	for _, x := range o.Expenses {
		wantExp[fmt.Sprintf("%s/%d", x.DocumentID, x.Line)] = true
	}
	one, err := money.ParseQuantity("1", "quantity")
	if err != nil {
		return invoice.Draft{}, err
	}
	d := invoice.Draft{CustomerID: *pr.CustomerID, Date: o.Day, Currency: cur, Reference: pr.Code}
	billing := &invoice.ProjectBilling{ProjectID: pr.ID}
	line := func(desc string, amount money.Money) error {
		price, err := money.ParseUnitPrice(amount.String(), cur, "unit_price")
		if err != nil {
			return err
		}
		d.Lines = append(d.Lines, invoice.Line{Description: desc, Quantity: one, UnitPrice: price, AccountID: o.AccountID, GSTCode: code, Values: []uuid.UUID{pr.ID}})
		return nil
	}
	// Time: a line a person and a rate, in date order, its amount the sum of its entries' RND-TIME values.
	type group struct {
		user    uuid.UUID
		rate    string
		minutes int
		from    time.Time
		to      time.Time
		value   money.Money
	}
	var groups []*group
	for _, t := range un.Time {
		if len(wantTime) > 0 && !wantTime[t.EntryID] {
			continue
		}
		var g *group
		for _, x := range groups {
			if x.user == t.UserID && x.rate == t.Rate.String() {
				g = x
			}
		}
		if g == nil {
			g = &group{user: t.UserID, rate: t.Rate.String(), from: t.Date, to: t.Date, value: money.Zero(cur)}
			groups = append(groups, g)
		}
		g.minutes += t.Minutes
		g.to = t.Date
		if g.value, err = g.value.Add(*t.Value); err != nil {
			return d, err
		}
		billing.TimeEntries = append(billing.TimeEntries, t.EntryID)
	}
	for _, g := range groups {
		desc := fmt.Sprintf("%s, %s to %s: %s at %s %s an hour", o.Names[g.user], g.from.Format(time.DateOnly), g.to.Format(time.DateOnly), HoursText(g.minutes), g.rate, cur.Code)
		if err := line(desc, g.value); err != nil {
			return d, err
		}
	}
	for _, x := range un.Expenses {
		if len(wantExp) > 0 && !wantExp[fmt.Sprintf("%s/%d", x.DocumentID, x.Line)] {
			continue
		}
		desc := x.Label
		if x.MarkupPercent != "" && x.MarkupPercent != "0" {
			desc += fmt.Sprintf(" (cost plus %s%% markup)", x.MarkupPercent)
		}
		if err := line(strings.TrimSpace(desc), x.Billable); err != nil {
			return d, err
		}
		billing.Expenses = append(billing.Expenses, invoice.BilledExpense{DocumentID: x.DocumentID, Line: x.Line})
	}
	if len(d.Lines) == 0 {
		return d, &Error{CodeNothing, "Nothing is ready to bill on the project.", "Approve its time, enter a billing rate for it, or mark costs billable"}
	}
	slices.SortStableFunc(billing.TimeEntries, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
	d.ProjectBilling = billing
	return d, nil
}

// HoursText is minutes as "9 h 30 min".
func HoursText(minutes int) string {
	neg := minutes < 0
	if neg {
		minutes = -minutes
	}
	s := fmt.Sprintf("%d h %02d min", minutes/60, minutes%60)
	if neg {
		return "-" + s
	}
	return s
}
