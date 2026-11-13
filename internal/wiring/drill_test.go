package wiring

import (
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The restore drill reaches its copy with the service's own URL pointed at it: Cloud SQL's private address with the port
// kept, or the copy's database locally; the role, password and TLS settings stay; a URL that is not one is refused (DR-06).
// req: DR-06
func TestTheDrillsCopyIsReachedWithTheServicesOwnURL(t *testing.T) {
	base := "postgres://fyniq_worker:secret@10.0.0.3:5432/fyniq?sslmode=require"
	for _, c := range []struct {
		target ports.CloneTarget
		want   string
	}{
		{ports.CloneTarget{Name: "fyniq-production-drill-1", Host: "10.20.0.7"}, "postgres://fyniq_worker:secret@10.20.0.7:5432/fyniq?sslmode=require"},
		{ports.CloneTarget{Name: "fyniq_drill_1", Database: "fyniq_drill_1"}, "postgres://fyniq_worker:secret@10.0.0.3:5432/fyniq_drill_1?sslmode=require"},
	} {
		if got, err := pointedAt(base, c.target); err != nil || got != c.want {
			t.Errorf("%+v: %q, %v", c.target, got, err)
		}
	}
	if got, err := pointedAt("postgres://u@db/fyniq", ports.CloneTarget{Host: "10.20.0.7"}); err != nil || got != "postgres://u@10.20.0.7/fyniq" {
		t.Errorf("no port: %q, %v", got, err)
	}
	if _, err := pointedAt("host=10.0.0.3 dbname=fyniq", ports.CloneTarget{Host: "10.20.0.7"}); err == nil {
		t.Error("a keyword DSN was pointed at the copy")
	}
}
