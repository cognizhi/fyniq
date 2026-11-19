package nemura

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
)

// AuditSchema names the format Nemura's events are written in. It is Fyniq's format (the same canonical JSON, masking and
// chain rule, contracts/audit/) under an id of its own, so a verifier knows which application wrote an event.
const AuditSchema = "nemura-audit-event@1"

// Audit queues the audit event of a change made in tx (AUD-01): the unit of work calls it in the same transaction as the
// change, so both commit or neither does, and the database refuses an audited change that has none (AUD-08, NM061). Values
// under a secret's name are removed and NRIC or FIN numbers and tokens are masked by the shared code (SEC-15).
func Audit(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, actor audit.Actor, action string, object audit.Object,
	changes map[string]audit.Change, reason string) error {
	e, err := audit.New(org, at, actor, action, object, changes)
	if err != nil {
		return err
	}
	e.Schema = AuditSchema
	e.Reason = reason
	body, err := e.Canonical()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.audit_event (id, body) VALUES ($1, $2)", e.EventID, string(body)); err != nil {
		return fmt.Errorf("audit event %s: %w", action, err)
	}
	return nil
}

// VerifyAudit recomputes an organisation's whole chain from its first event and checks it against the chain head: every
// sequence number follows the last, every event links to the one before and still gives its stored hash, and the head is
// the last event (AUD-03). It reads as the role the store is connected as, which needs SELECT on the trail (the worker and
// the read-only role). It returns the number of events checked.
func (s *Store) VerifyAudit(ctx context.Context, org uuid.UUID) (int64, error) {
	var checked int64
	err := s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT id, seq, body, prev_hash, hash FROM nemura.audit_event ORDER BY seq")
		if err != nil {
			return err
		}
		var events []audit.Stored
		for rows.Next() {
			var e audit.Stored
			if err := rows.Scan(&e.ID, &e.Seq, &e.Body, &e.PrevHash, &e.Hash); err != nil {
				rows.Close()
				return err
			}
			events = append(events, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		hashes, err := audit.Chain(events)
		if err != nil {
			return err
		}
		var seq int64
		var head []byte
		if err := tx.QueryRow(ctx, "SELECT seq, head_hash FROM nemura.audit_chain").Scan(&seq, &head); err != nil {
			return fmt.Errorf("read the chain head: %w", err)
		}
		if seq != int64(len(events)) {
			return fmt.Errorf("the chain head says %d events and the trail holds %d: events are missing", seq, len(events))
		}
		if seq > 0 && string(hashes[seq]) != string(head) {
			return fmt.Errorf("the chain head is not the hash of the last event")
		}
		checked = seq
		return nil
	})
	return checked, err
}
