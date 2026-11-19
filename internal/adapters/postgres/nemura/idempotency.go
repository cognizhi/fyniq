package nemura

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/nemura/api"
)

// IdempotencyStore keeps the Idempotency-Keys of writes (migration 000038) as the API's own role. Each call is its own short transaction, so a claim is visible to a concurrent identical request at once and
// a claim whose request is still running makes the second wait rather than run again.
type IdempotencyStore struct{ Store *Store }

var _ api.Idempotency = IdempotencyStore{}

// staleInFlight is how long a claim with no answer is trusted: a process that died holding one is not waited for ever.
const staleInFlight = 2 * time.Minute

// Claim takes a key for a request. A key past its 24 hours, or an in-flight claim older than two minutes, is cleared first. It reports whether the caller owns the key, whether the key was used for a
// different request, whether an identical one is still running, or the answer already kept.
func (s IdempotencyStore) Claim(ctx context.Context, org, principal uuid.UUID, key string, hash []byte, now time.Time) (api.IdemClaim, error) {
	var out api.IdemClaim
	err := s.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM nemura.idempotency_key WHERE principal_id = $1 AND key = $2 AND (expires_at <= $3 OR (state = 'in_flight' AND created_at < $3::timestamptz - interval '2 minutes'))`, principal, key, now); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO nemura.idempotency_key (principal_id, key, request_hash, created_at, expires_at) VALUES ($1, $2, $3, $4::timestamptz, $4::timestamptz + interval '24 hours') ON CONFLICT DO NOTHING`, principal, key, hash, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			out.Owned = true
			return nil
		}
		var stored []byte
		var state string
		var status *int
		var ct, loc, etag *string
		err = tx.QueryRow(ctx, `SELECT request_hash, state, status, content_type, location, etag, body, had_body, body_kept FROM nemura.idempotency_key WHERE principal_id = $1 AND key = $2`, principal, key).
			Scan(&stored, &state, &status, &ct, &loc, &etag, &out.Rec.Body, &out.Rec.HadBody, &out.Rec.BodyKept)
		if errors.Is(err, pgx.ErrNoRows) {
			out.InFlight = true // cleared between the two statements: ask again
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case !bytes.Equal(stored, hash):
			out.Mismatch = true
		case state == "in_flight":
			out.InFlight = true
		default:
			out.Rec.Status = *status
			if ct != nil {
				out.Rec.ContentType = *ct
			}
			if loc != nil {
				out.Rec.Location = *loc
			}
			if etag != nil {
				out.Rec.ETag = *etag
			}
		}
		return nil
	})
	return out, err
}

// Finish records the answer of the request that owns a key.
func (s IdempotencyStore) Finish(ctx context.Context, org, principal uuid.UUID, key string, rec api.IdemRecord) error {
	return s.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE nemura.idempotency_key SET state = 'done', status = $3, content_type = $4, location = $5, etag = $6, body = $7, had_body = $8, body_kept = $9 WHERE principal_id = $1 AND key = $2`,
			principal, key, rec.Status, orNil(rec.ContentType), orNil(rec.Location), orNil(rec.ETag), rec.Body, rec.HadBody, rec.BodyKept)
		return err
	})
}

// Release gives a key up: the request failed with a server error, so the same key may try again.
func (s IdempotencyStore) Release(ctx context.Context, org, principal uuid.UUID, key string) error {
	return s.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM nemura.idempotency_key WHERE principal_id = $1 AND key = $2 AND state = 'in_flight'`, principal, key)
		return err
	})
}
