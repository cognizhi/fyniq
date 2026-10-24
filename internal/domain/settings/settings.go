// Package settings is the catalogue of the organisation's settings and their rules (FR-ORG-04, FR-ORG-06,
// FR-ORG-07, FR-ORG-08). Every setting ships at its recommended value, with a one-line reason and a link to
// the guide; a change is effective-dated, audited and never back-dated, and "reset to recommended" records
// the recommended value as a new change. A setting under dual control (FM-1: thresholds, retention, AI and
// MCP settings, approval policies) cannot change before the approval engine exists, so it is refused,
// never applied (PRD-001 rev 0.34). The database (migration 000028) keeps the records' retention from
// shortening.
package settings

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is what a setting holds, and how its value is written.
type Kind string

// The kinds of setting.
const (
	Switch    Kind = "switch"     // "true" or "false"
	Days      Kind = "days"       // a whole number of days
	Years     Kind = "years"      // a whole number of years
	AmountSGD Kind = "amount_sgd" // Singapore dollars, with cents, such as "50.00"
	Percent   Kind = "percent"    // a percentage with at most two decimals, such as "3" or "2.5"
	Text      Kind = "text"       // a fixed text, such as the data region
)

// Definition is one setting: what it holds, its recommended value and why, and the rules a change to it meets.
type Definition struct {
	Key          string
	Group        string
	Label        string
	Kind         Kind
	Recommended  string
	Reason       string // one line: why the recommended value
	Guide        string // the guide page and section that explains it, such as /help/organisation-settings#features
	Requirement  string // the PRD requirement it implements
	DualControl  bool   // FM-1: a change needs a second approver
	Fixed        bool   // set by the platform: shown, never changed
	LengthenOnly bool   // a change may only lengthen it
	Min, Max     string // the range of a number, inclusive
}

// guide is the guide page every setting links to (docs/user-guide/organisation-settings.md, served at /help/).
const guide = "/help/organisation-settings#"

