package gcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/signin"
)

// IdentityToolkit is Identity Platform's REST endpoint.
const IdentityToolkit = "https://identitytoolkit.googleapis.com"

// IdentityProvider is a sign-in provider federated through Identity Platform (FR-ADM-08).
type IdentityProvider struct {
	ID       string `json:"id"`        // Fyniq's name for it: google, zoho
	Name     string `json:"name"`      // as the sign-in page shows it
	IdP      string `json:"idp"`       // Identity Platform's provider ID: google.com, oidc.zoho
	Issuer   string `json:"issuer"`    // the provider's issuer, exactly: https://accounts.zoho.com (one Zoho provider per data centre)
	ClientID string `json:"client_id"` // Fyniq's client at the provider: the provider's ID token's audience
}

// IdentityPlatform is the SignIn port on Identity Platform (ADR-T3), driven from the server with its
// REST API: createAuthUri starts a sign-in with a provider and signInWithIdp completes it from the
// browser's return. The browser never holds a token: the API turns the result into a first-party
// session (SEC-08).
//
// A sign-in is bound to the browser that began it by the session ID the server sends with both calls,
// derived from the browser's sign-in cookie and never put in a URL; Identity Platform refuses a return
// without it. The result is checked twice: Identity Platform's own ID token (RS256 by Google's keys,
// issuer https://securetoken.google.com/<project>, audience the project), and the provider's ID token
// it passes on, whose issuer must be the provider's configured one exactly (FR-ADM-08). The identity is
// the provider's (issuer, subject), never Identity Platform's user ID or an email.
//
// [verify in staging, task 3.2] that createAuthUri sends the provider back to continueUri for an OIDC
// provider in the code flow, that signInWithIdp returns oauthIdToken for Zoho, and the iss in Zoho's
// real tokens. The project must allow several accounts per email address, so Identity Platform never
// links accounts by email (FR-ADM-08).
type IdentityPlatform struct {
	Endpoint  string             // IdentityToolkit; a fake in tests
	APIKey    string             // the project's web API key, sent in a header, never a URL (AI-79)
	Project   string             // the project ID: Identity Platform's ID tokens' audience
	Tenant    string             // optional Identity Platform tenant
	Federated []IdentityProvider // the providers people may sign in with, in order
	Keys      oidc.KeySet        // CertKeys on IdentityPlatformCertsURL
	Client    *http.Client
	Clock     ports.Clock
}

var _ ports.SignIn = (*IdentityPlatform)(nil)

// Providers implements ports.SignIn.
func (p *IdentityPlatform) Providers() []ports.SignInProvider {
	out := make([]ports.SignInProvider, len(p.Federated))
	for i, x := range p.Federated {
		out[i] = ports.SignInProvider{ID: x.ID, Name: x.Name}
	}
	return out
}

func (p *IdentityPlatform) provider(id string) (IdentityProvider, bool) {
	for _, x := range p.Federated {
		if x.ID == id {
			return x, true
		}
	}
	return IdentityProvider{}, false
}

// Start implements ports.SignIn.
func (p *IdentityPlatform) Start(ctx context.Context, provider, callback string, s ports.SignInSecrets) (string, error) {
	x, ok := p.provider(provider)
	if !ok {
		return "", ports.ErrUnknownProvider
	}
	req := map[string]any{"providerId": x.IdP, "continueUri": callback, "sessionId": s.Nonce}
	if x.IdP == "google.com" {
		req["authFlowType"] = "CODE_FLOW" // the code comes back in the query, which the server sees; never a fragment
	}
	if p.Tenant != "" {
		req["tenantId"] = p.Tenant
	}
	var out struct {
		AuthURI   string `json:"authUri"`
		SessionID string `json:"sessionId"`
	}
	if err := p.call(ctx, "accounts:createAuthUri", req, &out); err != nil {
		return "", err
	}
	u, err := url.Parse(out.AuthURI)
	switch {
	case err != nil || u.Scheme != "https" || u.Host == "":
		return "", fmt.Errorf("identity platform: the authorisation address is not an https URL")
	case out.SessionID != "" && out.SessionID != s.Nonce:
		return "", fmt.Errorf("identity platform: the session ID came back changed")
	}
	return out.AuthURI, nil
}

// signInResult is the part of signInWithIdp's answer Fyniq reads.
type signInResult struct {
	ProviderID           string `json:"providerId"`
	IDToken              string `json:"idToken"`
	OAuthIDToken         string `json:"oauthIdToken"`
	Email                string `json:"email"`
	EmailVerified        bool   `json:"emailVerified"`
	DisplayName          string `json:"displayName"`
	MFAPendingCredential string `json:"mfaPendingCredential"`
	NeedConfirmation     bool   `json:"needConfirmation"`
	ErrorMessage         string `json:"errorMessage"`
}

