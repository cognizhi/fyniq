//go:build local

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/apiclient"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	revoke "github.com/cognizhi/fyniq/internal/tokens"
)

// patToken asks Hydra for an API token as a personal access token: the client-credentials grant with the client ID and the
// secret as HTTP Basic credentials, as the response of createPersonalAccessToken says.
func patToken(t *testing.T, s *oauthStack, clientID, secret, scope, audience string) (int, map[string]any) {
	t.Helper()
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {scope}, "audience": {audience}}
	req, err := http.NewRequest(http.MethodPost, "http://hydra.test/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(secret))
	resp, err := s.browser().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// A personal access token (FR-INT-02, AI-34) end to end, against the pinned Hydra: a person signed in and stepped up makes
// one; every invalid field is refused at once and its expiry is within 90 days; the answer carries the secret once, and
// nothing Fyniq keeps (its record, its lists, its audit trail) ever does. The OAuth server issues its tokens for the API's
// audience and its registered scopes only, and the API accepts them as the agent class: they read what the member may
// and its scopes cover, and write nothing, a write scope notwithstanding, and they make no credential of their own.
// Networks and the client's own limit apply. An Administrator asks to reclass it as an integration; the Owner approves with
// a second approver; only then may it write, audited as the integration. A member revokes their own at once, another
// member's is not found to them, an Administrator revokes any, and the worker deletes the client from Hydra.
// req: FR-INT-02
// req: AI-34
// req: SEC-07
// req: FR-INT-16
// req: AUD-01
// asvs: V10.3.1, V10.4.10, V8.2.1, V8.3.1, V16.3.2, V13.3.1
func TestPersonalAccessTokens(t *testing.T) {
	s := startOAuth(t, 0)
	e := s.e
	e.api.deps.OAuthClients, e.api.deps.APIAudience = s.admin, apiAudience
	api := tokenAPI(t, s)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	mine, other := f.as(f.ahkow, authz.Owner, authz.Controller), f.as(f.weiling, authz.Owner, authz.Controller)
	// Ah Kow keeps the books too: the token acts for him, so once it is an integration it may post what an Accountant may.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		m, err := u.Members().Membership(ctx, uuid.MustParse(f.ahkow))
		if err != nil {
			return err
		}
		_, err = u.Members().Assign(ctx, m.ID, "accountant", nil)
		return err
	})
	body := func(name string, extra map[string]any) map[string]any {
		b := map[string]any{"name": name, "scopes": []string{"fyniq.read.accounting", "fyniq.write.accounting"},
			"expires_at": e.clock.Now().AddDate(0, 0, 60).UTC().Format(time.RFC3339)}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}

	// Only a person signed in to Fyniq and stepped up makes one: not an OAuth client, not without step-up.
	notStepped := e.principal(authn.Principal{Subject: "founder-" + f.ahkow, UserID: uuid.MustParse(f.ahkow), Client: "web", Class: authn.ClassUser,
		Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}, StepUpAt: e.clock.Now().Add(-10 * time.Minute)})
	e.problemIs(e.call(http.MethodPost, "/v1/personal-access-tokens", notStepped, body("No step-up", nil)), "createPersonalAccessToken", 403, CodeStepUpRequired)
	e.problemIs(e.call(http.MethodPost, "/v1/personal-access-tokens", e.as(authz.Owner), body("No user", nil)), "createPersonalAccessToken", 403, CodeSessionRequired)

	// Every invalid field at once, each naming the field and the fix; a year is more than 90 days.
	bad := e.call(http.MethodPost, "/v1/personal-access-tokens", mine, map[string]any{"name": " x", "scopes": []string{"fyniq.write.payroll"},
		"networks": []string{"10.0.0.1/99"}, "expires_at": e.clock.Now().AddDate(0, 4, 0).Format(time.RFC3339)})
	e.problemIs(bad, "createPersonalAccessToken", 422, apiclient.CodeName)
	if errs, _ := bad.Body["errors"].([]any); len(errs) != 4 {
		t.Errorf("want 4 field problems at once (name, scope, network, expiry), got %s", bad.Raw)
	}

	create := func(who string, b map[string]any) response {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/personal-access-tokens", who, b)
		if r.Status != http.StatusCreated {
			t.Fatalf("creating %v: %d %s", b["name"], r.Status, r.Raw)
		}
		return r
	}
	made := create(mine, body("Claude Code", nil))
	secret, clientID := made.str("client_secret"), made.str("client_id")
	if secret == "" || clientID == "" || made.str("class") != authn.ClassAgent || made.str("owner_id") != f.ahkow || made.str("audience") != apiAudience ||
		made.str("status") != "live" || made.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("the new token: %s", made.Raw)
	}

	// Shown once: neither its lists, nor its record, nor the audit trail has the secret.
	for _, l := range []response{e.call(http.MethodGet, "/v1/personal-access-tokens/mine", mine, nil), e.call(http.MethodGet, "/v1/personal-access-tokens", admin, nil)} {
		if l.Status != http.StatusOK || !strings.Contains(l.Raw, made.str("id")) || strings.Contains(l.Raw, secret) || strings.Contains(l.Raw, "client_secret") {
			t.Errorf("a list of tokens: %d %s", l.Status, l.Raw)
		}
	}
	if l := e.call(http.MethodGet, "/v1/personal-access-tokens/mine", other, nil); strings.Contains(l.Raw, made.str("id")) {
		t.Errorf("another member lists a member's token: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/integration-clients", admin, nil); strings.Contains(l.Raw, made.str("id")) {
		t.Errorf("a personal token among the integration clients: %s", l.Raw)
	}
	trail := workerStore(t)
	var registered bool
	if err := trail.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 100000)
		for _, ev := range events {
			if strings.Contains(ev.Body, secret) {
				t.Error("the audit trail holds the token's secret")
			}
			registered = registered || (strings.Contains(ev.Body, `"api_client.registered"`) && strings.Contains(ev.Body, clientID))
		}
		return err
	}); err != nil || !registered {
		t.Errorf("the token's registration is not audited with its client ID: %v", err)
	}

	// Hydra issues tokens for the API's audience and the registered scopes, to the secret alone.
	tokenOf := func(secret, scope, audience string) (int, string) {
		status, out := patToken(t, s, clientID, secret, scope, audience)
		tok, _ := out["access_token"].(string)
		return status, tok
	}
	if status, _ := tokenOf("not-the-secret", "fyniq.read.accounting", apiAudience); status == http.StatusOK {
		t.Error("a token for a wrong secret")
	}
	if status, _ := tokenOf(secret, "fyniq.read.banking", apiAudience); status == http.StatusOK {
		t.Error("a scope the token was not registered with")
	}
	if status, _ := tokenOf(secret, "fyniq.read.accounting", mcpAudience); status == http.StatusOK {
		t.Error("another audience (AI-36)")
	}
	status, tok := tokenOf(secret, "fyniq.read.accounting fyniq.write.accounting", apiAudience)
	if status != http.StatusOK || tok == "" {
		t.Fatalf("the token's access token: %d", status)
	}

	// An agent reads what its member and its scopes cover, and writes nothing, whatever its scopes (AI-34, CTL-MCP-01).
	if r := api.call(http.MethodGet, "/v1/accounts", tok, nil); r.Status != http.StatusOK {
		t.Fatalf("reading the chart as the agent token: %d %s", r.Status, r.Raw)
	}
	for _, x := range []struct{ method, path string }{{http.MethodPost, "/v1/journals"}, {http.MethodGet, "/v1/members"}, {http.MethodGet, "/v1/approvals"},
		{http.MethodGet, "/v1/personal-access-tokens"}, {http.MethodGet, "/v1/personal-access-tokens/mine"}} {
		r := api.call(x.method, x.path, tok, e.draft("1.00"))
		if r.Status != http.StatusForbidden || r.problemCode() != CodeForbidden {
			t.Errorf("%s %s as the agent token: %d %s", x.method, x.path, r.Status, r.Raw)
		}
	}
	// No credential makes another: the token cannot create a token, with or without a session.
	if r := api.call(http.MethodPost, "/v1/personal-access-tokens", tok, body("Child", nil)); r.Status != http.StatusForbidden {
		t.Errorf("a token creating a token: %d %s", r.Status, r.Raw)
	}

	// Its own read limit (FR-INT-16) and its networks (SEC-07).
	slow := create(mine, body("Slow", map[string]any{"reads_per_minute": 2}))
	_, out := patToken(t, s, slow.str("client_id"), slow.str("client_secret"), "fyniq.read.accounting", apiAudience)
	slowTok, _ := out["access_token"].(string)
	var limited response
	for range 3 {
		limited = api.call(http.MethodGet, "/v1/accounts", slowTok, nil)
	}
	if limited.Status != http.StatusTooManyRequests || limited.Header.Get("Retry-After") == "" || !strings.Contains(limited.Header.Get("RateLimit-Policy"), "q=2") {
		t.Errorf("the token's own read limit: %d %v", limited.Status, limited.Header)
	}
	far := create(mine, body("Far away", map[string]any{"networks": []string{"203.0.113.0/24"}}))
	_, out = patToken(t, s, far.str("client_id"), far.str("client_secret"), "fyniq.read.accounting", apiAudience)
	farTok, _ := out["access_token"].(string)
	if r := api.call(http.MethodGet, "/v1/accounts", farTok, nil); r.Status != http.StatusForbidden || r.problemCode() != CodeForbidden {
		t.Errorf("a call from outside its networks: %d %s", r.Status, r.Raw)
	}

	// Reclass: an Administrator asks, the Owner approves with a second approver; the token is an agent until then.
	reclass := "/v1/personal-access-tokens/" + made.str("id") + "/reclass"
	e.problemIs(e.call(http.MethodPost, reclass, mine, map[string]any{"reason": "Not an Administrator"}), "requestPersonalAccessTokenReclass", 403, CodeForbidden)
	asked := e.call(http.MethodPost, reclass, admin, map[string]any{"reason": "It feeds the nightly import."})
	if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectAPIClient || asked.str("operation") != approval.OpAPIClientReclass {
		t.Fatalf("asking to reclass: %d %s", asked.Status, asked.Raw)
	}
	if r := api.call(http.MethodPost, "/v1/journals", tok, e.draft("2.00")); r.Status != http.StatusForbidden {
		t.Fatalf("an agent token wrote before its reclass was approved: %d %s", r.Status, r.Raw)
	}
	e.problemIs(f.decide(admin, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
	if d := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK || d.str("status") != "approved" {
		t.Fatalf("approving the reclass: %d %s", d.Status, d.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/personal-access-tokens/mine", mine, nil); !strings.Contains(strings.ReplaceAll(l.Raw, " ", ""), `"class":"integration"`) {
		t.Fatalf("the reclassed token: %s", l.Raw)
	}
	draft := api.call(http.MethodPost, "/v1/journals", tok, e.draft("3.00"))
	if draft.Status != http.StatusCreated {
		t.Fatalf("a draft journal as the reclassed token: %d %s", draft.Status, draft.Raw)
	}
	var audited bool
	if err := trail.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 100000)
		for _, ev := range events {
			audited = audited || (strings.Contains(ev.Body, `"integration"`) && strings.Contains(ev.Body, clientID) && strings.Contains(ev.Body, draft.str("id")))
		}
		return err
	}); err != nil || !audited {
		t.Errorf("the reclassed token's draft is not audited as the integration with its client ID (AUD-01): %v", err)
	}
	e.problemIs(e.call(http.MethodPost, reclass, admin, map[string]any{"reason": "Again"}), "requestPersonalAccessTokenReclass", 409, apiclient.CodeState)

	// Revoking: another member's token is not found to a member; theirs is revoked at once; an Administrator revokes any.
	e.problemIs(e.call(http.MethodPost, "/v1/personal-access-tokens/mine/"+made.str("id")+"/revoke", other, map[string]any{"reason": "Not mine"}),
		"revokeMyPersonalAccessToken", 404, CodeNotFound)
	if r := e.call(http.MethodPost, "/v1/personal-access-tokens/mine/"+made.str("id")+"/revoke", mine, map[string]any{"reason": "The laptop was lost."}); r.Status != http.StatusOK ||
		r.str("status") != "revoked" || r.str("revoke_reason") != "The laptop was lost." {
		t.Fatalf("revoking my token: %d %s", r.Status, r.Raw)
	}
	if r := api.call(http.MethodGet, "/v1/accounts", tok, nil); r.Status != http.StatusUnauthorized {
		t.Errorf("a revoked token's access token: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/personal-access-tokens/mine/"+made.str("id")+"/revoke", mine, map[string]any{"reason": "Again"}),
		"revokeMyPersonalAccessToken", 409, apiclient.CodeState)
	e.problemIs(e.call(http.MethodPost, "/v1/personal-access-tokens/"+far.str("id")+"/revoke", mine, map[string]any{"reason": "Not an Administrator"}),
		"revokePersonalAccessToken", 403, CodeForbidden)
	if r := e.call(http.MethodPost, "/v1/personal-access-tokens/"+far.str("id")+"/revoke", admin, map[string]any{"reason": "The member left."}); r.Status != http.StatusOK ||
		r.str("status") != "revoked" {
		t.Fatalf("an Administrator revoking: %d %s", r.Status, r.Raw)
	}
	// The worker deletes the client from Hydra.
	var removal storage.OutboxRow
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		row, err := u.Outbox().ByKey(ctx, revoke.RemoveClientKind, "oauth.client.remove:"+clientID)
		if row != nil {
			removal = *row
		}
		return err
	})
	if removal.Kind != revoke.RemoveClientKind {
		t.Fatal("no removal queued for the worker")
	}
	if err := revoke.RemoveClientWork(s.admin)(context.Background(), e.books.Org, removal); err != nil {
		t.Fatal(err)
	}
	if status, _ := tokenOf(secret, "fyniq.read.accounting", apiAudience); status == http.StatusOK {
		t.Error("Hydra still issues tokens to a removed client")
	}
}

