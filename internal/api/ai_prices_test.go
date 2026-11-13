//go:build local

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// mitLicence is LiteLLM's licence as the check reads it.
const mitLicence = "Portions of this software are licensed as follows:\n* All content under enterprise/ is licensed under enterprise/LICENSE.\n\n" +
	"MIT License\n\nCopyright (c) 2023 Berri AI\n\nPermission is hereby granted, free of charge, to any person obtaining a copy"

// fakeCatalogue is the price catalogue as its source serves it now.
type fakeCatalogue struct {
	body, licence []byte
}

func (f *fakeCatalogue) Fetch(context.Context) (ports.PriceDocument, error) {
	return ports.PriceDocument{Source: "litellm", URL: "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json",
		Path: "model_prices_and_context_window.json", Body: f.body, LicenceURL: "https://raw.githubusercontent.com/BerriAI/litellm/main/LICENSE",
		Licence: f.licence}, nil
}

// liteLLM is a catalogue in LiteLLM's format: the entries given, each "key=provider:input:output" per token, and enough
// filler chat models to be a whole one.
func liteLLM(entries ...string) []byte {
	var b strings.Builder
	b.WriteString(`{"sample_spec": {"input_cost_per_token": 0}`)
	for i := range 100 {
		fmt.Fprintf(&b, `, "filler-%03d": {"litellm_provider": "filler", "mode": "chat", "input_cost_per_token": 1e-06, "output_cost_per_token": 2e-06}`, i)
	}
	for _, e := range entries {
		key, rest, _ := strings.Cut(e, "=")
		parts := strings.Split(rest, ":")
		fmt.Fprintf(&b, `, %q: {"litellm_provider": %q, "mode": "chat", "input_cost_per_token": %s, "output_cost_per_token": %s, "input_cost_per_token_batches": 1e-06}`,
			key, parts[0], parts[1], parts[2])
	}
	b.WriteString("}")
	return []byte(b.String())
}

