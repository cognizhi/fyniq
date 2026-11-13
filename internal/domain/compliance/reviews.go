package compliance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	registry "github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/domain/accessreview"
	"github.com/cognizhi/fyniq/internal/domain/accountantreview"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/breakglass"
	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// AcknowledgeWithin is how long a member has to acknowledge a published policy version: 30 days after the later of its
// publication and their joining (CMP-07, design D8).
const AcknowledgeWithin = 30 * 24 * time.Hour

// AcknowledgeBy is when a member who joined at joined is to acknowledge a version published at published.
func AcknowledgeBy(published, joined time.Time) time.Time {
	if joined.After(published) {
		published = joined
	}
	return published.Add(AcknowledgeWithin)
}

// The statuses of a review (design D6).
const (
	ReviewSigned        = "signed"         // nothing waits, and it was signed off before
	ReviewDue           = "due"            // something waits, within its time
	ReviewOverdue       = "overdue"        // something waits past its time
	ReviewNothingDue    = "nothing_due"    // nothing waits, and nothing was ever signed off
	ReviewNotApplicable = "not_applicable" // the team mode does not call for it
)

// ReviewKinds are the reviews in the order the Compliance Center lists them: the keys of registry.Attestations.
var ReviewKinds = []string{"review-log", "access-review", "accountant-review", "close-signoff", "break-glass-review",
	"policy-acknowledgement", "risk-review", "data-inventory", "dpia", "pd-requests", "breach", "breach-tabletop", "transfers", "restore-drill"}

// Review is one recurring review that people sign off, as it stands for an organisation (design D6): what waits for its
// sign-off, what of that is overdue, and the last sign-off. Each is read from its source's own records under its
// source's own rule, the rule the Checks catalogue applies too; a sign-off stays on its source's own page.
type Review struct {
	Kind     string     // a key of registry.Attestations
	Owner    string     // the role that signs it off; "member" when each member signs for themselves
	Cadence  string     // weekly, quarterly, monthly, each use, each version or by review date
	Status   string     // one of the Review statuses
	Period   string     // what waits, or was last signed off, as people say it
	DueOn    *time.Time // when the earliest waiting item falls due; for the risks, the next review date
	Waiting  int        // items waiting for their sign-off
	Overdue  int        // of them, those past their time
	LastBy   string     // the last sign-off's actor, as records name actors
	LastAt   *time.Time // and when
	Detail   string     // one sentence
	Where    string     // the web app's page where it is signed off
	Evidence string     // what it keeps
}

// signed records a sign-off, keeping the latest.
func (r *Review) signed(by string, at time.Time) {
	if r.LastAt == nil || at.After(*r.LastAt) {
		t := at.UTC()
		r.LastBy, r.LastAt = by, &t
	}
}

// wait records an item waiting until due, which is overdue at now when late.
func (r *Review) wait(due time.Time, late bool) {
	r.Waiting++
	if late {
		r.Overdue++
	}
	if d := Day(due); r.DueOn == nil || d.Before(*r.DueOn) {
		r.DueOn = &d
	}
}

// settle sets the status from what waits, and says so when nothing does.
func (r *Review) settle(none string) {
	switch {
	case r.Status == ReviewNotApplicable:
	case r.Overdue > 0:
		r.Status = ReviewOverdue
	case r.Waiting > 0:
		r.Status = ReviewDue
	case r.LastAt != nil:
		r.Status = ReviewSigned
	default:
		r.Status = ReviewNothingDue
	}
	if r.Detail == "" && r.Waiting == 0 {
		r.Detail = none
	}
}

