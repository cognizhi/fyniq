//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Comments on records are the database's too (golden rules 2, 7 and 8; FR-UX-13, FR-UX-14): each is a new row with its audit event, kept
// in the order written, never changed or deleted by anyone, refused when empty, over 2,000 characters, on a kind of record that takes none
// or mentioning more than ten people, written only as a member, and invisible to another organisation.
// req: FR-UX-13
// req: FR-UX-14
// asvs: V8.2.1, V4.2.1
func TestAttackCommentsKeepTheirThreadAndTheirTenant(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Comments Pte Ltd")
	other := books(t, "Comments elsewhere Pte Ltd")
	now := clock.System{}.Now()
	user := founder(t, store(t, "fyniq_api"), b.Org, 73, now)
	record := uuid.New()
	add := func(body string, mentions ...uuid.UUID) {
		t.Helper()
		if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.RecordComments().Add(ctx, storage.NewRecordComment{RecordType: "bill", RecordID: record, Author: user, Body: body, Mentions: mentions, CreatedAt: now})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("First.")
	add("Second, for the founder.", user)
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		thread, err := u.RecordComments().Thread(ctx, "bill", record)
		if err != nil || len(thread) != 2 || thread[0].Body != "First." || thread[1].Body != "Second, for the founder." || len(thread[1].Mentions) != 1 || thread[1].Mentions[0] != user {
			t.Errorf("the thread: %+v %v", thread, err)
		}
		if other, err := u.RecordComments().Thread(ctx, "invoice", record); err != nil || len(other) != 0 {
			t.Errorf("another kind of record under the same ID: %v %v", other, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store(t, "fyniq_api").Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if thread, err := u.RecordComments().Thread(ctx, "bill", record); err != nil || len(thread) != 0 {
			t.Errorf("another organisation reads the thread: %v %v", thread, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	insert := "INSERT INTO fyniq.record_comment (record_type, record_id, author, body, created_at) SELECT "
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "change a comment", "UPDATE fyniq.record_comment SET body = 'x'", "FY189"},
		{"fyniq_migrator", "delete a comment", "DELETE FROM fyniq.record_comment", "FY189"},
		{"fyniq_api", "change a comment as the API", "UPDATE fyniq.record_comment SET body = 'x'", "42501"},
		{"fyniq_api", "delete a comment as the API", "DELETE FROM fyniq.record_comment", "42501"},
		{"fyniq_worker", "write as the worker", insert + "'bill', gen_random_uuid(), author, 'x', now() FROM fyniq.record_comment LIMIT 1", "42501"},
		{"fyniq_api", "write an empty comment", insert + "'bill', gen_random_uuid(), author, '', now() FROM fyniq.record_comment LIMIT 1", "23514"},
		{"fyniq_api", "write a comment over 2,000 characters", insert + "'bill', gen_random_uuid(), author, repeat('x', 2001), now() FROM fyniq.record_comment LIMIT 1", "23514"},
		{"fyniq_api", "comment on a kind of record that takes none", insert + "'payslip', gen_random_uuid(), author, 'x', now() FROM fyniq.record_comment LIMIT 1", "23514"},
		{"fyniq_api", "write as someone who is no member", "INSERT INTO fyniq.record_comment (record_type, record_id, author, body, created_at) VALUES ('bill', gen_random_uuid(), gen_random_uuid(), 'x', now())", "23503"},
		{"fyniq_api", "mention eleven people", "INSERT INTO fyniq.record_comment (record_type, record_id, author, body, mentions, created_at) SELECT 'bill', gen_random_uuid(), author, 'x', ARRAY(SELECT gen_random_uuid() FROM generate_series(1, 11)), now() FROM fyniq.record_comment LIMIT 1", "23514"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.state)
	}
}
