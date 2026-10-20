//go:build local

package local

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/signin"
)

// DevSignInProvider is the local build's one provider: its issuer is the dev tokens' (DevIssuer), so a
// person signed in through the page and a dev token with the same subject are the same identity.
var DevSignInProvider = signin.Provider{ID: "local", Name: "Local sign-in (development)", Issuer: DevIssuer, ClientID: "fyniq-web"}

// DevSignIn is the local build's sign-in (§12.8): the fallback for Identity Platform, whose Firebase
// emulator fails TOTP enrolment. It is an OpenID Connect issuer inside the API: its page asks who you
// are, as a provider would after checking you, and it issues an ES256-signed ID token for the provider
// the sign-in began with, which Finish checks like any provider's (signin.Verify: issuer, audience,
// expiry, nonce) after the state and PKCE. Whoever runs the stack can sign in as anyone, so it exists
// only in the local build (DEV-07). Its key and codes live in memory: a restart ends sign-ins under way.
type DevSignIn struct {
	providers []signin.Provider
	callback  string // the only redirect URI it sends a browser back to, like a provider's registration
	page      string // where the browser finds its page: <base>/auth/dev/authorize
	clock     ports.Clock
	key       *ecdsa.PrivateKey

	mu    sync.Mutex
	codes map[string]devCode
}

type devCode struct {
	provider, nonce, challenge string
	who                        devPerson
	expires                    time.Time
}

type devPerson struct {
	subject, name, email string
	secondFactor         bool
}

var _ ports.SignIn = (*DevSignIn)(nil)

// codeLifetime is how long the page's code is good for.
const codeLifetime = time.Minute

// NewDevSignIn issues for providers; base is where the API is mounted for the browser (/api) and
// callback the flow's callback URL.
func NewDevSignIn(providers []signin.Provider, base, callback string, clock ports.Clock) (*DevSignIn, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &DevSignIn{providers: providers, callback: callback, page: base + "/auth/dev/authorize", clock: clock, key: key,
		codes: map[string]devCode{}}, nil
}

// Register adds the page to mux.
func (d *DevSignIn) Register(mux interface {
	HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request))
}) {
	mux.HandleFunc("GET /auth/dev/authorize", d.show)
	mux.HandleFunc("POST /auth/dev/authorize", d.authorize)
}

// Providers implements ports.SignIn.
func (d *DevSignIn) Providers() []ports.SignInProvider {
	out := make([]ports.SignInProvider, len(d.providers))
	for i, p := range d.providers {
		out[i] = ports.SignInProvider{ID: p.ID, Name: p.Name}
	}
	return out
}

func (d *DevSignIn) provider(id string) (signin.Provider, bool) {
	for _, p := range d.providers {
		if p.ID == id {
			return p, true
		}
	}
	return signin.Provider{}, false
}

func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Start implements ports.SignIn: the page, with the authorisation request a provider would get.
func (d *DevSignIn) Start(_ context.Context, provider, callback string, s ports.SignInSecrets) (string, error) {
	p, ok := d.provider(provider)
	if !ok {
		return "", ports.ErrUnknownProvider
	}
	q := url.Values{"provider": {p.ID}, "client_id": {p.ClientID}, "response_type": {"code"}, "scope": {"openid email profile"},
		"redirect_uri": {callback}, "state": {s.State}, "nonce": {s.Nonce}, "code_challenge": {challenge(s.CodeVerifier)},
		"code_challenge_method": {"S256"}}
	return d.page + "?" + q.Encode(), nil
}

// Finish implements ports.SignIn.
func (d *DevSignIn) Finish(ctx context.Context, provider string, callback *url.URL, s ports.SignInSecrets) (ports.SignedIn, error) {
	refused := func(why string) (ports.SignedIn, error) {
		return ports.SignedIn{}, errors.Join(ports.ErrSignInRefused, errors.New(why))
	}
	p, ok := d.provider(provider)
	if !ok {
		return ports.SignedIn{}, ports.ErrUnknownProvider
	}
	q := callback.Query()
	if q.Get("error") != "" {
		return refused("the person cancelled at the provider")
	}
	if q.Get("state") == "" || q.Get("state") != s.State {
		return refused("the callback's state is not this browser's sign-in")
	}
	d.mu.Lock()
	c, found := d.codes[q.Get("code")]
	delete(d.codes, q.Get("code")) // a code is used once
	d.mu.Unlock()
	now := d.clock.Now()
	switch {
	case !found || !now.Before(c.expires):
		return refused("the code is unknown, used or expired")
	case challenge(s.CodeVerifier) != c.challenge:
		return refused("the code verifier does not match (PKCE)")
	}
	issuedFor, _ := d.provider(c.provider) // the provider the page signed the person in with
	raw, err := d.idToken(issuedFor, c, now)
	if err != nil {
		return ports.SignedIn{}, err
	}
	return signin.Verify(ctx, p, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&d.key.PublicKey}}, raw, s.Nonce, now)
}

