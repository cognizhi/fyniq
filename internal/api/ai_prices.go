package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aiprice"
	"github.com/cognizhi/fyniq/internal/domain/approval"
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
	Status       string        `json:"status" enum:"active,pending,superseded,declined,withdrawn" doc:"active: in force; pending: waiting for an Owner's review, the version before it staying in force; withdrawn: an Owner's price its Owner withdrew." example:"active"`
	Review       string        `json:"review,omitempty" enum:"normalised,drop" doc:"Why it waits, or waited, for an Owner's review." example:"drop"`
	ReviewDetail string        `json:"review_detail,omitempty" example:"input fell from 3 to 1.2"`
	RecordedBy   string        `json:"recorded_by" doc:"The system user for a read; for an Owner's price, the Owner who entered it." example:"system:ai.prices.read"`
	Approval     string        `json:"approval,omitempty" format:"uuid" doc:"An Owner's price: the request its second approver approved (AI-82)." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2e"`
	RecordedAt   time.Time     `json:"recorded_at" example:"2026-11-14T06:15:00Z"`
	DecidedBy    string        `json:"decided_by,omitempty" doc:"The Owner who confirmed or declined it, or withdrew their own." example:"user:0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2d"`
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

// AIPriceRateEntry is a rate of an Owner's price.
type AIPriceRateEntry struct {
	Kind   string `json:"kind" enum:"input,output,cache_read,cache_write,cache_write_1h,reasoning,input_image,request" example:"input"`
	Above  int64  `json:"above,omitempty" minimum:"0" doc:"The prompt length in tokens above which it applies; 0 or absent for the base rate." example:"0"`
	Amount string `json:"amount" maxLength:"25" doc:"A plain decimal above zero with at most 10 decimal places: per million tokens, per image or per request as the kind's unit says." example:"3"`
}

// AIModelPrice is an Owner's price of a model a connection's provider lists (AI-82).
type AIModelPrice struct {
	Model    string             `json:"model" minLength:"1" maxLength:"200" doc:"The provider's ID for it, as the connection's list shows it." example:"gpt-6"`
	Currency string             `json:"currency" minLength:"3" maxLength:"3" doc:"The currency the provider bills in." example:"USD"`
	Rates    []AIPriceRateEntry `json:"rates" minItems:"2" maxItems:"40" doc:"At least an input and an output rate."`
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
	aiPriceOutput        struct{ Body AIPrice }
	aiPriceWithdrawInput struct {
		PriceID string `path:"price_id" format:"uuid" doc:"The Owner's price in force." example:"0193a3f2-7c1e-7a2b-9c4d-5e6f70819a2b"`
	}
	aiModelPriceInput struct {
		AIConnectionPath
		KeyHeader
		PreferHeader
		Body AIModelPrice
	}
)

// entered is an Owner's price as the domain checks it.
func entered(currency string, rates []AIPriceRateEntry) (aiprice.Price, error) {
	in := make([]aiprice.EnteredRate, 0, len(rates))
	for _, r := range rates {
		in = append(in, aiprice.EnteredRate{Kind: r.Kind, Above: r.Above, Amount: r.Amount})
	}
	p, err := aiprice.Entered(currency, in)
	return p, priceProblem(err)
}

// executeAIPrice records an Owner's price as its second approver approved it, checked once more (AI-82): the model
// still listed, the price still valid. It supersedes the price in force and any waiting, and no read overwrites it.
func (a *API) executeAIPrice(ctx context.Context, u storage.UnitOfWork, c storage.AIConnection, r storage.ApprovalRequest, str func(string) string) error {
	var rates []AIPriceRateEntry
	if err := json.Unmarshal([]byte(str("rates")), &rates); err != nil {
		return err
	}
	p, err := entered(str("currency"), rates)
	if err != nil {
		return err
	}
	model := str("model")
	if err := listedModel(ctx, u, c.ID, model); err != nil {
		return err
	}
	next := storage.AIPrice{ConnectionID: c.ID, Model: model, Source: aiprice.SourceOwner, Match: aiprice.MatchEntered, Currency: p.Currency,
		Status: aiprice.Active, RecordedBy: "user:" + r.RequestedBy.String(), RecordedAt: a.deps.Clock.Now(), Approval: &r.ID}
	for _, rate := range p.Rates {
		next.Rates = append(next.Rates, storage.AIPriceRate{Kind: rate.Kind, Above: rate.Above, Unit: rate.Unit(), Amount: rate.Amount.Text('f')})
	}
	_, err = u.AIPrices().Record(ctx, next)
	return err
}

// listedModel refuses a model the connection's provider never listed: there is no typed model ID (AI-80).
func listedModel(ctx context.Context, u storage.UnitOfWork, connection uuid.UUID, model string) error {
	all, err := u.AIModels().Of(ctx, connection)
	if err != nil {
		return err
	}
	for _, m := range all {
		if m.Model == model {
			return nil
		}
	}
	return problem(CodeNotFound, model+" is not a model this connection's provider lists.", "Refresh its models and choose one it lists")
}

