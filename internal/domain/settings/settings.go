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
	"strings"
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
	Minutes   Kind = "minutes"    // a whole number of minutes
	Hours     Kind = "hours"      // a whole number of hours
	Days      Kind = "days"       // a whole number of days
	Years     Kind = "years"      // a whole number of years
	Months    Kind = "months"     // a whole number of months
	AmountSGD Kind = "amount_sgd" // Singapore dollars, with cents, such as "50.00"
	Percent   Kind = "percent"    // a percentage with at most two decimals, such as "3" or "2.5"
	Text      Kind = "text"       // a fixed text, such as the data region
	// AccountCode is a chart account's code, such as "6460": the account a posting takes, by its code.
	AccountCode Kind = "account_code"
	// Choice is one of the definition's Choices, such as "keep".
	Choice Kind = "choice"
)

// Definition is one setting: what it holds, its recommended value and why, and the rules a change to it meets.
type Definition struct {
	Key          string
	Group        string
	Label        string
	Kind         Kind
	Recommended  string
	Reason       string   // one line: why the recommended value
	Guide        string   // the guide page and section that explains it, such as /help/organisation-settings#features
	Requirement  string   // the PRD requirement it implements
	DualControl  bool     // FM-1: a change needs a second approver
	Fixed        bool     // set by the platform: shown, never changed
	LengthenOnly bool     // a change may only lengthen it
	Min, Max     string   // the range of a number, inclusive
	Choices      []string // a Choice's values
}

// guide is the guide page every setting links to (docs/user-guide/organisation-settings.md, served at /help/).
const guide = "/help/organisation-settings#"

