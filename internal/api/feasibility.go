package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/feasibility"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/planning"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ feasibility studies (FR-PLN-06, FR-PLN-08)

// StudyAmount is one of a study's figures, in the base currency.
type StudyAmount string

// Schema implements huma.SchemaProvider.
func (StudyAmount) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "An amount in the base currency, at its minor units (RND-PLAN).", Examples: []any{"75446.46"}}
}

// FeasibilityStudyView is a study as the API shows it.
type FeasibilityStudyView struct {
	ID      string                `json:"id" format:"uuid" doc:"The study's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9e01"`
	Version int                   `json:"version" doc:"The version of its content; it makes the ETag." example:"1"`
	Date    string                `json:"date" format:"date" doc:"The study's date, its first version's: the tax rate is sg-cit's headline rate then." example:"2026-11-08"`
	Study   feasibility.StudyPlan `json:"study" doc:"Its inputs, as the workbook's sheet."`
}

// FeasibilityStudyPage is a page of studies, the latest first.
type FeasibilityStudyPage struct {
	Items      []FeasibilityStudyView `json:"items" nullable:"false"`
	NextCursor string                 `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

// FeasibilityYear is one year of a study's figures; year 0 holds the investment alone.
type FeasibilityYear struct {
	Year                 int         `json:"year" doc:"0 for the investment, then each year of the period." example:"1"`
	Units                string      `json:"units" doc:"Units sold, to 1 decimal: they stay fractional as they grow." example:"40.0"`
	Revenue              StudyAmount `json:"revenue"`
	VariableCosts        StudyAmount `json:"variable_costs"`
	FixedCosts           StudyAmount `json:"fixed_costs"`
	Depreciation         StudyAmount `json:"depreciation"`
	EBIT                 StudyAmount `json:"ebit"`
	Tax                  StudyAmount `json:"tax" doc:"At the study's tax rate; below zero when a loss saves tax."`
	NOPAT                StudyAmount `json:"nopat"`
	WorkingCapital       StudyAmount `json:"working_capital"`
	WorkingCapitalChange StudyAmount `json:"working_capital_change"`
	Salvage              StudyAmount `json:"salvage" doc:"The last year: the salvage less the tax on its gain over the book value."`
	Release              StudyAmount `json:"working_capital_release" doc:"The last year: the working capital released."`
	CashFlow             StudyAmount `json:"cash_flow"`
	PresentValue         StudyAmount `json:"present_value"`
	Cumulative           StudyAmount `json:"cumulative"`
	CumulativePV         StudyAmount `json:"cumulative_present_value"`
}

// FeasibilitySensitivity is one row of the sensitivity grid: the NPV at a sales volume, against fixed costs at 80%, 90%,
// 100%, 110% and 120%.
type FeasibilitySensitivity struct {
	Volume         string        `json:"volume" doc:"The sales volume as a multiple of the study's." example:"0.8"`
	NPVByFixedCost []StudyAmount `json:"npv_by_fixed_costs" nullable:"false" doc:"The NPV with fixed costs at 0.8, 0.9, 1, 1.1 and 1.2 of the study's."`
}

// FeasibilityRate is the NPV at a discount rate.
type FeasibilityRate struct {
	Rate string      `json:"rate" doc:"The discount rate, a fraction." example:"0.12"`
	NPV  StudyAmount `json:"npv"`
}

// FeasibilityResults are a study's figures, worked out on every read (FR-PLN-06, design D8).
type FeasibilityResults struct {
	StudyID            string                   `json:"study_id" format:"uuid" doc:"The study." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9e01"`
	Currency           string                   `json:"currency" doc:"The base currency." example:"SGD"`
	TaxRate            string                   `json:"tax_rate" doc:"sg-cit's headline rate at the study's date, fixed for its years." example:"0.17"`
	TaxPack            PackVersionID            `json:"tax_pack" doc:"The sg-cit version it came from."`
	Years              []FeasibilityYear        `json:"years" nullable:"false" doc:"Year 0 to the end of the period."`
	NPV                StudyAmount              `json:"npv" doc:"The present values added up."`
	IRR                string                   `json:"irr,omitempty" doc:"The internal rate of return nearest 10%, a fraction to 4 decimals; absent when there is none." example:"0.2690"`
	Payback            string                   `json:"payback_years,omitempty" doc:"Years until the running total is no longer below zero, to 1 decimal; absent when it never is in the period." example:"4.1"`
	DiscountedPayback  string                   `json:"discounted_payback_years,omitempty" doc:"The same with the present values." example:"4.6"`
	ProfitabilityIndex string                   `json:"profitability_index,omitempty" doc:"1 + NPV ÷ investment, to 2 decimals; absent without an investment." example:"1.50"`
	BreakEvenUnits     string                   `json:"break_even_units,omitempty" doc:"The first year's fixed costs ÷ (price − variable cost), to 1 decimal; absent when that margin is not above zero." example:"62.1"`
	BreakEvenRevenue   StudyAmount              `json:"break_even_revenue,omitempty" doc:"Break-even units × price."`
	MarginOfSafety     string                   `json:"margin_of_safety_percent,omitempty" doc:"(units − break-even units) ÷ units, the first year's, as a percentage (RND-PERCENT)." example:"-55.2"`
	Sensitivity        []FeasibilitySensitivity `json:"sensitivity" nullable:"false" doc:"The NPV with sales volume and fixed costs each at 80% to 120%."`
	NPVAtRates         []FeasibilityRate        `json:"npv_at_rates" nullable:"false" doc:"The NPV at 8%, 10%, 12%, 15%, 20%, 25%, 30% and 40%."`
	Verdict            string                   `json:"verdict" enum:"feasible,marginal,not_feasible,no_figures" doc:"feasible: a positive NPV and a payback within the longest acceptable; marginal: a positive NPV, but a longer payback or none; not_feasible: an NPV at or below zero; no_figures: no investment, units or price yet." example:"marginal"`
	Words              string                   `json:"verdict_words" doc:"The verdict in plain words." example:"Marginal: a positive NPV, but the payback of 4.1 years is longer than 3"`
	Note               string                   `json:"note" doc:"What the study is." example:"A study never posts: nothing here changes the books (FR-PLN-08)."`
}

