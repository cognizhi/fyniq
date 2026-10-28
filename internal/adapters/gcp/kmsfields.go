package gcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/fieldseal"
	"github.com/cognizhi/fyniq/internal/ports"
)

// KMSFields keeps the keys of ports.Cipher in Cloud KMS (SEC-11): Key is a symmetric ENCRYPT_DECRYPT key
// (projects/…/cryptoKeys/…), whose primary version wraps each data key and whose every version unwraps one, so a
// rotation needs no re-encryption; MACVersion is one version of an HMAC_SHA256 key (projects/…/cryptoKeyVersions/N),
// pinned, because another version would change every fingerprint. Neither key leaves KMS. Every request and answer
// carries CRC32C checksums, as KMS advises.
type KMSFields struct {
	Endpoint   string
	Key        string
	MACVersion string
	r          rest
}

var _ fieldseal.KEK = (*KMSFields)(nil)

// NewKMSFields returns the Cipher on Cloud KMS.
func NewKMSFields(endpoint, key, macVersion string, tokens oauth2.TokenSource, client *http.Client) fieldseal.Sealer {
	k := &KMSFields{Endpoint: strings.TrimSuffix(endpoint, "/"), Key: key, MACVersion: macVersion, r: rest{tokens: tokens, client: client}}
	return fieldseal.Sealer{KEK: k, MAC: k.MAC}
}

// ID implements fieldseal.KEK: the key, whose versions KMS picks by itself.
func (k *KMSFields) ID() string { return k.Key }

// Wrap implements fieldseal.KEK with KMS's encrypt, the record bound as additional authenticated data.
func (k *KMSFields) Wrap(ctx context.Context, dek, aad []byte) ([]byte, error) {
	body, err := jsonBody(map[string]string{"plaintext": base64.StdEncoding.EncodeToString(dek), "plaintextCrc32c": crc(dek),
		"additionalAuthenticatedData": base64.StdEncoding.EncodeToString(aad), "additionalAuthenticatedDataCrc32c": crc(aad)})
	if err != nil {
		return nil, err
	}
	var out struct {
		Name                                      string `json:"name"`
		Ciphertext                                string `json:"ciphertext"`
		CiphertextCrc32c                          string `json:"ciphertextCrc32c"`
		VerifiedPlaintextCrc32c                   bool   `json:"verifiedPlaintextCrc32c"`
		VerifiedAdditionalAuthenticatedDataCrc32c bool   `json:"verifiedAdditionalAuthenticatedDataCrc32c"`
	}
	if err := k.r.do(ctx, http.MethodPost, fmt.Sprintf("%s/v1/%s:encrypt", k.Endpoint, k.Key), "application/json", body, &out); err != nil {
		return nil, fmt.Errorf("kms encrypt: %w", err)
	}
	wrapped, err := base64.StdEncoding.DecodeString(out.Ciphertext)
	switch {
	case err != nil:
		return nil, errors.New("kms encrypt: the ciphertext is not base64")
	case !out.VerifiedPlaintextCrc32c || !out.VerifiedAdditionalAuthenticatedDataCrc32c || !strings.HasPrefix(out.Name, k.Key+"/cryptoKeyVersions/"):
		return nil, errors.New("kms encrypt: KMS did not confirm what it was sent, or another key encrypted")
	case crc(wrapped) != out.CiphertextCrc32c:
		return nil, errors.New("kms encrypt: the ciphertext was corrupted in transit")
	}
	return wrapped, nil
}

// Unwrap implements fieldseal.KEK with KMS's decrypt, only for this deployment's key: a value that names another is
// not sent to KMS at all.
func (k *KMSFields) Unwrap(ctx context.Context, id string, wrapped, aad []byte) ([]byte, error) {
	if id != k.Key {
		return nil, ports.ErrNotSealed
	}
	body, err := jsonBody(map[string]string{"ciphertext": base64.StdEncoding.EncodeToString(wrapped), "ciphertextCrc32c": crc(wrapped),
		"additionalAuthenticatedData": base64.StdEncoding.EncodeToString(aad), "additionalAuthenticatedDataCrc32c": crc(aad)})
	if err != nil {
		return nil, err
	}
	var out struct {
		Plaintext       string `json:"plaintext"`
		PlaintextCrc32c string `json:"plaintextCrc32c"`
	}
	if err := k.r.do(ctx, http.MethodPost, fmt.Sprintf("%s/v1/%s:decrypt", k.Endpoint, k.Key), "application/json", body, &out); err != nil {
		if errors.Is(err, errInvalid) { // KMS answers 400 to a ciphertext or AAD it cannot decrypt
			return nil, ports.ErrNotSealed
		}
		return nil, fmt.Errorf("kms decrypt: %w", err)
	}
	dek, err := base64.StdEncoding.DecodeString(out.Plaintext)
	if err != nil || crc(dek) != out.PlaintextCrc32c {
		return nil, errors.New("kms decrypt: the data key was corrupted in transit")
	}
	return dek, nil
}

// MAC is the fingerprint key's HMAC-SHA256 with KMS's macSign, on the pinned version.
func (k *KMSFields) MAC(ctx context.Context, data []byte) ([]byte, error) {
	body, err := jsonBody(map[string]string{"data": base64.StdEncoding.EncodeToString(data), "dataCrc32c": crc(data)})
	if err != nil {
		return nil, err
	}
	var out struct {
		Name               string `json:"name"`
		MAC                string `json:"mac"`
		MACCrc32c          string `json:"macCrc32c"`
		VerifiedDataCrc32c bool   `json:"verifiedDataCrc32c"`
	}
	if err := k.r.do(ctx, http.MethodPost, fmt.Sprintf("%s/v1/%s:macSign", k.Endpoint, k.MACVersion), "application/json", body, &out); err != nil {
		return nil, fmt.Errorf("kms macSign: %w", err)
	}
	mac, err := base64.StdEncoding.DecodeString(out.MAC)
	switch {
	case err != nil:
		return nil, errors.New("kms macSign: the MAC is not base64")
	case !out.VerifiedDataCrc32c || out.Name != k.MACVersion:
		return nil, errors.New("kms macSign: KMS did not confirm the data, or another key version signed")
	case crc(mac) != out.MACCrc32c:
		return nil, errors.New("kms macSign: the MAC was corrupted in transit")
	}
	return mac, nil
}
