package checks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D row whose view opens the equity register's items.
const viewEquity = "Cap table, holders, investors, equity transactions (rev 0.3)"

// The checks of the equity register (p1-equity-register).
var equityChecks = []Definition{
	{Key: "equity.instruments-converted", Ref: "C.2 #29", Area: "Equity", Worst: Warning, Title: "Every SAFE and note converts when it should",
		Explain: "A SAFE or note converts into shares at a priced round, and a note falls due at its maturity. A recorded one " +
			"whose maturity has passed, or that shares allotted for cash after it was signed have not converted, is for review: " +
			"draft its conversion from the round, or settle the note under its terms.",
		Rules: "FR-EQ-18", Owner: "accountant", View: viewEquity, Triggers: []string{"document", "share_issue", "holding_movement"}, Run: instrumentsConverted},
	{Key: "equity.controllers-registered", Ref: "C.2 #27", Area: "Equity", Worst: Due, Title: "Every registrable controller is in the registers on time",
		Explain: "A holder with more than 25% of the shares or of the votes is a registrable controller: the company enters them in " +
			"its register within 7 days, and updates ACRA's central register within 2 business days after, then sends each a notice " +
			"at least once a year. Rights that could confer control (a board majority, consent matters) cannot be tested from the cap " +
			"table: until the company secretary's conclusion is recorded, the check is not assessed, never all clear.",
		Rules: "FR-EQ-25", Owner: "accountant", View: viewEquity, Triggers: []string{"document", "share_issue", "holding_movement"}, Run: controllersRegistered},
	{Key: "equity.exempt-status", Ref: "C.2 #28", Area: "Equity", Worst: Warning, Title: "The exempt-private-company status recorded still holds",
		Explain: "Each change to the register tests the status again: more than 20 members, or a member that is a company or another " +
			"entity, ends exempt-private-company status. When the register says otherwise than the status recorded, record the new " +
			"status from the ACRA business profile and re-check the directors' accounts in debit (FR-EQ-11).",
		Rules: "FR-EQ-25, FR-EQ-11", Owner: "accountant", View: viewEquity, Triggers: []string{"document", "share_issue", "holding_movement"}, Run: exemptStatusHolds},
	{Key: "equity.acra-register", Ref: "C.2 #26", Area: "Equity", Worst: Warning, Title: "The register agrees with ACRA's register of members",
		Explain: "The register of members is compared with the latest ACRA business profile typed in, at the profile's day, holder by " +
			"holder and class by class. A difference is either the register's, fixed by a reversal citing the extract, or the " +
			"typing's, fixed by typing the profile in again. With holders and no extract recorded, it is not assessed.",
		Rules: "FR-EQ-26", Owner: "accountant", View: viewEquity, Triggers: []string{"document", "share_issue", "holding_movement"}, Run: acraRegisterAgrees},
}

