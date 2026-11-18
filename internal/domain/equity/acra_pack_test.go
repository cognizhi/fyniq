package equity

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/packs"
)

// The controllers' register keeps its own periods (p1-company-secretary design D1, non-goal): they must stay equal to
// sg-acra's, so the register and the company secretary's checks never disagree on a day. The yearly notice is due a
// year after the last, as the pack's 12 months.
// req: FR-CS-02
// req: FR-EQ-25
func TestTheControllersPeriodsEqualTheACRAPack(t *testing.T) {
	v, err := pack.Load(packs.FS, "sg-acra/2026.1")
	if err != nil {
		t.Fatal(err)
	}
	on := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		rule  string
		count int
		unit  string
	}{
		{"controller_register", CompanyRegisterDays, pack.UnitDays},
		{"controller_central", CentralRegisterDays, pack.UnitBusinessDays},
		{"controller_notice", 12, pack.UnitMonths},
	} {
		p, err := pack.PeriodOn(v, c.rule, on)
		if err != nil || p.Count != c.count || p.Unit != c.unit {
			t.Errorf("%s: the pack says %d %s (%v); the register counts %d %s", c.rule, p.Count, p.Unit, err, c.count, c.unit)
		}
	}
	entered := time.Date(2027, 4, 9, 0, 0, 0, 0, time.UTC)
	_, due, err := pack.AcraDue(v, "controller_central", entered, pack.Weekdays)
	if err != nil || !due.Equal(BusinessDaysAfter(entered, CentralRegisterDays)) {
		t.Fatalf("the pack gives %v (%v); the register %v", due, err, BusinessDaysAfter(entered, CentralRegisterDays))
	}
	sent := time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)
	if _, due, err := pack.AcraDue(v, "controller_notice", sent, pack.Weekdays); err != nil || !due.Equal(sent.AddDate(1, 0, 0)) {
		t.Fatalf("the pack's next notice is %v (%v); the register's %v", due, err, sent.AddDate(1, 0, 0))
	}
}
