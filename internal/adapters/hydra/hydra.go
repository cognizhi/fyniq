// Package hydra is the OAuthServer adapter for Ory Hydra's admin API (ADR-T3), Apache-2.0 build
// v26.2.0, pinned by digest (Image). It speaks plain HTTP and JSON; no SDK. The admin API is never
// public: locally it is the oauth container's port 4445, in the cloud an internal-only service.
package hydra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Image is the pinned Hydra image (deploy/local/compose.yaml uses the same; a test keeps them equal).
const Image = "oryd/hydra:v26.2.0@sha256:ff67c7fb5f95074fa53374d41151713554960504b340cd3f95b09e65deaea2a9"

// Admin is Hydra's admin API at BaseURL.
type Admin struct {
	BaseURL string
	HTTP    *http.Client
}

var _ ports.OAuthServer = (*Admin)(nil)

// New returns the admin API at baseURL, with a short timeout.
func New(baseURL string) *Admin {
	return &Admin{BaseURL: strings.TrimSuffix(baseURL, "/"), HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// do sends a request to the admin API and decodes a JSON answer into out.
func (a *Admin) do(ctx context.Context, method, path string, query url.Values, body any, form url.Values, out any) error {
	var rd io.Reader
	contentType := ""
	switch {
	case body != nil:
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd, contentType = bytes.NewReader(b), "application/json"
	case form != nil:
		rd, contentType = strings.NewReader(form.Encode()), "application/x-www-form-urlencoded"
	}
	u := a.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("hydra %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ports.ErrNoChallenge
	case resp.StatusCode >= 300:
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(raw, &e)
		return fmt.Errorf("hydra %s %s: %d %s: %s", method, path, resp.StatusCode, e.Error, e.Description)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

type client struct {
	ClientID     string         `json:"client_id"`
	ClientName   string         `json:"client_name"`
	Audience     []string       `json:"audience"`
	RedirectURIs []string       `json:"redirect_uris"`
	Metadata     map[string]any `json:"metadata"`
}

func (c client) port() ports.OAuthClient {
	class, _ := c.Metadata["class"].(string)
	return ports.OAuthClient{ID: c.ClientID, Name: c.ClientName, Class: class, Audiences: c.Audience, RedirectURIs: c.RedirectURIs}
}

type request struct {
	Challenge          string         `json:"challenge"`
	Subject            string         `json:"subject"`
	Skip               bool           `json:"skip"`
	Client             client         `json:"client"`
	RequestURL         string         `json:"request_url"`
	RequestedScope     []string       `json:"requested_scope"`
	RequestedAudiences []string       `json:"requested_access_token_audience"`
	Context            map[string]any `json:"context"`
}

type redirect struct {
	RedirectTo string `json:"redirect_to"`
}

// LoginRequest implements ports.OAuthServer.
func (a *Admin) LoginRequest(ctx context.Context, challenge string) (ports.OAuthLogin, error) {
	var r request
	err := a.do(ctx, http.MethodGet, "/admin/oauth2/auth/requests/login", url.Values{"login_challenge": {challenge}}, nil, nil, &r)
	return ports.OAuthLogin{Challenge: r.Challenge, Subject: r.Subject, Skip: r.Skip, Client: r.Client.port(), RequestURL: r.RequestURL,
		RequestedScopes: r.RequestedScope, RequestedAudiences: r.RequestedAudiences}, err
}

// AcceptLogin implements ports.OAuthServer.
func (a *Admin) AcceptLogin(ctx context.Context, challenge string, acc ports.LoginAcceptance) (string, error) {
	var r redirect
	err := a.do(ctx, http.MethodPut, "/admin/oauth2/auth/requests/login/accept", url.Values{"login_challenge": {challenge}},
		map[string]any{"subject": acc.Subject, "context": acc.Context, "remember": false}, nil, &r)
	return r.RedirectTo, err
}

// RejectLogin implements ports.OAuthServer.
func (a *Admin) RejectLogin(ctx context.Context, challenge string, rej ports.OAuthRejection) (string, error) {
	var r redirect
	err := a.do(ctx, http.MethodPut, "/admin/oauth2/auth/requests/login/reject", url.Values{"login_challenge": {challenge}},
		map[string]any{"error": rej.Error, "error_description": rej.Description}, nil, &r)
	return r.RedirectTo, err
}

// ConsentRequest implements ports.OAuthServer.
func (a *Admin) ConsentRequest(ctx context.Context, challenge string) (ports.OAuthConsent, error) {
	var r request
	err := a.do(ctx, http.MethodGet, "/admin/oauth2/auth/requests/consent", url.Values{"consent_challenge": {challenge}}, nil, nil, &r)
	return ports.OAuthConsent{Challenge: r.Challenge, Subject: r.Subject, Skip: r.Skip, Client: r.Client.port(),
		RequestedScopes: r.RequestedScope, RequestedAudiences: r.RequestedAudiences, Context: r.Context}, err
}

// AcceptConsent implements ports.OAuthServer. Consent is never remembered: every grant is asked for.
func (a *Admin) AcceptConsent(ctx context.Context, challenge string, acc ports.ConsentAcceptance) (string, error) {
	var r redirect
	err := a.do(ctx, http.MethodPut, "/admin/oauth2/auth/requests/consent/accept", url.Values{"consent_challenge": {challenge}},
		map[string]any{"grant_scope": acc.Scopes, "grant_access_token_audience": acc.Audiences, "remember": false,
			"session": map[string]any{"access_token": acc.AccessTokenClaims}}, nil, &r)
	return r.RedirectTo, err
}

// RejectConsent implements ports.OAuthServer.
func (a *Admin) RejectConsent(ctx context.Context, challenge string, rej ports.OAuthRejection) (string, error) {
	var r redirect
	err := a.do(ctx, http.MethodPut, "/admin/oauth2/auth/requests/consent/reject", url.Values{"consent_challenge": {challenge}},
		map[string]any{"error": rej.Error, "error_description": rej.Description}, nil, &r)
	return r.RedirectTo, err
}

// Introspect implements ports.OAuthServer.
func (a *Admin) Introspect(ctx context.Context, token string) (ports.Introspection, error) {
	var r struct {
		Active   bool           `json:"active"`
		Sub      string         `json:"sub"`
		ClientID string         `json:"client_id"`
		Aud      []string       `json:"aud"`
		Scope    string         `json:"scope"`
		Exp      int64          `json:"exp"`
		TokenUse string         `json:"token_use"`
		Ext      map[string]any `json:"ext"`
	}
	if err := a.do(ctx, http.MethodPost, "/admin/oauth2/introspect", nil, nil, url.Values{"token": {token}}, &r); err != nil {
		if errors.Is(err, ports.ErrNoChallenge) {
			return ports.Introspection{}, nil
		}
		return ports.Introspection{}, err
	}
	if !r.Active || r.TokenUse != "access_token" { // a refresh token is never a bearer token
		return ports.Introspection{}, nil
	}
	return ports.Introspection{Active: true, Subject: r.Sub, ClientID: r.ClientID, Audiences: r.Aud, Scopes: strings.Fields(r.Scope),
		ExpiresAt: time.Unix(r.Exp, 0).UTC(), Claims: r.Ext}, nil
}

// RevokeSubject implements ports.OAuthServer: deleting the subject's consent sessions revokes every
// token issued under them, access and refresh, for every client.
func (a *Admin) RevokeSubject(ctx context.Context, subject string) error {
	if subject == "" {
		return errors.New("hydra: revoke needs a subject")
	}
	err := a.do(ctx, http.MethodDelete, "/admin/oauth2/auth/sessions/consent", url.Values{"subject": {subject}, "all": {"true"}}, nil, nil, nil)
	if errors.Is(err, ports.ErrNoChallenge) {
		return nil // nothing to revoke
	}
	return err
}

// NewClient is what CreateClient registers.
type NewClient struct {
	Name         string
	Class        string // user, integration or agent (AI-34)
	Audiences    []string
	RedirectURIs []string
	Scopes       []string
	// AccessTokenLifespan, when set, overrides the server's access-token lifetime for this client.
	AccessTokenLifespan time.Duration
}

// CreateClient registers a public client that must use the authorisation code with PKCE (AI-36). Clients
// are created by the "Connected apps" page in P1; P0 uses it for test clients.
func (a *Admin) CreateClient(ctx context.Context, c NewClient) (ports.OAuthClient, error) {
	body := map[string]any{"client_name": c.Name, "audience": c.Audiences, "redirect_uris": c.RedirectURIs,
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "none", "scope": strings.Join(c.Scopes, " "), "metadata": map[string]any{"class": c.Class}}
	if c.AccessTokenLifespan > 0 {
		body["authorization_code_grant_access_token_lifespan"] = c.AccessTokenLifespan.String()
	}
	var out client
	err := a.do(ctx, http.MethodPost, "/admin/clients", nil, body, nil, &out)
	return out.port(), err
}
