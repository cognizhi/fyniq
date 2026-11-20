package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/forecastaccuracy"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the forecast accuracy (FR-PLN-04; p2-reporting-planning design D12)

// ForecastAccuracyRow is one kept projection compared with the cash that came, at one horizon.
type ForecastAccuracyRow struct {
	SnapshotID    string `json:"snapshot_id" format:"uuid" doc:"The kept projection." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9c10"`
	TakenOn       string `json:"taken_on" format:"date" doc:"The day it was taken: its first day is the projection's today." example:"2026-08-03"`
	Weeks         int    `json:"weeks" enum:"4,13" doc:"How many weeks later it is compared." example:"4"`
	WindowEnd     string `json:"window_end" format:"date" doc:"The last day of that week." example:"2026-08-30"`
	Projected     Money  `json:"projected" doc:"The balance the projection gave at the window's end."`
	Actual        Money  `json:"actual" doc:"The money accounts' balance at that day, from the ledger now."`
	Difference    Money  `json:"difference" doc:"Actual less projected: below zero, less cash came than the projection said."`
	ReceiptsLater Money  `json:"receipts_later" doc:"Invoices the projection expected in the window that were not settled by its end, signed as the cash moved."`
	Timing        Money  `json:"timing" doc:"Bills expected in the window not paid by its end, and invoices or bills expected after it that settled inside it."`
	Unplanned     Money  `json:"unplanned" doc:"The rest: spend and receipts the projection did not hold, recurring occurrences and batches, and anything else that moved cash. The three causes add to the difference exactly."`
}

type forecastAccuracyInput struct {
	Weeks int `query:"weeks" enum:"4,13" doc:"Compare at 4 or 13 weeks; both when left out." example:"4"`
}

type forecastAccuracyOutput struct {
	Body struct {
		Items []ForecastAccuracyRow `json:"items" nullable:"false" doc:"The kept projections whose window has ended, newest first, at most 52 weeks back."`
		Note  string                `json:"note" doc:"How the causes are worked out."`
	}
}

const accuracyNote = "A kept projection is compared with the cash at the end of its fourth and thirteenth week. Receipts later than expected are invoices expected in the window and not settled by its end; timing is bills not paid by its end and invoices or bills settled earlier than expected; unplanned is the rest, so the three add to the difference exactly. Only invoices and bills have a settlement day: recurring occurrences and payment batches fall into the unplanned part."

func registerForecastAccuracy(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetForecastAccuracy, huma.Operation{
		OperationID: "getForecastAccuracy", Method: http.MethodGet, Path: "/v1/forecast-accuracy", Tags: []string{"Planning"},
		Summary: "Compare the kept cash projections with the cash that came",
		Description: "Each week the worker keeps the 13-week short-term projection (FR-PLN-09). This compares each with the money accounts' balance 4 and 13 weeks later, once that week has ended, and splits the difference by cause: " +
			"receipts later than expected, timing and unplanned (FR-PLN-04). Read-only; no side effects. A projection is never recalculated: it is the one that was kept.",
	}, append([]string{CodeInvalid}, locks...), func(ctx context.Context, in *forecastAccuracyInput) (*forecastAccuracyOutput, error) {
		p, _ := authn.From(ctx)
		out := &forecastAccuracyOutput{}
		out.Body.Items, out.Body.Note = []ForecastAccuracyRow{}, accuracyNote
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			snaps, err := u.CashSnapshots().List(ctx)
			if err != nil {
				return err
			}
			accounts, err := u.Accounts().List(ctx)
			if err != nil {
				return err
			}
			var ar, ap []uuid.UUID
			for _, ac := range accounts {
				switch string(ac.Role) {
				case ledger.RoleAccountsReceivable:
					ar = append(ar, ac.ID)
				case ledger.RoleAccountsPayable:
					ap = append(ap, ac.ID)
				}
			}
			horizons := forecastaccuracy.Horizons
			if in.Weeks != 0 {
				horizons = []int{in.Weeks}
			}
			since := today.AddDate(0, 0, -52*7)
			for i := len(snaps) - 1; i >= 0; i-- {
				s := snaps[i]
				if s.WeekOf.Before(since) {
					break
				}
				var proj forecastaccuracy.Projection
				if err := json.Unmarshal(s.Projection, &proj); err != nil {
					return err
				}
				for _, weeks := range horizons {
					if weeks > len(proj.Weekly) {
						continue
					}
					end, err := time.Parse(time.DateOnly, proj.Weekly[weeks-1].To)
					if err != nil || end.After(today.AddDate(0, 0, -1)) { // the week has not ended
						continue
					}
					settled := map[string]time.Time{}
					for _, acc := range ar {
						ss, err := receivables.Settlements(ctx, u, base, acc, s.TakenOn, end)
						if err != nil {
							return err
						}
						for _, x := range ss {
							settled[x.DocumentID.String()] = x.On
						}
					}
					for _, acc := range ap {
						ss, err := payables.Settlements(ctx, u, base, acc, s.TakenOn, end)
						if err != nil {
							return err
						}
						for _, x := range ss {
							settled[x.DocumentID.String()] = x.On
						}
					}
					cash, err := banking.Cash(ctx, u, end)
					if err != nil {
						return err
					}
					r, err := forecastaccuracy.Compare(proj, weeks, cash.Total.String(), settled)
					if err != nil {
						return err
					}
					c := s.Currency
					out.Body.Items = append(out.Body.Items, ForecastAccuracyRow{SnapshotID: s.ID.String(), TakenOn: dateOf(s.TakenOn), Weeks: weeks, WindowEnd: r.WindowEnd,
						Projected: Money{r.Projected, c}, Actual: Money{r.Actual, c}, Difference: Money{r.Difference, c}, ReceiptsLater: Money{r.Causes.ReceiptsLater, c},
						Timing: Money{r.Causes.Timing, c}, Unplanned: Money{r.Causes.Unplanned, c}})
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		slices.SortStableFunc(out.Body.Items, func(x, y ForecastAccuracyRow) int {
			if x.TakenOn != y.TakenOn {
				if x.TakenOn > y.TakenOn {
					return -1
				}
				return 1
			}
			return x.Weeks - y.Weeks
		})
		return out, nil
	})
}
