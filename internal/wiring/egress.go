package wiring

import (
	"net/http"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/egress"
)

// The platform's own outbound connections go through PlatformEgress (SEC-36; p1-security-hardening design D3): Hydra,
// the render service and Google's APIs, each to the hosts its configuration names. Connections to outside services (the
// email providers, the connectors) go through a guard that also refuses the platform's own services (Kit.Egress).

// Hydra is Hydra's admin API at admin, through the platform's guard to admin's host only, with a short timeout.
func Hydra(admin string) *hydra.Admin {
	return hydra.New(admin, PlatformEgress().Bounded(0, 10*time.Second).For(egress.HostOf(admin)))
}

// People returns the client the worker reaches Nemura with (the link's token endpoint and API): the platform's guard, to those hosts only, with a short timeout (SEC-36). The hosts are the
// deployment's settings, never a link's row, so a link cannot point the worker at another host.
func People(hosts ...string) *http.Client {
	return PlatformEgress().Bounded(0, 15*time.Second).For(hosts...)
}
