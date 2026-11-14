package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/semantic"
)

// MeasureKey names a measure of the semantic layer.
type MeasureKey string

// Schema implements huma.SchemaProvider.
func (MeasureKey) Schema(huma.Registry) *huma.Schema {
	keys := make([]any, 0, len(semantic.Measures))
	for _, m := range semantic.Measures {
		keys = append(keys, m.Key)
	}
	return &huma.Schema{Type: "string", Enum: keys, Description: "A measure of the semantic layer (AI-03).", Examples: []any{"revenue"}}
}

// MetricsQuery asks the semantic layer for a measure (AI-03): over a run of months, by month or their total, split by a
// dimension or not, kept to one member of it or not.
type MetricsQuery struct {
	Measure  MeasureKey `json:"measure"`
	From     string     `json:"from,omitempty" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The first month, YYYY-MM; left out, the last month's." example:"2026-07"`
	To       string     `json:"to,omitempty" pattern:"^[0-9]{4}-[0-9]{2}$" doc:"The last month, YYYY-MM; left out, the month of the organisation's today." example:"2026-09"`
	Grain    string     `json:"grain,omitempty" enum:"month,total" default:"total" doc:"month: a fact a month; total: one over the months, a flow summed, a stock at the last month's end." example:"total"`
	By       string     `json:"by,omitempty" enum:"account,customer,vendor,department" doc:"The dimension to split it by, which the measure must offer." example:"customer"`
	Member   string     `json:"member,omitempty" maxLength:"60" doc:"Keep one member of the dimension: its ID (an account's code too)." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Revision int64      `json:"revision,omitempty" minimum:"0" doc:"Read the ledger as at this revision (FR-RPT-08); 0 or left out, as it stands." example:"1204"`
}

// MetricFact is one figure the semantic layer gives (AI-68): what it is, of what, when, its exact value, and the report
// it comes from.
type MetricFact struct {
	ID         string `json:"id" doc:"The fact's ID: an answer cites it, and platform code renders the figure from it (AI-08)." example:"F3a9c0d1e2b4f5a6"`
	Measure    string `json:"measure" example:"revenue"`
	Name       string `json:"name" example:"Revenue"`
	Value      string `json:"value,omitempty" doc:"Exact decimal text, as the report shows it before rounding for display; absent when the fact has no value." example:"125000.00"`
	Words      string `json:"words,omitempty" doc:"What stands for the value, such as Not burning." example:"Not burning"`
	Undefined  string `json:"undefined,omitempty" doc:"Why the fact has no value: never 0." example:"no revenue in the month"`
	Unit       string `json:"unit" enum:"amount,percent,months,ratio,days" example:"amount"`
	Currency   string `json:"currency,omitempty" doc:"An amount's currency: the base currency." example:"SGD"`
	From       string `json:"from" format:"date" doc:"The period's first day; a stock's date." example:"2026-07-01"`
	To         string `json:"to" format:"date" doc:"The period's last day; a stock's date." example:"2026-09-30"`
	Dimension  string `json:"dimension,omitempty" example:"customer"`
	Member     string `json:"member,omitempty" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	MemberName string `json:"member_name,omitempty" example:"Acme Pte Ltd"`
	Sign       string `json:"sign" doc:"How the value is signed." example:"what is owed, positive; a credit with the party, negative"`
	Revision   int64  `json:"ledger_revision" doc:"The ledger revision it was read at (ENG-46)." example:"1204"`
	Source     string `json:"source" doc:"What it is, in words." example:"the profit and loss's revenue"`
	Lineage    string `json:"lineage" doc:"The API path of the report it comes from." example:"/v1/reports/statements/pl?from=2026-07&to=2026-09&as_at_revision=1204"`
}

// MetricsResult is a query's facts, read at one ledger revision.
type MetricsResult struct {
	Facts    []MetricFact `json:"facts" nullable:"false"`
	Currency string       `json:"currency" example:"SGD"`
	Revision int64        `json:"ledger_revision" doc:"The ledger revision every figure is read as at (FR-RPT-08)." example:"1204"`
}

type (
	metricsInput  struct{ Body MetricsQuery }
	metricsOutput struct{ Body MetricsResult }
)

