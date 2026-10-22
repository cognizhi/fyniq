package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ exchange rates (FR-GL-05, ENG-40, RND-RATE)

// FXRateCodes are the refusals of the exchange-rate operations.
var FXRateCodes = []string{fx.CodeEntry, fx.CodeWithdrawn, fx.CodeReason, storage.CodeFXRate, storage.CodeDuplicate}

// FXRateEntry is an organisation's rate as entered.
type FXRateEntry struct {
	Kind      string `json:"kind" enum:"day,month" doc:"day: the rate for one day, which overrides the daily feed's on that date and replaces the organisation's earlier rate for it; month: a rate of the monthly table, used for a date with neither (ENG-40)." example:"day"`
	Currency  string `json:"currency" pattern:"^[A-Z]{3}$" doc:"The foreign currency, as an ISO 4217 code; never the base currency." example:"USD"`
	On        string `json:"on" format:"date" doc:"The day; for a month, the month's first day." example:"2026-10-16"`
	Quote     string `json:"quote" doc:"The rate as the source quotes it, a decimal string of up to 10 decimal places (ENG-12, ENG-44)." example:"1.2953"`
	QuoteForm string `json:"quote_form,omitempty" enum:"base-per-unit,base-per-100,foreign-per-base" default:"base-per-unit" doc:"How the quote is given: base currency per one unit (stored as it is), per 100 units, or foreign currency per one unit of the base. Fyniq stores base per one unit under RND-RATE: round(quote ÷ 100, 10) or round(1 ÷ quote, 10), half away from zero." example:"base-per-unit"`
	Note      string `json:"note,omitempty" maxLength:"200" doc:"Where the rate came from, such as the bank's board rate." example:"DBS board rate at 4 pm"`
}

// FXRate is an organisation's exchange rate (FR-GL-05).
type FXRate struct {
	ID string `json:"id" format:"uuid" doc:"The rate's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d80"`
	FXRateEntry
	Version         int        `json:"version" doc:"1 while the rate is current, 2 once it is withdrawn: the only change a rate has. Its ETag is the version." example:"1"`
	Rate            string     `json:"rate" doc:"Base currency per one unit of currency, as stored under RND-RATE (up to 10 decimal places)." example:"1.2953"`
	BaseCurrency    string     `json:"base_currency" doc:"The organisation's base currency, which the rate converts into." example:"SGD"`
	EnteredBy       *Person    `json:"entered_by,omitempty" doc:"Who entered it."`
	EnteredAt       time.Time  `json:"entered_at" doc:"When it was entered." example:"2026-10-16T08:30:00Z"`
	Withdrawn       bool       `json:"withdrawn" doc:"The rate is withdrawn: the cascade no longer finds it." example:"false"`
	WithdrawnBy     *Person    `json:"withdrawn_by,omitempty" doc:"Who withdrew it."`
	WithdrawnAt     *time.Time `json:"withdrawn_at,omitempty" doc:"When it was withdrawn." example:"2026-10-17T02:00:00Z"`
	WithdrawnReason string     `json:"withdrawn_reason,omitempty" doc:"Why it was withdrawn." example:"Replaced by a new rate for the same day"`
}

// FXRateMonth is a month of the monthly table with more than one current rate of a currency.
type FXRateMonth struct {
	Currency string `json:"currency" doc:"The currency." example:"USD"`
	Month    string `json:"month" format:"date" doc:"The month's first day." example:"2026-10-01"`
}

// FXRatePage is a page of the organisation's rates.
type FXRatePage struct {
	Items      []FXRate      `json:"items" doc:"The rates, by currency, date and the order they were entered."`
	NextCursor string        `json:"next_cursor,omitempty" doc:"Pass as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMDAifQ"`
	Duplicates []FXRateMonth `json:"duplicates" doc:"⚠ Months of the monthly table, within the filter, with more than one current rate of a currency: the cascade uses their average, which never sets a GST amount, until all but one are withdrawn (ENG-40; checks C.1 #9)."`
}

