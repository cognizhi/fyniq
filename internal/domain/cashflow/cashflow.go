// Package cashflow is the short-term cash flow (FR-PLN-09, FR-PLN-05; p1-planning design D7): a projection, day by day
// from today, of the money accounts' balance as the open invoices and bills, the recurring documents' next occurrences
// and the approved payment batches move it, each on the day it is expected; its lowest point; the buffer line; and the
// same days in weeks. It is sandboxed (FR-PLN-08): reading it, or asking a what-if of it, saves nothing.
package cashflow

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The horizons a projection is read for, in days (FR-PLN-09), and the weekly view's weeks (FR-PLN-05).
var Horizons = []int{30, 60, 90, 180}

const (
	DefaultHorizon = 60
	Weeks          = 13
)

// The kinds of item.
const (
	KindInvoice          = "invoice"
	KindBill             = "bill"
	KindRecurringInvoice = "recurring_invoice"
	KindRecurringBill    = "recurring_bill"
	KindBatch            = "payment_batch"
)

// Why an item is on its day.
const (
	BasisHistory   = "history"   // its due date + the contact's average days late
	BasisDue       = "due"       // its due date: the contact has no history
	BasisPlanned   = "planned"   // a bill's planned payment date: the supplier has no history
	BasisOverdue   = "overdue"   // it was expected before today, so today
	BasisScheduled = "scheduled" // a batch's payment date
	BasisMoved     = "moved"     // a what-if moved it
)

// Item is one expected movement of the money accounts: its source, the day it is expected and why, and its amount in the
// base currency, money in above zero and money out below.
type Item struct {
	Kind        string
	ID          uuid.UUID // the document, the recurring document or the batch
	Occurrence  time.Time // a recurring document's occurrence; zero otherwise
	Number      string    // the document's number, the recurring document's name
	Contact     uuid.UUID
	ContactName string
	DueDate     time.Time // zero for a batch
	On          time.Time
	Basis       string
	DaysLate    int // BasisHistory: the contact's average days late
	Amount      money.Money
}

// Key names an item for a what-if: "<id>" for a document or a batch, "<id>@YYYY-MM-DD" for a recurring occurrence.
func (it Item) Key() string {
	if it.Occurrence.IsZero() {
		return it.ID.String()
	}
	return it.ID.String() + "@" + it.Occurrence.Format(time.DateOnly)
}

// Excluded is an input the projection does not hold: what and why.
type Excluded struct {
	What, Why string
}

// Day is one day of the projection: the money in and out on it and the balance at its end.
type Day struct {
	On             time.Time
	In, Out, Close money.Money
}

// Week is seven days of the projection, the first from today.
type Week struct {
	From, To       time.Time
	In, Out, Close money.Money
}

// Projection is the short-term cash flow.
type Projection struct {
	Today   time.Time
	Days    int
	Opening money.Money // the money accounts' balance today, in the base currency
	Items   []Item      // by day, then kind, then number
	Daily   []Day
	Weekly  []Week // the first Weeks weeks, when the projection runs that far
	// The lowest balance at a day's end, today's included, and the first day it occurs.
	Lowest   money.Money
	LowestOn time.Time
	Buffer   Buffer
	// BelowOn is the first day whose balance closes below the buffer line; nil when none does, or there is no line.
	BelowOn  *time.Time
	Excluded []Excluded
}

// Buffer is the buffer line (design D7): Months × the average monthly net burn of the last closed months, rounded once;
// Line is nil when the company is not burning, or no month is closed yet, which Why says.
type Buffer struct {
	Months int
	Burn   *money.Money // the average monthly net burn it rests on
	Line   *money.Money
	Why    string
}

