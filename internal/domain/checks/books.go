package checks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D rows whose view opens the books' items (design D7).
const (
	viewReports  = "Reports, dashboards"
	viewChart    = "Chart of accounts, dimensions"
	viewJournals = "Journals"
	viewAudit    = "Audit trail"
)

// The audit object types whose change reruns the books' checks (design D5).
var (
	onPostings = []string{"journal_entry", "account", "period", "period_close", "period_reopen", "money_account"}
	onChart    = []string{"account"}
	onOpening  = []string{"document", "organisation", "journal_entry"}
)

// The checks of the books, the setup, the ledger and the audit trail: p1-checks slice 1 (task 1.6).
var booksChecks = []Definition{
	{Key: "books.debits-equal-credits", Ref: "C.1 #1", Area: "Books", Worst: Error, Title: "Debits equal credits",
		Explain: "Every entry's debits equal its credits, so each month's trial balance nets to zero. The database refuses an entry " +
			"that does not balance, so a month that does not means something went around it: report it.",
		Rules: "INV-01, INV-09", Owner: "controller", View: viewReports, Triggers: onPostings, CloseKeys: []string{"trial-balance"},
		Run: debitsEqualCredits},
	{Key: "books.balance-sheet-balances", Ref: "C.1 #2", Area: "Books", Worst: Error, Title: "The balance sheet balances every month",
		Explain: "Each month's assets equal its liabilities and equity, with the year's profit so far. A month that does not means " +
			"the books do not balance.",
		Rules: "INV-10, BS_Chk", Owner: "controller", View: viewReports, Triggers: onPostings,
		Run: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
			return eachMonth(ctx, u, e, financials.BalanceSheet, "balances", Error, "The balance sheet balances in every open month",
				"month(s) whose balance sheet does not balance", "Report it: the ledger's own checks should have refused this")
		}},
	{Key: "books.cash-flow-ties", Ref: "C.1 #3", Area: "Books", Worst: Error, Title: "The cash flow agrees to the bank balances",
		Explain: "Each month's cash flow adds up to the change in the bank, card and cash accounts. A month that does not means an " +
			"account's cash-flow line places its movement wrongly.",
		Rules: "INV-11, CF_Chk", Owner: "controller", View: viewReports, Triggers: onPostings, CloseKeys: []string{"statements-ties"},
		Run: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
			return eachMonth(ctx, u, e, financials.CashFlow, "ties", Error, "The cash flow ties to the money accounts in every open month",
				"month(s) whose cash flow does not tie to the money accounts", "Check the cash-flow line of each account in the chart of accounts")
		}},
	{Key: "books.pl-includes-every-account", Ref: "C.1 #4", Area: "Books", Worst: Error, Title: "The P&L includes every account",
		Explain: "Each month's net profit equals the ledger's income less its expenses for the month, so no income or expense " +
			"account is left out of the P&L.",
		Rules: "INV-10, PL_Chk", Owner: "controller", View: viewReports, Triggers: onPostings, Run: plIncludesEveryAccount},
	{Key: "books.balance-sheet-includes-every-account", Ref: "C.1 #5", Area: "Books", Worst: Warning,
		Title: "The balance sheet includes every account",
		Explain: "Every account with a balance sits in a section of the balance sheet and a line of the cash flow. The chart allows " +
			"an account without a cash-flow line, so one is named for review (rev 0.163).",
		Rules: "INV-10, BS_Uncl", Owner: "accountant", View: viewChart, Triggers: onPostings, CloseKeys: []string{"statements", "statements-cash"},
		Run: balanceSheetIncludesEveryAccount},
	{Key: "setup.opening-balances-balance", Ref: "C.1 #6", Area: "Setup", Worst: Error, Title: "The opening balances balance",
		Explain: "The opening balances' debits equal their credits. They post only once they do, so a draft that does not keeps " +
			"the books from starting where they should.",
		Rules: "FR-GL-06, INV-01", Owner: "accountant", View: viewJournals, Triggers: onOpening, Run: openingBalancesBalance},
	{Key: "setup.system-accounts-exist", Ref: "C.1 #7", Area: "Setup", Worst: Error, Title: "The system accounts exist",
		Explain: "Fyniq posts to the system accounts by their role, such as accounts receivable or the GST control account. A role " +
			"with no active account leaves those postings nowhere to go.",
		Rules: "FR-COA-03, B.5", Owner: "controller", View: viewChart, Triggers: onChart, Run: systemAccountsExist},
	{Key: "books.equity-movement-reconciles", Ref: "C.1 #23", Area: "Books", Worst: Error, Title: "The movement in equity reconciles",
		Explain: "Each month's equity moves from its opening to its closing position by the profit and the changes the cash flow " +
			"shows. A month that does not means an equity account's movement is unexplained.",
		Rules: "INV-11, BS_EqChk", Owner: "controller", View: viewReports, Triggers: onPostings, CloseKeys: []string{"statements-equity"},
		Run: func(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
			return eachMonth(ctx, u, e, financials.CashFlow, "equity", Error, "The movement in equity reconciles in every open month",
				"month(s) whose movement in equity does not reconcile", "Check the cash-flow line of each equity account in the chart of accounts")
		}},
	{Key: "ledger.balances-match-lines", Ref: "C.2 #1", Area: "Ledger", Worst: Error, Title: "The balances equal their lines",
		Explain: "The balances Fyniq keeps for speed equal a recomputation from the journal lines. A difference means they were " +
			"changed outside the posting service: report it, never rebuild them unseen.",
		Rules: "INV-08, CTL-FIN-01", Owner: "controller", View: viewReports, CloseKeys: []string{"balances"}, Run: balancesMatchLines},
	{Key: "audit.chain-intact", Ref: "C.2 #3", Area: "Audit", Worst: Error, Title: "The audit trail is intact and anchored",
		Explain: "Each night the audit trail's hash chain is checked from its first event, and against the signed anchors kept " +
			"outside the database. A break means an event was changed or removed.",
		Rules: "INV-12, AUD-03", Owner: "owner", View: viewAudit, Triggers: []string{"audit_chain"}, Run: auditChainIntact},
}

