package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/forecasts"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The funding plan of a forecast (FR-PLN-07; p2-reporting-planning task 6.2 and design D12): each planned equity round, taken through the equity register's round
// model over the cap table the plan leaves the day before it, starting from the register's cap table of today. A sandbox: nothing is posted or saved.

// FundingPlanHolder is a holder's part of the company fully diluted before and after a planned round.
type FundingPlanHolder struct {
	HolderID   string `json:"holder_id,omitempty" format:"uuid" doc:"The holder; absent for the new investors the plan does not name." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4c11"`
	HolderName string `json:"holder_name" doc:"Their name, or New investors." example:"Lion Rock Ventures Pte. Ltd."`
	Before     string `json:"before" doc:"Their percentage fully diluted before the round, to 2 decimals (RND-DISPLAY); empty when they held nothing." example:"70.00"`
	After      string `json:"after" doc:"Their percentage fully diluted after the round." example:"56.00"`
	PointsLost string `json:"points_lost" doc:"The percentage points the round took from them, exact, to 2 decimals; negative for a gain." example:"14.00"`
	Shares     string `json:"shares_after" doc:"Their shares fully diluted after the round." example:"700000"`
}

// FundingPlanRound is a planned equity round as the round model works it out.
type FundingPlanRound struct {
	Row       int                 `json:"row" doc:"The funding row of the forecast it is." example:"0"`
	Month     string              `json:"month" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month its money arrives; its first day is the round's day." example:"2027-06"`
	Planned   Money               `json:"planned" doc:"The money planned."`
	PreMoney  *Money              `json:"pre_money,omitempty" doc:"The pre-money valuation planned."`
	Modelled  bool                `json:"modelled" doc:"False when the round could not be modelled, which why says; the cap table passes through it unchanged." example:"true"`
	Why       string              `json:"why,omitempty" doc:"Why it was not modelled." example:"No pre-money valuation is planned, so the round has no price to model: add one to the funding row"`
	Price     *Money              `json:"price_per_share,omitempty" doc:"The price per share: the pre-money valuation divided by the shares fully diluted before, cut down to 4 decimals."`
	NewShares string              `json:"new_shares,omitempty" doc:"The shares the money buys, whole shares of the price (RND-SHARE)." example:"250000"`
	Raised    *Money              `json:"raised,omitempty" doc:"What those shares cost: at most the money planned, the rest buying no whole share."`
	PostMoney *Money              `json:"post_money,omitempty" doc:"The post-money valuation: the shares fully diluted after, at the price."`
	Holders   []FundingPlanHolder `json:"holders" nullable:"false" doc:"Each holder before and after, then the new investors; empty when the round was not modelled."`
}

// FundingPlanOther is a funding row the plan lists but does not model.
type FundingPlanOther struct {
	Row    int    `json:"row" doc:"The funding row of the forecast it is." example:"1"`
	Type   string `json:"type" enum:"SAFE,Loan,Grant" doc:"Its kind." example:"Loan"`
	Month  string `json:"month" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month its money arrives." example:"2027-02"`
	Amount Money  `json:"amount" doc:"The amount."`
	Why    string `json:"why" doc:"Why the cap table does not change by it." example:"A loan or a grant issues no shares"`
}

// FundingPlan is a forecast's funding plan taken through the round model.
type FundingPlan struct {
	ForecastID string             `json:"forecast_id" format:"uuid" doc:"The forecast." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c6a01"`
	Currency   string             `json:"currency" doc:"The base currency every amount is in." example:"SGD"`
	CapTableOn string             `json:"cap_table_on" format:"date" doc:"The day of the register's cap table the plan starts from: the organisation's today." example:"2026-11-21"`
	Diluted    string             `json:"shares_fully_diluted" doc:"The shares fully diluted on that day." example:"1000000"`
	Rounds     []FundingPlanRound `json:"rounds" nullable:"false" doc:"The planned equity rounds in the order of their days; each dilutes the holders as the one before left them."`
	Other      []FundingPlanOther `json:"other" nullable:"false" doc:"The funding rows that issue no shares in the plan: SAFEs, loans and grants."`
	Notes      []string           `json:"notes" nullable:"false" doc:"What the plan does not model."`
}

// TransformSchema implements huma.SchemaTransformer: an example for each note (NFR-API-01).
func (FundingPlan) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"notes": "A SAFE converts at the next priced round under its cap and discount, which a funding row does not hold: it is listed, not modelled [accountant to confirm]."})
	return s
}

type fundingPlanOutput struct{ Body FundingPlan }

