// Package accounting is the organisation's accounting framework and policies (PRD-001 FR-ORG-10, open question 31;
// built in rev 0.62). The framework (FRS, SFRS(I) or SFRS for Small Entities) with its edition, and each accounting
// policy's words, are recorded as versions effective from the start of a financial year until a later version
// replaces them. A member who holds the Accountant role confirms the framework (the company's eligibility for it) and
// approves each policy, never the person who wrote that version (the founders' decision of 21 Oct 2026, rev 0.62).
//
// A financial year's statements are "management statements" until its framework is confirmed and every policy in force
// for it is approved; then they show the framework and edition (G4-017). Fyniq never writes a policy's words: the areas
// are a checklist, and the accountant decides what each one says.
package accounting

import (
	"fmt"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable error codes (ENG-23).
const (
	CodeInput     = "ACCOUNTING-01" // a framework or policy version that cannot be recorded as sent
	CodeOwn       = "ACCOUNTING-02" // the caller wrote the version they confirm or approve
	CodeSigned    = "ACCOUNTING-03" // the version is confirmed or approved already, or a later one replaces it
	CodeNoBasis   = "ACCOUNTING-NO-FRAMEWORK"
	CodeUnsigned  = "ACCOUNTING-UNCONFIRMED"
	CodeEdition   = "ACCOUNTING-EDITION"
	CodeNoPolicy  = "ACCOUNTING-NO-POLICY"
	CodeUnapprove = "ACCOUNTING-UNAPPROVED"
)

// The frameworks FR-ORG-10 names, by code.
const (
	FrameworkSmallEntities = "sfrs_se"
	FrameworkFRS           = "frs"
	FrameworkSFRSI         = "sfrs_i"
)

// Frameworks are the frameworks' codes, the recommended one first (open question 31: SFRS for Small Entities, the
// founders' decision of 14 Oct 2026, rev 0.19).
var Frameworks = []string{FrameworkSmallEntities, FrameworkFRS, FrameworkSFRSI}

// FrameworkNames are the frameworks' names, as statements show them.
var FrameworkNames = map[string]string{
	FrameworkSmallEntities: "SFRS for Small Entities",
	FrameworkFRS:           "FRS",
	FrameworkSFRSI:         "SFRS(I)",
}

// ThirdEditionFrom is the first day of the first annual period SFRS for Small Entities' Third Edition applies to: the
// ASC issued it on 18 Aug 2025 for annual periods beginning on or after 1 Jan 2027 (PRD-001 FR-PRJ-11 and open question
// 31, confirmed in rev 0.19; https://asc.acra.gov.sg/singapore-financial-reporting-standards-for-small-entities/,
// checked 21 Oct 2026).
var ThirdEditionFrom = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

// The editions of SFRS for Small Entities by the year they apply to. The ASC's page names the Third Edition; the one
// before it has no name there, only the periods it applies to, so it is called by them.
const (
	ThirdEdition   = "Third Edition"
	EditionOf2024  = "effective for periods beginning on or after 1 January 2024"
	statementTitle = "Financial statements"
	managementOnly = "Management statements"
)

// SmallEntityEdition is the edition of SFRS for Small Entities that applies to a financial year that starts on start.
func SmallEntityEdition(start time.Time) string {
	if start.Before(ThirdEditionFrom) {
		return EditionOf2024
	}
	return ThirdEdition
}

// Area is one accounting policy FR-ORG-10 asks for: what it decides, never what it says.
type Area struct {
	Key     string
	Title   string
	Decides string
	// Setting is the organisation setting that holds the figure the policy approves, when one does.
	Setting string
	// Reads are the requirements whose rules follow the policy.
	Reads []string
}

// Areas are FR-ORG-10's accounting policies, in its order, and untaken leave (FR-PPL-07 makes it one).
var Areas = []Area{
	{Key: "revenue", Title: "Revenue", Decides: "When revenue is recognised, including subscriptions and fixed-fee projects, and how a change to a contract or plan is accounted for.",
		Reads: []string{"FR-AR-15", "FR-PSP-07", "FR-PRJ-11"}},
	{Key: "government_grants", Title: "Government grants", Decides: "When a grant is recognised, and how it is presented."},
	{Key: "leases", Title: "Leases", Decides: "How leases are recognised and measured."},
	{Key: "development_costs", Title: "Development costs", Decides: "Whether development costs are expensed or recognised as an asset, and from when."},
	{Key: "impairment", Title: "Impairment", Decides: "When assets, receivables included, are tested for impairment, and how a loss is measured."},
	{Key: "safes_and_notes", Title: "SAFEs and convertible notes", Decides: "How an instrument is classified, as a liability or as equity, from its terms; each instrument is classified on its own.",
		Reads: []string{"FR-EQ-18"}},
	{Key: "conversion_measurement", Title: "Conversions into shares", Decides: "The measurement basis when a balance or an instrument converts into shares.",
		Reads: []string{"FR-EQ-06"}},
	{Key: "capitalisation_threshold", Title: "Capitalisation threshold", Decides: "The cost from which an item is recorded as an asset; Fyniq applies the amount set under Defaults.",
		Setting: "assets.capitalisation_threshold", Reads: []string{"FR-ORG-04"}},
	{Key: "untaken_leave", Title: "Untaken leave", Decides: "Whether untaken leave is recognised as a liability, and the floor below which no accrual is proposed.",
		Reads: []string{"FR-PPL-07"}},
}

// AreaOf returns the area with the key.
func AreaOf(key string) (Area, bool) {
	for _, a := range Areas {
		if a.Key == key {
			return a, true
		}
	}
	return Area{}, false
}

// Problem is one thing wrong with what was sent, on a field.
type Problem struct {
	Code    string
	Field   string
	Message string
	Fix     string
}

// Error is a refusal with a stable code.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

func day(t time.Time) string { return t.Format(time.DateOnly) }

// checkFrom refuses a day that is not the start of a financial year, or of one more than a year after the current one.
func checkFrom(cal period.Calendar, from, today time.Time) []Problem {
	n := cal.Number(from)
	if n == 0 || !cal.Year(n).StartsOn.Equal(from) {
		first := cal.Year(max(n, 1))
		return []Problem{{CodeInput, "from_on", fmt.Sprintf("%s is not the first day of a financial year", day(from)),
			fmt.Sprintf("Send the first day of the year it applies from, such as %s", day(first.StartsOn))}}
	}
	if n > max(cal.Number(today), 1)+1 {
		return []Problem{{CodeInput, "from_on", fmt.Sprintf("the financial year from %s starts more than a year after the current one", day(from)),
			"Record it once the year before it has started"}}
	}
	return nil
}

// CheckFramework checks a framework version before it is recorded.
func CheckFramework(cal period.Calendar, from, today time.Time, framework, edition string) []Problem {
	out := checkFrom(cal, from, today)
	if _, ok := FrameworkNames[framework]; !ok {
		out = append(out, Problem{CodeInput, "framework", fmt.Sprintf("%q is not a framework Fyniq knows", framework),
			"Choose sfrs_se (SFRS for Small Entities), frs (FRS) or sfrs_i (SFRS(I))"})
	}
	if e := strings.TrimSpace(edition); e == "" || len(e) > 120 {
		out = append(out, Problem{CodeInput, "edition", "the edition is empty or longer than 120 characters",
			"Name the edition, as the standard does, in at most 120 characters"})
	}
	return out
}

// CheckPolicy checks a policy version before it is recorded.
func CheckPolicy(cal period.Calendar, from, today time.Time, wording string) []Problem {
	out := checkFrom(cal, from, today)
	if w := strings.TrimSpace(wording); w == "" || len(w) > 4000 {
		out = append(out, Problem{CodeInput, "wording", "the policy is empty or longer than 4,000 characters",
			"Write the policy as the accountant decided it, in at most 4,000 characters"})
	}
	return out
}

// MaySign says whether signer may confirm or approve a version written by writer: never its writer, never twice, and
// never one a later version from the same year replaces.
func MaySign(what string, writer, signer string, signed *string, replaced bool) error {
	switch {
	case signer == writer:
		return &Error{CodeOwn, fmt.Sprintf("you wrote this %s, so another member who holds the Accountant role signs it", what),
			"Ask another member who holds the Accountant role (the founders' decision of 21 Oct 2026)"}
	case signed != nil:
		return &Error{CodeSigned, fmt.Sprintf("this %s is signed already", what), "Nothing to do: it is recorded once"}
	case replaced:
		return &Error{CodeSigned, fmt.Sprintf("a later version of this %s, from the same year, replaces it", what),
			"Sign the latest version instead"}
	}
	return nil
}

// FrameworkReplaced says whether a later framework version from the same year replaces v.
func FrameworkReplaced(all []storage.FrameworkVersion, v storage.FrameworkVersion) bool {
	for _, f := range all {
		if f.FromOn.Equal(v.FromOn) && f.Version > v.Version {
			return true
		}
	}
	return false
}

// PolicyReplaced says whether a later version of the policy from the same year replaces v.
func PolicyReplaced(all []storage.PolicyVersion, v storage.PolicyVersion) bool {
	for _, p := range all {
		if p.Area == v.Area && p.FromOn.Equal(v.FromOn) && p.Version > v.Version {
			return true
		}
	}
	return false
}

// later says whether version a, from fromA, takes precedence over b: the later year, then the later version.
func later(fromA time.Time, a int, fromB time.Time, b int) bool {
	return fromA.After(fromB) || (fromA.Equal(fromB) && a > b)
}

// FrameworkFor is the framework version in force for the financial year that starts on start: the one from the latest
// year on or before it, the latest written when several are from that year; nil when none is.
func FrameworkFor(all []storage.FrameworkVersion, start time.Time) *storage.FrameworkVersion {
	var out *storage.FrameworkVersion
	for i := range all {
		f := &all[i]
		if !f.FromOn.After(start) && (out == nil || later(f.FromOn, f.Version, out.FromOn, out.Version)) {
			out = f
		}
	}
	return out
}

// PolicyFor is an area's policy version in force for the financial year that starts on start, as FrameworkFor.
func PolicyFor(all []storage.PolicyVersion, area string, start time.Time) *storage.PolicyVersion {
	var out *storage.PolicyVersion
	for i := range all {
		p := &all[i]
		if p.Area == area && !p.FromOn.After(start) && (out == nil || later(p.FromOn, p.Version, out.FromOn, out.Version)) {
			out = p
		}
	}
	return out
}

// Check is one thing that keeps a year's statements "management statements".
type Check struct {
	Code   string
	Area   string // the policy's area; empty for the framework
	Title  string
	Detail string
	Fix    string
}

// Year is a financial year's accounting framework and policies, and what its statements are titled.
type Year struct {
	Span      period.Span
	Framework *storage.FrameworkVersion
	Policies  map[string]*storage.PolicyVersion // by area; nil where none is in force
	// Checks are what stands between the year and approved statements; none once it is approved.
	Checks []Check
	// Approved is true once the framework in force is confirmed and every policy in force is approved.
	Approved bool
	// Title is how the year's statements are titled; Basis says what they are prepared under, once approved.
	Title string
	Basis string
	// Edition is the edition of SFRS for Small Entities that applies to the year, which Fyniq recommends.
	Edition string
}

// YearOf works out a financial year's framework and policies, and its statements' title (FR-ORG-10, G4-017).
func YearOf(span period.Span, frameworks []storage.FrameworkVersion, policies []storage.PolicyVersion) Year {
	y := Year{Span: span, Policies: map[string]*storage.PolicyVersion{}, Edition: SmallEntityEdition(span.StartsOn), Title: managementOnly}
	y.Framework = FrameworkFor(frameworks, span.StartsOn)
	switch f := y.Framework; {
	case f == nil:
		y.Checks = append(y.Checks, Check{Code: CodeNoBasis, Title: "No framework recorded",
			Detail: fmt.Sprintf("No accounting framework is recorded for the financial year from %s.", day(span.StartsOn)),
			Fix:    "Record the framework and its edition from this year or an earlier one"})
	case f.Framework == FrameworkSmallEntities && !span.StartsOn.Before(ThirdEditionFrom) && !strings.EqualFold(strings.TrimSpace(f.Edition), ThirdEdition):
		y.Checks = append(y.Checks, Check{Code: CodeEdition, Title: "The Third Edition applies",
			Detail: fmt.Sprintf("SFRS for Small Entities' Third Edition applies to financial years beginning on or after 1 January 2027; the framework in force names the edition %q.", f.Edition),
			Fix:    fmt.Sprintf("Record SFRS for Small Entities, %s, from %s, and have it confirmed", ThirdEdition, day(span.StartsOn))})
	case f.ConfirmedBy == nil:
		y.Checks = append(y.Checks, Check{Code: CodeUnsigned, Title: "Framework not confirmed",
			Detail: fmt.Sprintf("The accountant has not confirmed that the company may use %s.", FrameworkNames[f.Framework]),
			Fix:    "A member who holds the Accountant role, other than its writer, confirms it"})
	}
	for _, a := range Areas {
		p := PolicyFor(policies, a.Key, span.StartsOn)
		y.Policies[a.Key] = p
		switch {
		case p == nil:
			y.Checks = append(y.Checks, Check{Code: CodeNoPolicy, Area: a.Key, Title: a.Title + ": not recorded",
				Detail: fmt.Sprintf("No %s policy is recorded for the financial year from %s.", strings.ToLower(a.Title), day(span.StartsOn)),
				Fix:    "Record the policy as the accountant decides it; where it does not apply, say so"})
		case p.ApprovedBy == nil:
			y.Checks = append(y.Checks, Check{Code: CodeUnapprove, Area: a.Key, Title: a.Title + ": not approved",
				Detail: fmt.Sprintf("Version %d of the %s policy waits for the accountant's approval.", p.Version, strings.ToLower(a.Title)),
				Fix:    "A member who holds the Accountant role, other than its writer, approves it"})
		}
	}
	if len(y.Checks) == 0 {
		y.Approved, y.Title = true, statementTitle
		y.Basis = fmt.Sprintf("Prepared under %s (%s)", FrameworkNames[y.Framework.Framework], strings.TrimSpace(y.Framework.Edition))
	}
	return y
}
