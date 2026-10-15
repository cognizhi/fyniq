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
	ExpiresAt   time.Time
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
