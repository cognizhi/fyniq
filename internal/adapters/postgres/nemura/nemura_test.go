//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura/nemuratest"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
)

var update = flag.Bool("update", false, "rewrite the golden files from the database")

var db *nemuratest.DB

func TestMain(m *testing.M) {
	flag.Parse()
	var err error
	if db, err = nemuratest.Start(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	db.Stop()
	os.Exit(code)
}

func connect(t *testing.T, url string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.WithoutCancel(context.Background())) })
	return conn
}

func store(t *testing.T, role string) *nemura.Store {
	t.Helper()
	s, err := nemura.Open(context.Background(), db.URL(role))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// lines runs a query of one text column and returns the rows.
func lines(t *testing.T, conn *pgx.Conn, sql string, args ...any) []string {
	t.Helper()
	rows, err := conn.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// golden compares got with testdata/<name>, or rewrites it with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := "testdata/" + name
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to write it)", err)
	}
	if !bytes.Equal(want, []byte(got)) {
		t.Errorf("%s differs from the database: review the change, then rerun with -update\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}

// The migrations apply to an empty database, again harmlessly, into a goose table of Nemura's own, and Fyniq's table is
// never created in Nemura's database.
func TestMigrationsApplyOnceAndAreRecordedInNemurasOwnTable(t *testing.T) {
	var log bytes.Buffer
	if err := nemura.Migrate(context.Background(), db.URL(nemura.Migrator), &log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "up to date") {
		t.Errorf("a second run should find nothing to apply: %q", log.String())
	}
	log.Reset()
	if err := nemura.MigrationStatus(context.Background(), db.URL(nemura.Migrator), &log); err != nil || !strings.Contains(log.String(), "000001_roles_schema_tenancy.sql") ||
		strings.Contains(log.String(), "pending") {
		t.Errorf("status: %v\n%s", err, log.String())
	}
	conn := connect(t, db.SuperURL())
	schemas := lines(t, conn, "SELECT nspname FROM pg_namespace WHERE nspname LIKE 'nemura%' OR nspname LIKE 'fyniq%' ORDER BY 1")
	if got := strings.Join(schemas, ","); got != "nemura,nemura_migrations" {
		t.Errorf("schemas in Nemura's database: %s, want nemura,nemura_migrations only", got)
	}
}

// The schema after every migration: tables with their row-level-security flags, columns, constraints, indexes, triggers,
// policies and functions. A change is reviewed in the golden file.
func TestSchemaSnapshot(t *testing.T) {
	conn := connect(t, db.SuperURL())
	var b strings.Builder
	section := func(title string, rows []string) {
		fmt.Fprintf(&b, "## %s\n", title)
		for _, r := range rows {
			fmt.Fprintln(&b, r)
		}
		b.WriteString("\n")
	}
	section("tables (table | owner | rls | forced)", lines(t, conn, `
SELECT c.relname || ' | ' || pg_get_userbyid(c.relowner) || ' | ' || c.relrowsecurity || ' | ' || c.relforcerowsecurity
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = 'nemura' AND c.relkind IN ('r', 'v') ORDER BY 1`))
	section("columns (table | column | type | not null | default)", lines(t, conn, `
SELECT c.relname || ' | ' || a.attname || ' | ' || format_type(a.atttypid, a.atttypmod) || ' | ' || a.attnotnull || ' | ' || coalesce(pg_get_expr(d.adbin, d.adrelid), '')
  FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
  LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
 WHERE n.nspname = 'nemura' AND c.relkind = 'r' AND a.attnum > 0 AND NOT a.attisdropped ORDER BY c.relname, a.attnum`))
	section("constraints (table | name | definition)", lines(t, conn, `
SELECT c.relname || ' | ' || k.conname || ' | ' || pg_get_constraintdef(k.oid)
  FROM pg_constraint k JOIN pg_class c ON c.oid = k.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = 'nemura' ORDER BY c.relname, k.conname`))
	section("indexes", lines(t, conn, `SELECT indexdef FROM pg_indexes WHERE schemaname = 'nemura' ORDER BY indexname`))
	section("triggers", lines(t, conn, `
SELECT pg_get_triggerdef(t.oid) FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = 'nemura' AND NOT t.tgisinternal ORDER BY t.tgname`))
	section("policies (table | name | using | with check)", lines(t, conn, `
SELECT tablename || ' | ' || policyname || ' | ' || coalesce(qual, '') || ' | ' || coalesce(with_check, '')
  FROM pg_policies WHERE schemaname = 'nemura' ORDER BY tablename, policyname`))
	section("functions (definition)", lines(t, conn, `
SELECT pg_get_functiondef(p.oid) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = 'nemura' ORDER BY p.proname`))
	golden(t, "schema.golden", b.String())
}

// grants is every privilege any role holds on Nemura's database, schema, tables, columns and functions.
func grants(t *testing.T) []string {
	conn := connect(t, db.SuperURL())
	var out []string
	out = append(out, lines(t, conn, `
SELECT CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END || ' | database | ' || a.privilege_type
  FROM pg_database d, aclexplode(d.datacl) a WHERE d.datname = current_database() AND a.grantee <> d.datdba`)...)
	out = append(out, lines(t, conn, `
SELECT CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END || ' | schema ' || n.nspname || ' | ' || a.privilege_type
  FROM pg_namespace n, aclexplode(n.nspacl) a WHERE n.nspname LIKE 'nemura%' AND a.grantee <> n.nspowner`)...)
	out = append(out, lines(t, conn, `
SELECT CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END || ' | table nemura.' || c.relname || ' | ' || a.privilege_type
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace, aclexplode(c.relacl) a
 WHERE n.nspname = 'nemura' AND c.relkind IN ('r', 'v') AND a.grantee <> c.relowner`)...)
	out = append(out, lines(t, conn, `
SELECT CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END || ' | column nemura.' || c.relname || '.' || t.attname || ' | ' || a.privilege_type
  FROM pg_attribute t JOIN pg_class c ON c.oid = t.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace, aclexplode(t.attacl) a
 WHERE n.nspname = 'nemura' AND t.attacl IS NOT NULL AND a.grantee <> c.relowner`)...)
	out = append(out, lines(t, conn, `
SELECT CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END || ' | function nemura.' || p.proname || ' | ' || a.privilege_type
  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace, aclexplode(p.proacl) a
 WHERE n.nspname = 'nemura' AND a.grantee <> p.proowner`)...)
	return out
}

// Every privilege each Nemura role holds is the grants golden file's, and nothing more: a new grant is reviewed there.
// No role may truncate, reference, trigger or maintain; PUBLIC and the migrator's own are never granted to; the AI and
// the read-only roles never write (SEC-03, golden rule 5).
// asvs: V8.2.2, V15.2.3
func TestEachRoleHoldsExactlyItsGrants(t *testing.T) {
	got := grants(t)
	var b strings.Builder
	for _, g := range got {
		b.WriteString(g + "\n")
	}
	sortedGolden := sortLines(b.String())
	golden(t, "grants.golden", sortedGolden)
	allowed := map[string]bool{}
	for _, r := range nemura.ServiceRoles {
		allowed[r] = true
	}
	for _, g := range got {
		who, _, _ := strings.Cut(g, " | ")
		if !allowed[who] {
			t.Errorf("%q holds a privilege and is not a Nemura service role: %s", who, g)
		}
		for _, bad := range []string{"TRUNCATE", "REFERENCES", "TRIGGER", "MAINTAIN"} {
			if strings.HasSuffix(g, " | "+bad) {
				t.Errorf("a role holds %s: %s", bad, g)
			}
		}
		if strings.HasPrefix(g, "nemura_assistant | ") || strings.HasPrefix(g, "nemura_readonly | ") {
			if strings.HasSuffix(g, "| INSERT") || strings.HasSuffix(g, "| UPDATE") || strings.HasSuffix(g, "| DELETE") {
				t.Errorf("the assistant and the read-only role never write yet: %s", g)
			}
		}
	}
}

func sortLines(s string) string {
	l := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := 1; i < len(l); i++ {
		for j := i; j > 0 && l[j] < l[j-1]; j-- {
			l[j], l[j-1] = l[j-1], l[j]
		}
	}
	return strings.Join(l, "\n") + "\n"
}

// Nemura's database admits only Nemura's roles: a login that holds none of them, such as a Fyniq role, cannot open it.
// The other direction, that a Nemura role cannot open Fyniq's database, is Fyniq's database's setting and the cloud's IAM
// bindings per database (infra/, [live-verify]).
// asvs: V8.2.2
func TestOnlyNemurasRolesConnectToNemurasDatabase(t *testing.T) {
	ctx := context.Background()
	admin := connect(t, db.AdminURL())
	if _, err := admin.Exec(ctx, "CREATE ROLE "+nemuratest.ProbeRole+" LOGIN PASSWORD '"+nemuratest.ProbeLogin()+"'"); err != nil {
		t.Fatal(err)
	}
	if _, err := pgx.Connect(ctx, db.ProbeURL()); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("a login with no Nemura role connected to Nemura's database: %v", err)
	}
	for _, r := range nemura.ServiceRoles {
		c, err := pgx.Connect(ctx, db.URL(r))
		if err != nil {
			t.Errorf("%s cannot connect to its own database: %v", r, err)
			continue
		}
		_ = c.Close(ctx)
	}
}

// Tenancy: every transaction names its organisation, and row-level security matches only that organisation's rows, even
// for the table's grantee, and even when the work asks for another's (INV-06, golden rule 8).
// asvs: V8.2.2, V8.3.1
func TestRowLevelSecurityKeepsOrganisationsApart(t *testing.T) {
	ctx := context.Background()
	api := store(t, "nemura_api")
	a, b := uuid.New(), uuid.New()
	insert := func(org uuid.UUID, name string) error {
		return api.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "INSERT INTO nemura.organisation (name) VALUES ($1)", name); err != nil {
				return err
			}
			return nemura.Audit(ctx, tx, org, clock.System{}.Now(), audit.Actor{Type: "user", ID: "test"}, "nemura.org.created",
				audit.Object{Type: "organisation", ID: org}, map[string]audit.Change{"name": {nil, name}}, "")
		})
	}
	if err := insert(a, "Acme People Pte Ltd"); err != nil {
		t.Fatal(err)
	}
	if err := insert(b, "Borneo People Pte Ltd"); err != nil {
		t.Fatal(err)
	}
	names := func(org uuid.UUID) []string {
		var out []string
		if err := api.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, "SELECT name FROM nemura.organisation ORDER BY name")
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var n string
				if err := rows.Scan(&n); err != nil {
					return err
				}
				out = append(out, n)
			}
			return rows.Err()
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := names(a); len(got) != 1 || got[0] != "Acme People Pte Ltd" {
		t.Errorf("organisation A sees %v", got)
	}
	if got := names(b); len(got) != 1 || got[0] != "Borneo People Pte Ltd" {
		t.Errorf("organisation B sees %v", got)
	}
	if got := names(uuid.New()); len(got) != 0 {
		t.Errorf("an organisation that has no rows sees %v", got)
	}
	// Asking for another organisation's row in the same transaction changes nothing: the policy, not the query, decides.
	if err := api.InOrg(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM nemura.organisation WHERE organisation_id = $1", b).Scan(&n); err != nil || n != 0 {
			t.Errorf("A reads B's row: %d, %v", n, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// An insert that names another organisation fails the policy's check.
	err := api.InOrg(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO nemura.organisation (organisation_id, name) VALUES ($1, 'x')", b)
		return err
	})
	if err == nil {
		t.Error("A wrote a row in B")
	}
	// The directory lists both, to the worker alone, through a view.
	w := store(t, "nemura_worker")
	var n int
	if err := w.InOrg(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM nemura.tenants WHERE organisation_id IN ($1, $2)", a, b).Scan(&n)
	}); err != nil || n != 2 {
		t.Errorf("the worker's tenants view: %d, %v", n, err)
	}
	if err := api.InOrg(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT 1 FROM nemura.tenants")
		return err
	}); err == nil {
		t.Error("the API read the tenant directory")
	}
}

