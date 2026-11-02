package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/kpi"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/domain/writeoff"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D rows whose view opens the close's and the assets' items (design D7).
const (
	viewAssets = "Fixed assets"
	viewClose  = "Period close / reopen"
)

// The audit object types whose change reruns the close-backed checks (design D5): what the close reads.
var (
	onClose = []string{"journal_entry", "document", "account", "period", "period_close", "period_reopen", "money_account", "bank_line", "bank_statement",
		"bank_reconciliation", "bank_feed", "recurring_journal", "recurring_bill", "period_variance_explanation", "close_checklist_entry",
		"close_checklist_template", "organisation_setting", "fx_rate", "allocation"}
	onRunway = []string{"journal_entry", "period", "period_close", "period_reopen", "account"}
)

// The thresholds of slice 3's own checks.
const (
	unclosedAfter = 15            // days after a month's end it is to be closed by (C.2 #18)
	syncStale     = 3 * time.Hour // a feed not synced within it is late: the hourly bank.sync's own Stale (C.2 #43)
	runwayMonths  = "6"           // months of runway below which it warns (C.1 #36)
)

// closeBacked is a check that is one or more of the period close's own checks (design D2): the close's findings of its
// keys for the period to close next, each an item naming the month, at the close's own severity.
func closeBacked(keys ...string) func(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	return closeFindings(false, keys)
}

// atMonthEnd is closeBacked for the close's checks of a month's end (C.2 #56 to #59): the month the books are in has not
// ended, so they wait for it.
func atMonthEnd(keys ...string) func(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	return closeFindings(true, keys)
}

func closeFindings(ended bool, keys []string) func(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	return func(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
		if p := e.ToClose(); ended && p != nil && !p.EndsOn.Before(e.Today) {
			return Fine(Month(*p) + " has not ended: this is checked once it has"), nil
		}
		found, p, err := e.Close(ctx, u, keys...)
		if err != nil {
			return Result{}, err
		}
		if p == nil {
			return noMonth(), nil
		}
		var items []Item
		errs := 0
		var fine []string
		for _, c := range found {
			switch c.Severity {
			case closing.OK:
				if c.Detail != "" {
					fine = append(fine, c.Title+": "+c.Detail)
				} else {
					fine = append(fine, c.Title)
				}
				continue
			case closing.Failed:
				errs++
			}
			sev := Warning
			if c.Severity == closing.Failed {
				sev = Error
			}
			it := PeriodItem(*p, c.Title+": "+c.Detail)
			it.Kind, it.ID = "close", p.ID.String()+"/"+c.Key // one month can carry several of the close's findings
			it.Severity = string(sev)
			items = append(items, it)
		}
		if len(items) == 0 {
			words := "Nothing found for " + Month(*p)
			if len(fine) > 0 {
				words = Month(*p) + ": " + strings.Join(fine, "; ")
			}
			return Fine(words), nil
		}
		status := Warning
		if errs > 0 {
			status = Error
		}
		return Found(status, items, fmt.Sprintf("%d finding(s) for %s", len(items), Month(*p)),
			"Open the month's close in Accounting › Periods and close, and do what each finding says"), nil
	}
}

