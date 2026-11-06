package evidence

// The evidence rules per activity (PRD-001 FR-DOC-06; rev 0.182, the owner's decisions of 6 Nov 2026): each activity's
// evidence is required, recommended or optional, an organisation setting with the PRD's default (FR-ORG-08), changed
// under dual control. A missing required file blocks completing the activity (✖, DOC-10, in plain words: what is
// missing and how to add it); a missing recommended one lets it complete and shows on Checks (⚠, C.2 #30). The kinds
// of evidence each activity accepts are the PRD's table.
//
// Built here for the activities whose rule nothing else holds yet: bills, and manual journals above a threshold.
// Expense claims keep their categories' receipt thresholds (FR-AP-15); payee bank-detail changes, founders' costs and
// settlements require their files in their own modules; a reconciliation's statement is the imported one (FM-5); the
// close has its checklist and checks.

// The levels of a rule.
const (
	LevelRequired    = "required"
	LevelRecommended = "recommended"
	LevelOptional    = "optional"
)

// Levels are the levels, strictest first, as a setting offers them.
var Levels = []string{LevelRequired, LevelRecommended, LevelOptional}

// CodeMissing is a required evidence file missing when the activity completes (✖).
const CodeMissing = "DOC-10"

// Activity is an activity an evidence rule governs.
type Activity struct {
	Key      string // as Checks and the API name it
	Label    string // in a sentence: "a bill"
	Setting  string // the organisation setting that holds its level
	Expected string // what evidence it rests on, in plain words
	Default  string // its level as shipped (FR-DOC-06's table)
}

// Bill is a supplier's bill: required, the supplier's invoice or receipt.
var Bill = Activity{Key: "bill", Label: "a bill", Setting: "evidence.bill", Expected: "the supplier's invoice or receipt", Default: LevelRequired}

// ManualJournal is a manual journal whose debits in the base currency are above JournalThreshold: required, a
// supporting calculation or document.
var ManualJournal = Activity{Key: "manual_journal", Label: "a manual journal above the evidence threshold", Setting: "evidence.manual_journal",
	Expected: "a supporting calculation or document", Default: LevelRequired}

// JournalThreshold is the setting above which a manual journal's debits need its evidence: S$1,000.00 by default.
const JournalThreshold = "evidence.journal_threshold"

// Activities are the activities built here, in the order Checks lists them.
var Activities = []Activity{Bill, ManualJournal}

// Needs reports what an activity at level makes of a record with files attached: it blocks when its evidence is
// required and none is attached, and warns when it is recommended and none is.
func Needs(level string, files int) (blocks, warns bool) {
	if files > 0 {
		return false, false
	}
	return level == LevelRequired, level == LevelRecommended
}
