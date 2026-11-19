package checks

import (
	"fmt"
	"sort"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// PaymentFact is a person's payment in a pay run as the checks read it (NM-PAY-10): its latest state, the date it was confirmed paid, and the payslip made for it.
type PaymentFact struct {
	Person         string
	Month          time.Time // the wage month
	State          string    // the latest state
	PaidOn         *time.Time
	HasPayslip     bool
	PayslipDate    *time.Time // the date of payment on the payslip
	AmountMismatch bool       // paid an amount other than the one owed
}

// Payments is N.1 #14 (NM-PAY-10): a payment that was rejected, returned or has an unknown outcome is ✖, and one confirmed paid for an amount other than the one owed is ⚠.
func Payments(facts []PaymentFact) []Finding {
	var out []Finding
	for _, f := range facts {
		month := f.Month.Format("January 2006")
		switch f.State {
		case "rejected":
			out = append(out, Finding{14, "Payroll", Fail, f.Person, fmt.Sprintf("The bank rejected the payment of %s: check the bank details, then schedule the payment again.", month)})
		case "returned":
			out = append(out, Finding{14, "Payroll", Fail, f.Person, fmt.Sprintf("The payment of %s came back from the bank: find out why, then schedule it again.", month)})
		case "unknown":
			out = append(out, Finding{14, "Payroll", Fail, f.Person, fmt.Sprintf("Nobody knows whether the payment of %s reached the person. No new payment can be made until you record whether it was paid, rejected or returned, with evidence.", month)})
		}
		if f.State == "paid" && f.AmountMismatch {
			out = append(out, Finding{14, "Payroll", Warn, f.Person, fmt.Sprintf("The payment of %s was confirmed for an amount other than the one owed: compare it with the run and correct whichever is wrong.", month)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

// addWorkingDays is the day n working days after a day, leaving out weekends and the pack's public holidays.
func addWorkingDays(v pack.Version, from time.Time, n int) time.Time {
	d := from
	for n > 0 {
		d = d.AddDate(0, 0, 1)
		if pack.NonWorkingDay(v, d) {
			continue
		}
		n--
	}
	return d
}

// Payslips is N.1 #9 (NM-PAY-04): a payslip is due with the payment, or within the pack's working days of the date the person was confirmed paid. One not yet made is ⏰ until then and ✖ after;
// one made with a date of payment that is not the confirmed date is ⚠, and the correction is the next version's payslip.
func Payslips(v pack.Version, facts []PaymentFact, days int, on time.Time) []Finding {
	var out []Finding
	for _, f := range facts {
		if f.State != "paid" || f.PaidOn == nil {
			continue
		}
		month := f.Month.Format("January 2006")
		due := addWorkingDays(v, *f.PaidOn, days)
		switch {
		case !f.HasPayslip && on.After(due):
			out = append(out, Finding{9, "Payroll", Fail, f.Person, fmt.Sprintf("The payslip of %s was due on %s, %d working days after the person was paid on %s, and is not published: publish the run's payslips.", month, due.Format(time.DateOnly), days, f.PaidOn.Format(time.DateOnly))})
		case !f.HasPayslip:
			out = append(out, Finding{9, "Payroll", Due, f.Person, fmt.Sprintf("The payslip of %s is due by %s, %d working days after the person was paid on %s: publish the run's payslips.", month, due.Format(time.DateOnly), days, f.PaidOn.Format(time.DateOnly))})
		case f.PayslipDate != nil && !f.PayslipDate.Equal(*f.PaidOn):
			out = append(out, Finding{9, "Payroll", Warn, f.Person, fmt.Sprintf("The payslip of %s gives %s as the date of payment and the person was confirmed paid on %s: correct the run, and the corrected version's payslip carries the right date.", month, f.PayslipDate.Format(time.DateOnly), f.PaidOn.Format(time.DateOnly))})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}

// CalendarFact is a pay group's wage month as the payroll calendar checks read it: the pay day, the latest day to approve the run, and whether a version of the month's run is approved.
type CalendarFact struct {
	Group      string
	Month      time.Time // the first day of the wage month
	PayDate    time.Time
	ApproveBy  time.Time
	HasRun     bool // a run exists for the month
	Approved   bool // a version of it is approved
	HasMembers bool // at least one person is paid in the group that month
}

// PayrollCalendar is N.1 #2 (NM-PAY-03): a month whose run is not approved by the latest approval day is ⏰ until the pay day, and ✖ after it; once the statutory days after the end of the salary
// period are gone (7 in the pack) the message says that the Employment Act's deadline has passed too. A group with nobody to pay in the month is left out.
func PayrollCalendar(facts []CalendarFact, salaryDays int, on time.Time) []Finding {
	var out []Finding
	for _, f := range facts {
		if !f.HasMembers || f.Approved {
			continue
		}
		month := f.Month.Format("January 2006")
		end := f.Month.AddDate(0, 1, -1)
		switch {
		case on.After(f.PayDate):
			msg := fmt.Sprintf("The run for %s of %s is not approved and the pay day, %s, has passed: approve it, or pay the people and say why it is late.", month, f.Group, f.PayDate.Format(time.DateOnly))
			if salaryDays > 0 && on.After(end.AddDate(0, 0, salaryDays)) {
				msg = fmt.Sprintf("The run for %s of %s is not approved and the Employment Act's deadline, %d days after the end of the salary period (%s), has passed: approve it now.", month, f.Group, salaryDays, end.AddDate(0, 0, salaryDays).Format(time.DateOnly))
			}
			out = append(out, Finding{2, "Payroll", Fail, f.Group, msg})
		case !on.Before(f.ApproveBy):
			out = append(out, Finding{2, "Payroll", Due, f.Group, fmt.Sprintf("The run for %s of %s is due for approval by %s, before the pay day, %s.", month, f.Group, f.ApproveBy.Format(time.DateOnly), f.PayDate.Format(time.DateOnly))})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Message < out[j].Message })
	return out
}

// FinalPayFact is a leaver's final pay as the checks read it.
type FinalPayFact struct {
	Person string
	Latest time.Time // the latest day it may be paid, by the case
	Due    time.Time // the day it is due
	PaidOn *time.Time
	Held   bool // a tax-clearance hold is in force
}

// FinalPay is N.1 #15 (NM-PAY-08): a leaver's final pay is ⏰ from 7 days before it is due until the latest day for the case, ✖ after that day if it is not paid, and ⚠ if it was paid after it. Pay held under
// a tax-clearance hold is the one reason it is not paid: it shows as ⚠ with the hold named, never as late.
func FinalPay(facts []FinalPayFact, on time.Time) []Finding {
	var out []Finding
	for _, f := range facts {
		switch {
		case f.PaidOn != nil && f.PaidOn.After(f.Latest):
			out = append(out, Finding{15, "Payroll", Warn, f.Person, fmt.Sprintf("The final pay was due by %s and was paid on %s: look into why, and keep the reason.", f.Latest.Format(time.DateOnly), f.PaidOn.Format(time.DateOnly))})
		case f.PaidOn != nil:
		case f.Held && !on.Before(f.Due):
			out = append(out, Finding{15, "Payroll", Warn, f.Person, fmt.Sprintf("The final pay was due on %s and is held under a tax-clearance hold: release the hold when IRAS gives clearance, and pay.", f.Due.Format(time.DateOnly))})
		case on.After(f.Latest):
			out = append(out, Finding{15, "Payroll", Fail, f.Person, fmt.Sprintf("The final pay was due by %s and is not paid: prepare the final run and pay it now.", f.Latest.Format(time.DateOnly))})
		case !on.Before(f.Due.AddDate(0, 0, -7)):
			out = append(out, Finding{15, "Payroll", Due, f.Person, fmt.Sprintf("The final pay is due on %s, and by %s at the latest: prepare the final run.", f.Due.Format(time.DateOnly), f.Latest.Format(time.DateOnly))})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out
}