// statement is a month's statement of kind, laid out once per run however many checks read it (design D3).
func (e *Env) statement(ctx context.Context, u storage.UnitOfWork, kind financials.Kind, i int) (financials.Statement, error) {
	key := fmt.Sprintf("%s/%d", kind, i)
	if s, ok := e.statements[key]; ok {
		return s, nil
	}
	s, err := financials.Lay(ctx, u, e.Base, financials.Request{Kind: kind, All: e.Periods, First: i, Last: i, Revision: e.Revision})
	if err != nil {
		return s, err
	}
	if e.statements == nil {
		e.statements = map[string]financials.Statement{}
	}
	e.statements[key] = s
	return s, nil
}

// started are the indexes in e.Periods of the open periods that have started.
func (e *Env) started() []int {
	var out []int
	for i, p := range e.Periods {
		if p.Status != storage.PeriodClosed && !p.StartsOn.After(e.Today) {
			out = append(out, i)
		}
	}
	return out
}

// noMonth is the not-assessed result of a month check with no open month that has started.
func noMonth() Result {
	return Missing("an open month that has started", "Accounting › Periods")
}

// eachMonth runs a statement's check in each open month that has started: an item for each month where it fails, with the
// statement's words.
func eachMonth(ctx context.Context, u storage.UnitOfWork, e *Env, kind financials.Kind, check string, status Status, fine, found, fix string) (Result, error) {
	months := e.started()
	if len(months) == 0 {
		return noMonth(), nil
	}
	var items []Item
	for _, i := range months {
		s, err := e.statement(ctx, u, kind, i)
		if err != nil {
			return Result{}, err
		}
		for _, c := range s.Checks {
			if c.Key == check && !c.OK {
				items = append(items, PeriodItem(e.Periods[i], c.Detail))
			}
		}
	}
	if len(items) == 0 {
		return Fine(fine), nil
	}
	return Found(status, items, fmt.Sprintf("%d %s", len(items), found), fix), nil
}

