package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// RollForwardRow is one customer's or supplier's line of a deferral roll-forward (FR-RPT-13).
type RollForwardRow struct {
	PartyID   string `json:"party_id,omitempty" format:"uuid" doc:"The customer or supplier; left out for a spread with none, such as a bank line." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a10"`
	PartyName string `json:"party_name" doc:"Their name, or 'No customer or supplier' for a spread with none." example:"Acme Pte Ltd"`
	Opening   Money  `json:"opening" doc:"What its schedules held on the day before the run."`
	Added     Money  `json:"added" doc:"Scheduled by documents dated inside the run."`
	Released  Money  `json:"released" doc:"Released to the profit and loss inside the run."`
	Reversed  Money  `json:"reversed" doc:"Reversed or cancelled inside the run: a reversed release, a credit note, an edit or a cancellation; it moves the balance either way."`
	Closing   Money  `json:"closing" doc:"What they hold at the end: opening + added - released + reversed, exactly."`
}

// RollForwardHolding is the roll-forward of one holding account.
type RollForwardHolding struct {
	Kind        string           `json:"kind" enum:"prepayment,deferred_revenue" doc:"1210 Prepayments (by supplier) or 2400 Deferred revenue (by customer)." example:"deferred_revenue"`
	AccountCode string           `json:"account_code" doc:"The holding account's code." example:"2400"`
	AccountName string           `json:"account_name" doc:"Its name." example:"Deferred revenue"`
	Rows        []RollForwardRow `json:"rows" nullable:"false" doc:"One row per customer or supplier."`
	Total       RollForwardRow   `json:"total" doc:"The totals of the columns."`
	Ledger      Money            `json:"ledger_balance" doc:"What the account holds at the end of the run, from the balance service (ENG-04), read positive."`
	Direct      Money            `json:"direct" doc:"What documents put on it with no schedule to release it."`
	Unexplained Money            `json:"unexplained" doc:"What is left when the schedules, the direct amounts and the other sources are taken from the ledger balance: zero when it ties (INV-07)."`
	Ties        bool             `json:"ties" doc:"True when the closing total, the direct amounts and what other sources put on the account equal the ledger balance, so that nothing is unexplained." example:"true"`
}

// RollForward is the roll-forward of deferred revenue and prepayments over a run of months (FR-RPT-13).
type RollForward struct {
	Currency string               `json:"currency" doc:"The base currency every amount is in." example:"SGD"`
	From     string               `json:"from" format:"date" doc:"The run's first day." example:"2026-10-01"`
	To       string               `json:"to" format:"date" doc:"The run's last day." example:"2026-10-31"`
	Holdings []RollForwardHolding `json:"holdings" nullable:"false" doc:"1210 Prepayments and 2400 Deferred revenue, each that the chart has."`
}

type rollForwardInput struct {
	From string `query:"from" required:"true" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's first month, YYYY-MM." example:"2026-10"`
	To   string `query:"to" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's last month, YYYY-MM; left out, the first month's." example:"2026-10"`
}

type rollForwardOutput struct{ Body RollForward }

func registerDeferralRollForward(a *API) {
	register(a, authz.GetDeferralRollForward, huma.Operation{
		OperationID: "getDeferralRollForward", Method: http.MethodGet, Path: "/v1/reports/deferral-roll-forward", Tags: []string{"Reports"},
		Summary: "Get the roll-forward of deferred revenue and prepayments",
		Description: "For a run of months (FR-RPT-13): by customer for 2400 Deferred revenue and by supplier for 1210 Prepayments, what the schedules held on the day before the run, what was added, " +
			"what was released to the profit and loss, what was reversed or cancelled (reversed releases, credit notes, edits and cancellations) and what they hold at the end, so that closing = opening + added - " +
			"released + reversed exactly. Each holding account's closing is tied to the ledger (INV-07) with its direct amounts and what is unexplained. Months post only once reached (ENG-05): a month not yet " +
			"posted is not released. Read-only; no side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *rollForwardInput) (*rollForwardOutput, error) {
		p, _ := authn.From(ctx)
		first, err := time.Parse(statementMonthForm, in.From)
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "from", Message: "is not a month", Fix: "Send the month as YYYY-MM"}})
		}
		last := first
		if in.To != "" {
			if last, err = time.Parse(statementMonthForm, in.To); err != nil || last.Before(first) {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "to", Message: "is not a month on or after from", Fix: "Send the last month as YYYY-MM"}})
			}
		}
		end := last.AddDate(0, 1, -1)
		var out RollForward
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			rfs, err := journal.DeferralRollForward(ctx, u, first, end)
			if err != nil {
				return err
			}
			names := map[uuid.UUID]string{}
			nameOf := func(id uuid.UUID) string {
				if id == uuid.Nil {
					return "No customer or supplier"
				}
				if n, ok := names[id]; ok {
					return n
				}
				c, err := u.Contacts().Get(ctx, id)
				if errors.Is(err, ports.ErrNotFound) || err != nil {
					names[id] = "Unknown contact"
				} else {
					names[id] = c.LegalName
				}
				return names[id]
			}
			rowOf := func(r journal.RollRow) RollForwardRow {
				x := RollForwardRow{PartyName: nameOf(r.Party), Opening: moneyOut(r.Opening), Added: moneyOut(r.Added), Released: moneyOut(r.Released),
					Reversed: moneyOut(r.Reversed), Closing: moneyOut(r.Closing)}
				if r.Party != uuid.Nil {
					x.PartyID = r.Party.String()
				}
				return x
			}
			out = RollForward{Currency: base.Code, From: dateOf(first), To: dateOf(end), Holdings: []RollForwardHolding{}}
			for _, rf := range rfs {
				h := RollForwardHolding{Kind: scheduleKind(rf.Holding), AccountCode: rf.Account.Code, AccountName: rf.Account.Name, Rows: []RollForwardRow{}, Total: rowOf(rf.Total),
					Ledger: moneyOut(rf.Ledger), Direct: moneyOut(rf.Direct), Unexplained: moneyOut(rf.Unexplained), Ties: rf.Unexplained.IsZero()}
				h.Total.PartyName = "Total"
				for _, r := range rf.Rows {
					h.Rows = append(h.Rows, rowOf(r))
				}
				out.Holdings = append(out.Holdings, h)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &rollForwardOutput{Body: out}, nil
	})
}