// FeedRate is a rate of the daily rate feed.
type FeedRate struct {
	Currency     string `json:"currency" doc:"The foreign currency." example:"USD"`
	BaseCurrency string `json:"base_currency" doc:"The currency the rate converts into." example:"SGD"`
	On           string `json:"on" format:"date" doc:"The day the feed published it for." example:"2026-10-16"`
	Rate         string `json:"rate" doc:"Base currency per one unit of currency, under RND-RATE." example:"1.2953"`
	Quote        string `json:"quote" doc:"As the feed published it." example:"1.2953"`
	QuoteForm    string `json:"quote_form" enum:"base-per-unit,base-per-100,foreign-per-base" doc:"How the feed quoted it." example:"base-per-unit"`
	Feed         string `json:"feed" doc:"The feed's name." example:"MAS"`
}

// FeedRatePage is a page of the daily feed's rates.
type FeedRatePage struct {
	Items      []FeedRate `json:"items" doc:"The rates, by currency and date."`
	NextCursor string     `json:"next_cursor,omitempty" doc:"Pass as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEwMDAifQ"`
}

// FXRateLookup is the rate the cascade finds for a currency and a date (ENG-40).
type FXRateLookup struct {
	Currency     string `json:"currency" doc:"The foreign currency." example:"USD"`
	BaseCurrency string `json:"base_currency" doc:"The organisation's base currency." example:"SGD"`
	Date         string `json:"date" format:"date" doc:"The date asked about." example:"2026-10-18"`
	Rate         string `json:"rate" doc:"Base currency per one unit of currency: the rate a posting on the date takes when its document has no rate of its own." example:"1.2953"`
	Source       string `json:"source" doc:"Where the rate comes from, as a converted amount records it." example:"MAS daily feed of 2026-10-16"`
	Step         string `json:"step" enum:"organisation,feed,month" doc:"The cascade's step: the organisation's rate for the day, the daily feed (on the date or up to 7 days before), or the monthly table." example:"feed"`
	Of           string `json:"of" format:"date" doc:"The date the rate is of: the day, the feed's earlier date, or the month's first day." example:"2026-10-16"`
	Entries      int    `json:"entries" doc:"How many monthly-table rates it is the average of; 1 otherwise. An average never sets a GST amount (ENG-42)." example:"1"`
}

type fxRateListInput struct {
	PageQuery
	Currency  string `query:"currency" pattern:"^[A-Z]{3}$" doc:"Only this currency's rates." example:"USD"`
	Kind      string `query:"kind" enum:"day,month" doc:"Only rates for a day, or only the monthly table." example:"month"`
	From      string `query:"from" format:"date" doc:"Only rates dated on or after this day." example:"2026-10-01"`
	To        string `query:"to" format:"date" doc:"Only rates dated on or before this day." example:"2026-10-31"`
	Withdrawn bool   `query:"withdrawn" doc:"Include withdrawn rates." example:"false"`
}

type fxRateListOutput struct {
	Body FXRatePage
}

type feedRateListInput struct {
	PageQuery
	Currency string `query:"currency" pattern:"^[A-Z]{3}$" doc:"Only this currency's rates." example:"USD"`
	From     string `query:"from" required:"true" format:"date" doc:"The first day." example:"2026-10-01"`
	To       string `query:"to" required:"true" format:"date" doc:"The last day, at most 366 days after the first." example:"2026-10-31"`
}

type feedRateListOutput struct {
	Body FeedRatePage
}

type fxRateLookupInput struct {
	Currency string `query:"currency" required:"true" pattern:"^[A-Z]{3}$" doc:"The foreign currency." example:"USD"`
	Date     string `query:"date" required:"true" format:"date" doc:"The date of the posting." example:"2026-10-18"`
}

type fxRateLookupOutput struct {
	Body FXRateLookup
}

