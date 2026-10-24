//go:build local

package postgres_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// These tests read the database catalogue, so a table, column, grant or function added by a later
// migration is checked without anyone remembering to add a test.

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

// globalTables are the installation-scoped tables (§11.4): no organisation_id, no grant to any
// application role. Add a table here only with its access model in db/README.md.
// app_user, user_identity, installation and super_admin are the installation's people and first-run
// setup (migration 000010), super_admin_grant the Super admins' one-time codes (000012), user_session the
// web app's first-party sessions (000013): reached only through SECURITY DEFINER functions.
// statutory_pack_version and statutory_pack_row are the published statutory packs (000033): written only by
// the migrator at a release, read through views (§11.4, ENG-30). webauthn_credential and webauthn_challenge are people's
// passkeys and their ceremonies' challenges (000045): reached only through SECURITY DEFINER functions. fx_feed_rate is
// the daily exchange-rate feed (000070): read through a view, written by the worker only through record_feed_rate.
// capture_name is the registry of email-in address names (000102): written by capture_address's trigger, read only
// through capture_route. installation_announcement is the Super admins' banners to everyone (000108): written and read
// only through SECURITY DEFINER functions.
var globalTables = []string{"currency", "tenant_directory", "job_slot", "heartbeat", "app_user", "user_identity", "installation", "super_admin",
	"super_admin_grant", "user_session", "statutory_pack_version", "statutory_pack_row", "webauthn_credential", "webauthn_challenge",
	"fx_feed_rate", "capture_name", "installation_announcement"}

// onePerOrganisation are tenant tables keyed by the organisation alone, so they need no separate id.
var onePerOrganisation = []string{"organisation", "ledger_revision", "audit_chain", "opening_balance"}

var serviceRoles = []string{"fyniq_api", "fyniq_worker", "fyniq_executor", "fyniq_ai", "fyniq_readonly"}

func rows(t *testing.T, c *pgx.Conn, sql string, args ...any) [][]string {
	t.Helper()
	r, err := c.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
	defer r.Close()
	var out [][]string
	for r.Next() {
		vals, err := r.Values()
		if err != nil {
			t.Fatal(err)
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			row[i] = fmt.Sprint(v)
		}
		out = append(out, row)
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
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
		t.Fatalf("%v (run `go test -tags local ./internal/adapters/postgres -run %s -update` and review the diff)", err, t.Name())
	}
	if string(want) != got {
		t.Fatalf("%s differs from the database. If the migration is meant to change it, run\n"+
			"  go test -tags local ./internal/adapters/postgres -run '%s' -update\nand review the diff in the pull request.\n--- got:\n%s",
			path, t.Name(), got)
	}
}

const tablesSQL = `
SELECT c.relname,
       c.relrowsecurity, c.relforcerowsecurity,
       EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'organisation_id' AND NOT a.attisdropped),
       (SELECT count(*) FROM pg_policy p WHERE p.polrelid = c.oid),
       (SELECT count(*) FROM pg_policy p WHERE p.polrelid = c.oid AND p.polcmd = '*' AND p.polpermissive
          AND pg_get_expr(p.polqual, p.polrelid) = '(organisation_id = fyniq.current_org())'
          AND pg_get_expr(p.polwithcheck, p.polrelid) = '(organisation_id = fyniq.current_org())')
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = 'fyniq' AND c.relkind IN ('r', 'p')
 ORDER BY 1`

