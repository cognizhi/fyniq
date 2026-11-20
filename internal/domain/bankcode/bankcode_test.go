package bankcode

import (
	"testing"
	"time"
)

// req: FR-PPL-12
func TestABankCodeGivesItsBICAndAnUnknownOneGivesNone(t *testing.T) {
	on := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	for code, want := range map[string]string{"7171": "DBSSSGSG", "7171081": "DBSSSGSG", "7339": "OCBCSGSG", "7375": "UOVBSGSG"} {
		if got, ok := BIC(code, on); !ok || got != want {
			t.Errorf("%s: %q %v, want %s", code, got, ok, want)
		}
	}
	for _, code := range []string{"", "0000", "717", "71711", "DBSS", "9999"} {
		if got, ok := BIC(code, on); ok || got != "" {
			t.Errorf("%q gave %q: an unknown or malformed code gives no BIC", code, got)
		}
	}
	if _, ok := BIC("7171", time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)); ok {
		t.Error("a code before its table's effective date gave a BIC")
	}
	for _, b := range Table {
		if !ValidCode(b.Code) || !ValidBIC(b.BIC) || b.Name == "" || b.From.IsZero() {
			t.Errorf("a row is not in shape: %+v", b)
		}
	}
}