type (
	FeasibilityStudyPath struct {
		StudyID string `path:"study_id" format:"uuid" doc:"The study." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9e01"`
	}
	studyListInput  struct{ PageQuery }
	studyListOutput struct{ Body FeasibilityStudyPage }
	studyDraftInput struct {
		KeyHeader
		PreferHeader
		Body feasibility.StudyPlan
	}
	studyReviseInput struct {
		FeasibilityStudyPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body feasibility.StudyPlan
	}
	studyReadOutput struct {
		ETag string `header:"ETag" doc:"Its version, for If-Match."`
		Body FeasibilityStudyView
	}
	studyWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     FeasibilityStudyView
	}
	studyResultsOutput struct{ Body FeasibilityResults }
)

// studyOf is a study document as the API shows it.
func studyOf(doc storage.Document) (FeasibilityStudyView, error) {
	p, day, err := feasibility.Read(doc)
	if err != nil {
		return FeasibilityStudyView{}, err
	}
	return FeasibilityStudyView{ID: doc.ID.String(), Version: doc.Version, Date: day.Format(time.DateOnly), Study: p}, nil
}

func studyWriteAnswer(w written[FeasibilityStudyView]) *studyWriteOutput {
	if w.Previewed {
		return &studyWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &studyWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, string(storage.DocumentDraft)), Location: "/v1/feasibility-studies/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

// verdictWords says the verdict in plain words, with the figures it rests on.
func verdictWords(r planning.StudyResult, maxPayback string) string {
	years := func(f *planning.Fraction) string {
		x, err := f.Round(1)
		if err != nil {
			return "?"
		}
		return x.Text('f')
	}
	switch r.Verdict {
	case planning.VerdictFeasible:
		irr := ""
		if r.IRR != nil {
			if pct, err := rounding.PlanQuotient(r.IRR, apd.New(1, -2), 2); err == nil { // the 4-decimal fraction as a percentage
				irr = fmt.Sprintf(", an IRR of %s%%", pct.Text('f'))
			}
		}
		return fmt.Sprintf("Feasible: a positive NPV%s, and a payback of %s years", irr, years(r.Payback))
	case planning.VerdictMarginal:
		if r.Payback == nil {
			return "Marginal: a positive NPV, but no payback within the period"
		}
		return fmt.Sprintf("Marginal: a positive NPV, but the payback of %s years is longer than %s", years(r.Payback), maxPayback)
	case planning.VerdictNotFeasible:
		return "Not feasible: the NPV is not above zero"
	}
	return "Enter the investment, the units and the price to judge it"
}

func studyResultsOf(id string, r planning.StudyResult, base money.Currency, tax *apd.Decimal, packID, maxPayback string) (FeasibilityResults, error) {
	a := func(x *apd.Decimal) StudyAmount { return StudyAmount(x.Text('f')) }
	out := FeasibilityResults{StudyID: id, Currency: base.Code, TaxRate: tax.Text('f'), TaxPack: PackVersionID(packID), NPV: a(r.NPV),
		Verdict: r.Verdict, Words: verdictWords(r, maxPayback), Note: "A study never posts: nothing here changes the books (FR-PLN-08).",
		Years: []FeasibilityYear{}, Sensitivity: []FeasibilitySensitivity{}, NPVAtRates: []FeasibilityRate{}}
	for _, y := range r.Years {
		units, err := rounding.PlanQuotient(y.Units, apd.New(1, 0), 1)
		if err != nil {
			return out, err
		}
		out.Years = append(out.Years, FeasibilityYear{Year: y.Year, Units: units.Text('f'), Revenue: a(y.Revenue), VariableCosts: a(y.Variable),
			FixedCosts: a(y.Fixed), Depreciation: a(y.Depreciation), EBIT: a(y.EBIT), Tax: a(y.Tax), NOPAT: a(y.NOPAT),
			WorkingCapital: a(y.WorkingCap), WorkingCapitalChange: a(y.WorkingCapChange), Salvage: a(y.Salvage), Release: a(y.Relief),
			CashFlow: a(y.CashFlow), PresentValue: a(y.PV), Cumulative: a(y.Cumulative), CumulativePV: a(y.CumulativePV)})
	}
	if r.IRR != nil {
		out.IRR = r.IRR.Text('f')
	}
	shown := func(f *planning.Fraction, places int) (string, error) {
		if f == nil {
			return "", nil
		}
		x, err := f.Round(places)
		if err != nil {
			return "", err
		}
		return x.Text('f'), nil
	}
	var err error
	if out.Payback, err = shown(r.Payback, 1); err != nil {
		return out, err
	}
	if out.DiscountedPayback, err = shown(r.DiscountedPayback, 1); err != nil {
		return out, err
	}
	if out.BreakEvenUnits, err = shown(r.BreakEvenUnits, 1); err != nil {
		return out, err
	}
	if r.PI != nil {
		out.ProfitabilityIndex = r.PI.Text('f')
	}
	if r.BreakEvenRevenue != nil {
		out.BreakEvenRevenue = a(r.BreakEvenRevenue)
	}
	if r.Safety != nil {
		p, err := r.Safety.Percent()
		if err != nil {
			return out, err
		}
		out.MarginOfSafety = p.Text('f')
	}
	for i, row := range r.Grid {
		s := FeasibilitySensitivity{Volume: planning.Multipliers[i], NPVByFixedCost: []StudyAmount{}}
		for _, x := range row {
			s.NPVByFixedCost = append(s.NPVByFixedCost, a(x))
		}
		out.Sensitivity = append(out.Sensitivity, s)
	}
	for i, x := range r.NPVAt {
		out.NPVAtRates = append(out.NPVAtRates, FeasibilityRate{Rate: planning.NPVRates[i], NPV: a(x)})
	}
	return out, nil
}

func registerFeasibility(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		feasibility.CodeStudy, storage.CodeFeasibility}, locks...)

	register(a, authz.ListFeasibilityStudies, huma.Operation{
		OperationID: "listFeasibilityStudies", Method: http.MethodGet, Path: "/v1/feasibility-studies", Tags: []string{"Planning"},
		Summary:     "List the feasibility studies",
		Description: "Every feasibility study, the latest first, a page at a time (FR-PLN-06). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *studyListInput) (*studyListOutput, error) {
		p, _ := authn.From(ctx)
		out := studyListOutput{Body: FeasibilityStudyPage{Items: []FeasibilityStudyView{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var all []FeasibilityStudyView
			f := storage.DocumentFilter{Type: storage.DocumentFeasibilityStudy, Limit: 200}
			for {
				docs, err := u.Documents().Page(ctx, f)
				if err != nil {
					return err
				}
				for _, doc := range docs {
					v, err := studyOf(doc)
					if err != nil {
						return err
					}
					all = append(all, v)
				}
				if len(docs) < f.Limit {
					break
				}
				f.AfterDate, f.AfterID = docs[len(docs)-1].Date, &docs[len(docs)-1].ID
			}
			for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
				all[i], all[j] = all[j], all[i]
			}
			var err error
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x FeasibilityStudyView) string { return x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []FeasibilityStudyView{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateFeasibilityStudy, huma.Operation{
		OperationID: "createFeasibilityStudy", Method: http.MethodPost, Path: "/v1/feasibility-studies", Tags: []string{"Planning"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a feasibility study",
		Description: "Saves one idea's inputs as the workbook's sheet has them (FR-PLN-06, design D8), dated today: its tax rate is sg-cit's " +
			"headline rate then. A study is drafted, changed and kept; it never posts and is never recorded (FR-PLN-08). Its figures are " +
			"getFeasibilityResults'. Side effects: the study, audit events. Every problem comes back together. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *studyDraftInput) (*studyWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (FeasibilityStudyView, error) {
			base, err := assetBase(ctx, u)
			if err != nil {
				return FeasibilityStudyView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return FeasibilityStudyView{}, err
			}
			doc, err := feasibility.DraftIn(ctx, u, in.Body, base, today)
			if err != nil {
				return FeasibilityStudyView{}, equityProblems(err)
			}
			return studyOf(doc)
		})
		if err != nil {
			return nil, err
		}
		return studyWriteAnswer(w), nil
	})

	register(a, authz.GetFeasibilityStudy, huma.Operation{
		OperationID: "getFeasibilityStudy", Method: http.MethodGet, Path: "/v1/feasibility-studies/{study_id}", Tags: []string{"Planning"},
		Summary:     "Get a feasibility study",
		Description: "A study's inputs and its date (FR-PLN-06). Its figures are getFeasibilityResults'. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *FeasibilityStudyPath) (*studyReadOutput, error) {
		id, err := parseID("study_id", in.StudyID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out studyReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := feasibility.Get(ctx, u, id, false)
			if err != nil {
				return err
			}
			v, err := studyOf(doc)
			out.Body, out.ETag = v, recordETag(doc)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceFeasibilityStudy, huma.Operation{
		OperationID: "replaceFeasibilityStudy", Method: http.MethodPut, Path: "/v1/feasibility-studies/{study_id}", Tags: []string{"Planning"},
		Summary: "Change a feasibility study",
		Description: "Saves a new version of a study's inputs (FR-PLN-06); its date, and so its tax rate, stays its first. Side effects: " +
			"the new version, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), func(ctx context.Context, in *studyReviseInput) (*studyWriteOutput, error) {
		id, err := parseID("study_id", in.StudyID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (FeasibilityStudyView, error) {
			doc, err := feasibility.Get(ctx, u, id, true)
			if err != nil {
				return FeasibilityStudyView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return FeasibilityStudyView{}, err
			}
			base, err := assetBase(ctx, u)
			if err != nil {
				return FeasibilityStudyView{}, err
			}
			doc, err = feasibility.ReviseIn(ctx, u, id, in.Body, base)
			if err != nil {
				return FeasibilityStudyView{}, equityProblems(err)
			}
			return studyOf(doc)
		})
		if err != nil {
			return nil, err
		}
		return studyWriteAnswer(w), nil
	})

	register(a, authz.GetFeasibilityResults, huma.Operation{
		OperationID: "getFeasibilityResults", Method: http.MethodGet, Path: "/v1/feasibility-studies/{study_id}/results", Tags: []string{"Planning"},
		Summary: "Work a feasibility study out",
		Description: "A study's figures (FR-PLN-06, design D8), worked out on every read in exact decimals (RND-PLAN) at sg-cit's headline rate on " +
			"the study's date: each year's cash flows and present values, the NPV, the IRR nearest 10%, the paybacks, the profitability index, " +
			"break-even, the margin of safety, the 5 × 5 sensitivity grid, the NPV at eight rates, and the verdict. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, append(locks, ForecastCodes...)...), func(ctx context.Context, in *FeasibilityStudyPath) (*studyResultsOutput, error) {
		id, err := parseID("study_id", in.StudyID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out studyResultsOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := feasibility.Get(ctx, u, id, false)
			if err != nil {
				return err
			}
			plan, day, err := feasibility.Read(doc)
			if err != nil {
				return err
			}
			base, err := assetBase(ctx, u)
			if err != nil {
				return err
			}
			tax, packID, err := feasibility.TaxRate(ctx, u, day)
			if err != nil {
				return err
			}
			r, err := planning.Feasible(feasibility.Study(plan, base, tax))
			if err != nil {
				return err
			}
			out.Body, err = studyResultsOf(doc.ID.String(), r, base, tax, packID, plan.MaxPayback)
			return err
		})
		return &out, err
	})
}
