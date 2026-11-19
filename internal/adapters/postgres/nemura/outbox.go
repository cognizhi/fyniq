package nemura

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
)

var _ outbox.Store = (*Store)(nil)

const outboxColumns = `id, worker_class, kind, payload, coalesce(business_key, ''), state, attempts, max_attempts, dispatches,
  coalesce(task_name, ''), deadline_at, lease_expires_at`

func scanRow(row pgx.Row) (outbox.Row, error) {
	var r outbox.Row
	var state string
	err := row.Scan(&r.ID, &r.Class, &r.Kind, &r.Payload, &r.BusinessKey, &state, &r.Attempts, &r.MaxAttempts, &r.Dispatches,
		&r.TaskName, &r.Deadline, &r.LeaseUntil)
	r.State = outbox.State(state)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ports.ErrNotFound
	}
	return r, err
}

// Enqueue writes work as an outbox row in tx, the same transaction as the change it belongs to (Transactional outbox): a
// rollback leaves neither. The caller dispatches the row once the transaction commits (outbox.Dispatcher).
func Enqueue(ctx context.Context, tx pgx.Tx, item outbox.NewItem) (outbox.Row, error) {
	payload := item.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	var key *string
	if item.BusinessKey != "" {
		key = &item.BusinessKey
	}
	attempts := cmp.Or(item.MaxAttempts, 5)
	return scanRow(tx.QueryRow(ctx, `INSERT INTO nemura.outbox (worker_class, kind, payload, business_key, max_attempts)
VALUES ($1, $2, $3, $4, $5) RETURNING `+outboxColumns, item.Class, item.Kind, payload, key, attempts))
}

// MarkDispatched records that a task was sent for the row.
func (s *Store) MarkDispatched(ctx context.Context, org, id uuid.UUID, task string, deadline time.Time) error {
	return s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE nemura.outbox SET state = 'dispatched', dispatches = dispatches + 1, task_name = $2, deadline_at = $3,
  lease_holder = NULL, lease_expires_at = NULL WHERE id = $1 AND state IN ('pending', 'dispatched', 'failed', 'running')`, id, task, deadline)
		if err == nil && tag.RowsAffected() == 0 {
			return errors.New("the outbox row is done, dead or gone, so it cannot be dispatched")
		}
		return err
	})
}

// Lease takes the row for holder until until, counting an attempt.
func (s *Store) Lease(ctx context.Context, org, id uuid.UUID, holder string, now, until time.Time) (outbox.Row, bool, error) {
	var row outbox.Row
	var leased bool
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		r, err := scanRow(tx.QueryRow(ctx, `UPDATE nemura.outbox SET state = 'running', attempts = attempts + 1, lease_holder = $2, lease_expires_at = $3
WHERE id = $1 AND (state IN ('dispatched', 'failed') OR (state = 'running' AND lease_expires_at < $4)) RETURNING `+outboxColumns,
			id, holder, until, now))
		if err == nil {
			row, leased = r, true
			return nil
		}
		if !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		row, err = scanRow(tx.QueryRow(ctx, "SELECT "+outboxColumns+" FROM nemura.outbox WHERE id = $1", id))
		return err
	})
	return row, leased, err
}

// Finish ends holder's lease with state. A row that dies is audited in the same transaction (nemura.outbox.row.dead), for the
// alert that reaches the people who look after the organisation.
func (s *Store) Finish(ctx context.Context, org, id uuid.UUID, holder string, state outbox.State, msg string, now time.Time) error {
	return s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var kind string
		var attempts int
		var lastErr *string
		if len(msg) > 2000 {
			msg = msg[:2000]
		}
		if msg != "" {
			lastErr = &msg
		}
		err := tx.QueryRow(ctx, `UPDATE nemura.outbox SET state = $3, last_error = $4, lease_holder = NULL, lease_expires_at = NULL
WHERE id = $1 AND lease_holder = $2 AND state = 'running' RETURNING kind, attempts`, id, holder, string(state), lastErr).Scan(&kind, &attempts)
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("the lease on the outbox row was lost to another holder: its outcome stands")
		}
		if err != nil {
			return err
		}
		if state != outbox.Dead {
			return nil
		}
		return Audit(ctx, tx, org, now, audit.Actor{Type: "service", ID: "nemura-worker"}, "nemura.outbox.row.dead",
			audit.Object{Type: "outbox", ID: id}, map[string]audit.Change{"state": {"running", string(state)}, "kind": {nil, kind},
				"attempts": {nil, attempts}, "last_error": {nil, msg}}, "")
	})
}

// Due lists the rows the sweep must send again.
func (s *Store) Due(ctx context.Context, org uuid.UUID, now, pendingBefore time.Time) ([]outbox.Row, error) {
	var out []outbox.Row
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT "+outboxColumns+` FROM nemura.outbox WHERE
  (state = 'pending' AND created_at < $2) OR (state IN ('dispatched', 'failed') AND deadline_at < $1) OR (state = 'running' AND lease_expires_at < $1)
ORDER BY created_at LIMIT 500`, now, pendingBefore)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRow(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// MarkDead gives up on a row the sweep found out of attempts, and audits it.
func (s *Store) MarkDead(ctx context.Context, org, id uuid.UUID, reason string, now time.Time) error {
	return s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var kind string
		var attempts int
		err := tx.QueryRow(ctx, `UPDATE nemura.outbox SET state = 'dead', last_error = $2, lease_holder = NULL, lease_expires_at = NULL
WHERE id = $1 AND state IN ('pending', 'dispatched', 'running', 'failed') RETURNING kind, attempts`, id, reason).Scan(&kind, &attempts)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // finished or dead meanwhile
		}
		if err != nil {
			return err
		}
		return Audit(ctx, tx, org, now, audit.Actor{Type: "service", ID: "nemura-worker"}, "nemura.outbox.row.dead",
			audit.Object{Type: "outbox", ID: id}, map[string]audit.Change{"kind": {nil, kind}, "attempts": {nil, attempts}, "last_error": {nil, reason}}, "")
	})
}

// Organisations lists every organisation through the worker's tenants view.
func (s *Store) Organisations(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, "SELECT organisation_id FROM nemura.tenants ORDER BY created_at")
	if err != nil {
		return nil, fmt.Errorf("list the organisations: %w", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
