//go:build local

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/members"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/setupcode"
	"github.com/cognizhi/fyniq/internal/signin"
)

// sessionStack is a fresh installation with the whole way in: the local sign-in page, the sign-in
// flow, first-party sessions in PostgreSQL and the API, on one origin as a browser sees it.
type sessionStack struct {
	*env
	migrator string
	readonly string // the fresh database's read-only role, for the installation's trail
	api      string // its API role, as the attack tests connect
	out      outbox.Dispatcher
}

func startSessions(t *testing.T) *sessionStack {
	t.Helper()
	e, fresh := freshInstallation(t)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	out := outbox.Dispatcher{Store: e.store, Queue: local.NewMemory(), Clock: e.clock}
	flow := signin.Flow{Sessions: e.store, Clock: e.clock, Origin: srv.URL, Base: "", Log: slog.New(slog.NewJSONHandler(e.log, nil)),
		Alert: signin.EmailAlert(e.store, e.store, out)}
	dev, err := local.NewDevSignIn([]signin.Provider{local.DevSignInProvider}, "", flow.Callback(), e.clock)
	if err != nil {
		t.Fatal(err)
	}
	flow.SignIn = dev
	flow.Register(mux)
	dev.Register(mux)
	s := &sessionStack{env: e, migrator: fresh.URL("fyniq_migrator"), readonly: fresh.URL("fyniq_readonly"), api: fresh.URL("fyniq_api"),
		out: out}
	auth := authn.Local{Dev: e.tokens, Sessions: authn.Sessions{Store: e.store, Clock: e.clock, Cookie: authn.SessionCookie(false)}}
	e.api = New(mux, Deps{Store: e.store, Installation: e.store, Outbox: &s.out, Clock: e.clock, Auth: auth, SignIn: dev,
		Log: slog.New(slog.NewJSONHandler(e.log, nil)), Version: "0.0.0-test", Origin: strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)}) // a relying party ID is a host name
	e.srv = srv
	return s
}

// browser is a user agent with its own cookies on the stack's origin; email, when set, is the address its sign-ins
// give the provider.
type browser struct {
	s     *sessionStack
	c     *http.Client
	email string
}

func (s *sessionStack) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{s: s, c: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}

