//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// An undo, a first view and a month's review are appended and never changed or deleted (FY951): each once, with the API alone writing them.
// req: AI-31
// req: CTL-AI-07
// asvs: V8.2.2
func TestUndosViewsAndReviewsAreAppendOnly(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Proposal trail Pte Ltd")
	now := clock.System{}.Now().UTC()
	by := uuid.New()
	month := now.AddDate(0, -1, 0)
	month = month.AddDate(0, 0, 1-month.Day()).Truncate(24 * 60 * 60 * 1e9)
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r := u.AIProposalTrail()
		// A month's review is signed once, and an empty month has no rows.
		rows, err := r.QualityRows(ctx, now.AddDate(0, -1, 0), now.AddDate(0, 1, 0))
		if err != nil || len(rows) != 0 {
			t.Errorf("an empty month: %d %v", len(rows), err)
		}
		if _, err := r.Review(ctx, month, by, now, json.RawMessage(`{"made":0}`), "fine"); err != nil {
			return err
		}
		if _, err := r.Review(ctx, month, by, now, json.RawMessage(`{"made":0}`), "again"); err == nil {
			t.Error("a month's review was signed twice")
		}
		got, ok, err := r.ReviewOf(ctx, month)
		if err != nil || !ok || got.Note != "fine" || got.SignedBy != by {
			t.Errorf("the review: %+v %v %v", got, ok, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "change a review", "UPDATE fyniq.ai_quality_review SET note = 'x'", "FY951"},
		{"fyniq_migrator", "delete a review", "DELETE FROM fyniq.ai_quality_review", "FY951"},
		{"fyniq_migrator", "truncate the undos", "TRUNCATE fyniq.ai_proposal_undo", "FY002"},
		{"fyniq_worker", "write a review as the worker", "INSERT INTO fyniq.ai_quality_review (month, signed_by, signed_at, report) VALUES ('2020-01-01', gen_random_uuid(), now(), '{}')", "42501"},
		{"fyniq_executor", "read an undo as the executor", "SELECT * FROM fyniq.ai_proposal_undo", "42501"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.state, x.state)
	}
}
