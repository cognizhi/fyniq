package hiring

import (
	"strings"
	"time"
)

// Offer to hire (PRD-002 NM-REC-06): when an offer may be made for a role that needs a pass, and what a hire must say.

// CheckOffer refuses an offer made before the Fair Consideration Framework allows it: for a role that needs a pass, neither an advert's date nor an exemption recorded, an advert that came down more than
// 3 months ago, or the advert's 14 consecutive days not yet run (NM-REC-02). A role that needs no pass has no rule. asOf is the day the offer is made.
func CheckOffer(passNeeded bool, advertPosted, advertClosed *time.Time, exemption string, asOf time.Time) error {
	from, ok := OfferFrom(passNeeded, advertPosted, exemption)
	if !ok {
		return &Refusal{CodeConflict, "No offer can be made yet: the role needs a pass, so record the day the advert went up on MyCareersFuture, or an exemption with its evidence."}
	}
	if passNeeded && exemption == "" && !AdvertUsable(advertClosed, asOf) {
		return &Refusal{CodeConflict, "That advert came down more than 3 months ago, so it cannot be used: post a new advert and record its day before an offer is made."}
	}
	if !from.IsZero() && asOf.Before(from) {
		return &Refusal{CodeConflict, "No offer can be made until " + from.Format(time.DateOnly) + ": the advert's 14 consecutive days have not run, and no exemption is recorded."}
	}
	return nil
}

// CheckHire refuses a hire that is not whole: a start date not before the day the hire is made and the job's employment type and country are checked by the people rules.
func CheckHire(start, asOf time.Time, title string) error {
	if start.IsZero() || start.Before(asOf) {
		return invalid("The start date is the first day of employment, today or later: write it as 2027-02-01.")
	}
	if n := len([]rune(strings.TrimSpace(title))); n < 1 || n > 200 {
		return invalid("The job title is 1 to 200 characters.")
	}
	return nil
}

// CheckPassApplication refuses a pass application date in the future or for a hire that needs no pass.
func CheckPassApplication(passNeeded bool, on, asOf time.Time) error {
	if !passNeeded {
		return invalid("This role needs no Employment Pass or S Pass, so there is no application to track.")
	}
	if on.IsZero() || on.After(asOf) {
		return invalid("The day the pass application was made is a day up to today: write it as 2027-01-15.")
	}
	return nil
}
