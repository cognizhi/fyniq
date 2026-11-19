// Package accessreq is Nemura's handling of a person's request for their own data (PRD-002 NM-SEC-07; PRD-001 CMP-PD-04): a tracked
// request with a due date, HR's decision for each category of record, and the outcome in writing. Withdrawing consent never deletes
// records the law requires.
package accessreq

import (
	"fmt"
	"strings"
	"time"
)

// DueDays is how long the organisation has to answer, or to tell the person in writing when it will take longer (CMP-PD-04).
const DueDays = 30

// Due is the day an answer is due for a request received on day.
func Due(received time.Time) time.Time { return received.AddDate(0, 0, DueDays) }

// Decision is what is done with a category of record.
type Decision string

// The decisions: release the category, release it with other people's personal data removed, or withhold it under an exception.
const (
	Release  Decision = "release"
	Redact   Decision = "redact"
	Withhold Decision = "withhold"
)

// Exceptions are the grounds on which a category may be withheld; each is recorded with the decision. Opinions kept solely for an
// evaluative purpose are an exception in the PDPC's selected-topics guidelines [verify the current edition with the DPO]; the
// others are the Act's own limits and are confirmed with the DPO before a request relies on one.
var Exceptions = []string{
	"opinion kept solely for an evaluative purpose",
	"legal privilege",
	"would reveal another person's personal data that cannot be removed",
	"a legal hold the organisation is required to keep",
}

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

// CodeInvalid is the code of a decision that cannot be recorded.
const CodeInvalid = "NM_ACCESS_DECISION_INVALID"

// Check says whether a decision with this exception can be recorded: a withheld category names one of the exceptions, and the
// other decisions name none.
func Check(d Decision, exception string) error {
	switch d {
	case Release, Redact:
		if exception != "" {
			return &Refusal{CodeInvalid, "Only a withheld category needs an exception. Remove the exception, or withhold the category."}
		}
	case Withhold:
		for _, e := range Exceptions {
			if strings.EqualFold(strings.TrimSpace(exception), e) {
				return nil
			}
		}
		return &Refusal{CodeInvalid, fmt.Sprintf("A withheld category needs one of the recorded exceptions: %s.", strings.Join(Exceptions, "; "))}
	default:
		return &Refusal{CodeInvalid, "Choose release, redact or withhold."}
	}
	return nil
}

// Overdue reports whether a request not yet answered is past its due date, with no written notice given in time.
func Overdue(due, now time.Time, notice *time.Time, answered bool) bool {
	return !answered && now.After(due) && (notice == nil || notice.After(due))
}