// Catalogue is every setting, by key. A setting is added here, with its recommended value, reason and guide
// section, before anything reads it.
var Catalogue = []Definition{
	{Key: "accounting.signing_step_up", Group: "Approvals", Label: "Second factor to sign the accounting framework and policies", Kind: Switch, Recommended: "true",
		Reason: "The accountant's signature turns management statements into financial statements, so it asks for a passkey or authenticator app, as other approvals do",
		Guide:  guide + "accounting-sign-off", Requirement: "FR-ORG-10", DualControl: true},
	{Key: "approvals.break_glass_cap", Group: "Approvals", Label: "Break-glass payment cap", Kind: AmountSGD, Recommended: "20000.00",
		Reason: "In Founder mode, while the other founder is unreachable, an Owner's break-glass releases waiting payment batches of up to S$20,000 in all; more waits for the other founder (the founders' decision of 16 Oct 2026)",
		Guide:  guide + "break-glass-payment-cap", Requirement: "FM-8", DualControl: true, Min: "0.00", Max: "1000000.00"},
	{Key: "approvals.self_approval_threshold", Group: "Approvals", Label: "Self-approval threshold", Kind: AmountSGD, Recommended: "2000.00",
		Reason: "In Founder mode a founder posts a journal of up to S$2,000 alone, flagged for the weekly review; above it another founder approves",
		Guide:  guide + "self-approval-threshold", Requirement: "FM-2", DualControl: true, Min: "0.00", Max: "1000000.00"},
	{Key: "assets.capitalisation_threshold", Group: "Defaults", Label: "Capitalisation threshold", Kind: AmountSGD, Recommended: "1000.00",
		Reason: "Purchases of S$1,000 or more each are offered as fixed assets; smaller ones are expensed. The accountant confirms it",
		Guide:  guide + "capitalisation-threshold", Requirement: "FR-ORG-04", DualControl: true, Min: "0.00", Max: "1000000.00"},
	{Key: "assets.disposal_account", Group: "Defaults", Label: "Gain or loss on disposal account", Kind: AccountCode, Recommended: "7050",
		Reason: "When an asset is sold or written off, the difference between what it fetched and what it was still worth goes to 7050 Gain / (loss) on disposal of assets (the owner's decision of 27 Oct 2026; the accountant confirms it)",
		Guide:  guide + "disposal-account", Requirement: "FR-FA-03", DualControl: true},
	{Key: "assets.disposal_clearing_account", Group: "Defaults", Label: "Disposal clearing account", Kind: AccountCode, Recommended: "1160",
		Reason: "An asset sold on a sales invoice is invoiced to 1160 Asset disposal clearing, which its disposal clears, so the gain or loss falls in the disposal's month (the owner's decision of 27 Oct 2026; the accountant confirms it). A chart loaded before 1160 existed adds it, or names another balance-sheet account",
		Guide:  guide + "disposal-clearing-account", Requirement: "FR-FA-03", DualControl: true},
	{Key: "bank.auto_cap_line", Group: "Banking", Label: "Auto-reconcile, the largest line", Kind: AmountSGD, Recommended: "500.00",
		Reason: "Lines of up to S$500 that match exactly one open document, a transfer's other side or an approved rule are reconciled without a click; larger ones wait for a person (the owner's decision of 25 Oct 2026)",
		Guide:  guide + "auto-reconcile", Requirement: "FR-BNK-14", DualControl: true, Min: "0.00", Max: "1000000.00"},
	{Key: "bank.auto_cap_rule_day", Group: "Banking", Label: "Auto-reconcile, per rule a day", Kind: AmountSGD, Recommended: "2000.00",
		Reason: "One rule reconciles up to S$2,000 of lines a day by itself, so a wrong rule's harm is bounded until the weekly review (the owner's decision of 25 Oct 2026)",
		Guide:  guide + "auto-reconcile", Requirement: "FR-BNK-14", DualControl: true, Min: "0.00", Max: "1000000.00"},
	{Key: "bank.charges_account", Group: "Banking", Label: "Bank charges account", Kind: AccountCode, Recommended: "6460",
		Reason: "A fee a bank or provider reports on a transfer between your accounts is a bank charge, 6460 in the standard chart; the rest of a conversion's difference goes to FX and rounding (7030)",
		Guide:  guide + "bank-charges", Requirement: "FR-BNK-09", DualControl: true},
	{Key: "bank.signal_expiry_days", Group: "Banking", Label: "Incoming-funds alerts expire after", Kind: Days, Recommended: "7",
		Reason: "A bank's alert shows an invoice as likely paid until the statement line reconciles it; after 7 days with no such line it expires, so a payment that never came stops looking paid (the owner's decision of 26 Oct 2026)",
		Guide:  guide + "incoming-funds-alerts", Requirement: "FR-BNK-16", DualControl: true, Min: "1", Max: "90"},
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
	{Key: "calendar.pays_staff", Group: "Statutory calendar", Label: "Pays staff", Kind: Switch, Recommended: "false",
		Reason: "The calendar adds CPF each month and employment income each year (IR8A, AIS) from the day this is on; off while the company pays no one",
		Guide:  guide + "statutory-calendar", Requirement: "FR-TAX-07"},
	{Key: "claims.advances_account", Group: "Expense claims", Label: "Advances account", Kind: AccountCode, Recommended: "1250",
		Reason: "Money advanced to a member for a trip waits on 1250 Staff & director advances until their claims settle it or they refund the rest",
		Guide:  guide + "advances-account", Requirement: "FR-AP-17", DualControl: true},
	{Key: "claims.deadline_days", Group: "Expense claims", Label: "Claim deadline", Kind: Days, Recommended: "60",
		Reason: "A receipt or trip claimed more than 60 days after its date is late: the claim says why, and finance approves it too",
		Guide:  guide + "claim-deadline", Requirement: "FR-AP-15", DualControl: true, Min: "1", Max: "365"},
	{Key: "claims.mileage_rate_car", Group: "Expense claims", Label: "Mileage rate for a car, per km", Kind: AmountSGD, Recommended: "0.60",
		Reason: "IRAS prescribes no rate per km, only a reasonable one: S$0.60 a km is the owner's decision of 27 Oct 2026, which the accountant confirms",
		Guide:  guide + "mileage-rates", Requirement: "FR-AP-16", DualControl: true, Min: "0.01", Max: "5.00"},
	{Key: "claims.mileage_rate_motorcycle", Group: "Expense claims", Label: "Mileage rate for a motorcycle, per km", Kind: AmountSGD, Recommended: "0.25",
		Reason: "IRAS prescribes no rate per km, only a reasonable one: S$0.25 a km is the owner's decision of 27 Oct 2026, which the accountant confirms",
		Guide:  guide + "mileage-rates", Requirement: "FR-AP-16", DualControl: true, Min: "0.01", Max: "5.00"},
	{Key: "close.variance_amount", Group: "Period close", Label: "Variance to explain, amount", Kind: AmountSGD, Recommended: "1000.00",
		Reason: "At the close, an income or expense account that moved by more than S$1,000 against the previous month, and by more than the percentage, needs a short explanation",
		Guide:  guide + "variances-at-the-close", Requirement: "FR-GL-14", DualControl: true, Min: "0.00", Max: "1000000.00"},
	{Key: "close.variance_percent", Group: "Period close", Label: "Variance to explain, percentage of the previous month", Kind: Percent, Recommended: "10",
		Reason: "At the close, an income or expense account that moved by more than 10% of the previous month's total, and by more than the amount, needs a short explanation",
		Guide:  guide + "variances-at-the-close", Requirement: "FR-GL-14", DualControl: true, Min: "0", Max: "100"},
	{Key: "corporate.agm_route", Group: "Statutory calendar", Label: "Annual general meeting", Kind: Choice, Recommended: "hold",
		Choices: []string{"hold", "send_statements", "dispensed"},
		Reason:  "A private company holds its AGM within six months of its year's end, unless it sends its financial statements to every member within five months, or all members resolved to dispense with it (ACRA)",
		Guide:   guide + "statutory-calendar", Requirement: "FR-TAX-07"},
	{Key: "corporate.overseas_branch_register", Group: "Statutory calendar", Label: "Keeps a branch register outside Singapore", Kind: Switch, Recommended: "false",
		Reason: "ACRA gives a company with share capital and an overseas branch register eight months after its year's end to file its annual return, instead of seven",
		Guide:  guide + "statutory-calendar", Requirement: "FR-TAX-07"},
	{Key: "data.region", Group: "Data region and retention", Label: "Data region", Kind: Text, Recommended: "asia-southeast1",
		Reason: "Every record is kept and processed in Google Cloud's Singapore region",
		Guide:  guide + "data-region-and-retention", Requirement: "FR-ORG-07", Fixed: true},
	{Key: "email.quarantine_days", Group: "Email", Label: "Quarantined mail kept for", Kind: Days, Recommended: "30",
		Reason: "Mail from an unknown sender is deleted 30 days after it arrived unless someone accepts the sender; 0 keeps it until someone decides",
		Guide:  guide + "email-in", Requirement: "FR-INT-22", Min: "0", Max: "365"},
	{Key: "evidence.bill", Group: "Evidence", Label: "Evidence for bills", Kind: Choice, Recommended: "required", Choices: []string{"required", "recommended", "optional"},
		Reason: "A bill posts only with the supplier's invoice or receipt attached (FR-DOC-06; the owner's decision of 6 Nov 2026)",
		Guide:  guide + "evidence-rules", Requirement: "FR-DOC-06", DualControl: true},
	{Key: "evidence.journal_threshold", Group: "Evidence", Label: "Evidence for manual journals above", Kind: AmountSGD, Recommended: "1000.00",
		Reason: "A manual journal whose debits are above S$1,000 rests on a supporting calculation or document (FR-DOC-06)",
		Guide:  guide + "evidence-rules", Requirement: "FR-DOC-06", DualControl: true, Min: "0.00", Max: "1000000.00"},
	{Key: "evidence.manual_journal", Group: "Evidence", Label: "Evidence for manual journals", Kind: Choice, Recommended: "required", Choices: []string{"required", "recommended", "optional"},
		Reason: "A manual journal above the threshold posts only with its supporting calculation or document attached (FR-DOC-06)",
		Guide:  guide + "evidence-rules", Requirement: "FR-DOC-06", DualControl: true},
	{Key: "evidence.statutory_filing", Group: "Evidence", Label: "Evidence for statutory filings and payments", Kind: Choice, Recommended: "required",
		Choices: []string{"required", "recommended", "optional"},
		Reason:  "A filing or payment to IRAS, CPF Board or ACRA rests on its acknowledgement (FR-DOC-06)",
		Guide:   guide + "evidence-rules", Requirement: "FR-DOC-06", DualControl: true},
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
	{Key: "founders.default_route", Group: "Founders' money", Label: "Default route to settle what the company owes a founder", Kind: Choice,
		Recommended: "keep", Choices: []string{"keep", "repay", "convert", "waive"},
		Reason: "Keep it as an interest-free loan, repayable on demand, until the first priced round: no tax or CPF on money never received, and nobody's ownership moves at an arbitrary price",
		Guide:  guide + "founders-money", Requirement: "FR-EQ-05", DualControl: true},
	{Key: "founders.price_basis", Group: "Founders' money", Label: "Default price basis to convert to shares", Kind: Choice,
		Recommended: "latest_priced_round", Choices: []string{"latest_priced_round", "valuation"},
		Reason: "Convert at the price of the latest priced round, which outside investors paid; a valuation is the alternative when there is none",
		Guide:  guide + "founders-money", Requirement: "FR-EQ-05", DualControl: true},
	{Key: "founders.registration_days", Group: "Founders' money", Label: "Flag a conversion not registered by ACRA after", Kind: Days, Recommended: "14",
		Reason: "ACRA's guidance expects a return of allotment within 14 days; the Act sets no deadline for private companies",
		Guide:  guide + "founders-money", Requirement: "FR-EQ-09", Min: "1", Max: "90"},
	{Key: "gst.round_per_line", Group: "Defaults", Label: "Round GST per line", Kind: Switch, Recommended: "false",
		Reason: "GST is worked out once per GST code on a document's total for it (RND-GST-EXCL); IRAS accepts either method used consistently",
		Guide:  guide + "gst-rounding", Requirement: "RND-GST-EXCL"},
	{Key: "gst.threshold_warning_percent", Group: "GST registration", Label: "Warn when the year's taxable supplies reach", Kind: Percent, Recommended: "80",
		Reason: "Months of notice before the S$1 million test is met: IRAS gives 30 days to apply once a forecast exceeds it [accountant to confirm]",
		Guide:  guide + "gst-registration-monitor", Requirement: "FR-TAX-05", Min: "50", Max: "99"},
	{Key: "notify.email_details", Group: "Notifications", Label: "Details in notification email", Kind: Switch, Recommended: "false",
		Reason: "Notification email says what kind of thing happened and links to Fyniq; amounts, names and reasons stay in the app, since the email provider keeps mail outside Singapore",
		Guide:  guide + "notifications", Requirement: "FR-NTF-02", DualControl: true},
	{Key: "planning.buffer_months", Group: "Planning", Label: "Cash buffer to keep", Kind: Months, Recommended: "6",
		Reason: "Months of average net burn a forecast keeps as a buffer: its funding gap, and the short-term cash flow's buffer line and alert, as the workbook and C.1 #36",
		Guide:  guide + "cash-buffer", Requirement: "FR-PLN-03", Min: "0", Max: "36"},
	{Key: "purchases.payment_terms_days", Group: "Defaults", Label: "Payment terms for bills", Kind: Days, Recommended: "30",
		Reason: "A bill from a vendor with no terms of its own falls due 30 days after its date",
		Guide:  guide + "payment-terms", Requirement: "FR-ORG-04", Min: "0", Max: "365"},
	{Key: "purchases.reminder_days", Group: "Defaults", Label: "Remind of bills due within", Kind: Days, Recommended: "3",
		Reason: "The Inbox lists the bills to pay 3 days before they fall due or their planned payment date, and the overdue ones",
		Guide:  guide + "bill-reminders", Requirement: "FR-AP-13", Min: "0", Max: "30"},
	{Key: "retention.ai_conversations_days", Group: "Data region and retention", Label: "AI conversations kept for", Kind: Days, Recommended: "365",
		Reason: "Conversations with the AI assistant are kept a year, then purged; proposals follow the audit trail's retention",
		Guide:  guide + "data-region-and-retention", Requirement: "FR-ORG-07", DualControl: true, Min: "30", Max: "3650"},
	{Key: "retention.purge_after_retention", Group: "Data region and retention", Label: "Purge the ledger and the audit trail after retention", Kind: Switch, Recommended: "false",
		Reason: "Off: the ledger and the audit trail are kept for the life of the company; switched on, each year past its retention is purged with its balances carried forward, which cannot be undone",
		Guide:  guide + "data-region-and-retention", Requirement: "AUD-10", DualControl: true},
	{Key: "retention.records_years", Group: "Data region and retention", Label: "Records kept for", Kind: Years, Recommended: "7",
		Reason: "Documents and files are kept 7 years after the financial year they relate to, and the ledger and the audit trail at least as long; it can only lengthen",
		Guide:  guide + "data-region-and-retention", Requirement: "FR-ORG-07", DualControl: true, LengthenOnly: true, Min: "7", Max: "100"},
	{Key: "retention.security_events_days", Group: "Data region and retention", Label: "Security events' IP address and device kept for", Kind: Days, Recommended: "400",
		Reason: "A sign-in's, a sensitive read's and an export's IP address and device are kept 400 days, an annual audit and a quarter, then erased; the event stays",
		Guide:  guide + "data-region-and-retention", Requirement: "AUD-06", DualControl: true, Min: "30", Max: "3650"},
	{Key: "sales.payment_terms_days", Group: "Defaults", Label: "Payment terms for invoices", Kind: Days, Recommended: "30",
		Reason: "An invoice to a customer with no terms of its own falls due 30 days after its date",
		Guide:  guide + "payment-terms", Requirement: "FR-ORG-04", Min: "0", Max: "365"},
	{Key: "sales.simplified_tax_invoices", Group: "Defaults", Label: "Simplified tax invoices up to S$1,000", Kind: Switch, Recommended: "false",
		Reason: "Every invoice is a full tax invoice, with the customer's name and address and the GST breakdown that customers' accounts teams ask for",
		Guide:  guide + "simplified-tax-invoices", Requirement: "FR-AR-05"},
	{Key: "security.idle_timeout_minutes", Group: "Security", Label: "Sign out after inactivity", Kind: Minutes, Recommended: "30",
		Reason: "A session ends after 30 minutes without a request, whatever happens it ends 12 hours after sign-in (SEC-05; the owner's decision of 6 Nov 2026)",
		Guide:  guide + "sign-out-after-inactivity", Requirement: "SEC-05", DualControl: true, Min: "5", Max: "60"},
	{Key: "security.recovery_cooling_off_hours", Group: "Security", Label: "Hold after an account recovery", Kind: Hours, Recommended: "24",
		Reason: "For 24 hours after a person recovers their account, they cannot approve, release payments, change bank details, roles or sign-ins (SEC-35; the owner's decision of 12 Nov 2026)",
		Guide:  guide + "hold-after-an-account-recovery", Requirement: "SEC-35", DualControl: true, Min: "24", Max: "72"},
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
	// accountCode is the chart's code format (FR-COA-01): the account must also exist when a posting reads it.
	accountCode = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.-]{0,19}$`)
	sgd         = money.MustLookup("SGD")
)

// Normalise reads raw as a value of d, and returns it as stored: "true", "30", "50.00" or "2.5". It refuses
// a value outside d's range. A setting that ships unset (its recommended value empty, such as a mileage rate the
// organisation sets itself) takes "" for "not set".
func Normalise(d Definition, raw string) (string, *Problem) {
	bad := func(fix string) (string, *Problem) {
		return "", &Problem{CodeValue, "value", fmt.Sprintf("%q is not a value for %s", raw, d.Label), fix}
	}
	if raw == "" && d.Recommended == "" && !d.Fixed {
		return "", nil
	}
	switch d.Kind {
	case Switch:
		if raw != "true" && raw != "false" {
			return bad("Send true or false")
		}
		return raw, nil
	case Minutes, Hours, Days, Months, Years:
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
	case AccountCode:
		if !accountCode.MatchString(raw) {
			return bad("Send the code of an account in your chart, such as 6460")
		}
		return raw, nil
	case Choice:
		if !slices.Contains(d.Choices, raw) {
			return bad("Send one of " + strings.Join(d.Choices, ", "))
		}
		return raw, nil
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
