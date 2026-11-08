package cashflow

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

var sgd = money.MustLookup("SGD")

func day(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }

func amt(s string) money.Money {
	m, err := money.Parse(s, sgd)
	if err != nil {
		panic(err)
	}
	return m
}

// The projection worked by hand. On 2 Nov 2026 the money accounts hold 10,000.00. A bill of 500.00 was expected on
// 20 October, so today; a bill of 8,000.00 is expected on 5 November, an invoice of 3,000.00 on 10 November, a batch of
// 4,000.00 on 20 November, and a recurring invoice's occurrence of 1,000.00 on 1 February 2027, beyond 30 days. The
// balance closes 9,500.00 on the 2nd, 1,500.00 on the 5th, 4,500.00 on the 10th and 500.00 from the 20th: the lowest,
// first on the 20th. A buffer line of 2,000.00 is first crossed on the 5th.
// req: FR-PLN-09
// req: FR-PLN-05
func TestTheProjection(t *testing.T) {
	today := day(11, 2)
	bill := Item{Kind: KindBill, ID: uuid.New(), Number: "BILL-0008", On: day(11, 5), Basis: BasisDue, Amount: amt("-8000.00")}
	late := Item{Kind: KindBill, ID: uuid.New(), Number: "BILL-0005", On: day(10, 20), Basis: BasisOverdue, Amount: amt("-500.00")}
	inv := Item{Kind: KindInvoice, ID: uuid.New(), Number: "INV-0003", On: day(11, 10), Basis: BasisHistory, DaysLate: 7, Amount: amt("3000.00")}
	batch := Item{Kind: KindBatch, ID: uuid.New(), Number: "PB-0001", On: day(11, 20), Basis: BasisScheduled, Amount: amt("-4000.00")}
	later := Item{Kind: KindRecurringInvoice, ID: uuid.New(), Occurrence: time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC), Number: "Retainer",
		On: time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC), Amount: amt("1000.00")}
	line := amt("2000.00")
	p, err := Project(today, 30, amt("10000.00"), []Item{inv, bill, later, batch, late}, Buffer{Months: 6, Line: &line}, NotYet)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Daily) != 30 || !p.Daily[29].On.Equal(day(12, 1)) {
		t.Fatalf("the days: %d to %v", len(p.Daily), p.Daily[len(p.Daily)-1].On)
	}
	if len(p.Items) != 4 || p.Items[0].Number != "BILL-0005" || !p.Items[0].On.Equal(today) || p.Items[1].Number != "BILL-0008" ||
		p.Items[2].Number != "INV-0003" || p.Items[3].Number != "PB-0001" {
		t.Errorf("the items: %+v", p.Items)
	}
	for _, c := range []struct {
		on             time.Time
		in, out, close string
	}{
		{day(11, 2), "0.00", "500.00", "9500.00"},
		{day(11, 4), "0.00", "0.00", "9500.00"},
		{day(11, 5), "0.00", "8000.00", "1500.00"},
		{day(11, 10), "3000.00", "0.00", "4500.00"},
		{day(11, 20), "0.00", "4000.00", "500.00"},
		{day(12, 1), "0.00", "0.00", "500.00"},
	} {
		d := p.Daily[int(c.on.Sub(today).Hours()/24)]
		if d.In.String() != c.in || d.Out.String() != c.out || d.Close.String() != c.close {
			t.Errorf("%v: in %s, out %s, close %s; want %s, %s, %s", c.on, d.In, d.Out, d.Close, c.in, c.out, c.close)
		}
	}
	if p.Lowest.String() != "500.00" || !p.LowestOn.Equal(day(11, 20)) || p.BelowOn == nil || !p.BelowOn.Equal(day(11, 5)) {
		t.Errorf("the lowest %s on %v, below the buffer on %v", p.Lowest, p.LowestOn, p.BelowOn)
	}
	if len(p.Weekly) != 4 || len(p.Excluded) != 3 {
		t.Errorf("30 days give four whole weeks: %d; and the inputs not yet: %d", len(p.Weekly), len(p.Excluded))
	}

	// Thirteen weeks: 91 days.
	w, err := Project(today, 91, amt("10000.00"), []Item{inv, bill, batch, late}, Buffer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Weekly) != Weeks || w.BelowOn != nil {
		t.Fatalf("the weeks: %d, below %v", len(w.Weekly), w.BelowOn)
	}
	for i, c := range []struct{ from, to, in, out, close string }{
		{"2026-11-02", "2026-11-08", "0.00", "8500.00", "1500.00"},
		{"2026-11-09", "2026-11-15", "3000.00", "0.00", "4500.00"},
		{"2026-11-16", "2026-11-22", "0.00", "4000.00", "500.00"},
	} {
		x := w.Weekly[i]
		if x.From.Format(time.DateOnly) != c.from || x.To.Format(time.DateOnly) != c.to || x.In.String() != c.in || x.Out.String() != c.out ||
			x.Close.String() != c.close {
			t.Errorf("week %d: %+v", i+1, x)
		}
	}
	if last := w.Weekly[12]; last.To.Format(time.DateOnly) != "2027-01-31" || last.Close.String() != "500.00" {
		t.Errorf("the 13th week: %+v", last)
	}

	// A what-if: the 8,000.00 bill delayed to the 25th and the batch left out. The balance closes 9,500.00 from the 2nd,
	// 12,500.00 from the 10th and 4,500.00 from the 25th, the lowest; nothing is below the buffer.
	moved := day(11, 25)
	items, unknown := WhatIf([]Item{inv, bill, batch, late}, []Move{{Key: bill.Key(), To: &moved}, {Key: batch.Key(), Leave: true}, {Key: "no-such"}})
	if len(unknown) != 1 || unknown[0] != "no-such" || len(items) != 3 {
		t.Fatalf("the what-if: %d items, unknown %v", len(items), unknown)
	}
	q, err := Project(today, 30, amt("10000.00"), items, Buffer{Line: &line}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if q.Lowest.String() != "4500.00" || !q.LowestOn.Equal(moved) || q.BelowOn != nil || q.Items[2].Basis != BasisMoved {
		t.Errorf("after the what-if: lowest %s on %v, below %v, %+v", q.Lowest, q.LowestOn, q.BelowOn, q.Items)
	}

	if _, err := Project(today, 45, amt("0.00"), nil, Buffer{}, nil); err == nil {
		t.Error("45 days is no horizon")
	}
	if _, err := Project(today, 30, amt("0.00"), []Item{{Kind: KindInvoice, On: today, Amount: money.Zero(money.MustLookup("USD"))}}, Buffer{}, nil); err == nil {
		t.Error("an item in another currency")
	}
}

// The average days late, rounded half away from zero, an early payment counting as on time (design D7, rev 0.214).
// req: FR-PLN-09
func TestAverageLate(t *testing.T) {
	for _, c := range []struct {
		late []int
		want int
	}{
		{[]int{10, 4}, 7},
		{[]int{-8}, 0},
		{[]int{1, 2}, 2},
		{[]int{0, 0, 4}, 1},
		{[]int{-5, 6}, 3},
		{nil, 0},
	} {
		if got := AverageLate(c.late); got != c.want {
			t.Errorf("%v: %d, want %d", c.late, got, c.want)
		}
	}
}
