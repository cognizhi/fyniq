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

	"github.com/cognizhi/fyniq/internal/clock"
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

// Only the link holds Appendix E's scopes (p1-payroll-link design D1): an integration client or a personal access token cannot be given one, and
// the link's client cannot be given an ordinary scope; the database refuses each, whatever the API asks.
// asvs: V8.2.1, V4.2.1
func TestAttackOnlyTheLinkHoldsTheLinkScopes(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Link scopes Pte Ltd")
	now := clock.System{}.Now()
	owner := founder(t, store(t, "fyniq_api"), b.Org, 91, now)
	insert := func(kind, class, scopes string) string {
		return "INSERT INTO fyniq.api_client (kind, client_id, name, class, owner_id, scopes, key_thumbprint, expires_at, approval_id, created_by, created_at) VALUES ('" +
			kind + "', gen_random_uuid()::text, 'x', '" + class + "', '" + owner.String() + "', " + scopes + ", repeat('t', 43), now() + interval '30 days', gen_random_uuid(), 'user:x', now())"
	}
	for _, x := range []struct{ what, stmt, state string }{
		{"an integration client with a link scope", insert("integration", "integration", "ARRAY['people.projection.write']"), "23514"},
		{"a personal token with a link scope", insert("personal", "agent", "ARRAY['payroll.journal.submit']"), "23514"},
		{"an integration client with the scope that ends sessions", insert("integration", "integration", "ARRAY['identity.sessions.write']"), "23514"},
		{"a personal token with the scope that ends sessions", insert("personal", "agent", "ARRAY['identity.sessions.write']"), "23514"},
		{"the link's client with the scope that ends sessions beside an ordinary one", insert("link", "integration", "ARRAY['identity.sessions.write', 'fyniq.write.sales']"), "23514"},
		{"the link's client with an ordinary scope", insert("link", "integration", "ARRAY['fyniq.read.accounting']"), "23514"},
		{"the link's client with an ordinary scope beside a link scope", insert("link", "integration", "ARRAY['people.holds.write', 'fyniq.write.sales']"), "23514"},
		{"the link's client with a scope nobody defined", insert("link", "integration", "ARRAY['people.everything']"), "23514"},
		{"the link's client as an agent", insert("link", "agent", "ARRAY['people.holds.write']"), "23514"},
		{"a client of a kind nobody defined", insert("service", "integration", "ARRAY['people.holds.write']"), "23514"},
	} {
		err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.state)
	}
}

