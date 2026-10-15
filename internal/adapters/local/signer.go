//go:build local

package local

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Ed25519 signs audit anchors with a local key: the stand-in for the Cloud KMS asymmetric key
// (§12.8). Same role, not the same custody: the key is a file under .data/keys/.
type Ed25519 struct {
	key ed25519.PrivateKey
	id  string
}

var _ ports.Signer = (*Ed25519)(nil)

// NewEd25519 builds a signer from a 32-byte seed (tests pass their own).
func NewEd25519(seed []byte) (*Ed25519, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("an Ed25519 seed has %d bytes, not %d", ed25519.SeedSize, len(seed))
	}
	key := ed25519.NewKeyFromSeed(seed)
	sum := sha256.Sum256(key.Public().(ed25519.PublicKey))
	return &Ed25519{key: key, id: "local-ed25519:" + hex.EncodeToString(sum[:8])}, nil
}

// LoadOrCreateEd25519 reads the seed at path, creating it (0600) on first use. Never commit it.
func LoadOrCreateEd25519(path string) (*Ed25519, error) {
	seed, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		seed = make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, seed, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return NewEd25519(seed)
}

// KeyID implements ports.Signer.
func (s *Ed25519) KeyID() string { return s.id }

// Sign implements ports.Signer.
func (s *Ed25519) Sign(_ context.Context, msg []byte) ([]byte, error) {
	return ed25519.Sign(s.key, msg), nil
}

// Verify implements ports.Signer. Locally there is one key version.
func (s *Ed25519) Verify(keyID string, msg, sig []byte) error {
	if keyID != s.id || !ed25519.Verify(s.key.Public().(ed25519.PublicKey), msg, sig) {
		return ports.ErrBadSignature
	}
	return nil
}
