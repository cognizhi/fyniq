package payroll

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
)

// Codes of a refusal; each is in the API's catalogue.
const (
	CodeInvalid  = "NM_PAYROLL_INVALID"
	CodeConflict = "NM_PAYROLL_CONFLICT"
	CodeNotFound = "NM_NOT_FOUND"
	CodeRole     = "NM_ROLE_REQUIRED"
)

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

func invalid(format string, a ...any) error { return &Refusal{CodeInvalid, fmt.Sprintf(format, a...)} }

// Money reads an amount in dollars with at most two decimals, naming the field; it may be zero only when zeroOK.
func Money(field, s string, zeroOK bool) (*apd.Decimal, error) {
	d, _, err := apd.NewFromString(strings.TrimSpace(s))
	if err != nil || d.Form != apd.Finite || d.Exponent < -2 || d.Sign() < 0 || (!zeroOK && d.Sign() == 0) {
		if zeroOK {
			return nil, invalid("%s is an amount in dollars with at most two decimal places: write it as 4500.00.", field)
		}
		return nil, invalid("%s is an amount in dollars, more than zero, with at most two decimal places: write it as 4500.00.", field)
	}
	return d, nil
}

// PayGroup is the people paid on one calendar (NM-PAY-03): the day of the month salary is paid (0 is the last day), the cut-off for inputs and the latest approval date, as days
// before the pay day.
type PayGroup struct {
	Name                             string
	PayDay, CutoffDays, ApprovalDays int
}

// Check refuses a group that is not whole.
func (g PayGroup) Check() error {
	if n := utf8.RuneCountInString(strings.TrimSpace(g.Name)); n < 1 || n > 80 {
		return invalid("The name of a pay group is 1 to 80 characters: such as Monthly.")
	}
	if g.PayDay < 0 || g.PayDay > 28 {
		return invalid("The pay day is the day of the month from 1 to 28, or 0 for the last day of the month.")
	}
	if g.CutoffDays < 0 || g.CutoffDays > 20 || g.ApprovalDays < 0 || g.ApprovalDays > 20 || g.ApprovalDays > g.CutoffDays {
		return invalid("The cut-off for inputs and the latest approval are 0 to 20 days before the pay day, and the approval is no earlier than the cut-off.")
	}
	return nil
}

// PayDate is the pay day of a wage month: salary is paid within 7 days after the end of the salary period (the Employment Act, `sg-people` timing), so a pay day after the 7th of the
// following month is refused where the group is made; here it is the day in the month itself, or its last day.
func (g PayGroup) PayDate(month time.Time) time.Time {
	first := firstOf(month)
	if g.PayDay == 0 {
		return first.AddDate(0, 1, -1)
	}
	return first.AddDate(0, 0, g.PayDay-1)
}

// CutoffDate is the last day inputs are accepted for the month; ApprovalDate the latest day the run is approved.
func (g PayGroup) CutoffDate(month time.Time) time.Time {
	return g.PayDate(month).AddDate(0, 0, -g.CutoffDays)
}

// ApprovalDate is the latest day the month's run is approved.
func (g PayGroup) ApprovalDate(month time.Time) time.Time {
	return g.PayDate(month).AddDate(0, 0, -g.ApprovalDays)
}

// Kinds of a recurring item.
const (
	KindAllowance      = "allowance"
	KindFixedDeduction = "fixed_deduction"
)

// RecurringItem is an allowance or a fixed deduction a person has every month, with the facts that class it (FR-PAY-17).
type RecurringItem struct {
	Key, Name, Kind, Nature string
	Amount                  string
	CPFWages                bool
}

var itemNatures = map[string][]string{
	KindAllowance:      {"allowance", "overtime", "other"},
	KindFixedDeduction: {"accommodation", "cooperative", "other_deduction"},
}

// Check refuses an item that is not whole: a key and name, a kind and a nature of that kind, an amount; a deduction is never CPF wages.
func (it RecurringItem) Check() error {
	if k := strings.TrimSpace(it.Key); k == "" || utf8.RuneCountInString(k) > 40 {
		return invalid("An item needs a key of up to 40 characters, such as transport.")
	}
	if n := strings.TrimSpace(it.Name); n == "" || utf8.RuneCountInString(n) > 80 {
		return invalid("%s needs a name of up to 80 characters, as the payslip shows it.", it.Key)
	}
	natures, ok := itemNatures[it.Kind]
	if !ok {
		return invalid("%s is an allowance or a fixed deduction.", it.Key)
	}
	found := false
	for _, n := range natures {
		found = found || n == it.Nature
	}
	if !found {
		return invalid("%s: a %s is one of %s.", it.Key, it.Kind, strings.Join(natures, ", "))
	}
	if it.Kind == KindFixedDeduction && it.CPFWages {
		return invalid("%s: a deduction is never CPF wages.", it.Key)
	}
	if _, err := Money(it.Key+"'s amount", it.Amount, false); err != nil {
		return err
	}
	return nil
}

