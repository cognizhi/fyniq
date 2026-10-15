//go:build local

package local

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
)

// DevTokenKey signs the local issuer's dev tokens. Local only, valid nowhere else (SEC-12).
const DevTokenKey = "fyniq-local-only-dev-token-key"

// DevTokens are the local issuer of API bearer tokens until the OAuth server arrives
// (p0-identity-oauth-first-run): a principal, with its organisation and roles, and an expiry,
// signed with HMAC-SHA-256 under a local key. They exist only in the local build (DEV-07), so a
// release binary cannot accept one.
type DevTokens struct {
	key   []byte
	clock ports.Clock
}

var _ authn.Authenticator = (*DevTokens)(nil)

// NewDevTokens signs and checks with key.
func NewDevTokens(key []byte, clock ports.Clock) *DevTokens {
	return &DevTokens{key: key, clock: clock}
}

const devPrefix = "fyniq-dev."

type devClaims struct {
	Subject      string       `json:"sub"`
	Client       string       `json:"client"`
	Class        string       `json:"class"`
	Organisation uuid.UUID    `json:"org"`
	Roles        []authz.Role `json:"roles"`
	Expires      int64        `json:"exp"`
}

func (d *DevTokens) mac(msg string) string {
	m := hmac.New(sha256.New, d.key)
	m.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Issue returns a token for p that lasts ttl.
func (d *DevTokens) Issue(p authn.Principal, ttl time.Duration) (string, error) {
	if p.Subject == "" || p.Client == "" {
		return "", errors.New("a dev token needs a subject and a client")
	}
	if p.Class == "" {
		p.Class = authn.ClassUser
	}
	b, err := json.Marshal(devClaims{Subject: p.Subject, Client: p.Client, Class: p.Class, Organisation: p.Organisation,
		Roles: p.Roles, Expires: d.clock.Now().Add(ttl).Unix()})
	if err != nil {
		return "", err
	}
	msg := devPrefix + base64.RawURLEncoding.EncodeToString(b)
	return msg + "." + d.mac(msg), nil
}

// Authenticate implements authn.Authenticator: a bearer dev token, signed and not expired.
func (d *DevTokens) Authenticate(_ context.Context, authorization string) (authn.Principal, error) {
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || !strings.HasPrefix(token, devPrefix) {
		return authn.Principal{}, authn.ErrUnauthenticated
	}
	cut := strings.LastIndexByte(token, '.')
	msg, sig := token[:cut], token[cut+1:]
	if !hmac.Equal([]byte(d.mac(msg)), []byte(sig)) {
		return authn.Principal{}, authn.ErrUnauthenticated
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(msg, devPrefix))
	if err != nil {
		return authn.Principal{}, authn.ErrUnauthenticated
	}
	var c devClaims
	if err := json.Unmarshal(b, &c); err != nil || d.clock.Now().Unix() > c.Expires {
		return authn.Principal{}, authn.ErrUnauthenticated
	}
	return authn.Principal{Subject: c.Subject, Client: c.Client, Class: c.Class, Organisation: c.Organisation, Roles: c.Roles}, nil
}