// Prices come from their source (AI-82, AI-85): the worker reads LiteLLM's catalogue daily, keeps each download with
// its digest and licence check, and prices each listed model from it as exact decimals: exactly from an entry of the
// provider that names it, or, waiting for the Owner, only after normalising; a rate that falls by more than half waits
// for the Owner too; a model nothing prices stays unpriced, saying why, and cannot serve a usage-based route. A rejected
// download, by its licence or its form, changes no price; a document changed after its download is never used. New
// models are priced from the latest download as soon as a read lists them. Every price change is in the trail.
// req: AI-82
// req: AI-85
func TestPricesComeFromTheirSource(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	controller := f.as(f.ahkow, authz.Controller)
	models := &fakeModels{ids: []string{"gpt-test-6", "gpt-test-5", "gpt-test-9"}}
	e.api.deps.AIProviders = models.provider
	asked := e.call(http.MethodPost, "/v1/ai/connections", admin, map[string]any{"name": "Priced", "connector": "openai", "auth": "api-key",
		"billing": map[string]any{"mode": "usage"}, "credential": "sk-proj-" + "fixture0123456789abcdef"})
	if d := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	id := e.call(http.MethodGet, "/v1/ai/connections", admin, nil).Body["items"].([]any)[0].(map[string]any)["id"].(string)

	source := &fakeCatalogue{licence: []byte(mitLicence), body: liteLLM("gpt-test-6=openai:2e-06:8e-06", "gpt-test-5-2026-08-07=openai:1.25e-06:1e-05",
		"azure/gpt-test-9=azure:1e-06:1e-06", "gpt-test-7=openai:3e-07:1.2e-06")}
	worker := workerStore(t)
	pricer := ai.Pricer{Store: worker, Catalogues: worker, Blobs: e.blobs, Catalogue: source, Connectors: e.api.deps.Connectors.Catalogue, Clock: e.clock}
	lister := ai.Lister{Store: worker, Provider: models.provider, Clock: e.clock, Outbox: e.api.deps.Outbox}
	works := map[string]outbox.Work{ai.ReadKind: lister.Work(), ai.PriceKind: pricer.Work(),
		notify.FanoutKind: func(context.Context, uuid.UUID, storage.OutboxRow) error { return nil }}
	// The key's approval queued a read, which queued pricing; with no download yet, the models say so.
	e.work(works)
	byModel := func() map[string]AIModel {
		t.Helper()
		var out AIModelList
		r := e.call(http.MethodGet, "/v1/ai/connections/"+id+"/models", admin, nil)
		if r.Status != http.StatusOK || json.Unmarshal([]byte(r.Raw), &out) != nil {
			t.Fatalf("getAIConnectionModels: %d %s", r.Status, r.Raw)
		}
		m := map[string]AIModel{}
		for _, x := range out.Models {
			m[x.Model] = x
		}
		return m
	}
	if m := byModel(); len(m) != 3 || m["gpt-test-6"].Priced || m["gpt-test-6"].Serves || !strings.Contains(m["gpt-test-6"].Unpriced, "no valid download") {
		t.Fatalf("before any download: %+v", m)
	}

	daily := func() {
		t.Helper()
		if err := pricer.Daily(context.Background()); err != nil {
			t.Fatalf("the daily read: %v", err)
		}
	}
	daily()
	m := byModel()
	exact, normalised, none := m["gpt-test-6"], m["gpt-test-5"], m["gpt-test-9"]
	if exact.Price == nil || exact.Price.Source != "litellm" || exact.Price.Match != "exact" || exact.Price.CatalogueKey != "gpt-test-6" ||
		exact.Price.Currency != "USD" || len(exact.Price.Rates) != 2 || exact.Price.Rates[0].Kind != "input" || exact.Price.Rates[0].Amount != "2" ||
		exact.Price.Rates[1].Amount != "8" || exact.Price.Rates[0].Unit != "per_million_tokens" || len(exact.Price.Ignored) != 1 ||
		!exact.Priced || !exact.Serves || exact.Price.RecordedBy != "system:"+ai.PriceJob || exact.Price.Catalogue == "" {
		t.Errorf("an exact match: %+v %+v", exact, exact.Price)
	}
	if normalised.Price != nil || normalised.PendingPrice == nil || normalised.PendingPrice.Match != "normalised" ||
		normalised.PendingPrice.Review != "normalised" || normalised.PendingPrice.CatalogueKey != "gpt-test-5-2026-08-07" || normalised.Serves {
		t.Errorf("a normalised match: %+v %+v", normalised, normalised.PendingPrice)
	}
	if none.Price != nil || none.PendingPrice != nil || none.Serves || !strings.Contains(none.Unpriced, "no entry of openai names gpt-test-9") ||
		none.PriceChecked == nil {
		t.Errorf("an unpriced model: %+v", none)
	}
	cats := e.call(http.MethodGet, "/v1/ai/price-catalogues", admin, nil)
	items := ownDownloads(cats)
	sum := sha256.Sum256(source.body)
	if cats.Status != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["sha256"] != hex.EncodeToString(sum[:]) ||
		items[0].(map[string]any)["outcome"] != "valid" || items[0].(map[string]any)["licence"] != "MIT, outside enterprise/" {
		t.Fatalf("listAIPriceCatalogues: %d %s", cats.Status, cats.Raw)
	}

	// The Owner confirms the normalised match; an Administrator cannot; a decided price cannot be reviewed again.
	review := func(as, price, decision string) response {
		return e.call(http.MethodPost, "/v1/ai/prices/"+price+"/review", as, map[string]any{"decision": decision})
	}
	if r := review(controller, normalised.PendingPrice.ID, "confirm"); r.Status != http.StatusForbidden {
		t.Errorf("a Controller reviewing: %d %s", r.Status, r.Raw)
	}
	if r := review(owner, normalised.PendingPrice.ID, "approve"); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a decision that is not one: %d %s", r.Status, r.Raw)
	}
	if r := review(owner, normalised.PendingPrice.ID, "confirm"); r.Status != http.StatusOK || r.str("status") != "active" {
		t.Fatalf("the Owner confirming: %d %s", r.Status, r.Raw)
	}
	if r := review(owner, normalised.PendingPrice.ID, "decline"); r.Status != http.StatusConflict || !strings.Contains(r.Raw, "AI-PRICE-02") {
		t.Errorf("reviewing twice: %d %s", r.Status, r.Raw)
	}
	if n := byModel()["gpt-test-5"]; n.Price == nil || n.Price.DecidedBy == "" || !n.Serves || n.PendingPrice != nil {
		t.Errorf("after the confirmation: %+v", n)
	}

	// The same catalogue again changes nothing; a rate that falls by more than half waits, and the Owner declines it.
	daily()
	source.body = liteLLM("gpt-test-6=openai:5e-07:8e-06", "gpt-test-5-2026-08-07=openai:1.25e-06:1e-05", "gpt-test-7=openai:3e-07:1.2e-06")
	daily()
	held := byModel()["gpt-test-6"]
	if held.Price == nil || held.Price.Rates[0].Amount != "2" || held.PendingPrice == nil || held.PendingPrice.Review != "drop" ||
		!strings.Contains(held.PendingPrice.ReviewDetail, "input fell from 2 to 0.5") {
		t.Fatalf("a held drop: %+v %+v", held.Price, held.PendingPrice)
	}
	if r := review(owner, held.PendingPrice.ID, "decline"); r.Status != http.StatusOK || r.str("status") != "declined" {
		t.Fatalf("the Owner declining: %d %s", r.Status, r.Raw)
	}
	if g := byModel()["gpt-test-6"]; g.Price == nil || g.Price.Rates[0].Amount != "2" || g.PendingPrice != nil {
		t.Errorf("after declining: %+v", g)
	}

	// A download under another licence, or not a whole catalogue, is kept as rejected and changes no price.
	valid := source.body
	source.licence = []byte("All rights reserved.")
	daily()
	source.licence, source.body = []byte(mitLicence), []byte(`{"gpt-test-6": {"litellm_provider": "openai", "input_cost_per_token": 1e-09}}`)
	daily()
	cats = e.call(http.MethodGet, "/v1/ai/price-catalogues", admin, nil)
	items = ownDownloads(cats)
	if len(items) != 5 || items[0].(map[string]any)["outcome"] != "rejected" || !strings.Contains(items[0].(map[string]any)["failure"].(string), "fewer than 100") ||
		items[1].(map[string]any)["outcome"] != "rejected" || !strings.Contains(items[1].(map[string]any)["failure"].(string), "MIT") {
		t.Fatalf("the rejected downloads: %s", cats.Raw)
	}
	if g := byModel()["gpt-test-6"]; g.Price == nil || g.Price.Rates[0].Amount != "2" {
		t.Errorf("a rejected download changed a price: %+v", g.Price)
	}

	// A model a later read lists is priced from the latest valid download at once.
	models.ids = append(models.ids, "gpt-test-7")
	if r := e.call(http.MethodPost, "/v1/ai/connections/"+id+"/models/refresh", admin, nil); r.Status != http.StatusOK {
		t.Fatalf("refreshing: %d %s", r.Status, r.Raw)
	}
	e.work(works)
	if n := byModel()["gpt-test-7"]; n.Price == nil || n.Price.Rates[0].Amount != "0.3" || n.Price.Rates[1].Amount != "1.2" {
		t.Errorf("a new model: %+v", n)
	}

	// A kept document that no longer matches its digest is never used.
	sum = sha256.Sum256(valid)
	if err := e.blobs.Put(context.Background(), "ai/price-catalogues/litellm/"+hex.EncodeToString(sum[:])+".json", bytes.NewReader([]byte("{}"))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pricer.Latest(context.Background(), e.books.Org); err == nil || !strings.Contains(err.Error(), "no longer matches its digest") {
		t.Errorf("a changed document: %v", err)
	}

	var changed, confirmed, declined int
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		for action, n := range map[string]*int{"ai_price.changed": &changed, "ai_price.confirmed": &confirmed, "ai_price.declined": &declined} {
			all, err := u.Audit().Find(ctx, storage.AuditQuery{ObjectType: "ai_price", Action: action, Limit: 100})
			if err != nil {
				return err
			}
			*n = len(all)
		}
		return nil
	})
	if changed != 4 || confirmed != 1 || declined != 1 {
		t.Errorf("the trail: %d changes, %d confirmed, %d declined", changed, confirmed, declined)
	}
}

