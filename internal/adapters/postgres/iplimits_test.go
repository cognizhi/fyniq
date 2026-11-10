//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The per-IP windows count a client's requests of one kind a minute and keep no address (a SHA-256 of it); only the API
// counts and only the jobs purge, and no application role writes the table (SEC-23, migration 000312).
// req: SEC-23
// req: QA-ENG-05
// asvs: V2.4.1
func TestPerIPWindowsCountAndPurge(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	sum := sha256.Sum256([]byte("fyniq-ip:203.0.113.7"))
	client := hex.EncodeToString(sum[:])
	start := time.Date(2099, 3, 1, 9, 0, 0, 0, time.UTC)
	for want := 1; want <= 3; want++ {
		if used, err := api.CountIP(ctx, client, storage.IPSignIn, start); err != nil || used != want {
			t.Fatalf("request %d: %d, %v", want, used, err)
		}
	}
	if used, err := api.CountIP(ctx, client, storage.IPSetup, start); err != nil || used != 1 {
		t.Fatalf("another kind has its own window: %d, %v", used, err)
	}
	if _, err := api.CountIP(ctx, "203.0.113.7", storage.IPSignIn, start); err == nil {
		t.Fatal("an address was kept instead of its hash")
	}
	if _, err := api.CountIP(ctx, client, "everything", start); err == nil {
		t.Fatal("a kind the limits do not count was kept")
	}
	if _, err := api.PurgeIP(ctx, start.Add(time.Hour)); err == nil {
		t.Fatal("the API purged the windows; only the jobs do")
	}
	if n, err := worker.PurgeIP(ctx, start.Add(time.Minute)); err != nil || n != 2 {
		t.Fatalf("purging the ended windows: %d, %v", n, err)
	}
	for _, sql := range []string{`INSERT INTO fyniq.ip_rate_window (client, kind, window_start) VALUES ('` + client + `', 'join', now())`,
		`DELETE FROM fyniq.ip_rate_window`} {
		if _, err := conn(t, "fyniq_api").Exec(ctx, sql); sqlstate(err) != "42501" {
			t.Errorf("the API role ran %.30s: %v", sql, err)
		}
	}
}
