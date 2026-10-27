//go:build local

package local

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cognizhi/fyniq/internal/fieldseal"
)

// NewFieldKeys builds local mode's ports.Cipher from 64 bytes: the key that wraps data keys, then the fingerprint
// key. The stand-in for the Cloud KMS keys (SEC-11): same envelope, not the same custody.
func NewFieldKeys(seed []byte) (fieldseal.Sealer, error) {
	if len(seed) != 64 {
		return fieldseal.Sealer{}, fmt.Errorf("the field keys are 64 bytes, not %d", len(seed))
	}
	kek, mac := seed[:32], seed[32:]
	sum := sha256.Sum256(kek)
	return fieldseal.Sealer{
		KEK: fieldseal.AESKEK{Name: "local-aes256:" + hex.EncodeToString(sum[:8]), Key: kek},
		MAC: func(_ context.Context, data []byte) ([]byte, error) {
			h := hmac.New(sha256.New, mac)
			h.Write(data)
			return h.Sum(nil), nil
		},
	}, nil
}

// LoadOrCreateFieldKeys reads the field keys at path, creating them (0600) on first use. Never commit them; without
// them the local database's sealed values do not open (`make reset` starts again).
func LoadOrCreateFieldKeys(path string) (fieldseal.Sealer, error) {
	seed, err := os.ReadFile(path) // #nosec G304 -- the path is the deployment's own data directory
	if errors.Is(err, fs.ErrNotExist) {
		seed = make([]byte, 64)
		if _, err := rand.Read(seed); err != nil {
			return fieldseal.Sealer{}, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fieldseal.Sealer{}, err
		}
		if err := os.WriteFile(path, seed, 0o600); err != nil {
			return fieldseal.Sealer{}, err
		}
	} else if err != nil {
		return fieldseal.Sealer{}, err
	}
	return NewFieldKeys(seed)
}
