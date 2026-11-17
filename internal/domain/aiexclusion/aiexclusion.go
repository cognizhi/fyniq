// Package aiexclusion is the AI exclusion policy (PRD-001 AI-02, AI-20; p1-ai-assistant design D8): what never reaches
// a model, taken out of every read an AI principal makes before it leaves the API. The never-send fields go whatever the
// Owner sets: identity numbers (NRIC/FIN and the like), full bank account numbers, secrets and authentication data,
// and home addresses. Pay goes unless the organisation has turned on AI payroll access and the person holds a payroll
// right; the Owner may add fields of their own, never take one off the never-send list. Then the output filter masks
// any NRIC or bank-number shape left in text, as a second line of defence (AI-20).
package aiexclusion

import (
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/evidence"
)

// NeverSend are the fields no AI read carries, by name, and by name as the end of another, such as client_secret or
// director_nric (AI-02).
var NeverSend = []string{
	// identity numbers
	"nric", "fin", "nric_fin", "nric_number", "id_number", "identity_number", "passport_number", "national_id",
	// full bank account numbers
	"bank_account", "bank_account_number", "account_number", "iban", "card_number", "bank_details",
	// secrets and authentication data
	"password", "secret", "token", "api_key", "private_key", "authorization", "cookie", "credential", "credentials", "totp",
	"recovery_code", "recovery_codes", "passkey", "setup_code",
	// home addresses
	"home_address", "residential_address",
}

// Pay are the fields of pay, which an AI read carries only with AI payroll access and the person's payroll right
// (AI-02).
var Pay = []string{"salary", "monthly_salary", "annual_salary", "gross_salary", "wage", "wages", "gross_pay", "basic_pay", "net_pay",
	"cpf_employee", "cpf_employer", "payslip", "payslips", "pay_rate", "allowances", "bonus"}

// Policy is what one AI read keeps: pay, when AI payroll access is on and the person holds a payroll right, and the
// fields the Owner added.
type Policy struct {
	Payroll bool
	Extra   []string
}

// Apply takes out of v, a JSON value as decoded, every field the policy excludes, and masks NRIC and bank-number shapes
// in the text left. An individual's address goes too: a person's address is their home's.
func Apply(v any, p Policy) any {
	switch x := v.(type) {
	case map[string]any:
		individual, _ := x["individual"].(bool)
		out := make(map[string]any, len(x))
		for k, e := range x {
			if excluded(k, p) || individual && (k == "address" || k == "addresses") {
				continue
			}
			out[k] = Apply(e, p)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = Apply(e, p)
		}
		return out
	case string:
		return Filter(x)
	}
	return v
}

// excluded says whether the policy takes out a field named k.
func excluded(k string, p Policy) bool {
	k = strings.ToLower(k)
	named := func(list []string) bool {
		return slices.ContainsFunc(list, func(n string) bool { return k == n || strings.HasSuffix(k, "_"+n) })
	}
	return named(NeverSend) || !p.Payroll && named(Pay) || slices.Contains(p.Extra, k)
}

var (
	nricShape = regexp.MustCompile(`(?i)\b[STFGM]\d{7}[A-Z]\b`)
	// A bank writes an account number whole, in groups split by hyphens (012-345678-9) or in groups split by spaces
	// (0123 4567 89): each pass finds one way.
	hyphenRuns = regexp.MustCompile(`\d{2,}(?:-\d+){0,4}`)
	spaceRuns  = regexp.MustCompile(`\d{2,}(?: \d{2,}){1,4}`)
	isoDate    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	// yearFirst is a reference numbered within a year, such as 2026-00012: a document's number, not an account's.
	yearFirst = regexp.MustCompile(`^(19|20)\d{2}-`)
)

// Filter masks in s every NRIC or FIN whose check letter is right, as S****567D, and every bank-number shape, 9 to 18
// digits, as ****6789, leaving the last digits a person recognises (AI-20). A number that is part of a word, a decimal,
// a date or a path is not one, nor a reference numbered within a year (2026-00012); a UUID passes untouched.
func Filter(s string) string {
	if _, err := uuid.Parse(s); err == nil {
		return s
	}
	s = nricShape.ReplaceAllStringFunc(s, func(m string) string {
		if !evidence.ValidNRIC(m) {
			return m
		}
		return m[:1] + "****" + m[5:]
	})
	return maskRuns(maskRuns(s, hyphenRuns), spaceRuns)
}

// maskRuns masks the runs of digits re finds in s that stand alone and are as long as a bank account number.
func maskRuns(s string, re *regexp.Regexp) string {
	var b strings.Builder
	last := 0
	for _, m := range re.FindAllStringIndex(s, -1) {
		i, j := m[0], m[1]
		// A full stop or comma after it joins it to a decimal only when a digit follows: else it ends a sentence or a list.
		ends := j < len(s) && (s[j] == '.' || s[j] == ',') && (j+1 == len(s) || s[j+1] < '0' || s[j+1] > '9')
		if i > 0 && joined(s[i-1]) || j < len(s) && joined(s[j]) && !ends {
			continue
		}
		number := s[i:j]
		digits := strings.NewReplacer("-", "", " ", "").Replace(number)
		if len(digits) < 9 || len(digits) > 18 || isoDate.MatchString(number) || yearFirst.MatchString(number) {
			continue
		}
		b.WriteString(s[last:i])
		b.WriteString("****" + digits[len(digits)-4:])
		last = j
	}
	b.WriteString(s[last:])
	return b.String()
}

// joined says whether c joins the digits beside it into something else: a word, a decimal, a date or a path.
func joined(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || strings.IndexByte(".,/:-_*", c) >= 0
}