// signIn goes through the sign-in page as subject and returns where the browser ended.
func (b *browser) signIn(subject string, secondFactor bool) string {
	b.s.t.Helper()
	resp, err := b.c.Get(b.s.srv.URL + "/auth/signin?provider=local&return_to=" + url.QueryEscape("/after?x=1"))
	if err != nil {
		b.s.t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/auth/dev/authorize" {
		b.s.t.Fatalf("the sign-in page: %d %s", resp.StatusCode, resp.Request.URL)
	}
	form := resp.Request.URL.Query()
	form.Set("subject", subject)
	form.Set("name", "Mei Ling Tan")
	form.Set("decision", "sign-in")
	if secondFactor {
		form.Set("second_factor", "yes")
	}
	if b.email != "" {
		form.Set("email", b.email)
	}
	resp, err = b.c.PostForm(b.s.srv.URL+"/auth/dev/authorize", form)
	if err != nil {
		b.s.t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.Request.URL.RequestURI()
}

// call sends a request with the browser's cookies; site is its Sec-Fetch-Site ("" for none).
func (b *browser) call(method, path, site string, body any, headers ...string) response {
	b.s.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, b.s.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if site != "" {
		req.Header.Set("Sec-Fetch-Site", site)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.s.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := response{Status: resp.StatusCode, Header: resp.Header, Raw: string(raw)}
	if len(raw) > 0 && raw[0] == '{' {
		_ = json.Unmarshal(raw, &out.Body)
	}
	return out
}

func (b *browser) session() response {
	return b.call(http.MethodGet, "/v1/session", "same-origin", nil)
}

func roles(r response) string {
	rs, _ := r.Body["roles"].([]any)
	var out []string
	for _, x := range rs {
		out = append(out, x.(string))
	}
	return strings.Join(out, ",")
}

// A person signs in through the sign-in page and gets a first-party session: an HttpOnly cookie, never
// a bearer token in the browser (SEC-08). The session is in the database, which resolves its identity's
// user, organisation and live roles on every request, so first-run setup, a role change and a recovery
// apply at the next request. It ends after 30 minutes idle, 12 hours in all, and at sign-out (SEC-05;
// a recovery: TestARecoveryEndsTheSessionsItReplaces). A write it authenticates must come from the web
// app's own origin.
// req: SEC-05
// req: SEC-08
// req: FR-ADM-06
// req: FR-ADM-08
// req: FR-ORG-02
// asvs: V7.2.1, V7.3.1, V7.3.2, V7.4.1, V7.4.2, V3.5.3, V8.3.1
func TestBrowserSessions(t *testing.T) {
	ctx := context.Background()
	s := startSessions(t)
	a := s.browser()

	if r := a.call(http.MethodGet, "/v1/sign-in-providers", "", nil); r.Status != 200 || !strings.Contains(r.Raw, `"id":"local"`) {
		t.Fatalf("the providers: %d %s", r.Status, r.Raw)
	}
	s.problemIs(a.session(), "getSession", 401, CodeUnauthenticated)
	if end := a.signIn("founder-a", true); end != "/after?x=1" {
		t.Fatalf("after signing in the browser is at %s", end)
	}
	r := a.session()
	if r.Status != 200 || r.Body["provider"] != "local" || r.Body["user_id"] != nil || roles(r) != "" || r.Body["super_admin"] != false ||
		r.Body["stepped_up_until"] != s.clock.Now().Add(authn.StepUpWindow).Format(time.RFC3339) {
		t.Fatalf("a new identity's session: %d %s", r.Status, r.Raw)
	}

	// First-run setup through the session: refused from another site, done from the web app.
	code, hash := setupcode.New()
	if err := postgres.SetSetupCode(ctx, s.migrator, hash); err != nil {
		t.Fatal(err)
	}
	// No base currency: it defaults to SGD (FR-ORG-02).
	setup := map[string]any{"setup_code": code, "display_name": "Mei Ling Tan", "organisation": map[string]any{"name": "Cognizhi Pte. Ltd."}}
	for _, site := range []string{"", "cross-site", "same-site"} {
		s.problemIs(a.call(http.MethodPost, "/v1/setup", site, setup), "completeSetup", 403, CodeCrossSite)
	}
	r = a.call(http.MethodPost, "/v1/setup", "same-origin", setup)
	if r.Status != http.StatusCreated {
		t.Fatalf("setup through the session: %d %s", r.Status, r.Raw)
	}
	user, org := r.str("user_id"), r.str("organisation_id")
	r = a.session()
	if r.Body["user_id"] != user || r.Body["organisation_id"] != org || roles(r) != "owner" || r.Body["super_admin"] != true {
		t.Fatalf("the session after setup: %s", r.Raw)
	}
	if r := a.call(http.MethodGet, "/v1/organisation", "same-origin", nil); r.Status != 200 || r.Body["name"] != "Cognizhi Pte. Ltd." ||
		r.Body["base_currency"] != "SGD" {
		t.Fatalf("the organisation, by the session's membership: %d %s", r.Status, r.Raw)
	}
	if r := a.call(http.MethodGet, "/v1/organisation", "cross-site", nil); r.Status != 200 {
		t.Errorf("a read from another site is not a state change: %d", r.Status)
	}
	s.problemIs(a.call(http.MethodGet, "/v1/session", "same-origin", nil, "Authorization", "Bearer not-a-token"), "getSession", 401, CodeUnauthenticated)

	// A role change applies at the next request: no token or snapshot to wait for. (The last Owner keeps the
	// role, rev 0.35, so the founder gains a role and loses it again.)
	userID, orgID := uuid.MustParse(user), uuid.MustParse(org)
	as := storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: user}})
	given, err := members.Assign(as, s.store, s.out, orgID, userID, string(authz.Accountant), nil, "test: the founder keeps the books")
	if err != nil {
		t.Fatal(err)
	}
	if r := a.session(); roles(r) != "accountant,owner" {
		t.Errorf("the new role is not in the session: %s", r.Raw)
	}
	if err := members.Revoke(as, s.store, s.out, orgID, userID, given.ID, "test: the founder hands the books over"); err != nil {
		t.Fatal(err)
	}
	if r := a.session(); roles(r) != "owner" {
		t.Errorf("the revoked role is still in the session: %s", r.Raw)
	}
	s.problemIs(a.call(http.MethodPost, "/v1/journals", "same-origin", map[string]any{}), "createJournal", 403, CodeForbidden)

	// 30 minutes idle end a session; activity keeps it to 12 hours at most.
	s.clock.Advance(authn.SessionIdle + time.Minute)
	s.problemIs(a.session(), "getSession", 401, CodeUnauthenticated)
	a.signIn("founder-a", false)
	if r := a.session(); r.Status != 200 || r.Body["stepped_up_until"] != nil {
		t.Fatalf("a sign-in without a second factor: %s", r.Raw)
	}
	for start := s.clock.Now(); s.clock.Now().Sub(start) < authn.SessionLifetime-25*time.Minute; {
		s.clock.Advance(25 * time.Minute)
		if r := a.session(); r.Status != 200 {
			t.Fatalf("an active session ended %v after it began", s.clock.Now().Sub(start))
		}
	}
	s.clock.Advance(25 * time.Minute)
	s.problemIs(a.session(), "getSession", 401, CodeUnauthenticated)

	// Signing out ends the session.
	a.signIn("founder-a", false)
	if r := a.call(http.MethodPost, "/auth/signout", "same-origin", nil); r.Status != 200 && r.Status != 404 { // 303, then "/" on this mux
		t.Fatalf("sign-out: %d", r.Status)
	}
	s.problemIs(a.session(), "getSession", 401, CodeUnauthenticated)

	if !strings.Contains(s.log.String(), `"msg":"auth.signed_in"`) || strings.Contains(s.log.String(), code) {
		t.Error("the sign-ins were not logged, or the setup code was (SEC-15)")
	}
}

