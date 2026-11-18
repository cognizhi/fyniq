package checks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/corporate"
	"github.com/cognizhi/fyniq/internal/domain/deadlines"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The corporate profile's cross-checks (PRD-001 FR-CS-03, C.2 #32 to #35; p1-company-secretary task 3.1, design D4).
// Each runs without the agent, and is not assessed, naming the input, while that input is missing or out of date.

// The Appendix D row whose view opens the corporate profile's items (rev 0.282).
const viewCorporate = "Corporate profile and registers: the name, registered office and SSIC code, the officers, the nominee registers and the directors' approval of the financial statements (rev 0.282)"

// The setting where the profile's records are kept.
const corporateSetting = "Compliance › Corporate profile and registers"

// The corporate profile's checks.
var corporateChecks = []Definition{
	{Key: "corporate.secretary", Ref: "C.2 #32", Area: "Corporate", Worst: Error, Title: "The company has its secretary",
		Explain: "A company appoints a secretary within six months of incorporation, and the office is never vacant for more than " +
			"six months. The secretary is a natural person resident in Singapore and never the sole director. With no details " +
			"recorded from incorporation, it is not assessed.",
		Rules: "FR-CS-06, s171", Owner: "accountant", View: viewCorporate, Triggers: []string{"document"}, Packs: []string{"sg-acra"}, Run: secretaryAppointed},
	{Key: "corporate.statements-approved", Ref: "C.2 #33", Area: "Corporate", Worst: Due, Title: "The directors approve the financial statements in time",
		Explain: "The directors approve each year's financial statements before the AGM, or before they are sent to the members " +
			"instead. Within 30 days of that deadline, an approval not recorded is due. With no financial year set, it is not assessed.",
		Rules: "FR-CS-03, s201", Owner: "accountant", View: viewCorporate, Triggers: []string{"document", "statutory_filing"},
		Packs: []string{"sg-deadlines", "sg-acra"}, Run: statementsApproved},
	{Key: "corporate.acra-profile", Ref: "C.2 #34", Area: "Corporate", Worst: Warning, Title: "The corporate profile agrees with ACRA's business profile",
		Explain: "The officers in office and the registered office are compared with the latest ACRA business profile typed in, on " +
			"its day. ACRA keeps the registers of officers, so a difference is fixed in the profile, or explained. With no extract, " +
			"no officers typed in, or one older than the last change recorded, it is not assessed.",
		Rules: "FR-CS-01", Owner: "accountant", View: viewCorporate, Triggers: []string{"document"}, Run: acraProfileAgrees},
	{Key: "corporate.lodgements", Ref: "C.2 #35", Area: "Corporate", Worst: Due, Title: "Every corporate change is lodged with ACRA on time",
		Explain: "An officer's appointment or cessation and a change of the registered office are lodged within 14 days; a nominee " +
			"director or shareholder is entered in the company's register within 7 days and filed with ACRA's central register within " +
			"2 business days after. A change past its deadline with no day lodged recorded is due.",
		Rules: "FR-CS-01, FR-CS-02, G4-102", Owner: "accountant", View: viewCorporate, Triggers: []string{"document"}, Packs: []string{"sg-deadlines", "sg-acra"},
		Run: lodgementsMade},
}

// incorporatedOn is the day of the profile's first detail: the company's incorporation; nil when none is recorded.
func incorporatedOn(details []equity.Recorded[corporate.Detail]) *time.Time {
	var out *time.Time
	for _, d := range details {
		if d.Doc.Status == storage.DocumentPosted && (out == nil || d.Value.From.Before(*out)) {
			from := d.Value.From
			out = &from
		}
	}
	return out
}

// sameOfficer says whether two officers are one person: the same member, or the same name.
func sameOfficer(a, b corporate.Officer) bool {
	if a.Member != nil && b.Member != nil {
		return *a.Member == *b.Member
	}
	return strings.EqualFold(strings.Join(strings.Fields(a.Name), " "), strings.Join(strings.Fields(b.Name), " "))
}