// PayRecord is a person's pay from a day: the group, the monthly basic salary, the self-help group fund and what they have every month.
type PayRecord struct {
	Group     string // the pay group's ID, which the caller has checked exists
	Basic     string
	SHGFund   string
	SHGOptOut bool
	Items     []RecurringItem
}

// SHGFunds are the funds the packs know.
var SHGFunds = []string{"CDAC", "ECF", "MBMF", "SINDA"}

// Check refuses a record that is not whole.
func (p PayRecord) Check() error {
	if p.Group == "" {
		return invalid("A person's pay has a pay group: choose one.")
	}
	if _, err := Money("The basic monthly salary", p.Basic, true); err != nil {
		return err
	}
	if p.SHGFund != "" {
		ok := false
		for _, f := range SHGFunds {
			ok = ok || f == p.SHGFund
		}
		if !ok {
			return invalid("The self-help group fund is CDAC, ECF, MBMF or SINDA, or empty for none.")
		}
	}
	seen := map[string]bool{}
	for _, it := range p.Items {
		if err := it.Check(); err != nil {
			return err
		}
		if seen[it.Key] {
			return invalid("Two items have the key %s: each key is used once.", it.Key)
		}
		seen[it.Key] = true
	}
	if len(p.Items) > 30 {
		return invalid("A pay record holds up to 30 items.")
	}
	return nil
}

// YTDImport is the year to date of CPF wages this employer has counted for a person, as of a month: imported at cut-over (FR-PAY-14).
type YTDImport struct {
	Year                  int
	OW, AWSubject, AWPaid string
	Through               time.Time
}

// Check refuses a year to date that is not whole: the amounts are not negative, the additional wages subject are no more than were paid, and the month is in the year.
func (y YTDImport) Check() error {
	if y.Year < 2020 || y.Year > 2100 {
		return invalid("The year is the calendar year the wages were paid in, such as 2026.")
	}
	ow, err := Money("Ordinary wages subject to CPF", y.OW, true)
	if err != nil {
		return err
	}
	_ = ow
	subject, err := Money("Additional wages subject to CPF", y.AWSubject, true)
	if err != nil {
		return err
	}
	paid, err := Money("Additional wages paid", y.AWPaid, true)
	if err != nil {
		return err
	}
	if subject.Cmp(paid) > 0 {
		return invalid("Additional wages subject to CPF cannot be more than the additional wages paid.")
	}
	if y.Through.Year() != y.Year || y.Through.Day() != 1 {
		return invalid("The last month counted is the first day of a month in %d: write it as %d-05-01.", y.Year, y.Year)
	}
	return nil
}

// Natures of a one-off item.
var OneOffNatures = []string{"bonus", "commission", "allowance", "other"}

// OneOff is an item paid once, with the facts that class it (FR-PAY-17).
type OneOff struct {
	Month, Key, Name, Nature, Amount string
	CPFWages                         bool
	ForMonth, PayableOn              *time.Time
	Note                             string
}

// Check refuses a one-off that is not whole.
func (o OneOff) Check() error {
	if k := strings.TrimSpace(o.Key); k == "" || utf8.RuneCountInString(k) > 40 {
		return invalid("A one-off item needs a key of up to 40 characters, such as bonus-2026.")
	}
	if n := strings.TrimSpace(o.Name); n == "" || utf8.RuneCountInString(n) > 80 {
		return invalid("A one-off item needs a name of up to 80 characters, as the payslip shows it.")
	}
	ok := false
	for _, n := range OneOffNatures {
		ok = ok || n == o.Nature
	}
	if !ok {
		return invalid("A one-off item is a bonus, a commission, an allowance or other.")
	}
	if _, err := Money("The amount", o.Amount, false); err != nil {
		return err
	}
	if o.ForMonth != nil && o.ForMonth.Day() != 1 {
		return invalid("The month the item is for is the first day of a month: write it as 2026-06-01.")
	}
	if utf8.RuneCountInString(o.Note) > 300 {
		return invalid("The note is up to 300 characters.")
	}
	return nil
}
