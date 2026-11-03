package checks

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D rows whose view opens the setup's and the bank's items (design D7).
const (
	viewContacts = "Contacts and items (rev 0.33)"
	viewBank     = "Bank transactions, categorise"
)

// The audit object types whose change reruns the setup's and the bank's checks (design D5).
var (
	onChartUse = []string{"account", "bank_rule", "item", "claim_category", "money_account", "contact"}
	onContacts = []string{"contact", "fx_rate"}
	onLines    = []string{"bank_line", "bank_statement", "period", "period_close", "period_reopen", "fx_rate", "account", "money_account", "document"}
)

// The checks of the setup and of the bank's lines: p1-checks slice 2 (tasks 2.1, 2.2).
var setupChecks = []Definition{
	{Key: "setup.chart-problems", Ref: "C.1 #8", Area: "Setup", Worst: Error, Title: "The chart of accounts has no problems",
		Explain: "What posts by itself, a bank rule, an item, a claim category, a money account or a contact's default category, " +
			"needs an active account that takes postings. An account no statement section maps, or two accounts with one name, " +
			"is for review.",
		Rules: "FR-COA-02, FR-COA-03", Owner: "accountant", View: viewChart, Triggers: onChartUse, Run: chartProblems},
	{Key: "setup.contacts-and-rates", Ref: "C.1 #9", Area: "Setup", Worst: Warning, Title: "Contacts and exchange rates have no problems",
		Explain: "Two contacts that look like one, by name, UEN or tax ID, split what one owes or is owed. Two current monthly " +
			"rates for one currency and month are averaged, which never sets a GST amount (ENG-42). Employees join with the " +
			"payroll link.",
		Rules: "FR-CT-04, ENG-40", Owner: "accountant", View: viewContacts, Triggers: onContacts, Run: contactsAndRates},
	{Key: "bank.lines-cannot-post", Ref: "C.1 #10", Area: "Bank", Worst: Error, Title: "Every waiting bank line can post",
		Explain: "A line waiting in the queue posts as it is coded. One dated in a closed month or one with no period, with no " +
			"exchange rate for its day, or on a money account whose ledger account is inactive cannot, whatever its coding " +
			"(rev 0.164). A line that merely waits is not a problem.",
		Rules: "FR-BNK-06, INV-03, ENG-40", Owner: "bookkeeper", View: viewBank, Triggers: onLines, Run: linesCannotPost},
	{Key: "bank.lines-in-suspense", Ref: "C.1 #11", Area: "Bank", Worst: Warning, Title: "No bank line waits in suspense",
		Explain: "A line posted to suspense (2999) is in the books but not where it belongs: classify it, or mark it reviewed " +
			"while you find out (FR-BNK-11).",
		Rules: "FR-BNK-11", Owner: "bookkeeper", View: viewBank, Triggers: onLines, Run: linesInSuspense},
	{Key: "bank.lines-in-the-future", Ref: "C.1 #12", Area: "Bank", Worst: Warning, Title: "No bank line is dated in the future",
		Explain: "A bank statement records what happened: a line booked after today is usually a scheduled payment or a date " +
			"read wrongly from the file.",
		Rules: "FR-BNK-02", Owner: "bookkeeper", View: viewBank, Triggers: onLines, Run: linesInTheFuture},
}

// accountsByID are the chart's accounts by ID.
func accountsByID(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID]storage.Account, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]storage.Account, len(accounts))
	for _, a := range accounts {
		out[a.ID] = a
	}
	return out, nil
}

// unusable says why an account cannot take postings, or "" when it can: missing, inactive, or a group.
func unusable(byID map[uuid.UUID]storage.Account, id uuid.UUID) string {
	a, ok := byID[id]
	switch {
	case !ok:
		return "an account the chart does not have"
	case !a.Active:
		return fmt.Sprintf("%s %s, which is inactive", a.Code, a.Name)
	case a.Group:
		return fmt.Sprintf("%s %s, a group that takes no postings", a.Code, a.Name)
	}
	return ""
}

