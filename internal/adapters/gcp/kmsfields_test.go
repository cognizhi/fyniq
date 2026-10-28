package gcp

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

const (
	fieldsKey  = "projects/fyniq-staging/locations/asia-southeast1/keyRings/platform-staging/cryptoKeys/fields"
	macVersion = "projects/fyniq-staging/locations/asia-southeast1/keyRings/platform-staging/cryptoKeys/fingerprints/cryptoKeyVersions/1"
)

// fakeFieldsKMS encrypts and decrypts like Cloud KMS for one symmetric key whose primary version can rotate, refusing
// a ciphertext or AAD it cannot decrypt with 400, and signs MACs with one HMAC key version.
type fakeFieldsKMS struct {
	mu       sync.Mutex
	versions [][]byte // the symmetric key's versions; the last is primary
	mac      []byte
	corrupt  bool
	calls    int
}

func (f *fakeFieldsKMS) rotate() {
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	f.versions = append(f.versions, k)
}

func (f *fakeFieldsKMS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	var req map[string]string
	_ = json.NewDecoder(r.Body).Decode(&req)
	b64 := func(k string) []byte { b, _ := base64.StdEncoding.DecodeString(req[k]); return b }
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	path := strings.TrimPrefix(r.URL.Path, "/v1/")
	switch {
	case path == fieldsKey+":encrypt":
		n := len(f.versions)
		gcm := fakeGCM(f.versions[n-1])
		nonce := make([]byte, gcm.NonceSize())
		_, _ = rand.Read(nonce)
		ct := append([]byte{byte(n)}, gcm.Seal(nonce, nonce, b64("plaintext"), b64("additionalAuthenticatedData"))...)
		sum := crc(ct)
		if f.corrupt {
			sum += "1"
		}
		reply(map[string]any{"name": fieldsKey + "/cryptoKeyVersions/" + string(rune('0'+n)), "ciphertext": base64.StdEncoding.EncodeToString(ct),
			"ciphertextCrc32c": sum, "verifiedPlaintextCrc32c": crc(b64("plaintext")) == req["plaintextCrc32c"],
			"verifiedAdditionalAuthenticatedDataCrc32c": crc(b64("additionalAuthenticatedData")) == req["additionalAuthenticatedDataCrc32c"]})
	case path == fieldsKey+":decrypt":
		ct := b64("ciphertext")
		if len(ct) < 13 || int(ct[0]) < 1 || int(ct[0]) > len(f.versions) {
			http.Error(w, `{"error":{"code":400,"status":"INVALID_ARGUMENT"}}`, http.StatusBadRequest)
			return
		}
		gcm := fakeGCM(f.versions[ct[0]-1])
		plain, err := gcm.Open(nil, ct[1:13], ct[13:], b64("additionalAuthenticatedData"))
		if err != nil {
			http.Error(w, `{"error":{"code":400,"status":"INVALID_ARGUMENT"}}`, http.StatusBadRequest)
			return
		}
		reply(map[string]any{"plaintext": base64.StdEncoding.EncodeToString(plain), "plaintextCrc32c": crc(plain)})
	case path == macVersion+":macSign":
		h := hmac.New(sha256.New, f.mac)
		h.Write(b64("data"))
		mac := h.Sum(nil)
		reply(map[string]any{"name": macVersion, "mac": base64.StdEncoding.EncodeToString(mac), "macCrc32c": crc(mac),
			"verifiedDataCrc32c": crc(b64("data")) == req["dataCrc32c"]})
	default:
		http.NotFound(w, r)
	}
}

func fakeGCM(key []byte) cipher.AEAD {
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	return gcm
}

// The field keys stay in Cloud KMS: the adapter passes the Cipher suite against KMS's REST API, a rotation leaves
// earlier values readable, a value naming another key is never sent to KMS, and a corrupted answer is refused.
// [live-verify] against the staging key ring once p0-infra-opentofu applies it.
// req: NFR-PORT-04
// req: SEC-11
// asvs: V11.3.2, V11.3.3
func TestKMSFieldsConformance(t *testing.T) {
	f := &fakeFieldsKMS{mac: []byte("fingerprint key")}
	f.rotate()
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := NewKMSFields(srv.URL, fieldsKey, macVersion, nil, srv.Client())
	porttest.Cipher(t, c)

	ctx := context.Background()
	before, err := c.Seal(ctx, []byte("12345678"), []byte("r"))
	if err != nil {
		t.Fatal(err)
	}
	f.rotate()
	if got, err := c.Open(ctx, before, []byte("r")); err != nil || string(got) != "12345678" {
		t.Fatalf("a value sealed before the rotation: %q, %v", got, err)
	}

	other := NewKMSFields(srv.URL, strings.Replace(fieldsKey, "/fields", "/other", 1), macVersion, nil, srv.Client())
	foreign, err := other.Seal(ctx, []byte("12345678"), []byte("r"))
	if err == nil {
		t.Fatal("the fake encrypted under a key it does not hold")
	}
	foreign = bytes.Replace(before, []byte(fieldsKey), []byte(strings.Replace(fieldsKey, "/fields", "/fieldz", 1)), 1)
	calls := f.calls
	if _, err := c.Open(ctx, foreign, []byte("r")); !errors.Is(err, ports.ErrNotSealed) || f.calls != calls {
		t.Fatalf("a value naming another key: %v, %d calls to KMS", err, f.calls-calls)
	}

	f.corrupt = true
	if _, err := c.Seal(ctx, []byte("12345678"), []byte("r")); err == nil || !strings.Contains(err.Error(), "corrupted") {
		t.Fatalf("a corrupted ciphertext was accepted: %v", err)
	}
}
