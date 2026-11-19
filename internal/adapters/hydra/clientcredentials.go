package hydra

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
)

// TokenSource gets access tokens from an OAuth server's token endpoint for a confidential client that authenticates with private_key_jwt (RFC 7523, ES256)
// and asks for the client-credentials grant, for one audience: what the worker of either app uses to call the other's API (the link, p1-payroll-link design D1). The
// token is kept until shortly before it ends; it is never logged and never passed on to another audience (AI-36). The HTTP client is the egress guard's, to the token
// endpoint's host only.
type TokenSource struct {
	TokenURL string // the OAuth server's public token endpoint; the assertion's audience is exactly this
	ClientID string
	Key      *ecdsa.PrivateKey
	Audience string // the resource the token is for
	HTTP     *http.Client
	Clock    ports.Clock

	mu    sync.Mutex
	token map[string]cachedToken // by scope list
}

type cachedToken struct {
	value string
	until time.Time
}

// Token returns a token with the scopes, from the cache while it lasts.
func (t *TokenSource) Token(ctx context.Context, scopes ...string) (string, error) {
	key, now := strings.Join(scopes, " "), t.Clock.Now()
	t.mu.Lock()
	if c, ok := t.token[key]; ok && now.Add(30*time.Second).Before(c.until) {
		t.mu.Unlock()
		return c.value, nil
	}
	t.mu.Unlock()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: t.Key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{"iss": t.ClientID, "sub": t.ClientID, "aud": t.TokenURL, "jti": uuid.NewString(), "iat": now.Unix(), "exp": now.Add(time.Minute).Unix()})
	if err != nil {
		return "", err
	}
	jws, err := signer.Sign(claims)
	if err != nil {
		return "", err
	}
	assertion, err := jws.CompactSerialize()
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {key}, "audience": {t.Audience},
		"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"}, "client_assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := t.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("the token endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &out) != nil || out.AccessToken == "" {
		return "", errors.New("the token endpoint did not issue a token for the client (status " + resp.Status + ")") // never the answer: it may name the client
	}
	t.mu.Lock()
	if t.token == nil {
		t.token = map[string]cachedToken{}
	}
	t.token[key] = cachedToken{value: out.AccessToken, until: now.Add(time.Duration(out.ExpiresIn) * time.Second)}
	t.mu.Unlock()
	return out.AccessToken, nil
}
