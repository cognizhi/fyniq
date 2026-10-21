package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ flux analysis in the close (FR-GL-14)

// VarianceCodes are the codes of an explanation.
var VarianceCodes = []string{closing.CodeNotOpen, closing.CodeNoVariance, closing.CodeMoved, storage.CodeVariance}

// VarianceExplanation is the explanation of a variance: the words, the two totals it explained, who wrote it and when.
type VarianceExplanation struct {
	ID          string    `json:"id" format:"uuid" doc:"The explanation's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4de0"`
	Explanation string    `json:"explanation" doc:"Why the account moved." example:"The annual design-tool licence renewed in November."`
	Current     Money     `json:"current" doc:"The account's total for the period it explained."`
	Previous    Money     `json:"previous" doc:"The account's total for the previous period it explained."`
	WrittenBy   *Person   `json:"written_by,omitempty" doc:"Who wrote it."`
	WrittenAt   time.Time `json:"written_at" doc:"When." example:"2026-12-02T09:30:00Z"`
}

// Variance is one income or expense account's month-on-month movement.
type Variance struct {
	AccountID        string               `json:"account_id" format:"uuid" doc:"The account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Code             string               `json:"code" doc:"Its code." example:"6420"`
	Name             string               `json:"name" doc:"Its name." example:"Software subscriptions"`
	Type             string               `json:"type" enum:"revenue,expense" doc:"revenue (income) or expense: balance-sheet accounts rely on the support the close requires." example:"expense"`
	Current          Money                `json:"current" doc:"Its total for the period in the base currency: debit positive, credit negative, so income is negative, as in the trial balance."`
	Previous         Money                `json:"previous" doc:"Its total for the previous period."`
	Change           Money                `json:"change" doc:"current − previous."`
	ChangePercent    string               `json:"change_percent,omitempty" pattern:"^-?(0|[1-9][0-9]*)\\.[0-9]$" doc:"The change as a percentage of the previous total's size, to one decimal, half away from zero (RND-PERCENT), with the change's sign; absent when the previous total is zero. For showing only: the thresholds compare the exact amounts." example:"25.0"`
	NeedsExplanation bool                 `json:"needs_explanation" doc:"The change is more than both thresholds, so the close needs an explanation of it." example:"true"`
	Explained        bool                 `json:"explained" doc:"The latest explanation is of the totals as they are now; once either total changes it is stale, and the close asks for a new one." example:"false"`
	Explanation      *VarianceExplanation `json:"explanation,omitempty" doc:"The latest explanation, current or stale; absent while there is none."`
}

// VariancePage is a period's month-on-month comparison, a page of accounts at a time, by code.
type VariancePage struct {
	Period           Period     `json:"period"`
	PreviousPeriod   *Period    `json:"previous_period,omitempty" doc:"The period it is compared with: the one that ends the day before it starts; absent when there is none, and then there is nothing to compare."`
	ThresholdAmount  Money      `json:"threshold_amount" doc:"A variance needs an explanation when its size is more than this (setting close.variance_amount)…"`
	ThresholdPercent string     `json:"threshold_percent" doc:"…and more than this percentage of the previous total's size (setting close.variance_percent); when the previous total is zero the amount alone decides." example:"10"`
	Unexplained      int        `json:"unexplained" doc:"How many variances, on every page, need an explanation and have no current one: the close checks refuse the close while there are any." example:"1"`
	Items            []Variance `json:"items" nullable:"false" doc:"Each income or expense account with a total in either period."`
	NextCursor       string     `json:"next_cursor,omitempty" doc:"Pass it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjY0MjAifQ"`
}

type varianceListInput struct {
	PeriodPath
	PageQuery
}

type varianceListOutput struct {
	Body VariancePage
}

// ExplainVarianceRequest explains an account's variance, for the totals the writer read.
type ExplainVarianceRequest struct {
	Current     Money  `json:"current" doc:"The account's total for the period as listPeriodVariances showed it: the explanation is of these totals, and other totals now are refused (CLOSE-10)."`
	Previous    Money  `json:"previous" doc:"Its total for the previous period as listPeriodVariances showed it."`
	Explanation string `json:"explanation" minLength:"1" maxLength:"1000" doc:"Why the account moved, in a sentence or two, for the attester and the accountant." example:"The annual design-tool licence renewed in November."`
}

type explainVarianceInput struct {
	PeriodPath
	AccountID string `path:"account_id" doc:"The account whose variance is explained." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	KeyHeader
	PreferHeader
	Body ExplainVarianceRequest
}

type explainVarianceOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Variance
}

func amountOf(m money.Money) Money { return Money{Amount: m.String(), Currency: m.Currency().Code} }

func varianceOf(v closing.Variance) Variance {
	out := Variance{AccountID: v.Account.ID.String(), Code: v.Account.Code, Name: v.Account.Name, Type: string(v.Account.Type),
		Current: amountOf(v.Current), Previous: amountOf(v.Previous), Change: amountOf(v.Change), ChangePercent: v.Percent,
		NeedsExplanation: v.Needs, Explained: v.Explained}
	if e := v.Explanation; e != nil {
		out.Explanation = &VarianceExplanation{ID: e.ID.String(), Explanation: e.Explanation, Current: moneyOf(e.Current, v.Current.Currency().Code),
			Previous: moneyOf(e.Previous, v.Current.Currency().Code), WrittenBy: personOf(e.WrittenBy), WrittenAt: e.WrittenAt.UTC()}
	}
	return out
}

// explainedBy are the writers of the variances' explanations, to name.
func explainedBy(vs []Variance) []*Person {
	var out []*Person
	for i := range vs {
		if e := vs[i].Explanation; e != nil {
			out = append(out, e.WrittenBy)
		}
	}
	return out
}