// debitsEqualCredits reads each open month's trial balance at its end from the balance service, as the close's trial-balance
// check does (C.1 #1, INV-01, INV-09): a month whose balances do not net to zero.
func debitsEqualCredits(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	months := e.started()
	if len(months) == 0 {
		return noMonth(), nil
	}
	var items []Item
	for _, i := range months {
		tb, err := balance.Position(ctx, u, e.Base, e.Periods, e.Periods[i])
		if err != nil {
			return Result{}, err
		}
		if !tb.Total.IsZero() {
			items = append(items, PeriodItem(e.Periods[i], fmt.Sprintf("It is off by %s %s", tb.Total, e.Base.Code)))
		}
	}
	if len(items) == 0 {
		return Fine("The trial balance of every open month nets to zero"), nil
	}
	return Found(Error, items, fmt.Sprintf("%d month(s) whose trial balance does not net to zero", len(items)),
		"Report it: the ledger's own checks should have refused this"), nil
}

// plIncludesEveryAccount compares each open month's net profit in the P&L with the ledger's income less expenses for the
// month, account by account (C.1 #4, PL_Chk): they differ only when an income or expense account sits outside the P&L.
func plIncludesEveryAccount(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	months := e.started()
	if len(months) == 0 {
		return noMonth(), nil
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return Result{}, err
	}
	pl := map[string]bool{}
	for _, a := range accounts {
		pl[a.ID.String()] = a.Type == storage.Revenue || a.Type == storage.Expense
	}
	var items []Item
	for _, i := range months {
		p := e.Periods[i]
		s, err := e.statement(ctx, u, financials.ProfitAndLoss, i)
		if err != nil {
			return Result{}, err
		}
		shown := money.Zero(e.Base)
		for _, b := range s.Blocks {
			if b.Key == financials.KeyNetProfit && len(b.Totals) > 0 {
				shown = b.Totals[0]
			}
		}
		tb, err := balance.TrialAt(ctx, u, e.Base, balance.At(e.Revision), p.ID)
		if err != nil {
			return Result{}, err
		}
		// Net profit is income less expenses: the ledger's credits less debits on those accounts, so it and their debit-positive
		// total add up to zero.
		sum := shown
		for _, r := range tb.Rows {
			if pl[r.AccountID.String()] {
				if sum, err = sum.Add(r.Base); err != nil {
					return Result{}, err
				}
			}
		}
		if !sum.IsZero() {
			items = append(items, PeriodItem(p, fmt.Sprintf("The P&L's net profit of %s %s differs from the ledger's by %s", shown, e.Base.Code, sum)))
		}
	}
	if len(items) == 0 {
		return Fine("The P&L includes every income and expense account in every open month"), nil
	}
	return Found(Error, items, fmt.Sprintf("%d month(s) whose P&L leaves out an income or expense account", len(items)),
		"Report it: every income and expense account belongs in the P&L by its type"), nil
}

// balanceSheetIncludesEveryAccount names, for each open month, the accounts with a balance that sit in no section of the
// balance sheet or line of the cash flow (C.1 #5): ⚠, as the close finds it (rev 0.163).
func balanceSheetIncludesEveryAccount(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	months := e.started()
	if len(months) == 0 {
		return noMonth(), nil
	}
	var items []Item
	for _, i := range months {
		var found []string // the balance sheet's words, then the cash flow's: one item for the month
		for _, kind := range []financials.Kind{financials.BalanceSheet, financials.CashFlow} {
			s, err := e.statement(ctx, u, kind, i)
			if err != nil {
				return Result{}, err
			}
			for _, c := range s.Checks {
				if c.Key == "placed" && !c.OK {
					found = append(found, c.Title+": "+c.Detail)
				}
			}
		}
		if len(found) > 0 {
			items = append(items, PeriodItem(e.Periods[i], strings.Join(found, "; ")))
		}
	}
	if len(items) == 0 {
		return Fine("Every account with a balance sits in the balance sheet and the cash flow"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d month(s) with accounts missing from the balance sheet or the cash flow", len(items)),
		"Give each account named a cash-flow line of the standard chart in the chart of accounts"), nil
}

// openingBalancesBalance: draft opening balances that do not net to zero (C.1 #6). Posted ones balance (INV-01).
func openingBalancesBalance(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	doc, err := u.Documents().OpeningBalance(ctx)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return Fine("No opening balances are entered"), nil
	case err != nil:
		return Result{}, err
	case doc.Status != storage.DocumentDraft:
		return Fine("The opening balances are posted, so they balance"), nil
	}
	saved, err := journal.ReadOpening(doc)
	if err != nil {
		return Result{}, err
	}
	total := money.Zero(e.Base)
	for _, l := range saved.Lines {
		if l.Amount.Currency() != e.Base {
			continue // the draft is in the base currency; posting refuses a line in another
		}
		if total, err = total.Add(l.Amount); err != nil {
			return Result{}, err
		}
	}
	if total.IsZero() {
		return Fine("The draft opening balances net to zero"), nil
	}
	if total.Sign() < 0 {
		total = total.Neg()
	}
	item := Item{Kind: "opening_balances", ID: doc.ID.String(), Label: "Opening balances",
		Detail: fmt.Sprintf("The debits and credits differ by %s %s", total, e.Base.Code)}
	return Found(Error, []Item{item}, "The draft opening balances do not net to zero, so they cannot post",
		"Add the line that balances them, often to retained earnings, in Accounting › Opening balances"), nil
}

