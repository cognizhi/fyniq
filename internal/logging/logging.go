// Package logging builds Fyniq's structured logger. Its output never carries secrets, tokens, full
// NRIC or bank numbers (SEC-15): values under sensitive keys are masked by name, and NRIC/FIN numbers
// and tokens are found by their shape in any text, the same rules the audit trail applies.
package logging

import (
	"io"
	"log/slog"

	"github.com/cognizhi/fyniq/internal/domain/audit"
)

// New returns a JSON logger writing to w at level.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: redact}))
}

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
