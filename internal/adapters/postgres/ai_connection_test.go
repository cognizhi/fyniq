//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The database keeps an AI connection's rules whoever writes (golden rule 2; AI-72, AI-73, AI-79): it starts off at
// its first version; each change is the next version, kept beside it as written and never changed; it is switched
// off, never removed; a person's own plan cannot be stored, no authentication only for a self-hosted server, an
// API-key connection is switched on only with its credential, and a subscription always has its quota; the worker and
// the AI write nothing; another organisation sees none of it.
// req: AI-72
// req: AI-73
// req: AI-79
func TestAttackTheAIConnectionKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Connections A Pte Ltd"), books(t, "Connections B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	approval := uuid.Must(uuid.NewV7())
	key := "projects/fyniq/secrets/org-a-ai-1-api_key/versions/1"
	c := storage.AIConnection{Name: "Claude API", Connector: "anthropic", Auth: "api-key", Configuration: json.RawMessage(`{"endpoint":"anthropic-api"}`),
		Host: "api.anthropic.com", CredentialRef: key, CredentialSetAt: &now, Billing: "usage", CreatedBy: "user:x", CreatedAt: now}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		c, err = u.AIConnections().Create(ctx, c, approval)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if c.State != "disabled" || c.Version != 1 || c.ChangedBy != "user:x" {
		t.Fatalf("a new connection: %+v", c)
	}
	step := func(next storage.AIConnection, s string, by *uuid.UUID) (storage.AIConnection, error) {
		var out storage.AIConnection
		err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AIConnections().Step(ctx, next, s, by)
			return err
		})
		return out, err
	}
	on := c
	on.State, on.ChangedBy, on.ChangedAt = "enabled", "user:y", now
	on, err := step(on, storage.AIConnectionEnabled, &approval)
	if err != nil || on.Version != 2 || on.State != "enabled" {
		t.Fatalf("switching it on: %v %+v", err, on)
	}
	if _, err := step(c, storage.AIConnectionDisabled, nil); !storageCode(err, storage.CodeVersion) {
		t.Errorf("a step from an old version: %v", err)
	}
	off := on
	off.State = "disabled"
	if off, err = step(off, storage.AIConnectionDisabled, nil); err != nil || off.Version != 3 {
		t.Fatalf("switching it off: %v %+v", err, off)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIConnections().Create(ctx, storage.AIConnection{Name: "Claude API", Connector: "openai", Auth: "api-key", Configuration: json.RawMessage(`{}`),
			Host: "api.openai.com", Billing: "usage", CreatedBy: "user:x", CreatedAt: now}, approval)
		return err
	}); !storageCode(err, storage.CodeDuplicate) {
		t.Errorf("a name in use: %v", err)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		versions, err := u.AIConnections().Versions(ctx, c.ID)
		if err == nil && (len(versions) != 3 || versions[0].Step != "created" || versions[1].Step != "enabled" || versions[2].Approval != nil) {
			t.Errorf("the versions: %+v", versions)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	insert := `INSERT INTO fyniq.ai_connection (name, connector, auth, configuration, host, self_hosted, billing_mode, quota, created_by, created_at, changed_by, changed_at)
	  VALUES ($1, 'openai-compatible', $2, '{}', 'api.x.ai', $3, $4, $5, 'user:x', now(), 'user:x', now())`
	for _, x := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"a person's own plan", "fyniq_api", insert, []any{"Grok plan", "oauth-grant", false, "usage", nil}, []string{"23514"}},
		{"no authentication for a hosted provider", "fyniq_api", insert, []any{"Grok", "none", false, "usage", nil}, []string{"23514"}},
		{"a subscription without its quota", "fyniq_api", insert, []any{"Grok", "api-key", false, "subscription", nil}, []string{"23514"}},
		{"a quota on usage", "fyniq_api", insert, []any{"Grok", "api-key", false, "usage", `{"amount":1}`}, []string{"23514"}},
		{"a connection without its version's record", "fyniq_api", insert, []any{"Grok", "api-key", false, "usage", nil}, []string{"FY330"}},
		{"the API starting one switched on", "fyniq_api", `INSERT INTO fyniq.ai_connection (name, connector, auth, host, billing_mode, state, created_by, created_at, changed_by, changed_at)
		  VALUES ('Grok', 'openai-compatible', 'api-key', 'api.x.ai', 'usage', 'enabled', 'user:x', now(), 'user:x', now())`, nil, []string{"42501"}},
		{"the owner starting one switched on", "fyniq_migrator", `INSERT INTO fyniq.ai_connection (name, connector, auth, host, billing_mode, state, created_by, created_at, changed_by, changed_at)
		  VALUES ('Grok', 'openai-compatible', 'api-key', 'api.x.ai', 'usage', 'enabled', 'user:x', now(), 'user:x', now())`, nil, []string{"FY330", "23514"}},
		{"switching it on without the version's record", "fyniq_api", `UPDATE fyniq.ai_connection SET state = 'enabled', version = version + 1`, nil, []string{"FY330"}},
		{"an API-key connection on without its key", "fyniq_api", `UPDATE fyniq.ai_connection SET state = 'enabled', credential_ref = NULL, credential_set_at = NULL, version = version + 1`, nil, []string{"23514"}},
		{"the owner skipping a version", "fyniq_migrator", `UPDATE fyniq.ai_connection SET version = version + 2`, nil, []string{"FY330"}},
		{"the owner moving it to another provider", "fyniq_migrator", `UPDATE fyniq.ai_connection SET connector = 'openai', version = version + 1`, nil, []string{"FY330"}},
		{"the API removing it", "fyniq_api", `DELETE FROM fyniq.ai_connection`, nil, []string{"42501"}},
		{"the owner removing it", "fyniq_migrator", `DELETE FROM fyniq.ai_connection`, nil, []string{"FY330"}},
		{"the API rewriting a version", "fyniq_api", `UPDATE fyniq.ai_connection_version SET step = 'changed'`, nil, []string{"42501"}},
		{"the owner rewriting a version", "fyniq_migrator", `UPDATE fyniq.ai_connection_version SET snapshot = '{}'`, nil, []string{"FY330"}},
		{"the owner removing a version", "fyniq_migrator", `DELETE FROM fyniq.ai_connection_version`, nil, []string{"FY330"}},
		{"the owner emptying them", "fyniq_migrator", `TRUNCATE fyniq.ai_connection_version, fyniq.ai_connection`, nil, []string{"FY060", "FY061", "0A000"}},
		{"the worker writing one", "fyniq_worker", `UPDATE fyniq.ai_connection SET state = 'disabled'`, nil, []string{"42501"}},
		{"the AI reading them", "fyniq_ai", `SELECT count(*) FROM fyniq.ai_connection`, nil, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt, x.args...); return err })
		wantState(t, x.what, err, x.states...)
	}
	// The data-class policy: one approved version after another, kept as written; restricted data allowed nowhere.
	policy := func(version int, p string) error {
		return api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.AIDataPolicies().Save(ctx, storage.AIDataPolicy{Version: version, Policy: json.RawMessage(p), Approval: approval, MadeBy: "user:x", MadeAt: now})
			return err
		})
	}
	if err := policy(1, `{"allow":{"business-figures":["`+c.ID.String()+`"]}}`); err != nil {
		t.Fatal(err)
	}
	if err := policy(1, `{}`); !storageCode(err, storage.CodeVersion) {
		t.Errorf("a policy version twice: %v", err)
	}
	if err := policy(2, `{"allow":{"restricted":["`+c.ID.String()+`"]}}`); !storageCode(err, storage.CodeAIConnection) {
		t.Errorf("restricted data allowed: %v", err)
	}
	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the owner skipping a policy version", "fyniq_migrator", `INSERT INTO fyniq.ai_data_policy (version, policy, approval_id, made_by, made_at)
		  VALUES (5, '{}', gen_random_uuid(), 'user:x', now())`, []string{"FY331"}},
		{"the owner rewriting the policy", "fyniq_migrator", `UPDATE fyniq.ai_data_policy SET policy = '{}'`, []string{"FY331"}},
		{"the API rewriting the policy", "fyniq_api", `UPDATE fyniq.ai_data_policy SET policy = '{}'`, []string{"42501"}},
		{"the owner removing the policy", "fyniq_migrator", `DELETE FROM fyniq.ai_data_policy`, []string{"FY331"}},
		{"the worker writing a policy", "fyniq_worker", `INSERT INTO fyniq.ai_data_policy (version, policy, approval_id, made_by, made_at)
		  VALUES (2, '{}', gen_random_uuid(), 'user:x', now())`, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}

	// The models a provider lists, and each read: kept, never removed; only a complete read removes a model (AI-84).
	worker := store(t, "fyniq_worker")
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: c.ID, Trigger: "daily", Outcome: "complete", Pages: 1, Added: 1,
			ReadBy: "system:ai.models.read", ReadAt: now}, []storage.AIModel{{ConnectionID: c.ID, Model: "gpt-6", FirstListed: now, LastListed: now, Listed: true}})
		return err
	}); err != nil {
		t.Fatalf("the worker recording a read: %v", err)
	}
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: c.ID, Trigger: "daily", Outcome: "partial", Pages: 1, Removed: 1,
			Failure: "the page cap", ReadBy: "system:ai.models.read", ReadAt: now}, nil)
		return err
	}); err == nil {
		t.Error("a partial read removed a model")
	}
	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the owner removing a model", "fyniq_migrator", `DELETE FROM fyniq.ai_model`, []string{"FY332"}},
		{"the owner moving a model to another ID", "fyniq_migrator", `UPDATE fyniq.ai_model SET model = 'gpt-7'`, []string{"FY332"}},
		{"the owner rewriting a read", "fyniq_migrator", `UPDATE fyniq.ai_model_list_read SET outcome = 'failed', failure = 'x'`, []string{"FY332"}},
		{"the API removing a read", "fyniq_api", `DELETE FROM fyniq.ai_model_list_read`, []string{"42501"}},
		{"a complete read with why it failed", "fyniq_api", `INSERT INTO fyniq.ai_model_list_read (connection_id, trigger, outcome, pages, added, removed, failure, read_by, read_at)
		  SELECT id, 'refresh', 'complete', 1, 0, 0, 'x', 'user:x', now() FROM fyniq.ai_connection`, []string{"23514"}},
		{"the AI reading the models", "fyniq_ai", `SELECT count(*) FROM fyniq.ai_model`, []string{"42501"}},
		{"a model allowed unprobed", "fyniq_api", `UPDATE fyniq.ai_model SET allowed = true`, []string{"23514"}},
		{"the worker passing a probe", "fyniq_worker", `UPDATE fyniq.ai_model SET probe_ok = true, probed_at = now()`, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}

	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if p, err := u.AIDataPolicies().Latest(ctx); err != nil || p.Version != 0 {
			t.Errorf("another organisation's policy: %v %+v", err, p)
		}
		all, err := u.AIConnections().List(ctx)
		if err == nil && len(all) != 0 {
			t.Errorf("another organisation sees %d connections", len(all))
		}
		if _, err := u.AIConnections().Get(ctx, c.ID); err == nil {
			t.Error("another organisation reads the connection")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
