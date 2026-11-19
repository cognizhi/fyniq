//go:build local

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The link to Nemura, its inbox and the employee projection are the database's too (golden rules 2, 7 and 8; FR-PPL-06, 15, 16): a link is
// never deleted and its epochs only go up; unlinked is final; one live link per organisation; an event is received once; a person only moves
// to a newer version of each part; the inbox is append-only; and another organisation sees none of it.
// part of FR-PPL-16 (the link record; the requirement is tagged when unlinking settles work in flight)
// asvs: V8.2.1, V4.2.1
func TestAttackTheLinkKeepsItsEpochsAndItsTenant(t *testing.T) {
	ctx := context.Background()
	b := books(t, "People link Pte Ltd")
	other := books(t, "People link elsewhere Pte Ltd")
	now := time.Date(2026, time.November, 2, 3, 0, 0, 0, time.UTC)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	approval, nemura := uuid.New(), uuid.New()
	do := func(org uuid.UUID, fn func(u storage.UnitOfWork) error) error {
		return store(t, "fyniq_api").Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(u) })
	}
	var first storage.PeopleLinkRow
	if err := do(b.Org, func(u storage.UnitOfWork) (err error) {
		first, err = u.PeopleLinks().Link(ctx, storage.PeopleLinkRow{NemuraOrganisationID: nemura, LinkKey: key, ApprovalID: approval, CreatedBy: "user:" + uuid.NewString(), CreatedAt: now})
		return err
	}); err != nil || first.Epoch != 1 || first.State != storage.LinkActive || first.Version != 1 {
		t.Fatalf("the first link: %+v %v", first, err)
	}
	if err := do(b.Org, func(u storage.UnitOfWork) error {
		_, err := u.PeopleLinks().Link(ctx, storage.PeopleLinkRow{NemuraOrganisationID: nemura, LinkKey: key, ApprovalID: approval, CreatedBy: "user:x", CreatedAt: now})
		return err
	}); err == nil {
		t.Error("an organisation has one live link")
	}
	// active -> unlinked is not a move; active -> unlinking -> unlinked is.
	var err error
	if err = do(b.Org, func(u storage.UnitOfWork) error {
		_, err := u.PeopleLinks().SetState(ctx, first.ID, 1, storage.LinkUnlinked, uuid.Nil, now)
		return err
	}); err == nil {
		t.Error("a link cannot skip unlinking")
	}
	var stale *storage.Error
	if err = do(b.Org, func(u storage.UnitOfWork) error {
		_, err := u.PeopleLinks().SetState(ctx, first.ID, 7, storage.LinkSuspended, uuid.Nil, now)
		return err
	}); !errors.As(err, &stale) || stale.Code != storage.CodeVersion {
		t.Errorf("a stale version: %v", err)
	}
	for i, state := range []string{storage.LinkUnlinking, storage.LinkUnlinked} {
		if err := do(b.Org, func(u storage.UnitOfWork) error {
			_, err := u.PeopleLinks().SetState(ctx, first.ID, i+1, state, uuid.Nil, now)
			return err
		}); err != nil {
			t.Fatalf("%s: %v", state, err)
		}
	}
	if err := do(b.Org, func(u storage.UnitOfWork) error {
		if _, err := u.PeopleLinks().Live(ctx); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("nothing is live after unlinking: %v", err)
		}
		again, err := u.PeopleLinks().Link(ctx, storage.PeopleLinkRow{NemuraOrganisationID: nemura, LinkKey: key, ApprovalID: approval, CreatedBy: "user:x", CreatedAt: now})
		if err == nil && again.Epoch != 2 {
			t.Errorf("a later link is one epoch higher: %d", again.Epoch)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := do(other.Org, func(u storage.UnitOfWork) error {
		if _, err := u.PeopleLinks().Live(ctx); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation reads the link: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "delete a link", "DELETE FROM fyniq.people_link", "FY900"},
		{"fyniq_api", "delete a link as the API", "DELETE FROM fyniq.people_link", "42501"},
		{"fyniq_migrator", "change a link's key", "UPDATE fyniq.people_link SET link_key = decode(repeat('00', 32), 'hex')", "FY900"},
		{"fyniq_migrator", "change a link's epoch", "UPDATE fyniq.people_link SET epoch = epoch + 5", "FY900"},
		{"fyniq_api", "make a link with a key of the wrong size", "INSERT INTO fyniq.people_link (state, epoch, nemura_organisation_id, link_key, approval_id, created_by, created_at, changed_at) VALUES ('active', 3, gen_random_uuid(), '\\x00', gen_random_uuid(), 'user:x', now(), now())", "23514"},
		{"fyniq_worker", "make a link as the worker", "INSERT INTO fyniq.people_link (state, epoch, nemura_organisation_id, link_key, approval_id, created_by, created_at, changed_at) VALUES ('active', 3, gen_random_uuid(), decode(repeat('01', 32), 'hex'), gen_random_uuid(), 'user:x', now(), now())", "42501"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.state)
	}
}

// A person of the projection moves only to newer versions of each part, a removal is a tombstone, and every change is audited by field name,
// never by value: the inbox receives an event once and cannot be changed (FR-PPL-06, FR-PPL-15).
// part of FR-PPL-06 (the projection store; the requirement is tagged when events apply it through the API)
// asvs: V8.2.1, V4.2.1, V7.1.1
func TestAttackTheProjectionOnlyMovesForwardAndTheInboxReceivesOnce(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Projection Pte Ltd")
	now := time.Date(2026, time.November, 2, 3, 0, 0, 0, time.UTC)
	person, mgr1, mgr2 := uuid.New(), uuid.New(), uuid.New()
	do := func(fn func(u storage.UnitOfWork) error) error {
		return store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(u) })
	}
	var link storage.PeopleLinkRow
	if err := do(func(u storage.UnitOfWork) (err error) {
		link, err = u.PeopleLinks().Link(ctx, storage.PeopleLinkRow{NemuraOrganisationID: uuid.New(), LinkKey: make([]byte, 32), ApprovalID: uuid.New(), CreatedBy: "user:x", CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	apply := func(manager uuid.UUID, version int) (ignored bool) {
		t.Helper()
		if err := do(func(u storage.UnitOfWork) error {
			cur, err := u.PeopleLinks().Person(ctx, person)
			if err != nil {
				return err
			}
			next, ign, err := peoplelink.ApplyPerson(cur, peoplelink.PersonUpserted{PersonID: person, DisplayName: "Wei Jie Ong", WorkEmail: "weijie@example.com", Status: "active", LineManager: &manager}, version, now)
			ignored = ign
			if err != nil || ign {
				return err
			}
			return u.PeopleLinks().SavePerson(ctx, next)
		}); err != nil {
			t.Fatal(err)
		}
		return ignored
	}
	if apply(mgr1, 1) || apply(mgr2, 2) {
		t.Fatal("versions 1 and 2 apply")
	}
	if !apply(mgr1, 1) {
		t.Error("a late version 1 is ignored")
	}
	if err := do(func(u storage.UnitOfWork) error {
		p, err := u.PeopleLinks().Person(ctx, person)
		if err != nil || p.LineManager != mgr2 || p.Version != 2 || !p.Known() {
			t.Errorf("the manager is not set back: %+v %v", p, err)
		}
		people, err := u.PeopleLinks().People(ctx)
		if err != nil || len(people) != 1 {
			t.Errorf("%v %v", people, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The inbox receives an event once.
	event := storage.InboxEntry{EventID: uuid.New(), LinkID: link.ID, Epoch: 1, Sequence: 1, Type: peoplelink.TypePersonUpserted, ObjectType: "person", ObjectID: person.String(),
		ObjectVersion: 2, PayloadSHA256: "0000000000000000000000000000000000000000000000000000000000000000", Outcome: peoplelink.OutcomeApplied, ReceivedAt: now}
	var first, second bool
	if err := do(func(u storage.UnitOfWork) (err error) {
		if first, err = u.PeopleLinks().Receive(ctx, event); err != nil {
			return err
		}
		second, err = u.PeopleLinks().Receive(ctx, event)
		return err
	}); err != nil || !first || second {
		t.Fatalf("an event is received once: %v %v %v", first, second, err)
	}
	// The audit trail names the fields that changed and never their values.
	if n := count(t, b.Org, "person_projection"); n != 1 {
		t.Errorf("one person: %d", n)
	}
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "delete a person", "DELETE FROM fyniq.person_projection", "FY900"},
		{"fyniq_migrator", "move a person to an older version", "UPDATE fyniq.person_projection SET version = 1", "FY900"},
		{"fyniq_migrator", "move the hold back", "UPDATE fyniq.person_projection SET hold_version = -1", "FY900"},
		{"fyniq_migrator", "make a person their own manager", "UPDATE fyniq.person_projection SET line_manager_person_id = person_id", "23514"},
		{"fyniq_migrator", "hold a person with no hold", "UPDATE fyniq.person_projection SET held = true, hold_version = 9", "23514"},
		{"fyniq_migrator", "change an inbox row", "UPDATE fyniq.people_link_inbox SET outcome = 'refused'", "FY189"},
		{"fyniq_migrator", "delete an inbox row", "DELETE FROM fyniq.people_link_inbox", "FY189"},
		{"fyniq_api", "change an inbox row as the API", "UPDATE fyniq.people_link_inbox SET code = 'x'", "42501"},
		{"fyniq_worker", "write the projection as the worker", "UPDATE fyniq.person_projection SET status = 'active'", "42501"},
		{"fyniq_api", "keep an email that is no address", "UPDATE fyniq.person_projection SET work_email = 'not an address'", "23514"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.state)
	}
	var trail string
	if err := asOrg(ctx, conn(t, "fyniq_readonly"), b.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT coalesce(string_agg(body::text, ' '), '') FROM fyniq.audit_event WHERE object_type = 'person_projection'").Scan(&trail)
	}); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"Wei Jie", "weijie@example.com", mgr1.String(), mgr2.String()} {
		if strings.Contains(strings.ToLower(trail), strings.ToLower(secret)) {
			t.Errorf("the audit trail holds a person's value %q", secret)
		}
	}
	if !strings.Contains(trail, "line_manager") {
		t.Errorf("the audit trail names the field that changed: %s", trail)
	}
}
