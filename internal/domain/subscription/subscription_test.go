package subscription

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func day(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }

// A contract from 15 Jan at 100.00 a month, agreed to end on 31 Dec; an upgrade to 150.00 on 1 Apr, a downgrade to 120.00 on 1 Jul, a cancellation on
// 10 Sep and a renewal at 130.00 on 1 Nov: worked by hand, month by month (FR-AR-15, FR-KPI-02 contracted MRR).
func fixture() (storage.SubscriptionContract, []storage.SubscriptionAmendment) {
	end := day(time.December, 31)
	c := storage.SubscriptionContract{ID: uuid.New(), Currency: "SGD", StartsOn: day(time.January, 15), EndsOn: &end, MRR: "100.0000"}
	am := func(kind string, on time.Time, mrr string) storage.SubscriptionAmendment {
		return storage.SubscriptionAmendment{ContractID: c.ID, Kind: kind, EffectiveOn: on, MRR: storage.Decimal(mrr)}
	}
	return c, []storage.SubscriptionAmendment{ // deliberately out of order: the derivation sorts them
		am(storage.AmendRenew, day(time.November, 1), "130.0000"),
		am(storage.AmendUpgrade, day(time.April, 1), "150.0000"),
		am(storage.AmendCancel, day(time.September, 10), "0.0000"),
		am(storage.AmendDowngrade, day(time.July, 1), "120.0000"),
	}
}

// req: FR-AR-15
// req: FR-KPI-02
func TestAContractsMonthlyValueFollowsItsAmendmentsFromTheirDays(t *testing.T) {
	c, ams := fixture()
	for _, x := range []struct {
		on     time.Time
		status string
		mrr    string
	}{
		{day(time.January, 14), NotStarted, "0.00"},
		{day(time.January, 15), Active, "100.00"},
		{day(time.March, 31), Active, "100.00"},
		{day(time.April, 1), Active, "150.00"},
		{day(time.June, 30), Active, "150.00"},
		{day(time.July, 1), Active, "120.00"},
		{day(time.September, 9), Active, "120.00"},
		{day(time.September, 10), Ended, "0.00"},
		{day(time.October, 31), Ended, "0.00"},
		{day(time.November, 1), Active, "130.00"},
		{day(time.December, 31), Active, "130.00"},
		{time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC), Active, "130.00"}, // a renewal outlives the agreed end
	} {
		s, err := On(c, ams, x.on)
		if err != nil || s.Status != x.status || s.MRR.String() != x.mrr {
			t.Errorf("on %s: %v %s %s, want %s %s (%v)", x.on.Format(time.DateOnly), s.Status, s.MRR, "", x.status, x.mrr, err)
		}
	}
	// With no renewal the agreed end is the last day.
	only := ams[1:2]
	if s, _ := On(c, only, day(time.December, 31)); s.Status != Active || s.MRR.String() != "150.00" {
		t.Errorf("on the agreed end day: %+v", s)
	}
	if s, _ := On(c, only, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)); s.Status != Ended || s.MRR.String() != "0.00" {
		t.Errorf("the day after the agreed end: %+v", s)
	}
}

// req: FR-AR-15
func TestTheScheduleIsTheValueInForceOnEachMonthsLastDay(t *testing.T) {
	c, ams := fixture()
	got, err := Schedule(c, ams, day(time.December, 1))
	if err != nil || len(got) != 12 {
		t.Fatalf("the schedule: %d months, %v", len(got), err)
	}
	want := []string{"100.00", "100.00", "100.00", "150.00", "150.00", "150.00", "120.00", "120.00", "0.00", "0.00", "130.00", "130.00"}
	for i, m := range got {
		if m.MRR.String() != want[i] {
			t.Errorf("%s: %s, want %s", m.Month.Format("Jan"), m.MRR, want[i])
		}
	}
	if got[3].Amendment == nil || got[3].Amendment.Kind != storage.AmendUpgrade || got[4].Amendment != nil || got[8].Amendment.Kind != storage.AmendCancel {
		t.Errorf("the amendments by month: %+v %+v %+v", got[3].Amendment, got[4].Amendment, got[8].Amendment)
	}
	if got[8].Status != Ended || got[0].Status != Active {
		t.Errorf("statuses: %s %s", got[0].Status, got[8].Status)
	}
}
