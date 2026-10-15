package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// req: SEC-15
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