// Fyniq's own credentials for the link are the database's to guard (p1-payroll-link design D1, p1-nemura-fyniq-link design D10; FR-PPL-15): the keys are made once with the link and never change, the
// sequence of what Fyniq sends only goes up by one, a key with a private part or of the wrong size is refused, the API writes the private halves and cannot read them back, the worker reads them and
// writes nothing, another organisation sees none of it, and the audit trail says that keys exist and holds none.
// asvs: V8.2.1, V6.4.1, V11.2.1, V13.2.1
func TestAttackTheLinksKeysAreKeptOnceAndOnlyTheWorkerReadsTheSecrets(t *testing.T) {
	ctx := context.Background()
	b := books(t, "People link keys Pte Ltd")
	other := books(t, "People link keys elsewhere Pte Ltd")
	now := time.Date(2026, time.November, 2, 3, 0, 0, 0, time.UTC)
	nemuraKey := make([]byte, 32)
	api := func(org uuid.UUID, fn func(u storage.UnitOfWork) error) error {
		return store(t, "fyniq_api").Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(u) })
	}
	var link storage.PeopleLinkRow
	if err := api(b.Org, func(u storage.UnitOfWork) (err error) {
		link, err = u.PeopleLinks().Link(ctx, storage.PeopleLinkRow{NemuraOrganisationID: uuid.New(), LinkKey: nemuraKey, ApprovalID: uuid.New(), CreatedBy: "user:" + uuid.NewString(), CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	pub, priv := make([]byte, 32), make([]byte, 64)
	for i := range pub {
		pub[i] = byte(i + 7)
	}
	keys := storage.PeopleLinkKeys{LinkID: link.ID, NemuraClientID: "fyniq-link-" + uuid.NewString(), LinkPublic: pub, ClientPublic: []byte(`{"kty":"EC","crv":"P-256","x":"a","y":"b"}`)}
	secrets := storage.PeopleLinkSecrets{LinkID: link.ID, LinkPrivate: priv, ClientPrivate: []byte("a-pkcs8-der-stand-in-of-some-length-0123456789")}
	if err := api(b.Org, func(u storage.UnitOfWork) error { return u.PeopleLinks().SaveKeys(ctx, keys, secrets) }); err != nil {
		t.Fatalf("saving the keys: %v", err)
	}
	if err := api(b.Org, func(u storage.UnitOfWork) error { return u.PeopleLinks().SaveKeys(ctx, keys, secrets) }); err == nil {
		t.Error("a link's keys were made twice")
	}
	var seq [3]int64
	for i := range seq {
		if err := api(b.Org, func(u storage.UnitOfWork) (err error) {
			seq[i], err = u.PeopleLinks().NextSequence(ctx, link.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if seq != [3]int64{1, 2, 3} {
		t.Errorf("the sequence: %v", seq)
	}
	if err := api(b.Org, func(u storage.UnitOfWork) error {
		got, err := u.PeopleLinks().Keys(ctx, link.ID)
		if err != nil || got.NemuraClientID != keys.NemuraClientID || string(got.LinkPublic) != string(pub) || got.Sequence != 3 {
			t.Errorf("the keys: %+v %v", got, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := api(b.Org, func(u storage.UnitOfWork) error {
		_, err := u.PeopleLinks().Secrets(ctx, link.ID)
		return err
	}); err == nil {
		t.Error("the API read the link's private keys")
	}
	if err := store(t, "fyniq_worker").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		got, err := u.PeopleLinks().Secrets(ctx, link.ID)
		if err != nil || len(got.LinkPrivate) != 64 || string(got.ClientPrivate) != string(secrets.ClientPrivate) {
			t.Errorf("the worker's secrets: %v", err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api(other.Org, func(u storage.UnitOfWork) error {
		if _, err := u.PeopleLinks().Keys(ctx, link.ID); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation reads the keys: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "delete the keys", "DELETE FROM fyniq.people_link_key", "FY900"},
		{"fyniq_migrator", "change the public key", "UPDATE fyniq.people_link_key SET link_public = decode(repeat('00', 32), 'hex')", "FY900"},
		{"fyniq_migrator", "change the client ID", "UPDATE fyniq.people_link_key SET nemura_client_id = 'someone-else'", "FY900"},
		{"fyniq_migrator", "skip a sequence number", "UPDATE fyniq.people_link_key SET sequence = sequence + 5", "FY900"},
		{"fyniq_migrator", "send the sequence back", "UPDATE fyniq.people_link_key SET sequence = 0", "FY900"},
		{"fyniq_api", "set the sequence as the API directly", "UPDATE fyniq.people_link_key SET link_public = link_public", "42501"},
		{"fyniq_migrator", "change a secret", "UPDATE fyniq.people_link_secret SET link_private = link_private", "FY"},
		{"fyniq_api", "read the secrets as the API", "SELECT link_private FROM fyniq.people_link_secret", "42501"},
		{"fyniq_readonly", "read the secrets as the read-only role", "SELECT link_private FROM fyniq.people_link_secret", "42501"},
		{"fyniq_worker", "write a secret as the worker", "UPDATE fyniq.people_link_secret SET link_private = link_private", "42501"},
		{"fyniq_worker", "write the keys as the worker", "UPDATE fyniq.people_link_key SET sequence = sequence + 1", "42501"},
		{"fyniq_api", "make a key with a private part", "INSERT INTO fyniq.people_link_key (link_id, nemura_client_id, link_public, client_public) VALUES (gen_random_uuid(), 'x', decode(repeat('01', 32), 'hex'), '{\"kty\":\"EC\",\"d\":\"secret\"}')", "23514"},
		{"fyniq_api", "make a public key of the wrong size", "INSERT INTO fyniq.people_link_key (link_id, nemura_client_id, link_public, client_public) VALUES (gen_random_uuid(), 'x', '\\x01', '{\"kty\":\"EC\"}')", "23514"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		if x.state == "FY" {
			if err == nil {
				t.Errorf("%s ran", x.what)
			}
			continue
		}
		wantState(t, x.what, err, x.state)
	}
	var trail string
	if err := asOrg(ctx, conn(t, "fyniq_readonly"), b.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT coalesce(string_agg(body::text, ' '), '') FROM fyniq.audit_event WHERE action = 'people_link.keys_made'").Scan(&trail)
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(trail, keys.NemuraClientID) || strings.Contains(trail, "kty") || strings.Contains(trail, "pkcs8") || strings.Contains(trail, "link_public") {
		t.Errorf("the audit trail of the keys: %q", trail)
	}
}
