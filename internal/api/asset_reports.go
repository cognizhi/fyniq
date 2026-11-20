package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/capallow"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The asset reports (FR-FA-04; p2-reporting-planning task 5.2): the movement schedule of cost, accumulated depreciation and net book value for the financial statements, tied to the
// ledger; and the tax capital allowance schedule of a year of assessment from the pack sg-capital-allowances.

// MovementAsset is one asset's line of the movement schedule: cost and accumulated depreciation at the start, what moved, and at the end.
type MovementAsset struct {
	AssetID             string `json:"asset_id,omitempty" format:"uuid" doc:"The asset; absent on a total." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5fa1"`
	Number              string `json:"number,omitempty" doc:"Its number in the register." example:"FA-0001"`
	Name                string `json:"name" doc:"Its name, or Total." example:"MacBook Pro 14, Wei Ling"`
	PurchasedOn         string `json:"purchased_on,omitempty" format:"date" doc:"The day it was bought." example:"2026-03-14"`
	OpeningCost         Money  `json:"opening_cost" doc:"Its cost on the day before the run."`
	Additions           Money  `json:"additions" doc:"The cost of it, when it was bought inside the run."`
	Disposals           Money  `json:"disposals" doc:"The cost a disposal inside the run took off it."`
	ClosingCost         Money  `json:"closing_cost" doc:"opening cost + additions - disposals, exactly."`
	OpeningAccumulated  Money  `json:"opening_accumulated" doc:"Its accumulated depreciation on the day before the run."`
	Charge              Money  `json:"charge" doc:"The depreciation the run posted, net of its reversals."`
	DisposedAccumulated Money  `json:"disposed_accumulated" doc:"The accumulated depreciation a disposal inside the run took off it."`
	ClosingAccumulated  Money  `json:"closing_accumulated" doc:"opening accumulated + charge - disposals, exactly."`
	OpeningNBV          Money  `json:"opening_nbv" doc:"Opening cost less opening accumulated depreciation."`
	ClosingNBV          Money  `json:"closing_nbv" doc:"Closing cost less closing accumulated depreciation."`
}

// MovementCategory is an asset account's assets and their totals.
type MovementCategory struct {
	AccountID   string          `json:"account_id" format:"uuid" doc:"The asset account: its category." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c1510"`
	AccountCode string          `json:"account_code" doc:"Its code." example:"1510"`
	AccountName string          `json:"account_name" doc:"Its name." example:"Computer equipment"`
	Assets      []MovementAsset `json:"assets" nullable:"false" doc:"Its assets, by number."`
	Total       MovementAsset   `json:"total" doc:"The totals of its assets."`
}

// MovementTie is the register tied to the ledger at the end of the run (INV-07).
type MovementTie struct {
	AccountCode string `json:"account_code" doc:"The ledger account: an asset account of the register, or 1590 Accumulated depreciation." example:"1510"`
	AccountName string `json:"account_name" doc:"Its name." example:"Computer equipment"`
	Ledger      Money  `json:"ledger" doc:"Its balance at the end of the run, from the balance service (ENG-04), read positive."`
	Register    Money  `json:"register" doc:"What the register and the purchases not in it yet account for."`
	Unexplained Money  `json:"unexplained" doc:"What is left over: zero when it ties."`
}

// AssetMovementReport is the movement schedule of the register over a run of months (FR-FA-04).
type AssetMovementReport struct {
	Currency   string             `json:"currency" doc:"The base currency every amount is in." example:"SGD"`
	From       string             `json:"from" format:"date" doc:"The run's first day." example:"2026-01-01"`
	To         string             `json:"to" format:"date" doc:"The run's last day." example:"2026-12-31"`
	Categories []MovementCategory `json:"categories" nullable:"false" doc:"One per asset account that holds an asset."`
	Total      MovementAsset      `json:"total" doc:"The totals of every asset."`
	Ties       []MovementTie      `json:"ties" nullable:"false" doc:"The register against the ledger at the end of the run."`
	Tied       bool               `json:"tied" doc:"True when every account ties: nothing is unexplained." example:"true"`
}

type movementInput struct {
	From string `query:"from" required:"true" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's first month, YYYY-MM." example:"2026-01"`
	To   string `query:"to" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The run's last month, YYYY-MM; left out, the first month's." example:"2026-12"`
}

type movementOutput struct{ Body AssetMovementReport }

