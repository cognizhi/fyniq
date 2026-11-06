//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// An Owner succession is the database's too (FY268; PRD-001 FR-ADM-09, rev 0.181): it starts in notice, by an active
// Owner, of another active Owner, on its own document, one at a time; it ends once: objected only by the outgoing
// Owner, withdrawn only by the one who started it, completed only by a Super admin from its due time with a file
// attached; nothing else of it changes, and it never goes. Another organisation sees none.
// req: FR-ADM-09
// asvs: V8.2.2, V16.3.3
func TestAttackAnOwnerSuccessionKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Attack succession A Pte Ltd"), books(t, "Attack succession B Pte Ltd")
	st := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	ann, ben, cat := founder(t, st, a.Org, 1, now), founder(t, st, a.Org, 2, now), founder(t, st, a.Org, 3, now)
	membership := func(user uuid.UUID) uuid.UUID {
		var m storage.Membership
		if err := st.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			m, err = u.Members().Membership(ctx, user)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	as := func(who uuid.UUID) context.Context {
		return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: who.String()}})
	}
	doc := func() uuid.UUID {
		var d storage.Document
		if err := st.Do(as(ann), a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			body, _ := json.Marshal(map[string]string{"for": "test"})
			d, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentOwnerSuccession, Payload: body})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return d.ID
	}
	start := func(n storage.NewOwnerSuccession) (storage.OwnerSuccession, error) {
		var s storage.OwnerSuccession
		err := st.Do(as(n.StartedBy), a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			s, err = u.OwnerSuccessions().Start(ctx, n)
			return err
		})
		return s, err
	}
	end := func(s storage.OwnerSuccession, status string, by uuid.UUID, at time.Time) (storage.OwnerSuccession, error) {
		var out storage.OwnerSuccession
		err := st.Do(as(by), a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.OwnerSuccessions().End(ctx, s.ID, s.Version, status, by, at)
			return err
		})
		return out, err
	}
	ok := storage.NewOwnerSuccession{DocumentID: doc(), OutgoingMembershipID: membership(ben), OutgoingUserID: ben, Reason: "Ben left",
		EvidenceKind: "board_resolution", StartedBy: ann, StartedAt: now}
	self, other, notDoc := ok, ok, ok
	self.StartedBy = ben
	other.OutgoingUserID = cat // another user than the membership's
	notDoc.DocumentID = a.Rent.ID
	for name, n := range map[string]storage.NewOwnerSuccession{"by the outgoing Owner": self, "a user who is not the membership's": other,
		"on no succession document": notDoc} {
		if _, err := start(n); sqlstate(err) != "FY268" && sqlstate(err) != "23503" {
			t.Errorf("a succession %s: %v", name, err)
		}
	}
	s, err := start(ok)
	if err != nil {
		t.Fatalf("the succession: %v", err)
	}
	if _, err := start(storage.NewOwnerSuccession{DocumentID: doc(), OutgoingMembershipID: membership(ben), OutgoingUserID: ben, Reason: "Again",
		EvidenceKind: "board_resolution", StartedBy: cat, StartedAt: now}); sqlstate(err) != "23505" {
		t.Errorf("a second open succession of Ben: %v", err)
	}
	_, err = end(s, storage.SuccessionObjected, cat, now)
	wantState(t, "an objection by someone other than the outgoing Owner", err, "FY268")
	_, err = end(s, storage.SuccessionWithdrawn, ben, now)
	wantState(t, "a withdrawal by someone other than its starter", err, "FY268")
	_, err = end(s, storage.SuccessionCompleted, ann, now.Add(8*24*time.Hour))
	wantState(t, "a completion by someone who is no Super admin", err, "FY268")
	makeSuperAdmin(t, ann)
	_, err = end(s, storage.SuccessionCompleted, ann, now.Add(6*24*time.Hour))
	wantState(t, "a completion before its due time", err, "FY268")
	_, err = end(s, storage.SuccessionCompleted, ann, now.Add(8*24*time.Hour))
	wantState(t, "a completion with no file attached", err, "FY268")
	if b2 := count(t, b.Org, "owner_succession"); b2 != 0 {
		t.Errorf("another organisation sees %d successions", b2)
	}
	objected, err := end(s, storage.SuccessionObjected, ben, now.Add(time.Hour))
	if err != nil || objected.Status != storage.SuccessionObjected {
		t.Fatalf("Ben objecting: %+v %v", objected, err)
	}
	_, err = end(objected, storage.SuccessionWithdrawn, ann, now.Add(2*time.Hour))
	if storage.CodeOf(err) != storage.CodeVersion && sqlstate(err) != "FY268" {
		t.Errorf("an ended succession ending again: %v", err)
	}

	// The table's owner neither changes nor deletes it.
	owner := conn(t, "fyniq")
	for _, stmt := range []string{"UPDATE fyniq.owner_succession SET reason = 'Changed'", "DELETE FROM fyniq.owner_succession"} {
		tx, err := owner.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", a.Org.String())
		if err == nil {
			_, err = tx.Exec(ctx, stmt)
		}
		_ = tx.Rollback(ctx)
		wantState(t, "the owner: "+stmt, err, "FY268")
	}
}
