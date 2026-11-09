//go:build local

package postgres_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/domain/compliance"
)

// schema.verify reads the database's guards as the worker, which runs it, and finds them equal to the snapshots this
// build carries. A grant added by hand, INSERT granted to the AI's role, a dropped require_audit trigger and row-level
// security switched off are each found and named; each is planted in a transaction that rolls back (GRANT, DROP
// TRIGGER and ALTER TABLE are transactional), so the shared database never changes (p1-compliance-center D4).
// req: CTL-FIN-02
// req: CTL-AUD-01
// req: CTL-AI-01
// req: CMP-AI-01
// asvs: V8.2.2
func TestSchemaVerifyFindsWhatWasChangedByHand(t *testing.T) {
	ctx := context.Background()
	release, err := postgres.ReleaseGuards()
	if err != nil {
		t.Fatal(err)
	}
	if diffs, err := compliance.VerifySchema(ctx, store(t, "fyniq_worker"), release); err != nil || len(diffs) > 0 {
		t.Fatalf("the migrated database, read as the worker: %v %q", err, diffs)
	}
	owner := conn(t, "fyniq_migrator")
	for _, attack := range []struct {
		sql, want string
	}{
		{"GRANT UPDATE ON fyniq.journal_line TO fyniq_worker", "fyniq_worker holds UPDATE on journal_line, which the release does not grant"},
		{"GRANT INSERT ON fyniq.journal_entry TO fyniq_ai", "fyniq_ai holds INSERT on journal_entry, which the release does not grant"},
		{"DROP TRIGGER require_audit ON fyniq.account", "the trigger require_audit on account is missing"},
		{"ALTER TABLE fyniq.account NO FORCE ROW LEVEL SECURITY", "row-level security on account now enabled and not forced"},
	} {
		tx, err := owner.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, attack.sql); err != nil {
			t.Fatalf("%s: %v", attack.sql, err)
		}
		got, err := postgres.ReadGuards(ctx, tx)
		_ = tx.Rollback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if diffs := compliance.CompareGuards(release, got); !slices.Contains(diffs, attack.want) {
			t.Errorf("%s: want %q, got %q", attack.sql, attack.want, diffs)
		}
	}
	if diffs, err := compliance.VerifySchema(ctx, store(t, "fyniq_worker"), release); err != nil || len(diffs) > 0 {
		t.Fatalf("after the attacks rolled back: %v %q", err, diffs)
	}
}

// CMP-AI-01: the AI's database role holds no privilege that writes, on any table, view or sequence, and executes no
// function of fyniq's, directly or through PUBLIC or a role it belongs to; and it may create nothing. (The btree_gist
// operator support in fyniq_ext is PUBLIC's by default and writes nothing; fyniq_ai has no USAGE on that schema.)
// req: CMP-AI-01
// req: CTL-AI-01
// req: AI-P2
// asvs: V8.2.2
func TestTheAIRoleHoldsNoWritePrivilege(t *testing.T) {
	c := conn(t, "fyniq")
	for _, r := range rows(t, c, `
SELECT n.nspname || '.' || c.relname, c.relkind::text
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname IN ('fyniq', 'fyniq_ext', 'fyniq_migrations') AND c.relkind IN ('r', 'p', 'v', 'm', 'S')
   AND CASE WHEN c.relkind = 'S' THEN has_sequence_privilege('fyniq_ai', c.oid, 'USAGE, UPDATE')
            ELSE has_table_privilege('fyniq_ai', c.oid, 'INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER') END`) {
		t.Errorf("fyniq_ai may write %s (kind %s): the AI holds no write privilege (CMP-AI-01)", r[0], r[1])
	}
	for _, r := range rows(t, c, `
SELECT p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
 WHERE n.nspname = 'fyniq' AND has_function_privilege('fyniq_ai', p.oid, 'EXECUTE')`) {
		t.Errorf("fyniq_ai may execute %s: the AI writes nothing, through a function either (CMP-AI-01)", r[0])
	}
	for _, r := range rows(t, c, `
SELECT nspname FROM pg_namespace WHERE has_schema_privilege('fyniq_ai', oid, 'CREATE')`) {
		t.Errorf("fyniq_ai may create objects in schema %s (CMP-AI-01)", r[0])
	}
	if got := rows(t, c, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'fyniq'`); len(got) != 1 || strings.TrimSpace(got[0][0]) == "0" {
		t.Fatal("no tables in schema fyniq: the migrations did not run?")
	}
}
