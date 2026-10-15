//go:build local

package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/mcpdelegation"
)

const mcpIssuer = "urn:fyniq:mcp"

// delegationStack is the OAuth stack with the MCP server's delegation set up: its key, its
// JWT-bearer client and the trust relationship (open question 40, option (b)).
type delegationStack struct {
	*oauthStack
	user      uuid.UUID
	org       uuid.UUID
	key       *ecdsa.PrivateKey
	client    string
	delegator mcpdelegation.Delegator
	api       *authn.Introspector
}

func startDelegation(t *testing.T) *delegationStack {
	t.Helper()
	s := startOAuth(t, 0)
	d := &delegationStack{oauthStack: s, user: uuid.New(), org: s.e.books.Org}
	s.e.tokens.mu.Lock()
	p := s.e.tokens.m[s.user]
	p.UserID = d.user // the OAuth server knows the person by their user ID
	s.e.tokens.m[s.user] = p
	s.e.tokens.mu.Unlock()

	ctx := context.Background()
	var err error
	if d.key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
		t.Fatal(err)
	}
	var secret string
	d.client, secret, err = s.admin.CreateDelegationClient(ctx, hydra.DelegationClient{Name: "MCP delegation", Audiences: []string{apiAudience},
		Scopes: []string{"fyniq.read.accounting", "fyniq.propose.bank.categorise"}, AccessTokenLifespan: mcpdelegation.MaxLifetime})
	if err != nil {
		t.Fatalf("create the delegation client: %v", err)
	}
	d.delegator = mcpdelegation.Delegator{MCP: &authn.Introspector{OAuth: s.admin, Audience: mcpAudience, Clock: clock.System{}},
		Key: d.key, KeyID: "mcp-1", Issuer: mcpIssuer, TokenURL: "http://hydra.test/oauth2/token", ClientID: d.client, Secret: secret,
		APIAudience: apiAudience, Clock: clock.System{}, HTTP: s.browser()}
	if _, err := s.admin.Trust(ctx, hydra.TrustRelationship{Issuer: mcpIssuer, AllowAnySubject: true,
		Scopes: []string{"fyniq.read.accounting", "fyniq.propose.bank.categorise"}, Key: d.delegator.PublicKey(),
		ExpiresAt: clock.System{}.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("trust the MCP server's key: %v", err)
	}
	d.api = &authn.Introspector{OAuth: s.admin, Audience: apiAudience, Clock: clock.System{}, Delegation: &authn.Delegation{Client: d.client,
		Lookup: func(_ context.Context, u uuid.UUID) (uuid.UUID, []authz.Role, error) {
			if u != d.user {
				return uuid.Nil, nil, errors.New("not a member")
			}
			return d.org, []authz.Role{authz.Accountant}, nil
		}}}
	return d
}

// mcpToken is an MCP-audience token for the signed-in user, through login and consent.
func (d *delegationStack) mcpToken(scope string) string {
	d.t.Helper()
	c, err := d.admin.CreateClient(context.Background(), hydra.NewClient{Name: "Claude Desktop", Class: authn.ClassAgent, Audiences: []string{mcpAudience},
		RedirectURIs: []string{callback}, Scopes: strings.Fields(scope)})
	if err != nil {
		d.t.Fatal(err)
	}
	tok, refused := d.authorize(c.ID, mcpAudience, scope, true, "accept")
	if refused != "" {
		d.t.Fatalf("the MCP client's flow was refused: %s", refused)
	}
	return tok["access_token"].(string)
}

// assert signs an assertion with the MCP server's key, as a thief holding it could.
func (d *delegationStack) assert(subject string, jti string) string {
	d.t.Helper()
	s, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: jose.JSONWebKey{Key: d.key, KeyID: "mcp-1"}}, nil)
	now := clock.System{}.Now()
	b, _ := json.Marshal(map[string]any{"iss": mcpIssuer, "sub": subject, "aud": "http://hydra.test/oauth2/token", "iat": now.Unix(),
		"exp": now.Add(time.Minute).Unix(), "jti": jti})
	jws, _ := s.Sign(b)
	out, _ := jws.CompactSerialize()
	return out
}

