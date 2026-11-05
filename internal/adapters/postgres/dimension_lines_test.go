//go:build local

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-DIM-01 and FR-DIM-02 (rev 0.171; p1-organisation-chart task 3.3, slice 3): a journal's lines post with their
// dimension values, each combination held in one dimension set that later lines reuse, and the balances cache keeps
// each set's own row; a reversal keeps each line's values, so every set nets to zero; an account that requires
// Department refuses a journal line without one (DIM-05) and posts one with it; an account requires only the
// organisation's own dimensions, once each (FY259); every entry replays.
// req: FR-DIM-01
// req: FR-DIM-02
// req: INV-08
// req: ENG-06
func TestJournalLinesCarryTheirDimensionValues(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := porttest.NewBooks(t, s, "Dimension lines Pte Ltd")
	svc := journal.Service{Store: s}
	var dept, proj storage.Dimension
	var eng, atlas storage.DimensionValue
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if dept, err = u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Department"}); err != nil {
			return err
		}
		if proj, err = u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionProject, Name: "Project"}); err != nil {
			return err
		}
		if eng, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "ENG", Name: "Engineering"}); err != nil {
			return err
		}
		atlas, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: proj.ID, Code: "ATLAS", Name: "Atlas"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	post := func(amt string, values ...uuid.UUID) (storage.Document, storage.Entry, error) {
		t.Helper()
		d := saleDraft(t, b, porttest.Day(10, 14), amt)
		d.Lines[1].Values = values
		doc, err := svc.Create(ctx, b.Org, d)
		if err != nil {
			t.Fatal(err)
		}
		e, err := svc.Post(ctx, b.Org, doc.ID)
		return doc, e, err
	}
	first, e1, err := post("100.00", eng.ID, atlas.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := e1.Lines[1].DimensionValues; !slices.Equal(got, ascending(eng.ID, atlas.ID)) || e1.Lines[0].DimensionValues != nil {
		t.Errorf("the posted lines' values: %v and %v", e1.Lines[0].DimensionValues, got)
	}
	if _, _, err := post("40.00", atlas.ID, eng.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := post("5.00", eng.ID); err != nil {
		t.Fatal(err)
	}

	type cell struct {
		set     string
		balance string
	}
	sales := func() []cell {
		var out []cell
		if err := asOrg(ctx, conn(t, "fyniq_readonly"), b.Org, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT coalesce(array_to_string(s.value_ids, ','), '-'), sum(c.base_amount)::text
FROM fyniq.balance c LEFT JOIN fyniq.dimension_set s ON s.organisation_id = c.organisation_id AND s.id = c.dimension_set_id
WHERE c.account_id = $1 GROUP BY 1 ORDER BY 1`, b.Sales.ID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var c cell
				if err := rows.Scan(&c.set, &c.balance); err != nil {
					return err
				}
				out = append(out, c)
			}
			return rows.Err()
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	both := ascending(eng.ID, atlas.ID)
	want := []cell{{both[0].String() + "," + both[1].String(), "-140.0000"}, {eng.ID.String(), "-5.0000"}}
	slices.SortFunc(want, func(x, y cell) int { return compare(x.set, y.set) })
	if got := sales(); !slices.Equal(got, want) {
		t.Errorf("the cache per set: %v, want %v", got, want)
	}
	if n := count(t, b.Org, "dimension_set"); n != 2 {
		t.Errorf("%d dimension sets for 2 combinations", n)
	}

	// The void reverses each line with its values: the first journal's set falls back to its second's.
	r, err := svc.Reverse(ctx, b.Org, first.ID, porttest.Day(10, 15), "Wrong customer")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Lines[1].DimensionValues, both) {
		t.Errorf("the reversal's values: %v", r.Lines[1].DimensionValues)
	}
	want[slices.IndexFunc(want, func(c cell) bool { return c.set != eng.ID.String() })].balance = "-40.0000"
	if got := sales(); !slices.Equal(got, want) {
		t.Errorf("after the void: %v, want %v", got, want)
	}

	// Sales requires Department: a journal line without one is refused, one with it posts.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Get(ctx, b.Sales.ID)
		if err != nil {
			return err
		}
		c := storage.AccountChange{Name: a.Name, ParentID: a.ParentID, Group: a.Group, Active: a.Active, AccountAttributes: a.AccountAttributes}
		c.RequiredDimensions = []uuid.UUID{dept.ID}
		_, err = u.Accounts().Update(ctx, a.ID, a.Version, c)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var probs ledger.Problems
	if _, _, err := post("7.00", atlas.ID); !errors.As(err, &probs) || !probs.Has(ledger.CodeDimensionRequired) {
		t.Errorf("a project without a department: %v", err)
	}
	if _, _, err := post("7.00", eng.ID); err != nil {
		t.Errorf("with a department: %v", err)
	}
	var se *storage.Error
	err = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Get(ctx, b.Bank.ID)
		if err != nil {
			return err
		}
		c := storage.AccountChange{Name: a.Name, ParentID: a.ParentID, Group: a.Group, Active: a.Active, AccountAttributes: a.AccountAttributes}
		c.RequiredDimensions = []uuid.UUID{dept.ID, dept.ID}
		_, err = u.Accounts().Update(ctx, a.ID, a.Version, c)
		return err
	})
	if !errors.As(err, &se) || se.Code != storage.CodeDimensionRequirement {
		t.Errorf("requiring Department twice: %v", err)
	}

	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rep, err := journal.Replay(ctx, u)
		if err == nil && len(rep.Mismatches) > 0 {
			t.Errorf("replay: %+v", rep.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func compare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
