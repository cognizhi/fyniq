package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// --------------------------------------------------------------------------- AR ageing (FR-AR-10; p1-sales task 1.6)

// ReceivablesRow is what customers owe in one currency by days past due, with their credits apart.
type ReceivablesRow struct {
	Currency string `json:"currency" doc:"The currency." example:"SGD"`
	Current  Money  `json:"current" doc:"Not yet due, or due on the day."`
	Days1    Money  `json:"days_1_30" doc:"1 to 30 days past due."`
	Days31   Money  `json:"days_31_60" doc:"31 to 60 days past due."`
	Days61   Money  `json:"days_61_90" doc:"61 to 90 days past due."`
	Days90   Money  `json:"days_90_plus" doc:"More than 90 days past due."`
	Credits  Money  `json:"credits" doc:"Unapplied credits with customers, negative: what receipts and credit notes left over (FR-AR-18), as they arrive."`
	Total    Money  `json:"total" doc:"Every bucket and the credits."`
	Carrying Money  `json:"carrying" doc:"The total's carrying amount in the base currency, at the invoices' own rates (ENG-14)."`
}

// ReceivablesCustomer is one customer's ageing.
type ReceivablesCustomer struct {
	ID   string           `json:"id" format:"uuid" doc:"The customer's contact ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Name string           `json:"name" doc:"The customer's legal name." example:"Acme Analytics Pte Ltd"`
	Rows []ReceivablesRow `json:"rows" nullable:"false" doc:"A row per currency."`
}

// ReceivablesExposure is the unrealised FX on what customers owe in one foreign currency.
type ReceivablesExposure struct {
	Currency     string `json:"currency" doc:"The currency." example:"USD"`
	Open         Money  `json:"open" doc:"What customers owe in it."`
	Carrying     Money  `json:"carrying" doc:"Its carrying amount in the base currency, at the invoices' own rates (ENG-14)."`
	FXRate       string `json:"fx_rate,omitempty" doc:"Base currency per one unit on the day, from the rate cascade (ENG-40); absent when it has none." example:"1.3456"`
	FXRateSource string `json:"fx_rate_source,omitempty" doc:"Where the rate came from." example:"MAS daily feed of 2026-10-31"`
	AtRate       *Money `json:"at_rate,omitempty" doc:"What it is worth at the rate, under RND-FX."`
	Unrealised   *Money `json:"unrealised,omitempty" doc:"The unrealised gain (positive) or loss (negative): at the rate less the carrying amount; what the month-end revaluation would post before any earlier run (FR-GL-04)."`
	Missing      string `json:"missing,omitempty" doc:"Why there is no rate (FX-01), when the cascade has none for the day." example:"No exchange rate for USD on 2026-10-31"`
}

// Receivables is AR as at a day: the ageing by customer and currency, and the unrealised FX exposure.
type Receivables struct {
	AsAt      string                `json:"as_at" format:"date" doc:"The day." example:"2026-10-31"`
	Customers []ReceivablesCustomer `json:"customers" nullable:"false" doc:"Each customer with something open, by name."`
	Totals    []ReceivablesRow      `json:"totals" nullable:"false" doc:"The ageing's totals, a row per currency."`
	Exposure  []ReceivablesExposure `json:"fx_exposure" nullable:"false" doc:"The unrealised FX of each foreign currency's total at the day's rate; empty when every open invoice is in the base currency."`
}

type receivablesInput struct {
	AsAt string `query:"as_at" format:"date" doc:"The day; left out, the organisation's today." example:"2026-10-31"`
}

type receivablesOutput struct{ Body Receivables }

func receivablesRowOf(r receivables.Row) ReceivablesRow {
	return ReceivablesRow{Currency: r.Currency.Code, Current: moneyOut(r.Buckets[0]), Days1: moneyOut(r.Buckets[1]), Days31: moneyOut(r.Buckets[2]),
		Days61: moneyOut(r.Buckets[3]), Days90: moneyOut(r.Buckets[4]), Credits: moneyOut(r.Credits), Total: moneyOut(r.Total), Carrying: moneyOut(r.Carrying)}
}

func registerReceivables(a *API) {
	register(a, authz.GetReceivables, huma.Operation{
		OperationID: "getReceivables", Method: http.MethodGet, Path: "/v1/receivables", Tags: []string{"Sales"},
		Summary: "Get AR ageing",
		Description: "What customers owe the company as at a day (FR-AR-10), from the AR sub-ledger (INV-07): each customer's open " +
			"invoices by days past due (current, 1–30, 31–60, 61–90, 90+) and unapplied credits, by currency, with the totals and " +
			"their carrying amounts in the base currency, and the unrealised FX exposure of each foreign currency at the day's rate " +
			"from the cascade (ENG-40). An invoice falls due on its due date as entered, else by the customer's or the " +
			"organisation's payment terms. Read-only; no side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *receivablesInput) (*receivablesOutput, error) {
		p, _ := authn.From(ctx)
		var out Receivables
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			asAt, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			if in.AsAt != "" {
				if asAt, err = time.Parse(time.DateOnly, in.AsAt); err != nil {
					return invalid([]FieldProblem{{Code: CodeInvalid, Field: "as_at", Message: "is not a date", Fix: "Send the day as YYYY-MM-DD"}})
				}
			}
			out = Receivables{AsAt: dateOf(asAt), Customers: []ReceivablesCustomer{}, Totals: []ReceivablesRow{}, Exposure: []ReceivablesExposure{}}
			accounts, err := u.Accounts().List(ctx)
			if err != nil {
				return err
			}
			for _, acc := range accounts {
				if string(acc.Role) != ledger.RoleAccountsReceivable {
					continue
				}
				l, err := receivables.Open(ctx, u, base, acc.ID, asAt)
				if err != nil {
					return err
				}
				ageing, err := receivables.Age(l, base)
				if err != nil {
					return err
				}
				for _, c := range ageing.Customers {
					x := ReceivablesCustomer{ID: c.ID.String(), Name: c.Name, Rows: []ReceivablesRow{}}
					for _, r := range c.Rows {
						x.Rows = append(x.Rows, receivablesRowOf(r))
					}
					out.Customers = append(out.Customers, x)
				}
				for _, r := range ageing.Totals {
					out.Totals = append(out.Totals, receivablesRowOf(r))
				}
				exposures, err := receivables.Exposures(ctx, u, base, ageing)
				if err != nil {
					return err
				}
				for _, x := range exposures {
					e := ReceivablesExposure{Currency: x.Currency.Code, Open: moneyOut(x.Open), Carrying: moneyOut(x.Carrying)}
					if x.Missing != nil {
						e.Missing = x.Missing.Message
					} else {
						atRate, unrealised := moneyOut(x.AtRate), moneyOut(x.Unrealised)
						e.FXRate, e.FXRateSource, e.AtRate, e.Unrealised = x.Rate.String(), x.Rate.Source, &atRate, &unrealised
					}
					out.Exposure = append(out.Exposure, e)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &receivablesOutput{Body: out}, nil
	})
}