// Catalogue is every setting, by key. A setting is added here, with its recommended value, reason and guide
// section, before anything reads it.
var Catalogue = []Definition{
	{Key: "accounting.signing_step_up", Group: "Approvals", Label: "Second factor to sign the accounting framework and policies", Kind: Switch, Recommended: "true",
		Reason: "The accountant's signature turns management statements into financial statements, so it asks for a passkey or authenticator app, as other approvals do",
		Guide:  guide + "accounting-sign-off", Requirement: "FR-ORG-10", DualControl: true},
	{Key: "approvals.self_approval_threshold", Group: "Approvals", Label: "Self-approval threshold", Kind: AmountSGD, Recommended: "2000.00",
		Reason: "In Founder mode a founder posts a journal of up to S$2,000 alone, flagged for the weekly review; above it another founder approves",
		Guide:  guide + "self-approval-threshold", Requirement: "FM-2", DualControl: true, Min: "0.00", Max: "1000000.00"},
	{Key: "assets.capitalisation_threshold", Group: "Defaults", Label: "Capitalisation threshold", Kind: AmountSGD, Recommended: "1000.00",
		Reason: "Purchases of S$1,000 or more each are offered as fixed assets; smaller ones are expensed. The accountant confirms it",
		Guide:  guide + "capitalisation-threshold", Requirement: "FR-ORG-04", DualControl: true, Min: "0.00", Max: "1000000.00"},
	{Key: "bank.tolerance_base", Group: "Settlement tolerances", Label: "Tolerance in the base currency", Kind: AmountSGD, Recommended: "1.00",
		Reason: "A payment within S$1.00 of its document clears the difference to FX and rounding (7030), as the workbook did",
		Guide:  guide + "settlement-tolerances", Requirement: "FR-BNK-08", DualControl: true, Min: "0.00", Max: "100.00"},
	{Key: "bank.tolerance_cap_document", Group: "Settlement tolerances", Label: "Cap per document", Kind: AmountSGD, Recommended: "50.00",
		Reason: "No tolerance clears more than S$50 on one document, so a tolerance never hides a short payment",
		Guide:  guide + "settlement-tolerances", Requirement: "FR-BNK-08", DualControl: true, Min: "0.00", Max: "10000.00"},
	{Key: "bank.tolerance_cap_month", Group: "Settlement tolerances", Label: "Cap per month", Kind: AmountSGD, Recommended: "200.00",
		Reason: "Tolerances clear at most S$200 a month in all; anything more needs a reason-coded adjustment",
		Guide:  guide + "settlement-tolerances", Requirement: "FR-BNK-08", DualControl: true, Min: "0.00", Max: "100000.00"},
	{Key: "bank.tolerance_foreign_minimum", Group: "Settlement tolerances", Label: "Foreign-currency tolerance, at least", Kind: AmountSGD,
		Recommended: "1.00", Reason: "A foreign-currency residual within the greater of S$1.00 and the percentage clears to 7030",
		Guide: guide + "settlement-tolerances", Requirement: "FR-BNK-08", DualControl: true, Min: "0.00", Max: "100.00"},
	{Key: "bank.tolerance_foreign_percent", Group: "Settlement tolerances", Label: "Foreign-currency tolerance, percentage of the document",
		Kind: Percent, Recommended: "3", Reason: "Bank charges and rate differences on foreign payments are usually within 3% of the document",
		Guide: guide + "settlement-tolerances", Requirement: "FR-BNK-08", DualControl: true, Min: "0", Max: "10"},
	{Key: "close.variance_amount", Group: "Period close", Label: "Variance to explain, amount", Kind: AmountSGD, Recommended: "1000.00",
		Reason: "At the close, an income or expense account that moved by more than S$1,000 against the previous month, and by more than the percentage, needs a short explanation",
		Guide:  guide + "variances-at-the-close", Requirement: "FR-GL-14", DualControl: true, Min: "0.00", Max: "1000000.00"},
	{Key: "close.variance_percent", Group: "Period close", Label: "Variance to explain, percentage of the previous month", Kind: Percent, Recommended: "10",
		Reason: "At the close, an income or expense account that moved by more than 10% of the previous month's total, and by more than the amount, needs a short explanation",
		Guide:  guide + "variances-at-the-close", Requirement: "FR-GL-14", DualControl: true, Min: "0", Max: "100"},
	{Key: "data.region", Group: "Data region and retention", Label: "Data region", Kind: Text, Recommended: "asia-southeast1",
		Reason: "Every record is kept and processed in Google Cloud's Singapore region",
		Guide:  guide + "data-region-and-retention", Requirement: "FR-ORG-07", Fixed: true},
	{Key: "email.quarantine_days", Group: "Email", Label: "Quarantined mail kept for", Kind: Days, Recommended: "30",
		Reason: "Mail from an unknown sender is deleted 30 days after it arrived unless someone accepts the sender; 0 keeps it until someone decides",
		Guide:  guide + "email-in", Requirement: "FR-INT-22", Min: "0", Max: "365"},
	{Key: "features.ai_assistant", Group: "Features", Label: "AI assistant", Kind: Switch, Recommended: "false",
		Reason: "AI is off until an Owner turns it on with a second approver (closed by default)",
		Guide:  guide + "features", Requirement: "FR-ORG-06", DualControl: true},
	{Key: "features.multi_currency", Group: "Features", Label: "Multi-currency", Kind: Switch, Recommended: "true",
		Reason: "Start-ups usually pay some bills in other currencies, such as cloud and software subscriptions in US dollars",
		Guide:  guide + "features", Requirement: "FR-ORG-06"},
	{Key: "features.payroll", Group: "Features", Label: "Payroll", Kind: Switch, Recommended: "false",
		Reason: "Off while the company has no employees and the founders draw no salary; turn it on before the first pay run",
		Guide:  guide + "features", Requirement: "FR-ORG-06"},
	{Key: "features.portals", Group: "Features", Label: "Portals for customers and investors", Kind: Switch, Recommended: "false",
		Reason: "Off until there are customers or investors to invite", Guide: guide + "features", Requirement: "FR-ORG-06"},
	{Key: "features.projects", Group: "Features", Label: "Projects and time", Kind: Switch, Recommended: "false",
		Reason: "Off until you track time or costs by project", Guide: guide + "features", Requirement: "FR-ORG-06"},
	{Key: "gst.round_per_line", Group: "Defaults", Label: "Round GST per line", Kind: Switch, Recommended: "false",
		Reason: "GST is worked out once per GST code on a document's total for it (RND-GST-EXCL); IRAS accepts either method used consistently",
		Guide:  guide + "gst-rounding", Requirement: "RND-GST-EXCL"},
	{Key: "notify.email_details", Group: "Notifications", Label: "Details in notification email", Kind: Switch, Recommended: "false",
		Reason: "Notification email says what kind of thing happened and links to Fyniq; amounts, names and reasons stay in the app, since the email provider keeps mail outside Singapore",
		Guide:  guide + "notifications", Requirement: "FR-NTF-02", DualControl: true},
	{Key: "purchases.payment_terms_days", Group: "Defaults", Label: "Payment terms for bills", Kind: Days, Recommended: "30",
		Reason: "A bill from a vendor with no terms of its own falls due 30 days after its date",
		Guide:  guide + "payment-terms", Requirement: "FR-ORG-04", Min: "0", Max: "365"},
	{Key: "purchases.reminder_days", Group: "Defaults", Label: "Remind of bills due within", Kind: Days, Recommended: "3",
		Reason: "The Inbox lists the bills to pay 3 days before they fall due or their planned payment date, and the overdue ones",
		Guide:  guide + "bill-reminders", Requirement: "FR-AP-13", Min: "0", Max: "30"},
	{Key: "retention.ai_conversations_days", Group: "Data region and retention", Label: "AI conversations kept for", Kind: Days, Recommended: "365",
		Reason: "Conversations with the AI assistant are kept a year, then purged; proposals follow the audit trail's retention",
		Guide:  guide + "data-region-and-retention", Requirement: "FR-ORG-07", DualControl: true, Min: "30", Max: "3650"},
	{Key: "retention.records_years", Group: "Data region and retention", Label: "Records kept for", Kind: Years, Recommended: "7",
		Reason: "Ledger, documents, files and the audit trail are kept 7 years after the financial year they relate to; it can only lengthen",
		Guide:  guide + "data-region-and-retention", Requirement: "FR-ORG-07", DualControl: true, LengthenOnly: true, Min: "7", Max: "100"},
	{Key: "sales.payment_terms_days", Group: "Defaults", Label: "Payment terms for invoices", Kind: Days, Recommended: "30",
		Reason: "An invoice to a customer with no terms of its own falls due 30 days after its date",
		Guide:  guide + "payment-terms", Requirement: "FR-ORG-04", Min: "0", Max: "365"},
	{Key: "sales.simplified_tax_invoices", Group: "Defaults", Label: "Simplified tax invoices up to S$1,000", Kind: Switch, Recommended: "false",
		Reason: "Every invoice is a full tax invoice, with the customer's name and address and the GST breakdown that customers' accounts teams ask for",
		Guide:  guide + "simplified-tax-invoices", Requirement: "FR-AR-05"},
}

