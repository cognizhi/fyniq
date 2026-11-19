package nemura

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
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
	if linked, err := c.linked(ctx, s.Organisation); err != nil {
		return api.Client{}, err
	} else if linked { // from the day the link exists a second approver decides (NM-ARC-04)
		return api.Client{}, &apiclient.Refusal{Code: CodeSecondApprover, Message: "Nemura is linked to Fyniq, so a client needs a second approver: ask for it, and an Owner or Administrator other than you approves."}
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

// CodeSecondApprover is the refusal of a registration a linked organisation must have approved by a second person (NM-ARC-04).
const CodeSecondApprover = "NM_API_CLIENT_SECOND_APPROVER"

func (c APIClientService) linked(ctx context.Context, org uuid.UUID) (bool, error) {
	var live bool
	err := c.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := LiveLink(ctx, tx)
		live = err == nil
		var r *payroll.Refusal
		if errors.As(err, &r) && r.Code == CodeLinkNone {
			return nil
		}
		return err
	})
	return live, err
}

// RequestRegistration asks to register an API client when the organisation is linked to Fyniq: the request waits for an Owner or Administrator other than the one who asked, with a fresh
// passkey. It needs the asker's own passkey confirmation too (NM-ARC-06).
func (c APIClientService) RequestRegistration(ctx context.Context, s api.Session, name string, scopes []string, alg string, key json.RawMessage) (uuid.UUID, error) {
	now := c.Clock.Now()
	if !s.Fresh(now) {
		return uuid.Nil, &apiclient.Refusal{Code: apiclient.CodeInvalid, Message: "Confirm with your passkey first: registering a client needs a passkey confirmed in the last five minutes."}
	}
	if err := apiclient.Check(name, scopes, alg, key); err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.api_client_request (id, name, scopes, alg, public_key, requested_by, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, name, scopes, alg, []byte(key), s.Member, now); err != nil {
			return err
		}
		digest := sha256.Sum256([]byte("api_client_request|" + name + "|" + alg + "|" + string(key)))
		approval := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.approval_request (id, object_type, object_id, digest, requested_by, required_role, expires_at) VALUES ($1, 'api_client_request', $2, $3, $4, 'owner', $5)`,
			approval, id, digest[:], s.Member, now.AddDate(0, 0, 7)); err != nil {
			return err
		}
		if err := Audit(ctx, tx, s.Organisation, now, user(s.Member), "nemura.approval.requested", audit.Object{Type: "approval_request", ID: approval},
			map[string]audit.Change{"object_type": {nil, "api_client_request"}, "request": {nil, id.String()}}, ""); err != nil {
			return err
		}
		return Audit(ctx, tx, s.Organisation, now, user(s.Member), "nemura.client.requested", audit.Object{Type: "api_client_request", ID: id},
			map[string]audit.Change{"name": {nil, name}, "scopes": {nil, scopes}}, "")
	})
	return id, err
}

// ApproveRegistration approves a request: another Owner or Administrator than the one who asked, with a passkey confirmed in the last five minutes. The client is then registered with
// the OAuth server and recorded, as Register does.
func (c APIClientService) ApproveRegistration(ctx context.Context, s api.Session, request uuid.UUID) (api.Client, error) {
	if c.OAuth == nil {
		return api.Client{}, api.ErrUnavailable
	}
	now := c.Clock.Now()
	if !s.Fresh(now) {
		return api.Client{}, &apiclient.Refusal{Code: apiclient.CodeInvalid, Message: "Confirm with your passkey first: approving a client needs a passkey confirmed in the last five minutes."}
	}
	var name, alg string
	var scopes []string
	var key []byte
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		var requester uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT name, scopes, alg, public_key, requested_by FROM nemura.api_client_request WHERE id = $1`, request).Scan(&name, &scopes, &alg, &key, &requester); err != nil {
			return &apiclient.Refusal{Code: apiclient.CodeInvalid, Message: "There is no such request."}
		}
		held, err := ActiveRoles(ctx, tx, s.Member, now)
		if err != nil {
			return err
		}
		if s.Member == requester || !(slices.Contains(held, authz.Owner) || slices.Contains(held, authz.Administrator)) {
			return &apiclient.Refusal{Code: CodeSecondApprover, Message: "A second approver decides: an Owner or Administrator other than the one who asked."}
		}
		var approval uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM nemura.approval_request WHERE object_type = 'api_client_request' AND object_id = $1 AND state = 'pending'`, request).Scan(&approval); err != nil {
			return &apiclient.Refusal{Code: apiclient.CodeInvalid, Message: "The request was decided already."}
		}
		if _, err := tx.Exec(ctx, `UPDATE nemura.approval_request SET state = 'approved', decided_by = $2, decided_at = $3 WHERE id = $1`, approval, s.Member, now); err != nil {
			return err
		}
		return Audit(ctx, tx, s.Organisation, now, user(s.Member), "nemura.approval.approved", audit.Object{Type: "approval_request", ID: approval}, map[string]audit.Change{"state": {"pending", "approved"}}, "")
	})
	if err != nil {
		return api.Client{}, err
	}
	hydraID, err := c.OAuth.RegisterIntegration(ctx, ports.IntegrationRegistration{Name: name, Class: "integration", Audience: apiclient.Audience, Scopes: scopes,
		Key: key, Alg: alg, TokenLifespan: apiclient.TokenLifespan, Organisation: s.Organisation.String()})
	if err != nil {
		return api.Client{}, err
	}
	var id uuid.UUID
	if err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) (err error) {
		id, err = SaveAPIClient(ctx, tx, s.Organisation, now, s.Member, name, scopes, hydraID)
		return err
	}); err != nil {
		_ = c.OAuth.RemoveClient(context.WithoutCancel(ctx), hydraID)
		return api.Client{}, err
	}
	return api.Client{ID: id.String(), Name: name, Scopes: scopes, ClientID: hydraID, Audience: apiclient.Audience}, nil
}