// The unit of work refuses a transaction with no organisation, and a connection used for another organisation afterwards
// carries nothing over: app.org_id is transaction-local.
// asvs: V8.2.2
func TestTheUnitOfWorkNeedsAnOrganisationAndLeavesNoneBehind(t *testing.T) {
	ctx := context.Background()
	api := store(t, "nemura_api")
	if err := api.InOrg(ctx, uuid.Nil, func(context.Context, pgx.Tx) error { return nil }); err != nemura.ErrNoOrganisation {
		t.Errorf("no organisation: %v", err)
	}
	if err := api.InOrg(ctx, uuid.New(), func(ctx context.Context, tx pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	// Outside the unit of work the setting is gone: the pool's connection matches no row.
	conn := connect(t, db.URL("nemura_api"))
	var org *string
	if err := conn.QueryRow(ctx, "SELECT nullif(current_setting('app.org_id', true), '')").Scan(&org); err != nil || org != nil {
		t.Errorf("app.org_id outside a transaction: %v, %v", org, err)
	}
	// require_org, which Nemura's triggers call and no role may, says why, with Nemura's code.
	if _, err := connect(t, db.SuperURL()).Exec(ctx, "SELECT nemura.require_org()"); err == nil || !strings.Contains(err.Error(), "NM006") {
		t.Errorf("require_org with none set: %v", err)
	}
}
