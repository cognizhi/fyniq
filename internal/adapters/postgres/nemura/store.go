package nemura

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoOrganisation is returned for work asked of the store with no organisation: nothing runs outside a tenant (INV-06).
var ErrNoOrganisation = errors.New("nemura store: no organisation: every transaction names the organisation it works for")

// Store is Nemura's database: a pool connected as one service role (NEMURA_DATABASE_URL), whose unit of work sets the
// organisation first in every transaction. Only this file begins or ends a transaction (NFR-PORT-07; archtest).
type Store struct{ pool *pgxpool.Pool }

// Open connects as the role in url.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect to Nemura's database: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// Ping reports whether the database answers: the check behind /readyz.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// InOrg runs fn in one transaction for org: it sets app.org_id first, transaction-scoped, so row-level security matches
// only org's rows (golden rule 8, PRD-001 §11.4), and commits when fn returns nil. Unset, nothing matches.
// The transaction type is the adapter's own, for the adapter's code and its tests; the domain gets ports.
func (s *Store) InOrg(ctx context.Context, org uuid.UUID, fn func(ctx context.Context, tx pgx.Tx) error) (err error) {
	if org == uuid.Nil {
		return ErrNoOrganisation
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	if _, err = tx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", org.String()); err != nil {
		return fmt.Errorf("set the organisation: %w", err)
	}
	if err = fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
