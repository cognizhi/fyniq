package approval

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Delegation of approval authority (FR-APR-06; p2-mobile-approvals-automation design D1). A delegation lends no right. It makes its delegate eligible for a
// step the delegator could sign, for the days it names, and nothing else: the delegate must hold the approval right on the object's row themselves, signs
// as themselves with their own step-up, and the signature records on whose behalf. A delegator and the person who signs for them are one signature of a
// step's quorum, never two.

// Codes of the delegation rules.
const (
	CodeDelegation       = "APR-17" // a delegation that cannot be made: oneself, too long, dates the wrong way round, a delegate who may not approve
	CodeDelegationNotFor = "APR-18" // a delegation that is not the caller's to end, or ended already
)

// MaxDelegationDays is the most days one delegation spans, from its first day to its last, so a longer absence is a conscious renewal.
const MaxDelegationDays = 90

// Delegation is what the step rules need of a delegation.
type Delegation struct {
	ID        uuid.UUID
	Delegator uuid.UUID
	Delegate  uuid.UUID
	StartsOn  time.Time // the first day, a date
	EndsOn    time.Time // the last day, a date
	Ended     bool
}

// day is t's calendar date as a number comparable across zones.
func day(t time.Time) int { return t.Year()*10000 + int(t.Month())*100 + t.Day() }

// Live reports whether the delegation is in force on the business day today: not ended, between its first and last day, inclusive.
func (d Delegation) Live(today time.Time) bool {
	return !d.Ended && day(today) >= day(d.StartsOn) && day(today) <= day(d.EndsOn)
}

// CheckDelegation returns what is wrong with a new delegation from delegator to delegate for the days startsOn to endsOn, as of the business day today. The
// delegate must be an active member who may approve something (delegateMayApprove: the API reads their roles); each problem names its field.
func CheckDelegation(delegator, delegate uuid.UUID, startsOn, endsOn, today time.Time, delegateActive, delegateMayApprove bool) []FieldProblem {
	var out []FieldProblem
	switch {
	case delegate == delegator:
		out = append(out, FieldProblem{"body.delegate", CodeDelegation, "is you: you cannot delegate to yourself", "Choose another member"})
	case !delegateActive:
		out = append(out, FieldProblem{"body.delegate", CodeDelegation, "is not an active member of this organisation", "Choose a member who is active"})
	case !delegateMayApprove:
		out = append(out, FieldProblem{"body.delegate", CodeDelegation, "holds no right to approve anything, so a delegation would lend them nothing (FR-APR-01)",
			"Choose a member who holds an approval right, or ask an Owner to give them the role first"})
	}
	if day(endsOn) < day(startsOn) {
		out = append(out, FieldProblem{"body.ends_on", CodeDelegation, "is before the first day", "Choose a last day on or after the first"})
	} else if int(endsOn.Sub(startsOn).Hours()/24+0.5) > MaxDelegationDays {
		out = append(out, FieldProblem{"body.ends_on", CodeDelegation, fmt.Sprintf("is more than %d days after the first day", MaxDelegationDays),
			"Delegate for up to 90 days and renew it if the absence is longer"})
	}
	if day(endsOn) < day(today) {
		out = append(out, FieldProblem{"body.ends_on", CodeDelegation, "is in the past", "Choose today or a later day"})
	}
	return out
}

// StandIn finds the delegator whom delegate may sign step s for on the business day today, or false. delegations are the organisation's; signed says whether
// a person's place in the request is taken already (they signed, or someone signed for them); roles returns a person's live roles and whether they are an active
// member; delegatorApproves says whether those roles hold the right to sign the request. The first delegation, in the order given, that fits wins.
func StandIn(s Step, delegate uuid.UUID, delegations []Delegation, today time.Time, signed func(uuid.UUID) bool, roles func(uuid.UUID) ([]string, bool),
	delegatorApproves func([]string) bool) (uuid.UUID, bool) {
	for _, d := range delegations {
		if d.Delegate != delegate || !d.Live(today) || signed(d.Delegator) {
			continue
		}
		rs, active := roles(d.Delegator)
		if !active || !delegatorApproves(rs) || !Eligible(s, d.Delegator, rs) {
			continue
		}
		return d.Delegator, true
	}
	return uuid.Nil, false
}
