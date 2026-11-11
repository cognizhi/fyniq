//go:build !local

package app

import (
	"context"
	"os"

	"go.opentelemetry.io/otel/attribute"

	"github.com/cognizhi/fyniq/internal/adapters/gcp"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/telemetry"
	"github.com/cognizhi/fyniq/internal/version"
)

// tracing is the release build's: the kept spans to Cloud Trace through the Telemetry API's OTLP endpoint, as the
// service's own account, in the project the deployment names (FYNIQ_TRACE_PROJECT, infra/); none without it
// [live-verify].
func tracing(service string) telemetry.Config {
	c := telemetry.Config{Service: service, Version: version.Version}
	project := os.Getenv("FYNIQ_TRACE_PROJECT")
	if project == "" {
		return c
	}
	tokens := gcp.MetadataTokens()
	c.Exporter = &telemetry.OTLP{URL: gcp.TelemetryAPI + "/v1/traces", Client: (&egress.Guard{}).For(egress.HostOf(gcp.TelemetryAPI)),
		Token: func(context.Context) (string, error) {
			t, err := tokens.Token()
			if err != nil {
				return "", err
			}
			return t.AccessToken, nil
		}}
	c.Attributes = []attribute.KeyValue{attribute.String("gcp.project_id", project), attribute.String("cloud.provider", "gcp"),
		attribute.String("cloud.region", "asia-southeast1")}
	return c
}