func priceOut(p storage.AIPrice) AIPrice {
	out := AIPrice{ID: p.ID.String(), Number: p.Number, Source: p.Source, CatalogueKey: p.CatalogueKey, Match: p.Match, Currency: p.Currency,
		Rates: []AIPriceRate{}, Ignored: []PriceField{}, Status: p.Status, Review: p.Review, ReviewDetail: p.ReviewDetail, RecordedBy: p.RecordedBy,
		RecordedAt: p.RecordedAt.UTC(), DecidedBy: p.DecidedBy}
	if p.CatalogueID != nil {
		out.Catalogue = p.CatalogueID.String()
	}
	if p.Approval != nil {
		out.Approval = p.Approval.String()
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

	register(a, authz.PriceAIConnectionModel, huma.Operation{
		OperationID: "priceAIConnectionModel", Method: http.MethodPost, Path: "/v1/ai/connections/{connection_id}/models/price", Tags: []string{"AI"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask to put an Owner's price on a model",
		Description: "An Owner prices a model the connection's provider lists, when no source prices it or its source's price should not " +
			"apply (AI-82): rates per million tokens, per image or per request, as plain decimals above zero, in the provider's currency, " +
			"with at least an input and an output rate. It is dual controlled: the answer is the approval request, which a second approver " +
			"approves. In force, it is labelled with who entered it, and no later read overwrites it until the Owner withdraws it. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing. On the never-list for the AI and MCP (§9.6).",
	}, append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired,
		approval.CodePending, aiprice.CodeEntered}, locks...), func(ctx context.Context, in *aiModelPriceInput) (*aiConnectionAskOutput, error) {
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			id, err := parseID("connection_id", in.ConnectionID)
			if err != nil {
				return Approval{}, err
			}
			c, err := u.AIConnections().Get(ctx, id)
			if err != nil {
				return Approval{}, err
			}
			if err := listedModel(ctx, u, id, in.Body.Model); err != nil {
				return Approval{}, err
			}
			p, err := entered(in.Body.Currency, in.Body.Rates)
			if err != nil {
				return Approval{}, err
			}
			rates, err := json.Marshal(in.Body.Rates)
			if err != nil {
				return Approval{}, err
			}
			diff := make([]ApprovalDiff, 0, len(p.Rates))
			for _, r := range p.Rates {
				field := "price." + r.Kind
				if r.Above > 0 {
					field += fmt.Sprintf(" above %d", r.Above)
				}
				diff = append(diff, ApprovalDiff{Field: field, After: p.Currency + " " + r.Amount.Text('f') + " " + r.Unit()})
			}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectAIConnection, ObjectKey: id.String(),
				Operation: approval.OpAIConnectionPrice, Summary: "Price " + in.Body.Model + " on the AI connection " + c.Name,
				Execute: map[string]any{"id": id.String(), "model": in.Body.Model, "currency": p.Currency, "rates": string(rates)}, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &aiConnectionAskOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
	})

	register(a, authz.WithdrawAIPrice, exempt(exempt(huma.Operation{
		OperationID: "withdrawAIPrice", Method: http.MethodPost, Path: "/v1/ai/prices/{price_id}/withdraw", Tags: []string{"AI"},
		Summary: "Withdraw an Owner's price",
		Description: "The Owner withdraws an Owner's price in force at once (AI-82): never held back, as a model without it is priced again " +
			"from its source or stays unpriced, and an unpriced model on a usage-based connection serves no route. The connection's models are " +
			"priced again from the latest download straight away. Side effects: the audit event ai_price.withdrawn. On the never-list for the " +
			"AI and MCP (§9.6).",
	}, "Idempotency-Key", "A price is withdrawn once: a second withdrawal is refused as no longer in force, so the request is idempotent by itself."),
		"Prefer", "Withdrawing is never held back; there is nothing to preview."),
		append([]string{CodeNotFound, aiprice.CodeWithdraw, storage.CodeAIPrice}, locks...), func(ctx context.Context, in *aiPriceWithdrawInput) (*aiPriceOutput, error) {
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
				if err := aiprice.Withdrawable(v.Source, v.Status); err != nil {
					return priceProblem(err)
				}
				if out, err = u.AIPrices().Withdraw(ctx, id, uploader(ctx), a.deps.Clock.Now()); err != nil {
					return err
				}
				item, err := ai.PriceItem(v.ConnectionID, id)
				if err != nil {
					return err
				}
				if a.deps.Outbox != nil {
					_, err = a.deps.Outbox.Enqueue(ctx, u, p.Organisation, item)
					return err
				}
				_, err = u.Outbox().Enqueue(ctx, item) // no dispatcher here: the tick sends it
				return err
			})
			if err != nil {
				return nil, err
			}
			return &aiPriceOutput{Body: priceOut(out)}, nil
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
