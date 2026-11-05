//go:build local

package postgres_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// dimensions gives the books a Department with two values and a Project with one, written as the owner with their
// audit events: the application roles write dimensions with the API that comes with them (FR-DIM-01).
func dimensions(t *testing.T, b porttest.Books) (dept1, dept2, proj uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		value := func(dim uuid.UUID, code string) (uuid.UUID, error) {
			var id uuid.UUID
			return id, tx.QueryRow(ctx, "INSERT INTO fyniq.dimension_value (dimension_id, code, name) VALUES ($1, $2, $2) RETURNING id",
				dim, code).Scan(&id)
		}
		for _, d := range []struct {
			kind  string
			codes []string
		}{{"department", []string{"ENG", "OPS"}}, {"project", []string{"ATLAS"}}} {
			var dim uuid.UUID
			if err := tx.QueryRow(ctx, "INSERT INTO fyniq.dimension (kind, name) VALUES ($1, initcap($1)) RETURNING id", d.kind).Scan(&dim); err != nil {
				return err
			}
			if err := auditRaw(ctx, tx, "dimension.created", "dimension", dim); err != nil {
				return err
			}
			for _, code := range d.codes {
				id, err := value(dim, code)
				if err != nil {
					return err
				}
				switch code {
				case "ENG":
					dept1 = id
				case "OPS":
					dept2 = id
				default:
					proj = id
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return dept1, dept2, proj
}

// dimensionSet writes a set of values as the application writes it, and returns its ID.
func dimensionSet(t *testing.T, b porttest.Books, values ...uuid.UUID) (uuid.UUID, error) {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "INSERT INTO fyniq.dimension_set (value_ids) VALUES ($1) RETURNING id", values).Scan(&id)
	})
	return id, err
}

func ascending(ids ...uuid.UUID) []uuid.UUID {
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return ids
}

// A dimension set is one combination of the organisation's own values, at most one of each dimension, written in its
// canonical form, held once, and never changed once written, by any role: posted lines name it (§11.4, INV-02).
// req: FR-DIM-01
// req: FR-DIM-02
// req: INV-02
// req: QA-ENG-05
// asvs: V13.2.2
func TestAttackDimensionSetsAreCanonicalAndNeverChange(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack dimension sets Pte Ltd")
	dept1, dept2, proj := dimensions(t, b)
	other := books(t, "Attack dimension sets B Pte Ltd")
	foreign, _, _ := dimensions(t, other)

	both := ascending(dept1, proj)
	set, err := dimensionSet(t, b, both...)
	if err != nil {
		t.Fatalf("a department and a project: %v", err)
	}
	_, err = dimensionSet(t, b, both...)
	wantState(t, "the same combination twice", err, "23505")
	_, err = dimensionSet(t, b, both[1], both[0])
	wantState(t, "values out of order", err, "FY257")
	_, err = dimensionSet(t, b, ascending(dept1, dept2)...)
	wantState(t, "two values of one dimension", err, "FY257")
	_, err = dimensionSet(t, b, dept1, dept1)
	wantState(t, "a value twice", err, "FY257")
	_, err = dimensionSet(t, b, uuid.Must(uuid.NewV7()))
	wantState(t, "a value nobody has", err, "FY257")
	_, err = dimensionSet(t, b, foreign)
	wantState(t, "another organisation's value", err, "FY257")
	_, err = dimensionSet(t, b, []uuid.UUID{}...)
	wantState(t, "no value at all (a line with none names no set)", err, "23514")

	for _, role := range []string{"fyniq_api", "fyniq_migrator"} {
		err := asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE fyniq.dimension_set SET value_ids = $2 WHERE id = $1", set, []uuid.UUID{dept1})
			return err
		})
		wantState(t, role+" changing a set", err, "42501", "FY002")
		err = asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "DELETE FROM fyniq.dimension_set WHERE id = $1", set)
			return err
		})
		wantState(t, role+" deleting a set", err, "42501", "FY002")
	}
}

