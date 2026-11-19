package mcpgate

import (
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/oauthflow"
)

// The first-party clients are what the release pre-registers: each has an ID that is its allowlist name, a publisher, and
// exact redirects (https, or loopback with no wildcard), and the scopes they may ask for are the read areas P1 has and never
// payroll or equity (AI-35, AI-37).
// req: AI-35
// req: AI-37
// asvs: V10.4.1
func TestTheFirstPartyClientsAreExactAndNarrow(t *testing.T) {
	clients, err := Clients()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range clients {
		ids = append(ids, c.ID)
		for _, u := range c.RedirectURIs {
			if strings.Contains(u, "*") || strings.Contains(u, "#") {
				t.Errorf("%s: %s is not exact", c.ID, u)
			}
		}
	}
	for _, app := range []string{"claude-code", "claude-desktop"} {
		if !slices.Contains(ids, app) {
			t.Errorf("%s is not pre-registered", app)
		}
	}
	for _, s := range DefaultScopes {
		if !oauthflow.Grantable(s) || strings.Contains(s, "payroll") || strings.Contains(s, "equity") || strings.HasPrefix(s, "fyniq.propose") {
			t.Errorf("%s may not be in a first-party client's scopes", s)
		}
	}
	// The settings' allowlist can name exactly the pre-registered apps.
	for _, id := range ids {
		if !slices.Contains(allowlistChoices(), id) {
			t.Errorf("%s is pre-registered but cannot be put on the allowlist", id)
		}
	}
}

func allowlistChoices() []string {
	d, _ := settings.Lookup("mcp.clients")
	return d.Choices
}
