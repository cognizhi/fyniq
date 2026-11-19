// Package hydra is the OAuthServer adapter for Ory Hydra's admin API (ADR-T3), Apache-2.0 build
// v26.2.0, pinned by digest (Image). It speaks plain HTTP and JSON; no SDK. The admin API is never
// public: locally it is the oauth container's port 4445, in the cloud an internal-only service.
package hydra

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/egress"
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

// New returns the admin API at baseURL, called through client (wiring.Hydra: the egress guard, to baseURL's host only,
// with a short timeout); egress.Public when nil.
func New(baseURL string, client *http.Client) *Admin {
	if client == nil {
		client = egress.Public()
	}
	return &Admin{BaseURL: strings.TrimSuffix(baseURL, "/"), HTTP: client}
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
		RequestedScopes: r.RequestedScope, RequestedAudiences: r.RequestedAudiences, Context: r.Context, RequestURL: r.RequestURL}, err
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
		Iat      int64          `json:"iat"`
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
		ExpiresAt: time.Unix(r.Exp, 0).UTC(), IssuedAt: time.Unix(r.Iat, 0).UTC(), Claims: r.Ext}, nil
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

// TrustRelationship lets a JWT issuer's assertions obtain access tokens with the RFC 7523 JWT-bearer
// grant: assertions signed by Key, for Subject or (AllowAnySubject) anyone, for at most Scopes, until
// ExpiresAt. Open question 40's option (b): the MCP server's key (ADR-T3).
type TrustRelationship struct {
	Issuer          string
	Subject         string
	AllowAnySubject bool
	Scopes          []string
	Key             any // the public JWK (go-jose's JSONWebKey)
	ExpiresAt       time.Time
}

// Trust registers a trust relationship and returns its ID. A relationship the issuer already has is replaced, so registering at
// each start leaves one.
func (a *Admin) Trust(ctx context.Context, t TrustRelationship) (string, error) {
	var existing []struct {
		ID string `json:"id"`
	}
	if err := a.do(ctx, http.MethodGet, "/admin/trust/grants/jwt-bearer/issuers", url.Values{"issuer": {t.Issuer}}, nil, nil, &existing); err == nil {
		for _, g := range existing {
			_ = a.do(ctx, http.MethodDelete, "/admin/trust/grants/jwt-bearer/issuers/"+url.PathEscape(g.ID), nil, nil, nil, nil)
		}
	}
	body := map[string]any{"issuer": t.Issuer, "allow_any_subject": t.AllowAnySubject, "scope": t.Scopes, "jwk": t.Key,
		"expires_at": t.ExpiresAt.UTC().Format(time.RFC3339)}
	if t.Subject != "" {
		body["subject"] = t.Subject
	}
	var out struct {
		ID string `json:"id"`
	}
	err := a.do(ctx, http.MethodPost, "/admin/trust/grants/jwt-bearer/issuers", nil, body, nil, &out)
	return out.ID, err
}

// DelegationClient is the MCP server's confidential client for the JWT-bearer grant: it may ask only
// for Audiences and Scopes, and its tokens live AccessTokenLifespan.
type DelegationClient struct {
	// ID and Secret, when set, are the client's fixed credentials (the local stack's, so the API and the MCP service agree
	// without sharing a database): registering again replaces the client. Empty: the server chooses them.
	ID, Secret          string
	Name                string
	Audiences, Scopes   []string
	AccessTokenLifespan time.Duration
}

