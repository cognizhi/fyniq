//go:build !local

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/version"
)

type noSessions struct{}

func (noSessions) StartSession(context.Context, storage.NewSession) error { return nil }
func (noSessions) Session(context.Context, []byte, time.Time, time.Duration) (storage.Session, error) {
	return storage.Session{}, ports.ErrNotFound
}
func (noSessions) EndSession(context.Context, []byte, time.Time) error { return nil }

func configure(t *testing.T, origin string) {
	t.Setenv("FYNIQ_SIGNIN", "identity-platform")
	t.Setenv("FYNIQ_PUBLIC_ORIGIN", origin)
	t.Setenv("FYNIQ_IDP_PROJECT", "fyniq-staging")
	t.Setenv("FYNIQ_IDP_API_KEY", "test-only-key")
	providers, _ := json.Marshal([]map[string]string{{"id": "zoho", "name": "Zoho", "idp": "oidc.zoho", "issuer": "https://accounts.zoho.com", "client_id": "1000.FYNIQ"}})
	t.Setenv("FYNIQ_SIGNIN_PROVIDERS", string(providers))
}

// The release build accepts no credential until the deployment configures sign-in completely; then it
// serves the sign-in flow through Identity Platform and takes the session cookie, and never a local dev
// token (DEV-07). An http origin, a provider without its issuer or a provider listed twice keeps it off.
// req: FR-ADM-08
// req: SEC-05
// req: DEV-07
// asvs: V3.3.1, V10.5.3
func TestTheReleaseBuildSignsInOnlyWhenConfigured(t *testing.T) {
	c := clock.NewFrozen(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	t.Setenv("FYNIQ_SIGNIN", "")
	if _, ok := newAuthenticator(c, noSessions{}).(authn.NotYet); !ok {
		t.Fatal("without sign-in configured the release build accepts a credential")
	}
	s := httpserver.New(version.Info{Service: "api"})
	if registerSignIn(s, c, noSessions{}) != nil {
		t.Fatal("without sign-in configured the release build signs people in")
	}

	for name, origin := range map[string]string{"an http origin": "http://fyniq.example.test", "an origin with a path": "https://fyniq.example.test/app"} {
		configure(t, origin)
		if _, err := releaseSignIn(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	configure(t, "https://fyniq.example.test")
	for name, providers := range map[string]string{
		"no issuer":   `[{"id":"zoho","name":"Zoho","idp":"oidc.zoho","client_id":"c"}]`,
		"http issuer": `[{"id":"zoho","name":"Zoho","idp":"oidc.zoho","issuer":"http://accounts.zoho.com","client_id":"c"}]`,
		"listed twice": `[{"id":"zoho","name":"Zoho","idp":"oidc.zoho","issuer":"https://accounts.zoho.com","client_id":"c"},` +
			`{"id":"zoho","name":"Zoho","idp":"oidc.zoho","issuer":"https://accounts.zoho.com","client_id":"c"}]`,
		"a bad id": `[{"id":"Zoho.US","name":"Zoho","idp":"oidc.zoho","issuer":"https://accounts.zoho.com","client_id":"c"}]`,
		"none":     `[]`,
	} {
		t.Setenv("FYNIQ_SIGNIN_PROVIDERS", providers)
		if _, err := releaseSignIn(); err == nil {
			t.Errorf("providers with %s were accepted", name)
		}
	}

	configure(t, "https://fyniq.example.test")
	auth := newAuthenticator(c, noSessions{})
	if _, ok := auth.(authn.Release); !ok {
		t.Fatalf("configured, the release build's authenticator is %T", auth)
	}
	if _, err := auth.Authenticate(context.Background(), authn.Credentials{Authorization: "Bearer " + authn.DevPrefix + "a.b"}); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Error("the release build took a dev token")
	}
	s = httpserver.New(version.Info{Service: "api"})
	p := registerSignIn(s, c, noSessions{})
	if p == nil || len(p.Providers()) != 1 || p.Providers()[0].ID != "zoho" {
		t.Fatalf("the configured providers: %v", p)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/signin?provider=github", nil))
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "AUTH-SIGNIN-01") {
		t.Errorf("the sign-in flow is not served: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	for _, c := range rec.Result().Cookies() {
		t.Errorf("an unknown provider set the cookie %s", c.Name)
	}
}
