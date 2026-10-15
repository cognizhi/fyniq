package oauthflow_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/oauthflow"
	"github.com/cognizhi/fyniq/internal/ports"
)

// req: AI-37
// asvs: V10.4.11, V10.7.2
func TestOnlyKnownScopesAreGranted(t *testing.T) {
	for scope, want := range map[string]bool{
		"openid": true, "offline_access": true, "fyniq.read.accounting": true, "fyniq.read.proposals": true,
		"fyniq.propose.bank.categorise": true, "fyniq.read.payroll": false, "fyniq.read.equity": false,
		"fyniq.write.accounting": false, "fyniq.read.": false, "admin": false, "fyniq.propose.": false,
	} {
		if got := oauthflow.Grantable(scope); got != want {
			t.Errorf("Grantable(%q) = %v, want %v", scope, got, want)
		}
	}
}

// req: AI-36
// req: AI-34
// asvs: V10.3.1, V10.7.1
func TestConsentGrantsOnlyWhatTheClientRecordAllows(t *testing.T) {
	f := oauthflow.Flow{APIAudience: "https://api", MCPAudience: "https://mcp"}
	user := authn.Principal{Subject: "u", Organisation: uuid.New(), Roles: []authz.Role{authz.Accountant}}
	client := ports.OAuthClient{ID: "c", Class: authn.ClassAgent, Audiences: []string{"https://mcp"}}
	req := ports.OAuthConsent{Subject: "u", Client: client, RequestedAudiences: []string{"https://mcp"},
		RequestedScopes: []string{"openid", "fyniq.read.accounting", "fyniq.read.payroll", "sudo"}}
	d := f.Decide(req, user)
	if d.Problem != "" || strings.Join(d.Scopes, " ") != "openid fyniq.read.accounting" || strings.Join(d.RefusedScopes, " ") != "fyniq.read.payroll sudo" ||
		strings.Join(d.Audiences, " ") != "https://mcp" {
		t.Fatalf("Decide = %+v", d)
	}
	for name, change := range map[string]func(c *ports.OAuthConsent, p *authn.Principal){
		"an audience the client record does not allow": func(c *ports.OAuthConsent, _ *authn.Principal) { c.RequestedAudiences = []string{"https://api"} },
		"an audience that is neither the API nor MCP": func(c *ports.OAuthConsent, _ *authn.Principal) {
			c.Client.Audiences = append(c.Client.Audiences, "https://evil")
			c.RequestedAudiences = []string{"https://evil"}
		},
		"no audience":                       func(c *ports.OAuthConsent, _ *authn.Principal) { c.RequestedAudiences = nil },
		"a client without a class":          func(c *ports.OAuthConsent, _ *authn.Principal) { c.Client.Class = "" },
		"a class the request made up":       func(c *ports.OAuthConsent, _ *authn.Principal) { c.Client.Class = "superuser" },
		"a user outside every organisation": func(_ *ports.OAuthConsent, p *authn.Principal) { p.Organisation = uuid.Nil },
	} {
		c, p := req, user
		c.Client.Audiences = append([]string(nil), client.Audiences...)
		change(&c, &p)
		if d := f.Decide(c, p); d.Problem == "" {
			t.Errorf("%s: consent was not refused: %+v", name, d)
		}
	}
}
