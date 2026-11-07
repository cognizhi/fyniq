package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Joining by email domain (FR-ADM-08, rev 0.199; migration 000244): the session's verified email, the organisation a
// person may ask to join, their request (audited in that organisation's trail with the person as an applicant), and the
// Owners' side: the requests, their decisions and the claimed domains.

// RecordSessionEmail implements storage.Sessions.
func (s *Store) RecordSessionEmail(ctx context.Context, tokenHash []byte, email string) error {
	return mapError(s.q().SetSessionEmail(ctx, sqlcdb.SetSessionEmailParams{TokenHash: tokenHash, Email: email}))
}

// JoinOffer implements storage.Joining.
func (s *Store) JoinOffer(ctx context.Context, sessionHash []byte, now time.Time) (storage.JoinOffer, bool, error) {
	rows, err := s.q().JoinOffer(ctx, sqlcdb.JoinOfferParams{SessionHash: sessionHash, Now: now})
	if err != nil || len(rows) == 0 {
		return storage.JoinOffer{}, false, mapError(err)
	}
	r := rows[0]
	name, err := s.q().JoinOfferName(ctx, r.OrganisationID)
	if err != nil {
		return storage.JoinOffer{}, false, mapError(err)
	}
	return storage.JoinOffer{Organisation: r.OrganisationID, Name: name, Domain: r.Domain, Email: r.Email, Provider: r.Provider}, true, nil
}

// RequestToJoin implements storage.Joining: one transaction that records the request in the organisation that takes it
// and audits it there, with the person as the applicant.
func (s *Store) RequestToJoin(ctx context.Context, sessionHash []byte, name string, now time.Time) (storage.JoinRequest, error) {
	var out storage.JoinRequest
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlcdb.Queries) error {
		row, err := q.RequestToJoin(ctx, sqlcdb.RequestToJoinParams{SessionHash: sessionHash, Name: name, Now: now})
		if err != nil {
			return mapError(err)
		}
		if err := s.setTenant(ctx, q, row.OrganisationID); err != nil {
			return err
		}
		all, err := q.JoinRequests(ctx)
		if err != nil {
			return mapError(err)
		}
		for _, r := range all {
			if r.ID == row.RequestID {
				out = toJoinRequest(r)
			}
		}
		a := storage.AuditFrom(ctx)
		a.Actor, a.OnBehalfOf = &storage.Actor{Type: "applicant", ID: row.RequestID.String()}, nil
		actx := storage.WithAudit(ctx, a)
		u := &unitOfWork{q: q, store: s, org: row.OrganisationID}
		if err := u.event(actx, "join_request.created", "join_request", row.RequestID, nil, map[string][2]any{
			"display_name": {nil, out.Name}, "email": {nil, out.Email}, "provider": {nil, out.Provider}}); err != nil {
			return err
		}
		return u.flush(actx)
	})
	return out, err
}

func toJoinRequest(r sqlcdb.FyniqJoinRequest) storage.JoinRequest {
	return storage.JoinRequest{ID: r.ID, Organisation: r.OrganisationID, Provider: r.Provider, Email: r.Email, Name: r.DisplayName,
		RequestedAt: r.RequestedAt, Status: r.Status, DecidedBy: r.DecidedBy, DecidedAt: r.DecidedAt, UserID: r.UserID}
}

func (u *unitOfWork) JoinRequests() storage.JoinRequests { return joinRequests{u} }

type joinRequests struct{ u *unitOfWork }

func (r joinRequests) List(ctx context.Context) ([]storage.JoinRequest, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.JoinRequests(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.JoinRequest, 0, len(rows))
	for _, x := range rows {
		out = append(out, toJoinRequest(x))
	}
	return out, nil
}

func (r joinRequests) Decide(ctx context.Context, by, id uuid.UUID, approve bool, now time.Time) (storage.JoinRequest, error) {
	if err := r.u.check(); err != nil {
		return storage.JoinRequest{}, err
	}
	row, err := r.u.q.DecideJoinRequest(ctx, sqlcdb.DecideJoinRequestParams{ByUser: by, ID: id, Approve: approve, Now: now})
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "P0002" { // no pending request with that ID
			return storage.JoinRequest{}, ports.ErrNotFound
		}
		return storage.JoinRequest{}, mapError(err)
	}
	all, err := r.List(ctx)
	if err != nil {
		return storage.JoinRequest{}, err
	}
	var out storage.JoinRequest
	for _, x := range all {
		if x.ID == id {
			out = x
		}
	}
	if !approve {
		return out, r.u.event(ctx, "join_request.rejected", "join_request", id, nil, map[string][2]any{"status": {"pending", "rejected"}})
	}
	if err := r.u.event(ctx, "join_request.approved", "join_request", id, nil, map[string][2]any{"status": {"pending", "approved"},
		"user_id": {nil, row.UserID}}); err != nil {
		return out, err
	}
	version := 1
	if err := r.u.event(ctx, "user_membership.created", "user_membership", row.MembershipID, &version, map[string][2]any{
		"user_id": {nil, row.UserID}, "join_request_id": {nil, id}, "new_user": {nil, true}}); err != nil {
		return out, err
	}
	return out, r.u.event(ctx, "role_assignment.created", "role_assignment", row.RoleAssignmentID, nil, map[string][2]any{
		"membership_id": {nil, row.MembershipID}, "role": {nil, "employee"}, "expires_at": {nil, nil}})
}

func (r joinRequests) Domains(ctx context.Context) ([]storage.JoinDomain, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.JoinDomains(ctx, r.u.org)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.JoinDomain, 0, len(rows))
	for _, x := range rows {
		out = append(out, storage.JoinDomain{Domain: x.Domain, ClaimedBy: x.ClaimedBy, ClaimedAt: x.ClaimedAt})
	}
	return out, nil
}

func (r joinRequests) Claim(ctx context.Context, by uuid.UUID, domain string, now time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if err := r.u.q.ClaimJoinDomain(ctx, sqlcdb.ClaimJoinDomainParams{ByUser: by, Org: r.u.org, Domain: domain, Now: now}); err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "join_domain.claimed", "organisation", r.u.org, nil, map[string][2]any{"domain": {nil, domain}})
}

func (r joinRequests) Release(ctx context.Context, domain string) (bool, error) {
	if err := r.u.check(); err != nil {
		return false, err
	}
	released, err := r.u.q.ReleaseJoinDomain(ctx, sqlcdb.ReleaseJoinDomainParams{Org: r.u.org, Domain: domain})
	if err != nil || !released {
		return false, mapError(err)
	}
	return true, r.u.event(ctx, "join_domain.released", "organisation", r.u.org, nil, map[string][2]any{"domain": {domain, nil}})
}