// CreateDelegationClient registers it and returns its ID and secret (client_secret_basic).
func (a *Admin) CreateDelegationClient(ctx context.Context, c DelegationClient) (id, secret string, err error) {
	body := map[string]any{"client_name": c.Name, "audience": c.Audiences, "scope": strings.Join(c.Scopes, " "),
		"grant_types": []string{"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "response_types": []string{},
		"token_endpoint_auth_method": "client_secret_basic", "metadata": map[string]any{"class": "agent"}}
	if c.AccessTokenLifespan > 0 {
		body["jwt_bearer_grant_access_token_lifespan"] = c.AccessTokenLifespan.String()
	}
	var out struct {
		ID     string `json:"client_id"`
		Secret string `json:"client_secret"`
	}
	if c.ID != "" {
		body["client_id"], body["client_secret"] = c.ID, c.Secret
		err = a.do(ctx, http.MethodPut, "/admin/clients/"+url.PathEscape(c.ID), nil, body, nil, &out)
		if errors.Is(err, ports.ErrNoChallenge) {
			err = a.do(ctx, http.MethodPost, "/admin/clients", nil, body, nil, &out)
		}
		return cmp.Or(out.ID, c.ID), c.Secret, err
	}
	err = a.do(ctx, http.MethodPost, "/admin/clients", nil, body, nil, &out)
	return out.ID, out.Secret, err
}

var _ ports.OAuthClients = (*Admin)(nil)

// RegisterIntegration implements ports.OAuthClients: a confidential client with the client-credentials grant only,
// authenticating with private_key_jwt by its own public key (FR-INT-02, SEC-07), never a shared secret; its tokens name
// only the API's audience and live TokenLifespan.
func (a *Admin) RegisterIntegration(ctx context.Context, c ports.IntegrationRegistration) (string, error) {
	if len(c.Key) == 0 || c.Alg == "" || c.Audience == "" {
		return "", errors.New("hydra: an integration client needs its key, its algorithm and the API's audience")
	}
	// #nosec G101 -- OAuth client metadata (its grant and how it authenticates), not a credential
	body := map[string]any{"client_name": c.Name, "audience": []string{c.Audience}, "scope": strings.Join(c.Scopes, " "),
		"grant_types": []string{"client_credentials"}, "response_types": []string{}, "token_endpoint_auth_method": "private_key_jwt",
		"token_endpoint_auth_signing_alg": c.Alg, "jwks": map[string]any{"keys": []json.RawMessage{c.Key}},
		"metadata": map[string]any{"class": c.Class, "organisation": c.Organisation}}
	if c.ClientID != "" {
		body["client_id"] = c.ClientID
	}
	if c.TokenLifespan > 0 {
		body["client_credentials_grant_access_token_lifespan"] = c.TokenLifespan.String()
	}
	var out client
	if err := a.do(ctx, http.MethodPost, "/admin/clients", nil, body, nil, &out); err != nil {
		return "", err
	}
	if out.ClientID == "" {
		return "", errors.New("hydra: the client was registered without an ID")
	}
	return out.ClientID, nil
}

// RegisterPersonal implements ports.OAuthClients: a confidential client with the client-credentials grant only, whose
// secret Hydra generates, returns once and keeps as a hash (client_secret_basic), so a personal access token is hashed at
// rest and shown once (FR-INT-02); its tokens name only the API's audience and live TokenLifespan (AI-38).
func (a *Admin) RegisterPersonal(ctx context.Context, c ports.PersonalRegistration) (string, string, error) {
	if c.Audience == "" || len(c.Scopes) == 0 {
		return "", "", errors.New("hydra: a personal access token needs the API's audience and its scopes")
	}
	// #nosec G101 -- OAuth client metadata (its grant and how it authenticates), not a credential
	body := map[string]any{"client_name": c.Name, "audience": []string{c.Audience}, "scope": strings.Join(c.Scopes, " "),
		"grant_types": []string{"client_credentials"}, "response_types": []string{}, "token_endpoint_auth_method": "client_secret_basic",
		"metadata": map[string]any{"class": c.Class, "organisation": c.Organisation, "kind": "personal"}}
	if c.TokenLifespan > 0 {
		body["client_credentials_grant_access_token_lifespan"] = c.TokenLifespan.String()
	}
	var out struct {
		ClientID string `json:"client_id"`
		Secret   string `json:"client_secret"`
	}
	if err := a.do(ctx, http.MethodPost, "/admin/clients", nil, body, nil, &out); err != nil {
		return "", "", err
	}
	if out.ClientID == "" || out.Secret == "" {
		return "", "", errors.New("hydra: the client was registered without an ID or a secret")
	}
	return out.ClientID, out.Secret, nil
}

// RemoveClient implements ports.OAuthClients: its tokens are deleted first, then the client.
func (a *Admin) RemoveClient(ctx context.Context, clientID string) error {
	if clientID == "" {
		return errors.New("hydra: remove needs a client ID")
	}
	err := a.do(ctx, http.MethodDelete, "/admin/oauth2/tokens", url.Values{"client_id": {clientID}}, nil, nil, nil)
	if err != nil && !errors.Is(err, ports.ErrNoChallenge) {
		return err
	}
	err = a.do(ctx, http.MethodDelete, "/admin/clients/"+url.PathEscape(clientID), nil, nil, nil, nil)
	if errors.Is(err, ports.ErrNoChallenge) {
		return nil // gone already
	}
	return err
}

// RegisterPublicClient implements ports.OAuthClients: a public client that must use the authorisation code with PKCE, for one
// audience, with exact redirect URIs; with a fixed ID it replaces what the server holds under that ID, so registering again
// is how a change to the first-party clients reaches a running server.
func (a *Admin) RegisterPublicClient(ctx context.Context, c ports.PublicRegistration) (string, error) {
	if c.Name == "" || c.Audience == "" || len(c.RedirectURIs) == 0 || c.Class == "" {
		return "", errors.New("hydra: a public client needs a name, a class, an audience and redirect URIs")
	}
	meta := map[string]any{"class": c.Class}
	if c.App != "" {
		meta["app"] = c.App
	}
	body := map[string]any{"client_name": c.Name, "audience": []string{c.Audience}, "redirect_uris": c.RedirectURIs,
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "none", "scope": strings.Join(c.Scopes, " "), "metadata": meta}
	method, path := http.MethodPost, "/admin/clients"
	if c.ID != "" {
		body["client_id"] = c.ID
		method, path = http.MethodPut, "/admin/clients/"+url.PathEscape(c.ID)
	}
	var out client
	err := a.do(ctx, method, path, nil, body, nil, &out)
	if err != nil && c.ID != "" && errors.Is(err, ports.ErrNoChallenge) { // not there yet: create it with its ID
		err = a.do(ctx, http.MethodPost, "/admin/clients", nil, body, nil, &out)
	}
	if err != nil {
		return "", err
	}
	if out.ClientID == "" {
		return "", errors.New("hydra: the client was registered without an ID")
	}
	return out.ClientID, nil
}

var _ ports.OAuthConsents = (*Admin)(nil)

// RevokeAgentConsents implements ports.OAuthConsents: the subject's consent sessions are listed, and those of an agent-class client
// are deleted, one client at a time, which revokes every token issued under them.
func (a *Admin) RevokeAgentConsents(ctx context.Context, subject string) error {
	if subject == "" {
		return errors.New("hydra: revoke needs a subject")
	}
	var sessions []struct {
		ConsentRequest struct {
			Client client `json:"client"`
		} `json:"consent_request"`
	}
	err := a.do(ctx, http.MethodGet, "/admin/oauth2/auth/sessions/consent", url.Values{"subject": {subject}}, nil, nil, &sessions)
	if errors.Is(err, ports.ErrNoChallenge) {
		return nil // nothing to revoke
	}
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, s := range sessions {
		c := s.ConsentRequest.Client
		if c.Metadata["class"] != "agent" || c.ClientID == "" || seen[c.ClientID] {
			continue
		}
		seen[c.ClientID] = true
		err := a.do(ctx, http.MethodDelete, "/admin/oauth2/auth/sessions/consent", url.Values{"subject": {subject}, "client": {c.ClientID}}, nil, nil, nil)
		if err != nil && !errors.Is(err, ports.ErrNoChallenge) {
			return err
		}
	}
	return nil
}

// AgentConsents implements ports.OAuthConsents: the subject's consent sessions with agent-class clients, newest grant of each.
func (a *Admin) AgentConsents(ctx context.Context, subject string) ([]ports.AgentConsent, error) {
	if subject == "" {
		return nil, errors.New("hydra: a consent list needs a subject")
	}
	var sessions []struct {
		ConsentRequest struct {
			Client client `json:"client"`
		} `json:"consent_request"`
		GrantScope []string  `json:"grant_scope"`
		HandledAt  time.Time `json:"handled_at"`
	}
	err := a.do(ctx, http.MethodGet, "/admin/oauth2/auth/sessions/consent", url.Values{"subject": {subject}}, nil, nil, &sessions)
	if errors.Is(err, ports.ErrNoChallenge) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	latest := map[string]ports.AgentConsent{}
	for _, s := range sessions {
		c := s.ConsentRequest.Client
		if c.Metadata["class"] != "agent" || c.ClientID == "" {
			continue
		}
		if old, ok := latest[c.ClientID]; !ok || s.HandledAt.After(old.GrantedAt) {
			latest[c.ClientID] = ports.AgentConsent{ClientID: c.ClientID, ClientName: c.ClientName, Scopes: s.GrantScope, GrantedAt: s.HandledAt}
		}
	}
	out := make([]ports.AgentConsent, 0, len(latest))
	for _, c := range latest {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GrantedAt.After(out[j].GrantedAt) })
	return out, nil
}

// RevokeClientConsent implements ports.OAuthConsents.
func (a *Admin) RevokeClientConsent(ctx context.Context, subject, clientID string) error {
	if subject == "" || clientID == "" {
		return errors.New("hydra: revoke needs a subject and a client")
	}
	err := a.do(ctx, http.MethodDelete, "/admin/oauth2/auth/sessions/consent", url.Values{"subject": {subject}, "client": {clientID}}, nil, nil, nil)
	if errors.Is(err, ports.ErrNoChallenge) {
		return nil
	}
	return err
}
