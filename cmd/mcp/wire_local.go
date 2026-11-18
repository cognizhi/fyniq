//go:build local

package main

import (
	"errors"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/mcpserver"
)

// introspector is how the MCP server asks the OAuth server what a token is: Hydra's admin API, which it may call for
// introspection and for nothing else (the type is the narrow mcpserver.Introspector), through a guard that allows the local
// stack's addresses, as the platform's own guard does in local mode.
func introspector(admin string) (mcpserver.Introspector, error) {
	if admin == "" {
		return nil, errors.New("the MCP server needs FYNIQ_OAUTH_ADMIN_URL to check tokens")
	}
	return hydra.New(admin, (&egress.Guard{Local: true, Propagate: true}).Bounded(0, 10*time.Second).For(egress.HostOf(admin))), nil
}