// chartProblems: ✖ what posts by itself pointing at an account that cannot take postings; ⚠ an active account no
// statement section maps, or two active accounts with one name (C.1 #8).
func chartProblems(ctx context.Context, u storage.UnitOfWork, _ *Env) (Result, error) {
	byID, err := accountsByID(ctx, u)
	if err != nil {
		return Result{}, err
	}
	if len(byID) == 0 {
		return Missing("a chart of accounts", "Accounting › Chart of accounts"), nil
	}
	var items []Item
	bad := func(kind string, id uuid.UUID, label string, version int, account uuid.UUID) {
		if why := unusable(byID, account); why != "" {
			items = append(items, Item{Kind: kind, ID: id.String(), Label: label, Detail: "It points to " + why, Severity: string(Error),
				Version: fmt.Sprint(version)})
		}
	}
	rules, err := u.BankMatching().Rules(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, r := range rules {
		if !r.Active {
			continue
		}
		rule, err := banking.ReadRule(r)
		if err != nil {
			return Result{}, err
		}
		bad("bank_rule", r.ID, "Bank rule "+r.Name, r.Version, rule.Then.AccountID)
	}
	for after := ""; ; {
		page, err := u.Items().Page(ctx, after, 500)
		if err != nil {
			return Result{}, err
		}
		for _, it := range page {
			if it.Active && it.RevenueAccountID != nil {
				bad("item", it.ID, "Item "+cmp.Or(it.Name, it.Code), it.Version, *it.RevenueAccountID)
			}
		}
		if len(page) < 500 {
			break
		}
		after = page[len(page)-1].Code
	}
	categories, err := u.ClaimCategories().List(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, c := range categories {
		if c.Active {
			bad("claim_category", c.ID, "Claim category "+c.Name, c.Version, c.AccountID)
		}
	}
	monies, err := u.Banking().MoneyAccounts(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, m := range monies {
		if m.ClosedOn == nil {
			bad("money_account", m.ID, "Money account "+m.Name, m.Version, m.AccountID)
		}
	}
	err = eachContact(ctx, u, func(c storage.Contact) {
		if c.Active && c.DefaultAccountID != nil {
			bad("contact", c.ID, "Contact "+c.LegalName+"'s default category", c.Version, *c.DefaultAccountID)
		}
	})
	if err != nil {
		return Result{}, err
	}
	// ⚠: an active account that takes postings and no statement section maps; two active accounts with one name.
	names := map[string][]storage.Account{}
	var ids []uuid.UUID
	for id := range byID {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return cmp.Compare(byID[a].Code, byID[b].Code) })
	for _, id := range ids {
		a := byID[id]
		if !a.Active || a.Group {
			continue
		}
		if strings.TrimSpace(a.ReportGroup) == "" {
			items = append(items, Item{Kind: "account", ID: a.ID.String(), Label: a.Code + " " + a.Name, Severity: string(Warning),
				Detail: "No statement section maps it: give it a report group", Version: fmt.Sprint(a.Version)})
		}
		n := strings.ToLower(strings.Join(strings.Fields(a.Name), " "))
		names[n] = append(names[n], a)
	}
	var dupes []string
	for n, as := range names {
		if len(as) > 1 {
			dupes = append(dupes, n)
		}
	}
	slices.Sort(dupes)
	for _, n := range dupes {
		as := names[n]
		var codes, versions []string
		for _, a := range as {
			codes, versions = append(codes, a.Code), append(versions, fmt.Sprintf("%s@%d", a.ID, a.Version))
		}
		items = append(items, Item{Kind: "account", ID: as[0].ID.String(), Label: as[0].Name, Severity: string(Warning),
			Detail:  "Accounts " + strings.Join(codes, " and ") + " share this name: rename one so a choice of account is clear",
			Version: Fingerprint(strings.Join(versions, ","))})
	}
	if len(items) == 0 {
		return Fine("Every account that posts by itself takes postings, and every account is mapped and named once"), nil
	}
	status := Warning
	if slices.ContainsFunc(items, func(it Item) bool { return it.Severity == string(Error) }) {
		status = Error
	}
	return Found(status, items, fmt.Sprintf("%d problem(s) in the chart of accounts", len(items)),
		"Point each one named at an active account that takes postings, map or rename the accounts named"), nil
}

// eachContact calls each for every contact.
func eachContact(ctx context.Context, u storage.UnitOfWork, each func(storage.Contact)) error {
	for after := uuid.Nil; ; {
		page, err := u.Contacts().Page(ctx, after, "", 500)
		if err != nil {
			return err
		}
		for _, c := range page {
			each(c)
		}
		if len(page) < 500 {
			return nil
		}
		after = page[len(page)-1].ID
	}
}

// contactsAndRates: ⚠ active contacts that look like one by name, UEN or tax ID; ⚠ two current monthly rates for one
// currency and month (C.1 #9, ENG-40). Employees join with the payroll link.
func contactsAndRates(ctx context.Context, u storage.UnitOfWork, _ *Env) (Result, error) {
	groups := map[string][]storage.Contact{}
	err := eachContact(ctx, u, func(c storage.Contact) {
		if !c.Active {
			return
		}
		if n := strings.ToLower(strings.Join(strings.Fields(c.LegalName), " ")); n != "" {
			groups["name:"+n] = append(groups["name:"+n], c)
		}
		if id := strings.ToUpper(strings.ReplaceAll(c.UENOrTaxID, " ", "")); id != "" {
			groups["id:"+id] = append(groups["id:"+id], c)
		}
	})
	if err != nil {
		return Result{}, err
	}
	var keys []string
	for k, cs := range groups {
		if len(cs) > 1 {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var items []Item
	seen := map[string]bool{}
	for _, k := range keys {
		cs := groups[k]
		var names, versions []string
		for _, c := range cs {
			names, versions = append(names, c.LegalName), append(versions, fmt.Sprintf("%s@%d", c.ID, c.Version))
		}
		v := Fingerprint(strings.Join(versions, ","))
		if seen[v] {
			continue // the same contacts alike by name and by UEN: one item
		}
		seen[v] = true
		by := "name"
		if strings.HasPrefix(k, "id:") {
			by = "UEN or tax ID"
		}
		items = append(items, Item{Kind: "contact", ID: cs[0].ID.String(), Label: cs[0].LegalName, Version: v,
			Detail: fmt.Sprintf("%d contacts share this %s: %s. Merge them, or mark them reviewed if they are different", len(cs), by, strings.Join(names, ", "))})
	}
	rates, err := u.FXRates().List(ctx, storage.FXRateFilter{Kind: storage.FXRateMonth})
	if err != nil {
		return Result{}, err
	}
	dupes := fx.Duplicates(rates)
	var curs []string
	for cur := range dupes {
		curs = append(curs, cur)
	}
	slices.Sort(curs)
	for _, cur := range curs {
		months := dupes[cur]
		slices.SortFunc(months, func(a, b time.Time) int { return a.Compare(b) })
		for _, m := range months {
			var ids []string
			for _, r := range rates {
				if r.Currency == cur && r.WithdrawnAt == nil && r.Kind == storage.FXRateMonth && r.On.Equal(m) {
					ids = append(ids, r.ID.String())
				}
			}
			items = append(items, Item{Kind: "fx_month", ID: cur + "-" + m.Format("2006-01"), Label: fmt.Sprintf("%s, %s", cur, m.Format("January 2006")),
				Detail:  fmt.Sprintf("%d current rates in the monthly table: they are averaged, which never sets a GST amount. Withdraw all but one", len(ids)),
				Version: Fingerprint(strings.Join(ids, ","))})
		}
	}
	if len(items) == 0 {
		return Fine("No two contacts look like one, and the monthly table has one rate a currency and month"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d contact or rate problem(s) to review", len(items)),
		"Merge or rename the contacts named; withdraw the extra monthly rates in Accounting › Exchange rates"), nil
}

// waiting are the lines in the queue: unreconciled, or possible duplicates not yet kept or excluded.
func waiting(ctx context.Context, u storage.UnitOfWork) ([]storage.BankLine, error) {
	return u.Banking().Lines(ctx, storage.BankLineFilter{Statuses: []string{storage.LineUnreconciled, storage.LinePossibleDuplicate}})
}

// lineItem is an item naming a bank line.
func lineItem(l storage.BankLine, accounts map[uuid.UUID]storage.MoneyAccount, detail string) Item {
	label := l.BookingDate.Format("2 Jan 2006") + " " + string(l.Amount) + " " + l.Currency
	if m, ok := accounts[l.MoneyAccountID]; ok {
		label = m.Name + ", " + label
	}
	if d := cmp.Or(l.Counterparty, l.Description); d != "" {
		label += ", " + d
	}
	return Item{Kind: "bank_line", ID: l.ID.String(), Label: label, Detail: detail, Version: fmt.Sprint(l.Version)}
}

func moneyAccounts(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID]storage.MoneyAccount, error) {
	ms, err := u.Banking().MoneyAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]storage.MoneyAccount, len(ms))
	for _, m := range ms {
		out[m.ID] = m
	}
	return out, nil
}

// linesCannotPost: waiting lines that cannot post as they stand (C.1 #10, rev 0.164): dated in a closed month or one no
// period covers, with no exchange rate for the day, or on a money account whose ledger account is inactive.
func linesCannotPost(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	lines, err := waiting(ctx, u)
	if err != nil {
		return Result{}, err
	}
	if len(lines) == 0 {
		return Fine("No bank line waits in the queue"), nil
	}
	monies, err := moneyAccounts(ctx, u)
	if err != nil {
		return Result{}, err
	}
	byID, err := accountsByID(ctx, u)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, l := range lines {
		var why []string
		switch p := periodOf(e.Periods, l.BookingDate); {
		case p == nil:
			why = append(why, "no period covers "+l.BookingDate.Format(time.DateOnly)+": open its financial year")
		case p.Status == storage.PeriodClosed:
			why = append(why, Month(*p)+" is closed: ask to reopen it, or exclude the line")
		}
		if cur, err := money.Lookup(l.Currency); err != nil {
			why = append(why, "its currency "+l.Currency+" is not one Fyniq knows")
		} else if cur != e.Base {
			if _, err := fx.Lookup(ctx, u, e.Base, cur, l.BookingDate); err != nil {
				why = append(why, "there is no "+l.Currency+" rate for "+l.BookingDate.Format(time.DateOnly)+": enter one in Accounting › Exchange rates")
			}
		}
		if m, ok := monies[l.MoneyAccountID]; ok {
			if bad := unusable(byID, m.AccountID); bad != "" {
				why = append(why, "its money account posts to "+bad)
			}
		}
		if len(why) > 0 {
			items = append(items, lineItem(l, monies, strings.Join(why, "; ")))
		}
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("Each of the %d waiting line(s) can post once coded", len(lines))), nil
	}
	return Found(Error, items, fmt.Sprintf("%d waiting bank line(s) cannot post as they stand", len(items)),
		"Do what each line names, then code it in Banking › Categorise"), nil
}

// periodOf is the period that covers day, or nil.
func periodOf(periods []storage.Period, day time.Time) *storage.Period {
	for i := range periods {
		if !day.Before(periods[i].StartsOn) && !day.After(periods[i].EndsOn) {
			return &periods[i]
		}
	}
	return nil
}

// linesInSuspense: lines posted to suspense, waiting to be classified (C.1 #11, FR-BNK-11).
func linesInSuspense(ctx context.Context, u storage.UnitOfWork, _ *Env) (Result, error) {
	lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{Statuses: []string{storage.LineSuspense}})
	if err != nil {
		return Result{}, err
	}
	if len(lines) == 0 {
		return Fine("No bank line waits in suspense"), nil
	}
	monies, err := moneyAccounts(ctx, u)
	if err != nil {
		return Result{}, err
	}
	items := make([]Item, 0, len(lines))
	for _, l := range lines {
		items = append(items, lineItem(l, monies, "Posted to suspense: classify it"))
	}
	return Found(Warning, items, fmt.Sprintf("%d bank line(s) wait in suspense", len(items)),
		"Classify each in Banking, or mark it reviewed while you find out what it was"), nil
}

// linesInTheFuture: lines booked after the organisation's today, excluded ones aside (C.1 #12).
func linesInTheFuture(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	after := e.Today.AddDate(0, 0, 1)
	lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{From: &after})
	if err != nil {
		return Result{}, err
	}
	monies, err := moneyAccounts(ctx, u)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, l := range lines {
		if l.Status != storage.LineExcluded && l.BookingDate.After(e.Today) {
			items = append(items, lineItem(l, monies, "Booked "+l.BookingDate.Format(time.DateOnly)+", after today"))
		}
	}
	if len(items) == 0 {
		return Fine("No bank line is booked after today"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d bank line(s) booked after today", len(items)),
		"Check each against the bank: exclude a scheduled payment, or correct the file's dates and import it again"), nil
}
