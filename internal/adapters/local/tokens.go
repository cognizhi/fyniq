//go:build local

package local

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// HMACTokens are local task credentials: class, expiry and an HMAC-SHA-256 over both, with a key the
// local services share. In production Cloud Tasks mints OIDC tokens for each class's service
// account and the handler checks the audience (p0-infra-opentofu).
type HMACTokens struct {
	key   []byte
	clock ports.Clock
	ttl   time.Duration
}

var _ ports.TaskTokens = (*HMACTokens)(nil)

// NewHMACTokens signs with key; tokens last ttl by clock.
func NewHMACTokens(key []byte, clock ports.Clock, ttl time.Duration) *HMACTokens {
	return &HMACTokens{key: key, clock: clock, ttl: ttl}
}

func (h *HMACTokens) mac(msg string) string {
	m := hmac.New(sha256.New, h.key)
	m.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Token implements ports.TaskTokens.
func (h *HMACTokens) Token(class string) (string, error) {
	if class == "" || strings.Contains(class, ".") {
		return "", fmt.Errorf("worker class %q cannot have a token", class)
	}
	msg := class + "." + strconv.FormatInt(h.clock.Now().Add(h.ttl).Unix(), 10)
	return msg + "." + h.mac(msg), nil
}

// Verify implements ports.TaskTokens.
func (h *HMACTokens) Verify(token, class string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != class {
		return ports.ErrUnauthorised
	}
	msg := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(h.mac(msg)), []byte(parts[2])) {
		return ports.ErrUnauthorised
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || h.clock.Now().Unix() > exp {
		return ports.ErrUnauthorised
	}
	return nil
}
