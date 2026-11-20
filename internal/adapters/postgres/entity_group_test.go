//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// groupFixture is a parent organisation, an entity and an outsider, each with an Owner of their own.
type groupFixture struct {
	s                                  *postgres.Store
	parent, entity, outsider           porttest.Books
	parentOwner, entityOwner, outOwner uuid.UUID
	now                                time.Time
}

func newGroupFixture(t *testing.T, name string, n byte) groupFixture {
	t.Helper()
	s := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	f := groupFixture{s: s, now: now,
		parent: books(t, name+" parent Pte Ltd"), entity: books(t, name+" entity Sdn Bhd"), outsider: books(t, name+" outsider Pte Ltd")}
	f.parentOwner = founder(t, s, f.parent.Org, n, now)
	f.entityOwner = founder(t, s, f.entity.Org, n+1, now)
	f.outOwner = founder(t, s, f.outsider.Org, n+2, now)
	return f
}

func (f groupFixture) invite(t *testing.T, label string, code byte, by uuid.UUID, org uuid.UUID) ([]byte, storage.GroupEntity, error) {
	t.Helper()
	h := sha256.Sum256([]byte{code, 7, org[15]})
	var e storage.GroupEntity
	err := f.s.Do(storageCtx(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		e, err = u.EntityGroups().Invite(ctx, storage.NewGroupInvitation{Label: label, CodeHash: h[:], ExpiresAt: f.now.Add(time.Hour), By: by, At: f.now})
		return err
	})
	return h[:], e, err
}

func (f groupFixture) accept(org uuid.UUID, hash []byte, by uuid.UUID, at time.Time) (storage.GroupMembership, error) {
	var m storage.GroupMembership
	err := f.s.Do(storageCtx(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		m, err = u.EntityGroups().Accept(ctx, hash, by, at)
		return err
	})
	return m, err
}

func (f groupFixture) figures(org, entity uuid.UUID) (storage.EntityFigures, error) {
	var out storage.EntityFigures
	err := f.s.Do(storageCtx(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		out, err = u.EntityGroups().Figures(ctx, entity, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), f.parentOwner)
		return err
	})
	return out, err
}

func (f groupFixture) end(org, other uuid.UUID, by uuid.UUID, reason string) (storage.EndedLink, error) {
	var out storage.EndedLink
	err := f.s.Do(storageCtx(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		out, err = u.EntityGroups().End(ctx, by, other, reason, f.now.Add(time.Minute))
		return err
	})
	return out, err
}

func storageCtx() context.Context {
	return storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "owner"}})
}

