// Package breakglass is the break-glass of a single available founder (PRD-001 FM-8; rev 0.175, the owner's decisions of
// 6 Nov 2026): what it allows, how long it lasts, when its review is due and who may give it. In Founder mode an Owner
// starts a break-glass with a reason, for 24 hours; while it is active, they release a payment batch still waiting for
// approval without a second person, within the cap it started with. Its review is due 7 days after it ends: another
// Owner gives it at any time after it ends, and from the due date the Accountant may give it instead. A new break-glass
// waits until the last one is reviewed. The database keeps the same rules (FY261 to FY263).
package breakglass

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of break-glass (ENG-23).
const (
	CodeFounderOnly = "BREAKGLASS-01" // break-glass is for Founder mode only
	CodeWaiting     = "BREAKGLASS-02" // an earlier break-glass waits for its review
	CodeNotActive   = "BREAKGLASS-03" // the caller has no active break-glass
	CodeNotWaiting  = "BREAKGLASS-04" // the batch is not waiting for approval
	CodeDirector    = "BREAKGLASS-05" // the batch repays a director
	CodeOwn         = "BREAKGLASS-06" // the batch pays the Owner using break-glass (FM-9)
	CodeNewAccount  = "BREAKGLASS-07" // a payee's account came into force in the last 30 days
	CodeCap         = "BREAKGLASS-08" // the batch takes the break-glass above its cap
	CodeEnded       = "BREAKGLASS-09" // the break-glass has ended already
	CodeTooEarly    = "BREAKGLASS-10" // the break-glass is reviewed once it has ended
	CodeReviewer    = "BREAKGLASS-11" // the caller may not review it (yet)
)

// The limits of FM-8.
const (
	// Lasts is how long a break-glass lasts, unless an Owner ends it sooner.
	Lasts = 24 * time.Hour
	// ReviewDue is how long after a break-glass ends its review is due (the owner's decision of 6 Nov 2026).
	ReviewDue = 7 * 24 * time.Hour
	// Settled is how long a payee's bank account must have been in force for break-glass to pay it.
	Settled = 30 * 24 * time.Hour
)

// Active reports whether g is active at now: started, and neither expired nor ended.
func Active(g storage.BreakGlass, now time.Time) bool {
	return !now.Before(g.StartedAt) && now.Before(g.Ends())
}

// Due is when g's review is due: 7 days after it ends.
func Due(g storage.BreakGlass) time.Time { return g.Ends().Add(ReviewDue) }

// Used is what g's releases total, in its currency.
func Used(g storage.BreakGlass) (money.Money, error) {
	cur, err := money.Lookup(g.Currency)
	if err != nil {
		return money.Money{}, err
	}
	sum := money.Zero(cur)
	for _, x := range g.Uses {
		v, err := money.ParseStored(string(x.Total), cur)
		if err != nil {
			return money.Money{}, err
		}
		if sum, err = sum.Add(v); err != nil {
			return money.Money{}, err
		}
	}
	return sum, nil
}

// Left is what g may still release: its cap less what it released.
func Left(g storage.BreakGlass) (money.Money, error) {
	cur, err := money.Lookup(g.Currency)
	if err != nil {
		return money.Money{}, err
	}
	capped, err := money.ParseStored(string(g.Cap), cur)
	if err != nil {
		return money.Money{}, err
	}
	used, err := Used(g)
	if err != nil {
		return money.Money{}, err
	}
	return capped.Sub(used)
}

// Problem is a reason a batch is not released under break-glass, naming the payee when it is one payee's.
type Problem struct {
	Code    string
	Payee   string
	Message string
	Fix     string
}

// Payment is a batch's payment to one payee as break-glass reads it.
type Payment struct {
	Payee    string     // the payee's name, for the message
	Director bool       // it repays a director or shareholder
	Member   uuid.UUID  // the member it pays: a claimant, or the member a vendor signs in as; uuid.Nil for none
	InForce  *time.Time // when the account it pays came into force; nil when that is not known
}

// Refusals are why owner may not release a batch paying pays, total in all, under g at now: g is not theirs or not
// active, a payment repays a director, pays them, or pays an account in force for less than 30 days, or the total takes g
// above its cap. None: it may.
func Refusals(g storage.BreakGlass, owner uuid.UUID, total money.Money, pays []Payment, now time.Time) ([]Problem, error) {
	if g.StartedBy != owner || !Active(g, now) {
		return []Problem{{Code: CodeNotActive, Message: "You have no active break-glass.",
			Fix: "Start break-glass on Team mode, or ask another person to approve the batch"}}, nil
	}
	var out []Problem
	for _, p := range pays {
		switch {
		case p.Director:
			out = append(out, Problem{Code: CodeDirector, Payee: p.Payee, Message: "It repays a director or shareholder, which break-glass never does.",
				Fix: "Take the repayment out of the batch; another founder approves it later"})
		case p.Member == owner:
			out = append(out, Problem{Code: CodeOwn, Payee: p.Payee, Message: "It pays you, and break-glass never pays the founder using it (FM-9).",
				Fix: "Take your own payment out of the batch; another founder approves it later"})
		case p.InForce == nil || now.Sub(*p.InForce) < Settled:
			out = append(out, Problem{Code: CodeNewAccount, Payee: p.Payee, Message: "Its bank account came into force in the last 30 days.",
				Fix: "Take it out of the batch: break-glass pays only accounts unchanged for 30 days"})
		}
	}
	left, err := Left(g)
	if err != nil {
		return nil, err
	}
	if c, err := total.Cmp(left); err != nil {
		return nil, err
	} else if c > 0 {
		out = append(out, Problem{Code: CodeCap, Message: fmt.Sprintf("It pays %s %s, more than the %s %s this break-glass may still release.",
			total.Currency().Code, total, left.Currency().Code, left), Fix: "Release a smaller batch, or ask another person to approve it"})
	}
	return out, nil
}

// Reviewer says whether a person holding roles, who is not g's Owner, may review g at now, and if not, why: once it has
// ended, another Owner may; from its due date the Accountant may too.
func Reviewer(g storage.BreakGlass, person uuid.UUID, roles []authz.Role, now time.Time) (bool, *Problem) {
	switch {
	case person == g.StartedBy:
		return false, &Problem{Code: CodeReviewer, Message: "You used this break-glass, so another person reviews it.",
			Fix: "Ask another Owner, or the Accountant once its review is due"}
	case now.Before(g.Ends()):
		return false, &Problem{Code: CodeTooEarly, Message: "The break-glass is still active: it is reviewed once it has ended.",
			Fix: "End it first, or wait until it expires"}
	case slices.Contains(roles, authz.Owner):
		return true, nil
	case slices.Contains(roles, authz.Accountant) && !now.Before(Due(g)):
		return true, nil
	case slices.Contains(roles, authz.Accountant):
		return false, &Problem{Code: CodeReviewer, Message: "Another Owner reviews this break-glass until its review is due; the Accountant may from then.",
			Fix: "Wait until its review is due, or ask another Owner"}
	}
	return false, &Problem{Code: CodeReviewer, Message: "Only another Owner, or the Accountant once its review is due, reviews a break-glass.",
		Fix: "Ask another Owner"}
}
