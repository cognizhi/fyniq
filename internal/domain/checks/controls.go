package checks

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/accessreview"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/breakglass"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D row whose view opens the members' items (design D7).
const viewUsers = "Users, roles, team mode"

// Where the controls' items are reviewed, being the control itself (rev 0.165).
const (
	InReviewLog    = "the review log"
	InAccessReview = "the access review"
	InBreakGlass   = "the break-glass review" // on Team mode (FM-8)
)

// The Appendix D row whose view opens the break-glass's items: whoever views payment batches sees break-glass (FM-8).
const viewBatches = "Payment batches"

// The audit object types whose change reruns the controls' checks (design D5).
var (
	onReviewLog = []string{"review_log_entry", "review_log_review", "team_mode"}
	onRoles     = []string{"role_assignment", "user_membership", "team_mode", "sod_override"}
	onAttest    = []string{"access_review_attestation", "user_membership"}
	onGlass     = []string{"break_glass", "review_log_review"}
	onOverride  = []string{"bank_reconciliation", "team_mode"}
)

// The Appendix D row whose view opens the overrides' items: whoever views reconciliations sees how they were signed.
const viewReconciliations = "Bank reconciliation sign-off"

// The checks of the controls: p1-checks slice 4 (task 4.2).
var controlChecks = []Definition{
	{Key: "controls.review-log-overdue", Ref: "C.2 #4", Area: "Controls", Worst: Warning, Title: "Another founder reviews each logged action within 14 days",
		Explain: "In Founder mode, each self-approved post and each high-risk change is logged for another founder's weekly review " +
			"(FM-3). An entry still unreviewed 14 days after it executed is overdue. Review it in the review log: it cannot be marked " +
			"reviewed here.",
		Rules: "FM-2, FM-3", Owner: "owner", View: viewAudit, Triggers: onReviewLog, ReviewedIn: InReviewLog, Run: reviewLogOverdue},
	{Key: "controls.sod-conflicts", Ref: "C.2 #5", Area: "Controls", Worst: Error, Title: "Every conflict of duties is covered by the team mode",
		Explain: "One person who administers users and roles and also posts or approves transactions holds a conflict (SOD-6). " +
			"Founder mode allows it with its compensating controls; Standard mode blocks it unless an approved override lets that " +
			"person keep it (§6.4).",
		Rules: "SOD-6, §6.4", Owner: "owner", View: viewUsers, Triggers: onRoles, Run: sodConflicts},
	{Key: "controls.external-users-past-expiry", Ref: "C.2 #6", Area: "Controls", Worst: Error, Title: "No external user stays a member past their end date",
		Explain: "An external user's roles end at their end date (SEC-06). Once every role has ended they can do nothing, but they " +
			"are still a member: remove them, or give them a role again with a new end date.",
		Rules: "SEC-06, FR-ADM-01", Owner: "owner", View: viewUsers, Triggers: onRoles, Run: externalUsersPastExpiry},
	{Key: "controls.access-review-overdue", Ref: "C.2 #7", Area: "Controls", Worst: Warning, Title: "An Owner attests the access review each quarter",
		Explain: "Once each calendar quarter ends, an Owner reviews who can do what on Settings › Access review and attests it " +
			"(FR-CTL-06). A quarter is overdue 14 days after it ends without its attestation or a later one. Attest it there: it " +
			"cannot be marked reviewed here.",
		Rules: "FR-CTL-06, SEC-06", Owner: "owner", View: viewUsers, Triggers: onAttest, ReviewedIn: InAccessReview, Run: accessReviewOverdue},
	{Key: "controls.break-glass-unreviewed", Ref: "C.2 #53", Area: "Controls", Worst: Error, Title: "Every break-glass is reviewed within 7 days of its end",
		Explain: "While the other founder is unreachable, an Owner may use break-glass to release payment batches alone (FM-8). Another " +
			"Owner reviews it, with what it released, once it ends; from 7 days after it ends the Accountant may review it instead. " +
			"Unreviewed by then, it refuses the month's close (FM-4). Review it on Team mode, under Break-glass: it cannot be marked " +
			"reviewed here.",
		Rules: "FM-8, FM-4", Owner: "owner", View: viewBatches, Triggers: onGlass, ReviewedIn: InBreakGlass, Run: breakGlassUnreviewed},
	{Key: "controls.sod-overrides", Ref: "C.2 #62", Area: "Controls", Worst: Warning, Title: "Every override of a conflict of duties is reported",
		Explain: "In Standard and Enterprise modes a person who decided bank lines of an account in a month does not sign off its " +
			"reconciliation (SOD-5), unless they asked for an override with a reason and another person approved it (§6.4). Each " +
			"reconciliation of an open month signed off under an override is listed here, with its reason, for review.",
		Rules: "SOD-5, §6.4", Owner: "owner", View: viewReconciliations, Triggers: onOverride, Run: sodOverrides},
}

