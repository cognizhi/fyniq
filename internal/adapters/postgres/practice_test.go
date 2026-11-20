//go:build local

package postgres_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A practice organisation is the database's rule, not the API's alone (FR-UX-36; the owner's decision of 21 Nov 2026): the function that makes
// it refuses a person who is no Owner of the organisation it is opened from (FY713), one who holds a practice organisation already (FY712),
// and an opening from a practice organisation (FY711); two requests at once make one; the registry is read by no application role and only the
// API's role runs the function; and the new organisation holds the creator's membership and nothing of the source's.
// req: FR-UX-36
// req: FR-ADM-01
// asvs: V8.2.1, V8.3.1, V4.2.1
func TestAttackAPracticeOrganisationIsOpenedByOneOwnerAtATime(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "owner"}})
	s := store(t, "fyniq_api")
	b := books(t, "Practice source Pte Ltd")
	now := clock.System{}.Now()
	owner := founder(t, s, b.Org, 91, now)
	// An Accountant, who is no Owner.
	hash := make([]byte, 32)
	hash[0], hash[1] = 9, 92
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash, DisplayName: "Accountant", Roles: []string{"accountant"}, ExpiresAt: now.Add(time.Hour), CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	j, err := s.JoinOrganisation(ctx, storage.NewJoin{CodeHash: hash, DisplayName: "Accountant", Issuer: "urn:fyniq:attack", Subject: uuid.NewString(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	open := func(user uuid.UUID, from uuid.UUID) (storage.Practice, error) {
		var p storage.Practice
		err := s.Do(ctx, from, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			p, err = u.Practices().Open(ctx, storage.NewPractice{User: user, Name: "Practice: x", Currency: "SGD", Now: now})
			return err
		})
		return p, err
	}
	if _, err := open(j.UserID, b.Org); storage.CodeOf(err) != storage.CodePractice {
		t.Errorf("an Accountant opens one: %v", err)
	}
	if _, err := open(uuid.New(), b.Org); storage.CodeOf(err) != storage.CodePractice {
		t.Errorf("someone who is no member opens one: %v", err)
	}
	// Two requests at once make one.
	var wg sync.WaitGroup
	results := make([]error, 2)
	made := make([]storage.Practice, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			made[i], results[i] = open(owner, b.Org)
		}()
	}
	wg.Wait()
	ok := 0
	var practice storage.Practice
	for i, err := range results {
		if err == nil {
			ok++
			practice = made[i]
		} else if storage.CodeOf(err) != storage.CodePracticeHeld {
			t.Errorf("the request that lost: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d of two simultaneous requests made a practice organisation, want 1", ok)
	}
	if _, err := open(owner, b.Org); storage.CodeOf(err) != storage.CodePracticeHeld {
		t.Errorf("a second one: %v", err)
	}
	// From the practice organisation itself: none, and the creator is an Owner there.
	if _, err := open(owner, practice.OrganisationID); storage.CodeOf(err) != storage.CodePractice {
		t.Errorf("a practice organisation opens another: %v", err)
	}
	// What the new organisation holds: the creator's membership and Owner role, and none of the source's books.
	if n := count(t, practice.OrganisationID, "user_membership"); n != 1 {
		t.Errorf("the practice organisation has %d members, want its creator alone", n)
	}
	for _, table := range []string{"account", "document", "journal_entry", "contact"} {
		if n := count(t, practice.OrganisationID, table); n != 0 {
			t.Errorf("the practice organisation starts with %d rows of %s", n, table)
		}
	}
	if n := count(t, b.Org, "account"); n == 0 {
		t.Error("the source lost its chart")
	}
	// Only the API's role runs the function, and no application role reads or changes the registry.
	run := "SELECT organisation_id FROM fyniq.create_practice_organisation($1, $2, 'Practice: y', 'SGD', now())"
	for _, role := range []string{"fyniq_worker", "fyniq_readonly", "fyniq_executor"} {
		err := asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, run, owner, b.Org); return err })
		wantState(t, role+" runs it", err, "42501")
	}
	for _, role := range []string{"fyniq_api", "fyniq_worker", "fyniq_readonly", "fyniq_executor"} {
		err := asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, "SELECT * FROM fyniq.practice_organisation"); return err })
		wantState(t, role+" reads the registry", err, "42501")
		err = asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "DELETE FROM fyniq.practice_organisation")
			return err
		})
		wantState(t, role+" deletes from the registry", err, "42501")
	}
	// The function answers its own codes when called directly, with the same rules.
	err = asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, run, owner, b.Org); return err })
	wantState(t, "a second, called directly", err, "FY712")
	err = asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, run, j.UserID, b.Org); return err })
	wantState(t, "an Accountant, called directly", err, "FY713")
	if is := isPractice(t, b.Org); is {
		t.Error("the real organisation is a practice one")
	}
	if is := isPractice(t, practice.OrganisationID); !is {
		t.Error("the practice organisation is not marked")
	}
}

