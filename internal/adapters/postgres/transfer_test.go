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

// The overseas transfers keep every version (migration 000307, FY316): a transfer is saved and revised, each audited;
// a version out of order, one that changes the flow it covers, an unknown basis, a review not after the assessment, and
// anything changed or deleted are refused; the worker writes none of it; another organisation sees none.
// req: CMP-PD-06
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackTheTransfersKeepTheirHistory(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Transfers A Pte Ltd"), books(t, "Transfers B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	dpo := "user:" + founder(t, api, a.Org, 1, now).String()
	assessed := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	transfer := storage.Transfer{Flow: "email", Recipient: "An email provider", Countries: "United States", Data: "Recipients' names and email addresses",
		Purpose: "To send email", Basis: "contract", AssessedOn: assessed, AssessedBy: "The DPO", Evidence: "The provider's data processing addendum",
		NextReviewOn: assessed.AddDate(1, 0, 0), State: "open", SavedBy: dpo, SavedAt: now, Version: 1}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		t2, err := u.Compliance().SaveTransfer(ctx, transfer)
		if err != nil {
			return err
		}
		transfer = t2
		t2.Version, t2.Basis = 2, "certification"
		_, err = u.Compliance().SaveTransfer(ctx, t2)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		ts, err := u.Compliance().Transfers(ctx)
		if err == nil && (len(ts) != 1 || ts[0].Version != 2 || ts[0].Basis != "certification" || ts[0].Flow != "email") {
			t.Errorf("the transfers: %+v", ts)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var actions []string
	if err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action FROM fyniq.audit_event WHERE object_type = 'pd_transfer' ORDER BY seq`)
		if err != nil {
			return err
		}
		actions, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil || len(actions) != 2 || actions[0] != "transfer.saved" {
		t.Errorf("the transfers' audit events: %v %v", actions, err)
	}
	version := `INSERT INTO fyniq.pd_transfer (transfer_id, version, flow, recipient, countries, data, purpose, basis, assessed_on, assessed_by, evidence,
	              next_review_on, state, saved_by, saved_at)
	            VALUES ($1, $2, $3, 'x', 'y', 'z', 'p', $4, DATE '2026-11-01', 'a', 'e', $5, 'open', 'user:x', now())`
	later := time.Date(2027, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker adding a transfer", "fyniq_worker", version, []any{uuid.New(), 1, nil, "contract", later}, []string{"42501"}},
		{"a version skipping one", "fyniq_api", version, []any{transfer.TransferID, 4, "email", "contract", later}, []string{"FY316"}},
		{"a version for another flow", "fyniq_api", version, []any{transfer.TransferID, 3, "sign-in", "contract", later}, []string{"FY316"}},
		{"a version for no flow", "fyniq_api", version, []any{transfer.TransferID, 3, nil, "contract", later}, []string{"FY316"}},
		{"an unknown basis", "fyniq_api", version, []any{uuid.New(), 1, nil, "trust", later}, []string{"23514"}},
		{"a review before the assessment", "fyniq_api", version, []any{uuid.New(), 1, nil, "contract", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
			[]string{"23514"}},
		{"the API changing a transfer", "fyniq_api", `UPDATE fyniq.pd_transfer SET recipient = 'z'`, nil, []string{"42501"}},
		{"the owner changing a transfer", "fyniq_migrator", `UPDATE fyniq.pd_transfer SET recipient = 'z' WHERE transfer_id = $1`, []any{transfer.TransferID},
			[]string{"FY316"}},
		{"the owner deleting a transfer", "fyniq_migrator", `DELETE FROM fyniq.pd_transfer`, nil, []string{"FY316"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		ts, err := u.Compliance().Transfers(ctx)
		if err == nil && len(ts) != 0 {
			t.Errorf("another organisation sees %d transfers", len(ts))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
