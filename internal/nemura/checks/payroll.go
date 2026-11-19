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