// grant posts a JWT-bearer grant, with or without the delegation client's credentials.
func (d *delegationStack) grant(assertion, scope string, withClient bool) (int, map[string]any) {
	d.t.Helper()
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}, "scope": {scope}, "audience": {apiAudience}}
	req, _ := http.NewRequest(http.MethodPost, "http://hydra.test/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if withClient {
		req.SetBasicAuth(d.delegator.ClientID, d.delegator.Secret)
	}
	resp, err := d.browser().Do(req)
	if err != nil {
		d.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// P0 exit item 6, option (b) of open question 40, against the pinned Hydra: the MCP server turns a
// valid MCP token into an API-audience token for the same person, through the JWT-bearer grant signed
// by its own key. The MCP token itself is refused at the API (no passthrough); the delegated token
// lives at most 5 minutes, carries at most the MCP token's scopes within the trust relationship's, and
// the API treats it as the AI acting for that person with their rights now (the intersection).
// req: AI-36
// req: AI-34
// req: SEC-02
// asvs: V10.3.1, V10.4.6, V8.2.1
func TestMCPDelegationOptionB(t *testing.T) {
	ctx := context.Background()
	d := startDelegation(t)
	mcp := d.mcpToken("openid fyniq.read.accounting fyniq.propose.bank.categorise")

	if _, err := d.api.Authenticate(ctx, authn.Credentials{Authorization: "Bearer " + mcp}); !errors.Is(err, authn.ErrWrongAudience) {
		t.Fatalf("the MCP token at the API (passthrough): %v", err)
	}
	start := clock.System{}.Now()
	tok, err := d.delegator.APIToken(ctx, "Bearer "+mcp)
	took := clock.System{}.Now().Sub(start)
	if err != nil {
		t.Fatalf("delegation: %v", err)
	}
	if tok.AccessToken == mcp || tok.Subject != d.user.String() {
		t.Fatalf("the delegated token: %+v", tok)
	}
	in, err := d.admin.Introspect(ctx, tok.AccessToken)
	if err != nil || !in.Active {
		t.Fatalf("introspect the delegated token: %v %+v", err, in)
	}
	life := in.ExpiresAt.Sub(in.IssuedAt)
	t.Logf("measured: delegation took %s; the token lives %s; audiences %v; scopes %v; client %s", took, life, in.Audiences, in.Scopes, in.ClientID)
	if life > mcpdelegation.MaxLifetime+time.Second || !slices.Contains(in.Audiences, apiAudience) || slices.Contains(in.Audiences, mcpAudience) {
		t.Errorf("the delegated token lives %s for %v; want at most 5 minutes, for the API only", life, in.Audiences)
	}
	for _, s := range in.Scopes {
		if !slices.Contains([]string{"fyniq.read.accounting", "fyniq.propose.bank.categorise"}, s) {
			t.Errorf("the delegated token has the scope %q, beyond the MCP token's", s)
		}
	}
	p, err := d.api.Authenticate(ctx, authn.Credentials{Authorization: "Bearer " + tok.AccessToken})
	if err != nil {
		t.Fatalf("the API refused the delegated token: %v", err)
	}
	if !p.AI() || p.UserID != d.user || p.Organisation != d.org || !slices.Equal(p.Roles, []authz.Role{authz.Accountant}) {
		t.Fatalf("the API's principal: %+v", p)
	}
	if authz.Decide(authz.CreateJournal, p.Roles, p.AI()).Allowed || !authz.Decide(authz.ListAccounts, p.Roles, p.AI()).Allowed {
		t.Error("the delegated token is not limited to what the AI may do for the person (AI-34)")
	}

	// Only a valid MCP token is delegated; no assertion is made for anything else.
	for name, authorization := range map[string]string{"no token": "", "a made-up token": "Bearer ory_at_madeup", "a dev token": "Bearer " + authn.DevPrefix + "x.y"} {
		if _, err := d.delegator.APIToken(ctx, authorization); !errors.Is(err, mcpdelegation.ErrRefused) {
			t.Errorf("%s was delegated: %v", name, err)
		}
	}
}

// What option (b) cannot prevent, measured for the security reviewer (docs/adr/ADR-T3-mcp-delegation-spike.md).
// The trust relationship allows any subject, so whoever holds the MCP server's key can obtain a token
// for any person, within the agent class and that person's rights; an assertion is used once; a scope
// beyond the trust relationship is refused; and revoking the person's consents does not reach a
// delegated token, which only its 5-minute life ends.
// req: AI-36
// req: AI-38
// asvs: V10.4.6
func TestMCPDelegationLimitsAreMeasured(t *testing.T) {
	ctx := context.Background()
	d := startDelegation(t)
	other := uuid.NewString()

	status, out := d.grant(d.assert(other, "jti-1"), "fyniq.read.accounting", true)
	t.Logf("measured: a stolen key's assertion for another person: %d %v", status, out["error"])
	if status != http.StatusOK {
		t.Fatalf("expected the known weakness (any subject) to hold: %d %v", status, out)
	}
	if p, err := d.api.Authenticate(ctx, authn.Credentials{Authorization: "Bearer " + out["access_token"].(string)}); err == nil {
		t.Errorf("the API accepted a delegated token for someone with no membership: %+v", p)
	}

	if status, out := d.grant(d.assert(d.user.String(), "jti-1"), "fyniq.read.accounting", true); status == http.StatusOK {
		t.Errorf("a used assertion (same jti) was accepted again: %v", out)
	}
	status, out = d.grant(d.assert(d.user.String(), "jti-2"), "fyniq.read.reports", true)
	t.Logf("measured: a scope beyond the trust relationship: %d %v", status, out["error"])
	if status == http.StatusOK && strings.Contains(out["scope"].(string), "fyniq.read.reports") {
		t.Errorf("a scope beyond the trust relationship was granted: %v", out)
	}
	status, out = d.grant(d.assert(d.user.String(), "jti-3"), "fyniq.read.accounting", false)
	t.Logf("measured: the grant without the delegation client's credentials: %d %v", status, out["error"])

	status, out = d.grant(d.assert(d.user.String(), "jti-4"), "fyniq.read.accounting", true)
	if status != http.StatusOK {
		t.Fatalf("a delegated token: %d %v", status, out)
	}
	if err := d.admin.RevokeSubject(ctx, d.user.String()); err != nil {
		t.Fatal(err)
	}
	in, err := d.admin.Introspect(ctx, out["access_token"].(string))
	t.Logf("measured: after revoking the person's consents, the delegated token is active: %v (%v)", in.Active, err)
}