func registerVariances(a *API) {
	register(a, authz.ListPeriodVariances, huma.Operation{
		OperationID: "listPeriodVariances", Method: http.MethodGet, Path: "/v1/periods/{period_id}/variances", Tags: []string{"Periods"},
		Summary: "List a period's variances",
		Description: "Flux analysis for the close (FR-GL-14; PRD-001 rev 0.65, provisional): each income or expense account's total for the " +
			"period against its total for the previous period (the one ending the day before it starts), in the base currency from the " +
			"balance service, by account code, a page at a time. A variance needs an explanation when its size is more than both the " +
			"amount and the percentage of the previous total in the organisation's settings (close.variance_amount, " +
			"close.variance_percent; when the previous total is zero the amount alone decides); each shows its latest explanation and " +
			"whether that explanation is still of the totals as they are. unexplained counts, over every page, those the close checks " +
			"refuse the close for (Appendix C.2 #57). Balance-sheet accounts rely on the support the close requires; budget variances " +
			"arrive with budgets. Read-only; no side effects.",
	}, []string{CodeNotFound, CodeCursor}, func(ctx context.Context, in *varianceListInput) (*varianceListOutput, error) {
		id, err := parseID("period_id", in.PeriodID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := varianceListOutput{Body: VariancePage{Items: []Variance{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			f, err := closing.Compare(ctx, u, id, today)
			if err != nil {
				return err
			}
			cal, err := calendar(ctx, u)
			if err != nil {
				return err
			}
			out.Body.Period = periodOf(f.Period, cal)
			if f.Previous != nil {
				prev := periodOf(*f.Previous, cal)
				out.Body.PreviousPeriod = &prev
			}
			out.Body.ThresholdAmount, out.Body.ThresholdPercent = amountOf(f.Thresholds.Amount), f.Thresholds.Percent.Text('f')
			out.Body.Unexplained = f.Unexplained()
			page, next, err := pageOf(f.Variances, func(v closing.Variance) string { return v.Account.Code }, in.PageQuery)
			if err != nil {
				return err
			}
			out.Body.NextCursor = next
			for _, v := range page {
				out.Body.Items = append(out.Body.Items, varianceOf(v))
			}
			return a.name(ctx, u, explainedBy(out.Body.Items)...)
		})
		return &out, err
	})

	register(a, authz.ExplainVariance, huma.Operation{
		OperationID: "explainVariance", Method: http.MethodPost, Path: "/v1/periods/{period_id}/variances/{account_id}/explanation", Tags: []string{"Periods"},
		Summary: "Explain a variance",
		Description: "Records why an income or expense account's total for an open period moved against the previous period (FR-GL-14; " +
			"PRD-001 rev 0.65, provisional). The request names the two totals as listPeriodVariances showed them: the explanation is of " +
			"those totals, and is refused when the account's totals are not those now (CLOSE-10), so read the variances again. It " +
			"replaces the account's earlier explanation for the period; explanations are never changed or removed, and once either total " +
			"changes the explanation is stale and the close asks for a new one. The database refuses one in a period that is not open " +
			"or for a balance-sheet account (STORAGE-CLOSE-02). Run the close checks again afterwards: they record what they found when " +
			"they ran. Side effects: the explanation and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview " +
			"checks it and records nothing.",
	}, append([]string{CodeNotFound, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, storage.CodeRetry, storage.CodeLockTimeout, money.CodeNotDecimal, money.CodePrecision, money.CodeRange, money.CodeCurrency}, VarianceCodes...),
		func(ctx context.Context, in *explainVarianceInput) (*explainVarianceOutput, error) {
			id, err := parseID("period_id", in.PeriodID)
			if err != nil {
				return nil, err
			}
			account, err := parseID("account_id", in.AccountID)
			if err != nil {
				return nil, err
			}
			words := strings.TrimSpace(in.Body.Explanation)
			if words == "" {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.explanation", Message: "is empty", Fix: "Say why the account moved"}})
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Variance, error) {
				o, err := u.Organisation(ctx)
				if err != nil {
					return Variance{}, err
				}
				base, err := money.Lookup(o.BaseCurrency)
				if err != nil {
					return Variance{}, err
				}
				var fields []FieldProblem
				read := func(m Money, field string) money.Money {
					if m.Currency != base.Code {
						fields = append(fields, FieldProblem{Code: money.CodeCurrency, Field: field + ".currency", Message: "is " + m.Currency + ", not the base currency " + base.Code,
							Fix: "Send the totals in the base currency, as listPeriodVariances shows them"})
						return money.Money{}
					}
					v, err := money.ParseField(m.Amount, base, field+".amount")
					var me *money.Error
					switch {
					case errors.As(err, &me):
						fields = append(fields, FieldProblem{Code: me.Code, Field: me.Field, Message: me.Message, Fix: me.Fix})
					case err != nil:
						fields = append(fields, FieldProblem{Code: money.CodeNotDecimal, Field: field + ".amount", Message: err.Error(),
							Fix: "Send the total as listPeriodVariances shows it"})
					}
					return v
				}
				current, previous := read(in.Body.Current, "body.current"), read(in.Body.Previous, "body.previous")
				if len(fields) > 0 {
					return Variance{}, invalid(fields)
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return Variance{}, err
				}
				_, v, err := closing.Explain(ctx, u, id, account, current, previous, words, today)
				if err != nil {
					return Variance{}, err
				}
				out := varianceOf(v)
				return out, a.name(ctx, u, explainedBy([]Variance{out})...)
			})
			if err != nil {
				return nil, err
			}
			return &explainVarianceOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