// A personal access token never does more than its member: the same token and scopes read the chart for a member who may
// view it and are refused for an Employee, who may not, although the AI column of Appendix D allows the view (AI-34, the
// intersection of the member's roles and the AI's).
// req: AI-34
// req: FR-INT-02
// asvs: V8.2.1, V8.3.1
func TestAPersonalAccessTokenNeverExceedsItsMember(t *testing.T) {
	s := startOAuth(t, 0)
	e := s.e
	e.api.deps.OAuthClients, e.api.deps.APIAudience = s.admin, apiAudience
	api := tokenAPI(t, s)
	member := matrixMemberUser(e) // an Employee, who may not view the chart
	read := func(user uuid.UUID) response {
		t.Helper()
		id, secret, err := s.admin.RegisterPersonal(context.Background(), ports.PersonalRegistration{Name: "Member's", Class: authn.ClassAgent, Audience: apiAudience,
			Scopes: []string{"fyniq.read.accounting"}, TokenLifespan: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.APIClients().Create(ctx, storage.APIClient{Kind: storage.APIClientPersonal, ClientID: id, Name: "Member's", Class: authn.ClassAgent, OwnerID: user,
				Scopes: []string{"fyniq.read.accounting"}, ExpiresAt: e.clock.Now().AddDate(0, 0, 30), CreatedBy: "user:" + user.String(), CreatedAt: e.clock.Now()})
			return err
		})
		status, out := patToken(t, s, id, secret, "fyniq.read.accounting", apiAudience)
		tok, _ := out["access_token"].(string)
		if status != http.StatusOK || tok == "" {
			t.Fatalf("the token: %d", status)
		}
		return api.call(http.MethodGet, "/v1/accounts", tok, nil)
	}
	if r := read(member); r.Status != http.StatusForbidden || r.problemCode() != CodeForbidden {
		t.Fatalf("a token of an Employee, who may not view the chart: %d %s", r.Status, r.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error { // the member is given a role that may view it
		m, err := u.Members().Membership(ctx, member)
		if err != nil {
			return err
		}
		_, err = u.Members().Assign(ctx, m.ID, "accountant", nil)
		return err
	})
	if r := read(member); r.Status != http.StatusOK {
		t.Errorf("a token of the same member once they may view the chart: %d %s", r.Status, r.Raw)
	}
}

// matrixPAT is a live personal access token of the matrix's member, made once.
func matrixPAT(e *env) string {
	e.t.Helper()
	var id string
	owner := uuid.MustParse(matrixApprover(e))
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.APIClients().List(ctx, storage.APIClientPersonal)
		if err != nil || len(all) > 0 {
			if len(all) > 0 {
				id = all[0].ID.String()
			}
			return err
		}
		c, err := u.APIClients().Create(ctx, storage.APIClient{Kind: storage.APIClientPersonal, ClientID: "matrix-pat-" + uuid.NewString(), Name: "Matrix token",
			Class: authn.ClassAgent, OwnerID: owner, Scopes: []string{"fyniq.read.reports"}, ExpiresAt: e.clock.Now().AddDate(0, 0, 30),
			CreatedBy: "user:" + owner.String(), CreatedAt: e.clock.Now()})
		id = c.ID.String()
		return err
	})
	return id
}

