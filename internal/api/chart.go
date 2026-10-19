package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/chart"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the standard chart (FR-COA-01)

// CodeChartLoaded refuses the standard chart for an organisation that has accounts already.
const CodeChartLoaded = "API-CHART-01"

// ChartLoaded is what loading the standard chart did.
type ChartLoaded struct {
	Accounts int `json:"accounts" doc:"How many accounts the chart now holds." example:"86"`
}

type loadChartInput struct {
	KeyHeader
	PreferHeader
}

type loadChartOutput struct {
	Status   int
	Location string `header:"Location" doc:"The chart of accounts."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ChartLoaded
}

func registerChart(a *API) {
	register(a, authz.LoadStandardChart, huma.Operation{
		OperationID: "loadStandardChart", Method: http.MethodPost, Path: "/v1/accounts:load-standard-chart", Tags: []string{"Chart of accounts"},
		DefaultStatus: http.StatusCreated, Summary: "Load the standard chart of accounts",
		Description: "Loads the standard Singapore start-up chart into an organisation that has no account yet: 86 accounts with their " +
			"report groups, cash-flow lines, functions, GST codes, flags and guidance (FR-COA-01, FR-COA-02; PRD-001 rev 0.31, provisional " +
			"until the accountant's review). Refused once the organisation has any account. Side effects: the accounts and an audit event " +
			"for each. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, []string{CodeChartLoaded, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeDuplicate, storage.CodeRetry,
		storage.CodeLockTimeout},
		func(ctx context.Context, in *loadChartInput) (*loadChartOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ChartLoaded, error) {
				n, err := u.Accounts().Count(ctx)
				if err != nil {
					return ChartLoaded{}, err
				}
				if n > 0 {
					return ChartLoaded{}, problem(CodeChartLoaded, fmt.Sprintf("The organisation has %d accounts already.", n),
						"Change the chart account by account; the standard chart loads only into an organisation with none.")
				}
				accounts, err := chart.Standard()
				if err != nil {
					return ChartLoaded{}, err
				}
				for _, acc := range accounts {
					if _, err := u.Accounts().Create(ctx, acc.NewAccount); err != nil {
						return ChartLoaded{}, err
					}
				}
				return ChartLoaded{Accounts: len(accounts)}, nil
			})
			if err != nil {
				return nil, err
			}
			out := &loadChartOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.Location, out.Replayed = "/v1/accounts", replayedHeader(w.Replayed)
			}
			return out, nil
		})
}
