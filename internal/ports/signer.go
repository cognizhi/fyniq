package ports

import (
	"context"
	"errors"
)

// Signer signs audit anchors (§11.5 L3, CTL-AUD-02). Production: a Cloud KMS asymmetric key, whose
// public key allows offline verification. Local: an Ed25519 key under .data/keys/.
type Signer interface {
	// KeyID names the key version that signs; anchors record it.
	KeyID() string
	Sign(ctx context.Context, msg []byte) ([]byte, error)
	// Verify checks a signature made by the key version keyID; rotation keeps old versions
	// verifiable.
	Verify(keyID string, msg, sig []byte) error
}

// ErrBadSignature is returned when a signature does not verify.
var ErrBadSignature = errors.New("the signature does not verify")
