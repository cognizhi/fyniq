// Package contact holds the rules a contact must meet before it is saved (FR-CT-01): every rule that fails
// is reported at once, with its field, a stable code and how to fix it (ENG-23, FR-INT-07). The database's
// checks (migration 000026) are the backstop.
package contact

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The codes of a contact's rules.
const (
	CodeName     = "CT-01" // the legal name is blank or too long
	CodeRole     = "CT-02" // neither a customer nor a vendor
	CodeEmail    = "CT-03" // not an email address
	CodeCountry  = "CT-04" // not a country code
	CodeCurrency = "CT-05" // not a currency Fyniq knows
	CodeTerms    = "CT-06" // payment terms outside 0 to 365 days
	CodeGSTCode  = "CT-07" // not the form of a GST code
	CodePeppol   = "CT-08" // not a Peppol participant ID
	CodeText     = "CT-09" // a text is too long
	CodeCategory = "CT-10" // the default category is not an active leaf account
)

// Problem is one rule that failed.
type Problem struct {
	Code, Field, Message, Fix string
}

var (
	country = regexp.MustCompile(`^[A-Z]{2}$`)
	gstCode = regexp.MustCompile(`^[A-Z]{2,4}$`)
	// A Peppol participant ID is a scheme and an identifier, such as 0195:SGUEN202312345K (ISO 6523; 0195
	// is Singapore's UEN scheme). [verify] the schemes InvoiceNow accepts, with p1-sales' InvoiceNow work.
	peppol = regexp.MustCompile(`^[0-9]{4}:[A-Za-z0-9._-]{1,64}$`)
)

// Check returns every rule d breaks, or nil.
func Check(d storage.ContactDetails) []Problem {
	var out []Problem
	add := func(code, field, message, fix string) { out = append(out, Problem{code, field, message, fix}) }
	if strings.TrimSpace(d.LegalName) == "" || len([]rune(d.LegalName)) > 200 {
		add(CodeName, "legal_name", "is blank or longer than 200 characters", "Enter the contact's legal name")
	}
	if !d.Customer && !d.Vendor {
		add(CodeRole, "customer", "the contact is neither a customer nor a vendor", "Mark it as a customer, a vendor or both")
	}
	for field, v := range map[string]string{"billing_email": d.BillingEmail, "remittance_email": d.RemittanceEmail} {
		if v == "" {
			continue
		}
		if a, err := mail.ParseAddress(v); err != nil || a.Address != v {
			add(CodeEmail, field, fmt.Sprintf("%q is not an email address", v), "Enter one address, such as accounts@example.com")
		}
	}
	for field, c := range map[string]struct {
		v   string
		max int
	}{"uen_or_tax_id": {d.UENOrTaxID, 30}, "phone": {d.Phone, 40}, "address": {d.Address, 500}} {
		if len([]rune(c.v)) > c.max || (c.v != "" && strings.TrimSpace(c.v) == "") {
			add(CodeText, field, fmt.Sprintf("is only spaces or longer than %d characters", c.max), "Shorten it, or leave it empty")
		}
	}
	if d.Country != "" && !country.MatchString(d.Country) {
		add(CodeCountry, "country", fmt.Sprintf("%q is not a country code", d.Country), "Use the two-letter ISO 3166 code, such as SG or US")
	}
	if d.Currency != "" {
		if _, err := money.Lookup(d.Currency); err != nil {
			add(CodeCurrency, "currency", fmt.Sprintf("%q is not a currency Fyniq knows", d.Currency), "Use an ISO 4217 code, such as SGD or USD")
		}
	}
	if d.PaymentTermsDays != nil && (*d.PaymentTermsDays < 0 || *d.PaymentTermsDays > 365) {
		add(CodeTerms, "payment_terms_days", fmt.Sprintf("%d days is out of range", *d.PaymentTermsDays), "Enter 0 to 365 days")
	}
	if d.DefaultGSTCode != "" && !gstCode.MatchString(d.DefaultGSTCode) {
		add(CodeGSTCode, "default_gst_code", fmt.Sprintf("%q is not a GST code", d.DefaultGSTCode), "Use the GST code, such as TX, SR or NA")
	}
	if d.PeppolID != "" && !peppol.MatchString(d.PeppolID) {
		add(CodePeppol, "peppol_id", fmt.Sprintf("%q is not a Peppol participant ID", d.PeppolID), "Enter the scheme and identifier, such as 0195:SGUEN202312345K")
	}
	return out
}

// CheckCategory returns the problem with a default category that is not an active leaf account, or nil.
func CheckCategory(a storage.Account) []Problem {
	if a.Group || !a.Active {
		return []Problem{{CodeCategory, "default_account_id", fmt.Sprintf("account %s is a group or inactive", a.Code),
			"Choose an active account that takes postings"}}
	}
	return nil
}