// CapitalAllowanceRow is one asset's line of the capital allowance schedule.
type CapitalAllowanceRow struct {
	AssetID        string `json:"asset_id" format:"uuid" doc:"The asset." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5fa1"`
	Number         string `json:"number,omitempty" doc:"Its number in the register." example:"FA-0001"`
	Name           string `json:"name" doc:"Its name." example:"MacBook Pro 14, Wei Ling"`
	PurchasedOn    string `json:"purchased_on" format:"date" doc:"The day it was bought." example:"2026-03-14"`
	Cost           Money  `json:"cost" doc:"The cost it was bought for."`
	Method         string `json:"method" enum:"s19,s19a1,s19a10a" doc:"The method elected." example:"s19a1"`
	WorkingLife    int    `json:"working_life_years,omitempty" doc:"Section 19: the years elected." example:"6"`
	FirstYA        int    `json:"first_ya" doc:"The year of assessment of its first claim: the year after the financial year it was bought in ends." example:"2027"`
	BroughtForward Money  `json:"brought_forward" doc:"The tax written down value brought forward: its cost less the allowances of the years before."`
	Initial        Money  `json:"initial_allowance" doc:"Section 19 only, in the first year: 20% of the cost."`
	Annual         Money  `json:"annual_allowance" doc:"The rest of the year's allowance."`
	Allowance      Money  `json:"allowance" doc:"The year's allowance: initial + annual."`
	CarriedForward Money  `json:"carried_forward" doc:"brought forward - allowance."`
}

// CapitalAllowanceApart is an asset the schedule cannot place.
type CapitalAllowanceApart struct {
	AssetID     string `json:"asset_id" format:"uuid" doc:"The asset." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5fa1"`
	Number      string `json:"number,omitempty" doc:"Its number in the register." example:"FA-0001"`
	Name        string `json:"name" doc:"Its name." example:"MacBook Pro 14, Wei Ling"`
	PurchasedOn string `json:"purchased_on" format:"date" doc:"The day it was bought." example:"2026-03-14"`
	Cost        Money  `json:"cost" doc:"The cost it was bought for."`
	Reason      string `json:"reason" enum:"no_election,before_pack,disposed,over_cap,bad_life,not_sgd" doc:"Why it is listed apart: no_election, the company has not elected a method; before_pack, acquired in a basis period before YA 2023; disposed, a disposal stands and its balancing allowance or charge is the tax agent's; over_cap, elected under section 19A(10A) but over S$5,000 or past the year's S$30,000; bad_life, a working life the prescribed life does not allow; not_sgd, the base currency is not Singapore dollars." example:"no_election"`
	Fix         string `json:"fix" doc:"What to do." example:"Elect a method on the asset: section 19, 19A(1) or 19A(10A)"`
}

// CapitalAllowanceTotals are the schedule's columns' sums.
type CapitalAllowanceTotals struct {
	BroughtForward Money `json:"brought_forward" doc:"The sum of the tax written down values brought forward."`
	Initial        Money `json:"initial_allowance" doc:"The sum of the initial allowances."`
	Annual         Money `json:"annual_allowance" doc:"The sum of the annual allowances."`
	Allowance      Money `json:"allowance" doc:"The total claim of the year: initial + annual."`
	CarriedForward Money `json:"carried_forward" doc:"The sum of the tax written down values carried forward."`
}

// CapitalAllowanceSchedule is the tax capital allowance schedule of a year of assessment (FR-FA-04).
type CapitalAllowanceSchedule struct {
	Currency string                  `json:"currency" doc:"The base currency every amount is in." example:"SGD"`
	YA       int                     `json:"ya" doc:"The year of assessment." example:"2027"`
	Pack     string                  `json:"pack" doc:"The sg-capital-allowances version the rules come from." example:"sg-capital-allowances@2026.1"`
	Rows     []CapitalAllowanceRow   `json:"rows" nullable:"false" doc:"The assets claiming in the year, by purchase date."`
	Apart    []CapitalAllowanceApart `json:"apart" nullable:"false" doc:"The assets the schedule cannot place, listed apart and never guessed."`
	Totals   CapitalAllowanceTotals  `json:"totals" doc:"The columns' sums."`
	Notes    []string                `json:"notes" nullable:"false" doc:"What the schedule does not work out, [accountant to confirm]."`
}

