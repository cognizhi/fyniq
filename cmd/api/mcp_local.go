//go:build local

package main

import (
	"cmp"
	"context"
	"os"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/mcpdelegation"
	"github.com/cognizhi/fyniq/internal/mcpgate"
	"github.com/cognizhi/fyniq/internal/ports"
)

// ensureDelegation registers, at the OAuth server, the MCP service's delegation client and the trust relationship for its key
// in local mode: the key is derived from a seed both services read (mcpdelegation.LocalKey), so the API registers the public half
// and the MCP service signs with the private. Registering again replaces both. The trust relationship allows any subject, within
// the agent class and the read scopes a first-party client may hold; that is option (b)'s known limit (ADR-T3 spike), accepted by
// the founders on 16 Oct 2026 and answered by the API's per-call gates. In the cloud OpenTofu registers it, with Cloud KMS's key
// [live-verify].
func ensureDelegation(ctx context.Context, oauth *hydra.Admin, clock ports.Clock) error {
	client := delegationClient()
	if client == "" {
		return nil
	}
	key, err := mcpdelegation.LocalKey(cmp.Or(os.Getenv("FYNIQ_MCP_DELEGATION_SEED"), "fyniq-local-only-mcp-delegation-seed"))
	if err != nil {
		return err
	}
	d := mcpdelegation.Delegator{Key: key, KeyID: "mcp-local-1"}
	audience := apiAudience()
	scopes := nonProtocol(mcpgate.DefaultScopes)
	if _, _, err := oauth.CreateDelegationClient(ctx, hydra.DelegationClient{ID: client, Secret: cmp.Or(os.Getenv("FYNIQ_MCP_DELEGATION_SECRET"), "fyniq-local-only-delegation-secret"),
		Name: "Fyniq MCP service (delegation)", Audiences: []string{audience}, Scopes: scopes, AccessTokenLifespan: mcpdelegation.MaxLifetime}); err != nil {
		return err
	}
	_, err = oauth.Trust(ctx, hydra.TrustRelationship{Issuer: "urn:fyniq:mcp", AllowAnySubject: true, Scopes: scopes, Key: d.PublicKey(),
		ExpiresAt: clock.Now().Add(365 * 24 * time.Hour)})
	return err
}