// A link of a group needs both Owners and is read-only and narrow (FR-ENT-04; migration 001350). Only an Owner of the parent invites; a code that is wrong, used, expired or aimed
// at another organisation fails with the one answer a guess learns nothing from; only an Owner of the entity accepts, and not the parent itself; the entity is then in the
// parent's list as active and the parent in the entity's consent row; the parent reads the entity's figures and nothing else of it, an outsider and the entity itself
// reading the other way read none, and a read after either Owner ended the link is refused on the next call.
// req: FR-ENT-04
// asvs: V8.2.1, V8.3.1, V4.2.1, V2.3.1
func TestAttackAGroupLinkNeedsBothOwnersAndReadsOnlyWhileItStands(t *testing.T) {
	f := newGroupFixture(t, "Group link", 40)
	// A person who is no Owner invites nobody.
	if _, _, err := f.invite(t, "Subsidiary", 1, f.outOwner, f.parent.Org); storage.CodeOf(err) != storage.CodeGroup {
		t.Errorf("a stranger invites: %v", err)
	}
	hash, row, err := f.invite(t, "Subsidiary", 2, f.parentOwner, f.parent.Org)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != "invited" {
		t.Errorf("the new row is %s", row.State)
	}
	// Before acceptance the parent reads nothing.
	if _, err := f.figures(f.parent.Org, f.entity.Org); storage.CodeOf(err) != storage.CodeGroupLink {
		t.Errorf("a read before the entity accepted: %v", err)
	}
	// A wrong code, a stranger, the parent itself and an outsider's Owner all fail; none of them uses the code.
	wrong := sha256.Sum256([]byte("wrong"))
	if _, err := f.accept(f.entity.Org, wrong[:], f.entityOwner, f.now); storage.CodeOf(err) != storage.CodeGroupCode {
		t.Errorf("a wrong code: %v", err)
	}
	if _, err := f.accept(f.entity.Org, hash, uuid.New(), f.now); storage.CodeOf(err) != storage.CodeGroup {
		t.Errorf("a person who is no Owner of the entity accepts: %v", err)
	}
	if _, err := f.accept(f.parent.Org, hash, f.parentOwner, f.now); storage.CodeOf(err) != storage.CodeGroup {
		t.Errorf("the parent accepts its own invitation: %v", err)
	}
	if _, err := f.accept(f.entity.Org, hash, f.entityOwner, f.now.Add(25*time.Hour)); storage.CodeOf(err) != storage.CodeGroupCode {
		t.Errorf("an expired code: %v", err)
	}
	// The entity's Owner accepts once.
	m, err := f.accept(f.entity.Org, hash, f.entityOwner, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if m.ParentOrg != f.parent.Org || m.State != "active" || len(m.Scopes) != 2 {
		t.Errorf("the consent row: %+v", m)
	}
	if _, err := f.accept(f.entity.Org, hash, f.entityOwner, f.now); storage.CodeOf(err) != storage.CodeGroupCode {
		t.Errorf("a used code: %v", err)
	}
	if _, err := f.accept(f.outsider.Org, hash, f.outOwner, f.now); storage.CodeOf(err) != storage.CodeGroupCode {
		t.Errorf("an outsider uses the used code: %v", err)
	}
	// The parent's list shows the entity as active; the entity reads none of the parent's rows.
	_ = f.s.Do(storageCtx(), f.parent.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		es, err := u.EntityGroups().Entities(ctx)
		if err != nil || len(es) != 1 || es[0].State != "active" || es[0].EntityOrg != f.entity.Org || es[0].EntityCurrency != "SGD" {
			t.Errorf("the parent's list: %+v (%v)", es, err)
		}
		return nil
	})
	_ = f.s.Do(storageCtx(), f.entity.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		es, err := u.EntityGroups().Entities(ctx)
		if err != nil || len(es) != 0 {
			t.Errorf("the entity reads the parent's rows: %+v (%v)", es, err)
		}
		return nil
	})
	// The figures: the entity's own movements through November, and only those.
	postRaw(t, conn(t, "fyniq_api"), f.entity, "group-figures", "2026-10-14", "250.00")
	got, err := f.figures(f.parent.Org, f.entity.Org)
	if err != nil {
		t.Fatal(err)
	}
	if got.Currency != "SGD" || len(got.Lines) != 2 || len(got.Periods) == 0 || got.Revision < 1 {
		t.Fatalf("figures: %+v", got)
	}
	var sum string
	for _, l := range got.Lines {
		if l.Month != time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) {
			t.Errorf("the month of %s is %s", l.Code, l.Month)
		}
		sum += string(l.Net) + " "
	}
	if sum != "250.00 -250.00 " && sum != "-250.00 250.00 " && sum != "250.0000 -250.0000 " && sum != "-250.0000 250.0000 " {
		t.Errorf("the net movements: %s", sum)
	}
	// The wrong direction and the wrong asker read nothing.
	if _, err := f.figures(f.entity.Org, f.parent.Org); storage.CodeOf(err) != storage.CodeGroupLink {
		t.Errorf("the entity reads the parent: %v", err)
	}
	if _, err := f.figures(f.outsider.Org, f.entity.Org); storage.CodeOf(err) != storage.CodeGroupLink {
		t.Errorf("an outsider reads the entity: %v", err)
	}
	if _, err := f.figures(f.parent.Org, f.outsider.Org); storage.CodeOf(err) != storage.CodeGroupLink {
		t.Errorf("the parent reads an organisation that is no entity of its group: %v", err)
	}
	// Both trails record the read.
	for org, action := range map[uuid.UUID]string{f.parent.Org: "group.figures_read", f.entity.Org: "group.figures_read_by_parent"} {
		if n := countAudit(t, org, action); n != 1 {
			t.Errorf("the %s events of %s: %d", action, org, n)
		}
	}
	// A stranger ends nothing; the entity's Owner ends it, with a reason, and the parent's next read is refused.
	if _, err := f.end(f.entity.Org, f.parent.Org, uuid.New(), "no"); storage.CodeOf(err) != storage.CodeGroup {
		t.Errorf("a stranger ends the link: %v", err)
	}
	if _, err := f.end(f.entity.Org, f.parent.Org, f.entityOwner, " "); storage.CodeOf(err) != storage.CodeGroup {
		t.Errorf("an end without a reason: %v", err)
	}
	if _, err := f.end(f.outsider.Org, f.parent.Org, f.outOwner, "mine"); storage.CodeOf(err) != storage.CodeGroupLink {
		t.Errorf("an outsider ends the link: %v", err)
	}
	ended, err := f.end(f.entity.Org, f.parent.Org, f.entityOwner, "We left the group")
	if err != nil || ended.Role != "entity" {
		t.Fatalf("the entity ends it: %+v %v", ended, err)
	}
	if _, err := f.figures(f.parent.Org, f.entity.Org); storage.CodeOf(err) != storage.CodeGroupLink {
		t.Errorf("a read after the entity ended the link: %v", err)
	}
	if _, err := f.end(f.parent.Org, f.entity.Org, f.parentOwner, "again"); storage.CodeOf(err) != storage.CodeGroupLink {
		t.Errorf("ending an ended link: %v", err)
	}
	// Both rows are ended, and ended is final.
	_ = f.s.Do(storageCtx(), f.parent.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		es, _ := u.EntityGroups().Entities(ctx)
		if len(es) != 1 || es[0].State != "ended" || es[0].EndedReason != "We left the group" {
			t.Errorf("the parent's row after the entity left: %+v", es)
		}
		return nil
	})
	// A second link, ended by the parent this time: the next read of the entity is refused as well.
	hash2, _, err := f.invite(t, "Subsidiary again", 3, f.parentOwner, f.parent.Org)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.accept(f.entity.Org, hash2, f.entityOwner, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.figures(f.parent.Org, f.entity.Org); err != nil {
		t.Errorf("the new link reads nothing: %v", err)
	}
	if e, err := f.end(f.parent.Org, f.entity.Org, f.parentOwner, "Sold"); err != nil || e.Role != "parent" {
		t.Fatalf("the parent ends it: %+v %v", e, err)
	}
	if _, err := f.figures(f.parent.Org, f.entity.Org); storage.CodeOf(err) != storage.CodeGroupLink {
		t.Errorf("a read after the parent ended the link: %v", err)
	}
}

