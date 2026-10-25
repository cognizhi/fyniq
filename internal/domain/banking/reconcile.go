package banking

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Reconciliation (FR-BNK-10, FR-GL-07, FM-5, SOD-5; p1-bank-import-reconcile task 6.1, design D9): per money account and
// period, the statement's balance at the period's end, the book balance of the account in its own currency from the
// balance service, the lines the books do not hold yet, what they leave unexplained, and the ledger revision it saw
// (ENG-46). One person prepares it; another signs it off, with step-up (the API's); the period's close locks it (FY226).
//
// The daily cash position (FR-BNK-13; task 6.3) is every money account's balance in its currency on a day, valued at
// the latest rate the cascade has for that day (ENG-40), with the rate's source.

// CodeNoStatement is a reconciliation with no statement balance: none was imported up to the period's end, and none
// was entered.
const CodeNoStatement = "BANK-10"

// CodeTyped is a statement balance typed in Founder mode, where it comes only from an imported statement or feed (FM-5).
const CodeTyped = "BANK-12"

// ReconcileRequest is a reconciliation to prepare: the money account, the period, and the statement's balance at the
// period's end when it is entered from an e-statement (nil: the latest imported statement closing on or before the
// period's end).
type ReconcileRequest struct {
	MoneyAccountID   uuid.UUID
	PeriodID         uuid.UUID
	StatementBalance *money.Money
	Founder          bool // the organisation is in Founder mode, so a typed statement balance is refused (FM-5)
	At               time.Time
}

// ReconciledPeriod is a prepared reconciliation, with where its statement balance came from and the lines it counts as
// unreconciled, earliest first.
type ReconciledPeriod struct {
	storage.BankReconciliation
	StatementDate time.Time // the day the statement balance is of
	Unreconciled  []storage.BankLine
}

// BookBalance is a chart account's balance in a currency through a day, and its value in the base currency, from the
// balance service (ENG-04, balance.Through).
func BookBalance(ctx context.Context, u storage.UnitOfWork, account uuid.UUID, cur, base money.Currency, through time.Time) (money.Money, money.Money, error) {
	return balance.Through(ctx, u, account, cur, base, through)
}

// statementBalance is the closing balance of the latest statement of an account ending on or before a day, and its day.
func statementBalance(ctx context.Context, u storage.UnitOfWork, account uuid.UUID, cur money.Currency, through time.Time) (*money.Money, time.Time, error) {
	stmts, err := u.Banking().Statements(ctx, account)
	if err != nil {
		return nil, time.Time{}, err
	}
	var best *storage.BankStatement
	for i := range stmts {
		s := &stmts[i]
		if s.Closing == nil || s.PeriodTo.After(through) {
			continue
		}
		if best == nil || s.PeriodTo.After(best.PeriodTo) || (s.PeriodTo.Equal(best.PeriodTo) && s.ImportedAt.After(best.ImportedAt)) {
			best = s
		}
	}
	if best == nil {
		return nil, time.Time{}, nil
	}
	m, err := money.ParseStored(string(*best.Closing), cur)
	return &m, best.PeriodTo, err
}

// Prepare prepares an account's reconciliation for a period, in u, or prepares it again while it is unsigned: the
// statement's balance, the book balance at the period's end, the lines booked by then that are not in the books yet
// (unreconciled, or possible duplicates), and the difference they leave: statement − book − unreconciled.
func Prepare(ctx context.Context, u storage.UnitOfWork, r ReconcileRequest) (ReconciledPeriod, error) {
	acct, err := u.Banking().MoneyAccount(ctx, r.MoneyAccountID)
	if err != nil {
		return ReconciledPeriod{}, err
	}
	p, err := u.Periods().Get(ctx, r.PeriodID)
	if err != nil {
		return ReconciledPeriod{}, err
	}
	cur, err := money.Lookup(acct.Currency)
	if err != nil {
		return ReconciledPeriod{}, err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return ReconciledPeriod{}, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return ReconciledPeriod{}, err
	}
	if r.StatementBalance != nil && r.Founder {
		return ReconciledPeriod{}, refuse(CodeTyped, "Import the bank's statement for the month: in Founder mode the balance comes from it, never typed (FM-5).",
			"the statement balance of %s was typed", acct.Name)
	}
	out := ReconciledPeriod{StatementDate: p.EndsOn}
	stmt := r.StatementBalance
	if stmt == nil {
		if stmt, out.StatementDate, err = statementBalance(ctx, u, acct.ID, cur, p.EndsOn); err != nil {
			return ReconciledPeriod{}, err
		}
		if stmt == nil {
			return ReconciledPeriod{}, refuse(CodeNoStatement, "Import the statement for the month, or enter the balance the e-statement shows.",
				"%s has no statement balance up to %s", acct.Name, p.EndsOn.Format(time.DateOnly))
		}
	} else if stmt.Currency() != cur {
		return ReconciledPeriod{}, refuse(CodeCurrency, fmt.Sprintf("Enter the balance in %s.", cur.Code), "the statement balance is in %s; %s is in %s",
			stmt.Currency().Code, acct.Name, cur.Code)
	}
	book, _, err := BookBalance(ctx, u, acct.AccountID, cur, base, p.EndsOn)
	if err != nil {
		return ReconciledPeriod{}, err
	}
	lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &acct.ID, To: &p.EndsOn,
		Statuses: []string{storage.LineUnreconciled, storage.LinePossibleDuplicate}})
	if err != nil {
		return ReconciledPeriod{}, err
	}
	open := money.Zero(cur)
	for _, l := range lines {
		a, err := money.ParseStored(string(l.Amount), cur)
		if err != nil {
			return ReconciledPeriod{}, err
		}
		open, _ = open.Add(a)
	}
	diff, _ := stmt.Sub(book)
	diff, _ = diff.Sub(open)
	rev, err := u.Ledger().Revision(ctx)
	if err != nil {
		return ReconciledPeriod{}, err
	}
	rec, err := u.BankMatching().PrepareReconciliation(ctx, storage.NewBankReconciliation{MoneyAccountID: acct.ID, PeriodID: p.ID, Currency: cur.Code,
		StatementBalance: storage.Decimal(stmt.String()), BookBalance: storage.Decimal(book.String()), UnreconciledTotal: storage.Decimal(open.String()),
		UnreconciledCount: len(lines), Difference: storage.Decimal(diff.String()), LedgerRevision: rev, At: r.At})
	if err != nil {
		return ReconciledPeriod{}, err
	}
	out.BankReconciliation, out.Unreconciled = rec, lines
	return out, nil
}

