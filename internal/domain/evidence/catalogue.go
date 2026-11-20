package evidence

// The evidence rules table of PRD-001 §8.16 (FR-DOC-06), as data: each activity's required and recommended evidence, in the
// table's words, with the kinds of file that meet each item. The Evidence panel shows an activity's rules from it
// (required items ✖ while missing, recommended ones ⚠), and the API answers it as GET /v1/evidence-rules with each
// configurable activity's level in force.
//
// A rule's level is an organisation setting (FR-ORG-08) for the activities whose evidence nothing else holds (Setting);
// the others are enforced by their own modules as their level says (Built), and an activity whose module has not arrived
// ships as a rule that starts to apply with it.

// The areas of the table, as the specification groups them.
const (
	AreaPurchases  = "purchases"  // bills, claims, founders' costs, payee changes, payment releases
	AreaJournals   = "journals"   // manual journals, reconciliations, closes
	AreaFilings    = "filings"    // statutory filings, CPF payments, grant claims
	AreaEquity     = "equity"     // share issues and transfers, instruments, settlements, investor records
	AreaGovernance = "governance" // reopens, parameter and role changes, access reviews, control evidence
)

// Item is one piece of evidence a rule names.
type Item struct {
	Label string   // in the table's words
	Types []string // evidence types that meet it, as a file names what it is; none means any file does
}

// Rule is one row of the table.
type Rule struct {
	Key         string
	Area        string
	Label       string
	Setting     string // the organisation setting that holds its level; empty when its module fixes it
	Level       string // as shipped: required when it names required evidence, else recommended
	Built       bool   // its module exists, so the rule is enforced now
	Required    []Item
	Recommended []Item
}