// C.2 #32: no secretary in office six months after incorporation, or after the last one ceased (✖), one still to
// appoint within those six months (⏰), or a sole director as the secretary (✖).
func secretaryAppointed(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	details, err := corporate.Details(ctx, u)
	if err != nil {
		return Result{}, err
	}
	incorporated := incorporatedOn(details)
	if incorporated == nil {
		return Missing("the company's details from incorporation, in the corporate profile", corporateSetting), nil
	}
	officers, err := corporate.Officers(ctx, u)
	if err != nil {
		return Result{}, err
	}
	var directors, secretaries []corporate.Officer
	var ceased *time.Time
	for _, x := range corporate.OfficersOn(officers, e.Today) {
		switch x.Value.Role {
		case corporate.RoleDirector:
			directors = append(directors, x.Value)
		case corporate.RoleSecretary:
			secretaries = append(secretaries, x.Value)
		}
	}
	for _, versions := range officers {
		if cur := equity.InForce(versions); cur != nil && cur.Value.Role == corporate.RoleSecretary && cur.Value.Ceased != nil &&
			(ceased == nil || cur.Value.Ceased.After(*ceased)) {
			ceased = cur.Value.Ceased
		}
	}
	// The two findings never come together: one needs no secretary in office, the other one in office.
	var items []Item
	worst := OK
	if len(secretaries) == 0 {
		acra, ok, err := gst.PackOn(ctx, u, "sg-acra", e.Today)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Missing("an sg-acra version in force", "Settings › Statutory packs"), nil
		}
		rule, from, what := "secretary_appointed", *incorporated, "incorporation on "+day(*incorporated)
		if ceased != nil {
			rule, from, what = "secretary_vacancy", *ceased, "the last secretary ceasing on "+day(*ceased)
		}
		p, due, err := pack.AcraDueSince(acra, rule, from, pack.Weekdays)
		switch {
		case errors.Is(err, pack.ErrNoRule):
			return Missing("an sg-acra rule for the secretary's office", "Settings › Statutory packs"), nil
		case err != nil:
			return Result{}, err
		}
		item := Item{Kind: "corporate_profile", ID: "secretary", Label: "The company secretary", Version: rule + "/" + day(due),
			Detail: fmt.Sprintf("No secretary is in office: appoint one by %s, six months after %s (%s, %s)", day(due), what, p.Section, p.Verdict)}
		if due.Before(e.Today) {
			worst = Error
			item.Detail = fmt.Sprintf("No secretary has been in office since the six months after %s ended on %s (%s): appoint one now", what,
				day(due), p.Section)
		} else {
			worst = Due
		}
		items = append(items, item)
	}
	if len(directors) == 1 {
		for _, s := range secretaries {
			if sameOfficer(directors[0], s) {
				items = append(items, Item{Kind: "corporate_profile", ID: "sole-director", Label: s.Name,
					Version: "sole-director/" + s.Name,
					Detail:  "The sole director is also the secretary, which the Companies Act forbids (s171(1E)): appoint another person as the secretary"})
				worst = Error
			}
		}
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("%s is the company secretary", secretaries[0].Name)), nil
	}
	return Found(worst, items, fmt.Sprintf("%d step(s) for the company secretary's office", len(items)),
		"Appoint a company secretary who is not the sole director, record the appointment in the corporate profile, and lodge it with ACRA within 14 days"), nil
}

// C.2 #33: each financial year whose AGM, or whose statements to the members, falls due within 30 days or has passed with
// nothing recorded, and whose statements the directors' approval is not recorded for (⏰).
func statementsApproved(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	c, err := deadlines.Read(ctx, u, e.Today, e.Today.AddDate(0, 0, 30), e.Today)
	if err != nil {
		return Result{}, err
	}
	approvals, err := corporate.StatementsApprovals(ctx, u)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, it := range c.Items {
		switch {
		case it.Obligation == "sg-deadlines":
			return Missing("an sg-deadlines version in force", "Settings › Statutory packs"), nil
		case it.Obligation == "financial_year":
			return Missing("the books start and the first financial year's end", "Settings › Organisation"), nil
		case it.Obligation != "agm" && it.Obligation != "statements_to_members":
			continue
		case it.Status != deadlines.Overdue && it.Status != deadlines.DueSoon && it.Status != deadlines.Open:
			continue
		case it.Due.After(e.Today.AddDate(0, 0, 30)) || corporate.ApprovalFor(approvals, it.Anchor) != nil:
			continue
		}
		deadline := "the AGM"
		if it.Obligation == "statements_to_members" {
			deadline = "sending them to the members"
		}
		items = append(items, Item{Kind: "statutory_deadline", ID: it.Key, Label: "Financial statements for the year ended " + day(it.Anchor),
			Version: string(it.Status), Detail: fmt.Sprintf("The directors' approval is not recorded, and %s is due on %s: approve them, and record "+
				"the approval with the directors' resolution", deadline, day(it.Due))})
	}
	if len(items) == 0 {
		return Fine("No financial statements wait for the directors' approval within 30 days of their deadline"), nil
	}
	return Found(Due, items, fmt.Sprintf("%d year(s) of financial statements to approve", len(items)),
		"Have the directors approve the statements, then record the approval in the corporate profile with their resolution"), nil
}

