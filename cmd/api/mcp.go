package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
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
				return
			}
			log.Warn("mcp.clients_not_registered", "error", err.Error(), "attempt", attempt+1)
			time.Sleep(5 * time.Second)
		}
	}()
	return mcpgate.Gate{Store: store, Clock: clock}, publishers
}