// The balances cache is keyed by account × period × dimension set × currency (§11.4): each set of an account keeps its
// own row, the lines with no dimension one more, and the rebuild and the comparison with the lines agree per set
// (INV-08). The balance service's period balances sum an account's sets (FR-DIM-02: a reader asks for a dimension to see
// it apart), so every report reads as before.
// req: FR-DIM-02
// req: INV-08
// req: QA-ENG-05
// asvs: V13.2.2
func TestAttackTheBalancesCacheKeepsEachDimensionSet(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack balances by set Pte Ltd")
	dept1, _, proj := dimensions(t, b)
	s1, err := dimensionSet(t, b, dept1)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := dimensionSet(t, b, ascending(dept1, proj)...)
	if err != nil {
		t.Fatal(err)
	}
	post := func(identity string, lines ...[3]any) {
		t.Helper()
		err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
			id, err := entry(ctx, tx, identity, "2026-10-14")
			if err != nil {
				return err
			}
			for i, l := range lines {
				if _, err := tx.Exec(ctx, `
INSERT INTO fyniq.journal_line (entry_id, line_no, account_id, currency, amount, fx_rate, fx_rate_source, base_amount, dimension_set_id)
VALUES ($1, $2, $3, 'SGD', $4::numeric, 1, 'base', $4::numeric, $5)`, id, i+1, l[0], l[1], l[2]); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("post %s: %v", identity, err)
		}
	}
	none := (*uuid.UUID)(nil)
	post("by-set-1", [3]any{b.Bank.ID, "100.00", none}, [3]any{b.Sales.ID, "-60.00", s1}, [3]any{b.Sales.ID, "-40.00", s2})
	post("by-set-2", [3]any{b.Bank.ID, "5.00", none}, [3]any{b.Sales.ID, "-5.00", s1})

	cached := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		err := asOrg(ctx, conn(t, "fyniq_readonly"), b.Org, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT account_id::text || '/' || coalesce(dimension_set_id::text, 'none'), amount::text || ' x' || line_count
  FROM fyniq.balance WHERE period_id = $1`, b.Oct.ID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var k, v string
				if err := rows.Scan(&k, &v); err != nil {
					return err
				}
				out[k] = v
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	want := map[string]string{
		b.Bank.ID.String() + "/none":            "105.0000 x2",
		b.Sales.ID.String() + "/" + s1.String(): "-65.0000 x2",
		b.Sales.ID.String() + "/" + s2.String(): "-40.0000 x1",
	}
	if got := cached(); !sameMap(got, want) {
		t.Fatalf("the cache holds %v; want %v", got, want)
	}

	api := store(t, "fyniq_api")
	var period []storage.Balance
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		period, err = u.Balances().ForPeriod(ctx, b.Oct.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, x := range period {
		if x.AccountID == b.Sales.ID && (x.Amount != "-105.0000" || x.LineCount != 3) {
			t.Errorf("the period's sales balance is %s over %d lines; want the sets summed, -105.0000 over 3", x.Amount, x.LineCount)
		}
	}
	if len(period) != 2 {
		t.Errorf("the period has %d balances; want one per account and currency: %v", len(period), period)
	}

	worker := store(t, "fyniq_worker")
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		diff, err := u.Balances().Differences(ctx)
		if err != nil || len(diff) != 0 {
			t.Errorf("the cache and the lines differ before the rebuild: %v, %v", diff, err)
		}
		n, err := u.Balances().Rebuild(ctx)
		if err != nil || n != 3 {
			t.Errorf("the rebuild wrote %d rows, %v; want one per account and set, 3", n, err)
		}
		diff, err = u.Balances().Differences(ctx)
		if err != nil || len(diff) != 0 {
			t.Errorf("the cache and the lines differ after the rebuild: %v, %v", diff, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := cached(); !sameMap(got, want) {
		t.Fatalf("after the rebuild the cache holds %v; want %v", got, want)
	}
}

func sameMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// A dimension keeps its kind and place and a value its dimension and code, each changes one version at a time, and
// neither is ever deleted, as the dimension sets lines name refer to them: the API role has no grant for any of it, and
// the guard refuses it to the owner too (FR-DIM-01; FY258). The API role renames and puts out of use, at the next
// version, with its audit event.
// req: FR-DIM-01
// req: QA-ENG-05
// asvs: V13.2.2, V2.3.2
func TestAttackDimensionsKeepWhatTheyAre(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack dimensions keep Pte Ltd")
	eng, _, _ := dimensions(t, b)
	var dept uuid.UUID
	if err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT dimension_id FROM fyniq.dimension_value WHERE id = $1", eng).Scan(&dept)
	}); err != nil {
		t.Fatal(err)
	}
	exec := func(role, sql string, args ...any) error {
		return asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error {
			if err := auditRaw(ctx, tx, "dimension.changed", "dimension", dept); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
	}
	for _, x := range []struct {
		name, sql string
	}{
		{"changing a dimension's kind", "UPDATE fyniq.dimension SET kind = 'project', version = version + 1 WHERE id = $1"},
		{"moving a dimension to a place", "UPDATE fyniq.dimension SET position = 2, version = version + 1 WHERE id = $1"},
		{"skipping a dimension's version", "UPDATE fyniq.dimension SET name = 'Teams', version = version + 2 WHERE id = $1"},
		{"keeping a dimension's version", "UPDATE fyniq.dimension SET name = 'Teams' WHERE id = $1"},
		{"deleting a dimension", "DELETE FROM fyniq.dimension WHERE id = $1"},
		{"changing a value's code", "UPDATE fyniq.dimension_value SET code = 'RND', version = version + 1 WHERE dimension_id = $1"},
		{"deleting a value", "DELETE FROM fyniq.dimension_value WHERE dimension_id = $1"},
	} {
		wantState(t, "the API role "+x.name, exec("fyniq_api", x.sql, dept), "42501", "FY258")
		wantState(t, "the owner "+x.name, exec("fyniq_migrator", x.sql, dept), "FY258")
	}
	err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, "INSERT INTO fyniq.dimension (kind, position, name, version) VALUES ('custom', 1, 'Region', 2) RETURNING id").Scan(&id); err != nil {
			return err
		}
		return auditRaw(ctx, tx, "dimension.created", "dimension", id)
	})
	wantState(t, "a new dimension at version 2", err, "FY258")
	err = asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO fyniq.dimension (kind, position, name, is_active) VALUES ('custom', 1, 'Region', false)")
		return err
	})
	wantState(t, "the API role adding a dimension out of use", err, "42501")

	if err := exec("fyniq_api", "UPDATE fyniq.dimension SET name = 'Teams', is_active = false, version = version + 1 WHERE id = $1", dept); err != nil {
		t.Errorf("renaming and putting out of use: %v", err)
	}
	if err := exec("fyniq_api", "UPDATE fyniq.dimension_value SET name = 'Engineering', is_active = false, version = version + 1 WHERE id = $1", eng); err != nil {
		t.Errorf("renaming a value and putting it out of use: %v", err)
	}
	err = asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.dimension SET name = 'Unaudited', version = version + 1 WHERE id = $1", dept)
		return err
	})
	wantState(t, "a change without its audit event", err, "FY061")
}