// C.2 #29: each recorded instrument no conversion still stands for, whose maturity has passed, or which converts at a
// priced round while shares were allotted for cash on or after the day it was signed (the register of share issues).
func instrumentsConverted(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	docs, err := equity.Instruments(ctx, u)
	if err != nil {
		return Result{}, err
	}
	standing, err := equity.StandingConversions(ctx, u)
	if err != nil {
		return Result{}, err
	}
	issues, err := u.ShareIssues().List(ctx)
	if err != nil {
		return Result{}, err
	}
	var rounds []storage.ShareIssue // the allotments for cash to today, latest first
	for _, s := range issues {
		if s.Consideration == storage.ConsiderationCash && !s.EffectiveOn.After(e.Today) {
			rounds = append(rounds, s)
		}
	}
	sort.Slice(rounds, func(a, b int) bool { return rounds[a].EffectiveOn.After(rounds[b].EffectiveOn) })
	var items []Item
	for _, d := range docs {
		if d.Status != storage.DocumentPosted {
			continue
		}
		if _, ok := standing[d.ID]; ok {
			continue
		}
		i, err := equity.ReadInstrument(d)
		if err != nil {
			return Result{}, err
		}
		c, err := u.Contacts().Get(ctx, i.HolderID)
		if err != nil {
			return Result{}, err
		}
		label := c.LegalName + ", " + formName[i.Form]
		if i.MaturityOn != nil && i.MaturityOn.Before(e.Today) {
			items = append(items, Item{Kind: "document", ID: d.ID.String() + "/maturity", Label: label,
				Detail:  fmt.Sprintf("Past its maturity on %s. Convert it under its terms, or settle it with its holder", day(*i.MaturityOn)),
				Version: i.MaturityOn.Format(time.DateOnly)})
		}
		if i.Trigger != "priced_round" {
			continue
		}
		for _, s := range rounds {
			if !s.EffectiveOn.Before(i.SignedOn) {
				items = append(items, Item{Kind: "document", ID: d.ID.String() + "/round", Label: label,
					Detail: fmt.Sprintf("Shares were allotted for cash on %s, after it was signed on %s, and it has not converted. Draft its "+
						"conversion from the round's page, or note why that allotment was not its priced round", day(s.EffectiveOn), day(i.SignedOn)),
					Version: s.ID.String()})
				break
			}
		}
	}
	if len(items) == 0 {
		return Fine("Every recorded SAFE and note is converting, or waits for its round or maturity"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d SAFE(s) or note(s) to convert or settle", len(items)),
		"Draft the conversions from the priced round, or settle a note that has matured"), nil
}

// C.2 #27: each holder over 25% of the shares or votes not yet in the company's register by 7 days after, or not lodged
// in ACRA's central register 2 business days after (⏰); a registered controller whose yearly notice is due (⏰), or whose
// notice has gone unanswered 30 days (⚠); a controller in force whom the test no longer names (⚠); and each holder whose
// rights could confer control and whom no conclusion settles (not assessed, G4-101).
func controllersRegistered(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	t, err := equity.StatutoryTestsIn(ctx, u, e.Today)
	if err != nil {
		return Result{}, err
	}
	name := func(id uuid.UUID) (string, error) {
		c, err := u.Contacts().Get(ctx, id)
		return c.LegalName, err
	}
	var items []Item
	var assess []string
	due := false
	for _, s := range t.Controllers {
		n, err := name(s.HolderID)
		if err != nil {
			return Result{}, err
		}
		over := ""
		switch {
		case s.Shares && s.Votes:
			over = fmt.Sprintf("holds %s%% of the shares and %s%% of the votes", s.Ownership, s.Voting)
		case s.Shares:
			over = fmt.Sprintf("holds %s%% of the shares", s.Ownership)
		case s.Votes:
			over = fmt.Sprintf("holds %s%% of the votes", s.Voting)
		}
		version := s.Standing
		if s.Entry != nil {
			version += "@" + s.Entry.Doc.ID.String() + "/" + fmt.Sprint(s.Entry.Doc.Version)
		}
		item := Item{Kind: "contact", ID: s.HolderID.String(), Label: n, Version: version}
		switch s.Standing {
		case equity.StandingNotAssessed:
			assess = append(assess, n)
			item.ID += "/rights"
			item.Severity = string(Warning)
			item.Detail = "Not assessed: their recorded rights (a board majority or consent matters) could make them a registrable " +
				"controller. Record the company secretary's conclusion on their holder page"
		case equity.StandingToEnter:
			due = true
			lead := "Is a registrable controller"
			if over != "" {
				lead = upperFirst(over) + " since " + day(s.Since) + ", more than 25%"
			}
			item.Detail = fmt.Sprintf("%s: enter them in the company's register of registrable controllers by %s, then ACRA's central "+
				"register within 2 business days", lead, day(*s.CompanyRegisterDue))
		case equity.StandingToLodge:
			due = true
			item.Detail = fmt.Sprintf("Entered in the company's register on %s: update ACRA's central register by %s", day(*s.Entry.Value.CompanyRegister),
				day(*s.CentralRegisterDue))
		case equity.StandingRegistered:
			if s.NoticeDue != nil && !s.NoticeDue.After(e.Today) {
				due = true
				items = append(items, Item{Kind: "contact", ID: s.HolderID.String() + "/notice", Label: n, Version: version,
					Detail: fmt.Sprintf("Their yearly notice was due on %s: send it, and record it on their controller entry", day(*s.NoticeDue))})
			}
			if s.Unanswered != nil {
				items = append(items, Item{Kind: "contact", ID: s.HolderID.String() + "/reply", Label: n, Version: version, Severity: string(Warning),
					Detail: fmt.Sprintf("No reply to the notice of %s after 30 days: follow it up, and update the register within 7 days of the reply",
						day(s.Unanswered.Sent))})
			}
			continue
		default:
			continue
		}
		items = append(items, item)
	}
	for _, x := range t.Lapsed {
		n, err := name(x.Value.HolderID)
		if err != nil {
			return Result{}, err
		}
		items = append(items, Item{Kind: "contact", ID: x.Value.HolderID.String() + "/ceased", Label: n, Severity: string(Warning),
			Version: x.Doc.ID.String() + "/" + fmt.Sprint(x.Doc.Version),
			Detail: "No longer more than 25% of the shares or votes: record the day they ceased to be a controller in both registers, " +
				"unless other grounds keep them one"})
	}
	switch {
	case len(items) == len(assess) && len(assess) > 0:
		return Missing("the company secretary's conclusion on whether the rights of "+strings.Join(assess, ", ")+" confer control",
			"Equity › Holders › the holder › Registrable controller"), nil
	case len(items) == 0:
		return Fine("Every registrable controller is entered in both registers, with their yearly notice sent"), nil
	}
	status := Warning
	if due {
		status = Due
	}
	return Found(status, items, fmt.Sprintf("%d registrable controller step(s) to follow up", len(items)),
		"Enter each controller in the company's register and ACRA's central register, send the yearly notices, and record each step "+
			"on their controller entry"), nil
}

// C.2 #28: the register decides the company's exempt status otherwise than the status recorded in force (⚠): an entity
// member or a 21st one ends it; with every member an individual and 20 or fewer, it holds again. With no status
// recorded, C.2 #21 already treats the company as not exempt, so nothing is raised.
func exemptStatusHolds(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	t, err := equity.StatutoryTestsIn(ctx, u, e.Today)
	if err != nil {
		return Result{}, err
	}
	x := t.Exempt
	if x.Recorded == nil {
		return Fine("No exempt-private-company status is recorded, so the company is treated as not exempt"), nil
	}
	if !x.Differs {
		if !x.Finding.Decided {
			return Fine("The register cannot test it until every member's holder record says individual or entity; the status recorded stands"), nil
		}
		return Fine("The register agrees with the exempt-private-company status recorded"), nil
	}
	var why string
	switch {
	case len(x.Finding.Entities) > 0:
		var names []string
		for _, id := range x.Finding.Entities {
			c, err := u.Contacts().Get(ctx, id)
			if err != nil {
				return Result{}, err
			}
			names = append(names, c.LegalName)
		}
		why = fmt.Sprintf("a member is an entity (%s), which ends exempt status", strings.Join(names, ", "))
	case x.Finding.Members > equity.MaxExemptMembers:
		why = fmt.Sprintf("it has %d members, more than %d", x.Finding.Members, equity.MaxExemptMembers)
	default:
		why = fmt.Sprintf("its %d members are all individuals, so it may be exempt again", x.Finding.Members)
	}
	recorded := "exempt"
	if !x.Recorded.Status.Exempt {
		recorded = "not exempt"
	}
	return Found(Warning, []Item{{Kind: "document", ID: x.Recorded.ID.String(), Label: "Exempt-private-company status",
		Version: fmt.Sprintf("%d/%d/%t", x.Finding.Members, len(x.Finding.Entities), x.Finding.Exempt),
		Detail:  fmt.Sprintf("Recorded as %s from %s, but %s", recorded, day(x.Recorded.Status.From), why)}},
		"The register no longer agrees with the exempt status recorded",
		"Check the ACRA business profile and record the status it shows, then re-check the directors' accounts in debit (FR-EQ-11)"), nil
}

// C.2 #26: each holding where the register on the latest ACRA extract's day differs from the extract (⚠); not assessed
// while the register holds shares and no extract is recorded.
func acraRegisterAgrees(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	latest, diffs, err := equity.ReconcileIn(ctx, u)
	if err != nil {
		return Result{}, err
	}
	if latest == nil {
		t, err := equity.TableIn(ctx, u, e.Today)
		if err != nil {
			return Result{}, err
		}
		if t.Issued.Sign() == 0 {
			return Fine("No shares are recorded, so there is nothing to compare with ACRA yet"), nil
		}
		return Missing("the latest ACRA business profile, its holdings typed in with the PDF attached", "Equity › ACRA reconciliation"), nil
	}
	if len(diffs) == 0 {
		return Fine(fmt.Sprintf("The register agrees with ACRA's business profile of %s", day(latest.Value.ProfileOn))), nil
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return Result{}, err
	}
	codes := map[uuid.UUID]string{}
	for _, a := range accounts {
		codes[a.ID] = a.Code
	}
	var items []Item
	for _, d := range diffs {
		c, err := u.Contacts().Get(ctx, d.HolderID)
		if err != nil {
			return Result{}, err
		}
		items = append(items, Item{Kind: "contact", ID: d.HolderID.String() + "/" + codes[d.AccountID], Label: c.LegalName + ", " + codes[d.AccountID],
			Version: latest.Doc.ID.String() + "/" + d.Register + "/" + d.Acra,
			Detail: fmt.Sprintf("The register holds %s shares and ACRA's business profile of %s lists %s. Fix the register by a reversal citing the "+
				"extract, or type the profile in again", grouped(d.Register), day(latest.Value.ProfileOn), grouped(d.Acra))})
	}
	return Found(Warning, items, fmt.Sprintf("%d holding(s) differ from ACRA's register of members", len(items)),
		"Find which is right: correct the register by a reversal citing the extract, or type the business profile in again"), nil
}

// grouped writes a whole number of shares with thousands separators: "300000" is "300,000".
func grouped(n string) string {
	var b strings.Builder
	for i, r := range n {
		if i > 0 && (len(n)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// formName is an instrument's form in a check's words.
var formName = map[string]string{
	equity.FormPostMoneySAFE: "post-money SAFE", equity.FormPreMoneySAFE: "pre-money SAFE", equity.FormDiscountSAFE: "discount-only SAFE",
	equity.FormMFNSAFE: "MFN SAFE", equity.FormConvertibleNote: "convertible note",
}