// Finish implements ports.SignIn.
func (p *IdentityPlatform) Finish(ctx context.Context, provider string, callback *url.URL, s ports.SignInSecrets) (ports.SignedIn, error) {
	x, ok := p.provider(provider)
	if !ok {
		return ports.SignedIn{}, ports.ErrUnknownProvider
	}
	refused := func(format string, a ...any) (ports.SignedIn, error) {
		return ports.SignedIn{}, errors.Join(ports.ErrSignInRefused, fmt.Errorf("identity platform: "+format, a...))
	}
	if callback.Query().Get("error") != "" {
		return refused("the person cancelled at the provider")
	}
	req := map[string]any{"requestUri": callback.String(), "sessionId": s.Nonce, "returnSecureToken": true, "returnIdpCredential": true}
	if p.Tenant != "" {
		req["tenantId"] = p.Tenant
	}
	var r signInResult
	if err := p.call(ctx, "accounts:signInWithIdp", req, &r); err != nil {
		return ports.SignedIn{}, err
	}
	switch {
	case r.ErrorMessage != "" || r.NeedConfirmation:
		return refused("the sign-in needs an account link, which Fyniq makes only after a step-up (FR-ADM-08)")
	case r.MFAPendingCredential != "":
		return refused("the account has a second factor at Identity Platform; Fyniq's own second step arrives in P1")
	case r.ProviderID != x.IdP:
		return refused("the sign-in came back from provider %q, not %q", r.ProviderID, x.IdP)
	case r.OAuthIDToken == "":
		return refused("the provider's ID token did not come back, so its issuer cannot be checked (FR-ADM-08)")
	}
	now := p.Clock.Now()
	platform := signin.Provider{ID: provider, Issuer: "https://securetoken.google.com/" + p.Project, ClientID: p.Project}
	who, err := signin.VerifyBound(ctx, platform, p.Keys, r.IDToken, now)
	if err != nil {
		return ports.SignedIn{}, err
	}
	issuer, subject, err := upstream(r.OAuthIDToken, x)
	if err != nil {
		return refused("%v", err)
	}
	who.Provider, who.Issuer, who.Subject = x.ID, issuer, subject
	if who.Email == "" {
		who.Email, who.EmailVerified = r.Email, r.EmailVerified
	}
	if who.Name == "" {
		who.Name = r.DisplayName
	}
	return who, nil
}

// upstream reads the provider's ID token that Identity Platform verified and passed on: its issuer must
// be the provider's configured one exactly (FR-ADM-08) and its audience Fyniq's client there. The
// signature is Identity Platform's to check; the identity is this token's (issuer, subject).
func upstream(raw string, x IdentityProvider) (issuer, subject string, err error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", "", errors.New("the provider's ID token is not a JWT")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", errors.New("the provider's ID token cannot be read")
	}
	var c struct {
		Issuer   string `json:"iss"`
		Subject  string `json:"sub"`
		Audience any    `json:"aud"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return "", "", errors.New("the provider's ID token cannot be read")
	}
	var aud []string
	switch a := c.Audience.(type) {
	case string:
		aud = []string{a}
	case []any:
		for _, v := range a {
			if s, ok := v.(string); ok {
				aud = append(aud, s)
			}
		}
	}
	switch {
	case c.Issuer != x.Issuer:
		return "", "", fmt.Errorf("the provider's ID token was issued by %q, not the configured %q", c.Issuer, x.Issuer)
	case !slices.Contains(aud, x.ClientID):
		return "", "", fmt.Errorf("the provider's ID token is for another client")
	case c.Subject == "":
		return "", "", errors.New("the provider's ID token has no subject")
	}
	return c.Issuer, c.Subject, nil
}

// call POSTs a request to the toolkit with the API key in a header. A 4xx answer refuses the sign-in;
// anything else is a failure. Neither error ever quotes the request (the key) or the answer (tokens).
func (p *IdentityPlatform) call(ctx context.Context, method string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(p.Endpoint, "/")+"/v1/"+method, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", p.APIKey)
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("identity platform %s: the service did not answer", method)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("identity platform %s: the answer cannot be read", method)
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		code, _, _ := strings.Cut(e.Error.Message, " ") // e.g. INVALID_IDP_RESPONSE: a code, never a token
		return errors.Join(ports.ErrSignInRefused, fmt.Errorf("identity platform %s: %s %s", method, resp.Status, code))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("identity platform %s: %s", method, resp.Status)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("identity platform %s: the answer is not what Fyniq expects", method)
	}
	return nil
}
