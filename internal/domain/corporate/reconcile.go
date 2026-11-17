package corporate

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The corporate profile against ACRA's business profile (FR-CS-01, C.2 #34; p1-company-secretary design D3): the
// officers in office and the registered office on the extract's day, compared with the officers and office the extract
// lists. The registers of officers are ACRA's, so a difference is fixed in Fyniq's records, or explained.

// The kinds of difference.
const (
	DiffMissingHere   = "missing_in_fyniq" // ACRA lists an officer the profile does not have in office on the day
	DiffMissingAtACRA = "missing_at_acra"  // the profile has an officer in office that ACRA does not list
	DiffAppointed     = "appointed_differs"
	DiffOffice        = "office_differs"
)

// ProfileDifference is one way the profile and ACRA differ on the extract's day.
type ProfileDifference struct {
	Kind    string
	Role    string
	Name    string
	Profile string // what the profile holds: the day appointed, or the office
	ACRA    string // what ACRA's extract lists
}

// Reconciliation is the profile against the extract in force: assessed, with each difference, or not assessed with why.
type Reconciliation struct {
	Extract     *equity.Recorded[equity.AcraExtract]
	Assessed    bool
	Reason      string // why it is not assessed, naming the input
	Differences []ProfileDifference
}

// sameName compares names as a person reads them: case, spacing and the punctuation of "Pte. Ltd." aside.
func sameName(a, b string) bool { return nameKey(a) == nameKey(b) }

func nameKey(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		case unicode.IsSpace(r) || r == ',' || r == '.' || r == '-':
			space = true
		}
	}
	return b.String()
}

// LastChange is the day of the profile's last recorded change: a detail taking effect, an officer appointed or
// ceasing; zero when nothing is recorded.
func LastChange(details []equity.Recorded[Detail], officers map[uuid.UUID][]equity.Recorded[Officer]) time.Time {
	var last time.Time
	later := func(d time.Time) {
		if d.After(last) {
			last = d
		}
	}
	for _, d := range details {
		if d.Doc.Status == storage.DocumentPosted {
			later(d.Value.From)
		}
	}
	for _, versions := range officers {
		if cur := equity.InForce(versions); cur != nil {
			later(cur.Value.Appointed)
			if cur.Value.Ceased != nil {
				later(*cur.Value.Ceased)
			}
		}
	}
	return last
}

// ReconcileProfile compares the profile on the extract's day with the extract in force (design D3). It is not assessed,
// naming the input, while no extract is recorded, the extract has no officers typed in, or it is older than the
// profile's last recorded change: then it no longer shows the company.
func ReconcileProfile(extract *equity.Recorded[equity.AcraExtract], details []equity.Recorded[Detail],
	officers map[uuid.UUID][]equity.Recorded[Officer]) Reconciliation {
	out := Reconciliation{Extract: extract}
	switch {
	case extract == nil:
		out.Reason = "No ACRA business profile is recorded: type one in with its officers and registered office, with its PDF"
		return out
	case len(extract.Value.Officers) == 0:
		out.Reason = "The business profile of " + extract.Value.ProfileOn.Format("2 Jan 2006") + " has no officers typed in: type them in on the next extract"
		return out
	}
	day := extract.Value.ProfileOn
	if last := LastChange(details, officers); last.After(day) {
		out.Reason = "The business profile of " + day.Format("2 Jan 2006") + " is older than the last recorded change, of " + last.Format("2 Jan 2006") +
			": record a newer one"
		return out
	}
	out.Assessed = true
	var inOffice []Officer
	for _, x := range OfficersOn(officers, day) {
		inOffice = append(inOffice, x.Value)
	}
	matched := make([]bool, len(inOffice))
	for _, a := range extract.Value.Officers {
		k := slices.IndexFunc(inOffice, func(o Officer) bool { return o.Role == a.Role && sameName(o.Name, a.Name) })
		switch {
		case k < 0:
			out.Differences = append(out.Differences, ProfileDifference{Kind: DiffMissingHere, Role: a.Role, Name: a.Name,
				ACRA: a.Appointed.Format(time.DateOnly)})
		default:
			matched[k] = true
			if !inOffice[k].Appointed.Equal(a.Appointed) {
				out.Differences = append(out.Differences, ProfileDifference{Kind: DiffAppointed, Role: a.Role, Name: inOffice[k].Name,
					Profile: inOffice[k].Appointed.Format(time.DateOnly), ACRA: a.Appointed.Format(time.DateOnly)})
			}
		}
	}
	for k, o := range inOffice {
		if !matched[k] {
			out.Differences = append(out.Differences, ProfileDifference{Kind: DiffMissingAtACRA, Role: o.Role, Name: o.Name,
				Profile: o.Appointed.Format(time.DateOnly)})
		}
	}
	if extract.Value.Office != "" {
		if d := DetailOn(details, day); d != nil && nameKey(d.Value.Office) != nameKey(extract.Value.Office) {
			out.Differences = append(out.Differences, ProfileDifference{Kind: DiffOffice, Profile: d.Value.Office, ACRA: extract.Value.Office})
		}
	}
	return out
}

// ReconcileProfileIn is the profile in u against the ACRA extract in force (design D3).
func ReconcileProfileIn(ctx context.Context, u storage.UnitOfWork) (Reconciliation, error) {
	extracts, err := equity.AcraExtracts(ctx, u)
	if err != nil {
		return Reconciliation{}, err
	}
	details, err := Details(ctx, u)
	if err != nil {
		return Reconciliation{}, err
	}
	officers, err := Officers(ctx, u)
	if err != nil {
		return Reconciliation{}, err
	}
	return ReconcileProfile(equity.LatestAcra(extracts), details, officers), nil
}
