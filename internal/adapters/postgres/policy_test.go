//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The policy library keeps every draft revision, published version and acknowledgement (migration 000297, FY310): each
// is audited; a revision or version out of order, a text whose hash is not its own, an acknowledgement of a version not
// published or given twice, and any change or deletion are refused; the worker writes none of it; another organisation
// sees none of it.
// req: CMP-07
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackThePolicyLibraryKeepsItsHistory(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Policies A Pte Ltd"), books(t, "Policies B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Compliance().SavePolicyDraft(ctx, storage.PolicyDraft{Key: "backup", Revision: 1, Body: "# Backup\n", SavedBy: "user:dpo", SavedAt: now}); err != nil {
			return err
		}
		if _, err := u.Compliance().PublishPolicy(ctx, storage.PublishedPolicy{Key: "backup", Version: 1, Title: "Backup and recovery policy", Body: "# Backup\n",
			From: "revision 1", PublishedBy: "user:dpo", PublishedAt: now}); err != nil {
			return err
		}
		return u.Compliance().AcknowledgePolicy(ctx, storage.PolicyAcknowledgement{Key: "backup", Version: 1, By: "user:member", At: now})
	}); err != nil {
		t.Fatal(err)
	}
	draft := `INSERT INTO fyniq.policy_draft (key, revision, body, sha256, saved_by, saved_at) VALUES ('backup', $1, $2, sha256(convert_to($2, 'UTF8')), 'user:dpo', now())`
	version := `INSERT INTO fyniq.policy_version (key, version, title, body, sha256, from_draft, published_by, published_at)
	            VALUES ('backup', $1, 'Backup', 'x', $2, 'revision 1', 'user:dpo', now())`
	ack := `INSERT INTO fyniq.policy_acknowledgement (key, version, acknowledged_by, acknowledged_at) VALUES ('backup', $1, $2, now())`
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker saving a draft", "fyniq_worker", draft, []any{2, "# Backup"}, []string{"42501"}},
		{"a draft skipping a revision", "fyniq_api", draft, []any{3, "# Backup"}, []string{"FY310"}},
		{"a draft of a policy the library does not hold", "fyniq_api", `INSERT INTO fyniq.policy_draft (key, revision, body, sha256, saved_by, saved_at)
			VALUES ('payroll', 1, 'x', sha256(convert_to('x', 'UTF8')), 'user:dpo', now())`, nil, []string{"23514"}},
		{"a version whose hash is not its text's", "fyniq_api", version, []any{2, make([]byte, 32)}, []string{"23514"}},
		{"a version skipping one", "fyniq_api", `INSERT INTO fyniq.policy_version (key, version, title, body, sha256, from_draft, published_by, published_at)
			VALUES ('backup', 3, 'Backup', 'x', sha256(convert_to('x', 'UTF8')), 'revision 1', 'user:dpo', now())`, nil, []string{"FY310"}},
		{"an acknowledgement of a version not published", "fyniq_api", ack, []any{2, "user:member"}, []string{"23503"}},
		{"an acknowledgement given twice", "fyniq_api", ack, []any{1, "user:member"}, []string{"23505"}},
		{"the API changing a version", "fyniq_api", `UPDATE fyniq.policy_version SET body = 'y'`, nil, []string{"42501"}},
		{"the owner changing a version", "fyniq_migrator", `UPDATE fyniq.policy_version SET title = 'y'`, nil, []string{"FY310"}},
		{"the owner deleting an acknowledgement", "fyniq_migrator", `DELETE FROM fyniq.policy_acknowledgement`, nil, []string{"FY310"}},
		{"the owner deleting a draft", "fyniq_migrator", `DELETE FROM fyniq.policy_draft`, nil, []string{"FY310"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	for org, want := range map[uuid.UUID]int{a.Org: 1, b.Org: 0} {
		if err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			lib, err := u.Compliance().Policies(ctx)
			if err == nil && (len(lib.Drafts) != want || len(lib.Versions) != want || len(lib.Acknowledgements) != want) {
				t.Errorf("organisation %s sees %+v, want %d of each", org, lib, want)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	var actions []string
	if err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action FROM fyniq.audit_event WHERE action LIKE 'policy.%' ORDER BY seq`)
		if err != nil {
			return err
		}
		actions, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil || len(actions) != 3 || actions[0] != "policy.draft_saved" || actions[1] != "policy.published" || actions[2] != "policy.acknowledged" {
		t.Errorf("the library's audit events: %v %v", actions, err)
	}
}