// The checks of the bank, the assets, the close and the runway: p1-checks slice 3 (tasks 3.1 to 3.5).
var closeChecks = []Definition{
	{Key: "assets.register", Ref: "C.1 #22", Area: "Assets", Worst: Error, Title: "The asset register agrees with its accounts",
		Explain: "The register's costs and accumulated depreciation equal the asset accounts (INV-07); an amount on an asset " +
			"account no asset supports, or a purchase there not in the register, is for review.",
		Rules: "INV-07, FR-FA-01, FR-FA-02", Owner: "accountant", View: viewAssets, Triggers: onClose,
		CloseKeys: []string{"assets", "assets-unsupported", "assets-unregistered"}, Run: closeBacked("assets", "assets-unsupported", "assets-unregistered")},
	{Key: "ledger.sub-ledgers", Ref: "C.2 #2", Area: "Ledger", Worst: Error, Title: "Each sub-ledger agrees with its control account",
		Explain: "What customers owe, what is owed to suppliers, the deposits held, the prepayments and the deferred revenue " +
			"equal their control accounts (INV-07); an amount there no document supports is for review. The fixed assets are " +
			"C.1 #22's; payroll and GST join with their modules.",
		Rules: "INV-07, FR-CTL-05", Owner: "controller", View: viewReports, Triggers: onClose,
		CloseKeys: subLedgerKeys, Run: closeBacked(subLedgerKeys...)},
	{Key: "recurring.due", Ref: "C.1 #28", Area: "Recurring", Worst: Error, Title: "Every recurring entry due is made",
		Explain: "A recurring journal or bill whose occurrence is due has its draft; one more than a day late means the hourly " +
			"scheduler failed, which refuses the close.",
		Rules: "FR-AUT-01, FR-AP-09", Owner: "controller", View: viewJournals, Triggers: onClose,
		CloseKeys: []string{"recurring", "recurring-bills"}, Run: closeBacked("recurring", "recurring-bills")},
	{Key: "recurring.content", Ref: "C.1 #29", Area: "Recurring", Worst: Warning, Title: "Every recurring template can post",
		Explain: "An active recurring journal or bill whose content the posting rules refuse makes drafts that cannot post: fix " +
			"it or pause it (rev 0.61).",
		Rules: "FR-AUT-01, FR-AP-09", Owner: "accountant", View: viewJournals, Triggers: onClose,
		CloseKeys: []string{"recurring-content", "recurring-bills-content"}, Run: closeBacked("recurring-content", "recurring-bills-content")},
	{Key: "bank.suspense", Ref: "C.1 #30", Area: "Bank", Worst: Warning, Title: "Suspense is at nothing at the month end",
		Explain: "The suspense account (2999) holds what is not yet classified: at the month end it should hold nothing.",
		Rules:   "FR-BNK-11", Owner: "bookkeeper", View: viewBank, Triggers: onClose, CloseKeys: []string{"suspense"}, Run: closeBacked("suspense")},
	{Key: "bank.below-zero", Ref: "C.1 #31", Area: "Bank", Worst: Warning, Title: "No bank or cash account is below zero at the month end",
		Explain: "A bank or cash account below zero at the month end usually means a missing receipt, or a line coded the " +
			"wrong way.",
		Rules: "FR-BNK-10", Owner: "bookkeeper", View: viewBank, Triggers: onClose, CloseKeys: []string{"bank-negative"}, Run: closeBacked("bank-negative")},
	{Key: "bank.reconciliations-match", Ref: "C.1 #32", Area: "Bank", Worst: Warning, Title: "Every signed reconciliation matches",
		Explain: "A reconciliation signed off with a difference left needs the difference explained, or what is missing found.",
		Rules:   "FR-BNK-10", Owner: "bookkeeper", View: viewBank, Triggers: onClose, CloseKeys: []string{"bank-unmatched"}, Run: closeBacked("bank-unmatched")},
	{Key: "bank.feeds-stale", Ref: "C.2 #8", Area: "Bank", Worst: Warning, Title: "Bank data is up to date",
		Explain: "An account fed by emailed reports or a connector that has had nothing for more than 3 days may have stopped.",
		Rules:   "FR-BNK-15, FR-BNK-17", Owner: "bookkeeper", View: viewBank, Triggers: onClose, CloseKeys: []string{"bank-stale"}, Run: closeBacked("bank-stale")},
	{Key: "bank.lines-waiting", Ref: "C.2 #9", Area: "Bank", Worst: Warning, Title: "No bank line has waited more than 30 days",
		Explain: "A line in the queue for more than 30 days is easy to forget: code it, match it or exclude it.",
		Rules:   "FR-BNK-06", Owner: "bookkeeper", View: viewBank, Triggers: onClose, CloseKeys: []string{"bank-old-lines"}, Run: closeBacked("bank-old-lines")},
	{Key: "bank.duplicates", Ref: "C.2 #51", Area: "Bank", Worst: Warning, Title: "No possible duplicate bank line waits",
		Explain: "A line marked a possible duplicate when it arrived waits for a person to keep it or exclude it.",
		Rules:   "FR-BNK-04, FR-BNK-16", Owner: "bookkeeper", View: viewBank, Triggers: onClose, CloseKeys: []string{"bank-duplicates"}, Run: closeBacked("bank-duplicates")},
	{Key: "bank.sync", Ref: "C.2 #43", Area: "Bank", Worst: Warning, Title: "Bank data syncs on schedule",
		Explain: "A connector's sync that failed, or has not run within its schedule, leaves the account's lines behind. The " +
			"payment providers' completeness sweep joins with them.",
		Rules: "FR-BNK-15", Owner: "bookkeeper", View: viewBank, Triggers: []string{"bank_feed", "money_account"}, Run: syncsOnSchedule},
	{Key: "bank.settlements", Ref: "C.2 #49", Area: "Bank", Worst: Warning, Title: "Settlements stay within their caps and their rules",
		Explain: "Settlement differences cleared to 7030 in the month stay under the monthly cap (FR-BNK-08); a write-off is " +
			"never approved by the person who recorded a receipt of its invoice (SOD-9).",
		Rules: "FR-BNK-08, SOD-9", Owner: "controller", View: viewInvoices, Triggers: onClose, CloseKeys: []string{"tolerances"}, Run: settlements},
	{Key: "close.reversals", Ref: "C.2 #56", Area: "Closing", Worst: Warning, Title: "No automatic reversal waits",
		Explain: "A journal that reverses on or before the month's end, or one dated in the month that waits for a period to " +
			"open, cannot reverse once the month closes.",
		Rules: "FR-GL-02", Owner: "controller", View: viewClose, Triggers: onClose, CloseKeys: []string{"reversals"}, Run: atMonthEnd("reversals")},
	{Key: "close.variances", Ref: "C.2 #57", Area: "Closing", Worst: Error, Title: "Every large variance is explained",
		Explain: "An income or expense account that moved against the month before by more than both the variance amount and " +
			"percentage needs its explanation before the close (FR-GL-14).",
		Rules: "FR-GL-14", Owner: "controller", View: viewClose, Triggers: onClose, CloseKeys: []string{"variances"}, Run: atMonthEnd("variances")},
	{Key: "close.checklist", Ref: "C.2 #58", Area: "Closing", Worst: Error, Title: "The close checklist is finished",
		Explain: "Each task of the month's close checklist is done, or skipped with a reason, before the close (FR-GL-07).",
		Rules:   "FR-GL-07", Owner: "controller", View: viewClose, Triggers: onClose, CloseKeys: []string{"checklist"}, Run: atMonthEnd("checklist")},
	{Key: "close.revaluation", Ref: "C.2 #59", Area: "Closing", Worst: Error, Title: "Foreign-currency balances are revalued for the close",
		Explain: "An account marked revalue with a foreign-currency balance at the month's end needs the month's revaluation " +
			"after its last posting; the check names a currency with no rate for the last day (FR-GL-04).",
		Rules: "FR-GL-04", Owner: "controller", View: viewClose, Triggers: onClose, CloseKeys: []string{"revaluation"}, Run: atMonthEnd("revaluation")},
	{Key: "close.closed-unchanged", Ref: "C.1 #33", Area: "Closing", Worst: Error, Title: "No closed month has changed since its close",
		Explain: "A closed month's trial balance equals the one recorded when it closed. The database refuses a posting into a " +
			"closed month, so a difference means its lock was bypassed: report it.",
		Rules: "INV-03, AUD-04", Owner: "controller", View: viewClose, Triggers: []string{"journal_entry", "period_close", "period_reopen"}, Run: closedUnchanged},
	{Key: "close.revalued", Ref: "C.2 #17", Area: "FX", Worst: Warning, Title: "Foreign-currency items were revalued at the last month end",
		Explain: "The foreign-currency balances of the accounts marked revalue are restated at the last month end's rate once " +
			"its last posting is in; until then the balance sheet shows them at old rates.",
		Rules: "FR-GL-04", Owner: "accountant", View: viewClose, Triggers: onClose, Run: revaluedLastMonthEnd},
	{Key: "close.month-unclosed", Ref: "C.2 #18", Area: "Closing", Worst: Warning, Title: "No month stays open more than 15 days after its end",
		Explain: "A month still open 15 days after it ended keeps its figures provisional and open to late postings.",
		Rules:   "FR-GL-07", Owner: "controller", View: viewClose, Triggers: []string{"period", "period_close", "period_reopen"}, Run: monthsUnclosed},
	{Key: "planning.runway", Ref: "C.1 #36", Area: "Planning", Worst: Warning, Title: "The runway is 6 months or more",
		Explain: "The cash at the last month end lasts fewer than 6 months at the average net burn of the last three months: " +
			"time to raise, cut or plan. Not burning passes.",
		Rules: "FR-KPI-02", Owner: "owner", View: viewReports, Triggers: onRunway, Run: runway},
}

