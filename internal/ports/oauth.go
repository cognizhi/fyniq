package ports

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// OAuthServer is the OAuth 2.1 authorisation server for non-browser clients, integrations and MCP
// (ADR-T3): Ory Hydra's admin API in every build. Hydra holds the clients and tokens; it delegates
// login and consent to the API (/auth/login, /auth/consent), which decides who signs in and what is
// granted.
type OAuthServer interface {
	// LoginRequest returns the pending login of a challenge.
	LoginRequest(ctx context.Context, challenge string) (OAuthLogin, error)
	// AcceptLogin signs the subject in for the request; it returns where to send the browser.
	AcceptLogin(ctx context.Context, challenge string, a LoginAcceptance) (redirect string, err error)
	RejectLogin(ctx context.Context, challenge string, r OAuthRejection) (redirect string, err error)
	// ConsentRequest returns the pending consent of a challenge.
	ConsentRequest(ctx context.Context, challenge string) (OAuthConsent, error)
	// AcceptConsent grants scopes and audiences, with claims carried by the access token.
	AcceptConsent(ctx context.Context, challenge string, a ConsentAcceptance) (redirect string, err error)
	RejectConsent(ctx context.Context, challenge string, r OAuthRejection) (redirect string, err error)
	// Introspect returns what an access token is; an unknown, expired or revoked token is not active.
	Introspect(ctx context.Context, token string) (Introspection, error)
	// RevokeSubject revokes every access and refresh token issued to subject, by every client (AI-38).
	RevokeSubject(ctx context.Context, subject string) error
}

// ErrNoChallenge is a login or consent challenge the server does not know (expired, or used).
var ErrNoChallenge = errors.New("unknown or expired challenge")

// OAuthClient is a registered client, as the server holds it. Its class (AI-34) and its allowed
// audiences are the client's record, never the request's.
type OAuthClient struct {
	ID           string
	Name         string
	Class        string   // user, integration or agent (client metadata); empty when the record has none
	Audiences    []string // the audiences the client may ask for
	RedirectURIs []string
}

// OAuthLogin is a pending login.
type OAuthLogin struct {
	Challenge          string
	Subject            string // set when the server already knows the user (Skip)
	Skip               bool
	Client             OAuthClient
	RequestURL         string // the authorisation request as the client sent it
	RequestedScopes    []string
	RequestedAudiences []string
}

// LoginAcceptance signs a subject in; Context reaches the consent request.
type LoginAcceptance struct {
	Subject string
	Context map[string]any
}

// OAuthConsent is a pending consent.
type OAuthConsent struct {
	Challenge          string
	Subject            string
	Skip               bool
	Client             OAuthClient
	RequestedScopes    []string
	RequestedAudiences []string
	Context            map[string]any
	// RequestURL is the authorisation request as the client sent it: it carries the RFC 8707 resource parameter, which the
	// OAuth server does not turn into an audience (p1-mcp-first-party task 1.1).
	RequestURL string
}

// ConsentAcceptance is what consent grants.
type ConsentAcceptance struct {
	Scopes            []string
	Audiences         []string
	AccessTokenClaims map[string]any // carried by the access token, returned by introspection
}

// OAuthRejection refuses a login or consent with an OAuth error.
type OAuthRejection struct {
	Error       string // e.g. access_denied, invalid_request
	Description string
}

// Introspection is what the server says about an access token.
type Introspection struct {
	Active    bool
	Subject   string
	ClientID  string
	Audiences []string
	Scopes    []string
	ExpiresAt time.Time
	IssuedAt  time.Time
	Claims    map[string]any // the claims consent gave the access token
}

// OAuthClients registers the organisation's API clients with the OAuth server (ADR-T3, FR-INT-02): the server holds the
// client and issues its tokens; Fyniq's record of the client decides what its tokens may do.
type OAuthClients interface {
	// RegisterIntegration registers a confidential client that obtains access tokens for one audience with the
	// client-credentials grant only, authenticating with private_key_jwt (RFC 7523) signed by the key it names. It
	// returns the server's client ID.
	RegisterIntegration(ctx context.Context, c IntegrationRegistration) (string, error)
	// RegisterPublicClient registers, or replaces, a public client with a fixed ID: the authorisation code with PKCE and refresh
	// tokens only, for the audience it names, with exact redirect URIs (a loopback one matches on any port, RFC 8252). It
	// returns the client's ID. Used for the first-party MCP clients (AI-35); a client that registers itself goes through the
	// API's registration endpoint (AI-90), which calls this.
	RegisterPublicClient(ctx context.Context, c PublicRegistration) (string, error)
	// RemoveClient deletes a client and every token issued to it; a client the server does not know is gone already.
	RemoveClient(ctx context.Context, clientID string) error
}

// IntegrationRegistration is an integration client as the OAuth server registers it.
type IntegrationRegistration struct {
	Name     string
	Class    string // AI-34: always integration here; kept in the client's metadata
	Audience string // the API's: the only audience its tokens may name
	Scopes   []string
	// Key is the client's public key as a JWK (RFC 7517), and Alg the algorithm its assertions are signed with.
	Key json.RawMessage
	Alg string
	// TokenLifespan is how long each access token lives (AI-38: at most 60 minutes).
	TokenLifespan time.Duration
	Organisation  string // kept in the client's metadata, for the operator; never trusted from it
}

// PublicRegistration is a public client as the OAuth server registers it.
type PublicRegistration struct {
	ID           string // the client's ID: fixed for a first-party client, chosen by the server otherwise when empty
	Name         string
	Class        string // AI-34: agent for an MCP client
	Audience     string
	Scopes       []string
	RedirectURIs []string
	// App is the first-party app's name on the organisation's allowlist (mcp.clients), kept in the client's metadata; empty for
	// a client that registered itself.
	App string
}

// OAuthConsents revokes what a person granted the AI apps (agent-class clients) at the OAuth server, without touching what they
// granted anything else: their own opt-out of AI apps stops the tokens of every app at once (AI-35, AI-38), while their web
// session and integrations are untouched.
type OAuthConsents interface {
	// RevokeAgentConsents revokes every access and refresh token issued to subject by an agent-class client.
	RevokeAgentConsents(ctx context.Context, subject string) error
	// AgentConsents are what subject has granted agent-class clients now: each client, the scopes, and when.
	AgentConsents(ctx context.Context, subject string) ([]AgentConsent, error)
	// RevokeClientConsent revokes what subject granted one client: its access and refresh tokens.
	RevokeClientConsent(ctx context.Context, subject, clientID string) error
}

// AgentConsent is what a person granted an AI app, as the OAuth server holds it.
type AgentConsent struct {
	ClientID, ClientName string
	Scopes               []string
	GrantedAt            time.Time
}
