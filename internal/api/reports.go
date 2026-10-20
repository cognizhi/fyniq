package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the general ledger report (FR-GL-11)

// LedgerLine is one line of the general ledger report.
type LedgerLine struct {
	PostingDate    string `json:"posting_date" format:"date" doc:"The date it posted on." example:"2026-10-31"`
	EntryID        string `json:"entry_id" format:"uuid" doc:"Its entry." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d80"`
	LedgerRevision int64  `json:"ledger_revision" doc:"The ledger revision its entry made (ENG-46)." example:"42"`
	SourceType     string `json:"source_type" doc:"Where it comes from: manual_journal or opening_balance today; invoices, bills and the rest as they arrive (ENG-03)." example:"manual_journal"`
	SourceID       string `json:"source_id" doc:"The source document's ID, to open it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	SourceVersion  int    `json:"source_version" doc:"The version of the source that posted." example:"1"`
	Number         string `json:"number,omitempty" doc:"The source document's number, once it has one." example:"JNL-0042"`
	Reversal       bool   `json:"reversal" doc:"true for a line of a reversing entry." example:"false"`
	Memo           string `json:"memo,omitempty" doc:"The entry's memo." example:"October rent accrual"`
	Description    string `json:"description,omitempty" doc:"The line's description." example:"October office rent"`
	Amount         Money  `json:"amount" doc:"In the line's currency: debit positive, credit negative."`
	Base           Money  `json:"base" doc:"In the base currency, as posted."`
	Balance        Money  `json:"balance" doc:"The account's running balance in the base currency after this line."`
}

// AccountLedger is one account's general ledger over the range.
type AccountLedger struct {
	AccountID string       `json:"account_id" format:"uuid" doc:"The account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Code      string       `json:"code" doc:"Its code." example:"6110"`
	Name      string       `json:"name" doc:"Its name." example:"Rent"`
	Opening   Money        `json:"opening" doc:"Its balance before the range, in the base currency: debit positive."`
	Debits    Money        `json:"debits" doc:"The total of its debit lines in the range."`
	Credits   Money        `json:"credits" doc:"The total of its credit lines in the range, as a positive amount."`
	Closing   Money        `json:"closing" doc:"Its balance at the end of the range."`
	Lines     []LedgerLine `json:"lines" nullable:"false" doc:"Its lines in the range, in posting order: date, then revision, then line."`
}

// GeneralLedgerReport is the general ledger of some accounts over a range of dates, in the base currency (FR-GL-11).
type GeneralLedgerReport struct {
	From     string          `json:"from" format:"date" doc:"The first day." example:"2026-10-01"`
	To       string          `json:"to" format:"date" doc:"The last day." example:"2026-10-31"`
	Currency string          `json:"currency" doc:"The base currency the balances are in." example:"SGD"`
	Revision int64           `json:"ledger_revision" doc:"The ledger revision the report reads (ENG-46): the same revision gives the same report." example:"42"`
	Accounts []AccountLedger `json:"accounts" nullable:"false"`
}

type generalLedgerInput struct {
	AccountID []string `query:"account_id,explode" minItems:"1" maxItems:"20" doc:"The accounts, one to 20, each as its own account_id parameter." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	From      string   `query:"from" required:"true" format:"date" doc:"The first day." example:"2026-10-01"`
	To        string   `query:"to" required:"true" format:"date" doc:"The last day." example:"2026-10-31"`
}

type generalLedgerOutput struct {
	Body GeneralLedgerReport
}

func registerReports(a *API) {
	register(a, authz.GetGeneralLedger, huma.Operation{
		OperationID: "getGeneralLedger", Method: http.MethodGet, Path: "/v1/reports/general-ledger", Tags: []string{"Reports"},
		Summary: "Get the general ledger report",
		Description: "The general ledger of one to 20 accounts over a range of dates, in the base currency (FR-GL-11; PRD-001 rev 0.52, " +
			"provisional): each account's balance before the range, its lines in posting order (date, revision, line) with the " +
			"running balance and each line's source to open it, its debits and credits, and its closing balance; all read at one " +
			"ledger revision. At most 5,000 lines per account: a wider range is refused (REPORT-01) until the asynchronous export. " +
			"Needs an unqualified view of the reports: a view limited to sales, purchases, payroll, a department or the board pack " +
			"does not reach every line. Read-only; no side effects.",
	}, []string{CodeInvalid, CodeNotFound, balance.CodeTooManyLines}, func(ctx context.Context, in *generalLedgerInput) (*generalLedgerOutput, error) {
		p, _ := authn.From(ctx)
		var bad []FieldProblem
		from, err := time.Parse(time.DateOnly, in.From)
		if err != nil {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "from", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
		to, err := time.Parse(time.DateOnly, in.To)
		if err != nil {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "to", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"})
		}
		if len(bad) == 0 && to.Before(from) {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "to", Message: "is before from", Fix: "Send a last day on or after the first"})
		}
		var ids []uuid.UUID
		for i, s := range in.AccountID {
			id, err := uuid.Parse(s)
			if err != nil {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: fmt.Sprintf("account_id[%d]", i), Message: "is not an account ID", Fix: "Send the account's ID"})
				continue
			}
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		if len(bad) > 0 {
			return nil, invalid(bad)
		}
		out := generalLedgerOutput{Body: GeneralLedgerReport{From: in.From, To: in.To, Accounts: []AccountLedger{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			org, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(org.BaseCurrency)
			if err != nil {
				return err
			}
			out.Body.Currency = base.Code
			if out.Body.Revision, err = u.Ledger().Revision(ctx); err != nil {
				return err
			}
			for _, id := range ids {
				acc, err := u.Accounts().Get(ctx, id)
				if err != nil {
					return err
				}
				gl, err := balance.GeneralLedger(ctx, u, base, id, from, to)
				if err != nil {
					return err
				}
				out.Body.Accounts = append(out.Body.Accounts, accountLedgerOf(acc, gl))
			}
			return nil
		})
		var many *balance.TooMany
		if errors.As(err, &many) {
			return nil, problem(balance.CodeTooManyLines, fmt.Sprintf("An account has more than %d lines from %s to %s.", balance.MaxLedgerLines, in.From, in.To),
				"Choose a shorter range of dates")
		}
		return &out, err
	})
}

func accountLedgerOf(acc storage.Account, gl balance.AccountLedger) AccountLedger {
	m := func(x money.Money) Money { return Money{Amount: x.String(), Currency: x.Currency().Code} }
	out := AccountLedger{AccountID: acc.ID.String(), Code: acc.Code, Name: acc.Name, Opening: m(gl.Opening), Debits: m(gl.Debits), Credits: m(gl.Credits),
		Closing: m(gl.Closing), Lines: make([]LedgerLine, 0, len(gl.Lines))}
	for _, l := range gl.Lines {
		out.Lines = append(out.Lines, LedgerLine{PostingDate: dateOf(l.PostingDate), EntryID: l.EntryID.String(), LedgerRevision: l.LedgerRevision,
			SourceType: l.Source.Type, SourceID: l.Source.ID, SourceVersion: l.Source.Version, Number: l.Number, Reversal: l.ReversesEntryID != nil,
			Memo: l.Memo, Description: l.Description, Amount: m(l.Amount), Base: m(l.Base), Balance: m(l.Balance)})
	}
	return out
}
