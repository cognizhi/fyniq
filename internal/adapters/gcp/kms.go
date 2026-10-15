package gcp

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"hash/crc32"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

// KMSAPI is Cloud KMS's REST API.
const KMSAPI = "https://cloudkms.googleapis.com"

// KMS is the Signer port on a Cloud KMS asymmetric key (EC_SIGN_P256_SHA256): the audit anchors'
// signing key (§11.5 L3, CTL-AUD-02) never leaves KMS, and its public key lets anyone verify an anchor
// offline. KeyVersion is the version that signs (projects/…/cryptoKeys/…/cryptoKeyVersions/N); every
// version of the same key stays verifiable after a rotation.
type KMS struct {
	Endpoint   string
	KeyVersion string
	r          rest

	mu   sync.Mutex
	pubs map[string]*ecdsa.PublicKey
}

var _ ports.Signer = (*KMS)(nil)

// NewKMS returns the adapter.
func NewKMS(endpoint, keyVersion string, tokens oauth2.TokenSource, client *http.Client) *KMS {
	return &KMS{Endpoint: strings.TrimSuffix(endpoint, "/"), KeyVersion: keyVersion, r: rest{tokens: tokens, client: client},
		pubs: map[string]*ecdsa.PublicKey{}}
}

// KeyID implements ports.Signer.
func (k *KMS) KeyID() string { return k.KeyVersion }

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// Sign implements ports.Signer: KMS signs the SHA-256 of msg. The request and the answer carry
// CRC32C checksums, so a corrupted digest or signature in transit is caught (as KMS advises).
func (k *KMS) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	digest := sha256.Sum256(msg)
	body, err := jsonBody(map[string]any{"digest": map[string]string{"sha256": base64.StdEncoding.EncodeToString(digest[:])},
		"digestCrc32c": strconv.FormatUint(uint64(crc32.Checksum(digest[:], castagnoli)), 10)})
	if err != nil {
		return nil, err
	}
	var out struct {
		Signature            string `json:"signature"`
		SignatureCrc32c      string `json:"signatureCrc32c"`
		VerifiedDigestCrc32c bool   `json:"verifiedDigestCrc32c"`
		Name                 string `json:"name"`
	}
	if err := k.r.do(ctx, http.MethodPost, fmt.Sprintf("%s/v1/%s:asymmetricSign", k.Endpoint, k.KeyVersion), "application/json", body, &out); err != nil {
		return nil, fmt.Errorf("kms sign: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(out.Signature)
	switch {
	case err != nil:
		return nil, errors.New("kms sign: the signature is not base64")
	case !out.VerifiedDigestCrc32c || out.Name != k.KeyVersion:
		return nil, errors.New("kms sign: KMS did not confirm the digest, or another key version signed")
	case strconv.FormatUint(uint64(crc32.Checksum(sig, castagnoli)), 10) != out.SignatureCrc32c:
		return nil, errors.New("kms sign: the signature was corrupted in transit")
	}
	return sig, nil
}

// Verify implements ports.Signer: any version of the configured key, with its public key from KMS
// (cached; a version's public key never changes).
func (k *KMS) Verify(keyID string, msg, sig []byte) error {
	key, _, _ := strings.Cut(k.KeyVersion, "/cryptoKeyVersions/")
	if !strings.HasPrefix(keyID, key+"/cryptoKeyVersions/") {
		return ports.ErrBadSignature // never fetch a key an anchor names but this deployment does not own
	}
	pub, err := k.publicKey(keyID)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(msg)
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		return ports.ErrBadSignature
	}
	return nil
}

func (k *KMS) publicKey(version string) (*ecdsa.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if p, ok := k.pubs[version]; ok {
		return p, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out struct {
		PEM       string `json:"pem"`
		Algorithm string `json:"algorithm"`
	}
	if err := k.r.do(ctx, http.MethodGet, fmt.Sprintf("%s/v1/%s/publicKey", k.Endpoint, version), "", nil, &out); err != nil {
		return nil, fmt.Errorf("kms public key: %w", err)
	}
	if out.Algorithm != "EC_SIGN_P256_SHA256" {
		return nil, fmt.Errorf("kms public key: the key is %s, not EC_SIGN_P256_SHA256", out.Algorithm)
	}
	block, _ := pem.Decode([]byte(out.PEM))
	if block == nil {
		return nil, errors.New("kms public key: not PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("kms public key: %w", err)
	}
	pub, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("kms public key: not an EC key")
	}
	k.pubs[version] = pub
	return pub, nil
}