// req: INV-06
// req: CTL-IAM-05
// asvs: V8.4.1
// req: data/data-model Tenancy and row-level security
// req: data/data-model Global tables
func TestEveryTenantTableForcesRowLevelSecurity(t *testing.T) {
	c := conn(t, "fyniq")
	// An empty search_path makes pg_get_expr qualify every name, so the policy must call
	// fyniq.current_org() and not a look-alike in another schema.
	if _, err := c.Exec(context.Background(), "SET search_path = ''"); err != nil {
		t.Fatal(err)
	}
	tables := rows(t, c, tablesSQL)
	if len(tables) < 8 {
		t.Fatalf("found %d tables in schema fyniq; the migrations did not run?", len(tables))
	}
	for _, r := range tables {
		name, rls, force, hasOrg, policies, tenantPolicies := r[0], r[1], r[2], r[3], r[4], r[5]
		if slices.Contains(globalTables, name) {
			if hasOrg == "true" && name != "tenant_directory" { // the directory lists organisations
				t.Errorf("%s is listed as global but has organisation_id", name)
			}
			continue
		}
		if hasOrg != "true" || rls != "true" || force != "true" || policies != "1" || tenantPolicies != "1" {
			t.Errorf("tenant table %s: organisation_id %s, RLS %s, FORCE %s, policies %s (tenant policies %s); "+
				"want organisation_id, ENABLE and FORCE ROW LEVEL SECURITY and exactly one policy on fyniq.current_org() (INV-06)",
				name, hasOrg, rls, force, policies, tenantPolicies)
		}
	}
}

// req: INV-06
// req: data/data-model Keys
// req: data/data-model Tenancy and row-level security
func TestTenantKeysIndexesAndForeignKeys(t *testing.T) {
	c := conn(t, "fyniq")
	tenant := map[string]bool{}
	for _, r := range rows(t, c, tablesSQL) {
		if !slices.Contains(globalTables, r[0]) {
			tenant[r[0]] = true
		}
	}
	// organisation_id leads every index of a tenant table.
	for _, r := range rows(t, c, `
SELECT c.relname, i.relname, coalesce(a.attname, '(expression)')
  FROM pg_index x
  JOIN pg_class c ON c.oid = x.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
  JOIN pg_class i ON i.oid = x.indexrelid
  LEFT JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = x.indkey[0]
 WHERE n.nspname = 'fyniq' ORDER BY 1, 2`) {
		if tenant[r[0]] && r[2] != "organisation_id" {
			t.Errorf("index %s on %s starts with %s; organisation_id must lead every index (§11.4)", r[1], r[0], r[2])
		}
	}
	// UNIQUE (organisation_id, id) on every tenant table with an id.
	withKey := map[string]bool{}
	for _, r := range rows(t, c, `
SELECT c.relname
  FROM pg_index x
  JOIN pg_class c ON c.oid = x.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
  JOIN pg_attribute a0 ON a0.attrelid = c.oid AND a0.attnum = x.indkey[0]
  JOIN pg_attribute a1 ON a1.attrelid = c.oid AND a1.attnum = x.indkey[1]
 WHERE n.nspname = 'fyniq' AND x.indisunique AND x.indnkeyatts = 2
   AND a0.attname = 'organisation_id' AND a1.attname = 'id'`) {
		withKey[r[0]] = true
	}
	for name := range tenant {
		if !withKey[name] && !slices.Contains(onePerOrganisation, name) {
			t.Errorf("tenant table %s has no UNIQUE (organisation_id, id) (§11.4)", name)
		}
	}
	// Every foreign key between tenant tables is composite, organisation first on both sides.
	for _, r := range rows(t, c, `
SELECT con.conname, src.relname, dst.relname,
       (SELECT attname FROM pg_attribute WHERE attrelid = con.conrelid AND attnum = con.conkey[1]),
       (SELECT attname FROM pg_attribute WHERE attrelid = con.confrelid AND attnum = con.confkey[1])
  FROM pg_constraint con
  JOIN pg_class src ON src.oid = con.conrelid JOIN pg_namespace n ON n.oid = src.relnamespace
  JOIN pg_class dst ON dst.oid = con.confrelid
 WHERE n.nspname = 'fyniq' AND con.contype = 'f' ORDER BY 1`) {
		if tenant[r[2]] && (r[3] != "organisation_id" || r[4] != "organisation_id") {
			t.Errorf("foreign key %s (%s → %s) is not keyed on organisation_id first: foreign-key checks bypass RLS (§11.4)", r[0], r[1], r[2])
		}
	}
}

