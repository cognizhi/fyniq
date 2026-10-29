package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/management"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ---------------------------------------------------------------- the management reports (FR-RPT-03, FR-RPT-08)

// ManagementRow is one customer's, item's, supplier's or expense account's figure in a management report.
type ManagementRow struct {
	ID        string  `json:"id,omitempty" format:"uuid" doc:"The customer, item, supplier or account; none for the rest: the lines without an item, or the expenses after the top ten." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Code      string  `json:"code,omitempty" doc:"An item's or account's code." example:"6110"`
	Name      string  `json:"name" doc:"Its name, as on file today." example:"Acme Analytics Pte Ltd"`
	Amount    Money   `json:"amount" doc:"Its figure in the base currency: sales or spend net of GST, or the expense as the P&L shows it."`
	Share     Percent `json:"share" doc:"Its % of the total (RND-PERCENT); empty when the total is zero."`
	Quantity  string  `json:"quantity,omitempty" doc:"Sales by item: the quantity sold, net of credits, in the item's unit." example:"12"`
	Documents int     `json:"documents" doc:"The invoices, bills and credit notes behind it; 0 for the top expenses." example:"3"`
}

// ManagementReport is a management report over a run of whole months, read as at one ledger revision.
type ManagementReport struct {
	Report          string          `json:"report" enum:"sales_by_customer,sales_by_item,spend_by_supplier,top_expenses" doc:"The report." example:"sales_by_customer"`
	Currency        string          `json:"currency" doc:"The base currency every amount is in." example:"SGD"`
	From            string          `json:"from" format:"date" doc:"The run's first day." example:"2026-10-01"`
	To              string          `json:"to" format:"date" doc:"The run's last day." example:"2026-10-31"`
	Revision        int64           `json:"ledger_revision" doc:"The ledger revision every figure is read as at (ENG-46): asking again as at it gives the same figures." example:"42"`
	PlatformVersion string          `json:"platform_version" doc:"The platform version that laid it out (REL-05)." example:"0.1.0"`
	Rows            []ManagementRow `json:"rows" nullable:"false" doc:"Largest first."`
	Total           Money           `json:"total" doc:"The rows' total."`
}

type managementInput struct {
	From     string `query:"from" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's first month, YYYY-MM; left out, the last month's." example:"2026-10"`
	To       string `query:"to" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's last month, YYYY-MM; left out, the month of the organisation's today." example:"2026-10"`
	Revision int64  `query:"as_at_revision" minimum:"1" doc:"Read as at this ledger revision (FR-RPT-08), as an earlier report recorded it; left out, the ledger as it stands." example:"42"`
}

type managementOutput struct{ Body ManagementReport }

// managementReport reads and lays out a management report in u.
func (a *API) managementReport(ctx context.Context, u storage.UnitOfWork, k management.Kind, in *managementInput) (out ManagementReport, err error) {
	org, err := u.Organisation(ctx)
	if err != nil {
		return out, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return out, err
	}
	r, err := a.monthsOf(ctx, u, in.From, in.To, in.Revision)
	if err != nil {
		return out, err
	}
	m, err := management.Lay(ctx, u, base, management.Request{Kind: k, All: r.all, First: r.first, Last: r.last, Revision: r.revision})
	if err != nil {
		return out, err
	}
	out = ManagementReport{Report: string(k), Currency: base.Code, From: dateOf(r.all[r.first].StartsOn), To: dateOf(r.all[r.last].EndsOn),
		Revision: r.revision, PlatformVersion: a.deps.Version, Rows: make([]ManagementRow, 0, len(m.Rows)), Total: moneyOut(m.Total)}
	for _, x := range m.Rows {
		row := ManagementRow{Code: x.Code, Name: x.Name, Amount: moneyOut(x.Amount), Share: Percent(x.Share), Quantity: x.Quantity, Documents: x.Documents}
		if x.ID != uuid.Nil {
			row.ID = x.ID.String()
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

func registerManagementReports(a *API) {
	common := " Over a run of whole months (FR-RPT-02), every amount in the base currency, read as at one ledger revision (FR-RPT-08), " +
		"which it returns: asking again with as_at_revision gives the same figures. A month the books have no period for is refused " +
		"(REPORT-03), as is a revision later than the ledger's (REPORT-04). Read-only; no side effects."
	documents := " What the documents posted in the run earned or spent net of GST: their entries' lines on any account but the receivables, " +
		"payables, GST, deposits, tax withheld and FX controls, revenue or cost spread over months counted in full when invoiced or billed. " +
		"A void or an edit posts a reversal, which counts in the month it posts, as in the P&L. Money received or paid at the bank " +
		"without an invoice or bill, and expense claims, are in the P&L and the top expenses, not here."
	for _, x := range []struct {
		policy  authz.Policy
		kind    management.Kind
		path    string
		summary string
		about   string
	}{
		{authz.GetSalesByCustomer, management.SalesByCustomer, "/v1/reports/sales-by-customer", "Get sales by customer",
			"The invoices and credit notes of the run by customer, largest first, each with its share and how many documents (FR-RPT-03)." + documents},
		{authz.GetSalesByItem, management.SalesByItem, "/v1/reports/sales-by-item", "Get sales by item",
			"The invoices' and credit notes' lines of the run by item, largest first, each with its share, the quantity sold net of credits " +
				"and how many documents (FR-RPT-03); the lines without an item are one row." + documents},
		{authz.GetSpendBySupplier, management.SpendBySupplier, "/v1/reports/spend-by-supplier", "Get spend by supplier",
			"The bills and suppliers' credit notes of the run by supplier, largest first, each with its share and how many documents (FR-RPT-03)." + documents},
		{authz.GetTopExpenses, management.TopExpenses, "/v1/reports/top-expenses", "Get the top expenses",
			"The P&L's expense accounts over the run, largest first: the top ten, and the rest as one row, each with its share of the " +
				"expenses (FR-RPT-03). They are the P&L's figures, so they include what was paid at the bank and claimed."},
	} {
		register(a, x.policy, huma.Operation{
			OperationID: x.policy.Operation, Method: http.MethodGet, Path: x.path, Tags: []string{"Reports"},
			Summary: x.summary, Description: x.about + common,
		}, []string{CodeInvalid, CodeNoPeriod, CodeLaterRevision}, func(ctx context.Context, in *managementInput) (*managementOutput, error) {
			p, _ := authn.From(ctx)
			var out ManagementReport
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				out, err = a.managementReport(ctx, u, x.kind, in)
				return err
			})
			if err != nil {
				return nil, err
			}
			return &managementOutput{Body: out}, nil
		})
	}
}
