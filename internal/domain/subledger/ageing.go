package subledger

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// Buckets are the ageing buckets by days past due on the day (FR-AP-06, FR-AR-10): not yet due (due on the day
// included), 1–30, 31–60, 61–90 and more than 90 days.
var Buckets = [5]string{"current", "1-30", "31-60", "61-90", "90+"}

// Bucket is the bucket of a document due on due, as at asAt; a document with no due date is current.
func Bucket(due, asAt time.Time) int {
	if due.IsZero() {
		return 0
	}
	days := int(asAt.Sub(due).Hours() / 24)
	switch {
	case days <= 0:
		return 0
	case days <= 30:
		return 1
	case days <= 60:
		return 2
	case days <= 90:
		return 3
	}
	return 4
}

// Row is what is open in one currency, by ageing bucket, with the credits apart (negative) and the total, and the
// carrying amount of it all in the base currency.
type Row struct {
	Currency money.Currency
	Buckets  [5]money.Money
	Credits  money.Money
	Total    money.Money
	Carrying money.Money
}

func newRow(c, base money.Currency) *Row {
	r := &Row{Currency: c, Credits: money.Zero(c), Total: money.Zero(c), Carrying: money.Zero(base)}
	for i := range r.Buckets {
		r.Buckets[i] = money.Zero(c)
	}
	return r
}

func (r *Row) add(it Item, asAt time.Time) error {
	var err error
	if it.Kind == KindCredit {
		r.Credits, err = r.Credits.Add(it.Open)
	} else {
		i := Bucket(it.DueDate, asAt)
		r.Buckets[i], err = r.Buckets[i].Add(it.Open)
	}
	if err != nil {
		return err
	}
	if r.Total, err = r.Total.Add(it.Open); err != nil {
		return err
	}
	r.Carrying, err = r.Carrying.Add(it.Carrying)
	return err
}

// GroupAgeing is one party's ageing, a row per currency.
type GroupAgeing struct {
	ID   uuid.UUID
	Name string
	Rows []Row
}

// Ageing is a sub-ledger's ageing as at a day: each party's open documents by days past due and credits by currency,
// and the totals by currency.
type Ageing struct {
	AsAt   time.Time
	Groups []GroupAgeing
	Totals []Row
}

type rows struct {
	base  money.Currency
	order []string
	by    map[string]*Row
}

func (rs *rows) of(c money.Currency) *Row {
	if rs.by == nil {
		rs.by = map[string]*Row{}
	}
	r := rs.by[c.Code]
	if r == nil {
		r = newRow(c, rs.base)
		rs.by[c.Code] = r
		rs.order = append(rs.order, c.Code)
	}
	return r
}

func (rs *rows) list() []Row {
	codes := slices.Clone(rs.order)
	slices.Sort(codes)
	out := make([]Row, 0, len(codes))
	for _, c := range codes {
		out = append(out, *rs.by[c])
	}
	return out
}

// Age is the ageing of groups' open items as at asAt, in base for the carrying amounts.
func Age(asAt time.Time, base money.Currency, groups []Group) (Ageing, error) {
	out := Ageing{AsAt: asAt}
	totals := rows{base: base}
	for _, g := range groups {
		mine := rows{base: base}
		for _, it := range g.Items {
			if err := mine.of(it.Open.Currency()).add(it, asAt); err != nil {
				return Ageing{}, err
			}
			if err := totals.of(it.Open.Currency()).add(it, asAt); err != nil {
				return Ageing{}, err
			}
		}
		out.Groups = append(out.Groups, GroupAgeing{ID: g.ID, Name: g.Name, Rows: mine.list()})
	}
	out.Totals = totals.list()
	return out, nil
}
