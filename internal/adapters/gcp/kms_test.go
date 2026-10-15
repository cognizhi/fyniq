package gcp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

const keyName = "projects/fyniq-staging/locations/asia-southeast1/keyRings/audit/cryptoKeys/anchor"

// fakeKMS signs like Cloud KMS's asymmetricSign for one EC P-256 key with two versions, and serves
// their public keys.
type fakeKMS struct {
	mu       sync.Mutex
	keys     map[string]*ecdsa.PrivateKey
	corrupt  bool
	pubFetch int
	auth     string
}

func (f *fakeKMS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = r.Header.Get("Authorization")
	path := strings.TrimPrefix(r.URL.Path, "/v1/")
	if version, ok := strings.CutSuffix(path, ":asymmetricSign"); ok && r.Method == http.MethodPost {
		var req struct {
			Digest struct {
				SHA256 string `json:"sha256"`
			} `json:"digest"`
			DigestCrc32c string `json:"digestCrc32c"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		digest, _ := base64.StdEncoding.DecodeString(req.Digest.SHA256)
		ok := strconv.FormatUint(uint64(crc32.Checksum(digest, castagnoli)), 10) == req.DigestCrc32c
		sig, _ := ecdsa.SignASN1(rand.Reader, f.keys[version], digest)
		crc := crc32.Checksum(sig, castagnoli)
		if f.corrupt {
			crc++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"signature": base64.StdEncoding.EncodeToString(sig), "signatureCrc32c": strconv.FormatUint(uint64(crc), 10),
			"verifiedDigestCrc32c": ok, "name": version})
		return
	}
	if version, ok := strings.CutSuffix(path, "/publicKey"); ok {
		k, found := f.keys[version]
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.pubFetch++
		der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
		_ = json.NewEncoder(w).Encode(map[string]any{"pem": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), "algorithm": "EC_SIGN_P256_SHA256", "name": version})
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

// The audit anchors are signed in Cloud KMS: the adapter sends the SHA-256 with its CRC32C, checks
// KMS's checksums and key version, and verifies a signature with the public key of the version that
// made it, so a rotated key's old anchors still verify. A tampered message or signature, a key of
// another deployment and a signature corrupted in transit are refused.
// req: CTL-AUD-02
// req: INV-12
// asvs: V11.4.2, V13.3.1
func TestKMSSignsAndVerifiesAnchors(t *testing.T) {
	v1, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	v2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	f := &fakeKMS{keys: map[string]*ecdsa.PrivateKey{keyName + "/cryptoKeyVersions/1": v1, keyName + "/cryptoKeyVersions/2": v2}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	tokens := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "service-token"})
	ctx := context.Background()

	old := NewKMS(srv.URL, keyName+"/cryptoKeyVersions/1", tokens, nil)
	anchor := []byte(`{"org":"0192f1a0-0000-7000-8000-000000000001","seq":42,"head":"ab12"}`)
	sig1, err := old.Sign(ctx, anchor)
	if err != nil || f.auth != "Bearer service-token" {
		t.Fatalf("Sign: %v (auth %q)", err, f.auth)
	}
	k := NewKMS(srv.URL, keyName+"/cryptoKeyVersions/2", tokens, nil) // after a rotation
	sig2, err := k.Sign(ctx, anchor)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Verify(old.KeyID(), anchor, sig1); err != nil {
		t.Errorf("an anchor signed before the rotation: %v", err)
	}
	if err := k.Verify(k.KeyID(), anchor, sig2); err != nil {
		t.Errorf("an anchor signed now: %v", err)
	}
	fetches := f.pubFetch
	_ = k.Verify(k.KeyID(), anchor, sig2)
	if f.pubFetch != fetches {
		t.Error("a public key was fetched again: versions never change, so it is cached")
	}
	tampered := append([]byte(nil), anchor...)
	tampered[len(tampered)-2] = 'x'
	for name, check := range map[string]error{
		"a tampered anchor":                k.Verify(k.KeyID(), tampered, sig2),
		"another version's signature":      k.Verify(old.KeyID(), anchor, sig2),
		"a key of another deployment":      k.Verify("projects/attacker/locations/x/keyRings/y/cryptoKeys/z/cryptoKeyVersions/1", anchor, sig2),
		"a version that does not exist":    k.Verify(keyName+"/cryptoKeyVersions/9", anchor, sig2),
		"another key in the same key ring": k.Verify(strings.Replace(keyName, "anchor", "other", 1)+"/cryptoKeyVersions/1", anchor, sig2),
	} {
		if check == nil {
			t.Errorf("%s verified", name)
		}
	}
	if err := k.Verify("projects/attacker/locations/x/keyRings/y/cryptoKeys/z/cryptoKeyVersions/1", anchor, sig2); !errors.Is(err, ports.ErrBadSignature) {
		t.Errorf("a foreign key is refused without a fetch: %v", err)
	}
	f.corrupt = true
	if _, err := k.Sign(ctx, anchor); err == nil {
		t.Error("a signature corrupted in transit was accepted")
	}
}