func registerFundingPlan(a *API) {
	register(a, authz.GetForecastFundingPlan, huma.Operation{
		OperationID: "getForecastFundingPlan", Method: http.MethodGet, Path: "/v1/forecasts/{forecast_id}/funding-plan", Tags: []string{"Planning"},
		Summary: "Model a forecast's planned equity rounds on the cap table",
		Description: "A sandbox (FR-PLN-07): each Equity funding row of the forecast that has a pre-money valuation, taken in the order of its days through the equity register's round model " +
			"(FR-EQ-21) over the cap table fully diluted the day before, starting from the register's cap table of today: the pre-money valuation divided by the shares fully diluted before the " +
			"round gives its price (cut down to 4 decimals), the money buys whole shares of it, and each holder's part before and after gives the dilution in percentage points. A round with " +
			"no valuation, or one the model refuses, is listed with why; SAFEs, loans and grants are listed apart, as they issue no shares in the plan. The rows are the forecast's own, before any " +
			"scenario's levers. It reads the whole cap table, so it needs the right to. Nothing is posted or saved. Read-only.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *ForecastPath) (*fundingPlanOutput, error) {
		id, err := parseID("forecast_id", in.ForecastID)
		if err != nil {
			return nil, err
		}
		pr, _ := authn.From(ctx)
		// It shows a forecast's funding rows beside every holder's ownership, so it needs the right to read the forecast as well as the whole cap table.
		if !authz.Decide(authz.GetForecast, pr.Roles, pr.Class == authn.ClassAgent).Allowed {
			return nil, problem(CodeForbidden, "Your roles may read the cap table but not forecasts, and the funding plan shows both.", "Ask the Owner, the Controller or the Accountant for the funding plan")
		}
		var out FundingPlan
		err = a.deps.Store.Do(ctx, pr.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			if doc.Type != storage.DocumentForecast {
				return ports.ErrNotFound
			}
			plan, err := forecasts.Read(doc)
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
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			before, err := equity.TableIn(ctx, u, today)
			if err != nil {
				return err
			}
			out = FundingPlan{ForecastID: id.String(), Currency: base.Code, CapTableOn: dateOf(today), Diluted: before.FullyDiluted.String(), Rounds: []FundingPlanRound{},
				Other: []FundingPlanOther{}, Notes: []string{"A SAFE converts at the next priced round under its cap and discount, which a funding row does not hold: it is listed, not modelled [accountant to confirm].",
					"The options the pool still holds count in the shares fully diluted; no pool top-up is planned."}}
			var planned []equity.PlannedRound
			for i, f := range plan.Funding {
				month, err := time.Parse("2006-01", f.Month)
				if err != nil {
					return err
				}
				amount, err := money.ParseField(f.Amount, base, "amount")
				if err != nil {
					return err
				}
				switch f.Type {
				case "Equity":
					r := equity.PlannedRound{Row: i, On: month, Amount: amount}
					if f.PreMoney != "" {
						pm, err := money.ParseField(f.PreMoney, base, "pre_money")
						if err != nil {
							return err
						}
						r.PreMoney = &pm
					}
					planned = append(planned, r)
				default:
					why := "A loan or a grant issues no shares"
					if f.Type == "SAFE" {
						why = "A SAFE converts at the next priced round under its cap and discount: not modelled"
					}
					out.Other = append(out.Other, FundingPlanOther{Row: i, Type: f.Type, Month: f.Month, Amount: moneyOut(amount), Why: why})
				}
			}
			sort.SliceStable(out.Other, func(i, j int) bool { return out.Other[i].Month < out.Other[j].Month })
			results, err := equity.PlanFunding(before, planned)
			if err != nil {
				return err
			}
			v, err := a.capViewOf(ctx, u)
			if err != nil {
				return err
			}
			for _, res := range results {
				round := FundingPlanRound{Row: res.Round.Row, Month: res.Round.On.Format("2006-01"), Planned: moneyOut(res.Round.Amount), Modelled: res.Modelled, Why: res.Why,
					Holders: []FundingPlanHolder{}}
				if res.Round.PreMoney != nil {
					pm := moneyOut(*res.Round.PreMoney)
					round.PreMoney = &pm
				}
				if res.Modelled {
					oc := res.Outcome
					price, raised, post := moneyOut(oc.PreMoney), moneyOut(res.Raised), moneyOut(oc.PostMoney)
					_ = price
					price = Money{Amount: oc.Price.String(), Currency: base.Code}
					round.Price, round.Raised, round.PostMoney = &price, &raised, &post
					round.NewShares = oc.NewShares.String()
					for _, h := range oc.Holders {
						view := FundingPlanHolder{Before: h.DilutedShareBefore, After: h.DilutedShareAfter, PointsLost: h.Drop, Shares: h.DilutedAfter.String(), HolderName: "New investors"}
						if h.HolderID != equity.NewInvestors {
							view.HolderID = h.HolderID.String()
							if view.HolderName, err = v.name(ctx, u, h.HolderID); err != nil {
								return err
							}
						}
						round.Holders = append(round.Holders, view)
					}
					sort.SliceStable(round.Holders, func(i, j int) bool { return round.Holders[i].HolderName < round.Holders[j].HolderName })
				}
				out.Rounds = append(out.Rounds, round)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &fundingPlanOutput{Body: out}, nil
	})
}
