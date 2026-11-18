// Package logging builds Fyniq's structured logger. Its output never carries secrets, tokens, full
// NRIC or bank numbers (SEC-15): values under sensitive keys are masked by name, and NRIC/FIN numbers
// and tokens are found by their shape in any text, the same rules the audit trail applies. A line logged
// with a request's context names its trace (NFR-OBS-01), as Cloud Logging links it when the project is set.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"

	"github.com/cognizhi/fyniq/internal/domain/audit"
)

// New returns a JSON logger writing to w at level.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(traced{Handler: slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: redact}),
		project: os.Getenv("FYNIQ_TRACE_PROJECT")})
}

// traced adds the trace and span of the context a line is logged with: trace_id and span_id, and in the cloud
// Cloud Logging's own fields, which link the line to its trace in Cloud Trace.
type traced struct {
	slog.Handler
	project string
}

func (t traced) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
		if t.project != "" {
			r.AddAttrs(slog.String("logging.googleapis.com/trace", "projects/"+t.project+"/traces/"+sc.TraceID().String()),
				slog.String("logging.googleapis.com/spanId", sc.SpanID().String()), slog.Bool("logging.googleapis.com/trace_sampled", sc.IsSampled()))
		}
	}
	return t.Handler.Handle(ctx, r)
}

func (t traced) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traced{Handler: t.Handler.WithAttrs(attrs), project: t.project}
}

func (t traced) WithGroup(name string) slog.Handler { return traced{Handler: t.Handler.WithGroup(name), project: t.project} }

func redact(_ []string, a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		if s, ok := audit.MaskValue(a.Key, a.Value.String()).(string); ok {
			a.Value = slog.StringValue(s)
		}
	case slog.KindAny:
		a.Value = slog.AnyValue(audit.MaskValue(a.Key, a.Value.Any()))
	}
	return a
}