// countAudit counts an organisation's audit events of an action, as the read-only role sees its own tenant.
func countAudit(t *testing.T, org uuid.UUID, action string) int {
	t.Helper()
	ctx := context.Background()
	var n int
	err := asOrg(ctx, conn(t, "fyniq_readonly"), org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM fyniq.audit_event WHERE action = $1", action).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A group is a tree of one level and each organisation is in one group at a time: an entity is not a parent, a parent is not an entity, and an entity joins no second group; a
// practice organisation holds no group (FR-ENT-04; FYE02, FYE03, FYE07).
// req: FR-ENT-04
// asvs: V8.2.1, V4.2.1
func TestAttackAGroupHasOneLevelAndAnOrganisationOneGroup(t *testing.T) {
	f := newGroupFixture(t, "Group shape", 50)
	hash, _, err := f.invite(t, "Entity", 1, f.parentOwner, f.parent.Org)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.accept(f.entity.Org, hash, f.entityOwner, f.now); err != nil {
		t.Fatal(err)
	}
	// The entity invites nobody (it is not a parent) and the parent joins nobody's group (it is a parent).
	if _, _, err := f.invite(t, "Grandchild", 2, f.entityOwner, f.entity.Org); storage.CodeOf(err) != storage.CodeGroup {
		t.Errorf("an entity invites: %v", err)
	}
	other, _, err := f.invite(t, "Outsider as entity", 3, f.outOwner, f.outsider.Org)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.accept(f.parent.Org, other, f.parentOwner, f.now); storage.CodeOf(err) != storage.CodeGroup {
		t.Errorf("a parent joins a group: %v", err)
	}
	// An entity of one group accepts no second invitation.
	third := books(t, "Group shape third Pte Ltd")
	thirdOwner := founder(t, f.s, third.Org, 60, f.now)
	h3, _, err := f.invite(t, "Another", 4, thirdOwner, third.Org)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.accept(f.entity.Org, h3, f.entityOwner, f.now); storage.CodeOf(err) != storage.CodeGroup {
		t.Errorf("an entity joins a second group: %v", err)
	}
	// A practice organisation holds no group.
	var practice storage.Practice
	if err := f.s.Do(storageCtx(), f.outsider.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		practice, err = u.Practices().Open(ctx, storage.NewPractice{User: f.outOwner, Name: "Practice: x", Currency: "SGD", Now: f.now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.invite(t, "In practice", 5, f.outOwner, practice.OrganisationID); storage.CodeOf(err) != storage.CodeGroup {
		t.Errorf("a practice organisation invites: %v", err)
	}
}

// Nothing of the link can be reached around the functions: the API's role cannot change a link's row, insert a consent row or read the registry; the worker and the read-only role
// cannot run any of the functions; the functions take the asker from the transaction, so an organisation that is not the parent reads nothing with the parent's ID in the
// arguments; and a tenant reads none of another's link rows (FR-ENT-04; SEC-02).
// req: FR-ENT-04
// req: SEC-02
// asvs: V8.2.1, V8.3.1, V4.2.1, V1.4.1
func TestAttackAGroupLinkIsReachedOnlyThroughItsFunctions(t *testing.T) {
	ctx := context.Background()
	f := newGroupFixture(t, "Group raw", 70)
	hash, row, err := f.invite(t, "Entity", 1, f.parentOwner, f.parent.Org)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.accept(f.entity.Org, hash, f.entityOwner, f.now); err != nil {
		t.Fatal(err)
	}
	api := conn(t, "fyniq_api")
	// No role writes the rows by hand: the state is the functions'.
	wantState(t, "api updates a link row", asOrg(ctx, api, f.parent.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE fyniq.group_entity SET state = 'ended' WHERE id = $1", row.ID)
		return err
	}), "42501")
	wantState(t, "api deletes a link row", asOrg(ctx, api, f.parent.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM fyniq.group_entity WHERE id = $1", row.ID)
		return err
	}), "42501")
	wantState(t, "api inserts a consent row for itself", asOrg(ctx, api, f.outsider.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO fyniq.group_membership (parent_org, parent_name, parent_row, accepted_by, accepted_at) VALUES ($1, 'x', $2, $3, now())", f.parent.Org, row.ID, f.outOwner)
		return err
	}), "42501")
	wantState(t, "api reads the registry", asOrg(ctx, api, f.parent.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT * FROM fyniq.group_invitation")
		return err
	}), "42501")
	wantState(t, "api invites a row naming itself as the entity", asOrg(ctx, api, f.parent.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO fyniq.group_entity (label, invited_by, invited_at, entity_org, entity_name, entity_currency, state) VALUES ('x', $1, now(), $2, 'x', 'SGD', 'active')", f.parentOwner, f.entity.Org)
		return err
	}), "42501")
	// The other roles run no function of the link.
	for _, role := range []string{"fyniq_worker", "fyniq_readonly", "fyniq_executor"} {
		c := conn(t, role)
		wantState(t, role+" reads figures", asOrg(ctx, c, f.parent.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "SELECT * FROM fyniq.group_entity_figures($1, '2026-12-31')", f.entity.Org)
			return err
		}), "42501")
		wantState(t, role+" accepts", asOrg(ctx, c, f.entity.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "SELECT * FROM fyniq.accept_group_invitation($1, $2, now())", hash, f.entityOwner)
			return err
		}), "42501")
	}
	// The asker is the transaction's organisation: the outsider, with the parent's ID in the arguments, reads nothing; so does the entity reading the parent.
	for name, org := range map[string]uuid.UUID{"the outsider": f.outsider.Org, "the entity": f.entity.Org} {
		wantState(t, name+" reads the entity's figures", asOrg(ctx, api, org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "SELECT * FROM fyniq.group_entity_figures($1, '2026-12-31')", f.entity.Org)
			return err
		}), "FYE10")
	}
	// A tenant reads none of another's link rows.
	for _, org := range []uuid.UUID{f.outsider.Org, f.entity.Org} {
		var n int
		if err := asOrg(ctx, api, org, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT count(*) FROM fyniq.group_entity").Scan(&n)
		}); err != nil || n != 0 {
			t.Errorf("%s reads %d of the parent's link rows (%v)", org, n, err)
		}
	}
	// The figures function returns the grant's six columns and no others.
	var cols int
	if err := asOrg(ctx, api, f.parent.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM pg_proc p, unnest(p.proargnames) n WHERE p.proname = 'group_entity_figures'").Scan(&cols)
	}); err != nil || cols != 8 { // two arguments and six result columns
		t.Errorf("group_entity_figures names %d parameters and columns: the grant widened? (%v)", cols, err)
	}
}
