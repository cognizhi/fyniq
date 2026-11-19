// Package leave is the rules of leave in Nemura (PRD-002 NM-LV-01, NM-LV-02, NM-LV-11, NM-LV-13, NM-TIM-01): days as exact hundredths, the working pattern, the
// kinds of entry in the ledger and the balance they sum to, and a year's entitlement from the statute's pack and the company's policy. It holds no database and
// no clock; the adapter keeps the ledger and the service gives the day.
package leave

import (
	"fmt"
	"strconv"
	"strings"
)

// Days is an amount of leave in hundredths of a day: 7.50 days is 750. Leave is never a float (golden rule 1).
type Days int64

// Whole is a number of whole days.
func Whole(n int) Days { return Days(n) * 100 }

// String writes the amount as a decimal string with two places and an ASCII minus: "7.50", "-0.50".
func (d Days) String() string {
	sign := ""
	if d < 0 {
		sign, d = "-", -d
	}
	return fmt.Sprintf("%s%d.%02d", sign, d/100, d%100)
}

// ParseDays reads a decimal string of at most two places: "7", "7.5", "-0.25".
func ParseDays(s string) (Days, error) {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || len(frac) > 2 {
		return 0, fmt.Errorf("%q is not an amount of days with at most two decimal places", s)
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w < 0 || strings.ContainsAny(whole+frac, "+-") {
		return 0, fmt.Errorf("%q is not an amount of days with at most two decimal places", s)
	}
	f := int64(0)
	if frac != "" {
		if f, err = strconv.ParseInt((frac + "0")[:2], 10, 64); err != nil {
			return 0, fmt.Errorf("%q is not an amount of days with at most two decimal places", s)
		}
	}
	d := Days(w*100 + f)
	if neg {
		d = -d
	}
	return d, nil
}

// Kinds of ledger entry (design D1). A grant, an accrual and a release add; a reservation, a use, an expiry and an encashment take; a transfer and a
// correction go either way.
const (
	Grant       = "grant"
	Accrual     = "accrual"
	Reservation = "reservation"
	Use         = "use"
	Release     = "release"
	Expiry      = "expiry"
	Encashment  = "encashment"
	Transfer    = "transfer"
	Correction  = "correction"
)

// Kinds lists them, as the database's check does.
var Kinds = []string{Grant, Accrual, Reservation, Use, Release, Expiry, Encashment, Transfer, Correction}

// Entry is one line of a person's ledger for a leave type and year.
type Entry struct {
	Kind     string
	Amount   Days   // signed
	Source   string // a pack rule, a policy row, a request or a person: where the entry comes from
	Expires  string // a carried-forward grant's last day, as 2006-01-02, or empty
	Negative bool   // the type allows the balance to go below zero
}

// Balance is the four sums a person sees, and what is left (NM-LV-02): entitled, taken, booked and left, with expired and encashed shown apart.
type Balance struct {
	Entitled Days // grants, accruals, transfers and corrections
	Taken    Days // used
	Booked   Days // reserved for requests not yet taken, net of releases
	Expired  Days
	Encashed Days
	Left     Days
}

// Sum folds a ledger into its balance. A balance is only ever this sum.
func Sum(entries []Entry) Balance {
	var b Balance
	for _, e := range entries {
		switch e.Kind {
		case Grant, Accrual, Transfer, Correction:
			b.Entitled += e.Amount
		case Use:
			b.Taken -= e.Amount
		case Reservation, Release:
			b.Booked -= e.Amount
		case Expiry:
			b.Expired -= e.Amount
		case Encashment:
			b.Encashed -= e.Amount
		}
		b.Left += e.Amount
	}
	return b
}

// SignOK says whether an amount has the sign its kind needs.
func SignOK(kind string, amount Days) bool {
	switch kind {
	case Grant, Accrual, Release:
		return amount > 0
	case Reservation, Use, Expiry, Encashment:
		return amount < 0
	case Transfer, Correction:
		return amount != 0
	}
	return false
}
