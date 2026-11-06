//go:build local

package postgres_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-DIM-01 and FR-DIM-02 on expense claims (rev 0.171; slice 3d): a claim's line keeps its dimension values through its
// draft, and once approved its cost posts with them; Meals requiring Department, a line of a meal without one is what
// the check says stops it (DIM-05); every entry replays.
// req: FR-DIM-01
// req: FR-DIM-02
func TestClaimLinesCarryTheirDimensionValues(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "mei-ling"}})
	s := store(t, "fyniq_api")
	b := newClaimBooks(t, ctx, s, "Dimension claims Pte Ltd")
	var eng storage.DimensionValue
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		dept, err := u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Department"})
		if err != nil {
			return err
		}
		if eng, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "ENG", Name: "Engineering"}); err != nil {
			return err
		}
		a, err := u.Accounts().Get(ctx, b.Meals.ID)
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
	claims := journal.Claims{Store: s}
	oct24, oct26 := porttest.Day(10, 24), porttest.Day(10, 26)
	meal := func(values ...uuid.UUID) claim.Draft {
		return claim.Draft{ClaimantID: b.Claimant, Date: oct26, Title: "Team lunch", Lines: []claim.Line{{Date: oct24, Merchant: "Din Tai Fung",
			CategoryID: b.Dining.ID, Description: "Lunch with Acme", Amount: sgdOf(t, "109.00"), GSTCode: "TX", GST: sgdOf(t, "9.00"),
			Document: bill.DocSimplified, Attendees: "Mei Ling Tan; Tan Wei, Acme", Values: values}}}
	}

	none, err := claims.Create(ctx, b.Org, meal())
	if err != nil {
		t.Fatal(err)
	}
	if c, err := claims.Check(ctx, b.Org, none.ID); err != nil || !c.Problems.Has(ledger.CodeDimensionRequired) {
		t.Errorf("a meal with no department: %+v %v", c.Problems, err)
	}

	doc, err := claims.Create(ctx, b.Org, meal(eng.ID))
	if err != nil {
		t.Fatal(err)
	}
	if c, err := claims.Check(ctx, b.Org, doc.ID); err != nil || len(c.Problems) > 0 {
		t.Fatalf("a meal with Engineering: %+v %v", c.Problems, err)
	}
	approved(t, s, b, doc.ID, oct26)
	e, err := claims.Post(ctx, b.Org, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range e.Lines {
		want := []uuid.UUID(nil)
		if l.AccountID == b.Meals.ID {
			want = []uuid.UUID{eng.ID}
		}
		if !slices.Equal(l.DimensionValues, want) {
			t.Errorf("the line on %s: %v, want %v", l.AccountID, l.DimensionValues, want)
		}
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
