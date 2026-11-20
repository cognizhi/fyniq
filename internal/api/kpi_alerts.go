package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// KPIAlert is an alert the worker raised when a closed month's spend in an expense category was three standard deviations above its trend (FR-KPI-06).
type KPIAlert struct {
	ID             string `json:"id" format:"uuid" doc:"The alert's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7a10"`
	Kind           string `json:"kind" enum:"spend_anomaly" doc:"What kind of alert it is." example:"spend_anomaly"`
	Category       string `json:"category" doc:"The expense category: a report group of the chart." example:"Hosting"`
	Month          string `json:"month" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The closed month whose spend broke from its trend." example:"2026-11"`
	Spend          Money  `json:"spend" doc:"The month's spend in the category."`
	Mean           Money  `json:"mean" doc:"The mean of the months before it, which the spend was compared with."`
	HistoryMonths  int    `json:"history_months" doc:"How many months before it the trend is worked out from (6 to 12)." example:"8"`
	LedgerRevision int64  `json:"ledger_revision" doc:"The ledger revision the figures were read at (FR-RPT-08)." example:"42"`
}

// KPIAlertPage is a page of alerts.
type KPIAlertPage struct {
	Items      []KPIAlert `json:"items" nullable:"false" doc:"The alerts, latest month first."`
	NextCursor string     `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last." example:"eyJhZnRlciI6IjEifQ"`
}

func registerKPIAlerts(a *API) {
	register(a, authz.ListKPIAlerts, huma.Operation{
		OperationID: "listKpiAlerts", Method: http.MethodGet, Path: "/v1/kpi-alerts", Tags: []string{"Reports"},
		Summary: "List the spend anomaly alerts",
		Description: "The alerts the worker raised (FR-KPI-06): a closed month's spend in an expense category above the mean of the months before it by more than three " +
			"standard deviations, worked out exactly (at least six months of history, and an excess of at least a tenth of the mean), each once for a category and month, " +
			"a page at a time. The people who approve budgets are told by a notification when one is raised. Read-only; no side effects.",
	}, []string{CodeCursor, storage.CodeRetry, storage.CodeLockTimeout}, func(ctx context.Context, in *struct{ PageQuery }) (*struct{ Body KPIAlertPage }, error) {
		p, _ := authn.From(ctx)
		var all []storage.KPIAlert
		if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			all, err = u.KPIAlerts().List(ctx)
			return err
		}); err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(x storage.KPIAlert) string { return x.ID.String() }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		out := KPIAlertPage{Items: []KPIAlert{}, NextCursor: next}
		for _, x := range items {
			out.Items = append(out.Items, KPIAlert{ID: x.ID.String(), Kind: x.Kind, Category: x.Category, Month: x.Month.Format(statementMonthForm), Spend: moneyOf(x.Spend, x.Currency),
				Mean: moneyOf(x.Mean, x.Currency), HistoryMonths: x.HistoryMonths, LedgerRevision: x.LedgerRevision})
		}
		return &struct{ Body KPIAlertPage }{Body: out}, nil
	})
}
