package nemura

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
)

// APIClient is a registered client as the database keeps it.
type APIClient struct {
	ID       uuid.UUID
	Name     string
	Scopes   []string
	ClientID string // the OAuth server's
	Revoked  *time.Time
}

// SaveAPIClient keeps a client the OAuth server has registered, with its audit event; a live client of the same name is refused.
func SaveAPIClient(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, name string, scopes []string, hydraID string) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.api_client (id, name, scopes, hydra_client_id, created_by) VALUES ($1, $2, $3, $4, $5)", id, name, scopes, hydraID, by); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return uuid.Nil, &apiclient.Refusal{Code: apiclient.CodeExists, Message: "A client with that name is registered already. Choose another name, or revoke the first."}
		}
		return uuid.Nil, err
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.client.created", audit.Object{Type: "api_client", ID: id},
		map[string]audit.Change{"name": {nil, name}, "scopes": {nil, scopes}, "client_id": {nil, hydraID}}, "")
}

// APIClients lists the organisation's live clients.
func APIClients(ctx context.Context, tx pgx.Tx) ([]APIClient, error) {
	rows, err := tx.Query(ctx, "SELECT id, name, scopes, hydra_client_id, revoked_at FROM nemura.api_client WHERE revoked_at IS NULL ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIClient
	for rows.Next() {
		var c APIClient
		if err := rows.Scan(&c.ID, &c.Name, &c.Scopes, &c.ClientID, &c.Revoked); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// APIClientByID is a live client of the organisation.
func APIClientByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (APIClient, error) {
	var c APIClient
	err := tx.QueryRow(ctx, "SELECT id, name, scopes, hydra_client_id, revoked_at FROM nemura.api_client WHERE id = $1 AND revoked_at IS NULL", id).Scan(&c.ID, &c.Name, &c.Scopes, &c.ClientID, &c.Revoked)
	return c, err
}

// MarkAPIClientRevoked records a client's revocation, with its audit event.
func MarkAPIClientRevoked(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.api_client SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL", id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.client.revoked", audit.Object{Type: "api_client", ID: id}, nil, "")
}
