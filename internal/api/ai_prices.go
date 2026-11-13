package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aiprice"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the prices of the AI connections' models (AI-82, AI-85)
//
// Each model's price names its source: the provider's list, Google's billing catalogue, LiteLLM's public model price
// catalogue (read daily at 06:15 SGT by the worker, never when a page loads), or an Owner with a second approver. A
// normalised match and a rate that fell by more than half wait for an Owner's review; a model nothing prices stays
// unpriced, saying why; an Owner's price is never overwritten by a read.

// PriceField is a rate a source gives that Fyniq does not price with, as the source names it.
type PriceField string

// Schema implements huma.SchemaProvider.
func (PriceField) Schema(huma.Registry) *huma.Schema {
	most := 200
	return &huma.Schema{Type: "string", MaxLength: &most, Description: "A rate the source gives that Fyniq does not price with, as the source names it: " +
		"batch, flex, priority, audio, search or per character. Shown, never used, as Fyniq makes no such request (AI-85).",
		Examples: []any{"input_cost_per_token_batches"}}
}

// AIPriceRate is one rate of a price (AI-85).
type AIPriceRate struct {
	Kind   string `json:"kind" enum:"input,output,cache_read,cache_write,cache_write_1h,reasoning,input_image,request" doc:"What it prices; reasoning, when absent, is priced as output." example:"input"`
	Above  int64  `json:"above" doc:"The prompt length in tokens above which it applies; 0 for the base rate. A call above a tier the price gives is priced at that tier, or unpriced for a kind the tier lacks." example:"0"`
	Unit   string `json:"unit" enum:"per_million_tokens,per_image,per_request" example:"per_million_tokens"`
	Amount string `json:"amount" pattern:"^[0-9]+(\\.[0-9]{1,10})?$" doc:"An exact decimal, in the price's currency, read from the source's text, never as a binary float." example:"3"`
}

// AIPrice is one version of a model's price (AI-82).
type AIPrice struct {
	ID           string        `json:"id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	Number       int           `json:"number" doc:"The version of this model's price, from 1." example:"1"`
	Source       string        `json:"source" enum:"provider_list,google_billing,litellm,owner" doc:"Where the price came from (AI-82)." example:"litellm"`
	Catalogue    string        `json:"catalogue,omitempty" format:"uuid" doc:"The catalogue download it came from, which every estimate made with it is reproduced with (AI-85)." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2c"`
	CatalogueKey string        `json:"catalogue_key,omitempty" doc:"The source's entry it matched." example:"claude-opus-4-6"`
	Match        string        `json:"match" enum:"exact,normalised,entered" doc:"exact: the source names this provider and model; normalised: only after removing a provider prefix or a version date, so an Owner confirms it; entered: an Owner's price." example:"exact"`
	Currency     string        `json:"currency" minLength:"3" maxLength:"3" example:"USD"`
	Rates        []AIPriceRate `json:"rates" nullable:"false"`
	Ignored      []PriceField  `json:"ignored" nullable:"false" doc:"The source's rates Fyniq does not price with."`
	Status       string        `json:"status" enum:"active,pending,superseded,declined" doc:"active: in force; pending: waiting for an Owner's review, the version before it staying in force." example:"active"`
	Review       string        `json:"review,omitempty" enum:"normalised,drop" doc:"Why it waits, or waited, for an Owner's review." example:"drop"`
	ReviewDetail string        `json:"review_detail,omitempty" example:"input fell from 3 to 1.2"`
	RecordedBy   string        `json:"recorded_by" doc:"The system user for a read; the Owner for an entered price." example:"system:ai.prices.read"`
	RecordedAt   time.Time     `json:"recorded_at" example:"2026-11-14T06:15:00Z"`
	DecidedBy    string        `json:"decided_by,omitempty" doc:"The Owner who confirmed or declined it." example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
	DecidedAt    *time.Time    `json:"decided_at,omitempty" example:"2026-11-14T09:00:00Z"`
}

// AIPriceCatalogue is one download of a public price catalogue (AI-85): the same for every organisation.
type AIPriceCatalogue struct {
	ID            string    `json:"id" format:"uuid" example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2c"`
	Source        string    `json:"source" enum:"litellm" example:"litellm"`
	URL           string    `json:"url" example:"https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"`
	FetchedAt     time.Time `json:"fetched_at" example:"2026-11-14T06:15:00Z"`
	SHA256        string    `json:"sha256" pattern:"^[0-9a-f]{64}$" example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Size          int64     `json:"size" doc:"Its size in bytes." example:"4072360"`
	Licence       string    `json:"licence,omitempty" doc:"What the licence check found; absent when it failed." example:"MIT, outside enterprise/"`
	LicenceURL    string    `json:"licence_url" example:"https://raw.githubusercontent.com/BerriAI/litellm/main/LICENSE"`
	LicenceSHA256 string    `json:"licence_sha256" pattern:"^[0-9a-f]{64}$" example:"2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"`
	Outcome       string    `json:"outcome" enum:"valid,rejected" doc:"Only a valid download prices anything; a rejected one changes no price." example:"valid"`
	Failure       string    `json:"failure,omitempty" doc:"Why it was rejected." example:"the catalogue prices 12 chat models, fewer than 100: it is not a whole catalogue"`
	Entries       int       `json:"entries" doc:"The chat models it prices." example:"2150"`
}

// AIPriceCataloguePage is a page of catalogue downloads, newest first.
type AIPriceCataloguePage struct {
	Items      []AIPriceCatalogue `json:"items" nullable:"false"`
	NextCursor string             `json:"next_cursor,omitempty" example:"eyJhZnRlciI6IjEwMDAifQ"`
}

// AIPriceReview is an Owner's review of a price waiting for one.
type AIPriceReview struct {
	Decision string `json:"decision" enum:"confirm,decline" doc:"confirm puts the price in force; decline keeps the one before it." example:"confirm"`
}

type (
	aiPriceCatalogueListInput struct {
		PageQuery
	}
	aiPriceCatalogueListOutput struct{ Body AIPriceCataloguePage }
	aiPriceReviewInput         struct {
		PriceID string `path:"price_id" format:"uuid" doc:"The price version waiting for a review." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
		Body    AIPriceReview
	}
	aiPriceOutput struct{ Body AIPrice }
)

