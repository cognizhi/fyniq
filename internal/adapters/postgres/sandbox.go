package postgres

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Sandboxes() storage.Sandboxes { return sandboxes{u} }

type sandboxes struct{ u *unitOfWork }

// known remembers what the registry said of an organisation: an organisation is a sandbox from the transaction that makes it and for ever,
// or never, so the answer is read once for each organisation of this process (the registry refuses every change, FY960).
var known sync.Map

// Create makes the organisation through the database's function and writes the audit events of both organisations in this transaction: the
// new one's creation, membership and Owner role under its own tenant, and the creation in the installation's trail.
func (r sandboxes) Create(ctx context.Context, n storage.NewSandbox) (storage.Sandbox, error) {
	if err := (superAdmins{r.u}).installation(ctx); err != nil {
		return storage.Sandbox{}, err
	}
	row, err := r.u.q.CreateSandboxOrganisation(ctx, sqlcdb.CreateSandboxOrganisationParams{ByUser: n.By, ChangeID: n.Change, Organisation: n.ID, Name: n.Name, At: n.At})
	if err != nil {
		return storage.Sandbox{}, mapError(err)
	}
	out := storage.Sandbox{OrganisationID: n.ID, MembershipID: row.MembershipID, AssignmentID: row.AssignmentID}
	if err := r.u.store.setTenant(ctx, r.u.q, n.ID); err != nil {
		return storage.Sandbox{}, err
	}
	fresh := &unitOfWork{q: r.u.q, store: r.u.store, org: n.ID}
	version := 1
	for _, e := range []struct {
		action, objectType string
		id                 uuid.UUID
		version            *int
		changes            map[string][2]any
	}{
		{"organisation.created", "organisation", n.ID, &version, map[string][2]any{"name": {nil, n.Name}, "base_currency": {nil, "SGD"}, "sandbox": {nil, true}, "created_by": {nil, n.By}}},
		{"user_membership.created", "user_membership", out.MembershipID, &version, map[string][2]any{"user_id": {nil, n.By}, "new_user": {nil, false}}},
		{"role_assignment.created", "role_assignment", out.AssignmentID, nil, map[string][2]any{"membership_id": {nil, out.MembershipID}, "role": {nil, "owner"}, "expires_at": {nil, nil}}},
	} {
		if err := fresh.event(as(ctx, n.By), e.action, e.objectType, e.id, e.version, e.changes); err != nil {
			return storage.Sandbox{}, err
		}
	}
	if err := fresh.flush(ctx); err != nil {
		return storage.Sandbox{}, err
	}
	if err := r.u.store.setTenant(ctx, r.u.q, r.u.org); err != nil {
		return storage.Sandbox{}, err
	}
	known.Store(n.ID, true)
	return out, r.u.event(as(ctx, n.By), "sandbox_organisation.created", "organisation", n.ID, nil, map[string][2]any{
		"sandbox_organisation_id": {nil, n.ID}, "created_by": {nil, n.By}, "name": {nil, n.Name}})
}

func (r sandboxes) Is(ctx context.Context) (bool, error) {
	if err := r.u.check(); err != nil {
		return false, err
	}
	if v, ok := known.Load(r.u.org); ok {
		return v.(bool), nil
	}
	is, err := r.u.q.OrganisationIsSandbox(ctx, r.u.org)
	if err != nil {
		return false, mapError(err)
	}
	known.Store(r.u.org, is)
	return is, nil
}
