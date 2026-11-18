//go:build !local

package main

import (
	"errors"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/mcpserver"
)

// introspector is how the MCP server asks the OAuth server what a token is: Hydra's admin API, for introspection and
// nothing else (the type is the narrow mcpserver.Introspector), through the platform's guard to that host only. In the cloud
// the admin API is reached over a route that allows introspection alone [live-verify]: the admin API can otherwise create
// clients and trust relationships, which the MCP service must never be able to do.
func introspector(admin string) (mcpserver.Introspector, error) {
	if admin == "" {
		return nil, errors.New("the MCP server needs FYNIQ_OAUTH_ADMIN_URL to check tokens")
	}
	return hydra.New(admin, (&egress.Guard{Propagate: true}).Bounded(0, 10*time.Second).For(egress.HostOf(admin))), nil
}