func factOut(f semantic.Fact) MetricFact {
	return MetricFact{ID: f.ID, Measure: f.Measure, Name: f.Name, Value: f.Value, Words: f.Words, Undefined: f.Undefined, Unit: f.Unit, Currency: f.Currency,
		From: dateOf(f.From), To: dateOf(f.To), Dimension: string(f.Dimension), Member: f.Member, MemberName: f.MemberName, Sign: f.Sign, Revision: f.Revision,
		Source: f.Source, Lineage: f.Lineage}
}

// semanticProblem is a query the semantic layer refuses, with its field.
func semanticProblem(err error) error {
	var e *semantic.Error
	if errors.As(err, &e) {
		return problem(e.Code, e.Field+" "+e.Message+".", e.Fix, FieldProblem{Code: e.Code, Field: "body." + e.Field, Message: e.Message, Fix: e.Fix})
	}
	return err
}

func registerMetrics(a *API) {
	// A query saves nothing, so a repeat is harmless and return=preview would answer the same.
	register(a, authz.QueryMetrics, exempt(exempt(huma.Operation{
		OperationID: "queryMetrics", Method: http.MethodPost, Path: "/v1/metrics:query", Tags: []string{"Reports"},
		Summary: "Query the semantic layer",
		Description: "A named measure (AI-03) over a run of months, by month or their total, split by a dimension the measure offers (account, " +
			"customer, vendor or department) or not: revenue, gross profit, EBITDA, net profit, costs, spend by function, net burn, cash, " +
			"receivables, payables, current assets and liabilities, total liabilities, equity, each account's movement or balance, and the " +
			"monthly KPIs. Each figure is a typed fact (AI-68) with its ID, period, exact value, currency and unit, sign, the ledger revision it " +
			"was read at and the report it comes from: the statements, the KPIs, the trial balance and the sub-ledgers, so it equals what " +
			"that report shows. A month the books have no period for is REPORT-03, a later revision REPORT-04. Saves nothing; no side effects.",
	}, "Idempotency-Key", "A query of the semantic layer saves nothing, so a repeat is harmless and answers the same (AI-03)."),
		"Prefer", "A query of the semantic layer saves nothing: return=preview would answer the same as the request itself (FR-INT-14)."),
		[]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeNoPeriod, CodeLaterRevision, CodeForbidden, semantic.CodeMeasure,
			semantic.CodeDimension, semantic.CodeGrain, semantic.CodeMember}, func(ctx context.Context, in *metricsInput) (*metricsOutput, error) {
			p, _ := authn.From(ctx)
			q := in.Body
			for _, need := range []struct {
				by     string
				policy authz.Policy
			}{{"customer", authz.GetReceivables}, {"vendor", authz.GetPayables}} {
				if q.By == need.by && !authz.Decide(need.policy, p.Roles, p.AI()).Allowed {
					return nil, problem(CodeForbidden, "Splitting by "+need.by+" reads "+string(need.policy.Capability)+", which your roles may not view.",
						"Ask without splitting by "+need.by+", or for a role that may view it (PRD-001 Appendix D).")
				}
			}
			if q.Grain == "" {
				q.Grain = semantic.Total
			}
			var out MetricsResult
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				org, err := u.Organisation(ctx)
				if err != nil {
					return err
				}
				base, err := money.Lookup(org.BaseCurrency)
				if err != nil {
					return err
				}
				from := q.From
				if from == "" {
					from = "-"
				}
				r, err := a.monthsOf(ctx, u, from, q.To, q.Revision)
				if err != nil {
					return err
				}
				facts, err := semantic.Run(ctx, u, semantic.Books{Base: base, Periods: r.all, First: r.first, Last: r.last, Revision: r.revision,
					YearStart: func(day time.Time) (time.Time, error) { return a.yearStart(ctx, u, day) }},
					semantic.Query{Measure: string(q.Measure), Grain: q.Grain, By: semantic.Dimension(q.By), Member: q.Member})
				if err != nil {
					return semanticProblem(err)
				}
				out = MetricsResult{Facts: []MetricFact{}, Currency: base.Code, Revision: r.revision}
				for _, f := range facts {
					out.Facts = append(out.Facts, factOut(f))
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			return &metricsOutput{Body: out}, nil
		})
}