// subLedgerKeys are the close's checks of the sub-ledgers against their control accounts (INV-07; C.2 #2).
var subLedgerKeys = []string{"payables", "payables-unsupported", "receivables", "receivables-unsupported", "deposits", "deposits-unsupported",
	"prepayments", "prepayments-unsupported", "prepayments-unscheduled", "deferred-revenue", "deferred-revenue-unsupported", "deferred-revenue-unscheduled"}

// syncsOnSchedule: a connector's feed whose last sync failed, or that has not synced within its schedule (C.2 #43).
func syncsOnSchedule(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	feeds, err := u.Banking().Feeds(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(feeds) == 0 {
		return Fine("No money account is fed by a connector"), nil
	}
	monies, err := moneyAccounts(ctx, u)
	if err != nil {
		return Result{}, err
	}
	var items []Item
	for _, f := range feeds {
		m, ok := monies[f.MoneyAccountID]
		if ok && m.ClosedOn != nil {
			continue
		}
		var why string
		switch {
		case f.LastError != "":
			why = "Its last sync failed: " + f.LastError
		case f.SyncedAt.Before(e.Now.Add(-syncStale)):
			why = "Not synced since " + f.SyncedAt.UTC().Format("2 Jan 2006 15:04") + " UTC: the hourly sync is late"
		default:
			continue
		}
		items = append(items, Item{Kind: "money_account", ID: f.MoneyAccountID.String(), Label: m.Name, Detail: why, Version: fmt.Sprint(f.Version)})
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("Each of the %d connected account(s) synced on schedule", len(feeds))), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d connected account(s) behind", len(items)),
		"Check the connector in Settings › Integrations, then sync the account again"), nil
}

