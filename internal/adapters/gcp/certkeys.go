package gcp

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/cognizhi/fyniq/internal/ports"
)

// IdentityPlatformCertsURL is where Google publishes the keys that sign Identity Platform's ID tokens, as a
// map of key ID to an X.509 certificate (firebase.google.com/docs/auth/admin/verify-id-tokens,
// checked 15 Oct 2026).
const IdentityPlatformCertsURL = "https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com"

// CertKeys verify RS256 tokens with the certificates published at URL, cached for the max-age of the
// response's Cache-Control (as the publisher asks) and fetched again at most once a minute for an
// unknown key ID. It implements go-oidc's KeySet.
type CertKeys struct {
	URL    string
	Client *http.Client
	Clock  ports.Clock

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	until   time.Time // the cache is fresh before then
	fetched time.Time
}

// minRefetch limits fetches for unknown key IDs, so a flood of forged tokens cannot hammer Google.
const minRefetch = time.Minute

// VerifySignature implements oidc.KeySet: the token's payload when its RS256 signature verifies with
// the key its header names.
func (c *CertKeys) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	jws, err := jose.ParseSigned(jwt, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return nil, fmt.Errorf("the token is not an RS256 JWS: %w", err)
	}
	if len(jws.Signatures) != 1 {
		return nil, errors.New("the token has not exactly one signature")
	}
	kid := jws.Signatures[0].Header.KeyID
	key, err := c.key(ctx, kid)
	if err != nil {
		return nil, err
	}
	return jws.Verify(key)
}

func (c *CertKeys) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Clock.Now()
	if k, ok := c.keys[kid]; ok && now.Before(c.until) {
		return k, nil
	}
	if c.keys == nil || !now.Before(c.until) || now.Sub(c.fetched) >= minRefetch {
		if err := c.fetch(ctx, now); err != nil {
			return nil, err
		}
	}
	if k, ok := c.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("no published key has the ID %q", kid)
}

func (c *CertKeys) fetch(ctx context.Context, now time.Time) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return err
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch the signing keys: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch the signing keys: %s", resp.Status)
	}
	var certs map[string]string
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&certs); err != nil {
		return fmt.Errorf("read the signing keys: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(certs))
	for kid, p := range certs {
		block, _ := pem.Decode([]byte(p))
		if block == nil {
			return fmt.Errorf("signing key %q is not PEM", kid)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("signing key %q: %w", kid, err)
		}
		pub, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("signing key %q is not RSA", kid)
		}
		keys[kid] = pub
	}
	c.keys, c.fetched, c.until = keys, now, now.Add(maxAge(resp.Header.Get("Cache-Control")))
	return nil
}

// maxAge reads Cache-Control's max-age; without one the keys are fetched again after a minute.
func maxAge(cacheControl string) time.Duration {
	for _, part := range strings.Split(cacheControl, ",") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(part), "max-age="); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return time.Duration(n) * time.Second
			}
		}
	}
	return minRefetch
}