// SignOff signs a prepared reconciliation at version, in u, as the person in ctx, who passed step-up at mfaAt: never
// its preparer (FM-5, SOD-5; FY226 refuses it), and never once its period is closed.
func SignOff(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, version int, mfaAt, at time.Time) (storage.BankReconciliation, error) {
	return u.BankMatching().SignReconciliation(ctx, id, version, mfaAt, at)
}

// CashRow is one money account's cash on a day: its balance in its currency, its value in the base currency at the
// day's rate (nil for an account in the base currency, or one with no rate), and where the rate came from.
type CashRow struct {
	Account storage.MoneyAccount
	Balance money.Money
	Value   *money.Money
	Rate    *money.Rate
	RateOf  time.Time // the day the rate is of
}

// CashPosition is the organisation's cash on a day (FR-BNK-13): each open money account, by currency then name, and the
// total in the base currency of those with a value. Missing names the accounts in another currency with no rate.
type CashPosition struct {
	Day     time.Time
	Rows    []CashRow
	Total   money.Money
	Missing []string
}

// Cash is the cash position on day, in u.
func Cash(ctx context.Context, u storage.UnitOfWork, day time.Time) (CashPosition, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return CashPosition{}, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return CashPosition{}, err
	}
	accounts, err := u.Banking().MoneyAccounts(ctx)
	if err != nil {
		return CashPosition{}, err
	}
	out := CashPosition{Day: day, Total: money.Zero(base)}
	for _, a := range accounts {
		if a.ClosedOn != nil && a.ClosedOn.Before(day) {
			continue
		}
		cur, err := money.Lookup(a.Currency)
		if err != nil {
			return CashPosition{}, err
		}
		bal, _, err := BookBalance(ctx, u, a.AccountID, cur, base, day)
		if err != nil {
			return CashPosition{}, err
		}
		row := CashRow{Account: a, Balance: bal}
		if cur == base {
			v := bal
			row.Value = &v
		} else {
			f, err := fx.Lookup(ctx, u, base, cur, day)
			var fe *fx.Error
			switch {
			case errors.As(err, &fe):
				out.Missing = append(out.Missing, a.Name)
			case err != nil:
				return CashPosition{}, err
			default:
				v, err := rounding.FX(bal, f.Rate)
				if err != nil {
					return CashPosition{}, err
				}
				rate := f.Rate
				row.Value, row.Rate, row.RateOf = &v, &rate, f.Of
			}
		}
		if row.Value != nil {
			out.Total, _ = out.Total.Add(*row.Value)
		}
		out.Rows = append(out.Rows, row)
	}
	slices.SortStableFunc(out.Rows, func(x, y CashRow) int {
		if x.Account.Currency != y.Account.Currency {
			if x.Account.Currency == base.Code {
				return -1
			}
			if y.Account.Currency == base.Code {
				return 1
			}
			if x.Account.Currency < y.Account.Currency {
				return -1
			}
			return 1
		}
		if x.Account.Name < y.Account.Name {
			return -1
		}
		if x.Account.Name > y.Account.Name {
			return 1
		}
		return 0
	})
	return out, nil
}
