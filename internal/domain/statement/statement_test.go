package statement

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

func day(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }

// FR-AR-08: a statement's period runs forwards, at most two years, and ends today at the latest.
// req: FR-AR-08
func TestAStatementsPeriod(t *testing.T) {
	today := day(11, 5)
	for _, c := range []struct {
		from, to time.Time
		ok       bool
	}{
		{day(10, 1), day(10, 31), true},
		{day(10, 1), today, true},
		{day(10, 1), day(10, 1), true},
		{day(10, 31), day(10, 1), false},             // backwards
		{day(10, 1), day(11, 6), false},              // after today
		{day(10, 1).AddDate(-2, 0, 0), today, false}, // more than two years
		{time.Time{}, day(10, 31), false},
	} {
		err := CheckPeriod(c.from, c.to, today)
		if (err == nil) != c.ok {
			t.Errorf("%s to %s: %v", c.from.Format(time.DateOnly), c.to.Format(time.DateOnly), err)
		}
		if err != nil && err.(*Error).Code != CodeRange {
			t.Errorf("the code: %v", err)
		}
	}
}

// FR-AR-08: each line says in words what moved the balance; an invoice its due date.
// req: FR-AR-08
func TestAStatementsLinesInWords(t *testing.T) {
	sgd := money.MustLookup("SGD")
	for _, c := range []struct {
		l    Line
		want string
	}{
		{Line{Kind: KindInvoice, Number: "INV-0012", DueDate: day(11, 13)}, "Invoice INV-0012, due 13 Nov 2026"},
		{Line{Kind: KindReceipt, Number: "RC-0003"}, "Payment received RC-0003"},
		{Line{Kind: KindVoid, Number: "INV-0002"}, "Voided INV-0002"},
		{Line{Kind: KindWriteOff, Number: "WO-0001"}, "Written off WO-0001"},
	} {
		c.l.Amount = money.Zero(sgd)
		if got := c.l.Description(); got != c.want {
			t.Errorf("%s: %q, want %q", c.l.Kind, got, c.want)
		}
	}
}
