package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D row whose view opens the founders' items (design D7).
const viewDirectors = "Director accounts, settlements, share issues (rev 0.3)"

// The audit object types whose change reruns the founders' checks (p1-founders-money design D15).
var (
	onDirectorBalances = []string{"document", "journal_entry", "contact"}
	onShareCapital     = []string{"document", "journal_entry", "share_issue"}
)

// The checks of the founders' money: p1-founders-money slice 6 (task 6.2, design D15).
var foundersChecks = []Definition{
	{Key: "founders.director-in-debit", Ref: "C.2 #21", Area: "Ledger", Worst: Warning, Title: "No director's account is in debit at the month end",
		Explain: "A director or shareholder whose balances on 2500 and 2120 together are a debit owes the company money. Loans " +
			"to directors are banned unless the company is an exempt private company, or the debit is an approved advance for " +
			"company expenses (Companies Act s162). Repay it, or ask the accountant.",
		Rules: "FR-EQ-11", Owner: "controller", View: viewDirectors, Triggers: onDirectorBalances, Run: directorInDebit},
	{Key: "founders.conversion-registered", Ref: "C.2 #22", Area: "Ledger", Worst: Due, Title: "Every approved conversion is lodged and registered on time",
		Explain: "Shares are issued only with the shareholders' approval, lodged with ACRA within 14 days of the resolution, and " +
			"the approval lapses at the next AGM. An approved conversion ACRA has not registered after the days set, or whose " +
			"approval lapses within 30 days, is for review.",
		Rules: "FR-EQ-09", Owner: "controller", View: viewDirectors, Triggers: []string{"document"}, Run: conversionsRegistered},
	{Key: "founders.balances-confirmed", Ref: "C.2 #23", Area: "Ledger", Worst: Warning, Title: "Every director confirmed their balances at the year end",
		Explain: "Each director or shareholder confirms what the company owed them, or they owed it, at each financial year's " +
			"end, with a passkey. A balance not confirmed, or changed since it was, is for review: the auditor asks for these " +
			"confirmations.",
		Rules: "FR-EQ-12", Owner: "controller", View: viewDirectors, Triggers: onDirectorBalances, CloseKeys: []string{"directors-confirmed"}, Run: balancesConfirmed},
	{Key: "founders.share-capital-registered", Ref: "C.2 #25", Area: "Ledger", Worst: Error, Title: "Share capital agrees with the register of share issues",
		Explain: "Each share-capital account moves only with the register of share issues or the opening balances. A balance " +
			"that differs from what the register says was issued in that class means an issue is missing from the register, " +
			"or share capital was posted another way.",
		Rules: "INV-13, FR-EQ-08", Owner: "accountant", View: viewDirectors, Triggers: onShareCapital, Run: shareCapitalRegistered},
}

// latestMonthEnd is the last day of the month before today's.
func latestMonthEnd(today time.Time) time.Time {
	return time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
}

// C.2 #21: each director or shareholder whose net balance on 2500 and 2120 is a debit at the latest month end, in the
// words the exempt-private-company status in force gives (design D9).
func directorInDebit(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	end := latestMonthEnd(e.Today)
	people, _, _, err := director.Accounts(ctx, u, e.Base, end)
	if err != nil {
		return Result{}, err
	}
	statuses, err := director.ExemptStatuses(ctx, u)
	if err != nil {
		return Result{}, err
	}
	words := director.DebitWarning(director.ExemptOn(statuses, end))
	var items []Item
	for _, p := range people {
		if p.Net.Sign() <= 0 {
			continue
		}
		items = append(items, Item{Kind: "contact", ID: p.Contact.ID.String(), Label: p.Contact.LegalName,
			Detail: fmt.Sprintf("Owes the company %s %s at %s. %s", p.Net, p.Net.Currency().Code, day(end), words), Version: p.Net.String()})
	}
	if len(items) == 0 {
		return Fine("No director's account is in debit at " + day(end)), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d director's account(s) in debit at %s", len(items), day(end)),
		"Repay what is owed, or ask the accountant: a loan to a director is banned unless the company is an exempt private company"), nil
}