func priceOut(p storage.AIPrice) AIPrice {
	out := AIPrice{ID: p.ID.String(), Number: p.Number, Source: p.Source, CatalogueKey: p.CatalogueKey, Match: p.Match, Currency: p.Currency,
		Rates: []AIPriceRate{}, Ignored: []PriceField{}, Status: p.Status, Review: p.Review, ReviewDetail: p.ReviewDetail, RecordedBy: p.RecordedBy,
		RecordedAt: p.RecordedAt.UTC(), DecidedBy: p.DecidedBy}
	if p.CatalogueID != nil {
		out.Catalogue = p.CatalogueID.String()
	}
	for _, r := range p.Rates {
		out.Rates = append(out.Rates, AIPriceRate{Kind: r.Kind, Above: r.Above, Unit: r.Unit, Amount: aiprice.Trim(r.Amount)})
	}
	for _, f := range p.Ignored {
		out.Ignored = append(out.Ignored, PriceField(f))
	}
	if p.DecidedAt != nil {
		at := p.DecidedAt.UTC()
		out.DecidedAt = &at
	}
	return out
}

func catalogueOut(c storage.AIPriceCatalogue) AIPriceCatalogue {
	return AIPriceCatalogue{ID: c.ID.String(), Source: c.Source, URL: c.URL, FetchedAt: c.FetchedAt.UTC(), SHA256: c.SHA256, Size: c.Size,
		Licence: c.Licence, LicenceURL: c.LicenceURL, LicenceSHA256: c.LicenceSHA256, Outcome: c.Outcome, Failure: c.Failure, Entries: c.Entries}
}