// An Owner's price takes a second approver (AI-82): an Owner asks, with at least an input and an output rate as plain
// decimals above zero; another Owner approves; it is then in force, labelled with who entered it, and no read overwrites
// it. Withdrawn at once by the Owner, the model is priced from its source again straight away. Neither an Administrator
// nor the requester decides it.
// req: AI-82
// asvs: V8.2.1
func TestAnOwnersPriceTakesASecondApprover(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	adminOnly := f.as(f.ahkow, authz.Administrator)
	models := &fakeModels{ids: []string{"gpt-test-6", "gpt-test-9"}}
	e.api.deps.AIProviders = models.provider
	asked := e.call(http.MethodPost, "/v1/ai/connections", admin, map[string]any{"name": "Owner priced", "connector": "openai", "auth": "api-key",
		"billing": map[string]any{"mode": "usage"}, "credential": "sk-proj-" + "fixture0123456789abcdef"})
	if d := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	id := e.call(http.MethodGet, "/v1/ai/connections", admin, nil).Body["items"].([]any)[0].(map[string]any)["id"].(string)
	worker := workerStore(t)
	source := &fakeCatalogue{licence: []byte(mitLicence), body: liteLLM("gpt-test-6=openai:2e-06:8e-06")}
	pricer := ai.Pricer{Store: worker, Catalogues: worker, Blobs: e.blobs, Catalogue: source, Connectors: e.api.deps.Connectors.Catalogue, Clock: e.clock}
	lister := ai.Lister{Store: worker, Provider: models.provider, Clock: e.clock, Outbox: e.api.deps.Outbox}
	works := map[string]outbox.Work{ai.ReadKind: lister.Work(), ai.PriceKind: pricer.Work(),
		notify.FanoutKind: func(context.Context, uuid.UUID, storage.OutboxRow) error { return nil }}
	e.work(works)
	if err := pricer.Daily(context.Background()); err != nil {
		t.Fatal(err)
	}
	byModel := func() map[string]AIModel {
		t.Helper()
		var out AIModelList
		r := e.call(http.MethodGet, "/v1/ai/connections/"+id+"/models", admin, nil)
		if r.Status != http.StatusOK || json.Unmarshal([]byte(r.Raw), &out) != nil {
			t.Fatalf("getAIConnectionModels: %d %s", r.Status, r.Raw)
		}
		m := map[string]AIModel{}
		for _, x := range out.Models {
			m[x.Model] = x
		}
		return m
	}
	if m := byModel(); m["gpt-test-9"].Priced || !m["gpt-test-6"].Priced {
		t.Fatalf("before the Owner's prices: %+v", m)
	}

	base := "/v1/ai/connections/" + id + "/models/price"
	price := func(model string, rates ...any) map[string]any {
		out := map[string]any{"model": model, "currency": "USD", "rates": []any{}}
		for i := 0; i+1 < len(rates); i += 2 {
			out["rates"] = append(out["rates"].([]any), map[string]any{"kind": rates[i], "amount": rates[i+1]})
		}
		return out
	}
	e.problemIs(e.call(http.MethodPost, base, steppedUp(e, admin), price("gpt-test-9", "input", "1.5")), "priceAIConnectionModel", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodPost, base, steppedUp(e, admin), price("gpt-test-9", "input", "1.5", "cache_read", "0.15")), "priceAIConnectionModel", 422, "AI-PRICE-03")
	e.problemIs(e.call(http.MethodPost, base, steppedUp(e, admin), price("gpt-test-9", "input", "1.5", "output", "0")), "priceAIConnectionModel", 422, "AI-PRICE-03")
	e.problemIs(e.call(http.MethodPost, base, steppedUp(e, admin), price("typed-by-hand", "input", "1", "output", "2")), "priceAIConnectionModel", 404, CodeNotFound)
	if r := e.call(http.MethodPost, base, steppedUp(e, adminOnly), price("gpt-test-9", "input", "1.5", "output", "6")); r.Status != http.StatusForbidden {
		t.Errorf("an Administrator pricing: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, base, steppedUp(e, admin), price("gpt-test-9", "input", "1.5", "output", "6"), "Prefer", "return=preview"); r.Status != http.StatusAccepted ||
		byModel()["gpt-test-9"].Priced {
		t.Errorf("a preview: %d %s", r.Status, r.Raw)
	}
	ask := e.call(http.MethodPost, base, steppedUp(e, admin), price("gpt-test-9", "input", "1.5", "output", "6", "cache_read", "0.15"))
	if ask.Status != http.StatusAccepted || ask.str("operation") != "ai_connection.price" {
		t.Fatalf("asking: %d %s", ask.Status, ask.Raw)
	}
	if d := f.decide(admin, ask.str("id"), "approve", hashOf(ask)); d.Status == http.StatusOK {
		t.Errorf("the requester approving their own price: %s", d.Raw)
	}
	if byModel()["gpt-test-9"].Priced {
		t.Fatal("priced before its second approver")
	}
	if d := f.decide(owner, ask.str("id"), "approve", hashOf(ask)); d.Status != http.StatusOK {
		t.Fatalf("the second approver: %d %s", d.Status, d.Raw)
	}
	entered := byModel()["gpt-test-9"]
	if p := entered.Price; p == nil || p.Source != "owner" || p.Match != "entered" || p.Approval != ask.str("id") || p.RecordedBy != "user:"+f.ahkow ||
		len(p.Rates) != 3 || p.Rates[0].Kind != "cache_read" || p.Rates[0].Amount != "0.15" || !entered.Serves {
		t.Fatalf("the Owner's price: %+v %+v", entered, entered.Price)
	}

	// An Owner's price over the catalogue's: kept by every later read until the Owner withdraws it.
	ask = e.call(http.MethodPost, base, steppedUp(e, admin), price("gpt-test-6", "input", "1", "output", "4"))
	if d := f.decide(owner, ask.str("id"), "approve", hashOf(ask)); d.Status != http.StatusOK {
		t.Fatalf("pricing gpt-test-6: %d %s", d.Status, d.Raw)
	}
	source.body = liteLLM("gpt-test-6=openai:2.5e-06:9e-06")
	if err := pricer.Daily(context.Background()); err != nil {
		t.Fatal(err)
	}
	kept := byModel()["gpt-test-6"]
	if kept.Price == nil || kept.Price.Source != "owner" || kept.Price.Rates[0].Amount != "1" || kept.PendingPrice != nil {
		t.Fatalf("a read over an Owner's price: %+v", kept)
	}
	withdraw := func(as, price string) response {
		return e.call(http.MethodPost, "/v1/ai/prices/"+price+"/withdraw", as, nil)
	}
	if r := withdraw(adminOnly, kept.Price.ID); r.Status != http.StatusForbidden {
		t.Errorf("an Administrator withdrawing: %d %s", r.Status, r.Raw)
	}
	if r := withdraw(owner, kept.Price.ID); r.Status != http.StatusOK || r.str("status") != "withdrawn" {
		t.Fatalf("withdrawing: %d %s", r.Status, r.Raw)
	}
	e.problemIs(withdraw(owner, kept.Price.ID), "withdrawAIPrice", 409, "AI-PRICE-04")
	e.work(works)
	again := byModel()["gpt-test-6"]
	if again.Price == nil || again.Price.Source != "litellm" || again.Price.Rates[0].Amount != "2.5" {
		t.Fatalf("priced from its source again: %+v", again.Price)
	}
	e.problemIs(withdraw(owner, again.Price.ID), "withdrawAIPrice", 409, "AI-PRICE-04")

	var withdrawn int
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Audit().Find(ctx, storage.AuditQuery{ObjectType: "ai_price", Action: "ai_price.withdrawn", Limit: 10})
		withdrawn = len(all)
		return err
	})
	if withdrawn != 1 {
		t.Errorf("the trail records %d withdrawals", withdrawn)
	}
}

