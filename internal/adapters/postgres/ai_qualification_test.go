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

// The database keeps each qualification as it ran (AI-78, AI-87; QA-ENG-05): a run is never changed or removed, and
// only its going out of date is added, once, with why; its result agrees with its counts and threshold, exactly; it
// names a model its connection lists and an agent of the release; the API and the worker record runs, the AI none
// (CMP-AI-01); the latest run of each model for each agent is what counts, and another organisation sees none of it.
// req: AI-78
// req: AI-87
// req: QA-ENG-05
// asvs: V2.3.2, V8.2.2
func TestAttackQualificationsAreKeptAsTheyRan(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Qualified A Pte Ltd"), books(t, "Qualified B Pte Ltd")
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	var c storage.AIConnection
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if c, err = u.AIConnections().Create(ctx, storage.AIConnection{Name: "OpenAI", Connector: "openai", Auth: "api-key", Configuration: json.RawMessage(`{}`),
			Host: "api.openai.com", Billing: "usage", CreatedBy: "user:x", CreatedAt: now}, uuid.Must(uuid.NewV7())); err != nil {
			return err
		}
		_, err = u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: c.ID, Trigger: "refresh", Outcome: "complete", Pages: 1, Added: 1,
			ReadBy: "user:x", ReadAt: now}, []storage.AIModel{{ConnectionID: c.ID, Model: "gpt-6", FirstListed: now, LastListed: now, Listed: true}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	run := func(s storage.Store, org uuid.UUID, q storage.AIQualification) (storage.AIQualification, error) {
		var out storage.AIQualification
		err := s.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AIQualifications().Record(ctx, q)
			return err
		})
		return out, err
	}
	q := func(passed int, result string, at time.Time) storage.AIQualification {
		return storage.AIQualification{Agent: "chat", ConnectionID: c.ID, Model: "gpt-6", Version: "gpt-6-20261001", Suite: "chat", SuiteVersion: 1,
			Threshold: "0.75", Cases: 4, Passed: passed, Result: result, Run: uuid.Must(uuid.NewV7()), RanBy: "user:x", RanAt: at}
	}

	// The result agrees with the counts exactly: 3 of 4 reaches 0.75, 2 does not.
	first, err := run(api, a.Org, q(3, "pass", now.Add(-time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run(api, a.Org, q(2, "pass", now)); !storageCode(err, storage.CodeInvalid) {
		t.Errorf("a pass short of the threshold: %v", err)
	}
	if _, err := run(api, a.Org, q(3, "fail", now)); !storageCode(err, storage.CodeInvalid) {
		t.Errorf("a fail at the threshold: %v", err)
	}
	other := q(4, "pass", now)
	other.Model = "gpt-9"
	if _, err := run(api, a.Org, other); err == nil {
		t.Error("a run of a model the connection does not list")
	}
	other = q(4, "pass", now)
	other.Agent = "dreamer"
	if _, err := run(api, a.Org, other); err == nil {
		t.Error("a run for an agent not of the release")
	}
	if _, err := run(api, b.Org, q(4, "pass", now)); err == nil {
		t.Error("a run on another organisation's connection")
	}

	// The worker's run is the latest; going out of date is added once.
	latest, err := run(worker, a.Org, q(2, "fail", now))
	if err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.AIQualifications().Latest(ctx, c.ID)
		if err == nil && (len(all) != 1 || all[0].ID != latest.ID || all[0].Threshold != "0.75") {
			t.Errorf("the latest: %+v", all)
		}
		if err == nil {
			err = u.AIQualifications().OutOfDate(ctx, first.ID, "gpt-6 answered as gpt-6-20261115", now)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the owner changing a run", "fyniq_migrator", `UPDATE fyniq.ai_qualification SET passed = 4, result = 'pass'`, []string{"FY339"}},
		{"the owner removing a run", "fyniq_migrator", `DELETE FROM fyniq.ai_qualification`, []string{"FY339"}},
		{"the owner emptying them", "fyniq_migrator", `TRUNCATE fyniq.ai_qualification`, []string{"FY060", "0A000"}},
		{"the API putting a run out of date twice", "fyniq_api", `UPDATE fyniq.ai_qualification SET out_of_date_why = 'again'
		  WHERE out_of_date_at IS NOT NULL`, []string{"FY339"}},
		{"the API bringing one back", "fyniq_api", `UPDATE fyniq.ai_qualification SET out_of_date_at = NULL, out_of_date_why = NULL
		  WHERE out_of_date_at IS NOT NULL`, []string{"FY339"}},
		{"the API changing a result", "fyniq_api", `UPDATE fyniq.ai_qualification SET result = 'pass'`, []string{"42501"}},
		{"the API removing a run", "fyniq_api", `DELETE FROM fyniq.ai_qualification`, []string{"42501"}},
		{"a threshold of NaN", "fyniq_api", `INSERT INTO fyniq.ai_qualification (agent, connection_id, model, suite, suite_version, threshold, cases,
		  passed, result, run_id, ran_by, ran_at) VALUES ('chat', '` + c.ID.String() + `', 'gpt-6', 'chat', 1, 'NaN', 1, 1, 'pass', gen_random_uuid(),
		  'user:x', now())`, []string{"23514"}},
		{"the AI recording a run", "fyniq_ai", `INSERT INTO fyniq.ai_qualification (agent, connection_id, model, suite, suite_version, threshold, cases,
		  passed, result, run_id, ran_by, ran_at) VALUES ('chat', '` + c.ID.String() + `', 'gpt-6', 'chat', 1, 1, 1, 1, 'pass', gen_random_uuid(),
		  'ai:x', now())`, []string{"42501"}},
		{"the AI reading them", "fyniq_ai", `SELECT count(*) FROM fyniq.ai_qualification`, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.AIQualifications().Latest(ctx, c.ID)
		if err == nil && len(all) != 0 {
			t.Errorf("another organisation sees %d runs", len(all))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