// TransformSchema implements huma.SchemaTransformer: an example for each note (NFR-API-01).
func (CapitalAllowanceSchedule) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"notes": "Amounts are in cents, each year's share by ALLOC-EVEN [accountant to confirm]."})
	return s
}

type capitalAllowanceInput struct {
	YA int `query:"ya" required:"true" minimum:"2020" maximum:"2100" doc:"The year of assessment, such as 2027 for the financial year that ends in 2026." example:"2027"`
}

type capitalAllowanceOutput struct{ Body CapitalAllowanceSchedule }

var capitalAllowanceReasons = map[string]string{
	capallow.ReasonNoElection: "Elect a method on the asset: section 19, 19A(1) or 19A(10A)",
	capallow.ReasonBeforePack: "Bought in a basis period before YA 2023: the tax agent works its allowance",
	capallow.ReasonDisposed:   "Its balancing allowance or charge is the tax agent's",
	capallow.ReasonOverCap:    "Elect section 19A(1) or section 19 for it, or leave it for the tax agent: section 19A(10A) takes only assets of at most S$5,000 within S$30,000 a year",
	capallow.ReasonBadLife:    "Elect a working life the Sixth Schedule's life allows: 6 or 12, or 16 for a prescribed life of 16",
	capallow.ReasonNotSGD:     "The capital allowance rules are in Singapore dollars",
}

// financialYears are the years a year of assessment is read from: the organisation's own, or, while its books start and first year end are not set, twelve-month years
// starting in its financial year's start month.
func financialYears(p storage.OrganisationProfile) period.Calendar {
	if c, err := period.CalendarOf(p); err == nil {
		return c
	}
	start := time.Date(2000, time.Month(max(1, p.FYStartMonth)), 1, 0, 0, 0, 0, time.UTC)
	return period.Calendar{BooksStart: start, FirstYearEnd: start.AddDate(1, 0, -1)}
}

// checkTaxElection refuses a section 19 election of years the pack does not allow for the asset's prescribed life (the Sixth Schedule's), for assets acquired in the basis period
// of the year of assessment of its purchase.
func checkTaxElection(ctx context.Context, u storage.UnitOfWork, today time.Time, doc storage.Document, t *asset.TaxElection) error {
	if t == nil || t.Method != asset.TaxS19 {
		return nil
	}
	x, err := asset.Read(doc)
	if err != nil {
		return err
	}
	o, err := u.Organisation(ctx)
	if err != nil {
		return err
	}
	cal := financialYears(o.Profile)
	ya := capallow.YearOfAssessment(cal, x.PurchasedOn)
	v, ok, err := gst.PackOn(ctx, u, "sg-capital-allowances", today)
	if err != nil {
		return err
	}
	if !ok {
		return &asset.Error{Code: asset.CodeTaxMethod, Message: "no capital allowance rules are in force", Fix: "Use a release that carries the capital allowance pack"}
	}
	allowed := pack.WorkingLifeElections(v, t.PrescribedYears, ya)
	if !slices.Contains(allowed, t.Years) {
		msg := "a prescribed life of " + strconv.Itoa(t.PrescribedYears) + " years has no working life to elect"
		if len(allowed) > 0 {
			parts := make([]string, len(allowed))
			for i, n := range allowed {
				parts[i] = strconv.Itoa(n)
			}
			msg = "a prescribed life of " + strconv.Itoa(t.PrescribedYears) + " years may be elected over " + strings.Join(parts, ", ") + " years, not " + strconv.Itoa(t.Years)
		}
		return &asset.Error{Code: asset.CodeTaxMethod, Message: msg, Fix: "Use the prescribed life the Sixth Schedule gives the asset (6, 12 or 16) and one of the years it allows"}
	}
	return nil
}

