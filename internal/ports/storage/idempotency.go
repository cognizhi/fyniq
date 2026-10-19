package storage

import (
	"context"
	"time"
)

// Idempotency keys (FR-INT-08): a client's Idempotency-Key is kept for 24 hours with a hash of the
// request it came with. The API claims the key in the unit of work that carries out the request and
// saves the response in the same unit of work, so they commit together or not at all. A second
// request with the same key waits for the first to commit or roll back, then reads its response.
// These keys only stop a repeated request; the posting identity of an entry never expires (§11.4).

// IdempotencyClaim is a key a client sends with a request.
type IdempotencyClaim struct {
	Client      string // who sent it: the OAuth client and the subject, so keys never collide across callers
	Key         string
	RequestHash []byte // SHA-256 of the request
	// CreatedAt and ExpiresAt come from the same clock, the API's (a business date in local mode,
	// DEV-05), never one from the API and one from the database.
	CreatedAt time.Time
	ExpiresAt time.Time
}

// StoredResponse is the result saved with a key. Status is 0 when the key was claimed but no
// response was saved.
type StoredResponse struct {
	RequestHash []byte
	Status      int
	Body        []byte
	ExpiresAt   time.Time
}

// Idempotency is the organisation's idempotency keys.
type Idempotency interface {
	// Claim records the key in this unit of work. When the client used it before, nothing is
	// recorded and the earlier request's hash and response are returned instead, expired or not.
	Claim(ctx context.Context, c IdempotencyClaim) (earlier *StoredResponse, err error)
	// Complete saves the response of a key claimed in this unit of work.
	Complete(ctx context.Context, client, key string, status int, body []byte) error
	// Purge deletes the keys that expired at or before now and returns how many (the worker's job).
	Purge(ctx context.Context, now time.Time) (int64, error)
}

// The kinds of request a rate limit counts (FR-INT-16).
const (
	RateRead  = "read"  // a safe method: GET or HEAD
	RateWrite = "write" // anything else
)

// RateLimits counts an organisation's requests per client in one-minute windows (FR-INT-16). The
// windows are a counter, not a change of record: they carry no audit event (AUD-08).
type RateLimits interface {
	// Count adds one request of kind (RateRead or RateWrite) by client to the window that starts at
	// start, and returns how many requests the window now holds.
	Count(ctx context.Context, client, kind string, start time.Time) (int, error)
	// Purge deletes the windows that started before before and returns how many (the worker's job).
	Purge(ctx context.Context, before time.Time) (int64, error)
}
