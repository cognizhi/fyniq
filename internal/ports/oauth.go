package ports

import (
	"context"
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
	Claims    map[string]any // the claims consent gave the access token
}
