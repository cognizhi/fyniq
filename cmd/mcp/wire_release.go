//go:build !local

package main

import (
	"errors"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"

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

// backend is the MCP server's way to the data in the cloud [live-verify]: the delegation key is Cloud KMS's
// (EC_SIGN_P256_SHA256) and the delegation client authenticates with private_key_jwt (SEC-07), neither of which is wired yet,
// and the service token is Google's for the api's audience. Until they are, a release build refuses to start the server with
// the switch on, so the endpoint is never half open; the launch shell is what runs.
func backend(ports.Clock, func(string) string) (mcpserver.Backend, error) {
	return nil, errors.New("the cloud's delegation key (Cloud KMS) and service identity are not wired yet [live-verify]")
}
