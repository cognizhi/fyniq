//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Access and correction requests keep their steps in order (migration 000305, FY314): a request is recorded, due 30 days
// after receipt, acknowledged, answered and closed, each audited; a step after closing, a step twice, closing before an
// answer, an extension late or to an earlier day, a due day not 30 days on, and anything changed or deleted are refused;
// the worker writes none of it; another organisation sees none.
// req: CMP-PD-04
// req: AUD-01
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackRequestsKeepTheirSteps(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Requests A Pte Ltd"), books(t, "Requests B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	dpo := "user:" + founder(t, api, a.Org, 1, now).String()
	received := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var req, late storage.PDRequest
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := u.Compliance().RecordPDRequest(ctx, storage.PDRequest{Kind: "access", Subject: "person", Requester: "Ada Tan", Details: "A copy",
			ReceivedOn: received, CreatedBy: dpo, CreatedAt: now})
		if err != nil {
			return err
		}
		req = r
		if late, err = u.Compliance().RecordPDRequest(ctx, storage.PDRequest{Kind: "access", Subject: "person", Requester: "Ben Lee", Details: "A copy",
			ReceivedOn: received.AddDate(0, 0, -40), CreatedBy: dpo, CreatedAt: now}); err != nil {
			return err
		}
		for _, s := range []string{"acknowledged", "answered", "closed"} {
			st := storage.PDRequestStep{Step: s, At: now, By: dpo}
			if s == "answered" {
				st.Outcome = "provided"
			}
			if err := u.Compliance().StepPDRequest(ctx, r.ID, st); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !req.DueOn.Equal(received.AddDate(0, 0, 30)) {
		t.Errorf("due %s, want 30 days after %s", req.DueOn, received)
	}
	var actions []string
	if err := asOrg(ctx, conn(t, "fyniq_api"), a.Org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action FROM fyniq.audit_event WHERE object_type = 'pd_request' AND object_id = $1 ORDER BY seq`, req.ID)
		if err != nil {
			return err
		}
		actions, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil || len(actions) != 4 || actions[0] != "pd_request.received" || actions[3] != "pd_request.closed" {
		t.Errorf("the request's audit events: %v %v", actions, err)
	}
	step := `INSERT INTO fyniq.pd_request_step (request_id, step, at, by_actor, new_due_on, outcome) VALUES ($1, $2, now(), 'user:x', $3, $4)`
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"the worker recording a request", "fyniq_worker", `INSERT INTO fyniq.pd_request (kind, subject, requester, details, received_on, due_on, created_by, created_at)
		  VALUES ('access', 'person', 'x', 'y', current_date, current_date + 30, 'user:x', now())`, nil, []string{"42501"}},
		{"a due day not 30 days on", "fyniq_api", `INSERT INTO fyniq.pd_request (kind, subject, requester, details, received_on, due_on, created_by, created_at)
		  VALUES ('access', 'person', 'x', 'y', current_date, current_date + 60, 'user:x', now())`, nil, []string{"23514"}},
		{"someone else's request with nobody asking", "fyniq_api", `INSERT INTO fyniq.pd_request (kind, subject, details, received_on, due_on, created_by, created_at)
		  VALUES ('access', 'person', 'y', current_date, current_date + 30, 'user:x', now())`, nil, []string{"23514"}},
		{"a step after closing", "fyniq_api", step, []any{req.ID, "acknowledged", nil, nil}, []string{"FY314"}},
		{"closing before an answer", "fyniq_api", step, []any{late.ID, "closed", nil, nil}, []string{"FY314"}},
		{"an extension after the first 30 days", "fyniq_api", step, []any{late.ID, "extended", received.AddDate(0, 1, 0), nil}, []string{"FY314"}},
		{"an answer without its outcome", "fyniq_api", step, []any{late.ID, "answered", nil, nil}, []string{"23514"}},
		{"the API changing a step", "fyniq_api", `UPDATE fyniq.pd_request_step SET note = 'z'`, nil, []string{"42501"}},
		{"the owner deleting a request's step", "fyniq_migrator", `DELETE FROM fyniq.pd_request_step`, nil, []string{"FY314"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt, c.args...); return err })
		wantState(t, c.what, err, c.states...)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rs, err := u.Compliance().PDRequests(ctx)
		if err == nil && len(rs) != 0 {
			t.Errorf("another organisation sees %d requests", len(rs))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
