// Package organisation holds the rules an organisation's profile and financial year must meet before
// they are saved (FR-ORG-01, FR-ORG-02). Every rule that fails is reported at once, with its field, a
// stable code and how to fix it (ENG-23, FR-INT-07). The database's checks (migration 000018) are the
// backstop.
package organisation

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The codes of the profile's rules.
const (
	CodeText      = "ORG-01" // a text is blank or too long
	CodeUEN       = "ORG-02" // not a UEN
	CodeEmail     = "ORG-03" // not an email address
	CodeTimeZone  = "ORG-04" // not a time zone
	CodeYearStart = "ORG-05" // the financial year's start month is not 1 to 12
	CodeFirstYear = "ORG-06" // the first financial year does not fit the books' start or the start month
	CodeBank      = "ORG-07" // not a bank account number
	CodePrefix    = "ORG-08" // a number sequence's prefix is not allowed
	CodeNext      = "ORG-09" // a sequence's next number goes back, or is out of range
	CodeDigits    = "ORG-10" // a sequence pads to too few or too many digits
)

// Problem is one rule that failed.
type Problem struct {
	Code, Field, Message, Fix string
}

// Problems are every rule that failed.
type Problems []Problem

func (p Problems) Error() string {
	parts := make([]string, len(p))
	for i, x := range p {
		parts[i] = fmt.Sprintf("%s %s: %s (%s)", x.Code, x.Field, x.Message, x.Fix)
	}
	return strings.Join(parts, "; ")
}

// uen is a Unique Entity Number: 9 or 10 letters and digits (ACRA). PayNow by UEN takes the same form.
var uen = regexp.MustCompile(`^[0-9A-Z]{9,10}$`)

// bankAccount is an account number as printed: digits, with spaces or hyphens between groups.
var bankAccount = regexp.MustCompile(`^[0-9]([0-9 -]*[0-9])?$`)

// limits are the longest each text may be, as the database allows.
var limits = []struct {
	field string
	get   func(storage.OrganisationProfile) string
	max   int
}{
	{"legal_name", func(p storage.OrganisationProfile) string { return p.LegalName }, 200},
	{"registered_address", func(p storage.OrganisationProfile) string { return p.RegisteredAddress }, 500},
	{"mailing_address", func(p storage.OrganisationProfile) string { return p.MailingAddress }, 500},
	{"email", func(p storage.OrganisationProfile) string { return p.Email }, 254},
	{"phone", func(p storage.OrganisationProfile) string { return p.Phone }, 40},
	{"website", func(p storage.OrganisationProfile) string { return p.Website }, 200},
	{"bank_name", func(p storage.OrganisationProfile) string { return p.BankName }, 100},
	{"bank_account_name", func(p storage.OrganisationProfile) string { return p.BankAccountName }, 140},
	{"bank_account_number", func(p storage.OrganisationProfile) string { return p.BankAccountNumber }, 34},
	{"document_footer", func(p storage.OrganisationProfile) string { return p.DocumentFooter }, 1000},
}