// grantsSQL lists every privilege on the objects in schema fyniq, by grantee, table and column.
const grantsSQL = `
WITH objs AS (
  SELECT c.oid, c.relname, c.relkind, c.relacl FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
   WHERE n.nspname = 'fyniq' AND c.relkind IN ('r', 'p', 'v')
), tbl AS (
  SELECT coalesce(r.rolname, 'PUBLIC') AS grantee, o.relname AS object, '' AS col, acl.privilege_type AS priv
    FROM objs o, aclexplode(o.relacl) acl LEFT JOIN pg_roles r ON r.oid = acl.grantee
), col AS (
  SELECT coalesce(r.rolname, 'PUBLIC'), o.relname, a.attname, acl.privilege_type
    FROM objs o JOIN pg_attribute a ON a.attrelid = o.oid AND a.attnum > 0 AND NOT a.attisdropped,
         aclexplode(a.attacl) acl LEFT JOIN pg_roles r ON r.oid = acl.grantee
), fn AS (
  SELECT coalesce(r.rolname, 'PUBLIC'), p.proname || '()', '', acl.privilege_type
    FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace,
         aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) acl LEFT JOIN pg_roles r ON r.oid = acl.grantee
   WHERE n.nspname = 'fyniq'
), sch AS (
  SELECT coalesce(r.rolname, 'PUBLIC'), 'schema ' || n.nspname, '', acl.privilege_type
    FROM pg_namespace n, aclexplode(n.nspacl) acl LEFT JOIN pg_roles r ON r.oid = acl.grantee
   WHERE n.nspname IN ('fyniq', 'fyniq_ext', 'fyniq_migrations')
)
SELECT * FROM tbl UNION ALL SELECT * FROM col UNION ALL SELECT * FROM fn UNION ALL SELECT * FROM sch
ORDER BY 1, 2, 4, 3`

// req: SEC-03
// asvs: V13.2.2
// req: data/data-model Tenancy and row-level security
// req: data/data-model Global tables
// req: data/data-model Insert-only ledger records
func TestEachRoleHoldsExactlyItsGrants(t *testing.T) {
	c := conn(t, "fyniq")
	var b strings.Builder
	b.WriteString("# Every privilege in the fyniq schemas, by role. Generated by TestEachRoleHoldsExactlyItsGrants;\n" +
		"# a change here is a change to who may do what (SEC-03), so review it like code.\n")
	writes := map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true, "TRUNCATE": true}
	var lines []string
	columns := map[string][]string{} // "grantee priv object" → columns, for column grants
	for _, r := range rows(t, c, grantsSQL) {
		grantee, object, column, priv := r[0], r[1], r[2], r[3]
		if grantee == "fyniq_migrator" {
			continue // the owner
		}
		key := fmt.Sprintf("%-15s %-9s %s", grantee, priv, object)
		if column != "" {
			if columns[key] == nil {
				lines = append(lines, key)
			}
			columns[key] = append(columns[key], column)
		} else {
			lines = append(lines, key)
		}

		if !slices.Contains(serviceRoles, grantee) {
			t.Errorf("%s holds %s on %s: only the service roles may hold privileges (no standing human role, SEC-03)", grantee, priv, object)
		}
		switch {
		case priv == "TRUNCATE" || priv == "REFERENCES" || priv == "TRIGGER" || priv == "MAINTAIN":
			t.Errorf("%s holds %s on %s: no application role gets TRUNCATE, REFERENCES, TRIGGER or MAINTAIN (§11.4)", grantee, priv, object)
		case writes[priv] && (grantee == "fyniq_ai" || grantee == "fyniq_readonly"):
			t.Errorf("%s holds %s on %s: the AI and read-only roles never write (AI-P2, CMP-AI-01)", grantee, priv, object)
		case writes[priv] && slices.Contains(globalTables, object):
			t.Errorf("%s holds %s on %s: a global table is read-only to every application role; it changes only through "+
				"the installation's audited functions or a release (§11.4)", grantee, priv, object)
		case (priv == "UPDATE" || priv == "DELETE") && (strings.HasPrefix(object, "journal_") || object == "ledger_revision"):
			t.Errorf("%s holds %s on %s: posted history is append-only (INV-02)", grantee, priv, object)
		case (priv == "UPDATE" || priv == "DELETE") && strings.HasPrefix(object, "audit_"):
			t.Errorf("%s holds %s on %s: the audit trail is append-only (AUD-08)", grantee, priv, object)
		case writes[priv] && (strings.HasPrefix(object, "balance") || strings.HasPrefix(object, "ledger_revision") ||
			strings.HasPrefix(object, "currency") || strings.HasPrefix(object, "tenant") || object == "audit_chain" ||
			strings.HasPrefix(object, "job_slot") || strings.HasPrefix(object, "heartbeat")):
			t.Errorf("%s holds %s on %s: only the ledger's own functions write it", grantee, priv, object)
		}
	}
	for _, l := range lines {
		if cols := columns[l]; cols != nil {
			slices.Sort(cols)
			l += " (" + strings.Join(cols, ", ") + ")"
		}
		b.WriteString(l + "\n")
	}
	golden(t, "grants.golden", b.String())
}