func isPractice(t *testing.T, org uuid.UUID) (is bool) {
	t.Helper()
	ctx := context.Background()
	if err := store(t, "fyniq_api").Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		is, err = u.Practices().IsPractice(ctx, org)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return is
}

// A person who is a member of two organisations works in the one they chose, and only one they are an active member of (FR-UX-36): the
// session lists their own memberships with the practice one marked, switches to the practice organisation where they are its Owner, comes back,
// and is refused another's organisation, whose existence it does not confirm; the choice is checked against the membership at every request.
// req: FR-UX-36
// req: SEC-34
// asvs: V8.2.1, V4.2.1
func TestAttackASessionChoosesOnlyAnOrganisationItsPersonBelongsTo(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "owner"}})
	s := store(t, "fyniq_api")
	b := books(t, "Switch source Pte Ltd")
	elsewhere := books(t, "Switch elsewhere Pte Ltd")
	now := clock.System{}.Now()
	hash := make([]byte, 32)
	hash[0], hash[1] = 9, 93
	issuer, subject := "urn:fyniq:attack", uuid.NewString()
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash, DisplayName: "Owner", Roles: []string{"owner"}, ExpiresAt: now.Add(time.Hour), CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	j, err := s.JoinOrganisation(ctx, storage.NewJoin{CodeHash: hash, DisplayName: "Owner", Issuer: issuer, Subject: subject, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	token := make([]byte, 32)
	token[0], token[1] = 7, 93
	if err := s.StartSession(ctx, storage.NewSession{TokenHash: token, Issuer: issuer, Subject: subject, Provider: "local", Now: now, Expires: now.Add(8 * time.Hour), StepUpAt: now}); err != nil {
		t.Fatal(err)
	}
	var practice storage.Practice
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		practice, err = u.Practices().Open(ctx, storage.NewPractice{User: j.UserID, Name: "Practice: x", Currency: "SGD", Now: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	inOrg := func() uuid.UUID {
		t.Helper()
		sess, err := s.Session(ctx, token, now, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return sess.Organisation
	}
	if inOrg() != b.Org {
		t.Fatal("a new practice organisation took the session from the real one")
	}
	orgs, err := s.OrganisationsOf(ctx, token, now)
	if err != nil || len(orgs) != 2 || orgs[0].ID != b.Org || orgs[0].Practice || orgs[1].ID != practice.OrganisationID || !orgs[1].Practice {
		t.Fatalf("the person's organisations: %+v %v", orgs, err)
	}
	if ok, err := s.SwitchOrganisation(ctx, token, practice.OrganisationID, now); err != nil || !ok || inOrg() != practice.OrganisationID {
		t.Fatalf("switching to the practice organisation: %v %v", ok, err)
	}
	if ok, err := s.SwitchOrganisation(ctx, token, elsewhere.Org, now); err != nil || ok || inOrg() != practice.OrganisationID {
		t.Errorf("switching to another's organisation: %v %v", ok, err)
	}
	if ok, err := s.SwitchOrganisation(ctx, token, uuid.New(), now); err != nil || ok {
		t.Errorf("switching to nothing: %v %v", ok, err)
	}
	if ok, err := s.SwitchOrganisation(ctx, make([]byte, 32), b.Org, now); err != nil || ok {
		t.Errorf("switching with no session: %v %v", ok, err)
	}
	if ok, err := s.SwitchOrganisation(ctx, token, b.Org, now); err != nil || !ok || inOrg() != b.Org {
		t.Errorf("switching back: %v %v", ok, err)
	}
}