// Lookup returns the setting with key, if there is one.
func Lookup(key string) (Definition, bool) {
	i := slices.IndexFunc(Catalogue, func(d Definition) bool { return d.Key == key })
	if i < 0 {
		return Definition{}, false
	}
	return Catalogue[i], true
}

// The codes of a setting's rules.
const (
	CodeValue        = "SET-01" // the value does not fit the setting
	CodeBackdated    = "SET-02" // the change would take effect before today
	CodeShortened    = "SET-03" // the change would shorten a lengthen-only setting
	CodeFixed        = "SET-04" // the platform sets it
	CodeValueOrReset = "SET-06" // a change sends a value or asks for a reset, not both or neither
)

// Problem is one rule that failed.
type Problem struct {
	Code, Field, Message, Fix string
}

var (
	whole   = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})$`)
	percent = regexp.MustCompile(`^(0|[1-9][0-9]{0,2})(\.[0-9]{1,2})?$`)
	sgd     = money.MustLookup("SGD")
)

// Normalise reads raw as a value of d, and returns it as stored: "true", "30", "50.00" or "2.5". It refuses
// a value outside d's range.
func Normalise(d Definition, raw string) (string, *Problem) {
	bad := func(fix string) (string, *Problem) {
		return "", &Problem{CodeValue, "value", fmt.Sprintf("%q is not a value for %s", raw, d.Label), fix}
	}
	switch d.Kind {
	case Switch:
		if raw != "true" && raw != "false" {
			return bad("Send true or false")
		}
		return raw, nil
	case Days, Years:
		if !whole.MatchString(raw) {
			return bad(fmt.Sprintf("Send a whole number of %s, from %s to %s", d.Kind, d.Min, d.Max))
		}
		n, _ := strconv.Atoi(raw) // at most six digits, checked above
		lo, _ := strconv.Atoi(d.Min)
		hi, _ := strconv.Atoi(d.Max)
		if n < lo || n > hi {
			return bad(fmt.Sprintf("Send a whole number of %s, from %s to %s", d.Kind, d.Min, d.Max))
		}
		return raw, nil
	case AmountSGD:
		m, err := money.ParseField(raw, sgd, "value")
		if err != nil || !within(m.Decimal(), d) {
			return bad(fmt.Sprintf("Send an amount in Singapore dollars with at most two decimals, from %s to %s, such as 50.00", d.Min, d.Max))
		}
		return m.String(), nil
	case Percent:
		var v apd.Decimal
		if !percent.MatchString(raw) {
			return bad(fmt.Sprintf("Send a percentage from %s to %s with at most two decimals, such as 3 or 2.5", d.Min, d.Max))
		}
		if _, _, err := v.SetString(raw); err != nil || !within(&v, d) {
			return bad(fmt.Sprintf("Send a percentage from %s to %s with at most two decimals, such as 3 or 2.5", d.Min, d.Max))
		}
		return raw, nil
	}
	return bad("This setting cannot be changed")
}

// within reports whether v is in d's range.
func within(v *apd.Decimal, d Definition) bool {
	var lo, hi apd.Decimal
	if _, _, err := lo.SetString(d.Min); err != nil {
		return false
	}
	if _, _, err := hi.SetString(d.Max); err != nil {
		return false
	}
	return v.Cmp(&lo) >= 0 && v.Cmp(&hi) <= 0
}

// Value is a setting's value on a day: the latest change in effect then, or the recommended value.
type Value struct {
	Value         string
	EffectiveFrom *time.Time // nil while it holds the recommended value it shipped with
	Version       int        // the change's version; 0 for the recommended value it shipped with
}

// On returns d's value on day, from its changes (any order). Of two changes taking effect on the same day,
// the later version wins.
func On(d Definition, changes []storage.SettingChange, day time.Time) Value {
	out := Value{Value: d.Recommended}
	var at *storage.SettingChange
	for i := range changes {
		c := &changes[i]
		if c.Key != d.Key || c.EffectiveFrom.After(day) {
			continue
		}
		if at == nil || c.EffectiveFrom.After(at.EffectiveFrom) || (c.EffectiveFrom.Equal(at.EffectiveFrom) && c.Version > at.Version) {
			at = c
		}
	}
	if at != nil {
		from := at.EffectiveFrom
		out = Value{Value: at.Value, EffectiveFrom: &from, Version: at.Version}
	}
	return out
}

// Gate returns the problem with changing d at all, or nil: the platform sets it. Run it before reading the value,
// so the refusal names the rule that refused it. A setting under dual control (FM-1) is not refused: its change
// waits for a second approver in the approval engine (rev 0.36).
func Gate(d Definition) *Problem {
	if d.Fixed {
		return &Problem{CodeFixed, "value", fmt.Sprintf("%s is set by the platform", d.Label), "Nothing to change: it is shown so you know where your data is kept"}
	}
	return nil
}

// Check returns every rule a change of d to value (already normalised), taking effect on from, breaks; today
// is the organisation's date, and changes are d's recorded changes. Gate is not part of it.
func Check(d Definition, value string, from, today time.Time, changes []storage.SettingChange) []Problem {
	var out []Problem
	if from.Before(today) {
		out = append(out, Problem{CodeBackdated, "effective_from", fmt.Sprintf("%s is before today, %s", from.Format(time.DateOnly), today.Format(time.DateOnly)),
			"Choose today or a later date: a setting never changes in the past"})
	}
	if d.LengthenOnly {
		// It may not drop below the value in effect on its date, and no change already scheduled after it may
		// then drop below it.
		n, _ := strconv.Atoi(value) // normalised: a whole number
		was, _ := strconv.Atoi(On(d, changes, from).Value)
		shortens := n < was
		for _, c := range changes {
			later, _ := strconv.Atoi(c.Value)
			shortens = shortens || (c.Key == d.Key && c.EffectiveFrom.After(from) && later < n)
		}
		if shortens {
			out = append(out, Problem{CodeShortened, "value", fmt.Sprintf("%s cannot shorten: %d %s from %s", d.Label, n, d.Kind, from.Format(time.DateOnly)),
				"Keep it or lengthen it: records already kept under a retention keep it"})
		}
	}
	return out
}
