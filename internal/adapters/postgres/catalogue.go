package postgres

import (
	"context"
	"embed"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The snapshots the schema tests keep (TestEachRoleHoldsExactlyItsGrants, TestSchemaSnapshot): what the migrations of
// this build make. schema.verify compares the database's guards with them each night (p1-compliance-center D4).
//
//go:embed testdata/grants.golden testdata/schema.golden
var snapshots embed.FS

// Querier is a connection, a pool or a transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// snapshotPath sets, until the transaction ends, the search_path the snapshots are read with, which decides how the
// catalogue's functions qualify names: schema fyniq, as the tests' superuser, fyniq, sees it.
const snapshotPath = "SELECT set_config('search_path', 'fyniq', true)"

// GrantsSQL lists every privilege on the objects in schema fyniq, by grantee, table and column.
const GrantsSQL = `
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

// The guards of the schema snapshot: the triggers, the row-level security policies, and which tables enable and force
// row-level security.
const (
	TriggersSQL = `
SELECT pg_get_triggerdef(g.oid) FROM pg_trigger g JOIN pg_class c ON c.oid = g.tgrelid
  JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'fyniq' AND NOT g.tgisinternal ORDER BY 1`
	PoliciesSQL = `
SELECT c.relname, p.polname, p.polcmd::text, pg_get_expr(p.polqual, p.polrelid), pg_get_expr(p.polwithcheck, p.polrelid)
  FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid ORDER BY 1, 2`
	RowSecuritySQL = `
SELECT c.relname, CASE WHEN c.relrowsecurity THEN 'enabled' ELSE 'disabled' END,
       CASE WHEN c.relforcerowsecurity THEN 'forced' ELSE 'not forced' END
  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = 'fyniq' AND c.relkind IN ('r', 'p') ORDER BY 1`
)

// Grant is a privilege a role holds on an object, or on one column of it.
type Grant struct {
	Grantee, Object, Column, Privilege string
}

// ReadGrants reads every privilege in the fyniq schemas.
func ReadGrants(ctx context.Context, q Querier) ([]Grant, error) {
	rows, err := readRows(ctx, q, GrantsSQL)
	if err != nil {
		return nil, err
	}
	out := make([]Grant, len(rows))
	for i, r := range rows {
		out[i] = Grant{Grantee: r[0], Object: r[1], Column: r[2], Privilege: r[3]}
	}
	return out, nil
}

// GrantLines are grants as grants.golden lists them: one line per role, privilege and object, with the columns of a
// column grant in brackets, leaving out the migrator, which owns everything.
func GrantLines(grants []Grant) []string {
	var lines []string
	columns := map[string][]string{}
	for _, g := range grants {
		if g.Grantee == "fyniq_migrator" {
			continue
		}
		key := fmt.Sprintf("%-15s %-9s %s", g.Grantee, g.Privilege, g.Object)
		if g.Column == "" {
			lines = append(lines, key)
			continue
		}
		if columns[key] == nil {
			lines = append(lines, key)
		}
		columns[key] = append(columns[key], g.Column)
	}
	for i, l := range lines {
		if cols := columns[l]; cols != nil {
			slices.Sort(cols)
			lines[i] = l + " (" + strings.Join(cols, ", ") + ")"
		}
	}
	return lines
}

// readRows reads a query's rows as text, as the snapshots write them.
func readRows(ctx context.Context, q Querier, sql string) ([][]string, error) {
	r, err := q.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out [][]string
	for r.Next() {
		vals, err := r.Values()
		if err != nil {
			return nil, err
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			row[i] = fmt.Sprint(v)
		}
		out = append(out, row)
	}
	return out, r.Err()
}

// readLines reads a query's rows as the schema snapshot's lines: fields joined by " | ".
func readLines(ctx context.Context, q Querier, sql string) ([]string, error) {
	rows, err := readRows(ctx, q, sql)
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = strings.Join(r, " | ")
	}
	return out, err
}

// ReadGuards reads the database's guards from its catalogue, with the snapshots' search_path; tx is a transaction, which
// that search_path lasts until it ends.
func ReadGuards(ctx context.Context, tx Querier) (storage.Guards, error) {
	var g storage.Guards
	if _, err := readRows(ctx, tx, snapshotPath); err != nil {
		return g, err
	}
	grants, err := ReadGrants(ctx, tx)
	if err != nil {
		return g, err
	}
	g.Grants = GrantLines(grants)
	if g.Triggers, err = readLines(ctx, tx, TriggersSQL); err != nil {
		return g, err
	}
	if g.Policies, err = readLines(ctx, tx, PoliciesSQL); err != nil {
		return g, err
	}
	g.RowSecurity, err = readLines(ctx, tx, RowSecuritySQL)
	return g, err
}

// ReleaseGuards are the guards of the snapshots this build carries: what its migrations make.
func ReleaseGuards() (storage.Guards, error) {
	var g storage.Guards
	grants, err := snapshots.ReadFile("testdata/grants.golden")
	if err != nil {
		return g, err
	}
	for _, l := range strings.Split(string(grants), "\n") {
		if l != "" && !strings.HasPrefix(l, "#") {
			g.Grants = append(g.Grants, l)
		}
	}
	schema, err := snapshots.ReadFile("testdata/schema.golden")
	if err != nil {
		return g, err
	}
	sections := map[string]*[]string{"triggers": &g.Triggers, "policies": &g.Policies, "row-level security": &g.RowSecurity}
	var into *[]string
	for _, l := range strings.Split(string(schema), "\n") {
		if title, ok := strings.CutPrefix(l, "## "); ok {
			into = sections[title]
			continue
		}
		if into != nil && l != "" {
			*into = append(*into, l)
		}
	}
	if len(g.Grants) == 0 || len(g.Triggers) == 0 || len(g.Policies) == 0 || len(g.RowSecurity) == 0 {
		return g, fmt.Errorf("the schema snapshots this build carries are incomplete: %d grants, %d triggers, %d policies, %d tables",
			len(g.Grants), len(g.Triggers), len(g.Policies), len(g.RowSecurity))
	}
	return g, nil
}
