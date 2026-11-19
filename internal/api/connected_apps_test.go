//go:build local

package api

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// fakeConsents stands in for the OAuth server's consent sessions.
type fakeConsents struct {
	mu      sync.Mutex
	granted map[string][]ports.AgentConsent
	revoked []string
}

func (f *fakeConsents) RevokeAgentConsents(context.Context, string) error { return nil }
func (f *fakeConsents) AgentConsents(_ context.Context, subject string) ([]ports.AgentConsent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.granted[subject], nil
}
func (f *fakeConsents) RevokeClientConsent(_ context.Context, subject, client string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, subject+" "+client)
	return nil
}

// Connected apps show a person what the organisation allows and what they have allowed, an Owner the registrations waiting, and the
// trail; a person disconnects their own apps and only theirs, an Owner denies a registration once, and nobody else does either (AI-44,
// AI-35, AI-90).
// req: AI-44
// req: AI-35
// req: AI-90
// asvs: V8.2.1, V8.2.2, V4.1.1
func TestConnectedAppsShowWhatAPersonAndAnOwnerNeed(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ctx := context.Background()
	ahkow := f.as(f.ahkow, authz.Owner, authz.Controller)
	weiling := f.as(f.weiling, authz.Owner, authz.Controller)
	staff := e.as(authz.Employee)
	granted := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	consents := &fakeConsents{granted: map[string][]ports.AgentConsent{f.ahkow: {{ClientID: "claude-code", ClientName: "Claude Code", Scopes: []string{"openid", "fyniq.read.reports"}, GrantedAt: granted}}}}
	e.api.deps.OAuthConsents = consents
	e.api.deps.OAuthClients, e.api.deps.MCPClients = fakeOAuthClients{}, e.store
	defer func() { e.api.deps.OAuthConsents, e.api.deps.OAuthClients, e.api.deps.MCPClients = nil, nil, nil }()
	y, m, d := e.clock.Now().In(appclock.Singapore).Date()
	from := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		for key, v := range map[string][2]string{"features.mcp": {"false", "true"}, "mcp.clients": {"", "claude-code,mcp-00000000000000000009"}, "mcp.registration": {"false", "true"}} {
			if _, err := u.Settings().Record(ctx, key, v[0], v[1], from, 1); err != nil {
				return err
			}
		}
		return nil
	})
	refused, err := e.store.Register(ctx, storage.MCPClientRegistration{ID: "mcp-00000000000000000009", Name: "Acme Books", RedirectURIs: []string{"https://acme.example/cb"}, Network: "203.0.113.0/24", At: e.clock.Now()},
		storage.MCPClientLimits{PerHour: 100, PerDay: 100, PendingCap: 100})
	if err != nil || refused != "" {
		t.Fatal(refused, err)
	}

	view := e.call(http.MethodGet, "/v1/connected-apps", ahkow, nil)
	apps, _ := view.Body["apps"].([]any)
	conns, _ := view.Body["connections"].([]any)
	if view.Status != 200 || len(apps) != 3 || len(conns) != 1 || view.Body["registration_on"] != true {
		t.Fatalf("the page: %d %s", view.Status, view.Raw)
	}
	byID := map[string]map[string]any{}
	for _, a := range apps {
		byID[a.(map[string]any)["id"].(string)] = a.(map[string]any)
	}
	if byID["claude-code"]["verified"] != true || byID["claude-code"]["allowed"] != true || byID["claude-desktop"]["allowed"] != false ||
		byID["mcp-00000000000000000009"]["verified"] != false || byID["mcp-00000000000000000009"]["name"] != "Acme Books" || byID["mcp-00000000000000000009"]["status"] != "pending" {
		t.Errorf("the apps: %v", byID)
	}
	c := conns[0].(map[string]any)
	if c["client_id"] != "claude-code" || c["name"] != "Claude Code" || c["expires_at"] != "2027-01-30T09:00:00Z" {
		t.Errorf("the connection: %v", c)
	}
	if other := e.call(http.MethodGet, "/v1/connected-apps", weiling, nil); len(other.Body["connections"].([]any)) != 0 {
		t.Errorf("another person's connections: %s", other.Raw)
	}

	// An Owner sees everyone's connections and revokes any member's; a person with no right to Connected apps does neither, and a stranger
	// is no member (AI-44, AI-38).
	everyone := e.call(http.MethodGet, "/v1/connected-apps/connections", weiling, nil)
	if list, _ := everyone.Body["connections"].([]any); everyone.Status != 200 || len(list) != 1 || list[0].(map[string]any)["user_id"] != f.ahkow || list[0].(map[string]any)["client_id"] != "claude-code" {
		t.Errorf("everyone's connections: %d %s", everyone.Status, everyone.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/connected-apps/connections", staff, nil); r.Status != 403 {
		t.Errorf("an Employee lists everyone's connections: %d", r.Status)
	}
	if r := e.call(http.MethodDelete, "/v1/connected-apps/people/"+f.ahkow+"/connections/claude-code", staff, nil); r.Status != 403 {
		t.Errorf("an Employee revokes another's app: %d", r.Status)
	}
	if r := e.call(http.MethodDelete, "/v1/connected-apps/people/"+uuid.NewString()+"/connections/claude-code", weiling, nil); r.Status != 404 {
		t.Errorf("a stranger's app: %d", r.Status)
	}
	if r := e.call(http.MethodDelete, "/v1/connected-apps/people/"+f.ahkow+"/connections/claude-code", weiling, nil); r.Status != 204 || len(consents.revoked) != 1 || consents.revoked[0] != f.ahkow+" claude-code" {
		t.Errorf("an Owner's revoke: %d %v", r.Status, consents.revoked)
	}
	consents.revoked = nil

	// A person disconnects their own app, only their own.
	if r := e.call(http.MethodDelete, "/v1/connected-apps/connections/claude-code", ahkow, nil); r.Status != 204 {
		t.Fatalf("disconnecting: %d %s", r.Status, r.Raw)
	}
	if len(consents.revoked) != 1 || consents.revoked[0] != f.ahkow+" claude-code" {
		t.Errorf("what was revoked: %v", consents.revoked)
	}
	if r := e.call(http.MethodDelete, "/v1/connected-apps/connections/Claude%20Code", ahkow, nil); r.Status != 422 && r.Status != 404 {
		t.Errorf("a client ID that is not one: %d", r.Status)
	}

	// The registrations waiting, for those who see Connected apps; denied once.
	if r := e.call(http.MethodGet, "/v1/connected-apps/registrations", ahkow, nil); r.Status != 200 || len(r.Body["registrations"].([]any)) != 1 {
		t.Fatalf("the registrations: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/connected-apps/registrations", staff, nil); r.Status != 403 {
		t.Errorf("an Employee saw the registrations: %d", r.Status)
	}
	if r := e.call(http.MethodPost, "/v1/connected-apps/registrations/mcp-00000000000000000009/deny", staff, nil); r.Status != 403 {
		t.Errorf("an Employee denied a registration: %d", r.Status)
	}
	if r := e.call(http.MethodPost, "/v1/connected-apps/registrations/mcp-00000000000000000009/deny", ahkow, nil); r.Status != 204 {
		t.Fatalf("denying: %d %s", r.Status, r.Raw)
	}
	if st, _ := e.store.ClientStatus(ctx, "mcp-00000000000000000009"); st != storage.MCPClientDenied {
		t.Errorf("after denying: %q", st)
	}
	if r := e.call(http.MethodPost, "/v1/connected-apps/registrations/mcp-00000000000000000009/deny", ahkow, nil); r.Status != 404 || r.str("code") != CodeConnectedNoRegist {
		t.Errorf("denying twice: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/connected-apps/registrations", ahkow, nil); len(r.Body["registrations"].([]any)) != 0 {
		t.Errorf("a denied registration still waits: %s", r.Raw)
	}

	// The trail: a person's own, everyone's for those who see Connected apps, nobody else's for an Employee.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		for _, who := range []string{f.ahkow, f.weiling} {
			if _, err := u.MCPCalls().Record(ctx, storage.MCPCall{UserID: uuid.MustParse(who), App: "claude-code", Tool: "metrics.query", Outcome: storage.MCPRead, At: e.clock.Now()}); err != nil {
				return err
			}
		}
		return nil
	})
	own := e.call(http.MethodGet, "/v1/connected-apps/activity", ahkow, nil)
	all := e.call(http.MethodGet, "/v1/connected-apps/activity?all=true", ahkow, nil)
	if len(own.Body["calls"].([]any)) != 1 || len(all.Body["calls"].([]any)) != 2 {
		t.Errorf("own %s, all %s", own.Raw, all.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/connected-apps/activity?all=true", staff, nil); r.Status != 403 {
		t.Errorf("an Employee read everyone's activity: %d", r.Status)
	}
}