// Reviews are the organisation's recurring reviews at now, in ReviewKinds' order (design D6), read in u.
func Reviews(ctx context.Context, u storage.UnitOfWork, now time.Time, installation bool) ([]Review, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	today := checks.TodayOf(now, o)
	mode := team.Default
	if all, err := u.TeamModes().List(ctx); err != nil {
		return nil, err
	} else if len(all) > 0 {
		mode = all[len(all)-1].Mode
	}
	entries, err := u.ReviewLog().List(ctx)
	if err != nil {
		return nil, err
	}
	members, err := u.Members().List(ctx, now)
	if err != nil {
		return nil, err
	}
	attested, err := u.AccessReviews().List(ctx)
	if err != nil {
		return nil, err
	}
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return nil, err
	}
	accountant, err := u.AccountantReviews().List(ctx)
	if err != nil {
		return nil, err
	}
	glass, err := u.BreakGlasses().List(ctx)
	if err != nil {
		return nil, err
	}
	lib, err := u.Compliance().Policies(ctx)
	if err != nil {
		return nil, err
	}
	risks, err := u.Compliance().Risks(ctx)
	if err != nil {
		return nil, err
	}
	inventory, err := u.Compliance().InventoryReviews(ctx)
	if err != nil {
		return nil, err
	}
	ownEntries, err := u.Compliance().InventoryEntries(ctx)
	if err != nil {
		return nil, err
	}
	dpias, approvals, err := u.Compliance().DPIAs(ctx)
	if err != nil {
		return nil, err
	}
	requests, err := u.Compliance().PDRequests(ctx)
	if err != nil {
		return nil, err
	}
	breaches, err := u.Compliance().Breaches(ctx)
	if err != nil {
		return nil, err
	}
	exercises, err := u.Compliance().BreachExercises(ctx)
	if err != nil {
		return nil, err
	}
	transfers, err := u.Compliance().Transfers(ctx)
	if err != nil {
		return nil, err
	}
	drills, err := u.Compliance().RestoreDrills(ctx)
	if err != nil {
		return nil, err
	}
	catalogue, err := registry.LoadInventory()
	if err != nil {
		return nil, err
	}
	features := map[string]bool{}
	for key, setting := range registry.DPIAFeatures {
		def, _ := settings.Lookup(setting)
		changes, err := u.Settings().Changes(ctx, setting)
		if err != nil {
			return nil, err
		}
		features[key] = settings.On(def, changes, today).Value == "true"
	}
	closeOf := func(p storage.Period) (*storage.PeriodClose, error) {
		c, err := u.Closes().Latest(ctx, p.ID)
		if errors.Is(err, ports.ErrNotFound) {
			return nil, nil
		}
		return &c, err
	}
	closing, err := closeSignOff(periods, closeOf, today)
	if err != nil {
		return nil, err
	}
	out := []Review{
		reviewLogReview(entries, mode, now),
		accessReviewOf(members, attested, today),
		accountantReviewOf(mode, o, periods, accountant, today),
		closing,
		breakGlassReview(glass, entries, now),
		policyAcknowledgements(lib, members, now),
		riskReviews(risks, today),
		inventoryReview(members, inventory, today),
		dpiaReview(features, ownEntries, dpias, approvals),
		requestsReview(requests, today),
		breachReview(breaches, today),
		tabletopReview(members, exercises, today),
		transfersReview(catalogue.Flows, transfers, today),
		restoreDrillReview(members, drills, today, installation),
	}
	for i := range out {
		out[i].Cadence = cadences[out[i].Kind]
	}
	return out, nil
}

var cadences = map[string]string{"review-log": "weekly", "access-review": "quarterly", "accountant-review": "quarterly",
	"close-signoff": "monthly", "break-glass-review": "each use", "policy-acknowledgement": "each version", "risk-review": "by review date",
	"data-inventory": "quarterly", "dpia": "each processing", "pd-requests": "each request",
	"breach": "each breach", "breach-tabletop": "twice a year", "transfers": "by review date", "restore-drill": "quarterly"}

func titled(mode string) string { return strings.ToUpper(mode[:1]) + mode[1:] }

func dayOf(t time.Time) string { return t.UTC().Format("2 Jan 2006") }

// The review log (FM-3): in Founder mode another founder reviews each logged action, overdue 14 days after it executed.
// A break-glass has its own review.
func reviewLogReview(entries []storage.ReviewLogEntry, mode string, now time.Time) Review {
	r := Review{Kind: "review-log", Owner: "owner", Where: "/review-log",
		Evidence: "Each logged action's review, with who reviewed it, when and their note, in the review log and the audit trail"}
	if mode != team.Founder {
		r.Status = ReviewNotApplicable
		r.Detail = titled(mode) + " mode: approvals before the action replace the weekly review."
		return r
	}
	for _, e := range entries {
		switch {
		case e.Kind == storage.ReviewBreakGlass:
		case e.ReviewedBy != nil && e.ReviewedAt != nil:
			r.signed("user:"+e.ReviewedBy.String(), *e.ReviewedAt)
		case e.ReviewedBy == nil:
			r.wait(e.LoggedAt.Add(approval.ReviewOverdue), now.Sub(e.LoggedAt) > approval.ReviewOverdue)
		}
	}
	if r.Waiting > 0 {
		r.Detail = fmt.Sprintf("%d logged action(s) wait for another founder's review, %d of them past 14 days.", r.Waiting, r.Overdue)
	}
	r.settle("No logged action waits for its review.")
	return r
}

