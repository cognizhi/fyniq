//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// integrationTasks runs fn in a unit of work on b's organisation as role.
func (b runBooks) integrationTasks(t *testing.T, role string, fn func(context.Context, storage.IntegrationTasks) error) error {
	t.Helper()
	return store(t, role).Do(signedIn(), b.org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(ctx, u.IntegrationTasks()) })
}

// A task an integration made is the client's own and keeps what it is (FR-INT-24; design D5): a retried create finds the task, another client's task is not found and not
// changed, a finished task stays finished, and neither the database's API role nor the worker's can delete one, change who made it or reach a compliance task through it.
// req: FR-INT-24
// asvs: V4.1.1, V8.2.2
func TestATaskAnIntegrationMadeIsTheClientsOwn(t *testing.T) {
	b := newRunBooks(t, "Integration tasks Pte Ltd")
	var made storage.IntegrationTask
	if err := b.integrationTasks(t, "fyniq_api", func(ctx context.Context, r storage.IntegrationTasks) (err error) {
		var created bool
		made, created, err = r.Make(ctx, storage.NewIntegrationTask{ClientID: "jira", ExternalRef: "OPS-1", Title: "Chase the letter", CreatedBy: b.owner, At: b.now})
		if !created {
			t.Errorf("the first create was not new")
		}
		again, created, err2 := r.Make(ctx, storage.NewIntegrationTask{ClientID: "jira", ExternalRef: "OPS-1", Title: "Another title", CreatedBy: b.owner, At: b.now})
		if err2 != nil || created || again.ID != made.ID || again.Title != "Chase the letter" {
			t.Errorf("a retried create: %+v new=%v %v", again, created, err2)
		}
		other, created, err2 := r.Make(ctx, storage.NewIntegrationTask{ClientID: "linear", ExternalRef: "OPS-1", Title: "Same reference, another client", CreatedBy: b.owner, At: b.now})
		if err2 != nil || !created || other.ID == made.ID {
			t.Errorf("the same reference from another client: %+v new=%v %v", other, created, err2)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// another client's update finds nothing, and changes nothing
	err := b.integrationTasks(t, "fyniq_api", func(ctx context.Context, r storage.IntegrationTasks) error {
		_, err := r.Update(ctx, "linear", made.ID, made.Version, storage.IntegrationTaskChange{Title: "Hijacked", Status: storage.IntegrationTaskDone, At: b.now})
		return err
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("another client's update: %v", err)
	}
	// the owner finishes it once; then it stays finished
	var done storage.IntegrationTask
	if err := b.integrationTasks(t, "fyniq_api", func(ctx context.Context, r storage.IntegrationTasks) (err error) {
		done, err = r.Update(ctx, "jira", made.ID, made.Version, storage.IntegrationTaskChange{Title: made.Title, Status: storage.IntegrationTaskDone, At: b.now})
		return err
	}); err != nil || done.Status != storage.IntegrationTaskDone || done.ClosedAt == nil || done.Version != 2 {
		t.Fatalf("the task finished: %+v %v", done, err)
	}
	if err := b.integrationTasks(t, "fyniq_api", func(ctx context.Context, r storage.IntegrationTasks) error {
		_, err := r.Update(ctx, "jira", made.ID, done.Version, storage.IntegrationTaskChange{Title: "Back again", Status: storage.IntegrationTaskOpen, At: b.now})
		return err
	}); err == nil {
		t.Errorf("a finished task was changed")
	}
	// the database's own fences, raw: no delete, no change of who made it, the worker changes nothing
	ctx := context.Background()
	for _, c := range []struct{ name, role, sql string }{
		{"delete", "fyniq_api", `DELETE FROM fyniq.integration_task`},
		{"another client", "fyniq_api", `UPDATE fyniq.integration_task SET client_id = 'linear'`},
		{"a new creator", "fyniq_api", `UPDATE fyniq.integration_task SET created_by = gen_random_uuid()`},
		{"the worker", "fyniq_worker", `UPDATE fyniq.integration_task SET title = 'x'`},
		{"a compliance task through it", "fyniq_api", `UPDATE fyniq.compliance_task SET title = 'x'`},
		{"a compliance task made by the API", "fyniq_api", `INSERT INTO fyniq.compliance_task (finding_key, title, owner_role, source, proposal_id, created_at) VALUES ('calendar:x:1', 'x', 'owner', 'ai_proposal', gen_random_uuid(), now())`},
	} {
		err := asOrg(ctx, conn(t, c.role), b.org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.sql); return err })
		if err == nil {
			t.Errorf("%s was allowed", c.name)
		}
	}
}
