package leave

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// The government-paid schemes Nemura tracks (NM-LV-07, NM-LV-14). Childcare and extended childcare need their reimbursement caps in the pack and arrive with them.
const (
	Maternity      = "maternity"
	Paternity      = "paternity"
	SharedParental = "shared_parental"
)

// Schemes lists them.
var Schemes = []string{Maternity, Paternity, SharedParental}

// Cents is an amount of Singapore dollars in cents: money is never a float (golden rule 1).
type Cents int64

// String writes dollars and cents with an ASCII minus: "2500.00".
func (c Cents) String() string {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}
	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100)
}

// ParseCents reads a decimal string with at most two places: "2500", "2500.5", "0.25".
func ParseCents(s string) (Cents, error) {
	s = strings.TrimSpace(s)
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || len(frac) > 2 || strings.ContainsAny(whole+frac, "+-") {
		return 0, invalid("The amount %q is not dollars with at most two decimal places: write it as 2500.00.", s)
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w < 0 {
		return 0, invalid("The amount %q is not dollars with at most two decimal places: write it as 2500.00.", s)
	}
	f := int64(0)
	if frac != "" {
		if f, err = strconv.ParseInt((frac + "0")[:2], 10, 64); err != nil {
			return 0, invalid("The amount %q is not dollars with at most two decimal places: write it as 2500.00.", s)
		}
	}
	return Cents(w*100 + f), nil
}

// SchemeKey is the pack row a scheme falls under for a child born on a day: maternity by the child's order, shared parental leave by the weeks the birth date
// gives (the pack's rows are keyed by birth date).
func SchemeKey(v pack.Version, scheme string, childOrder int, born time.Time) (string, error) {
	switch scheme {
	case Maternity:
		if childOrder < 1 {
			return "", invalid("The child's order is required: 1 for a first child, 2 for a second, and so on.")
		}
		if childOrder <= 2 {
			return "maternity_citizen_1st_2nd", nil
		}
		return "maternity_citizen_3rd_plus", nil
	case Paternity:
		return "paternity_citizen", nil
	case SharedParental:
		for _, k := range []string{"shared_parental_10_weeks", "shared_parental_6_weeks"} {
			if _, ok := pack.ParentalLeave(v, k, born); ok {
				return k, nil
			}
		}
		return "", invalid("Shared parental leave is for children born from 1 April 2025: the country pack has no scheme for a child born on %s.", born.Format(time.DateOnly))
	}
	return "", invalid("The scheme %q is not one Nemura tracks: choose maternity, paternity or shared parental.", scheme)
}

// Eligible says why a person cannot take a scheme for a child, or nothing: the child must be a Singapore Citizen, the person must have served the months the scheme
// asks for before the birth, and the leave must fall in the scheme's window. A researched value is used as it is; the company has decided on it (NM-PACK-07).
func Eligible(s ParentalRule, start, born time.Time, citizen bool) error {
	if !citizen {
		return invalid("The child is not a Singapore Citizen: government-paid leave is for a citizen child, so the employer pays any leave the company gives.")
	}
	if s.MinServiceMonths > 0 && completedMonths(start, born) < s.MinServiceMonths {
		return invalid("The person had served fewer than %d months before the child's birth on %s, which the scheme asks for.", s.MinServiceMonths, born.Format(time.DateOnly))
	}
	return nil
}

// ParentalRule is a scheme's rule as the pack holds it, with the pack's negative number for a figure it does not state kept out of the arithmetic.
type ParentalRule struct {
	Key                                                      string
	TotalWeeks, EmployerWeeks, GovernmentWeeks               int
	MinServiceMonths, CapPerPeriod, CapPeriodWeeks, CapTotal int
	ClaimMonths                                              int
}

// RuleOf reads a scheme's rule from the pack for a child born on a day.
func RuleOf(v pack.Version, key string, born time.Time) (ParentalRule, error) {
	p, ok := pack.ParentalLeave(v, key, born)
	if !ok {
		return ParentalRule{}, invalid("The country pack has no %s rule for a child born on %s.", key, born.Format(time.DateOnly))
	}
	pos := func(n int) int { return max(n, 0) }
	return ParentalRule{Key: key, TotalWeeks: pos(p.TotalWeeks), EmployerWeeks: pos(p.EmployerWeeks), GovernmentWeeks: pos(p.GovernmentWeeks), MinServiceMonths: pos(p.MinServiceMonths),
		CapPerPeriod: pos(p.CapPerPeriod), CapPeriodWeeks: pos(p.CapPeriodWeeks), CapTotal: pos(p.CapTotal), ClaimMonths: pos(p.ClaimMonths)}, nil
}

// Window checks the dates of a block of leave against a scheme (NM-LV-14): maternity may begin up to four weeks before the birth; paternity and shared parental leave
// begin on or after the birth and end within twelve months of it. It also keeps the days within the scheme's weeks, counting what the person took before.
func Window(scheme string, r ParentalRule, born, from, to time.Time, daysBefore int) error {
	if to.Before(from) {
		return invalid("The last day is before the first day: choose a last day on or after %s.", from.Format(time.DateOnly))
	}
	days := int(to.Sub(from).Hours()/24) + 1
	switch scheme {
	case Maternity:
		if from.Before(born.AddDate(0, 0, -28)) {
			return invalid("Maternity leave begins at the earliest 4 weeks before the birth, on %s.", born.AddDate(0, 0, -28).Format(time.DateOnly))
		}
	case Paternity, SharedParental:
		if from.Before(born) {
			return invalid("%s leave begins on or after the child's birth, on %s.", titleOf(scheme), born.Format(time.DateOnly))
		}
		if to.After(born.AddDate(1, 0, -1)) {
			return invalid("%s leave ends within 12 months of the birth, by %s.", titleOf(scheme), born.AddDate(1, 0, -1).Format(time.DateOnly))
		}
	}
	if daysBefore+days > r.TotalWeeks*7 {
		return invalid("That is %d days and the scheme allows %d weeks (%d days) in all, of which %d are already recorded: shorten the block.", days, r.TotalWeeks, r.TotalWeeks*7, daysBefore)
	}
	return nil
}

func titleOf(scheme string) string {
	if scheme == Paternity {
		return "Paternity"
	}
	return "Shared parental"
}

// ClaimDeadline is the last day to file the employer's reimbursement claim: the scheme's months after the last day of leave (NM-LV-14).
func ClaimDeadline(r ParentalRule, to time.Time) time.Time {
	return to.AddDate(0, max(r.ClaimMonths, 3), 0)
}

// MaxClaim is the most the Government reimburses for a block of leave of this many days: its weeks beyond what the employer pays, at the scheme's cap for each period,
// and never above the scheme's total cap. It is a guide: the portal decides, and the salary owed never depends on it. A scheme with no cap in the pack returns false.
func MaxClaim(r ParentalRule, days int) (Cents, bool) {
	if r.CapPerPeriod == 0 || r.CapPeriodWeeks == 0 {
		return 0, false
	}
	gov := max(days-r.EmployerWeeks*7, 0)
	gov = min(gov, r.GovernmentWeeks*7)
	capCents := Cents(int64(r.CapPerPeriod) * 100 * int64(gov) / int64(r.CapPeriodWeeks*7))
	if r.CapTotal > 0 {
		capCents = min(capCents, Cents(r.CapTotal)*100)
	}
	return capCents, true
}