// FXRatePath names one of the organisation's rates.
type FXRatePath struct {
	RateID string `path:"rate_id" doc:"The rate's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d80"`
}

type fxRateGetInput struct {
	FXRatePath
}

type fxRateOutput struct {
	ETag string `header:"ETag" doc:"The rate's version, for If-Match."`
	Body FXRate
}

type fxRateCreateInput struct {
	KeyHeader
	PreferHeader
	Body FXRateEntry
}

type fxRateWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The rate's version, for If-Match."`
	Location string `header:"Location" doc:"The rate's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     FXRate
}

// FXRateEntries are rates entered together, such as the rows of a file.
type FXRateEntries struct {
	Items []FXRateEntry `json:"items" minItems:"1" maxItems:"250" doc:"Up to 250 rates, each entered on its own (FR-INT-11)."`
}

type bulkCreateFxRatesInput struct {
	KeyHeader
	PreferHeader
	Body FXRateEntries
}

// WithdrawFXRateRequest says why a rate is withdrawn.
type WithdrawFXRateRequest struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why the rate is wrong or no longer used." example:"Typed per 100 units by mistake"`
}

type fxRateWithdrawInput struct {
	FXRatePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body WithdrawFXRateRequest
}

type fxRateWithdrawOutput struct {
	ETag     string `header:"ETag" doc:"The rate's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     FXRate
}

func fxRateOf(r storage.FXRate, base string) FXRate {
	out := FXRate{ID: r.ID.String(), Version: 1, Rate: fx.Trim(r.Rate), BaseCurrency: base, EnteredBy: personOf(r.EnteredBy), EnteredAt: r.EnteredAt.UTC(),
		FXRateEntry: FXRateEntry{Kind: string(r.Kind), Currency: r.Currency, On: r.On.Format(time.DateOnly), Quote: fx.Trim(r.Quote),
			QuoteForm: r.QuoteForm, Note: r.Note}}
	if r.WithdrawnAt != nil {
		at := r.WithdrawnAt.UTC()
		out.Version, out.Withdrawn, out.WithdrawnAt = 2, true, &at
		if r.WithdrawnBy != nil {
			out.WithdrawnBy = personOf(*r.WithdrawnBy)
		}
		if r.WithdrawnReason != nil {
			out.WithdrawnReason = *r.WithdrawnReason
		}
	}
	return out
}

// queryDate reads an optional date of the query; the zero time when it is empty.
func queryDate(field, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: field, Message: fmt.Sprintf("%q is not a date", s), Fix: "Send a date such as 2026-10-16"}})
	}
	return d, nil
}

// enterFXRate checks an entry and records it, saying what is wrong with each field under prefix.
func enterFXRate(ctx context.Context, u storage.UnitOfWork, e FXRateEntry, prefix string) (FXRate, error) {
	base, err := baseOf(ctx, u)
	if err != nil {
		return FXRate{}, err
	}
	var bad []FieldProblem
	on, err := time.Parse(time.DateOnly, e.On)
	if err != nil {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: prefix + "on", Message: fmt.Sprintf("%q is not a date", e.On), Fix: "Send a date such as 2026-10-16"})
	}
	r, problems := fx.Check(fx.Entry{Kind: storage.FXRateKind(e.Kind), Currency: e.Currency, On: on, Quote: e.Quote,
		QuoteForm: money.QuoteForm(e.QuoteForm), Note: e.Note}, base)
	for _, x := range problems {
		if x.Field == "on" && err != nil {
			continue
		}
		bad = append(bad, FieldProblem{Code: x.Code, Field: prefix + x.Field, Message: x.Message, Fix: x.Fix})
	}
	if len(bad) > 0 {
		return FXRate{}, invalid(bad)
	}
	saved, err := fx.Enter(ctx, u, r)
	if err != nil {
		return FXRate{}, err
	}
	return fxRateOf(saved, base.Code), nil
}

func registerFX(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListFxRates, huma.Operation{
		OperationID: "listFxRates", Method: http.MethodGet, Path: "/v1/fx/rates", Tags: []string{"Exchange rates"},
		Summary: "List the organisation's exchange rates",
		Description: "The organisation's own rates (FR-GL-05; PRD-001 rev 0.70, provisional): rates for a day, which override the daily " +
			"feed's on that date, and the monthly table, used for a date with neither (ENG-40). Every rate is base currency per one unit " +
			"of the currency, converted from its quote under RND-RATE, with the quote kept. Current rates only, unless withdrawn is true. " +
			"duplicates lists the months of the monthly table with more than one current rate of a currency (⚠ until all but one are " +
			"withdrawn). A page at a time. Read-only; no side effects.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *fxRateListInput) (*fxRateListOutput, error) {
		from, err := queryDate("from", in.From)
		if err != nil {
			return nil, err
		}
		to, err := queryDate("to", in.To)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := fxRateListOutput{Body: FXRatePage{Items: []FXRate{}, Duplicates: []FXRateMonth{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			rates, err := u.FXRates().List(ctx, storage.FXRateFilter{Currency: in.Currency, Kind: storage.FXRateKind(in.Kind), From: from, To: to,
				Withdrawn: in.Withdrawn})
			if err != nil {
				return err
			}
			dups := fx.Duplicates(rates)
			curs := make([]string, 0, len(dups))
			for c := range dups {
				curs = append(curs, c)
			}
			slices.Sort(curs)
			for _, c := range curs {
				months := dups[c]
				slices.SortFunc(months, func(x, y time.Time) int { return x.Compare(y) })
				for _, m := range months {
					out.Body.Duplicates = append(out.Body.Duplicates, FXRateMonth{Currency: c, Month: m.Format(time.DateOnly)})
				}
			}
			page, next, err := pageOf(rates, func(r storage.FXRate) string { return r.ID.String() }, in.PageQuery)
			if err != nil {
				return err
			}
			out.Body.NextCursor = next
			var people []*Person
			for _, r := range page {
				out.Body.Items = append(out.Body.Items, fxRateOf(r, base.Code))
			}
			for i := range out.Body.Items {
				people = append(people, out.Body.Items[i].EnteredBy, out.Body.Items[i].WithdrawnBy)
			}
			return a.name(ctx, u, people...)
		})
		return &out, err
	})

	register(a, authz.GetFxRate, huma.Operation{
		OperationID: "getFxRate", Method: http.MethodGet, Path: "/v1/fx/rates/{rate_id}", Tags: []string{"Exchange rates"},
		Summary:     "Get an exchange rate",
		Description: "One of the organisation's rates, withdrawn or not (FR-GL-05). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *fxRateGetInput) (*fxRateOutput, error) {
		id, err := parseID("rate_id", in.RateID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out fxRateOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			r, err := u.FXRates().Get(ctx, id)
			if err != nil {
				return err
			}
			out.Body = fxRateOf(r, base.Code)
			out.ETag = etag(out.Body.Version)
			return a.name(ctx, u, out.Body.EnteredBy, out.Body.WithdrawnBy)
		})
		return &out, err
	})

	register(a, authz.CreateFxRate, huma.Operation{
		OperationID: "createFxRate", Method: http.MethodPost, Path: "/v1/fx/rates", Tags: []string{"Exchange rates"},
		DefaultStatus: http.StatusCreated, Summary: "Enter an exchange rate",
		Description: "Enters a rate for a day or for the monthly table (FR-GL-05, ENG-40; PRD-001 rev 0.70, provisional). The quote is " +
			"stored as base currency per one unit under RND-RATE, with the quote and its form kept. A day's rate replaces the " +
			"organisation's current rate for that currency and day, which is withdrawn with the reason \"Replaced by a new rate for the " +
			"same day\"; the monthly table keeps every rate, and a second one for a month makes the cascade average them (⚠). A rate is " +
			"never changed: withdraw it and enter another. Refused for the base currency, or a monthly rate not dated on its month's " +
			"first day (FX-02). Postings already made keep the rate they took (ENG-14). Side effects: the rate, any rate it replaces, and " +
			"their audit events. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld},
		append(locks, FXRateCodes...)...),
		func(ctx context.Context, in *fxRateCreateInput) (*fxRateWriteOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (FXRate, error) {
				r, err := enterFXRate(ctx, u, in.Body, "body.")
				if err != nil {
					return FXRate{}, err
				}
				return r, a.name(ctx, u, r.EnteredBy)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &fxRateWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &fxRateWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/fx/rates/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.BulkCreateFxRates, huma.Operation{
		OperationID: "bulkCreateFxRates", Method: http.MethodPost, Path: "/v1/fx/rates:bulk", Tags: []string{"Exchange rates"},
		Summary: "Enter up to 250 exchange rates",
		Description: "Enters each rate on its own, as createFxRate does, such as the rows of a rates file, and answers with one result " +
			"per item in the order sent (FR-INT-11): 201 with the rate's ID, or the item's problem details. An item that is refused takes " +
			"no other item with it, and is never left out. Items run in order, so a later rate for the same day replaces an earlier one. " +
			"Side effects: each rate, any rate it replaces, and their audit events. Idempotent with Idempotency-Key, which covers the whole " +
			"request; with Prefer: return=preview every item is checked and none is saved.",
		MaxBodyBytes: 1 << 20,
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, locks...),
		func(ctx context.Context, in *bulkCreateFxRatesInput) (*bulkOutput, error) {
			w, err := bulk(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, len(in.Body.Items),
				func(ctx context.Context, u storage.UnitOfWork, i int) (BulkResult, error) {
					r, err := enterFXRate(ctx, u, in.Body.Items[i], fmt.Sprintf("body.items[%d].", i))
					if err != nil {
						return BulkResult{}, err
					}
					return BulkResult{Status: http.StatusCreated, ID: r.ID}, nil
				})
			if err != nil {
				return nil, err
			}
			return &bulkOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.WithdrawFxRate, huma.Operation{
		OperationID: "withdrawFxRate", Method: http.MethodPost, Path: "/v1/fx/rates/{rate_id}/withdraw", Tags: []string{"Exchange rates"},
		Summary: "Withdraw an exchange rate",
		Description: "Withdraws one of the organisation's rates with a reason (FR-GL-05): the cascade no longer finds it, and it stays " +
			"listed with who withdrew it, when and why. Needs If-Match with the rate's ETag. A rate never changes otherwise. Postings that " +
			"took it keep it (ENG-14). Refused when it is withdrawn already (FX-03). Side effects: the withdrawal and its audit event. Idempotent " +
			"with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, CodeStale, CodeIfMatchRequired}, append(locks, FXRateCodes...)...),
		func(ctx context.Context, in *fxRateWithdrawInput) (*fxRateWithdrawOutput, error) {
			id, err := parseID("rate_id", in.RateID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (FXRate, error) {
				base, err := baseOf(ctx, u)
				if err != nil {
					return FXRate{}, err
				}
				was, err := u.FXRates().Get(ctx, id)
				if err != nil {
					return FXRate{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(fxRateOf(was, base.Code).Version)); err != nil {
					return FXRate{}, err
				}
				r, err := fx.Withdraw(ctx, u, id, in.Body.Reason)
				if err != nil {
					return FXRate{}, err
				}
				out := fxRateOf(r, base.Code)
				return out, a.name(ctx, u, out.EnteredBy, out.WithdrawnBy)
			})
			if err != nil {
				return nil, err
			}
			out := &fxRateWithdrawOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.ETag = etag(w.Body.Version)
			}
			return out, nil
		})

	register(a, authz.ListFeedRates, huma.Operation{
		OperationID: "listFeedRates", Method: http.MethodGet, Path: "/v1/fx/feed", Tags: []string{"Exchange rates"},
		Summary: "List the daily feed's rates",
		Description: "The daily rate feed's rates into the organisation's base currency between two dates, at most 366 days apart (FR-GL-05; " +
			"PRD-001 rev 0.70): the same for every organisation, each as published, with its RND-RATE rate. The feed publishes business days " +
			"only. Locally the feed is a directory of rates files; MAS's rates arrive at the cloud gate. A page at a time. Read-only; no " +
			"side effects.",
	}, []string{CodeCursor, CodeInvalid}, func(ctx context.Context, in *feedRateListInput) (*feedRateListOutput, error) {
		from, err := queryDate("from", in.From)
		if err != nil {
			return nil, err
		}
		to, err := queryDate("to", in.To)
		if err != nil {
			return nil, err
		}
		if to.Before(from) || to.Sub(from) > 366*24*time.Hour {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "to", Message: "is before from, or more than 366 days after it",
				Fix: "Ask for a year or less, from the earlier date to the later"}})
		}
		p, _ := authn.From(ctx)
		out := feedRateListOutput{Body: FeedRatePage{Items: []FeedRate{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			rates, err := u.FXRates().Feed(ctx, base.Code, in.Currency, from, to)
			if err != nil {
				return err
			}
			page, next, err := pageOf(rates, func(r storage.FeedRate) string { return r.Currency + " " + r.On.Format(time.DateOnly) }, in.PageQuery)
			if err != nil {
				return err
			}
			out.Body.NextCursor = next
			for _, r := range page {
				out.Body.Items = append(out.Body.Items, FeedRate{Currency: r.Currency, BaseCurrency: r.Base, On: r.On.Format(time.DateOnly),
					Rate: fx.Trim(r.Rate), Quote: fx.Trim(r.Quote), QuoteForm: r.QuoteForm, Feed: r.Feed})
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.LookupFxRate, huma.Operation{
		OperationID: "lookupFxRate", Method: http.MethodGet, Path: "/v1/fx/rate", Tags: []string{"Exchange rates"},
		Summary: "Find the rate a posting takes",
		Description: "The rate the cascade finds for a currency and a date (ENG-40; PRD-001 rev 0.70, provisional), as a posting on that date " +
			"takes it when its document has no rate of its own: the organisation's rate for the day; else the daily feed's for the day, or " +
			"its latest up to 7 days before (weekends and public holidays); else the monthly table for the month, the average of its rates " +
			"when there are several. source is what a converted amount records. Refused with FX-01 when there is no rate anywhere. " +
			"Read-only; no side effects.",
	}, []string{CodeInvalid, fx.CodeNoRate}, func(ctx context.Context, in *fxRateLookupInput) (*fxRateLookupOutput, error) {
		on, err := queryDate("date", in.Date)
		if err != nil {
			return nil, err
		}
		cur, err := money.Lookup(strings.TrimSpace(in.Currency))
		if err != nil {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "currency", Message: fmt.Sprintf("%q is not a currency Fyniq knows", in.Currency),
				Fix: "Use an ISO 4217 code, such as USD"}})
		}
		p, _ := authn.From(ctx)
		var out fxRateLookupOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			base, err := baseOf(ctx, u)
			if err != nil {
				return err
			}
			if cur == base {
				return invalid([]FieldProblem{{Code: CodeInvalid, Field: "currency", Message: fmt.Sprintf("%s is the base currency, which takes rate 1", cur),
					Fix: "Ask about another currency"}})
			}
			f, err := fx.Lookup(ctx, u, base, cur, on)
			if err != nil {
				return err
			}
			out.Body = FXRateLookup{Currency: cur.Code, BaseCurrency: base.Code, Date: on.Format(time.DateOnly), Rate: f.Rate.String(),
				Source: f.Rate.Source, Step: f.Step, Of: f.Of.Format(time.DateOnly), Entries: f.Entries}
			return nil
		})
		return &out, err
	})
}
