// Package mcpgate holds the policy that decides whether an AI app may act for a person over MCP (PRD-001 AI-35, AI-37;
// p1-mcp-first-party design D2, D7): the first-party clients the release pre-registers, and the gate the consent app asks
// before it grants a token and the API asks again on every call. Closed by default at four levels: the deployment's switch
// (the MCP server's own), the organisation's switch (features.mcp, an Owner's, dual controlled), the allowlist (mcp.clients)
// and the person's opt-in. Each refusal is plain words with a stable code (ENG-23).
package mcpgate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	contracts "github.com/cognizhi/fyniq/contracts/ai"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The codes of a refusal (ENG-23).
const (
	CodeOrganisationOff = "MCP-GATE-01" // the organisation has not turned AI apps on
	CodeNotAllowed      = "MCP-GATE-02" // the app is not on the organisation's allowlist
	CodeNotOptedIn      = "MCP-GATE-03" // the person has not opted in
)

// Client is a first-party MCP client the release pre-registers.
type Client struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Publisher    string   `json:"publisher"`
	RedirectURIs []string `json:"redirect_uris"`
	Verify       string   `json:"verify"`
}

// Clients reads the release's first-party clients strictly.
func Clients() ([]Client, error) {
	var f struct {
		Version int      `json:"version"`
		Note    string   `json:"note"`
		Clients []Client `json:"clients"`
	}
	d := json.NewDecoder(bytes.NewReader(contracts.MCPClients))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return nil, fmt.Errorf("mcp-clients.json: %w", err)
	}
	if f.Version != 1 || len(f.Clients) == 0 {
		return nil, errors.New("mcp-clients.json: no clients, or a version this release does not read")
	}
	seen := map[string]bool{}
	for _, c := range f.Clients {
		if c.ID == "" || c.Name == "" || c.Publisher == "" || len(c.RedirectURIs) == 0 || seen[c.ID] {
			return nil, fmt.Errorf("mcp-clients.json: %q needs an ID that is its own, a name, a publisher and redirect URIs", c.ID)
		}
		seen[c.ID] = true
		for _, u := range c.RedirectURIs {
			if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://localhost") && !strings.HasPrefix(u, "http://127.0.0.1") && !strings.HasPrefix(u, "http://[::1]") {
				return nil, fmt.Errorf("mcp-clients.json: %s's redirect %q is neither https nor a loopback address", c.ID, u)
			}
			if strings.ContainsAny(u, "*#") {
				return nil, fmt.Errorf("mcp-clients.json: %s's redirect %q has a wildcard or a fragment: redirects are exact", c.ID, u)
			}
		}
	}
	return f.Clients, nil
}

// DefaultScopes is what a first-party client may ask for at most in P1: the default grant, the other read areas a person
// consents to, and the proposals' status. Payroll and equity are never in it (AI-37).
var DefaultScopes = []string{"openid", "offline_access", "fyniq.read.reports", "fyniq.read.sales", "fyniq.read.purchases",
	"fyniq.read.banking", "fyniq.read.accounting", "fyniq.read.planning", "fyniq.read.compliance", "fyniq.read.proposals"}

// Register registers the first-party clients with the OAuth server for the MCP audience, as agent-class public clients.
func Register(ctx context.Context, oauth ports.OAuthClients, audience string) error {
	clients, err := Clients()
	if err != nil {
		return err
	}
	for _, c := range clients {
		if _, err := oauth.RegisterPublicClient(ctx, ports.PublicRegistration{ID: c.ID, Name: c.Name, Class: "agent", Audience: audience,
			Scopes: DefaultScopes, RedirectURIs: c.RedirectURIs, App: c.ID}); err != nil {
			return fmt.Errorf("registering %s: %w", c.ID, err)
		}
	}
	return nil
}

// Gate asks the organisation's settings and the person's opt-in.
type Gate struct {
	Store storage.Store
	Clock ports.Clock
}

// Admit is "" when the person may use the app over MCP in the organisation now, and otherwise why not, in plain words, with
// the code. An error is a failure to ask, which the caller treats as a refusal (fail closed).
func (g Gate) Admit(ctx context.Context, org, user uuid.UUID, app string) (code, why string, err error) {
	err = g.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		loc, lerr := time.LoadLocation(o.Profile.TimeZone)
		if lerr != nil || o.Profile.TimeZone == "" {
			loc = time.UTC
		}
		y, m, d := g.Clock.Now().In(loc).Date()
		today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		changes, err := u.Settings().Changes(ctx, "")
		if err != nil {
			return err
		}
		value := func(key string) string {
			def, _ := settings.Lookup(key)
			return settings.On(def, changes, today).Value
		}
		if value("features.mcp") != "true" {
			code, why = CodeOrganisationOff, "AI apps are not turned on for this organisation: an Owner turns them on in Settings, with a second approver."
			return nil
		}
		allowed := false
		for _, a := range strings.Split(value("mcp.clients"), ",") {
			if strings.TrimSpace(a) == app && app != "" {
				allowed = true
			}
		}
		if !allowed {
			code, why = CodeNotAllowed, "This AI app is not on the organisation's list of apps that may connect: an Owner adds it in Settings, with a second approver."
			return nil
		}
		opt, ok, err := u.MCPOptIns().Latest(ctx, user)
		if err != nil {
			return err
		}
		if !ok || !opt.OptedIn {
			code, why = CodeNotOptedIn, "You have not allowed AI apps to work for you: opt in under Settings, Connected apps."
		}
		return nil
	})
	return code, why, err
}