// settlements: the close's monthly cap on settlement differences (FR-BNK-08), and a posted write-off of the open months
// posted by someone who recorded a receipt of its invoice (SOD-9), which the approval refuses (WRITEOFF-11): a backstop.
func settlements(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	capped, err := closeBacked("tolerances")(ctx, u, e)
	if err != nil {
		return Result{}, err
	}
	items := slices.Clone(capped.Items)
	if from, to, ok := e.openSpan(); ok {
		offs, err := documents(ctx, u, []storage.DocumentType{storage.DocumentWriteOff}, storage.DocumentPosted, from, to)
		if err != nil {
			return Result{}, err
		}
		for _, doc := range offs {
			d, err := writeoff.Read(doc)
			if err != nil {
				return Result{}, err
			}
			allocs, err := u.Allocations().OfDocument(ctx, d.InvoiceID)
			if err != nil {
				return Result{}, err
			}
			for _, a := range allocs {
				src, err := u.Documents().Get(ctx, a.SourceID)
				if err != nil {
					return Result{}, err
				}
				if src.Type == storage.DocumentCustomerReceipt && src.PostedBy != "" && src.PostedBy == doc.PostedBy {
					items = append(items, docItem("write_off", doc, "", fmt.Sprintf("Posted by the person who recorded receipt %s of its invoice (SOD-9)", src.Number)))
					break
				}
			}
		}
	}
	if len(items) == 0 {
		if capped.Status == NotAssessed {
			return capped, nil
		}
		return Fine(capped.Detail), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d settlement finding(s) to review", len(items)),
		"Review the settlement differences of the month, and have another person review each write-off named"), nil
}

// closedUnchanged compares each closed month's trial balance with the one its close recorded (C.1 #33).
func closedUnchanged(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	var items []Item
	closed := 0
	for _, p := range e.Periods {
		if p.Status != storage.PeriodClosed {
			continue
		}
		closed++
		c, err := u.Closes().Latest(ctx, p.ID)
		if errors.Is(err, ports.ErrNotFound) {
			continue // closed before the close recorded its snapshot
		}
		if err != nil {
			return Result{}, err
		}
		var snap closing.Snapshot
		if err := json.Unmarshal(c.Snapshot, &snap); err != nil {
			return Result{}, err
		}
		tb, err := balance.Position(ctx, u, e.Base, e.Periods, p)
		if err != nil {
			return Result{}, err
		}
		was := map[string]string{}
		for _, a := range snap.Accounts {
			was[a.AccountID] = a.Balance
		}
		var changed []string
		for _, r := range tb.Rows {
			before, err := money.ParseStored(cmpOr(was[r.AccountID.String()], "0"), e.Base)
			if err != nil {
				return Result{}, err
			}
			if !before.Equal(r.Base) {
				changed = append(changed, r.AccountID.String())
			}
			delete(was, r.AccountID.String())
		}
		for id, v := range was {
			if m, err := money.ParseStored(v, e.Base); err == nil && !m.IsZero() {
				changed = append(changed, id)
			}
		}
		if len(changed) > 0 {
			slices.Sort(changed)
			items = append(items, PeriodItem(p, fmt.Sprintf("%d account(s) differ from the trial balance recorded at its close: report it", len(changed))))
		}
	}
	if closed == 0 {
		return Fine("No month is closed yet"), nil
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("Each of the %d closed month(s) equals its close", closed)), nil
	}
	return Found(Error, items, fmt.Sprintf("%d closed month(s) changed since their close", len(items)),
		"Report it: the database's lock on closed months was bypassed"), nil
}

func cmpOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// lastEnded is the latest period that has ended by today, or nil.
func (e *Env) lastEnded() *storage.Period {
	var out *storage.Period
	for i := range e.Periods {
		if e.Periods[i].EndsOn.Before(e.Today) {
			out = &e.Periods[i]
		}
	}
	return out
}

// revaluedLastMonthEnd: the last month that has ended, if open, needs its revaluation (C.2 #17); a closed one was
// revalued for its close.
func revaluedLastMonthEnd(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	p := e.lastEnded()
	if p == nil {
		return Missing("a month that has ended", "Accounting › Periods"), nil
	}
	if p.Status == storage.PeriodClosed {
		return Fine(Month(*p) + " is closed: its revaluation was checked at its close"), nil
	}
	plan, err := fx.Prepare(ctx, u, p.ID, journal.SubLedgers())
	if err != nil {
		return Result{}, err
	}
	if !plan.Due() {
		return Fine("Nothing in a foreign currency is waiting to be revalued at the end of " + Month(*p)), nil
	}
	words := fmt.Sprintf("%d foreign-currency item(s) are not restated at the rate of %s", len(plan.Items), p.EndsOn.Format(time.DateOnly))
	if len(plan.Missing) > 0 {
		var curs []string
		for _, m := range plan.Missing {
			curs = append(curs, m.Currency)
		}
		words += "; no rate yet for " + strings.Join(curs, ", ")
	}
	it := PeriodItem(*p, words)
	return Found(Warning, []Item{it}, "The foreign-currency items of "+Month(*p)+" are not revalued",
		"Revalue them in Accounting › Exchange rates, or wait for the daily revaluation once the month's rates are in"), nil
}

// monthsUnclosed: months still open more than 15 days after they ended (C.2 #18).
func monthsUnclosed(_ context.Context, _ storage.UnitOfWork, e *Env) (Result, error) {
	var items []Item
	limit := e.Today.AddDate(0, 0, -unclosedAfter)
	for _, p := range e.Periods {
		if p.Status != storage.PeriodClosed && p.EndsOn.Before(limit) {
			days := int(e.Today.Sub(p.EndsOn).Hours() / 24)
			it := PeriodItem(p, fmt.Sprintf("Ended %s, %d days ago, and still open", p.EndsOn.Format(time.DateOnly), days))
			it.Version = p.StartsOn.Format("2006-01") // its review holds until the month closes
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("Every month that ended more than %d days ago is closed", unclosedAfter)), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d month(s) open more than %d days after their end", len(items), unclosedAfter),
		"Close each in Accounting › Periods and close, earliest first"), nil
}

// runway is the KPI's runway at the last month end (C.1 #36, FR-KPI-02): under 6 months warns; Not burning passes.
func runway(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	p := e.lastEnded()
	if p == nil {
		return Missing("a month that has ended", "Accounting › Periods"), nil
	}
	last := slices.IndexFunc(e.Periods, func(x storage.Period) bool { return x.ID == p.ID })
	start := e.Periods[0].StartsOn
	if cal, err := period.CalendarOf(e.Org.Profile); err == nil {
		start = cal.Year(max(cal.Number(p.StartsOn), 1)).StartsOn
	}
	months, before, err := kpi.Read(ctx, u, e.Base, e.Periods, last, start, e.Revision)
	if err != nil {
		return Result{}, err
	}
	i := slices.IndexFunc(kpi.Compute(months, before, e.Base), func(v kpi.Value) bool { return v.Key == "runway" })
	if i < 0 {
		return Result{}, errors.New("the KPI set has no runway")
	}
	v := kpi.Compute(months, before, e.Base)[i]
	switch {
	case v.Undefined != "":
		return Missing(v.Undefined, "Reports › Start-up KPIs"), nil
	case v.Words != "":
		return Fine(fmt.Sprintf("%s at the end of %s", v.Words, Month(*p))), nil
	}
	got, _, err := apd.NewFromString(v.Figure)
	if err != nil {
		return Result{}, err
	}
	limit, _, _ := apd.NewFromString(runwayMonths)
	if got.Cmp(limit) >= 0 {
		return Fine(fmt.Sprintf("%s months at the end of %s", v.Figure, Month(*p))), nil
	}
	it := PeriodItem(*p, fmt.Sprintf("%s months of runway at the average net burn of the last three months", v.Figure))
	return Found(Warning, []Item{it}, fmt.Sprintf("The runway is %s months at the end of %s", v.Figure, Month(*p)),
		"Plan the next raise or cuts; mark it reviewed once the plan is agreed"), nil
}
