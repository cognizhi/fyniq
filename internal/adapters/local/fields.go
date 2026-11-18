//go:build local

package local

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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
	seed, err := loadOrCreateSeed(path, 64)
	if err != nil {
		return fieldseal.Sealer{}, err
	}
	return NewFieldKeys(seed)
}
