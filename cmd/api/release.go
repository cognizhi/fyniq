//go:build !local

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/cognizhi/fyniq/internal/adapters/gcp"
	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/logging"
	"github.com/cognizhi/fyniq/internal/oauthflow"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/signin"
)

// The release build signs people in through Identity Platform (ADR-T3) once the deployment configures
// it, and accepts Hydra's tokens for the API once the OAuth server is configured. Until then it accepts
// no credential at all. The configuration (p0-infra-opentofu sets it; the key comes from Secret Manager):
//
//	FYNIQ_SIGNIN=identity-platform
//	FYNIQ_PUBLIC_ORIGIN       the web app's https origin, where the browser comes back
//	FYNIQ_IDP_PROJECT         the Identity Platform project; FYNIQ_IDP_TENANT optional
//	FYNIQ_IDP_API_KEY         its web API key
//	FYNIQ_SIGNIN_PROVIDERS    JSON: [{"id":"zoho","name":"Zoho","idp":"oidc.zoho","issuer":"https://accounts.zoho.com","client_id":"…"}]
//	FYNIQ_OAUTH_ADMIN_URL     Hydra's admin API (optional)
func init() {
	newAuthenticator = func(clock ports.Clock, sessions storage.Sessions) authn.Authenticator {
		r := authn.Release{}
		if admin := os.Getenv("FYNIQ_OAUTH_ADMIN_URL"); admin != "" {
			r.OAuth = &authn.Introspector{OAuth: hydra.New(admin), Audience: os.Getenv("FYNIQ_API_AUDIENCE"), Clock: clock, TTL: authn.MaxCache}
		}
		if _, err := releaseSignIn(); err == nil && sessions != nil {
			r.Sessions = authn.Sessions{Store: sessions, Clock: clock, Cookie: authn.SessionCookie(true)}
		}
		if r.OAuth == nil && r.Sessions == nil {
			return authn.NotYet{}
		}
		return r
	}
	registerSignIn = func(s *httpserver.Server, clock ports.Clock, sessions storage.Sessions, alert signin.Alert) ports.SignIn {
		log := logging.New(os.Stderr, slog.LevelInfo)
		c, err := releaseSignIn()
		if err != nil {
			log.Info("api.signin.off", "reason", err.Error())
			return nil
		}
		if sessions == nil {
			log.Warn("api.signin.off", "reason", "no database")
			return nil
		}
		c.platform.Clock = clock
		c.platform.Keys = &gcp.CertKeys{URL: gcp.IdentityPlatformCertsURL, Clock: clock}
		signin.Flow{SignIn: c.platform, Sessions: sessions, Clock: clock, Origin: c.origin, Base: "/api", Log: log, Alert: alert}.Register(s)
		return c.platform
	}
	webOrigin = func() string {
		c, err := releaseSignIn()
		if err != nil {
			return "" // nobody signs in, so nobody registers a passkey
		}
		return c.origin
	}
	registerOAuth = func(s *httpserver.Server, clock ports.Clock, sessions storage.Sessions) {
		admin := os.Getenv("FYNIQ_OAUTH_ADMIN_URL")
		if admin == "" || sessions == nil {
			return
		}
		if _, err := releaseSignIn(); err != nil {
			return // nobody can sign in, so nobody can consent
		}
		signedIn := authn.Release{Sessions: authn.Sessions{Store: sessions, Clock: clock, Cookie: authn.SessionCookie(true)}}
		oauthflow.Flow{OAuth: hydra.New(admin), SignedIn: signedIn, Clock: clock, APIAudience: os.Getenv("FYNIQ_API_AUDIENCE"),
			MCPAudience: os.Getenv("FYNIQ_MCP_AUDIENCE"), Log: logging.New(os.Stderr, slog.LevelInfo), SignInPage: "/", Base: "/api"}.Register(s)
	}
}

var providerID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

type releaseConfig struct {
	platform *gcp.IdentityPlatform
	origin   string
}

// releaseSignIn reads the sign-in configuration; any gap keeps sign-in off, with the reason.
func releaseSignIn() (releaseConfig, error) {
	if os.Getenv("FYNIQ_SIGNIN") != "identity-platform" {
		return releaseConfig{}, errors.New("FYNIQ_SIGNIN is not identity-platform")
	}
	origin := os.Getenv("FYNIQ_PUBLIC_ORIGIN")
	if !strings.HasPrefix(origin, "https://") || strings.Count(origin, "/") != 2 {
		return releaseConfig{}, errors.New("FYNIQ_PUBLIC_ORIGIN must be the web app's https origin, such as https://fyniq.example.com")
	}
	p := &gcp.IdentityPlatform{Endpoint: gcp.IdentityToolkit, APIKey: os.Getenv("FYNIQ_IDP_API_KEY"), Project: os.Getenv("FYNIQ_IDP_PROJECT"),
		Tenant: os.Getenv("FYNIQ_IDP_TENANT")}
	if p.APIKey == "" || p.Project == "" {
		return releaseConfig{}, errors.New("FYNIQ_IDP_PROJECT and FYNIQ_IDP_API_KEY are required")
	}
	if err := json.Unmarshal([]byte(os.Getenv("FYNIQ_SIGNIN_PROVIDERS")), &p.Federated); err != nil || len(p.Federated) == 0 {
		return releaseConfig{}, errors.New("FYNIQ_SIGNIN_PROVIDERS must list at least one provider as JSON")
	}
	seen := map[string]bool{}
	for _, x := range p.Federated {
		switch {
		case !providerID.MatchString(x.ID) || x.Name == "" || x.IdP == "" || x.ClientID == "":
			return releaseConfig{}, fmt.Errorf("sign-in provider %q needs an id (lower-case letters, digits, hyphens), a name, an idp and a client_id", x.ID)
		case !strings.HasPrefix(x.Issuer, "https://"):
			return releaseConfig{}, fmt.Errorf("sign-in provider %q needs its issuer, an https URL (FR-ADM-08)", x.ID)
		case seen[x.ID]:
			return releaseConfig{}, fmt.Errorf("sign-in provider %q is listed twice", x.ID)
		}
		seen[x.ID] = true
	}
	return releaseConfig{platform: p, origin: origin}, nil
}
