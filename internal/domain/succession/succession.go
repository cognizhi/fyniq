// Package succession holds the rules of an Owner succession (PRD-001 FR-ADM-09; rev 0.181, the owner's decisions of 6
// Nov 2026): when another Owner is permanently unavailable, an Owner starts one with a reason and the evidence it rests
// on; the outgoing Owner is told at once and may object for 7 days; then a Super admin, any one, completes it: the
// outgoing Owner's membership ends, and the member it names, if any, becomes an Owner.
package succession

import "time"

// Wait is the notice a succession gives before a Super admin may complete it.
const Wait = 7 * 24 * time.Hour

// The evidence a succession rests on (FR-DOC-05): a board resolution, or the external accountant's recorded
// confirmation.
const (
	BoardResolution        = "board_resolution"
	AccountantConfirmation = "accountant_confirmation"
)

// EvidenceKinds are the kinds of evidence, in the order a form offers them.
var EvidenceKinds = []string{BoardResolution, AccountantConfirmation}

// The codes of a succession's refusals.
const (
	CodeNotOwner  = "SUCC-01" // the outgoing member holds no Owner role, or it is the one who asks
	CodeNotDue    = "SUCC-02" // its 7 days of notice have not passed
	CodeNoFile    = "SUCC-03" // no file of its evidence is attached
	CodeNotYours  = "SUCC-04" // only the outgoing Owner objects, only the one who started it withdraws
	CodeEnded     = "SUCC-05" // it ended already: objected, withdrawn or completed
	CodeSuccessor = "SUCC-06" // the successor named is not an active member other than the outgoing one
)

// Codes are every code above.
var Codes = []string{CodeNotOwner, CodeNotDue, CodeNoFile, CodeNotYours, CodeEnded, CodeSuccessor}