// req: SEC-03
// asvs: V13.2.2
// req: data/data-model Tenancy and row-level security
func TestRolesCannotBypassTheDatabase(t *testing.T) {
	c := conn(t, "fyniq")
	for _, r := range rows(t, c, `
SELECT rolname, rolsuper, rolbypassrls, rolcreaterole, rolcreatedb, rolreplication
  FROM pg_roles WHERE rolname LIKE 'fyniq\_%' ORDER BY 1`) {
		name := r[0]
		if r[1] == "true" || r[2] == "true" || r[5] == "true" {
			t.Errorf("%s is a superuser, bypasses RLS or replicates", name)
		}
		if name != "fyniq_migrator" && (r[3] == "true" || r[4] == "true") {
			t.Errorf("%s can create roles or databases; only fyniq_migrator creates roles", name)
		}
	}
	// fyniq_migrator owns everything, and no service role owns anything.
	for _, r := range rows(t, c, `
SELECT c.relname, pg_get_userbyid(c.relowner) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname IN ('fyniq', 'fyniq_migrations')
UNION ALL
SELECT p.proname, pg_get_userbyid(p.proowner) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
 WHERE n.nspname = 'fyniq'
UNION ALL
SELECT nspname, pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname IN ('fyniq', 'fyniq_ext', 'fyniq_migrations')`) {
		if r[1] != "fyniq_migrator" {
			t.Errorf("%s is owned by %s; only fyniq_migrator owns objects", r[0], r[1])
		}
	}
	// fyniq_migrator administers the service roles but cannot act as one.
	for _, r := range rows(t, c, `
SELECT g.rolname, m.rolname, am.inherit_option, am.set_option
  FROM pg_auth_members am JOIN pg_roles g ON g.oid = am.roleid JOIN pg_roles m ON m.oid = am.member
 WHERE g.rolname LIKE 'fyniq\_%'`) {
		if r[2] == "true" || r[3] == "true" {
			t.Errorf("%s may act as %s (inherit %s, set %s)", r[1], r[0], r[2], r[3])
		}
	}
	// Every SECURITY DEFINER function pins its search_path.
	for _, r := range rows(t, c, `
SELECT p.proname, coalesce(array_to_string(p.proconfig, ','), '')
  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
 WHERE n.nspname = 'fyniq' AND p.prosecdef`) {
		if !strings.Contains(r[1], "search_path=pg_catalog, fyniq, pg_temp") {
			t.Errorf("SECURITY DEFINER function %s does not pin search_path to pg_catalog, fyniq, pg_temp (got %q)", r[0], r[1])
		}
	}
}

// req: QA-ENG-05
// asvs: V2.2.1
// req: data/data-model Money columns
func TestEveryNumericColumnHasPrecisionAndRefusesNaN(t *testing.T) {
	c := conn(t, "fyniq")
	for _, r := range rows(t, c, `
SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod),
       EXISTS (SELECT 1 FROM pg_constraint k WHERE k.conrelid = c.oid AND k.contype = 'c'
                 AND a.attnum = ANY (k.conkey) AND pg_get_constraintdef(k.oid) LIKE '%<> ''NaN''::numeric%')
  FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = 'fyniq' AND c.relkind IN ('r', 'p') AND a.atttypid = 'numeric'::regtype AND NOT a.attisdropped
 ORDER BY 1, 2`) {
		if r[2] == "numeric" {
			t.Errorf("%s.%s is unconstrained NUMERIC; use the column family of §11.3", r[0], r[1])
		}
		if r[3] != "true" {
			t.Errorf("%s.%s has no CHECK refusing NaN: PostgreSQL stores NaN in NUMERIC(p,s) (§11.3)", r[0], r[1])
		}
		if _, ok := nanAttacks[r[0]+"."+r[1]]; !ok {
			t.Errorf("%s.%s has no NaN and out-of-range attack in nanAttacks (attack_test.go)", r[0], r[1])
		}
	}
}

