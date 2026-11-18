package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// req: SEC-15
// asvs: V16.2.5
func TestLogsNeverCarrySecretsNRICOrBankNumbers(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, slog.LevelDebug)
	log.Info("paid S1234567D with Bearer abc.def.ghi",
		"bank_account_number", "123-456789-001",
		"api_key", "sk-live_1234567890abcdef",
		"request", map[string]any{"authorization": "Bearer xyz.uvw.rst", "nric": "S7654321A"},
		slog.Group("payee", "account_number", "0987654321", "name", "Acme Analytics Pte Ltd"),
		"amount", "1000000000.00")
	out := buf.String()
	for _, leaked := range []string{"S1234567D", "abc.def.ghi", "456789", "sk-live_1234567890abcdef", "xyz.uvw.rst", "S7654321A", "098765"} {
		if strings.Contains(out, leaked) {
			t.Errorf("the log line contains %q:\n%s", leaked, out)
		}
	}
	for _, kept := range []string{"Acme Analytics Pte Ltd", "1000000000.00", "567D", "4321"} {
		if !strings.Contains(out, kept) {
			t.Errorf("the log line lost %q:\n%s", kept, out)
		}
	}
}

// A line logged with a request's context names its trace and span, and in the cloud the fields Cloud Logging links to
// Cloud Trace by (NFR-OBS-01); a line without one names none.
// req: NFR-OBS-01
func TestALineNamesItsTrace(t *testing.T) {
	t.Setenv("FYNIQ_TRACE_PROJECT", "fyniq-prod")
	var buf bytes.Buffer
	log := New(&buf, slog.LevelInfo).With("service", "api")
	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled}))
	log.InfoContext(ctx, "api.request")
	log.Info("startup")
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"`) || !strings.Contains(lines[0], `"span_id":"00f067aa0ba902b7"`) ||
		!strings.Contains(lines[0], `"logging.googleapis.com/trace":"projects/fyniq-prod/traces/4bf92f3577b34da6a3ce929d0e0e4736"`) ||
		!strings.Contains(lines[0], `"logging.googleapis.com/trace_sampled":true`) || !strings.Contains(lines[0], `"service":"api"`) {
		t.Errorf("a line with a trace: %s", lines[0])
	}
	if strings.Contains(lines[1], "trace") {
		t.Errorf("a line without one: %s", lines[1])
	}
}