// The access review (FR-CTL-06, SEC-06): an Owner attests each calendar quarter once it ends, overdue 14 days after.
func accessReviewOf(members []storage.Member, attested []storage.AccessReviewAttestation, today time.Time) Review {
	r := Review{Kind: "access-review", Owner: "owner", Where: "/settings/access-review",
		Evidence: "Each quarter's attestation, with the report attested and its SHA-256, and a CSV of it"}
	since := today
	for _, m := range members {
		if d := m.Since.UTC().Truncate(24 * time.Hour); d.Before(since) {
			since = d
		}
	}
	var quarters []time.Time
	for _, a := range attested {
		quarters = append(quarters, a.Quarter)
		r.signed(a.AttestedBy, a.AttestedAt)
	}
	if q, ok := accessreview.Due(today, since); ok && !accessreview.Covered(q, quarters) {
		r.wait(accessreview.DueBy(q), true)
		r.Period = accessreview.Label(q)
	} else if q, ok := accessreview.ToAttest(today, since); ok && !accessreview.Covered(q, quarters) {
		r.wait(accessreview.DueBy(q), false)
		r.Period = accessreview.Label(q)
	}
	if r.Waiting > 0 {
		r.Detail = fmt.Sprintf("The access review of %s waits for an Owner's attestation, due by %s.", r.Period, dayOf(*r.DueOn))
	}
	r.settle("No quarter's access review waits for its attestation.")
	return r
}

// The accountant's review (FM-7): in Founder mode the external accountant reviews each calendar quarter's last closed
// month, overdue 60 days after the quarter ends.
func accountantReviewOf(mode string, o storage.Organisation, periods []storage.Period, reviews []storage.AccountantReview, today time.Time) Review {
	r := Review{Kind: "accountant-review", Owner: "accountant", Where: "/accounting/reviews",
		Evidence: "Each signed-off review, with its queries and answers, its adjusting journals and the close snapshot it covers"}
	for _, x := range reviews {
		if x.SignedBy != nil && x.SignedAt != nil && x.Status != storage.AccountantReviewSuperseded {
			r.signed("user:"+x.SignedBy.String(), *x.SignedAt)
		}
	}
	if mode != team.Founder {
		r.Status = ReviewNotApplicable
		r.Detail = titled(mode) + " mode: the accountant's quarterly review is a Founder-mode control (FM-7)."
		return r
	}
	var from time.Time
	switch {
	case o.Profile.BooksStart != nil:
		from = *o.Profile.BooksStart
	case len(periods) > 0:
		from = periods[0].StartsOn
	default:
		r.settle("No books yet.")
		return r
	}
	for _, q := range accountantreview.Quarters(periods, reviews, from, today) { // newest first
		if q.Status == accountantreview.Reviewed {
			continue
		}
		r.wait(q.DueBy, q.Status == accountantreview.Late)
		r.Period = q.Label()
	}
	if r.Waiting > 0 {
		r.Detail = fmt.Sprintf("%d quarter(s) wait for the accountant's signed-off review, the earliest %s, due by %s.", r.Waiting, r.Period,
			dayOf(*r.DueOn))
	}
	r.settle("Every quarter that ended has its signed-off review.")
	return r
}

