//go:build local

package main

import (
	"cmp"
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/mcpdelegation"
	"github.com/cognizhi/fyniq/internal/mcpserver"
	"github.com/cognizhi/fyniq/internal/ports"
)

// localTaskKey is the local service tokens' key (internal/wiring): local only, valid nowhere else (SEC-12).
const localTaskKey = "fyniq-local-only-task-key"

// introspector is how the MCP server asks the OAuth server what a token is: Hydra's admin API, which it may call for
// introspection and for nothing else (the type is the narrow mcpserver.Introspector), through a guard that allows the local
// stack's addresses, as the platform's own guard does in local mode.
func introspector(admin string) (mcpserver.Introspector, error) {
	if admin == "" {
		return nil, errors.New("the MCP server needs FYNIQ_OAUTH_ADMIN_URL to check tokens")
	}
	return hydra.New(admin, (&egress.Guard{Local: true, Propagate: true}).Bounded(0, 10*time.Second).For(egress.HostOf(admin))), nil
}

// backend is the MCP server's way to the data in local mode (design D3, D8): the delegation key derived from the local seed (the
// API registers its public half), Hydra's public token endpoint for the JWT-bearer grant, and the API's internal operation with
// the MCP service's local service token. The cloud's key is Cloud KMS's and its client authenticates with private_key_jwt
// [live-verify].
func backend(clock ports.Clock, getenv func(string) string) (mcpserver.Backend, error) {
	apiURL := getenv("FYNIQ_API_INTERNAL_URL")
	tokenURL := getenv("FYNIQ_OAUTH_TOKEN_URL")
	if apiURL == "" || tokenURL == "" {
		return nil, errors.New("the MCP server needs FYNIQ_API_INTERNAL_URL and FYNIQ_OAUTH_TOKEN_URL to reach the data")
	}
	key, err := mcpdelegation.LocalKey(cmp.Or(getenv("FYNIQ_MCP_DELEGATION_SEED"), "fyniq-local-only-mcp-delegation-seed"))
	if err != nil {
		return nil, err
	}
	hydraHost := egress.HostOf(tokenURL)
	d := mcpdelegation.Delegator{Key: key, KeyID: "mcp-local-1", Issuer: "urn:fyniq:mcp", TokenURL: tokenURL, Audience: getenv("FYNIQ_OAUTH_ISSUER") + "oauth2/token", ClientID: cmp.Or(getenv("FYNIQ_MCP_DELEGATION_CLIENT"), "mcp-delegation"),
		Secret: cmp.Or(getenv("FYNIQ_MCP_DELEGATION_SECRET"), "fyniq-local-only-delegation-secret"), APIAudience: getenv("FYNIQ_API_AUDIENCE"), Clock: clock,
		HTTP: (&egress.Guard{Local: true, Propagate: true}).Bounded(1<<20, 10*time.Second).For(hydraHost)}
	if _, err := url.Parse(apiURL); err != nil || d.APIAudience == "" {
		return nil, errors.New("the MCP server needs the API's URL and FYNIQ_API_AUDIENCE")
	}
	tokens := local.NewHMACTokens([]byte(cmp.Or(getenv("FYNIQ_TASK_TOKEN_KEY"), localTaskKey)), clock, time.Hour)
	return mcpserver.APIBackend{URL: apiURL, Delegate: d, ServiceToken: func(context.Context) (string, error) { return tokens.Token("mcp") },
		HTTP: (&egress.Guard{Local: true, Propagate: true}).Bounded(8<<20, 30*time.Second).For(egress.HostOf(apiURL))}, nil
}