// idToken is the ID token a provider would return for the code.
func (d *DevSignIn) idToken(p signin.Provider, c devCode, now time.Time) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: d.key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", err
	}
	amr := []string{"pwd"}
	if c.who.secondFactor {
		amr = []string{"pwd", "otp", "mfa"}
	}
	claims := map[string]any{"iss": p.Issuer, "sub": c.who.subject, "aud": p.ClientID, "exp": now.Add(5 * time.Minute).Unix(),
		"iat": now.Unix(), "auth_time": now.Unix(), "nonce": c.nonce, "name": c.who.name, "amr": amr}
	if c.who.email != "" {
		claims["email"], claims["email_verified"] = c.who.email, true
	}
	b, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	jws, err := signer.Sign(b)
	if err != nil {
		return "", err
	}
	return jws.CompactSerialize()
}

var page = template.Must(template.New("signin").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Local sign-in · Fyniq</title>
<style>
  :root { color-scheme: light dark; font-family: system-ui, sans-serif; }
  body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: Canvas; color: CanvasText; }
  main { width: min(26rem, calc(100% - 2rem)); padding: 1.5rem; border: 1px solid GrayText; border-radius: 0.75rem; }
  h1 { font-size: 1.25rem; margin: 0 0 0.25rem; }
  p { margin: 0 0 1rem; line-height: 1.5; }
  label { display: block; margin: 0.75rem 0 0.25rem; font-weight: 600; }
  input[type=text], input[type=email] { width: 100%; box-sizing: border-box; padding: 0.5rem; font: inherit; }
  .check { display: flex; gap: 0.5rem; align-items: start; font-weight: 400; }
  .actions { display: flex; gap: 0.75rem; margin-top: 1.25rem; }
  button { padding: 0.5rem 1rem; font: inherit; cursor: pointer; }
</style></head>
<body><main>
<h1>Local sign-in</h1>
<p>Local mode only. This page stands in for a sign-in provider such as Google or Zoho ({{.Provider}}): whoever runs this stack can sign in as anyone.</p>
<form method="post" action="{{.Action}}">
  {{range $k, $v := .Hidden}}<input type="hidden" name="{{$k}}" value="{{$v}}">{{end}}
  <label for="subject">Subject (your account ID at the provider)</label>
  <input id="subject" name="subject" type="text" required maxlength="200" autocomplete="username" placeholder="founder-a">
  <label for="name">Name</label>
  <input id="name" name="name" type="text" maxlength="200" autocomplete="name" placeholder="Mei Ling Tan">
  <label for="email">Email (optional)</label>
  <input id="email" name="email" type="email" maxlength="200" autocomplete="email">
  <label class="check"><input name="second_factor" type="checkbox" value="yes" checked> I passed a second factor (an authenticator app or a passkey). Every user needs one to use Fyniq; changes that need approval need it in the last 5 minutes.</label>
  <div class="actions"><button type="submit" name="decision" value="sign-in">Sign in</button>
  <button type="submit" name="decision" value="cancel" formnovalidate>Cancel</button></div>
</form>
</main></body></html>`))

// devRequest is the authorisation request the page carries, as Start built it.
var devRequest = []string{"provider", "client_id", "redirect_uri", "state", "nonce", "code_challenge", "code_challenge_method"}

func (d *DevSignIn) show(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, err := d.check(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hidden := map[string]string{}
	for _, k := range devRequest {
		hidden[k] = q.Get(k)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	_ = page.Execute(w, map[string]any{"Provider": p.Name, "Action": d.page, "Hidden": hidden})
}

func (d *DevSignIn) authorize(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "the form cannot be read", http.StatusBadRequest)
		return
	}
	f := r.PostForm
	p, err := d.check(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	back, _ := url.Parse(d.callback)
	q := url.Values{"state": {f.Get("state")}}
	if f.Get("decision") != "sign-in" {
		q.Set("error", "access_denied")
	} else {
		subject := f.Get("subject")
		if subject == "" || len(subject) > 200 || len(f.Get("name")) > 200 || len(f.Get("email")) > 200 {
			http.Error(w, "the subject is required; each field has at most 200 characters", http.StatusBadRequest)
			return
		}
		code := rand.Text() + rand.Text()
		d.mu.Lock()
		now := d.clock.Now()
		for k, c := range d.codes { // forget expired codes
			if !now.Before(c.expires) {
				delete(d.codes, k)
			}
		}
		d.codes[code] = devCode{provider: p.ID, nonce: f.Get("nonce"), challenge: f.Get("code_challenge"), expires: now.Add(codeLifetime),
			who: devPerson{subject: subject, name: f.Get("name"), email: f.Get("email"), secondFactor: f.Get("second_factor") == "yes"}}
		d.mu.Unlock()
		q.Set("code", code)
	}
	back.RawQuery = q.Encode()
	http.Redirect(w, r, back.String(), http.StatusSeeOther)
}

// check validates an authorisation request as a provider would: a known provider and its client, the
// registered redirect URI exactly, a state, a nonce and an S256 PKCE challenge.
func (d *DevSignIn) check(v url.Values) (signin.Provider, error) {
	p, ok := d.provider(v.Get("provider"))
	switch {
	case !ok || v.Get("client_id") != p.ClientID:
		return p, fmt.Errorf("unknown provider or client")
	case v.Get("redirect_uri") != d.callback:
		return p, fmt.Errorf("the redirect URI is not the registered one")
	case v.Get("state") == "" || v.Get("nonce") == "" || v.Get("code_challenge") == "" || v.Get("code_challenge_method") != "S256":
		return p, fmt.Errorf("the request needs a state, a nonce and an S256 code challenge")
	}
	return p, nil
}