// modeOf is the team mode in force and its version: Founder mode, version 0, until the first switch.
func modeOf(ctx context.Context, u storage.UnitOfWork) (string, int, error) {
	all, err := u.TeamModes().List(ctx)
	if err != nil || len(all) == 0 {
		return team.Default, 0, err
	}
	return all[len(all)-1].Mode, all[len(all)-1].Version, nil
}

// C.2 #62: the reconciliations of the open months signed off under an override of SOD-5 (rev 0.179).
func sodOverrides(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	var items []Item
	for _, p := range e.Started() {
		recs, err := u.BankMatching().Reconciliations(ctx, p.ID)
		if err != nil {
			return Result{}, err
		}
		for _, r := range recs {
			if r.SoDOverrideApproval == nil {
				continue
			}
			acc, err := u.Banking().MoneyAccount(ctx, r.MoneyAccountID)
			if err != nil {
				return Result{}, err
			}
			items = append(items, Item{Kind: "bank_reconciliation", ID: r.ID.String(), Label: acc.Name + ", " + p.StartsOn.Format("January 2006"),
				Detail: "Signed off under an override of SOD-5: " + r.SoDOverrideReason, Version: fmt.Sprint(r.Version)})
		}
	}
	if len(items) == 0 {
		return Fine("No reconciliation of an open month was signed off under an override"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d reconciliation(s) signed off under an override of SOD-5", len(items)),
		"Read each reason; mark it reviewed once you are satisfied"), nil
}

// C.2 #4: in Founder mode, review log entries unreviewed 14 days after they executed (FM-3).
func reviewLogOverdue(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	mode, _, err := modeOf(ctx, u)
	if err != nil {
		return Result{}, err
	}
	if mode != team.Founder {
		return Fine(fmt.Sprintf("%s mode: approvals before the action replace the weekly review", strings.ToUpper(mode[:1])+mode[1:])), nil
	}
	entries, err := u.ReviewLog().List(ctx)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, x := range entries {
		if x.Kind == storage.ReviewBreakGlass || x.ReviewedBy != nil || e.Now.Sub(x.LoggedAt) <= approval.ReviewOverdue {
			continue // a break-glass has its own check, C.2 #53
		}
		items = append(items, Item{Kind: "review_log", ID: x.ID.String(), Label: x.Summary,
			Detail: fmt.Sprintf("Logged %s, due for review by %s", day(x.LoggedAt), day(x.LoggedAt.Add(approval.ReviewOverdue)))})
	}
	if len(items) == 0 {
		return Fine("No logged action waits past 14 days for its review"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d logged action(s) unreviewed after 14 days", len(items)),
		"Another founder reviews each in the review log"), nil
}

// C.2 #5: members whose live roles hold a conflict the team mode blocks, with no override for the version in force.
func sodConflicts(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	mode, version, err := modeOf(ctx, u)
	if err != nil {
		return Result{}, err
	}
	if !team.Blocks(mode) {
		return Fine("Founder mode allows the conflicts of duties with its compensating controls"), nil
	}
	overrides, err := u.TeamModes().Overrides(ctx)
	if err != nil {
		return Result{}, err
	}
	members, err := u.Members().List(ctx, e.Now)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, m := range members {
		var roles []authz.Role
		for _, r := range m.Roles {
			roles = append(roles, authz.Role(r.Role))
		}
		for _, c := range authz.RoleConflicts(roles) {
			if slices.ContainsFunc(overrides, func(o storage.SoDOverride) bool {
				return o.TeamModeVersion == version && o.UserID == m.UserID && o.Conflict == c.SoD
			}) {
				continue
			}
			names := make([]string, len(c.Roles))
			for i, r := range c.Roles {
				names[i] = string(r)
			}
			items = append(items, Item{Kind: "member", ID: m.ID.String(), Label: m.DisplayName,
				Detail: fmt.Sprintf("%s: %s, which %s mode blocks, with no override", c.SoD, listed(names), mode)})
		}
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("No member holds a conflict %s mode blocks without an override", mode)), nil
	}
	return Found(Error, items, fmt.Sprintf("%d member(s) hold a conflict of duties nothing covers", len(items)),
		"End one of the conflicting roles, or switch the team mode again with an override and its reason"), nil
}

// C.2 #6: members whose roles have all ended at their end dates while they are still members (SEC-06).
func externalUsersPastExpiry(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	members, err := u.Members().List(ctx, e.Now)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, m := range members {
		if len(m.Roles) > 0 || len(m.Expired) == 0 {
			continue
		}
		var ended []string
		for _, r := range m.Expired {
			ended = append(ended, fmt.Sprintf("%s on %s", r.Role, day(*r.ExpiresAt)))
		}
		items = append(items, Item{Kind: "member", ID: m.ID.String(), Label: m.DisplayName, Detail: "Roles ended: " + strings.Join(ended, ", ")})
	}
	if len(items) == 0 {
		return Fine("No member stays past the end of their roles"), nil
	}
	return Found(Error, items, fmt.Sprintf("%d external user(s) still members after their roles ended", len(items)),
		"Remove them on Settings › Members, or give them a role again with a new end date"), nil
}

// C.2 #7: the latest quarter past its review's due date, with neither its attestation nor a later one (FR-CTL-06).
func accessReviewOverdue(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	members, err := u.Members().List(ctx, e.Now)
	if err != nil {
		return Result{}, err
	}
	since := e.Today
	for _, m := range members {
		if d := m.Since.UTC().Truncate(24 * time.Hour); d.Before(since) {
			since = d
		}
	}
	q, ok := accessreview.Due(e.Today, since)
	if !ok {
		return Fine("No quarter's review is due yet"), nil
	}
	attested, err := u.AccessReviews().List(ctx)
	if err != nil {
		return Result{}, err
	}
	var quarters []time.Time
	for _, a := range attested {
		quarters = append(quarters, a.Quarter)
	}
	if accessreview.Covered(q, quarters) {
		return Fine(fmt.Sprintf("The access review is attested for %s", accessreview.Label(q))), nil
	}
	return Found(Warning, []Item{{Kind: "access_review", ID: q.Format(time.DateOnly), Label: accessreview.Label(q) + " access review",
		Detail: "Due by " + day(accessreview.DueBy(q))}}, "The access review is overdue",
		"An Owner reviews who can do what on Settings › Access review and attests it"), nil
}

// day is a date as the checks write one, such as 2 Jan 2006.
func day(t time.Time) string { return t.UTC().Format("2 Jan 2006") }

// listed joins words as a sentence does: "a", "a and b", "a, b and c".
func listed(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// C.2 #53: break-glasses unreviewed after their review's due date, 7 days after they ended, in any mode (FM-8, FM-4).
func breakGlassUnreviewed(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	glass, err := u.BreakGlasses().List(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(glass) == 0 {
		return Fine("No break-glass was used"), nil
	}
	entries, err := u.ReviewLog().List(ctx)
	if err != nil {
		return Result{}, err
	}
	reviewed := map[string]bool{}
	for _, x := range entries {
		if x.Kind == storage.ReviewBreakGlass && x.ReviewedBy != nil {
			reviewed[x.ObjectKey] = true
		}
	}
	var items []Item
	for _, g := range glass {
		if reviewed[g.ID.String()] || e.Now.Before(breakglass.Due(g)) {
			continue
		}
		items = append(items, Item{Kind: "break_glass", ID: g.ID.String(), Label: fmt.Sprintf("Break-glass of %s: %s", day(g.StartedAt), g.Reason),
			Detail: fmt.Sprintf("Ended %s, due for review by %s", day(g.Ends()), day(breakglass.Due(g)))})
	}
	if len(items) == 0 {
		return Fine("Every break-glass is reviewed, or not due for review yet"), nil
	}
	return Found(Error, items, fmt.Sprintf("%d break-glass(es) unreviewed 7 days after they ended", len(items)),
		"Another Owner, or the Accountant, reviews each on Team mode"), nil
}