// req: SEC-03
// asvs: V13.2.2
func TestNoFunctionIsExecutableByPublic(t *testing.T) {
	c := conn(t, "fyniq")
	for _, r := range rows(t, c, `
SELECT p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace,
       aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) acl
 WHERE n.nspname = 'fyniq' AND acl.grantee = 0`) {
		t.Errorf("PUBLIC may execute fyniq.%s; grant it to the roles that need it", r[0])
	}
}

// The schema as the migrations leave it: tables, columns, constraints, indexes, triggers, policies and
// functions. A migration that changes any of them changes this file, so the review sees it.
// req: data/data-model Logical data model
func TestSchemaSnapshot(t *testing.T) {
	c := conn(t, "fyniq")
	var b strings.Builder
	b.WriteString("# The fyniq schema after every migration. Generated by TestSchemaSnapshot; review changes.\n")
	section := func(title, sql string) {
		fmt.Fprintf(&b, "\n## %s\n", title)
		for _, r := range rows(t, c, sql) {
			b.WriteString(strings.Join(r, " | ") + "\n")
		}
	}
	section("columns", `
SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod),
       CASE WHEN a.attnotnull THEN 'not null' ELSE 'null' END, coalesce(pg_get_expr(d.adbin, d.adrelid), '')
  FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
  LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum
 WHERE n.nspname = 'fyniq' AND c.relkind IN ('r', 'p', 'v') AND a.attnum > 0 AND NOT a.attisdropped
 ORDER BY c.relname, a.attnum`)
	section("constraints", `
SELECT c.relname, k.conname, pg_get_constraintdef(k.oid)
  FROM pg_constraint k JOIN pg_class c ON c.oid = k.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = 'fyniq' ORDER BY 1, 2`)
	section("indexes", `
SELECT pg_get_indexdef(i.indexrelid) FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid
  JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'fyniq' ORDER BY 1`)
	section("triggers", `
SELECT pg_get_triggerdef(g.oid) FROM pg_trigger g JOIN pg_class c ON c.oid = g.tgrelid
  JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'fyniq' AND NOT g.tgisinternal ORDER BY 1`)
	section("policies", `
SELECT c.relname, p.polname, p.polcmd::text, pg_get_expr(p.polqual, p.polrelid), pg_get_expr(p.polwithcheck, p.polrelid)
  FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid ORDER BY 1, 2`)
	section("functions", `
SELECT p.proname || '(' || pg_get_function_arguments(p.oid) || ') → ' || pg_get_function_result(p.oid),
       CASE WHEN p.prosecdef THEN 'security definer' ELSE 'security invoker' END,
       p.provolatile::text, coalesce(array_to_string(p.proconfig, ','), ''), md5(p.prosrc)
  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = 'fyniq' ORDER BY 1`)
	section("extensions", `SELECT e.extname, n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace
 WHERE e.extname <> 'plpgsql' ORDER BY 1`)
	golden(t, "schema.golden", b.String())
}

// The money package and the database know the same currencies, with the same minor units.
func TestTheCurrencyListMatchesTheDatabase(t *testing.T) {
	want := money.Currencies()
	got := map[string]int{}
	for _, r := range rows(t, conn(t, "fyniq"), "SELECT code, minor_units FROM fyniq.currency ORDER BY code") {
		var units int
		_, _ = fmt.Sscan(r[1], &units)
		got[r[0]] = units
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("the database has %v; internal/domain/money has %v", got, want)
	}
}