// The close's sign-off (FR-GL-07, FM-4): each month that ended is closed, attested by someone who did not prepare it,
// within 15 days of its end (C.2 #18).
func closeSignOff(periods []storage.Period, closeOf func(storage.Period) (*storage.PeriodClose, error), today time.Time) (Review, error) {
	r := Review{Kind: "close-signoff", Owner: "controller", Where: "/accounting/close",
		Evidence: "Each close's attestation, with the ledger revision it closed at and the SHA-256 of its snapshot"}
	signed := false
	for i := len(periods) - 1; i >= 0; i-- { // latest first
		p := periods[i]
		if !Day(p.EndsOn).Before(today) {
			continue
		}
		if p.Status == storage.PeriodClosed {
			if signed {
				continue
			}
			c, err := closeOf(p)
			if err != nil {
				return Review{}, err
			}
			if c != nil {
				r.signed(c.AttestedBy, c.AttestedAt)
				signed = true
			}
			continue
		}
		due := Day(p.EndsOn).AddDate(0, 0, checks.UnclosedAfter)
		r.wait(due, due.Before(today))
		r.Period = p.EndsOn.Format("January 2006")
	}
	if r.Waiting > 0 {
		r.Detail = fmt.Sprintf("%d month(s) that ended wait for their close, the earliest %s, due by %s.", r.Waiting, r.Period, dayOf(*r.DueOn))
	}
	r.settle("Every month that ended is closed.")
	return r, nil
}

// The break-glass post-review (FM-8): each break-glass is reviewed once it ends, by another Owner, or by the Accountant
// from its due date, 7 days after it ends.
func breakGlassReview(glass []storage.BreakGlass, entries []storage.ReviewLogEntry, now time.Time) Review {
	r := Review{Kind: "break-glass-review", Owner: "owner", Where: "/break-glass",
		Evidence: "Each break-glass's review, with what it released, in the review log and the audit trail"}
	reviewed := map[string]bool{}
	for _, e := range entries {
		if e.Kind == storage.ReviewBreakGlass && e.ReviewedBy != nil && e.ReviewedAt != nil {
			reviewed[e.ObjectKey] = true
			r.signed("user:"+e.ReviewedBy.String(), *e.ReviewedAt)
		}
	}
	for _, g := range glass {
		if reviewed[g.ID.String()] || now.Before(g.Ends()) {
			continue
		}
		r.wait(breakglass.Due(g), !now.Before(breakglass.Due(g)))
	}
	if r.Waiting > 0 {
		r.Detail = fmt.Sprintf("%d break-glass(es) that ended wait for their review, %d of them past 7 days.", r.Waiting, r.Overdue)
	}
	none := "Every break-glass is reviewed."
	if len(glass) == 0 {
		none = "No break-glass was used."
	}
	r.settle(none)
	return r
}

// Policy acknowledgements (CMP-07): each member acknowledges each version in force, within 30 days of the later of its
// publication and their joining, of each policy and of the members' privacy notice (CMP-PD-03). They are signed off on
// the Reviews and attestations page.
func policyAcknowledgements(lib storage.PolicyLibrary, members []storage.Member, now time.Time) Review {
	r := Review{Kind: "policy-acknowledgement", Owner: "member", Where: "/compliance/attestations",
		Evidence: "Each acknowledgement, with the member, the version and the SHA-256 of its text"}
	current := map[string]storage.PublishedPolicy{}
	for _, v := range lib.Versions { // oldest first
		current[v.Key] = v
	}
	acked := map[string]bool{}
	for _, a := range lib.Acknowledgements {
		acked[fmt.Sprintf("%s@%d %s", a.Key, a.Version, a.By)] = true
		r.signed(a.By, a.At)
	}
	for _, key := range registry.LibraryKeys {
		v, ok := current[key]
		if !ok || !registry.Acknowledged(key) {
			continue
		}
		for _, m := range members {
			if acked[fmt.Sprintf("%s@%d user:%s", key, v.Version, m.UserID)] {
				continue
			}
			due := AcknowledgeBy(v.PublishedAt, m.Since)
			r.wait(due, now.After(due))
		}
	}
	if r.Waiting > 0 {
		r.Detail = fmt.Sprintf("%d acknowledgement(s) of the policies in force wait, %d of them overdue.", r.Waiting, r.Overdue)
	}
	none := "Every member acknowledged every policy in force."
	if len(current) == 0 {
		none = "No policy is published yet."
	}
	r.settle(none)
	return r
}