// Check returns every rule p breaks, or nil.
func Check(p storage.OrganisationProfile) Problems {
	var out Problems
	add := func(code, field, message, fix string) { out = append(out, Problem{code, field, message, fix}) }
	for _, l := range limits {
		v := l.get(p)
		switch {
		case v != "" && strings.TrimSpace(v) == "":
			add(CodeText, l.field, "is only spaces", "Enter the text, or leave it empty")
		case len([]rune(v)) > l.max:
			add(CodeText, l.field, fmt.Sprintf("has %d characters; at most %d fit", len([]rune(v)), l.max), "Shorten it")
		}
	}
	if p.UEN != "" && !uen.MatchString(p.UEN) {
		add(CodeUEN, "uen", fmt.Sprintf("%q is not a UEN", p.UEN), "Enter the UEN as ACRA issued it: 9 or 10 capital letters and digits, such as 202312345K")
	}
	if p.PayNowUEN != "" && !uen.MatchString(p.PayNowUEN) {
		add(CodeUEN, "paynow_uen", fmt.Sprintf("%q is not a UEN", p.PayNowUEN), "Enter the UEN registered with PayNow: 9 or 10 capital letters and digits")
	}
	if p.Email != "" {
		if a, err := mail.ParseAddress(p.Email); err != nil || a.Address != p.Email {
			add(CodeEmail, "email", fmt.Sprintf("%q is not an email address", p.Email), "Enter one address, such as accounts@example.com")
		}
	}
	if p.BankAccountNumber != "" && !bankAccount.MatchString(p.BankAccountNumber) {
		add(CodeBank, "bank_account_number", "is not an account number", "Enter the digits as the bank prints them; spaces and hyphens may separate groups")
	}
	if _, err := time.LoadLocation(p.TimeZone); p.TimeZone == "" || p.TimeZone == "Local" || err != nil {
		add(CodeTimeZone, "time_zone", fmt.Sprintf("%q is not a time zone", p.TimeZone), "Enter an IANA time zone, such as Asia/Singapore")
	}
	if p.FYStartMonth < 1 || p.FYStartMonth > 12 {
		add(CodeYearStart, "fy_start_month", fmt.Sprintf("%d is not a month", p.FYStartMonth), "Enter the month the financial year starts in, 1 for January to 12 for December")
	} else if p.FirstYearEnd != nil {
		end := *p.FirstYearEnd
		next := end.AddDate(0, 0, 1)
		switch {
		case p.BooksStart == nil:
			add(CodeFirstYear, "books_start", "is not set, but the first financial year's end is", "Enter the date the books start")
		case !end.After(*p.BooksStart):
			add(CodeFirstYear, "first_year_end", fmt.Sprintf("%s is not after the books' start, %s", end.Format(time.DateOnly), p.BooksStart.Format(time.DateOnly)),
				"Enter the last day of the first financial year, after the books start")
		}
		if next.Day() != 1 || int(next.Month()) != p.FYStartMonth {
			add(CodeFirstYear, "first_year_end", fmt.Sprintf("%s is not the day before a financial year starts in %s", end.Format(time.DateOnly), time.Month(p.FYStartMonth)),
				fmt.Sprintf("Enter the last day of %s, the month before the financial year starts", time.Month((p.FYStartMonth+10)%12+1)))
		}
	}
	return out
}

// sequencePrefix is what a document number may start with: capital letters, digits, slashes and hyphens.
var sequencePrefix = regexp.MustCompile(`^[A-Z0-9][A-Z0-9/-]{0,11}$`)

// CheckSequence returns every rule the change from cur to next breaks, or nil (FR-ORG-04). The next
// number never goes back, so a number is never issued twice.
func CheckSequence(cur, next storage.SequenceSettings) Problems {
	var out Problems
	add := func(code, field, message, fix string) { out = append(out, Problem{code, field, message, fix}) }
	if !sequencePrefix.MatchString(next.Prefix) {
		add(CodePrefix, "prefix", fmt.Sprintf("%q is not a prefix", next.Prefix), "Use 1 to 12 capital letters, digits, slashes or hyphens, starting with a letter or digit, such as JNL-")
	}
	switch {
	case next.NextNumber < 1 || next.NextNumber > 999999999999:
		add(CodeNext, "next_number", fmt.Sprintf("%d is out of range", next.NextNumber), "Enter a number from 1 to 999,999,999,999")
	case next.NextNumber < cur.NextNumber:
		add(CodeNext, "next_number", fmt.Sprintf("%d is before the next number, %d", next.NextNumber, cur.NextNumber),
			"Move the next number forward, or leave it: a number is never issued twice")
	}
	if next.Digits < 1 || next.Digits > 12 {
		add(CodeDigits, "digits", fmt.Sprintf("%d is not 1 to 12", next.Digits), "Enter how many digits to pad the number to, 1 to 12")
	}
	return out
}