// fakeBilling is Google's billing catalogue as it answers now.
type fakeBilling struct {
	skus []ports.BillingSKU
	err  error
}

func (f *fakeBilling) SKUs(context.Context) ([]ports.BillingSKU, error) { return f.skus, f.err }

// Sources are taken in AI-82's order: a provider's own list first (OpenRouter's), then Google's billing catalogue for
// Agent Platform, then LiteLLM's catalogue. A list's conditional price, or a model Google does not offer in the
// connection's region, falls to the next source; Google's match always waits for the Owner, as a SKU names its model in
// words; a price from Google stays in force while its catalogue cannot be read.
// req: AI-82
func TestPricesComeFirstFromTheProviderAndGoogle(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	owner := f.as(f.weiling, authz.Owner)
	ctx := context.Background()
	now := e.clock.Now()
	var routed, gemini storage.AIConnection
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if routed, err = u.AIConnections().Create(ctx, storage.AIConnection{Name: "OpenRouter", Connector: "openai-compatible", Preset: "openrouter",
			Auth: "api-key", Configuration: []byte(`{}`), Host: "openrouter.ai", Billing: "usage", CreatedBy: "user:x", CreatedAt: now},
			uuid.Must(uuid.NewV7())); err != nil {
			return err
		}
		gemini, err = u.AIConnections().Create(ctx, storage.AIConnection{Name: "Gemini", Connector: "gemini", Auth: "workload-identity",
			Configuration: []byte(`{"project":"fyniq-test","location":"asia-southeast1"}`), Host: "asia-southeast1-aiplatform.googleapis.com",
			Billing: "usage", CreatedBy: "user:x", CreatedAt: now}, uuid.Must(uuid.NewV7()))
		return err
	})
	listed := func(p ports.ListedPrice) json.RawMessage {
		b, _ := json.Marshal(p)
		return b
	}
	worker := workerStore(t)
	if err := worker.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		model := func(c storage.AIConnection, id, name string, price json.RawMessage) storage.AIModel {
			return storage.AIModel{ConnectionID: c.ID, Model: id, Name: name, FirstListed: now, LastListed: now, Listed: true, ListedPrice: price}
		}
		if _, err := u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: routed.ID, Trigger: "daily", Outcome: "complete", Pages: 1, Added: 3,
			ReadBy: "system:ai.models.read", ReadAt: now}, []storage.AIModel{
			model(routed, "vendor/listed-1", "Vendor: Listed 1", listed(ports.ListedPrice{Currency: "USD", PerToken: map[string]string{"input": "0.0000006", "output": "0.0000025"}})),
			model(routed, "vendor/discounted-1", "", listed(ports.ListedPrice{Currency: "USD", PerToken: map[string]string{"input": "0.000001", "output": "0.000002"}, Conditional: true})),
			model(routed, "vendor/unpriced-1", "", nil)}); err != nil {
			return err
		}
		_, err := u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: gemini.ID, Trigger: "daily", Outcome: "complete", Pages: 1, Added: 2,
			ReadBy: "system:ai.models.read", ReadAt: now}, []storage.AIModel{model(gemini, "gemini-test-3-pro", "Gemini Test 3 Pro", nil),
			model(gemini, "gemini-test-3-flash", "Gemini Test 3 Flash", nil)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sku := func(id, desc, price string, regions ...string) ports.BillingSKU {
		return ports.BillingSKU{ID: id, Description: desc, Regions: regions, UsageUnit: "count", Tiers: []ports.BillingTier{{StartUsage: "0", Currency: "USD", Price: price}}}
	}
	billing := &fakeBilling{skus: []ports.BillingSKU{sku("A1", "Gemini Test 3 Pro Input Text Tokens", "0.00000125", "asia-southeast1"),
		sku("A2", "Gemini Test 3 Pro Output Text Tokens", "0.00001", "global"), sku("B1", "Gemini Test 3 Flash Input Text Tokens", "0.0000003", "us-central1"),
		sku("B2", "Gemini Test 3 Flash Output Text Tokens", "0.0000025", "us-central1")}}
	source := &fakeCatalogue{licence: []byte(mitLicence), body: liteLLM("openrouter/vendor/discounted-1=openrouter:1e-06:2e-06",
		"gemini-test-3-flash=vertex_ai-language-models:3e-07:2.5e-06", "gemini-test-3-pro=vertex_ai-language-models:9e-06:3e-05")}
	pricer := ai.Pricer{Store: worker, Catalogues: worker, Blobs: e.blobs, Catalogue: source, Billing: billing, Connectors: e.api.deps.Connectors.Catalogue,
		Clock: e.clock}
	if err := pricer.Daily(ctx); err != nil {
		t.Fatal(err)
	}
	byModel := func(c storage.AIConnection) map[string]AIModel {
		t.Helper()
		var out AIModelList
		r := e.call(http.MethodGet, "/v1/ai/connections/"+c.ID.String()+"/models", owner, nil)
		if r.Status != http.StatusOK || json.Unmarshal([]byte(r.Raw), &out) != nil {
			t.Fatalf("getAIConnectionModels: %d %s", r.Status, r.Raw)
		}
		m := map[string]AIModel{}
		for _, x := range out.Models {
			m[x.Model] = x
		}
		return m
	}
	r := byModel(routed)
	if p := r["vendor/listed-1"].Price; p == nil || p.Source != "provider_list" || p.Match != "exact" || p.Catalogue != "" || p.Rates[0].Amount != "0.6" ||
		p.Rates[1].Amount != "2.5" {
		t.Errorf("the list's price: %+v", p)
	}
	if p := r["vendor/discounted-1"].Price; p == nil || p.Source != "litellm" || p.CatalogueKey != "openrouter/vendor/discounted-1" {
		t.Errorf("a conditional list price falls to the catalogue: %+v", p)
	}
	if m := r["vendor/unpriced-1"]; m.Priced || !strings.Contains(m.Unpriced, "no entry of openrouter names vendor/unpriced-1") {
		t.Errorf("an unpriced model: %+v", m)
	}
	g := byModel(gemini)
	pro, flash := g["gemini-test-3-pro"], g["gemini-test-3-flash"]
	if pro.Price != nil || pro.PendingPrice == nil || pro.PendingPrice.Source != "google_billing" || pro.PendingPrice.Match != "normalised" ||
		pro.PendingPrice.CatalogueKey != "sku:A1+A2" || pro.PendingPrice.Rates[0].Amount != "1.25" {
		t.Fatalf("Google's match waits for the Owner: %+v", pro)
	}
	if flash.Price == nil || flash.Price.Source != "litellm" || flash.Price.Rates[0].Amount != "0.3" {
		t.Errorf("a model Google does not offer here: %+v", flash)
	}
	if r := e.call(http.MethodPost, "/v1/ai/prices/"+pro.PendingPrice.ID+"/review", owner, map[string]any{"decision": "confirm"}); r.Status != http.StatusOK {
		t.Fatalf("confirming Google's match: %d %s", r.Status, r.Raw)
	}
	billing.err = errors.New("the billing catalogue answered 503")
	if err := pricer.Daily(ctx); err != nil {
		t.Fatal(err)
	}
	if kept := byModel(gemini)["gemini-test-3-pro"]; kept.Price == nil || kept.Price.Source != "google_billing" || kept.PendingPrice != nil ||
		!strings.Contains(kept.Unpriced, "could not be read") {
		t.Errorf("Google's price while its catalogue cannot be read: %+v", kept)
	}
}

// ownDownloads are the downloads of LiteLLM's catalogue a test's own source made: the table is the installation's, so
// other tests' fixtures are in it too.
func ownDownloads(r response) []any {
	var out []any
	all, _ := r.Body["items"].([]any)
	for _, it := range all {
		if strings.HasPrefix(it.(map[string]any)["url"].(string), "https://raw.githubusercontent.com/") {
			out = append(out, it)
		}
	}
	return out
}
