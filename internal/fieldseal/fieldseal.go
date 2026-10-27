// Package fieldseal is the envelope behind ports.Cipher (SEC-11): each value is encrypted with a fresh AES-256-GCM
// data key, and the data key is wrapped by a key-encryption key that never leaves its keeper (Cloud KMS in the cloud,
// a key file in local mode). A sealed value names the key that wrapped it, so values sealed before a rotation still
// open. Fingerprints are HMAC-SHA256 under a key that is never rotated, since rotating it would change every
// fingerprint.
package fieldseal

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/cognizhi/fyniq/internal/ports"
)

// KEK wraps and unwraps data keys. ID names the key that wraps now; Unwrap is given the ID a sealed value names and
// refuses one it does not hold.
type KEK interface {
	ID() string
	Wrap(ctx context.Context, dek, aad []byte) ([]byte, error)
	Unwrap(ctx context.Context, id string, wrapped, aad []byte) ([]byte, error)
}

// MAC is a keyed hash of data: HMAC-SHA256, 32 bytes.
type MAC func(ctx context.Context, data []byte) ([]byte, error)

// Sealer is ports.Cipher on a KEK and a MAC.
type Sealer struct {
	KEK KEK
	MAC MAC
}

var _ ports.Cipher = Sealer{}

// version is the first byte of every sealed value: the layout below.
const version = 1

// Seal implements ports.Cipher. The layout: version, the KEK's ID (length byte, then the ID), the wrapped data key
// (two length bytes, then the key), the 12-byte nonce and the AES-GCM ciphertext with its tag.
func (s Sealer) Seal(ctx context.Context, plaintext, aad []byte) ([]byte, error) {
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return nil, err
	}
	wrapped, err := s.KEK.Wrap(ctx, dek, aad)
	if err != nil {
		return nil, fmt.Errorf("fieldseal: wrap the data key: %w", err)
	}
	id := s.KEK.ID()
	if id == "" || len(id) > 255 || len(wrapped) > 65535 {
		return nil, errors.New("fieldseal: the key's ID or its wrapped data key is too long")
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{version, byte(len(id))}, id...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(wrapped))) // #nosec G115 -- checked above
	out = append(append(out, wrapped...), nonce...)
	return gcm.Seal(out, nonce, plaintext, aad), nil
}

// Open implements ports.Cipher.
func (s Sealer) Open(ctx context.Context, sealed, aad []byte) ([]byte, error) {
	id, wrapped, nonce, body, ok := split(sealed)
	if !ok {
		return nil, ports.ErrNotSealed
	}
	dek, err := s.KEK.Unwrap(ctx, id, wrapped, aad)
	if err != nil {
		if errors.Is(err, ports.ErrNotSealed) {
			return nil, err
		}
		return nil, fmt.Errorf("fieldseal: unwrap the data key: %w", err)
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return nil, ports.ErrNotSealed
	}
	plain, err := gcm.Open(nil, nonce, body, aad)
	if err != nil {
		return nil, ports.ErrNotSealed
	}
	return plain, nil
}

// Fingerprint implements ports.Cipher: the MAC of the scope's length, the scope and the value, so no scope and value
// pair reads as another.
func (s Sealer) Fingerprint(ctx context.Context, scope, value []byte) ([]byte, error) {
	data := binary.BigEndian.AppendUint32(nil, uint32(len(scope))) // #nosec G115 -- a scope is an organisation's ID
	return s.MAC(ctx, append(append(data, scope...), value...))
}

// split reads a sealed value's parts.
func split(b []byte) (id string, wrapped, nonce, body []byte, ok bool) {
	if len(b) < 2 || b[0] != version {
		return "", nil, nil, nil, false
	}
	n := int(b[1])
	b = b[2:]
	if len(b) < n+2 {
		return "", nil, nil, nil, false
	}
	id, b = string(b[:n]), b[n:]
	w := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	if len(b) < w+12+16 {
		return "", nil, nil, nil, false
	}
	return id, b[:w], b[w : w+12], b[w+12:], true
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// AESKEK is a KEK held in memory: an AES-256 key that wraps data keys with AES-GCM. Local mode's key comes from a file
// (adapters/local); tests make their own.
type AESKEK struct {
	Name string
	Key  []byte // 32 bytes
}

// ID implements KEK.
func (k AESKEK) ID() string { return k.Name }

// Wrap implements KEK.
func (k AESKEK) Wrap(_ context.Context, dek, aad []byte) ([]byte, error) {
	gcm, err := newGCM(k.Key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, dek, aad), nil
}

// Unwrap implements KEK: only its own key.
func (k AESKEK) Unwrap(_ context.Context, id string, wrapped, aad []byte) ([]byte, error) {
	gcm, err := newGCM(k.Key)
	if err != nil {
		return nil, err
	}
	if id != k.Name || len(wrapped) < gcm.NonceSize() {
		return nil, ports.ErrNotSealed
	}
	dek, err := gcm.Open(nil, wrapped[:gcm.NonceSize()], wrapped[gcm.NonceSize():], aad)
	if err != nil {
		return nil, ports.ErrNotSealed
	}
	return dek, nil
}
