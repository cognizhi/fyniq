package nemura

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	"github.com/cognizhi/fyniq/internal/ports"
)

var _ api.APIClients = APIClientService{}

// APIClientService registers the organisation's API clients with Nemura's own OAuth server and keeps their records (NM-ARC-06, question 11).
type APIClientService struct {
	Store *Store
	OAuth ports.OAuthClients // nil: this deployment has none
	Clock ports.Clock
}

func (c APIClientService) Register(ctx context.Context, s api.Session, name string, scopes []string, alg string, key json.RawMessage) (api.Client, error) {
	if c.OAuth == nil {
		return api.Client{}, api.ErrUnavailable
	}
	if err := apiclient.Check(name, scopes, alg, key); err != nil {
		return api.Client{}, err
	}
	now := c.Clock.Now()
	// Refuse a repeat before asking the OAuth server for a client it would then hold with no record.
	var exists bool
	if err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.api_client WHERE lower(name) = lower($1) AND revoked_at IS NULL)", name).Scan(&exists)
	}); err != nil {
		return api.Client{}, err
	}
	if exists {
		return api.Client{}, &apiclient.Refusal{Code: apiclient.CodeExists, Message: "A client with that name is registered already. Choose another name, or revoke the first."}
	}
	hydraID, err := c.OAuth.RegisterIntegration(ctx, ports.IntegrationRegistration{Name: name, Class: "integration", Audience: apiclient.Audience, Scopes: scopes,
		Key: key, Alg: alg, TokenLifespan: apiclient.TokenLifespan, Organisation: s.Organisation.String()})
	if err != nil {
		return api.Client{}, err
	}
	var id uuid.UUID
	err = c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = SaveAPIClient(ctx, tx, s.Organisation, now, s.Member, name, scopes, hydraID)
		return err
	})
	if err != nil {
		_ = c.OAuth.RemoveClient(context.WithoutCancel(ctx), hydraID) // no client the OAuth server holds without a record
		return api.Client{}, err
	}
	return api.Client{ID: id.String(), Name: name, Scopes: scopes, ClientID: hydraID, Audience: apiclient.Audience}, nil
}

func (c APIClientService) List(ctx context.Context, s api.Session) ([]api.Client, error) {
	var out []api.Client
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		cs, err := APIClients(ctx, tx)
		for _, k := range cs {
			out = append(out, api.Client{ID: k.ID.String(), Name: k.Name, Scopes: k.Scopes, ClientID: k.ClientID, Audience: apiclient.Audience})
		}
		return err
	})
	return out, err
}

func (c APIClientService) Revoke(ctx context.Context, s api.Session, id uuid.UUID) error {
	if c.OAuth == nil {
		return api.ErrUnavailable
	}
	now := c.Clock.Now()
	var hydraID string
	if err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		k, err := APIClientByID(ctx, tx, id)
		hydraID = k.ClientID
		return err
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return api.ErrNoClient
		}
		return err
	}
	// The OAuth server first: a revoked client's tokens are gone before its record says so.
	if err := c.OAuth.RemoveClient(ctx, hydraID); err != nil {
		return err
	}
	return c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		return MarkAPIClientRevoked(ctx, tx, s.Organisation, now, s.Member, id)
	})
}
