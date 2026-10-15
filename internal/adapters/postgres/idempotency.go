package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Idempotency keys are a cache of responses, not a change of record: they carry no audit event
// (migration 000006, AUD-08).

func (u *unitOfWork) Idempotency() storage.Idempotency { return idempotency{u} }

type idempotency struct{ u *unitOfWork }

func (r idempotency) Claim(ctx context.Context, c storage.IdempotencyClaim) (*storage.StoredResponse, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	// A concurrent claim of the same key waits here until the other unit of work ends.
	_, err := r.u.q.ClaimIdempotencyKey(ctx, sqlcdb.ClaimIdempotencyKeyParams{ClientID: c.Client, Key: c.Key,
		RequestHash: c.RequestHash, ExpiresAt: c.ExpiresAt})
	if err == nil {
		return nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, mapError(err)
	}
	row, err := r.u.q.GetIdempotencyKey(ctx, sqlcdb.GetIdempotencyKeyParams{ClientID: c.Client, Key: c.Key})
	if err != nil {
		return nil, mapError(err)
	}
	earlier := &storage.StoredResponse{RequestHash: row.RequestHash, Body: row.ResponseBody, ExpiresAt: row.ExpiresAt}
	if row.ResponseStatus != nil {
		earlier.Status = int(*row.ResponseStatus)
	}
	return earlier, nil
}

func (r idempotency) Complete(ctx context.Context, client, key string, status int, body []byte) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if status < 100 || status > 599 {
		return &storage.Error{Code: storage.CodeInvalid, Message: "a response status outside 100 to 599"}
	}
	s := int16(status)
	n, err := r.u.q.CompleteIdempotencyKey(ctx, sqlcdb.CompleteIdempotencyKeyParams{ClientID: client, Key: key,
		ResponseStatus: &s, ResponseBody: body})
	if err != nil {
		return mapError(err)
	}
	if n != 1 {
		return &storage.Error{Code: storage.CodeInvalid, Message: "the idempotency key was not claimed in this unit of work"}
	}
	return nil
}

func (r idempotency) Purge(ctx context.Context, now time.Time) (int64, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.PurgeIdempotencyKeys(ctx, now)
	return n, mapError(err)
}
