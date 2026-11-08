package ledger

import (
	"fmt"
	"slices"

	"github.com/google/uuid"
)

// The roles of a director's account (FR-EQ-01, B.7): every line on one names the director or shareholder it belongs to,
// and no other line names a person.
const (
	RoleDirectorLoan         = "director-loan"         // 2500 Loan from director / shareholder
	RoleDeferredRemuneration = "deferred-remuneration" // 2120 Deferred remuneration payable
)

// CodeParty is a line on a director's account without its person, with a person who is not a director or shareholder,
// or a person on a line to any other account (B.7, FR-EQ-01; the database's FY250).
const CodeParty = "LEDGER-41"

// DirectorAccount reports whether the account is a director's account, whose lines name their person (B.7).
func (a Account) DirectorAccount() bool {
	return a.Role == RoleDirectorLoan || a.Role == RoleDeferredRemuneration
}

// The origins of a movement on a director's account (FR-EQ-01, design D3): the tax and accounting result depends on
// them. A source's own movements take theirs from the source; a manual journal or opening balance line enters one of
// EnteredOrigins.
const (
	OriginLent           = "lent"            // cash the person lent
	OriginPaidPersonally = "paid_personally" // a company cost the person paid
	OriginPay            = "pay"             // pay deferred
	OriginFee            = "fee"             // a director's fee
	OriginPaidOut        = "paid_out"        // money paid to the person: a repayment or an advance
	OriginConverted      = "converted"       // converted to shares
	OriginWaived         = "waived"          // waived
	OriginSAFE           = "safe"            // turned into a SAFE or convertible note the person holds (FR-EQ-05)
	OriginOther          = "other"           // anything else, explained in the line's description
)

// EnteredOrigins are the origins a person enters on a manual journal or opening balance line.
var EnteredOrigins = []string{OriginLent, OriginPaidPersonally, OriginPay, OriginFee, OriginPaidOut, OriginOther}

// originProblem is what is wrong with the origin entered on a line to acc, if anything.
func originProblem(acc Account, origin, field string) *Problem {
	switch {
	case acc.DirectorAccount() && !slices.Contains(EnteredOrigins, origin):
		return &Problem{CodeParty, field, fmt.Sprintf("the line on director's account %s says no origin, or one Fyniq does not know: %q", acc.Code, origin),
			"Choose where the amount came from: lent, paid_personally, pay, fee, paid_out or other"}
	case !acc.DirectorAccount() && origin != "":
		return &Problem{CodeParty, field, fmt.Sprintf("account %s is not a director's account, so the line has no origin", acc.Code),
			"Leave the origin out"}
	}
	return nil
}

// partyProblem is what is wrong with the person of a line to acc, if anything; field names the line's person: a
// director's account names a director or shareholder, share application monies a shareholder or investor, and no other
// line a person. A rule whose settings list no people (Settings.People or Subscribers nil) checks only that the person
// is there exactly when it must be; the database refuses the wrong contact either way (FY250).
func partyProblem(s Settings, acc Account, party uuid.UUID, field string) *Problem {
	switch {
	case acc.SubscriberAccount() && party == uuid.Nil: // B.7: share application monies name their subscriber
		return &Problem{CodeParty, field, fmt.Sprintf("account %s holds share application monies, so the line names its subscriber", acc.Code),
			"Choose the shareholder or investor who paid for shares not yet allotted"}
	case acc.SubscriberAccount() && s.Subscribers != nil && !s.Subscribers[party]:
		return &Problem{CodeParty, field, "the subscriber is not a shareholder or investor of this organisation",
			"Choose a contact marked shareholder or investor, or mark the contact as one"}
	case acc.SubscriberAccount():
		return nil
	case acc.DirectorAccount() && party == uuid.Nil:
		return &Problem{CodeParty, field, fmt.Sprintf("account %s is a director's account, so the line names whose it is", acc.Code),
			"Choose the director or shareholder the amount belongs to"}
	case acc.DirectorAccount() && s.People != nil && !s.People[party]:
		return &Problem{CodeParty, field, "the person is not a director or shareholder of this organisation",
			"Choose a contact marked director or shareholder, or mark the contact as one"}
	case !acc.DirectorAccount() && party != uuid.Nil:
		return &Problem{CodeParty, field, fmt.Sprintf("account %s is not a director's account, so the line names no person", acc.Code),
			"Leave the person out, or post to the director's account"}
	}
	return nil
}