// withPrices puts each listed model's price in force, the one waiting for a review, and why it has none.
func withPrices(ctx context.Context, u storage.UnitOfWork, connection storage.AIConnection, models []AIModel, stored []storage.AIModel) ([]AIModel, error) {
	current, err := u.AIPrices().Current(ctx, connection.ID)
	if err != nil {
		return nil, err
	}
	for i := range models {
		for _, p := range current {
			if p.Model != models[i].Model {
				continue
			}
			out := priceOut(p)
			switch p.Status {
			case aiprice.Active:
				models[i].Price = &out
			case aiprice.Pending:
				models[i].PendingPrice = &out
			}
		}
		for _, m := range stored {
			if m.Model == models[i].Model {
				models[i].Unpriced = m.Unpriced
				if m.PriceCheckedAt != nil {
					at := m.PriceCheckedAt.UTC()
					models[i].PriceChecked = &at
				}
			}
		}
		models[i].Priced = models[i].Price != nil
		models[i].Serves = aiprice.Serves(connection.Billing, connection.SelfHosted, models[i].Priced, models[i].Model) == nil
	}
	return models, nil
}

// priceProblem is a refusal of the price rules, with the field it is about.
func priceProblem(err error) error {
	var e *aiprice.Error
	if errors.As(err, &e) {
		return problem(e.Code, e.Field+" "+e.Message+".", e.Fix, FieldProblem{Code: e.Code, Field: "body." + e.Field, Message: e.Message, Fix: e.Fix})
	}
	return err
}

func registerAIPrices(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListAIPriceCatalogues, huma.Operation{
		OperationID: "listAIPriceCatalogues", Method: http.MethodGet, Path: "/v1/ai/price-catalogues", Tags: []string{"AI"},
		Summary: "List the price catalogue's downloads",
		Description: "Each daily download of LiteLLM's public model price catalogue, newest first (AI-85): the same for every organisation, " +
			"with its SHA-256, size, source, licence check and whether it was valid or why it was refused. Only a valid download prices " +
			"anything. A page at a time. Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *aiPriceCatalogueListInput) (*aiPriceCatalogueListOutput, error) {
		p, _ := authn.From(ctx)
		out := aiPriceCatalogueListOutput{Body: AIPriceCataloguePage{Items: []AIPriceCatalogue{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.AIPrices().Catalogues(ctx, 1000)
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(c storage.AIPriceCatalogue) string { return c.ID.String() }, in.PageQuery)
			if err != nil {
				return err
			}
			out.Body.NextCursor = next
			for _, c := range page {
				out.Body.Items = append(out.Body.Items, catalogueOut(c))
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.ReviewAIPrice, exempt(exempt(huma.Operation{
		OperationID: "reviewAIPrice", Method: http.MethodPost, Path: "/v1/ai/prices/{price_id}/review", Tags: []string{"AI"},
		Summary: "Review a model's price waiting for the Owner",
		Description: "The Owner confirms or declines a price a read could not put in force by itself (AI-82, AI-85): a match found only after " +
			"removing a provider prefix or a version date, or a rate that fell by more than half. Confirm puts it in force; decline keeps the " +
			"price before it. An Owner's own price is never replaced by a read. Side effects: the audit event ai_price.confirmed or " +
			"ai_price.declined. On the never-list for the AI and MCP (§9.6).",
	}, "Idempotency-Key", "A version is reviewed once: a second review is refused as no longer pending, so the request is idempotent by itself."),
		"Prefer", "A review decides one version, with nothing to preview."),
		append([]string{CodeNotFound, aiprice.CodeReview, storage.CodeAIPrice}, locks...), func(ctx context.Context, in *aiPriceReviewInput) (*aiPriceOutput, error) {
			p, _ := authn.From(ctx)
			id, err := parseID("price_id", in.PriceID)
			if err != nil {
				return nil, err
			}
			var out storage.AIPrice
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				v, err := u.AIPrices().Get(ctx, id)
				if err != nil {
					return err
				}
				if err := aiprice.Decide(v.Status, in.Body.Decision); err != nil {
					return priceProblem(err)
				}
				out, err = u.AIPrices().Decide(ctx, id, in.Body.Decision == aiprice.Confirm, uploader(ctx), a.deps.Clock.Now())
				return err
			})
			if err != nil {
				return nil, err
			}
			return &aiPriceOutput{Body: priceOut(out)}, nil
		})
}