func registerAssetReports(a *API) {
	register(a, authz.GetFixedAssetMovement, huma.Operation{
		OperationID: "getFixedAssetMovement", Method: http.MethodGet, Path: "/v1/reports/fixed-asset-movement", Tags: []string{"Fixed assets"},
		Summary: "Get the fixed asset movement schedule",
		Description: "For a run of months (FR-FA-04): each registered asset's cost and accumulated depreciation on the day before the run, the cost added by an asset bought inside it, the " +
			"depreciation it charged (net of reversals), what a disposal took off, and the cost and accumulated depreciation at the end, with the net book value; by asset account, " +
			"so that closing = opening + additions - disposals and closing accumulated = opening + charge - disposals, exactly. The register is tied to the ledger at the end of the run " +
			"through the balance service (INV-07): what the register does not explain is shown as unexplained, never hidden. Read-only; no side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *movementInput) (*movementOutput, error) {
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
		var out AssetMovementReport
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			mv, err := journal.FixedAssetMovement(ctx, u, first, end)
			if err != nil {
				return err
			}
			accounts, err := u.Accounts().List(ctx)
			if err != nil {
				return err
			}
			zero := moneyOut(money.Zero(base))
			rowOf := func(r journal.MovementRow) MovementAsset {
				return MovementAsset{AssetID: r.AssetID.String(), Number: r.Number, Name: r.Name, PurchasedOn: dateOf(r.PurchasedOn), OpeningCost: moneyOut(r.OpeningCost), Additions: moneyOut(r.Additions),
					Disposals: moneyOut(r.DisposedCost), ClosingCost: moneyOut(r.ClosingCost), OpeningAccumulated: moneyOut(r.OpeningAcc), Charge: moneyOut(r.Charge),
					DisposedAccumulated: moneyOut(r.DisposedAcc), ClosingAccumulated: moneyOut(r.ClosingAcc), OpeningNBV: moneyOut(r.NBVOpening()), ClosingNBV: moneyOut(r.NBVClosing())}
			}
			sum := func(rows []journal.MovementRow) (MovementAsset, error) {
				t := journal.MovementRow{OpeningCost: money.Zero(base), Additions: money.Zero(base), DisposedCost: money.Zero(base), ClosingCost: money.Zero(base),
					OpeningAcc: money.Zero(base), Charge: money.Zero(base), DisposedAcc: money.Zero(base), ClosingAcc: money.Zero(base)}
				for _, r := range rows {
					for _, f := range []struct {
						into *money.Money
						x    money.Money
					}{{&t.OpeningCost, r.OpeningCost}, {&t.Additions, r.Additions}, {&t.DisposedCost, r.DisposedCost}, {&t.ClosingCost, r.ClosingCost},
						{&t.OpeningAcc, r.OpeningAcc}, {&t.Charge, r.Charge}, {&t.DisposedAcc, r.DisposedAcc}, {&t.ClosingAcc, r.ClosingAcc}} {
						s, err := f.into.Add(f.x)
						if err != nil {
							return MovementAsset{}, err
						}
						*f.into = s
					}
				}
				m := rowOf(t)
				m.AssetID, m.PurchasedOn, m.Name = "", "", "Total"
				return m, nil
			}
			out = AssetMovementReport{Currency: base.Code, From: dateOf(first), To: dateOf(end), Categories: []MovementCategory{}, Ties: []MovementTie{}, Tied: true}
			var order []string
			byAccount := map[string][]journal.MovementRow{}
			for _, r := range mv.Rows {
				k := r.AccountID.String()
				if _, ok := byAccount[k]; !ok {
					order = append(order, k)
				}
				byAccount[k] = append(byAccount[k], r)
			}
			for _, k := range order {
				rows := byAccount[k]
				c := MovementCategory{AccountID: k, Assets: []MovementAsset{}}
				for _, acc := range accounts {
					if acc.ID.String() == k {
						c.AccountCode, c.AccountName = acc.Code, acc.Name
					}
				}
				for _, r := range rows {
					c.Assets = append(c.Assets, rowOf(r))
				}
				if c.Total, err = sum(rows); err != nil {
					return err
				}
				out.Categories = append(out.Categories, c)
			}
			slices.SortStableFunc(out.Categories, func(x, y MovementCategory) int { return strings.Compare(x.AccountCode, y.AccountCode) })
			if out.Total, err = sum(mv.Rows); err != nil {
				return err
			}
			_ = zero
			for _, r := range mv.Register {
				t := MovementTie{AccountCode: r.Account.Code, AccountName: r.Account.Name, Ledger: moneyOut(r.Reconciliation.Base.Control), Register: moneyOut(r.Reconciliation.Base.SubLedger),
					Unexplained: moneyOut(r.Reconciliation.Base.Unexplained)}
				out.Ties = append(out.Ties, t)
				if r.Reconciliation.Unexplained() {
					out.Tied = false
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &movementOutput{Body: out}, nil
	})

	register(a, authz.GetCapitalAllowanceSchedule, huma.Operation{
		OperationID: "getCapitalAllowanceSchedule", Method: http.MethodGet, Path: "/v1/reports/capital-allowances", Tags: []string{"Fixed assets"},
		Summary: "Get the tax capital allowance schedule of a year of assessment",
		Description: "For a year of assessment (FR-FA-04): each registered asset's allowance under the method the company elected for it (changeFixedAsset's tax_election), from the " +
			"pack sg-capital-allowances: section 19 (an initial allowance of 20% of the cost in the first year, and annual allowances of the other 80% over the elected working life of " +
			"6, 12 or 16 years), section 19A(1) (a third a year over 3 years) and section 19A(10A) (the whole cost in one year, for an asset of at most S$5,000, the year's claims within " +
			"S$30,000, taken whole in the order of purchase), with the tax written down value brought and carried forward. An asset the schedule cannot place (none elected, a disposal " +
			"stands, bought before YA 2023, a working life that is not allowed, over the cap) is listed apart with why, never guessed. The year of assessment of a purchase is the year " +
			"after the financial year it was bought in ends. Amounts are rounded to cents by ALLOC-EVEN, IRAS shows whole dollars [accountant to confirm]. Read-only; no side effects.",
	}, []string{CodeInvalid}, func(ctx context.Context, in *capitalAllowanceInput) (*capitalAllowanceOutput, error) {
		p, _ := authn.From(ctx)
		var out CapitalAllowanceSchedule
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			cal := financialYears(o.Profile)
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			v, ok, err := gst.PackOn(ctx, u, "sg-capital-allowances", today)
			if err != nil {
				return err
			}
			if !ok {
				return invalid([]FieldProblem{{Code: CodeInvalid, Field: "ya", Message: "no sg-capital-allowances version is in force today", Fix: "Use a release that carries the capital allowance pack"}})
			}
			assets, err := journal.TaxAssets(ctx, u)
			if err != nil {
				return err
			}
			in2 := make([]capallow.Asset, len(assets))
			for i, x := range assets {
				in2[i] = capallow.Asset{ID: x.AssetID, Number: x.Number, Name: x.Name, PurchasedOn: x.PurchasedOn, Cost: x.Cost, Election: x.Election, Disposed: x.Disposed}
			}
			s, err := capallow.Work(v, cal, base, in.YA, in2)
			if err != nil {
				return err
			}
			out = CapitalAllowanceSchedule{Currency: base.Code, YA: in.YA, Pack: s.Pack, Rows: []CapitalAllowanceRow{}, Apart: []CapitalAllowanceApart{},
				Totals: CapitalAllowanceTotals{BroughtForward: moneyOut(s.Totals.BroughtFw), Initial: moneyOut(s.Totals.Initial), Annual: moneyOut(s.Totals.Annual),
					Allowance: moneyOut(s.Totals.Allowance), CarriedForward: moneyOut(s.Totals.CarriedFw)},
				Notes: []string{
					"The allowances are worked for assets bought for cash; hire purchase, the 100% write-off of computers and prescribed automation equipment under section 19A(2), deferred claims, tax written down values brought forward into a one-year write-off and balancing allowances and charges are the tax agent's [accountant to confirm].",
					"Amounts are in cents, each year's share by ALLOC-EVEN; IRAS's own examples show whole dollars [accountant to confirm].",
				}}
			for _, r := range s.Rows {
				out.Rows = append(out.Rows, CapitalAllowanceRow{AssetID: r.Asset.ID.String(), Number: r.Asset.Number, Name: r.Asset.Name, PurchasedOn: dateOf(r.Asset.PurchasedOn),
					Cost: moneyOut(r.Asset.Cost), Method: r.Method, WorkingLife: r.Years, FirstYA: r.FirstYA, BroughtForward: moneyOut(r.BroughtFw), Initial: moneyOut(r.Initial),
					Annual: moneyOut(r.Annual), Allowance: moneyOut(r.Allowance), CarriedForward: moneyOut(r.CarriedFw)})
			}
			for _, x := range s.Apart {
				out.Apart = append(out.Apart, CapitalAllowanceApart{AssetID: x.Asset.ID.String(), Number: x.Asset.Number, Name: x.Asset.Name, PurchasedOn: dateOf(x.Asset.PurchasedOn),
					Cost: moneyOut(x.Asset.Cost), Reason: x.Reason, Fix: capitalAllowanceReasons[x.Reason]})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &capitalAllowanceOutput{Body: out}, nil
	})
}
