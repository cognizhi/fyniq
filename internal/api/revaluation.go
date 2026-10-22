package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ month-end FX revaluation (FR-GL-04)

// RevaluationItem is one account's balance in one foreign currency at the period end, revalued.
type RevaluationItem struct {
	AccountID  string `json:"account_id" format:"uuid" doc:"The account, marked revalue." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Code       string `json:"code" doc:"Its code." example:"1100"`
	Name       string `json:"name" doc:"Its name." example:"Accounts receivable"`
	Open       Money  `json:"open" doc:"Its balance in the currency at the period end: debit positive."`
	Carrying   Money  `json:"carrying" doc:"What that balance is in the books in the base currency, from the balance service."`
	Rate       string `json:"rate,omitempty" doc:"The period-end rate the cascade finds (ENG-40), base currency per unit; absent when there is none." example:"1.33"`
	RateSource string `json:"rate_source,omitempty" doc:"Where the rate comes from." example:"MAS daily feed of 2026-10-30"`
	Change     *Money `json:"change,omitempty" doc:"RND-FX(open × rate) − carrying: an unrealised gain when positive on an asset, a loss when negative; absent without a rate."`
}

// RevaluationMissing is a currency the cascade finds no period-end rate for (FX-01).
type RevaluationMissing struct {
	Currency string `json:"currency" doc:"The currency." example:"AUD"`
	Detail   string `json:"detail" doc:"FX-01's message." example:"✖ No exchange rate for AUD on 31 Oct 2026"`
}

// NotRevaluedBalance is a foreign-currency balance on an account not marked revalue: it stays at the rates it was
// recorded at, such as a prepayment paid in US dollars.
type NotRevaluedBalance struct {
	AccountID string `json:"account_id" format:"uuid" doc:"The account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d62"`
	Code      string `json:"code" doc:"Its code." example:"1210"`
	Name      string `json:"name" doc:"Its name." example:"Prepayments"`
	Amount    Money  `json:"amount" doc:"Its balance in the currency."`
	Carrying  Money  `json:"carrying" doc:"Its value in the books in the base currency."`
}

// RevaluationLine is a line of what the revaluation posts.
type RevaluationLine struct {
	AccountID   string `json:"account_id" format:"uuid" doc:"The account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	Code        string `json:"code" doc:"Its code." example:"7040"`
	Name        string `json:"name" doc:"Its name." example:"Unrealised FX gain or loss"`
	Amount      Money  `json:"amount" doc:"In the line's currency: 0 on a base-only line (INV-05)."`
	Base        Money  `json:"base" doc:"In the base currency: debit positive, credit negative."`
	Description string `json:"description" doc:"What the line is." example:"Unrealised FX loss"`
}

// RevaluationRun is a posted revaluation of the period.
type RevaluationRun struct {
	DocumentID string     `json:"document_id" format:"uuid" doc:"The revaluation document." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	PostedBy   *Person    `json:"posted_by,omitempty" doc:"Who ran it."`
	PostedAt   *time.Time `json:"posted_at,omitempty" doc:"When." example:"2026-11-01T02:00:00Z"`
	Revision   int64      `json:"revision" doc:"The ledger revision it read (ENG-46)." example:"318"`
	EntryID    string     `json:"entry_id,omitempty" format:"uuid" doc:"Its entry, dated the period's last day; absent when nothing changed." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e21"`
	ReversalID string     `json:"reversal_id,omitempty" format:"uuid" doc:"The entry's reversal on the next period's first day; absent while it waits for that period to open." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e22"`
	Current    bool       `json:"current" doc:"No posting dated in or before the period came after it." example:"true"`
}

