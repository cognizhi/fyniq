package ports

import (
	"context"
	"errors"
)

// Cipher seals the most sensitive fields at the application level (SEC-11): vendor bank account numbers first; NRIC/FIN
// and salaries when they arrive. Production: envelope encryption under a Cloud KMS key. Local: the same envelope under
// a key in .data/keys/.
type Cipher interface {
	// Seal encrypts plaintext bound to aad, the record it belongs to, so a sealed value copied to another record does
	// not open there.
	Seal(ctx context.Context, plaintext, aad []byte) ([]byte, error)
	// Open decrypts what Seal returned for the same aad, under any key version that sealed it.
	Open(ctx context.Context, sealed, aad []byte) ([]byte, error)
	// Fingerprint is a keyed hash of value within scope (the organisation): equal for equal values in the same scope,
	// so duplicates are found (FR-CT-04) and a change is proved in the audit trail (AUD-02), without the value.
	Fingerprint(ctx context.Context, scope, value []byte) ([]byte, error)
}

// ErrNotSealed is returned when a sealed value does not open: changed, sealed for another record, or under a key
// this deployment does not hold.
var ErrNotSealed = errors.New("the sealed value does not open")