// Risk reviews (CMP-06): each open risk is reviewed by its review date. A risk waits only once its date has passed;
// until then the next date is shown.
func riskReviews(risks []storage.Risk, today time.Time) Review {
	r := Review{Kind: "risk-review", Owner: "dpo", Where: "/compliance/risks",
		Evidence: "Each review, with the risk version reviewed, the reviewer and their note"}
	var next *time.Time
	var nextRef string
	for _, x := range risks {
		if l := x.LastReview; l != nil {
			r.signed(l.ReviewedBy, l.ReviewedAt)
		}
		if x.State != "open" {
			continue
		}
		d := Day(x.NextReviewOn)
		if d.Before(today) {
			r.wait(d, true)
			continue
		}
		if next == nil || d.Before(*next) {
			next, nextRef = &d, x.Ref
		}
	}
	if r.Waiting > 0 {
		r.Detail = fmt.Sprintf("%d risk(s) are past their review date.", r.Waiting)
	} else if next != nil {
		r.DueOn = next
		r.Detail = fmt.Sprintf("The next review is %s's, by %s.", nextRef, dayOf(*next))
	}
	none := "No risk is open."
	if len(risks) == 0 {
		none = "The risk register is empty."
	}
	r.settle(none)
	return r
}

// The personal-data inventory's review (CMP-PD-02, CTL-DP-05; rev 0.244): the Compliance officer reviews it once each
// calendar quarter ends, overdue 14 days after, as the access review is.
func inventoryReview(members []storage.Member, reviews []storage.InventoryReview, today time.Time) Review {
	r := Review{Kind: "data-inventory", Owner: "dpo", Where: "/compliance/data-protection",
		Evidence: "Each quarter's review, with who reviewed the inventory, when and their note"}
	since := today
	for _, m := range members {
		if d := m.Since.UTC().Truncate(24 * time.Hour); d.Before(since) {
			since = d
		}
	}
	var quarters []time.Time
	for _, x := range reviews {
		quarters = append(quarters, x.Quarter)
		r.signed(x.ReviewedBy, x.ReviewedAt)
	}
	if q, ok := accessreview.Due(today, since); ok && !accessreview.Covered(q, quarters) {
		r.wait(accessreview.DueBy(q), true)
		r.Period = accessreview.Label(q)
	} else if q, ok := accessreview.ToAttest(today, since); ok && !accessreview.Covered(q, quarters) {
		r.wait(accessreview.DueBy(q), false)
		r.Period = accessreview.Label(q)
	}
	if r.Waiting > 0 {
		r.Detail = fmt.Sprintf("The personal-data inventory's review of %s waits for the Compliance officer, due by %s.", r.Period, dayOf(*r.DueOn))
	}
	r.settle("No quarter's inventory review waits.")
	return r
}

// DPIAName is how people say each of Fyniq's impact assessments' processing.
var DPIAName = map[string]string{"payroll": "the payroll link", "ai-assistant": "the AI assistant"}

// Impact assessments (CMP-PD-08, rev 0.244): each processing that needs one has an approved assessment: Fyniq's
// payroll link and AI assistant once their features are switched on, and each open inventory entry marked as needing
// one. The processing goes on already, so one without an approved assessment is overdue.
func dpiaReview(features map[string]bool, entries []storage.InventoryEntry, versions []storage.DPIAVersion, approvals []storage.DPIAApproval) Review {
	r := Review{Kind: "dpia", Owner: "dpo", Where: "/compliance/data-protection",
		Evidence: "Each approved assessment's version, with its text's SHA-256, who approved it and when"}
	approved := map[string]bool{}
	for _, a := range approvals {
		approved[a.Key] = true
		r.signed(a.ApprovedBy, a.ApprovedAt)
	}
	var missing []string
	for _, key := range registry.DPIAKeys {
		if features[key] && !approved[key] {
			missing = append(missing, DPIAName[key])
		}
	}
	for _, e := range entries {
		if !e.NeedsDPIA || e.State != "open" {
			continue
		}
		covered := false
		for _, v := range versions {
			if v.EntryID != nil && *v.EntryID == e.EntryID && approved[v.Key] {
				covered = true
			}
		}
		if !covered {
			missing = append(missing, strings.ToLower(e.What[:1])+e.What[1:])
		}
	}
	for range missing {
		r.Waiting++
		r.Overdue++
	}
	if len(missing) > 0 {
		r.Detail = fmt.Sprintf("%d processing(s) go on without an approved impact assessment: %s.", len(missing), strings.Join(missing, "; "))
	}
	r.settle("Every processing that needs an impact assessment has an approved one.")
	return r
}

