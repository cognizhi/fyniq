package leave

import (
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// Kinds of sick leave (NM-LV-12): outpatient and hospitalisation leave share one ceiling, the hospitalisation days including the outpatient ones.
const (
	Outpatient      = "outpatient"
	Hospitalisation = "hospitalisation"
)

// Ledger types of sick leave: each holds the year's ceiling as it has stepped up, and its uses. An outpatient day is taken from both, so the shared ceiling holds.
const (
	SickOutpatientType = "sick_outpatient"
	SickHospitalType   = "sick_hospital"
)

// SickAllocation is how a notification's days fall against the year's ceilings.
type SickAllocation struct {
	Paid, Unpaid Days
	Outpatient   Days // the outpatient ceiling in force on the last day counted
	Hospital     Days // the hospitalisation ceiling in force on the last day counted
}

// SickAllocate takes each counted day in order against the ceiling in force on that day (MOM: entitlements step up at 3, 4, 5 and 6 months' service, on the date of
// absence), with the days already used in the year. A day within the ceiling is paid sick leave; a day beyond it, or before three months' service, is unpaid
// and left for the people team to decide on. An outpatient day also uses the hospitalisation ceiling; a half day costs half.
func SickAllocate(v pack.Version, start time.Time, kind string, counted []Counted, usedOutpatient, usedHospital Days) SickAllocation {
	var a SickAllocation
	for _, c := range counted {
		d, err := time.Parse(time.DateOnly, c.Date)
		if err != nil {
			continue
		}
		out, hosp, _ := pack.SickLeave(v, completedMonths(start, d), d)
		a.Outpatient, a.Hospital = Days(out)*100, Days(hosp)*100
		avail := a.Hospital - usedHospital
		if kind == Outpatient {
			avail = min(avail, a.Outpatient-usedOutpatient)
		}
		paid := min(c.Cost, max(avail, 0))
		a.Paid += paid
		a.Unpaid += c.Cost - paid
		usedHospital += paid
		if kind == Outpatient {
			usedOutpatient += paid
		}
	}
	return a
}

// LateNotice is whether a notification came after the 48 hours MOM allows: it is told on or before the end of the day after the first day of absence. It is
// recorded, never refused: the employer decides what a late notice means.
func LateNotice(first, notified time.Time) bool {
	return !notified.Before(first.AddDate(0, 0, 2))
}