// Catalogue is the table, in its order.
var Catalogue = []Rule{
	{Key: "bill", Area: AreaPurchases, Label: "Bill", Setting: Bill.Setting, Level: LevelRequired, Built: true,
		Required:    []Item{{Label: "Receipt or invoice"}},
		Recommended: []Item{{Label: "Business purpose note", Types: []string{"business purpose note"}}}},
	{Key: "expense_claim", Area: AreaPurchases, Label: "Expense claim", Level: LevelRequired, Built: true,
		Required:    []Item{{Label: "Receipt or invoice", Types: []string{"receipt", "tax invoice", "invoice"}}},
		Recommended: []Item{{Label: "Business purpose note", Types: []string{"business purpose note"}}}},
	{Key: "founder_cost", Area: AreaPurchases, Label: "Founder cost paid personally", Level: LevelRequired, Built: true,
		Required:    []Item{{Label: "Receipt or invoice", Types: []string{"receipt", "invoice"}}},
		Recommended: []Item{{Label: "Business purpose note", Types: []string{"business purpose note"}}}},
	{Key: "payee_bank_change", Area: AreaPurchases, Label: "Payee bank-detail change", Level: LevelRequired, Built: true,
		Required:    []Item{{Label: "Bank letter, or a screenshot of the vendor's confirmation through a known channel", Types: []string{"bank_letter", "vendor_confirmation"}}},
		Recommended: []Item{{Label: "Call-back note (who, when, number used)", Types: []string{"call-back record"}}}},
	{Key: "payment_release", Area: AreaPurchases, Label: "Payment batch release", Level: LevelRecommended,
		Recommended: []Item{{Label: "Bank confirmation screenshot or file", Types: []string{"bank confirmation", "screenshot"}}}},

	{Key: "manual_journal", Area: AreaJournals, Label: "Manual journal (above the evidence threshold)", Setting: ManualJournal.Setting, Level: LevelRequired, Built: true,
		Required:    []Item{{Label: "Supporting calculation or document"}},
		Recommended: []Item{{Label: "Explanation", Types: []string{"explanation"}}}},
	{Key: "bank_reconciliation", Area: AreaJournals, Label: "Bank reconciliation sign-off", Level: LevelRequired, Built: true,
		Required: []Item{{Label: "Bank statement for the period (FM-5)"}}},
	{Key: "period_close", Area: AreaJournals, Label: "Period close", Level: LevelRequired, Built: true,
		Required: []Item{{Label: "Signed checklist (generated)"}, {Label: "Reconciliations"},
			{Label: "For each bank, provider-clearing, control, deferral and director account with a balance, its support: a statement, or a subledger or schedule (INV-07)"},
			{Label: "At the year-end close, the director-account confirmation (FR-EQ-12)"}},
		Recommended: []Item{{Label: "Flux explanations (FR-GL-14)"}}},

	{Key: "statutory_filing", Area: AreaFilings, Label: "GST F5, Form C-S/C, ECI, AIS filing", Setting: StatutoryFiling.Setting, Level: LevelRequired, Built: true,
		Required:    []Item{{Label: "Submission acknowledgement", Types: []string{"iras acknowledgement", "acra acknowledgement", "cpf board acknowledgement", "agm minutes"}}},
		Recommended: []Item{{Label: "Working papers", Types: []string{"working papers"}}}},
	{Key: "cpf_payment", Area: AreaFilings, Label: "CPF payment (recorded on the statutory calendar)", Setting: StatutoryFiling.Setting, Level: LevelRequired, Built: true,
		Required:    []Item{{Label: "Payment acknowledgement", Types: []string{"cpf board acknowledgement"}}},
		Recommended: []Item{{Label: "CPF submission file", Types: []string{"cpf submission file"}}}},
	{Key: "grant_claim", Area: AreaFilings, Label: "Grant claim (when the organisation holds a grant)", Level: LevelRequired,
		Required:    []Item{{Label: "Letter of offer or award"}, {Label: "Paid invoices and proof of payment for each claimed cost"}},
		Recommended: []Item{{Label: "Evidence of each deliverable"}, {Label: "The claim working paper"}}},

	{Key: "share_issue", Area: AreaEquity, Label: "Share issue or conversion", Level: LevelRequired, Built: true,
		Required:    []Item{{Label: "Shareholders' and board resolutions"}, {Label: "Written agreement for non-cash consideration"}, {Label: "ACRA acknowledgement"}},
		Recommended: []Item{{Label: "Valuation or price basis"}}},
	{Key: "share_transfer", Area: AreaEquity, Label: "Share transfer", Level: LevelRequired, Built: true,
		Required: []Item{{Label: "Signed transfer form"}, {Label: "ACRA acknowledgement"}}, Recommended: []Item{{Label: "Stamp duty certificate"}}},
	{Key: "instrument", Area: AreaEquity, Label: "SAFE, note, option grant", Level: LevelRequired, Built: true,
		Required: []Item{{Label: "Signed instrument or grant letter"}}, Recommended: []Item{{Label: "Board resolution"}}},
	{Key: "founder_settlement", Area: AreaEquity, Label: "Founder settlement or waiver", Level: LevelRequired, Built: true,
		Required: []Item{{Label: "Founder's balance confirmation", Types: []string{"balance confirmation"}}, {Label: "Deed or resolution", Types: []string{"deed of waiver", "directors' resolution", "board resolution", "board minute", "shareholders' approval", "loan note"}}}},
	{Key: "investor_record", Area: AreaEquity, Label: "Investor record change", Level: LevelRecommended,
		Recommended: []Item{{Label: "Source document (such as an updated agreement)"}}},

	{Key: "governance_change", Area: AreaGovernance, Label: "Period reopen, parameter change, role change", Level: LevelRequired,
		Required: []Item{{Label: "Reason (text)"}}, Recommended: []Item{{Label: "Supporting document"}}},
	{Key: "control_evidence", Area: AreaGovernance, Label: "Access review, control evidence (manual)", Level: LevelRequired,
		Required: []Item{{Label: "Review record or screenshot"}}},
	{Key: "board_pack", Area: AreaGovernance, Label: "Board pack kept as evidence (rev 0.607)", Level: LevelRequired, Built: true,
		Required: []Item{{Label: "The published pack's PDF (generated)", Types: []string{"board pack PDF"}}}},
}

// RuleOf is the rule with key, if the table has it.
func RuleOf(key string) (Rule, bool) {
	for _, r := range Catalogue {
		if r.Key == key {
			return r, true
		}
	}
	return Rule{}, false
}
