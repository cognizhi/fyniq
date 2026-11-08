package subledger

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// When a document was settled in full (FR-PLN-09; p1-planning design D7): 60.00 on 5 October and 40.00 on 15 October
// settle 100.00 on the 15th; a reversal of the 40.00 on the 20th opens it again, and 40.00 on the 25th settles it then.
// req: FR-PLN-09
func TestSettledInFull(t *testing.T) {
	sgd := money.MustLookup("SGD")
	owed, err := money.Parse("100.00", sgd)
	if err != nil {
		t.Fatal(err)
	}
	on := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	a := func(d int, amount string) storage.Allocation {
		return storage.Allocation{On: on(d), Currency: "SGD", Amount: storage.Decimal(amount)}
	}
	allocs := []storage.Allocation{a(5, "60.00"), a(15, "40.00"), a(20, "-40.00"), a(25, "40.00")}
	for _, c := range []struct {
		asAt int
		ok   bool
		on   int
	}{
		{12, false, 0},
		{15, true, 15},
		{19, true, 15},
		{22, false, 0},
		{31, true, 25},
	} {
		got, ok, err := SettledInFull(allocs, owed, on(c.asAt))
		if err != nil || ok != c.ok || (ok && !got.Equal(on(c.on))) {
			t.Errorf("as at %d October: %v %v %v", c.asAt, got, ok, err)
		}
	}
	if _, ok, _ := SettledInFull(allocs, money.Zero(sgd), on(31)); ok {
		t.Error("nothing owed is never settled")
	}
}
