package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Practices() storage.Practices { return practices{u} }

type practices struct{ u *unitOfWork }

// Open makes the organisation through the database's function, then writes the audit events of both organisations in this transaction:
// the new one's creation, membership and Owner role under its own tenant (as first-run setup does), and the opening in the source's trail.
func (r practices) Open(ctx context.Context, n storage.NewPractice) (storage.Practice, error) {
	if err := r.u.check(); err != nil {
		return storage.Practice{}, err
	}
	row, err := r.u.q.CreatePracticeOrganisation(ctx, sqlcdb.CreatePracticeOrganisationParams{UserID: n.User, Source: r.u.org, Name: n.Name, Currency: n.Currency, At: n.Now})
	if err != nil {
		return storage.Practice{}, mapError(err)
	}
	out := storage.Practice{OrganisationID: row.OrganisationID, MembershipID: row.MembershipID, AssignmentID: row.AssignmentID}
	// The new organisation's trail, under its own tenant.
	if err := r.u.store.setTenant(ctx, r.u.q, out.OrganisationID); err != nil {
		return storage.Practice{}, err
	}
	fresh := &unitOfWork{q: r.u.q, store: r.u.store, org: out.OrganisationID}
	version := 1
	for _, e := range []struct {
		action, objectType string
		id                 uuid.UUID
		version            *int
		changes            map[string][2]any
	}{
		{"organisation.created", "organisation", out.OrganisationID, &version, map[string][2]any{"name": {nil, n.Name}, "base_currency": {nil, n.Currency}, "practice": {nil, true},
			"opened_by": {nil, n.User}, "opened_from": {nil, r.u.org}}},
		{"user_membership.created", "user_membership", out.MembershipID, &version, map[string][2]any{"user_id": {nil, n.User}, "new_user": {nil, false}}},
		{"role_assignment.created", "role_assignment", out.AssignmentID, nil, map[string][2]any{"membership_id": {nil, out.MembershipID}, "role": {nil, "owner"}, "expires_at": {nil, nil}}},
	} {
		if err := fresh.event(ctx, e.action, e.objectType, e.id, e.version, e.changes); err != nil {
			return storage.Practice{}, err
		}
	}
	if err := fresh.flush(ctx); err != nil {
		return storage.Practice{}, err
	}
	// Back to the source organisation, whose trail records who opened one.
	if err := r.u.store.setTenant(ctx, r.u.q, r.u.org); err != nil {
		return storage.Practice{}, err
	}
	err = r.u.event(ctx, "practice_organisation.opened", "organisation", out.OrganisationID, nil, map[string][2]any{
		"practice_organisation_id": {nil, out.OrganisationID}, "opened_by": {nil, n.User}, "name": {nil, n.Name}})
	return out, err
}

func (r practices) IsPractice(ctx context.Context, org uuid.UUID) (bool, error) {
	if err := r.u.check(); err != nil {
		return false, err
	}
	is, err := r.u.q.OrganisationIsPractice(ctx, org)
	return is, mapError(err)
}

// OrganisationsOf implements storage.SessionOrganisations.
func (s *Store) OrganisationsOf(ctx context.Context, tokenHash []byte, now time.Time) ([]storage.SessionOrganisation, error) {
	rows, err := s.q().OrganisationsOfSession(ctx, sqlcdb.OrganisationsOfSessionParams{TokenHash: tokenHash, Now: now})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SessionOrganisation, len(rows))
	for i, r := range rows {
		out[i] = storage.SessionOrganisation{ID: r.OrganisationID, Name: r.Name, Practice: r.Practice, Chosen: r.Chosen}
	}
	return out, nil
}

// SwitchOrganisation implements storage.SessionOrganisations.
func (s *Store) SwitchOrganisation(ctx context.Context, tokenHash []byte, org uuid.UUID, now time.Time) (bool, error) {
	ok, err := s.q().SwitchSessionOrganisation(ctx, sqlcdb.SwitchSessionOrganisationParams{TokenHash: tokenHash, Org: org, Now: now})
	return ok, mapError(err)
}