// RequestDue is when a request is to be answered: 30 days after receipt, or the day its written extension names.
func RequestDue(r storage.PDRequest) time.Time {
	due := r.DueOn
	for _, s := range r.Steps {
		if s.Step == "extended" && s.NewDueOn != nil {
			due = *s.NewDueOn
		}
	}
	return due
}

// Access and correction requests (CMP-PD-04, rev 0.244): each answered within 30 days of its receipt, or by the day
// the requester was told in writing within those 30 days.
func requestsReview(requests []storage.PDRequest, today time.Time) Review {
	r := Review{Kind: "pd-requests", Owner: "dpo", Where: "/compliance/data-protection",
		Evidence: "Each request with its steps: acknowledged, extended with the written notice, answered with its outcome, closed"}
	for _, q := range requests {
		answered := false
		for _, s := range q.Steps {
			if s.Step == "answered" {
				answered = true
				r.signed(s.By, s.At)
			}
		}
		if !answered {
			due := RequestDue(q)
			r.wait(due, today.After(due))
		}
	}
	if r.Waiting > 0 {
		r.Detail = fmt.Sprintf("%d access or correction request(s) wait for their answer, %d of them past their day.", r.Waiting, r.Overdue)
	}
	r.settle("No access or correction request waits.")
	return r
}

// BreachTimers are a breach's two PDPA timers on today (CMP-PD-05): its assessment, due 30 days after awareness until it
// is assessed; and, once it is decided notifiable, the PDPC's notification, due 3 calendar days after the decision until
// it is made. A nil due day means that timer does not run.
func BreachTimers(b storage.Breach) (assessment, pdpc *time.Time) {
	assessed, notified := false, false
	var due *time.Time
	for _, s := range b.Steps {
		switch s.Step {
		case "assessed":
			assessed = true
		case "decided":
			due = s.PDPCDueOn
		case "pdpc_notified":
			notified = true
		}
	}
	if !assessed {
		d := b.AssessmentDueOn
		assessment = &d
	}
	if due != nil && !notified {
		pdpc = due
	}
	return assessment, pdpc
}

// Breaches (CMP-PD-05, CTL-DP-06 E; rev 0.244): each open breach waits; one past its assessment's day, or past the
// PDPC's once decided notifiable, is overdue.
func breachReview(breaches []storage.Breach, today time.Time) Review {
	r := Review{Kind: "breach", Owner: "dpo", Where: "/compliance/data-protection",
		Evidence: "Each breach with its steps: assessed, decided notifiable or not, the PDPC notified with its reference, contained, closed"}
	for _, b := range breaches {
		closed := false
		for _, s := range b.Steps {
			if s.Step == "closed" {
				closed = true
				r.signed(s.By, s.At)
			}
		}
		if closed {
			continue
		}
		assessment, pdpc := BreachTimers(b)
		switch {
		case pdpc != nil:
			r.wait(*pdpc, today.After(*pdpc))
		case assessment != nil:
			r.wait(*assessment, today.After(*assessment))
		default:
			r.Waiting++ // decided not notifiable, or notified, and not yet closed: no timer runs
		}
	}
	if r.Waiting > 0 {
		r.Detail = fmt.Sprintf("%d breach(es) are open, %d of them past a PDPA timer.", r.Waiting, r.Overdue)
	}
	r.settle("No breach is open.")
	return r
}

