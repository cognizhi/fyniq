//go:build local

package app

import (
	"os"
	"strings"

	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/telemetry"
	"github.com/cognizhi/fyniq/internal/version"
)

// tracing is the local build's: the kept spans to an OTLP/HTTP collector when FYNIQ_OTLP_ENDPOINT names one (such as
// http://localhost:4318), none otherwise.
func tracing(service string) telemetry.Config {
	c := telemetry.Config{Service: service, Version: version.Version}
	if endpoint := strings.TrimSuffix(os.Getenv("FYNIQ_OTLP_ENDPOINT"), "/"); endpoint != "" {
		c.Exporter = &telemetry.OTLP{URL: endpoint + "/v1/traces", Client: (&egress.Guard{Local: true}).For(egress.HostOf(endpoint))}
	}
	return c
}
