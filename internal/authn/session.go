package authn

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The session's lifetimes (SEC-05): it ends after SessionIdle without a request, and SessionLifetime
// after it began, whatever happens.
const (
	SessionIdle     = 30 * time.Minute
	SessionLifetime = 12 * time.Hour
)

// WebClient is the client of every first-party session: the web app.
const WebClient = "web"

// SessionCookie is the name of the session cookie. Over https it carries the __Host- prefix, so the
// browser accepts it only from this host, on every path, with Secure; on http://localhost (local mode)
// the prefix is not allowed, and the cookie is plain.
func SessionCookie(secure bool) string {
	if secure {
		return "__Host-fyniq_session"
	}
	return "fyniq_session"
}

// HashToken is what the store keeps of a session token.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// Sessions authenticates the web app's first-party session cookie (SEC-05, SEC-08). The cookie holds an
// opaque token; the store resolves it, on every request, to the sign-in identity, its user, their
// organisation and their live roles, so a role change applies at the next request.
type Sessions struct {
	Store  storage.Sessions
	Clock  ports.Clock
	Cookie string // SessionCookie(secure)
}

var _ Authenticator = Sessions{}

// Authenticate implements Authenticator. It reads only the cookie.
func (s Sessions) Authenticate(ctx context.Context, c Credentials) (Principal, error) {
	token := CookieValue(c.Cookie, s.Cookie)
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}
	sess, err := s.Store.Session(ctx, HashToken(token), s.Clock.Now(), SessionIdle)
	if errors.Is(err, ports.ErrNotFound) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	p := Principal{Via: ViaSession, Issuer: sess.Issuer, Subject: sess.Subject, UserID: sess.UserID, Provider: sess.Provider, Client: WebClient,
		Class: ClassUser, Organisation: sess.Organisation, StepUpAt: sess.StepUpAt}
	for _, r := range sess.Roles {
		if _, known := authz.Roles[authz.Role(r)]; known {
			p.Roles = append(p.Roles, authz.Role(r))
		}
	}
	return p, nil
}

// CookieValue returns the value of the cookie named name in a Cookie header, or "".
func CookieValue(header, name string) string {
	if header == "" {
		return ""
	}
	cookies, err := http.ParseCookie(header)
	if err != nil {
		return ""
	}
	for _, c := range cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// SameOrigin reports whether a browser sent the request from the web app's own origin, by its Fetch
// Metadata (Sec-Fetch-Site), which a page cannot set. A request a session cookie authenticates may
// change state only when it is: the cookie is ambient, so any site could otherwise send it (CSRF; OWASP
// Fetch Metadata). Every browser Fyniq supports sends the header; without it, the request is refused.
func SameOrigin(r http.Header) bool {
	return r.Get("Sec-Fetch-Site") == "same-origin"
}

// Safe reports whether an HTTP method only reads.
func Safe(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
