package pack

import (
	"fmt"

	"github.com/google/uuid"
)

// Activation asks for an organisation to adopt a pack version: who proposed it, who approved it, and the
// approval engine's record of that approval (p1-approval-engine, SOD-10).
type Activation struct {
	ProposedBy, ApprovedBy uuid.UUID
	Approval               uuid.UUID // the approval engine's record; uuid.Nil until the engine exists
}

// CheckActivation returns every reason v may not be activated as a (ENG-32): every pinned statutory test
// of v must pass, and a second person must approve through the approval engine, never the one who
// proposed it (SOD-10). The database refuses an activation without its approved request, or approved by its
// proposer, as well (migrations 000034, 000041).
func CheckActivation(v Version, a Activation) Problems {
	var out Problems
	results := RunPinned(v)
	if len(results) == 0 {
		out = append(out, Problem{CodePinned, "pinned", fmt.Sprintf("%s has no pinned statutory tests", v.ID),
			"Pin the statutory cases for the version's effective date, worked by hand from the official pages (QA-ENG-08)"})
	}
	for _, r := range Failed(results) {
		out = append(out, Problem{CodePinned, "pinned." + r.Table, fmt.Sprintf("%s (%s) gives %s, not %s", r.Case, r.Why, r.Got, r.Want),
			"Correct the table from the official page, or the pinned case if it was worked wrongly; never activate a version that fails"})
	}
	switch {
	case a.Approval == uuid.Nil:
		out = append(out, Problem{CodeApproval, "approval", "activating a pack version needs a second approver through the approval engine",
			"Ask to activate the version, and have another Owner or Controller approve the request (SOD-10)"})
	case a.ProposedBy == uuid.Nil || a.ApprovedBy == uuid.Nil || a.ProposedBy == a.ApprovedBy:
		out = append(out, Problem{CodeApproval, "approved_by", "the person who proposed the activation cannot approve it",
			"Ask another Owner or Controller to approve it (SOD-10)"})
	}
	return out
}