// A recovery ends every session of the Super admin but the recovering one (SEC-35): the sessions
// recorded as theirs, and those of the sign-ins it replaced, even one that began before the sign-in had
// a user and was never used again (a laptop closed after setup).
// req: SEC-35
// req: FR-ADM-09
// req: SEC-05
// asvs: V7.4.2, V7.4.3
func TestARecoveryEndsTheSessionsItReplaces(t *testing.T) {
	ctx := context.Background()
	s := startSessions(t)
	laptop, desk := s.browser(), s.browser()
	laptop.signIn("founder-a", false) // before setup: the session has no user, and is never used again
	desk.signIn("founder-a", true)
	code, hash := setupcode.New()
	if err := postgres.SetSetupCode(ctx, s.migrator, hash); err != nil {
		t.Fatal(err)
	}
	r := desk.call(http.MethodPost, "/v1/setup", "same-origin", map[string]any{"setup_code": code, "display_name": "Mei Ling Tan",
		"organisation": map[string]any{"name": "Cognizhi Pte. Ltd.", "base_currency": "SGD"}})
	if r.Status != http.StatusCreated {
		t.Fatalf("setup: %d %s", r.Status, r.Raw)
	}
	user := r.str("user_id")
	if r := desk.session(); r.Body["user_id"] != user {
		t.Fatalf("the desk's session after setup: %s", r.Raw)
	}
	now := s.clock.Now()
	confirmation, chash := setupcode.New()
	if err := postgres.IssueDeploymentRecovery(ctx, s.migrator, uuid.MustParse(user), chash, "Mei Ling Tan, Wei Jie Lim", now, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	phone := s.browser()
	phone.signIn("founder-a-new-phone", false) // the lost authenticator is why they recover: the two proofs stand in for it
	if r := phone.call(http.MethodPost, "/v1/super-admins/recovery", "same-origin", map[string]any{"confirmation_code": confirmation}); r.Status != 200 {
		t.Fatalf("the recovery: %d %s", r.Status, r.Raw)
	}
	s.problemIs(desk.session(), "getSession", 401, CodeUnauthenticated)
	s.problemIs(laptop.session(), "getSession", 401, CodeUnauthenticated)
	if r := phone.session(); r.Body["user_id"] != user || r.Body["super_admin"] != true {
		t.Fatalf("the recovering session: %s", r.Raw)
	}
	if strings.Contains(s.log.String(), confirmation) {
		t.Error("the recovery code reached the log (SEC-15)")
	}
}

// The web app and a direct caller of the API reach the same command: the web app has no private path
// (QA-API-03), so the same input is refused the same way through the session and through a token,
// with the same status, code and invalid fields.
// req: FR-INT-01
func TestTheWebAppAndTheAPIRefuseTheSameInput(t *testing.T) {
	ctx := context.Background()
	s := startSessions(t)
	a := s.browser()
	a.signIn("founder-a", true)
	code, hash := setupcode.New()
	if err := postgres.SetSetupCode(ctx, s.migrator, hash); err != nil {
		t.Fatal(err)
	}
	r := a.call(http.MethodPost, "/v1/setup", "same-origin", map[string]any{"setup_code": code, "display_name": "Mei Ling Tan",
		"organisation": map[string]any{"name": "Cognizhi Pte. Ltd.", "base_currency": "SGD"}})
	if r.Status != http.StatusCreated {
		t.Fatalf("setup: %d %s", r.Status, r.Raw)
	}
	user, org := uuid.MustParse(r.str("user_id")), uuid.MustParse(r.str("organisation_id"))
	if _, err := members.Assign(storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: user.String()}}),
		s.store, s.out, org, user, string(authz.Accountant), nil, "test: the founder keeps the books"); err != nil {
		t.Fatal(err)
	}
	a.signIn("founder-a", true)                 // a role change ends the member's sessions and tokens
	token := authn.DevPrefix + uuid.NewString() // the same person, calling the API with a token
	s.tokens.mu.Lock()
	s.tokens.m[token] = authn.Principal{Subject: "founder-a", UserID: user, Client: "script", Class: authn.ClassUser, Organisation: org,
		Roles: []authz.Role{authz.Accountant}}
	s.tokens.mu.Unlock()

	// Two lines wrong in two ways: too many decimals, and a currency that does not exist.
	bad := map[string]any{"date": "2026-10-20", "memo": "October rent", "lines": []map[string]any{
		{"account_id": uuid.NewString(), "amount": map[string]any{"amount": "100.005", "currency": "SGD"}},
		{"account_id": uuid.NewString(), "amount": map[string]any{"amount": "-100.00", "currency": "XYZ"}},
	}}
	web := a.call(http.MethodPost, "/v1/journals", "same-origin", bad, "Idempotency-Key", uuid.NewString())
	api := s.call(http.MethodPost, "/v1/journals", token, bad, "Idempotency-Key", uuid.NewString())
	s.problemIs(web, "createJournal", 422, CodeInvalid)
	s.problemIs(api, "createJournal", 422, CodeInvalid)
	wf, af := fields(web), fields(api)
	if len(wf) < 2 || len(wf) != len(af) {
		t.Fatalf("the web app's refusal lists %v, the API's %v", wf, af)
	}
	for field, code := range wf {
		if af[field] != code {
			t.Errorf("%s: %s through the web app, %q through the API", field, code, af[field])
		}
	}
}
