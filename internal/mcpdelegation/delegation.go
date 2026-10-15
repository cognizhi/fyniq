// Package mcpdelegation is open question 40's option (b), the P0 spike (ADR-T3, AI-36): the MCP server
// gets an API-audience token for the person behind an MCP token it has just validated, with the RFC
// 7523 JWT-bearer grant of Hydra's Apache-2.0 build, signed by the MCP server's own key. The MCP token
// itself never reaches the API (no passthrough). Only cmd/mcp uses it, so choosing option (a) or (c)
// replaces this package alone. The spike's limits and measurements are in
// docs/adr/ADR-T3-mcp-delegation-spike.md, for the security reviewer.
package mcpdelegation

import (
	"context"
	"crypto"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/ports"
)

// MaxLifetime is the longest a delegated token may live (open question 40: at most 5 minutes).
const MaxLifetime = 5 * time.Minute

// assertionLifetime is how long the assertion itself is good for: one use, just now.
const assertionLifetime = time.Minute

// Delegator obtains delegated API tokens.
type Delegator struct {
	MCP         authn.Authenticator // the MCP audience's introspector: the MCP token must be valid now
	Key         crypto.Signer       // the MCP server's key: an EC P-256 key (ES256); Cloud KMS in the cloud
	KeyID       string
	Issuer      string // the trust relationship's issuer
	TokenURL    string // Hydra's token endpoint: the assertion's audience
	ClientID    string // the MCP server's delegation client (JWT-bearer only, agent class)
	Secret      string
	APIAudience string
	Clock       ports.Clock
	HTTP        *http.Client
}

// Token is a delegated API token.
type Token struct {
	AccessToken string
	Subject     string
	Scopes      []string
	Expires     time.Time
}

// ErrRefused is a delegation refused: the MCP token is not valid, not an agent's, or the OAuth server
// refused the grant.
var ErrRefused = errors.New("the delegation was refused")

// APIToken returns an API-audience token for the subject of the MCP token in authorization, for the
// MCP token's scopes, which the OAuth server intersects with the trust relationship's and the API with
// the person's rights now. It validates the MCP token first, so it never asks for anyone else.
func (d Delegator) APIToken(ctx context.Context, authorization string) (Token, error) {
	p, err := d.MCP.Authenticate(ctx, authn.Credentials{Authorization: authorization})
	if err != nil {
		return Token{}, errors.Join(ErrRefused, err)
	}
	if p.Via != authn.ViaToken || !p.AI() || p.Subject == "" {
		return Token{}, errors.Join(ErrRefused, errors.New("only an agent's valid MCP token can be delegated (AI-34)"))
	}
	assertion, err := d.assertion(p.Subject)
	if err != nil {
		return Token{}, err
	}
	scopes := slices.DeleteFunc(slices.Clone(p.Scopes), func(s string) bool { return s == "openid" || s == "offline_access" })
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion},
		"scope": {strings.Join(scopes, " ")}, "audience": {d.APIAudience}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(d.ClientID), url.QueryEscape(d.Secret))
	client := d.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("the OAuth server did not answer: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(raw, &out); err != nil {
		return Token{}, fmt.Errorf("the OAuth server's answer cannot be read (%s)", resp.Status)
	}
	if resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		return Token{}, errors.Join(ErrRefused, fmt.Errorf("%s: %s", out.Error, out.Description))
	}
	life := time.Duration(out.ExpiresIn) * time.Second
	if life <= 0 || life > MaxLifetime {
		return Token{}, errors.Join(ErrRefused, fmt.Errorf("the delegated token lives %s; at most %s", life, MaxLifetime))
	}
	return Token{AccessToken: out.AccessToken, Subject: p.Subject, Scopes: strings.Fields(out.Scope), Expires: d.Clock.Now().Add(life)}, nil
}

// assertion is the RFC 7523 JWT: issued by the MCP server, about subject, for Hydra's token endpoint,
// good for a minute and once (jti).
func (d Delegator) assertion(subject string) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: jose.JSONWebKey{Key: d.Key, KeyID: d.KeyID}},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", err
	}
	now := d.Clock.Now()
	claims := map[string]any{"iss": d.Issuer, "sub": subject, "aud": d.TokenURL, "iat": now.Unix(), "nbf": now.Unix(),
		"exp": now.Add(assertionLifetime).Unix(), "jti": rand.Text()}
	b, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	jws, err := signer.Sign(b)
	if err != nil {
		return "", err
	}
	return jws.CompactSerialize()
}

// PublicKey is the JWK to register in the trust relationship.
func (d Delegator) PublicKey() jose.JSONWebKey {
	return jose.JSONWebKey{Key: d.Key.Public(), KeyID: d.KeyID, Algorithm: string(jose.ES256), Use: "sig"}
}