// C.2 #34: each way the profile differs from the latest ACRA business profile on its day (⚠); not assessed with no
// extract, no officers typed in, or one older than the last recorded change.
func acraProfileAgrees(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	r, err := corporate.ReconcileProfileIn(ctx, u)
	if err != nil {
		return Result{}, err
	}
	if !r.Assessed {
		return Result{Status: NotAssessed, Needs: "a current ACRA business profile with its officers typed in", Setting: "Equity › ACRA reconciliation",
			Detail: "Not assessed: " + r.Reason}, nil
	}
	on := day(r.Extract.Value.ProfileOn)
	if len(r.Differences) == 0 {
		return Fine("The officers and the registered office agree with ACRA's business profile of " + on), nil
	}
	var items []Item
	for _, d := range r.Differences {
		it := Item{Kind: "corporate_profile", ID: d.Kind + "/" + d.Role + "/" + d.Name, Label: d.Name, Version: r.Extract.Doc.ID.String()}
		switch d.Kind {
		case corporate.DiffMissingHere:
			it.Detail = fmt.Sprintf("ACRA's business profile of %s lists them as %s from %s; the profile has no such officer in office: record them", on,
				d.Role, d.ACRA)
		case corporate.DiffMissingAtACRA:
			it.Detail = fmt.Sprintf("In office as %s in the profile from %s; ACRA's business profile of %s does not list them: lodge the appointment, or "+
				"record their cessation", d.Role, d.Profile, on)
		case corporate.DiffAppointed:
			it.Detail = fmt.Sprintf("Appointed as %s on %s in the profile and on %s in ACRA's business profile of %s: correct whichever is wrong", d.Role,
				d.Profile, d.ACRA, on)
		case corporate.DiffOffice:
			it.ID, it.Label = "office", "The registered office"
			it.Detail = fmt.Sprintf("The profile has %q and ACRA's business profile of %s has %q: lodge the change, or correct the profile", d.Profile, on,
				d.ACRA)
		}
		items = append(items, it)
	}
	return Found(Warning, items, fmt.Sprintf("%d difference(s) from ACRA's business profile of %s", len(items), on),
		"Find which is right: record a new version in the corporate profile, lodge the change with ACRA, or type in a newer business profile"), nil
}

// C.2 #35: each officer's appointment or cessation, change of the details or nominee's step past its deadline with no
// day lodged recorded (⏰); the nominee filings (G4-102) among them. Not assessed with no sg-acra version in force or no
// details from incorporation, when there is something to lodge.
func lodgementsMade(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	c, err := deadlines.Read(ctx, u, e.Today, e.Today, e.Today)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	var missing []string
	for _, it := range c.Items {
		switch {
		case it.Obligation == "sg-deadlines":
			return Missing("an sg-deadlines version in force", "Settings › Statutory packs"), nil
		case !it.Register || !strings.HasPrefix(it.Obligation, "register_officer") && !strings.HasPrefix(it.Obligation, "register_detail") &&
			!strings.HasPrefix(it.Obligation, "register_nominee") && it.Obligation != deadlines.ObligationACRAPack:
			continue
		case it.Status == deadlines.NotAssessed:
			missing = append(missing, it.Reason)
		case it.Status == deadlines.Overdue:
			items = append(items, calendarItem(it, e.Today))
		}
	}
	switch {
	case len(items) == 0 && len(missing) > 0:
		return Result{Status: NotAssessed, Needs: "the corporate profile's details and an sg-acra version in force", Setting: corporateSetting,
			Detail: "Not assessed: " + strings.Join(missing, "; ")}, nil
	case len(items) == 0:
		return Fine("Every corporate change is lodged with ACRA, or still within its deadline"), nil
	}
	return Found(Due, items, fmt.Sprintf("%d corporate change(s) past their lodgement deadline", len(items)),
		"Lodge each change with ACRA on BizFile+, then record the day lodged on its record with the acknowledgement"), nil
}
