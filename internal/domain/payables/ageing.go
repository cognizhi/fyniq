package payables

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
)

// Buckets are the ageing buckets by days past due on the day (FR-AP-06, as FR-AR-10 for receivables): not yet due
// (due on the day included), 1–30, 31–60, 61–90 and more than 90 days.
var Buckets = subledger.Buckets

// Row is what is owed in one currency, by ageing bucket, with the supplier credits apart (negative) and the total, and
// the carrying amount of it all in the base currency.
type Row = subledger.Row

// SupplierAgeing is one supplier's ageing, a row per currency.
type SupplierAgeing = subledger.GroupAgeing

// Ageing is AP ageing as at a day (FR-AP-06): each supplier's open bills by days past due and credits by currency, and
// the totals by currency, from the sub-ledger.
type Ageing struct {
	AsAt      time.Time
	Suppliers []SupplierAgeing
	Totals    []Row
}

// Age is the ageing of a sub-ledger as at its day, in base for the carrying amounts.
func Age(l Ledger, base money.Currency) (Ageing, error) {
	a, err := subledger.Age(l.AsAt, base, l.groups())
	if err != nil {
		return Ageing{}, err
	}
	return Ageing{AsAt: a.AsAt, Suppliers: a.Groups, Totals: a.Totals}, nil
}

// DueItem is an open bill to pay, with its supplier.
type DueItem struct {
	SupplierID   uuid.UUID
	SupplierName string
	Item
}

// Requirement is the cash a currency needs: what is overdue on the day, what falls due within the window after it,
// their total, and its carrying amount in the base currency (at the bills' own rates: what a foreign bill costs when it
// is paid differs by the realised FX).
type Requirement struct {
	Currency money.Currency
	Overdue  money.Money
	Within   money.Money
	Total    money.Money
	Carrying money.Money
}

// ToPay are the open bills to pay as at a sub-ledger's day, by due date, that are overdue or fall due within days after
// it, and the cash they need by currency (FR-AP-06's bills to pay by due date and cash requirement). Supplier credits
// are not taken off: they are applied to a bill before it is paid.
func ToPay(l Ledger, base money.Currency, days int) ([]DueItem, []Requirement, error) {
	until := l.AsAt.AddDate(0, 0, days)
	var items []DueItem
	for _, s := range l.Suppliers {
		for _, it := range s.Items {
			if it.Kind != KindBill || it.Open.Sign() <= 0 || it.DueDate.After(until) {
				continue
			}
			items = append(items, DueItem{SupplierID: s.ID, SupplierName: s.Name, Item: it})
		}
	}
	slices.SortStableFunc(items, func(a, b DueItem) int {
		if c := a.DueDate.Compare(b.DueDate); c != 0 {
			return c
		}
		if c := strings.Compare(a.SupplierName, b.SupplierName); c != 0 {
			return c
		}
		return strings.Compare(a.Number, b.Number)
	})
	by := map[string]*Requirement{}
	var codes []string
	for _, it := range items {
		c := it.Open.Currency()
		r := by[c.Code]
		if r == nil {
			r = &Requirement{Currency: c, Overdue: money.Zero(c), Within: money.Zero(c), Total: money.Zero(c), Carrying: money.Zero(base)}
			by[c.Code] = r
			codes = append(codes, c.Code)
		}
		var err error
		if it.DueDate.Before(l.AsAt) {
			r.Overdue, err = r.Overdue.Add(it.Open)
		} else {
			r.Within, err = r.Within.Add(it.Open)
		}
		if err != nil {
			return nil, nil, err
		}
		if r.Total, err = r.Total.Add(it.Open); err != nil {
			return nil, nil, err
		}
		if r.Carrying, err = r.Carrying.Add(it.Carrying); err != nil {
			return nil, nil, err
		}
	}
	slices.Sort(codes)
	out := make([]Requirement, 0, len(codes))
	for _, c := range codes {
		out = append(out, *by[c])
	}
	return items, out, nil
}
