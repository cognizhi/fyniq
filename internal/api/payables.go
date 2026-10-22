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
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// --------------------------------------------------------------------------- AP ageing and bills to pay (FR-AP-06)

// PayablesRow is what is owed in one currency by days past due, with the supplier credits apart.
type PayablesRow struct {
	Currency string `json:"currency" doc:"The currency." example:"SGD"`
	Current  Money  `json:"current" doc:"Not yet due, or due on the day."`
	Days1    Money  `json:"days_1_30" doc:"1 to 30 days past due."`
	Days31   Money  `json:"days_31_60" doc:"31 to 60 days past due."`
	Days61   Money  `json:"days_61_90" doc:"61 to 90 days past due."`
	Days90   Money  `json:"days_90_plus" doc:"More than 90 days past due."`
	Credits  Money  `json:"credits" doc:"Unapplied credits with suppliers, negative: what payments left over (FR-AR-18 mirrored)."`
	Total    Money  `json:"total" doc:"Every bucket and the credits."`
	Carrying Money  `json:"carrying" doc:"The total's carrying amount in the base currency, at the bills' own rates (ENG-14)."`
}

// PayablesSupplier is one supplier's ageing.
type PayablesSupplier struct {
	ID   string        `json:"id" format:"uuid" doc:"The supplier's contact ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	Name string        `json:"name" doc:"The supplier's legal name." example:"Atlassian Pty Ltd"`
	Rows []PayablesRow `json:"rows" nullable:"false" doc:"A row per currency."`
}

// PayableBill is an open bill to pay.
type PayableBill struct {
	BillID       string `json:"bill_id" format:"uuid" doc:"The bill." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e30"`
	Number       string `json:"number,omitempty" doc:"Its number." example:"BILL-0007"`
	SupplierID   string `json:"supplier_id" format:"uuid" doc:"Its supplier." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	SupplierName string `json:"supplier_name" doc:"The supplier's legal name." example:"Atlassian Pty Ltd"`
	Date         string `json:"date" format:"date" doc:"The bill's date." example:"2026-10-14"`
	DueDate      string `json:"due_date,omitempty" format:"date" doc:"When it falls due: as entered, else from the payment terms." example:"2026-11-13"`
	Open         Money  `json:"open" doc:"What is still to pay of it, in its currency."`
	Overdue      bool   `json:"overdue" doc:"true when its due date is before the day." example:"false"`
}

// PayablesRequirement is the cash one currency needs: what is overdue and what falls due within the window.
type PayablesRequirement struct {
	Currency string `json:"currency" doc:"The currency." example:"SGD"`
	Overdue  Money  `json:"overdue" doc:"Due before the day."`
	Within   Money  `json:"within" doc:"Due on the day or within the window after it."`
	Total    Money  `json:"total" doc:"Both."`
	Carrying Money  `json:"carrying" doc:"The total's carrying amount in the base currency, at the bills' own rates: a foreign bill costs what its rate on the day it is paid makes it."`
}

// Payables is AP as at a day: the ageing by supplier and currency, the bills to pay by due date, and the cash they need.
type Payables struct {
	AsAt        string                `json:"as_at" format:"date" doc:"The day." example:"2026-10-31"`
	Within      int                   `json:"within" doc:"The window, in days after the day, of the bills to pay." example:"30"`
	Suppliers   []PayablesSupplier    `json:"suppliers" nullable:"false" doc:"Each supplier with something open, by name."`
	Totals      []PayablesRow         `json:"totals" nullable:"false" doc:"The ageing's totals, a row per currency."`
	ToPay       []PayableBill         `json:"to_pay" nullable:"false" doc:"The open bills overdue or due within the window, by due date."`
	Requirement []PayablesRequirement `json:"requirement" nullable:"false" doc:"The cash those bills need, by currency. Supplier credits are not taken off: they are applied to a bill first."`
}

type payablesInput struct {
	AsAt   string `query:"as_at" format:"date" doc:"The day; left out, the organisation's today." example:"2026-10-31"`
	Within int    `query:"within" minimum:"0" maximum:"366" default:"30" doc:"The window of the bills to pay and the cash requirement, in days after the day." example:"30"`
}

type payablesOutput struct{ Body Payables }

func payablesRowOf(r payables.Row) PayablesRow {
	return PayablesRow{Currency: r.Currency.Code, Current: moneyOut(r.Buckets[0]), Days1: moneyOut(r.Buckets[1]), Days31: moneyOut(r.Buckets[2]),
		Days61: moneyOut(r.Buckets[3]), Days90: moneyOut(r.Buckets[4]), Credits: moneyOut(r.Credits), Total: moneyOut(r.Total), Carrying: moneyOut(r.Carrying)}
}

func registerPayables(a *API) {
	register(a, authz.GetPayables, huma.Operation{
		OperationID: "getPayables", Method: http.MethodGet, Path: "/v1/payables", Tags: []string{"Purchases"},
		Summary: "Get AP ageing and the bills to pay",
		Description: "What the company owes its suppliers as at a day (FR-AP-06), from the AP sub-ledger (INV-07): each supplier's open " +
			"bills by days past due (current, 1–30, 31–60, 61–90, 90+) and unapplied credits, by currency, with the totals and their " +
			"carrying amounts in the base currency; the open bills overdue or due within the window after the day, by due date; " +
			"and the cash they need by currency. A bill falls due on its due date as entered, else by the supplier's or the " +
			"organisation's payment terms. Read-only; no side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *payablesInput) (*payablesOutput, error) {
		p, _ := authn.From(ctx)
		var out Payables
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
			out = Payables{AsAt: dateOf(asAt), Within: in.Within, Suppliers: []PayablesSupplier{}, Totals: []PayablesRow{}, ToPay: []PayableBill{},
				Requirement: []PayablesRequirement{}}
			accounts, err := u.Accounts().List(ctx)
			if err != nil {
				return err
			}
			for _, acc := range accounts {
				if string(acc.Role) != ledger.RoleAccountsPayable {
					continue
				}
				l, err := payables.Open(ctx, u, base, acc.ID, asAt)
				if err != nil {
					return err
				}
				ageing, err := payables.Age(l, base)
				if err != nil {
					return err
				}
				for _, s := range ageing.Suppliers {
					x := PayablesSupplier{ID: s.ID.String(), Name: s.Name, Rows: []PayablesRow{}}
					for _, r := range s.Rows {
						x.Rows = append(x.Rows, payablesRowOf(r))
					}
					out.Suppliers = append(out.Suppliers, x)
				}
				for _, r := range ageing.Totals {
					out.Totals = append(out.Totals, payablesRowOf(r))
				}
				items, req, err := payables.ToPay(l, base, in.Within)
				if err != nil {
					return err
				}
				for _, it := range items {
					out.ToPay = append(out.ToPay, PayableBill{BillID: it.DocumentID.String(), Number: it.Number, SupplierID: it.SupplierID.String(),
						SupplierName: it.SupplierName, Date: dayText(it.Date), DueDate: dayText(it.DueDate), Open: moneyOut(it.Open), Overdue: it.DueDate.Before(asAt)})
				}
				for _, r := range req {
					out.Requirement = append(out.Requirement, PayablesRequirement{Currency: r.Currency.Code, Overdue: moneyOut(r.Overdue), Within: moneyOut(r.Within),
						Total: moneyOut(r.Total), Carrying: moneyOut(r.Carrying)})
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &payablesOutput{Body: out}, nil
	})
}