// systemAccountsExist: every role of B.5's system accounts on an active account (C.1 #7).
func systemAccountsExist(ctx context.Context, u storage.UnitOfWork, _ *Env) (Result, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(accounts) == 0 {
		return Missing("a chart of accounts", "Accounting › Chart of accounts"), nil
	}
	held := map[storage.AccountRole]bool{}
	for _, a := range accounts {
		if a.Active && a.Role != "" {
			held[a.Role] = true
		}
	}
	var items []Item
	for _, r := range storage.SystemRoles {
		if !held[r] {
			items = append(items, Item{Kind: "role", ID: string(r), Label: strings.ReplaceAll(string(r), "-", " "),
				Detail: "No active account has this role"})
		}
	}
	if len(items) == 0 {
		return Fine(fmt.Sprintf("Each of the %d system roles is on an active account", len(storage.SystemRoles))), nil
	}
	return Found(Error, items, fmt.Sprintf("%d system role(s) on no active account", len(items)),
		"Load the standard chart's missing accounts in Accounting › Chart of accounts, or ask support to add the role"), nil
}

// balancesMatchLines recomputes the balances from the journal lines (C.2 #1, INV-08), as the nightly verification and the
// close do: it runs nightly and on request, not on each change.
func balancesMatchLines(ctx context.Context, u storage.UnitOfWork, _ *Env) (Result, error) {
	diffs, err := balance.Differences(ctx, u)
	if err != nil {
		return Result{}, err
	}
	if diffs == 0 {
		return Fine("Every balance equals its recomputation from the journal lines"), nil
	}
	r := Found(Error, nil, fmt.Sprintf("%d balance(s) differ from their journal lines", diffs), "Report it: never rebuild the balances unseen")
	r.Count = diffs
	return r, nil
}

// actionVerified is the audit event of the audit trail's nightly verification (audit.VerifyAll).
const actionVerified = audit.ActionVerified

// auditChainIntact reads the latest outcome of the nightly verification of the audit trail (C.2 #3, design D6): it does
// not verify the chain itself.
func auditChainIntact(_ context.Context, _ storage.UnitOfWork, e *Env) (Result, error) {
	ev, ok := e.Latest[actionVerified]
	if !ok {
		if _, ran := e.Prior["audit.chain-intact"]; ran {
			return Result{Status: Unchanged}, nil
		}
		return Missing("the audit trail's nightly verification (audit.verify), which has not run since the checks began",
			"the platform's schedule: it runs at 02:00 Singapore time"), nil
	}
	status, _ := ev.Changes["status"][1].(string)
	problem, _ := ev.Changes["problem"][1].(string)
	when := strings.TrimSuffix(ev.OccurredAt, "Z")
	if status == "passed" {
		return Fine("The audit trail's hash chain and anchors verified at " + when), nil
	}
	item := Item{Kind: "audit_chain", Label: "Audit trail", Detail: problem}
	return Found(Error, []Item{item}, "The audit trail failed its verification at "+when,
		"Treat it as an incident: follow the audit-chain runbook and tell the Owner"), nil
}

// keys are the keys of defs.
func keys(defs []Definition) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Key)
	}
	slices.Sort(out)
	return out
}
