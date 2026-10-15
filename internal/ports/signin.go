package ports

import (
	"context"
	"errors"
	"net/url"
	"time"
)

// SignIn signs a person in to the web app with a sign-in provider, through the user identity provider
// (ADR-T3: Identity Platform, which federates Google and Zoho; FR-ADM-08). Locally it is the dev issuer
// in the local build (§12.8: the fallback, since the Firebase emulator fails TOTP enrolment). The
// browser goes to Start's address and comes back to the callback, which Finish checks. Who signed in
// becomes a first-party session (SEC-05, SEC-08); the provider's tokens never reach the browser.
type SignIn interface {
	// Providers are the providers people may sign in with, in the order to offer them.
	Providers() []SignInProvider
	// Start returns where to send the browser to sign in with provider; it comes back to callback.
	Start(ctx context.Context, provider, callback string, s SignInSecrets) (redirect string, err error)
	// Finish checks the browser's return to the callback (the full URL with its query) and returns who
	// signed in. It refuses, with ErrSignInRefused, a return without the state of s, and an ID token
	// from any issuer but the provider's configured one (FR-ADM-08), for another audience, expired, or
	// without the nonce of s.
	Finish(ctx context.Context, provider string, callback *url.URL, s SignInSecrets) (SignedIn, error)
}

// SignInProvider is a configured provider.
type SignInProvider struct {
	ID   string // e.g. google, zoho: letters, digits and hyphens
	Name string // as the sign-in page names it
}

// SignInSecrets bind a sign-in to the browser that started it. The API derives them from a random value
// it keeps in a short-lived HttpOnly cookie, so they are stored nowhere.
type SignInSecrets struct {
	State        string // comes back with the callback: a return the browser did not start is refused
	Nonce        string // carried by the ID token: a token from another sign-in is refused
	CodeVerifier string // PKCE (RFC 7636): the provider's code is useless without it
}

// SignedIn is who signed in.
type SignedIn struct {
	Provider string
	// Issuer and Subject are the identity: the provider's issuer and its stable subject (FR-ADM-08). An
	// email address is never the key.
	Issuer, Subject string
	Name, Email     string
	EmailVerified   bool
	AuthTime        time.Time // when the person authenticated to the provider
	// SecondFactor reports whether the sign-in passed a second factor (TOTP or a passkey): only then is
	// it a step-up (FR-ADM-07, SEC-01).
	SecondFactor bool
}

// ErrSignInRefused is a sign-in that failed its checks: nobody is signed in.
var ErrSignInRefused = errors.New("the sign-in was refused")

// ErrUnknownProvider is a provider that is not configured, or not enabled.
var ErrUnknownProvider = errors.New("no such sign-in provider")
