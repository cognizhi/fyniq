package oauthflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// Metadata is the authorisation server's metadata (RFC 8414) as Fyniq publishes it for MCP clients (AI-36, AI-90): the OAuth
// server's own endpoints, S256 PKCE only and public clients only, and, while an Owner has registration on, the registration endpoint
// of Fyniq's own (the OAuth server's own is off). It is built from the issuer, not fetched, so nothing is called to answer it. A
// client that finds no registration endpoint uses the client ID it was given, which is how a pre-registered app connects.
type Metadata struct {
	Issuer string // the OAuth server's issuer, with its path: http://localhost:8080/oauth/
	// RegisterURL is where an AI app registers: the API's /v1/oauth/register as the browser reaches it.
	RegisterURL string
	// RegistrationOpen says whether an Owner has registration on now; nil or false leaves the endpoint out.
	RegistrationOpen func(ctx context.Context) bool
	// Scopes are what a client may ask for in the metadata's words.
	Scopes []string
}

// Register adds the metadata at the issuer's well-known path (RFC 8414 section 3.1: the issuer's path after the well-known
// segment, its trailing slash removed).
func (m Metadata) Register(mux interface {
	HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request))
}) {
	u, err := url.Parse(m.Issuer)
	if err != nil {
		return
	}
	path := "/.well-known/oauth-authorization-server" + strings.TrimSuffix(u.Path, "/")
	mux.HandleFunc("GET "+path, m.serve)
}

func (m Metadata) serve(w http.ResponseWriter, r *http.Request) {
	issuer := m.Issuer
	doc := map[string]any{
		"issuer": issuer, "authorization_endpoint": issuer + "oauth2/auth", "token_endpoint": issuer + "oauth2/token",
		"revocation_endpoint": issuer + "oauth2/revoke", "jwks_uri": issuer + ".well-known/jwks.json",
		"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported": m.Scopes, "authorization_response_iss_parameter_supported": false,
	}
	if m.RegisterURL != "" && m.RegistrationOpen != nil && m.RegistrationOpen(r.Context()) {
		doc["registration_endpoint"] = m.RegisterURL
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store") // an Owner may close registration at once
	_ = json.NewEncoder(w).Encode(doc)
}
