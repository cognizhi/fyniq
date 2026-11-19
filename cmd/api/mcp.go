package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/mcpgate"
	"github.com/cognizhi/fyniq/internal/oauthflow"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// mcpConsent is what an MCP consent needs (p1-mcp-first-party tasks 3.1, 3.2): the gate that asks the organisation's switch,
// its allowlist and the person's opt-in, the first-party clients' verified publishers, and the registration of those clients
// with the OAuth server. Without a database or an MCP audience there is no gate, so no MCP consent is ever granted (closed by
// default). The clients are registered in the background, since the OAuth server may start after the API; registering again
// replaces, so a release that changes a client's redirects reaches a running server.
func mcpConsent(oauth *hydra.Admin, audience string, store storage.Store, clock ports.Clock, log *slog.Logger) (oauthflow.MCPGate, map[string]string) {
	if store == nil || audience == "" {
		return nil, nil
	}
	clients, err := mcpgate.Clients()
	if err != nil {
		log.Error("mcp.clients_invalid", "error", err.Error())
		return nil, nil
	}
	publishers := map[string]string{}
	for _, c := range clients {
		publishers[c.ID] = c.Publisher
	}
	go func() {
		for attempt := 0; attempt < 30; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := mcpgate.Register(ctx, oauth, audience)
			cancel()
			if err == nil {
				err = ensureDelegation(ctx, oauth, clock)
			}
			if err == nil {
				return
			}
			log.Warn("mcp.clients_not_registered", "error", err.Error(), "attempt", attempt+1)
			time.Sleep(5 * time.Second)
		}
	}()
	return mcpgate.Gate{Store: store, Clock: clock}, publishers
}

// delegationClient is the MCP service's delegation client at the OAuth server (open question 40, option (b)): its tokens are the
// ones the API accepts for an agent acting for a person. Empty, the API accepts none (a release build until the cloud wiring,
// [live-verify]).
func delegationClient() string { return os.Getenv("FYNIQ_MCP_DELEGATION_CLIENT") }

// delegation makes the API accept the delegated tokens of delegationClient: for a person in the organisation the MCP token
// named, an active member of it, with the roles they hold now. A person who is not a member of it gets no token.
func delegation(sessions storage.Sessions, clock ports.Clock) *authn.Delegation {
	store, ok := sessions.(storage.Store)
	client := delegationClient()
	if !ok || client == "" {
		return nil
	}
	return &authn.Delegation{Client: client, LookupIn: func(ctx context.Context, org, user uuid.UUID) ([]authz.Role, error) {
		var roles []authz.Role
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			assigned, err := u.Members().Roles(ctx, user, clock.Now())
			for _, a := range assigned {
				if _, known := authz.Roles[authz.Role(a.Role)]; known {
					roles = append(roles, authz.Role(a.Role))
				}
			}
			return err
		})
		if err == nil && len(roles) == 0 {
			err = errors.New("not an active member of the organisation")
		}
		return roles, err
	}}
}

// nonProtocol are the scopes without openid and offline_access, which a delegated token does not carry.
func nonProtocol(scopes []string) []string {
	var out []string
	for _, s := range scopes {
		if s != "openid" && s != "offline_access" {
			out = append(out, s)
		}
	}
	return out
}