var matrixPersonalTokens = map[string]func(e *env, tok string) (response, func() bool){
	"createPersonalAccessToken": func(e *env, tok string) (response, func() bool) {
		was, aud := e.api.deps.OAuthClients, e.api.deps.APIAudience
		defer func() { e.api.deps.OAuthClients, e.api.deps.APIAudience = was, aud }()
		e.api.deps.OAuthClients, e.api.deps.APIAudience = fakeOAuthClients{}, apiAudience
		return e.call(http.MethodPost, "/v1/personal-access-tokens", steppedUp(e, tok), map[string]any{"name": "Matrix new " + uuid.NewString()[:8],
			"scopes": []string{"fyniq.read.reports"}}), nil
	},
	"listMyPersonalAccessTokens": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/personal-access-tokens/mine", steppedUp(e, tok), nil), nil
	},
	"listPersonalAccessTokens": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/personal-access-tokens", tok, nil), nil
	},
	"revokeMyPersonalAccessToken": func(e *env, tok string) (response, func() bool) {
		id := matrixPAT(e)
		return e.call(http.MethodPost, "/v1/personal-access-tokens/mine/"+id+"/revoke", steppedUp(e, tok), map[string]any{"reason": "Matrix"}, "Prefer", "return=preview"), nil
	},
	"revokePersonalAccessToken": func(e *env, tok string) (response, func() bool) {
		id := matrixPAT(e)
		return e.call(http.MethodPost, "/v1/personal-access-tokens/"+id+"/revoke", tok, map[string]any{"reason": "Matrix"}, "Prefer", "return=preview"), nil
	},
	"requestPersonalAccessTokenReclass": func(e *env, tok string) (response, func() bool) {
		id := matrixPAT(e)
		return e.call(http.MethodPost, "/v1/personal-access-tokens/"+id+"/reclass", steppedUp(e, tok), map[string]any{"reason": "Matrix"}, "Prefer", "return=preview"), nil
	},
}