// Project lays the items out day by day from today for days days: an item before today is today's, one after the last
// day is left out. It is a pure function of its inputs.
func Project(today time.Time, days int, opening money.Money, items []Item, buffer Buffer, excluded []Excluded) (Projection, error) {
	if !slices.Contains(Horizons, days) && days != Weeks*7 {
		return Projection{}, fmt.Errorf("cashflow: %d days is not a horizon", days)
	}
	cur := opening.Currency()
	out := Projection{Today: today, Days: days, Opening: opening, Buffer: buffer, Excluded: excluded}
	last := today.AddDate(0, 0, days-1)
	byDay := map[time.Time][2]money.Money{}
	for _, it := range items {
		if it.Amount.Currency() != cur {
			return Projection{}, fmt.Errorf("cashflow: %s %s is in %s, not %s", it.Kind, it.Number, it.Amount.Currency().Code, cur.Code)
		}
		if it.On.Before(today) {
			it.On = today
		}
		if it.On.After(last) || it.Amount.IsZero() {
			continue
		}
		out.Items = append(out.Items, it)
		io, seen := byDay[it.On]
		if !seen {
			io = [2]money.Money{money.Zero(cur), money.Zero(cur)}
		}
		var err error
		if it.Amount.Sign() > 0 {
			io[0], err = io[0].Add(it.Amount)
		} else {
			io[1], err = io[1].Sub(it.Amount)
		}
		if err != nil {
			return Projection{}, err
		}
		byDay[it.On] = io
	}
	slices.SortStableFunc(out.Items, func(a, b Item) int {
		if d := a.On.Compare(b.On); d != 0 {
			return d
		}
		if d := strings.Compare(a.Kind, b.Kind); d != 0 {
			return d
		}
		return strings.Compare(a.Number, b.Number)
	})
	balance := opening
	for d := today; !d.After(last); d = d.AddDate(0, 0, 1) {
		io, ok := byDay[d]
		if !ok {
			io = [2]money.Money{money.Zero(cur), money.Zero(cur)}
		}
		var err error
		if balance, err = balance.Add(io[0]); err != nil {
			return Projection{}, err
		}
		if balance, err = balance.Sub(io[1]); err != nil {
			return Projection{}, err
		}
		out.Daily = append(out.Daily, Day{On: d, In: io[0], Out: io[1], Close: balance})
		if d.Equal(today) || balance.Decimal().Cmp(out.Lowest.Decimal()) < 0 {
			out.Lowest, out.LowestOn = balance, d
		}
		if l := buffer.Line; l != nil && out.BelowOn == nil && balance.Decimal().Cmp(l.Decimal()) < 0 {
			on := d
			out.BelowOn = &on
		}
	}
	for w := 0; w < Weeks && (w+1)*7 <= len(out.Daily); w++ {
		wk := Week{From: out.Daily[w*7].On, To: out.Daily[w*7+6].On, In: money.Zero(cur), Out: money.Zero(cur), Close: out.Daily[w*7+6].Close}
		for _, d := range out.Daily[w*7 : w*7+7] {
			var err error
			if wk.In, err = wk.In.Add(d.In); err != nil {
				return Projection{}, err
			}
			if wk.Out, err = wk.Out.Add(d.Out); err != nil {
				return Projection{}, err
			}
		}
		out.Weekly = append(out.Weekly, wk)
	}
	return out, nil
}

// Move is a what-if's change to one item: to another day, or left out (FR-PLN-09, design D7).
type Move struct {
	Key   string // Item.Key
	To    *time.Time
	Leave bool
}

// WhatIf is items with the moves applied; a move that names no item is in unknown.
func WhatIf(items []Item, moves []Move) (out []Item, unknown []string) {
	by := map[string]Move{}
	for _, m := range moves {
		by[m.Key] = m
	}
	seen := map[string]bool{}
	for _, it := range items {
		m, ok := by[it.Key()]
		if !ok {
			out = append(out, it)
			continue
		}
		seen[it.Key()] = true
		if m.Leave {
			continue
		}
		if m.To != nil {
			it.On, it.Basis = *m.To, BasisMoved
		}
		out = append(out, it)
	}
	for _, m := range moves {
		if !seen[m.Key] {
			unknown = append(unknown, m.Key)
		}
	}
	return out, unknown
}

// AverageLate is a contact's average days late over its settlements (design D7, rev 0.214): days late = the days from
// the due date to the day it was settled in full, at least 0, so an early payment counts as on time; their mean is
// rounded half away from zero to whole days.
func AverageLate(late []int) int {
	if len(late) == 0 {
		return 0
	}
	sum := 0
	for _, d := range late {
		sum += max(d, 0)
	}
	var q apd.Decimal
	c := apd.BaseContext.WithPrecision(30)
	c.Rounding = apd.RoundHalfUp
	if _, err := c.Quo(&q, apd.New(int64(sum), 0), apd.New(int64(len(late)), 0)); err != nil {
		return 0
	}
	var r apd.Decimal
	if _, err := c.Quantize(&r, &q, 0); err != nil {
		return 0
	}
	n, _ := r.Int64()
	return int(n)
}