// FXRevaluation is a period's month-end revaluation: what it revalues now, and its latest run.
type FXRevaluation struct {
	Period      Period               `json:"period" doc:"The period."`
	Date        string               `json:"date" format:"date" doc:"The revaluation's date: the period's last day." example:"2026-10-31"`
	ReverseOn   string               `json:"reverse_on" format:"date" doc:"The day its entry reverses: the next period's first." example:"2026-11-01"`
	State       string               `json:"state" enum:"nothing,due,revalued" doc:"nothing: no foreign-currency balance on an account marked revalue; due: no run yet, or a posting dated in or before the period came after the latest (the close refuses it); revalued: the latest run is current." example:"due"`
	Balances    []RevaluationItem    `json:"balances" doc:"The accounts marked revalue with a foreign-currency balance at the period end, by account and currency."`
	Missing     []RevaluationMissing `json:"missing" doc:"Currencies with no rate on the period's last day: nothing posts until each has one (FX-01)."`
	NotRevalued []NotRevaluedBalance `json:"not_revalued" doc:"Foreign-currency balances on accounts not marked revalue, which keep the rates they were recorded at."`
	Lines       []RevaluationLine    `json:"lines" doc:"What a run would post now: a base-only line per item that changes, and the losses and gains on the unrealised-FX account. Empty when nothing changes or a rate is missing."`
	Latest      *RevaluationRun      `json:"latest,omitempty" doc:"The period's latest run, if any."`
}

type fxRevaluationOutput struct {
	ETag string `header:"ETag" doc:"The period's ETag, for postFxRevaluation's If-Match."`
	Body FXRevaluation
}

type postFxRevaluationInput struct {
	PeriodPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type postFxRevaluationOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was posted (FR-INT-14)."`
	Body     FXRevaluation
}

// RevaluationCodes are the refusals of posting a revaluation.
var RevaluationCodes = []string{fx.CodeNoRate, fx.CodeNotOpen, fx.CodeNotEnded, fx.CodeUnrealised}

// revaluationOf answers a period's revaluation as Prepare found it.
func revaluationOf(ctx context.Context, a *API, u storage.UnitOfWork, plan fx.Plan) (FXRevaluation, error) {
	cal, err := calendar(ctx, u)
	if err != nil {
		return FXRevaluation{}, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return FXRevaluation{}, err
	}
	byID := map[uuid.UUID]storage.Account{}
	for _, x := range accounts {
		byID[x.ID] = x
	}
	out := FXRevaluation{Period: periodOf(plan.Period, cal), Date: plan.Period.EndsOn.Format(time.DateOnly), ReverseOn: plan.ReverseOn.Format(time.DateOnly),
		Balances: []RevaluationItem{}, Missing: []RevaluationMissing{}, NotRevalued: []NotRevaluedBalance{}, Lines: []RevaluationLine{}}
	switch {
	case len(plan.Items) == 0:
		out.State = "nothing"
	case plan.Due():
		out.State = "due"
	default:
		out.State = "revalued"
	}
	for _, it := range plan.Items {
		x := RevaluationItem{AccountID: it.AccountID.String(), Code: it.Account.Code, Name: it.Account.Name, Open: amountOf(it.Open), Carrying: amountOf(it.Carrying)}
		if it.Rate.From.Code != "" {
			change := amountOf(it.Change)
			x.Rate, x.RateSource, x.Change = it.Rate.String(), it.Rate.Source, &change
		}
		out.Balances = append(out.Balances, x)
	}
	for _, m := range plan.Missing {
		out.Missing = append(out.Missing, RevaluationMissing{Currency: m.Currency, Detail: m.Message})
	}
	for _, n := range plan.NotRevalued {
		out.NotRevalued = append(out.NotRevalued, NotRevaluedBalance{AccountID: n.Account.ID.String(), Code: n.Account.Code, Name: n.Account.Name,
			Amount: amountOf(n.Amount), Carrying: amountOf(n.Base)})
	}
	for _, l := range plan.Entry.Lines {
		acc := byID[l.AccountID]
		out.Lines = append(out.Lines, RevaluationLine{AccountID: l.AccountID.String(), Code: acc.Code, Name: acc.Name, Amount: amountOf(l.Amount),
			Base: amountOf(l.Base), Description: l.Description})
	}
	if r := plan.Latest; r != nil {
		run := RevaluationRun{DocumentID: r.Document.ID.String(), PostedBy: personOf(r.Document.PostedBy), PostedAt: r.Document.PostedAt,
			Revision: r.Revision, Current: r.Current}
		if r.Entry != nil {
			run.EntryID = r.Entry.ID.String()
		}
		if r.Reversal != nil {
			run.ReversalID = r.Reversal.ID.String()
		}
		out.Latest = &run
		if err := a.name(ctx, u, run.PostedBy); err != nil {
			return FXRevaluation{}, err
		}
	}
	return out, nil
}

