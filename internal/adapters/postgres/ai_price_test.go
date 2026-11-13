//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The database keeps the models' prices whoever writes (golden rule 2; AI-82, AI-85): the catalogue's downloads only
// through the worker's function, never changed; each price a version kept as recorded, with rates written with it as
// exact decimals above zero; one in force and one waiting per model, a newer one superseding what it replaces; a
// normalised match in force only by an Owner's decision; an Owner's price only as its approved request, from the API,
// never overwritten by a read and withdrawn only itself; the worker records no Owner's price and the API no other; the AI
// reads nothing, and another organisation sees none of it.
// req: AI-82
// req: AI-85
func TestAttackAIPricesKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Prices A Pte Ltd"), books(t, "Prices B Pte Ltd")
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	c := storage.AIConnection{Name: "OpenAI", Connector: "openai", Auth: "api-key", Configuration: json.RawMessage(`{}`), Host: "api.openai.com",
		Billing: "usage", CreatedBy: "user:x", CreatedAt: now}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		c, err = u.AIConnections().Create(ctx, c, uuid.Must(uuid.NewV7()))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: c.ID, Trigger: "daily", Outcome: "complete", Pages: 1, Added: 2,
			ReadBy: "system:ai.models.read", ReadAt: now}, []storage.AIModel{
			{ConnectionID: c.ID, Model: "gpt-6", FirstListed: now, LastListed: now, Listed: true},
			{ConnectionID: c.ID, Model: "gpt-5", FirstListed: now, LastListed: now, Listed: true}})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// A download, recorded only through the worker's function.
	digest := strings.Repeat("ab", 32)
	cat, err := worker.RecordAIPriceCatalogue(ctx, storage.AIPriceCatalogue{Source: "litellm", URL: "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json",
		FetchedAt: now, SHA256: digest, Size: 4072360, Licence: "MIT, outside enterprise/", LicenceURL: "https://raw.githubusercontent.com/BerriAI/litellm/main/LICENSE",
		LicenceSHA256: digest, Outcome: "valid", Entries: 2150, Document: "ai/price-catalogues/litellm/" + digest + ".json"})
	if err != nil || cat.ID == uuid.Nil || cat.Entries != 2150 {
		t.Fatalf("recording a download: %v %+v", err, cat)
	}
	if _, err := worker.RecordAIPriceCatalogue(ctx, storage.AIPriceCatalogue{Source: "litellm", URL: "https://example.com/x.json", FetchedAt: now,
		SHA256: digest, LicenceURL: "https://example.com/LICENSE", LicenceSHA256: digest, Outcome: "valid", Document: "elsewhere.json"}); err == nil {
		t.Error("a valid download without its licence, kept elsewhere")
	}

	record := func(model, status, review string, rates ...string) (storage.AIPrice, error) {
		p := storage.AIPrice{ConnectionID: c.ID, Model: model, Source: "litellm", CatalogueID: &cat.ID, CatalogueKey: model, Match: "exact",
			Currency: "USD", Status: status, Review: review, RecordedBy: "system:ai.prices.read", RecordedAt: now}
		if review != "" {
			p.ReviewDetail = "input fell"
		}
		for i := 0; i+1 < len(rates); i += 2 {
			p.Rates = append(p.Rates, storage.AIPriceRate{Kind: rates[i], Unit: "per_million_tokens", Amount: rates[i+1]})
		}
		var out storage.AIPrice
		err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AIPrices().Record(ctx, p)
			return err
		})
		return out, err
	}
	current := func(org uuid.UUID) []storage.AIPrice {
		var out []storage.AIPrice
		if err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AIPrices().Current(ctx, c.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	v1, err := record("gpt-6", "active", "", "input", "2", "output", "8")
	if err != nil || v1.Number != 1 {
		t.Fatalf("the first price: %v %+v", err, v1)
	}
	v2, err := record("gpt-6", "pending", "drop", "input", "0.5", "output", "8")
	if err != nil || v2.Number != 2 {
		t.Fatalf("a held drop: %v %+v", err, v2)
	}
	v3, err := record("gpt-6", "pending", "drop", "input", "0.4", "output", "8")
	if err != nil {
		t.Fatal(err)
	}
	if got := current(a.Org); len(got) != 2 || got[0].ID != v1.ID || got[1].ID != v3.ID || got[0].Rates[0].Amount != "2.0000000000" {
		t.Fatalf("in force and waiting: %+v", got)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		p, err := u.AIPrices().Decide(ctx, v3.ID, true, "user:owner", now)
		if err == nil && (p.Status != "active" || p.DecidedBy != "user:owner") {
			t.Errorf("the confirmed price: %+v", p)
		}
		return err
	}); err != nil {
		t.Fatalf("the Owner confirming: %v", err)
	}
	if got := current(a.Org); len(got) != 1 || got[0].ID != v3.ID {
		t.Errorf("after the review: %+v", got)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIPrices().Decide(ctx, v2.ID, true, "user:owner", now)
		return err
	}); !storageCode(err, storage.CodeAIPrice) {
		t.Errorf("confirming a superseded price: %v", err)
	}

	// An Owner's price, as its approval executes it, is never overwritten by a read.
	envelope := `{"object":{"type":"ai_connection"}}`
	sum := sha256.Sum256([]byte(envelope))
	if err := asOrg(ctx, conn(t, "fyniq"), a.Org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
			return err
		}
		var approval uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO fyniq.approval_request (object_type, object_key, operation, summary, change, envelope,
			envelope_hash, policy_version, requested_by, requested_at, requester_mfa_method, requester_mfa_at, expires_at, status, decided_at)
			VALUES ('ai_connection', $1, 'ai_connection.change', 'Price gpt-5', '{}', $2, $3, 'p', $4, $5, 'session_step_up', $5, $6, 'approved', $5)
			RETURNING id`, c.ID.String(), envelope, sum[:], uuid.Must(uuid.NewV7()), now, now.Add(time.Hour)).Scan(&approval); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO fyniq.ai_price (connection_id, model, number, source, match, currency, status, recorded_by, recorded_at, approval_id)
			VALUES ($1, 'gpt-5', 1, 'owner', 'entered', 'USD', 'active', 'user:owner', $2, $3)`, c.ID, now, approval)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := record("gpt-5", "active", "", "input", "1", "output", "4"); !storageCode(err, storage.CodeAIPrice) {
		t.Errorf("a read overwriting an Owner's price: %v", err)
	}
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.AIPrices().Checked(ctx, c.ID, "gpt-5", "", now)
	}); err != nil {
		t.Errorf("the worker noting a look: %v", err)
	}

	price := `INSERT INTO fyniq.ai_price (connection_id, model, number, source, catalogue_id, catalogue_key, match, currency, status, review, recorded_by, recorded_at)
	  VALUES ($1, 'gpt-6', 99, 'litellm', $2, 'gpt-6', $3, 'USD', $4, $5, 'system:ai.prices.read', now()) RETURNING id`
	rate := `INSERT INTO fyniq.ai_price_rate (price_id, kind, above, unit, amount) VALUES ($1, $2, $3, $4, $5)`
	for _, x := range []struct {
		what, role, match, status string
		review                    any
		kind                      string
		above                     int64
		unit, amount              string
		states                    []string
	}{
		{"a rate of zero", "fyniq_worker", "exact", "pending", "drop", "input", 0, "per_million_tokens", "0", []string{"23514"}},
		{"a NaN rate", "fyniq_worker", "exact", "pending", "drop", "input", 0, "per_million_tokens", "NaN", []string{"23514"}},
		{"a rate in another unit", "fyniq_worker", "exact", "pending", "drop", "input", 0, "per_image", "1", []string{"23514"}},
		{"an image rate with a tier", "fyniq_worker", "exact", "pending", "drop", "input_image", 200000, "per_image", "1", []string{"23514"}},
		{"a rate finer than ten places", "fyniq_worker", "exact", "pending", "drop", "input", 0, "per_million_tokens", "0.00000000001", []string{"23514"}},
		{"a normalised match in force undecided", "fyniq_worker", "normalised", "active", "normalised", "input", 0, "per_million_tokens", "1", []string{"23514"}},
		{"a normalised match not held", "fyniq_worker", "normalised", "active", nil, "input", 0, "per_million_tokens", "1", []string{"23514"}},
		{"a price starting superseded", "fyniq_worker", "exact", "superseded", nil, "input", 0, "per_million_tokens", "1", []string{"FY333"}},
		{"the API recording a price", "fyniq_api", "exact", "active", nil, "input", 0, "per_million_tokens", "1", []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, price, c.ID, cat.ID, x.match, x.status, x.review).Scan(&id); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, rate, id, x.kind, x.above, x.unit, x.amount)
			return err
		})
		wantState(t, x.what, err, x.states...)
	}
	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the owner changing a rate", "fyniq_migrator", `UPDATE fyniq.ai_price_rate SET amount = 1`, []string{"FY333"}},
		{"the owner removing a rate", "fyniq_migrator", `DELETE FROM fyniq.ai_price_rate`, []string{"FY333"}},
		{"the worker adding a rate to a recorded price", "fyniq_worker", `INSERT INTO fyniq.ai_price_rate (price_id, kind, above, unit, amount)
		  SELECT id, 'reasoning', 0, 'per_million_tokens', 1 FROM fyniq.ai_price WHERE model = 'gpt-6' AND number = 1`, []string{"FY333"}},
		{"the owner rewriting a price", "fyniq_migrator", `UPDATE fyniq.ai_price SET currency = 'SGD'`, []string{"FY333"}},
		{"the owner putting a superseded price back", "fyniq_migrator", `UPDATE fyniq.ai_price SET status = 'active' WHERE number = 1 AND model = 'gpt-6'`, []string{"FY333"}},
		{"the owner removing a price", "fyniq_migrator", `DELETE FROM fyniq.ai_price`, []string{"FY333"}},
		{"the owner emptying them", "fyniq_migrator", `TRUNCATE fyniq.ai_price_rate, fyniq.ai_price`, []string{"FY060", "0A000"}},
		{"the worker confirming a price", "fyniq_worker", `UPDATE fyniq.ai_price SET status = 'active', decided_by = 'system:x', decided_at = now()`, []string{"42501"}},
		{"the API noting a look", "fyniq_api", `UPDATE fyniq.ai_model SET price_checked_at = now(), unpriced = 'x'`, []string{"42501"}},
		{"a listed price that is not an object", "fyniq_worker", `UPDATE fyniq.ai_model SET listed_price = '["0.000001"]'`, []string{"23514"}},
		{"the API writing a download", "fyniq_api", `SELECT fyniq.record_ai_price_catalogue('litellm', 'https://x.example/a.json', now(), repeat('a', 64), 1, 'MIT',
		  'https://x.example/LICENSE', repeat('a', 64), 'valid', NULL, 1, 'ai/price-catalogues/litellm/' || repeat('a', 64) || '.json')`, []string{"42501"}},
		{"the worker writing the downloads' table", "fyniq_worker", `UPDATE fyniq.ai_price_catalogue SET entries = 0`, []string{"42501"}},
		{"the worker removing a download", "fyniq_worker", `DELETE FROM fyniq.ai_price_catalogue`, []string{"42501"}},
		{"the AI reading the prices", "fyniq_ai", `SELECT count(*) FROM fyniq.ai_price`, []string{"42501"}},
		{"the worker recording an Owner's price", "fyniq_worker", `INSERT INTO fyniq.ai_price (connection_id, model, number, source, match, currency,
		  status, recorded_by, recorded_at, approval_id) SELECT id, 'gpt-6', 50, 'owner', 'entered', 'USD', 'active', 'user:x', now(), gen_random_uuid()
		  FROM fyniq.ai_connection`, []string{"42501"}},
		{"the API recording an Owner's price without its approved request", "fyniq_api", `INSERT INTO fyniq.ai_price (connection_id, model, number,
		  source, match, currency, status, recorded_by, recorded_at, approval_id) SELECT id, 'gpt-6', 50, 'owner', 'entered', 'USD', 'active', 'user:x',
		  now(), gen_random_uuid() FROM fyniq.ai_connection`, []string{"FY333"}},
		{"the API recording a source's price", "fyniq_api", `INSERT INTO fyniq.ai_price (connection_id, model, number, source, match, currency,
		  status, recorded_by, recorded_at) SELECT id, 'gpt-6', 50, 'provider_list', 'exact', 'USD', 'active', 'user:x', now() FROM fyniq.ai_connection`,
			[]string{"FY333"}},
		{"the API withdrawing a source's price", "fyniq_api", `UPDATE fyniq.ai_price SET status = 'withdrawn', decided_by = 'user:x', decided_at = now()
		  WHERE status = 'active' AND source = 'litellm'`, []string{"FY333"}},
		{"the AI reading the downloads", "fyniq_ai", `SELECT count(*) FROM fyniq.ai_price_catalogues`, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}

	if got := current(b.Org); len(got) != 0 {
		t.Errorf("another organisation sees %d prices", len(got))
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.AIPrices().Get(ctx, v1.ID); err == nil {
			t.Error("another organisation reads a price")
		}
		latest, err := u.AIPrices().LatestCatalogue(ctx, "litellm")
		if err == nil && latest.SHA256 != digest {
			t.Errorf("the downloads are the installation's: %+v", latest)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
