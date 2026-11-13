//go:build local

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	// The key's approval queued a read; with no download yet, the models it lists wait for the daily read.
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
	if m := byModel(); len(m) != 3 || m["gpt-test-6"].Priced || m["gpt-test-6"].Serves || m["gpt-test-6"].PriceChecked != nil {
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
	items, _ := cats.Body["items"].([]any)
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
	items, _ = cats.Body["items"].([]any)
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
