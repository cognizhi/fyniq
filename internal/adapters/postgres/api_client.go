package postgres

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ API clients (FR-INT-02, SEC-07)

var _ storage.APIClientRoutes = (*Store)(nil)

func (u *unitOfWork) APIClients() storage.APIClients { return apiClients{u} }

type apiClients struct{ u *unitOfWork }

func toAPIClient(r sqlcdb.FyniqApiClient) storage.APIClient {
	c := storage.APIClient{ID: r.ID, Kind: r.Kind, ClientID: r.ClientID, Name: r.Name, Class: r.Class, OwnerID: r.OwnerID,
		Scopes: slices.Clone(r.Scopes), IPAllow: slices.Clone(r.IpAllow), ExpiresAt: r.ExpiresAt, ApprovalID: r.ApprovalID, Version: int(r.Version),
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, RevokedAt: r.RevokedAt}
	if r.ReadsPerMinute != nil {
		c.ReadsPerMinute = int(*r.ReadsPerMinute)
	}
	if r.WritesPerMinute != nil {
		c.WritesPerMinute = int(*r.WritesPerMinute)
	}
	if r.KeyThumbprint != nil {
		c.KeyThumbprint = *r.KeyThumbprint
	}
	if r.RevokedBy != nil {
		c.RevokedBy = *r.RevokedBy
	}
	if r.RevokeReason != nil {
		c.RevokeReason = *r.RevokeReason
	}
	if c.Scopes == nil {
		c.Scopes = []string{}
	}
	if c.IPAllow == nil {
		c.IPAllow = []string{}
	}
	return c
}

// perMinute is a client's own limit: none for 0, the platform's.
func perMinute(n int) *int32 {
	if n <= 0 {
		return nil
	}
	v := int32(min(n, 1<<20)) // #nosec G115 -- bounded above
	return &v
}

// apiClientView is what the audit trail keeps of a client: never a token or a key, only the key's thumbprint.
func apiClientView(c *storage.APIClient) any {
	if c == nil {
		return nil
	}
	return map[string]any{"kind": c.Kind, "client_id": c.ClientID, "name": c.Name, "class": c.Class, "owner_id": c.OwnerID.String(),
		"scopes": c.Scopes, "ip_allow": c.IPAllow, "reads_per_minute": c.ReadsPerMinute, "writes_per_minute": c.WritesPerMinute,
		"key_thumbprint": c.KeyThumbprint, "expires_at": c.ExpiresAt.UTC().Format(time.RFC3339), "revoked": c.RevokedAt != nil,
		"revoke_reason": c.RevokeReason}
}

func (r apiClients) List(ctx context.Context, kind string) ([]storage.APIClient, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.APIClients(ctx, kind)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.APIClient, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAPIClient(row))
	}
	return out, nil
}

func (r apiClients) Get(ctx context.Context, id uuid.UUID) (storage.APIClient, error) {
	if err := r.u.check(); err != nil {
		return storage.APIClient{}, err
	}
	row, err := r.u.q.GetAPIClient(ctx, id)
	if err != nil {
		return storage.APIClient{}, mapError(err)
	}
	return toAPIClient(row), nil
}

func (r apiClients) Create(ctx context.Context, c storage.APIClient) (storage.APIClient, error) {
	if err := r.u.check(); err != nil {
		return storage.APIClient{}, err
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.Must(uuid.NewV7())
	}
	row, err := r.u.q.InsertAPIClient(ctx, sqlcdb.InsertAPIClientParams{ID: c.ID, Kind: c.Kind, ClientID: c.ClientID, Name: c.Name, Class: c.Class,
		OwnerID: c.OwnerID, Scopes: c.Scopes, IpAllow: append([]string{}, c.IPAllow...), ReadsPerMinute: perMinute(c.ReadsPerMinute),
		WritesPerMinute: perMinute(c.WritesPerMinute), KeyThumbprint: optional(c.KeyThumbprint), ExpiresAt: c.ExpiresAt, ApprovalID: c.ApprovalID,
		CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt})
	if err != nil {
		return storage.APIClient{}, mapError(err)
	}
	out := toAPIClient(row)
	return out, r.u.event(ctx, "api_client.registered", "api_client", out.ID, &out.Version, map[string][2]any{"client": {nil, apiClientView(&out)}})
}

func (r apiClients) Revoke(ctx context.Context, id uuid.UUID, version int, by, reason string, at time.Time) (storage.APIClient, error) {
	if err := r.u.check(); err != nil {
		return storage.APIClient{}, err
	}
	before, err := r.u.q.GetAPIClientForUpdate(ctx, id)
	if err != nil {
		return storage.APIClient{}, mapError(err)
	}
	if int(before.Version) != version {
		return storage.APIClient{}, &storage.Error{Code: storage.CodeVersion, Message: "the API client changed since it was read",
			Hint: "Read it again and revoke it again"}
	}
	row, err := r.u.q.RevokeAPIClient(ctx, sqlcdb.RevokeAPIClientParams{ID: id, Version: int32(version), RevokedAt: &at, RevokedBy: &by, // #nosec G115 -- a version counts up from 1
		RevokeReason: &reason})
	if err != nil {
		return storage.APIClient{}, mapError(err)
	}
	was, out := toAPIClient(before), toAPIClient(row)
	return out, r.u.event(ctx, "api_client.revoked", "api_client", out.ID, &out.Version, map[string][2]any{"client": {apiClientView(&was), apiClientView(&out)}})
}

// APIClientAccess implements storage.APIClientRoutes, through api_client_access, which reads the global route before any
// tenant is set and the client and its member's roles in the client's organisation.
func (s *Store) APIClientAccess(ctx context.Context, clientID string, now time.Time) (storage.APIClientAccess, error) {
	row, err := s.q().APIClientAccess(ctx, sqlcdb.APIClientAccessParams{ClientID: clientID, Now: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.APIClientAccess{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.APIClientAccess{}, mapError(err)
	}
	if row.Kind == "" {
		return storage.APIClientAccess{}, ports.ErrNotFound
	}
	return storage.APIClientAccess{Organisation: row.OrganisationID, ID: row.ID, Kind: row.Kind, Class: row.Class, OwnerID: row.OwnerID,
		OwnerActive: row.OwnerActive, Roles: row.Roles, Scopes: row.Scopes, IPAllow: row.IpAllow, ReadsPerMinute: int(row.ReadsPerMinute),
		WritesPerMinute: int(row.WritesPerMinute), ExpiresAt: row.ExpiresAt, Revoked: row.Revoked}, nil
}