// C.2 #22: what each approved conversion not yet effective raises (design D12): ⏰ its approval not lodged 14 days after
// the resolution; ⚠ not registered after the days set; ⚠ within 30 days of its approval lapsing.
func conversionsRegistered(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	days, err := director.RegistrationDays(ctx, u, e.Today)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	due := false
	err = eachDocument(ctx, u, storage.DocumentDirectorSettlement, func(doc storage.Document) error {
		if doc.Status != storage.DocumentPosted {
			return nil
		}
		s, err := director.ReadSettlement(doc)
		if err != nil || s.Route != director.RouteConvert || s.ApprovedOn == nil {
			return err
		}
		issue, err := u.ShareIssues().OfSettlement(ctx, doc.ID)
		if err != nil || issue != nil {
			return err
		}
		all, err := u.ConversionSteps().Of(ctx, doc.ID)
		if err != nil {
			return err
		}
		steps := director.StepsOf(all)
		lapses, _ := director.LapsesOnFor(steps, e.Org.Profile, *s.ApprovedOn)
		c, err := u.Contacts().Get(ctx, s.ContactID)
		if err != nil {
			return err
		}
		for _, f := range director.Flags(steps, *s.ApprovedOn, lapses, e.Today, days) {
			sev := string(Warning) // a ⚠ beside a ⏰; the ⏰ items take the check's own status
			if f.Kind == "due" {
				due, sev = true, ""
			}
			items = append(items, Item{Kind: "document", ID: doc.ID.String() + "/" + f.Code, Label: doc.Number + " " + c.LegalName,
				Detail: upperFirst(f.Message) + ". " + f.Fix, Severity: sev, Version: fmt.Sprint(len(all))})
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	if len(items) == 0 {
		return Fine("Every approved conversion is lodged and registered on time"), nil
	}
	status := Warning
	if due {
		status = Due
	}
	return Found(status, items, fmt.Sprintf("%d step(s) of approved conversions to follow up", len(items)),
		"Lodge what is due with ACRA and record each step on the settlement, then the day ACRA registers the shares"), nil
}

// C.2 #23: each director or shareholder with a balance at the latest financial year end who has not confirmed it, or
// whose balances changed since they did (design D13); none before a year has ended.
func balancesConfirmed(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	end, ok := director.LastYearEnd(e.Org.Profile, e.Today)
	if !ok || !end.Before(e.Today) {
		return Fine("No financial year has ended yet"), nil
	}
	people, err := balance.People(ctx, u, e.Base, end)
	if err != nil {
		return Result{}, err
	}
	confirmations, err := director.ConfirmationsOf(ctx, u)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, x := range people {
		if x.Party == uuid.Nil || len(x.Accounts) == 0 {
			continue
		}
		state := director.StateOf(director.LatestFor(confirmations[x.Party], end), x.Accounts)
		if state == director.Confirmed {
			continue
		}
		c, err := u.Contacts().Get(ctx, x.Party)
		if err != nil {
			return Result{}, err
		}
		what := "Not confirmed: they confirm with a passkey in Money I put in"
		switch {
		case c.MemberUserID == nil:
			what = "Not confirmed: link the member who signs in as them first, then they confirm"
		case state == director.ChangedSince:
			what = "Changed since they confirmed: an entry dated in the year was posted after; they confirm again"
		}
		items = append(items, Item{Kind: "contact", ID: x.Party.String(), Label: c.LegalName, Detail: what + " (" + day(end) + ")", Version: end.Format(time.DateOnly) + "/" + string(state)})
	}
	if len(items) == 0 {
		return Fine("Every director confirmed their balances at " + day(end)), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d balance(s) at %s not confirmed", len(items), day(end)),
		"Ask each director to confirm with a passkey in Money I put in"), nil
}

// C.2 #25: each share-capital account whose balance through today differs from the register's issues of its class
// effective by then (INV-13).
func shareCapitalRegistered(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	all, err := u.Accounts().List(ctx)
	if err != nil {
		return Result{}, err
	}
	issues, err := u.ShareIssues().List(ctx)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	checked := 0
	for _, a := range all {
		if a.Role != storage.RoleShareCapital || a.Group {
			continue
		}
		checked++
		totals, err := balance.Control(ctx, u, e.Base, a.ID, e.Today)
		if err != nil {
			return Result{}, err
		}
		held := money.Zero(e.Base)
		for _, t := range totals {
			if held, err = held.Add(t.Base); err != nil {
				return Result{}, err
			}
		}
		registered := money.Zero(e.Base)
		for _, s := range issues {
			if s.ClassAccountID != a.ID || s.EffectiveOn.After(e.Today) {
				continue
			}
			m, err := money.ParseStored(string(s.Amount), e.Base)
			if err != nil {
				return Result{}, err
			}
			if registered, err = registered.Add(m); err != nil {
				return Result{}, err
			}
		}
		if held.Neg().Equal(registered) {
			continue
		}
		items = append(items, Item{Kind: "account", ID: a.ID.String(), Label: a.Code + " " + a.Name,
			Detail: fmt.Sprintf("The account holds %s; the register's issues of the class come to %s", held.Neg(), registered), Version: held.String() + "/" + registered.String()})
	}
	if len(items) == 0 {
		if checked == 0 {
			return Fine("The chart has no share-capital account yet"), nil
		}
		return Fine("Every share-capital account agrees with the register of share issues"), nil
	}
	return Found(Error, items, fmt.Sprintf("%d share-capital account(s) differ from the register", len(items)),
		"Record the shares issued before the books start in the register, or find the posting that is not an issue"), nil
}

// eachDocument runs each on every document of a type, oldest first.
func eachDocument(ctx context.Context, u storage.UnitOfWork, t storage.DocumentType, each func(storage.Document) error) error {
	var afterDate string
	var afterID *uuid.UUID
	for {
		docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: t, AfterDate: afterDate, AfterID: afterID, Limit: 500})
		if err != nil {
			return err
		}
		for _, d := range docs {
			afterDate, afterID = d.Date, &d.ID
			if err := each(d); err != nil {
				return err
			}
		}
		if len(docs) < 500 {
			return nil
		}
	}
}

// upperFirst starts a sentence with a capital.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
