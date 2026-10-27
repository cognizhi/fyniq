package fieldseal_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/cognizhi/fyniq/internal/fieldseal"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

func sealer(name string, key byte) fieldseal.Sealer {
	return fieldseal.Sealer{
		KEK: fieldseal.AESKEK{Name: name, Key: bytes.Repeat([]byte{key}, 32)},
		MAC: func(_ context.Context, data []byte) ([]byte, error) {
			h := hmac.New(sha256.New, []byte("fingerprint key"))
			h.Write(data)
			return h.Sum(nil), nil
		},
	}
}

// The envelope seals each value under a fresh data key, bound to its record, and names the key that wrapped it: a
// value sealed under another key, or with a version this build does not read, does not open.
// req: SEC-11
// req: AUD-02
// asvs: V11.3.2, V11.3.3, V11.2.2
func TestTheEnvelopeSealsBoundToTheRecord(t *testing.T) {
	porttest.Cipher(t, sealer("test-key", 1))
	ctx := context.Background()
	sealed, err := sealer("test-key", 1).Seal(ctx, []byte("12345678"), []byte("r"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sealer("other-key", 2).Open(ctx, sealed, []byte("r")); !errors.Is(err, ports.ErrNotSealed) {
		t.Errorf("a value sealed under another key: %v", err)
	}
	if _, err := sealer("test-key", 2).Open(ctx, sealed, []byte("r")); !errors.Is(err, ports.ErrNotSealed) {
		t.Errorf("a value under a key of the same name with other bytes: %v", err)
	}
	future := bytes.Clone(sealed)
	future[0] = 2
	if _, err := sealer("test-key", 1).Open(ctx, future, []byte("r")); !errors.Is(err, ports.ErrNotSealed) {
		t.Errorf("a value in a layout this build does not read: %v", err)
	}
	for _, short := range [][]byte{nil, {1}, {1, 200, 'x'}} {
		if _, err := sealer("test-key", 1).Open(ctx, short, []byte("r")); !errors.Is(err, ports.ErrNotSealed) {
			t.Errorf("Open(%x): %v", short, err)
		}
	}
}
