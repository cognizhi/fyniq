package payroll

import (
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// FinalPayDeadline is when a leaver's final salary is due and the latest day it may be paid (NM-PAY-08, from MOM's page on paying salary, sg-people@2026.9). A resignation with the notice served is due
// on the last day; a resignation without it within 7 days of the last day; a termination by the employer (a dismissal for misconduct included, and a retrenchment, which MOM's page does not name and
// which is treated as one `[verify]`) is due on the last day, or within 3 working days if that is not possible. Any other ending (the end of a contract, retirement) is due on the last day: the page
// names no other case. The days are the pack's; working days leave out weekends, public holidays and the Monday in lieu.
func FinalPayDeadline(v pack.Version, kind string, noticeServed bool, last time.Time) (due, latest time.Time, rule string) {
	switch kind {
	case "resignation":
		if noticeServed {
			return last, last, "On the last day: the employee resigned and served the notice."
		}
		days, ok := pack.TimingDays(v, "final_pay_resigned_unserved_days", last)
		if !ok {
			days = 7
		}
		return last, last.AddDate(0, 0, days), fmt.Sprintf("Within %d days of the last day: the employee resigned without serving the notice.", days)
	case "termination", "retrenchment":
		days, ok := pack.TimingDays(v, "final_pay_terminated_working_days", last)
		if !ok {
			days = 3
		}
		d := last
		for n := days; n > 0; {
			d = d.AddDate(0, 0, 1)
			if !pack.NonWorkingDay(v, d) {
				n--
			}
		}
		return last, d, fmt.Sprintf("On the last day, or within %d working days of it if that is not possible: the employer ended the contract.", days)
	default:
		return last, last, "On the last day: MOM's page names no other case."
	}
}