func registerRevaluation(a *API) {
	register(a, authz.GetFxRevaluation, huma.Operation{
		OperationID: "getFxRevaluation", Method: http.MethodGet, Path: "/v1/periods/{period_id}/fx-revaluation", Tags: []string{"Exchange rates"},
		Summary: "Preview a period's FX revaluation",
		Description: "The month-end revaluation of the period (FR-GL-04; PRD-001 rev 0.73, provisional): every account marked revalue with a " +
			"balance in a foreign currency at the period end, from the balance service, at the rate the cascade finds for the period's last " +
			"day (ENG-40), with its change, RND-FX(open × rate) − carrying; the currencies with no rate; the foreign-currency balances on " +
			"accounts not marked revalue, which stay as recorded; what a run would post now; and the latest run, with whether a posting " +
			"dated in or before the period came after it. Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *PeriodPath) (*fxRevaluationOutput, error) {
		id, err := parseID("period_id", in.PeriodID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out fxRevaluationOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			plan, err := fx.Prepare(ctx, u, id)
			if err != nil {
				return err
			}
			out.Body, err = revaluationOf(ctx, a, u, plan)
			out.ETag = periodETag(out.Body.Period)
			return err
		})
		return &out, err
	})

	register(a, authz.PostFxRevaluation, huma.Operation{
		OperationID: "postFxRevaluation", Method: http.MethodPost, Path: "/v1/periods/{period_id}/fx-revaluation", Tags: []string{"Exchange rates"},
		DefaultStatus: http.StatusCreated, Summary: "Revalue a period's foreign-currency balances",
		Description: "Runs the period's month-end revaluation (FR-GL-04; PRD-001 rev 0.73, provisional) under the ledger lock: records what it " +
			"read as a revaluation document and posts its entry, dated the period's last day: a base-only line per account and currency " +
			"that changes, and the losses and gains on the unrealised-FX account (fx_revaluation@1). The entry reverses on the next " +
			"period's first day, at once when that period is open, else when it opens (the scheduler's fx.reversals). A run with nothing " +
			"to change posts no entry and still records the revaluation. Run again after a posting dated in or before the period: the " +
			"new run posts what changed since. The scheduler also runs it by itself (fx.revalue, daily; rev 0.75) once the period has " +
			"ended and every currency has a rate for its last day, as scheduler:fx.revalue, so a person's run is needed only for a rate " +
			"entered later. Refused while the period is not open (FX-05), before its last day (FX-06), without an " +
			"unrealised-FX account (FX-07), or with a currency that has no rate (FX-01). Needs If-Match with the period's ETag, as " +
			"getFxRevaluation or getPeriod gives it. Answers the revaluation as it then stands. " +
			"Side effects: the document, its entry, its reversal, and their audit events. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and posts nothing.",
	}, append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeStale, CodeIfMatchRequired, storage.CodeRetry,
		storage.CodeLockTimeout}, RevaluationCodes...),
		func(ctx context.Context, in *postFxRevaluationInput) (*postFxRevaluationOutput, error) {
			id, err := parseID("period_id", in.PeriodID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (FXRevaluation, error) {
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return FXRevaluation{}, err
				}
				p, err := u.Periods().Get(ctx, id)
				if err != nil {
					return FXRevaluation{}, err
				}
				cal, err := calendar(ctx, u)
				if err != nil {
					return FXRevaluation{}, err
				}
				if err := checkIfMatch(in.IfMatch, periodETag(periodOf(p, cal))); err != nil { // the period as you read it
					return FXRevaluation{}, err
				}
				if _, err := journal.PostRevaluation(ctx, u, id, today); err != nil {
					return FXRevaluation{}, err
				}
				plan, err := fx.Prepare(ctx, u, id)
				if err != nil {
					return FXRevaluation{}, err
				}
				return revaluationOf(ctx, a, u, plan)
			})
			if err != nil {
				return nil, err
			}
			return &postFxRevaluationOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