// The tabletop exercise (CTL-DP-06 S): the breach runbook is rehearsed twice a year, the next due 6 months after the
// last, or after the organisation began; it waits from 30 days before.
func tabletopReview(members []storage.Member, exercises []storage.BreachExercise, today time.Time) Review {
	r := Review{Kind: "breach-tabletop", Owner: "dpo", Where: "/compliance/data-protection",
		Evidence: "Each exercise with its scenario and findings"}
	from := today
	for _, m := range members {
		if d := m.Since.UTC().Truncate(24 * time.Hour); d.Before(from) {
			from = d
		}
	}
	var last *time.Time
	for _, e := range exercises {
		r.signed(e.RecordedBy, e.RecordedAt)
		if last == nil || e.HeldOn.After(*last) {
			held := e.HeldOn
			last = &held
		}
	}
	if last != nil {
		from = *last
	}
	due := from.AddDate(0, 6, 0)
	if !today.Before(due.AddDate(0, 0, -30)) {
		r.wait(due, today.After(due))
		r.Detail = fmt.Sprintf("The next tabletop exercise of the breach runbook is due by %s.", dayOf(due))
	} else {
		r.DueOn = &due
	}
	r.settle(fmt.Sprintf("The next tabletop exercise is due by %s.", dayOf(due)))
	return r
}

// The full restore drill (CTL-RES-02 Q, DR-06): the weekly export restored into an isolated project by
// docs/runbooks/restore-drill.md each quarter, the next due 3 months after the last that recovered, or after the
// organisation began; it waits from 30 days before. A drill whose checks did not all pass is recorded but does not count.
// It is the platform's: it applies in the installation's organisation only.
func restoreDrillReview(members []storage.Member, drills []storage.RestoreDrill, today time.Time, installation bool) Review {
	r := Review{Kind: "restore-drill", Owner: "dpo", Where: "/compliance/controls",
		Evidence: "Each drill with its scenario, what it restored from, the data lost, the time taken and DR-06's checks"}
	if !installation {
		r.Status = ReviewNotApplicable
		r.Detail = "The platform's restore drills are recorded in the installation's own organisation."
		return r
	}
	from := today
	for _, m := range members {
		if d := m.Since.UTC().Truncate(24 * time.Hour); d.Before(from) {
			from = d
		}
	}
	var last *time.Time
	for _, d := range drills {
		if !d.Recovered() {
			continue
		}
		r.signed(d.RecordedBy, d.RecordedAt)
		if last == nil || d.HeldOn.After(*last) {
			held := d.HeldOn
			last = &held
		}
	}
	if last != nil {
		from = *last
	}
	due := from.AddDate(0, 3, 0)
	if !today.Before(due.AddDate(0, 0, -30)) {
		r.wait(due, today.After(due))
		r.Detail = fmt.Sprintf("The next full restore drill is due by %s.", dayOf(due))
		if n := len(drills); n > 0 && !drills[n-1].Recovered() {
			r.Detail += fmt.Sprintf(" The last, on %s, did not recover: it does not count.", dayOf(drills[n-1].HeldOn))
		}
	} else {
		r.DueOn = &due
	}
	r.settle(fmt.Sprintf("The next full restore drill is due by %s.", dayOf(due)))
	return r
}

// TransferReviewWindow is how long before a transfer's next review it waits on the reviews list.
const TransferReviewWindow = 30

// The overseas transfers (CMP-PD-06, CTL-DP-07): each of Fyniq's flows that leaves Singapore needs an open transfer
// assessment, and each open assessment is reviewed by its next review day, waiting from 30 days before. A flow without
// one waits with no day; an assessment past its review is overdue.
func transfersReview(flows []registry.Flow, transfers []storage.Transfer, today time.Time) Review {
	r := Review{Kind: "transfers", Owner: "dpo", Where: "/compliance/data-protection",
		Evidence: "Each overseas transfer with its recipient, countries, data, basis, assessment, evidence and next review"}
	covered := map[string]bool{}
	reviews := 0
	for _, t := range transfers {
		if t.State != "open" {
			continue
		}
		r.signed(t.SavedBy, t.SavedAt)
		if t.Flow != "" {
			covered[t.Flow] = true
		}
		if !today.Before(t.NextReviewOn.AddDate(0, 0, -TransferReviewWindow)) {
			r.wait(t.NextReviewOn, today.After(t.NextReviewOn))
			reviews++
		}
	}
	missing := 0
	for _, f := range flows {
		if f.CrossBorder && !covered[f.Key] {
			r.Waiting++
			missing++
		}
	}
	if r.Waiting > 0 {
		r.Detail = fmt.Sprintf("%d of Fyniq's flows abroad have no transfer assessment; %d assessment(s) are due for review, %d past it.", missing,
			reviews, r.Overdue)
	}
	r.settle("Every flow abroad has a current transfer assessment.")
	return r
}
