package outbox

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// platform records heartbeats in memory, or refuses them.
type platform struct {
	storage.Platform
	beats  map[string]time.Time
	refuse bool
}

func (p *platform) Beat(_ context.Context, name string, at time.Time, _ string) error {
	if p.refuse {
		return errors.New("the database is down")
	}
	p.beats[name] = at
	return nil
}

func (p *platform) Heartbeats(context.Context) (map[string]time.Time, error) { return p.beats, nil }

// Each heartbeat the database records is also logged as fyniq.heartbeat with its job, which Cloud Monitoring's absence
// alerts watch whatever the database's state; a heartbeat the database refused is not logged, so its absence alerts. The
// hourly check logs each missing or late heartbeat as fyniq.heartbeat_missing and fails (NFR-OBS-02).
// req: NFR-OBS-02
// req: CTL-RES-05
func TestEveryHeartbeatIsLoggedAndEveryMissingOne(t *testing.T) {
	var buf bytes.Buffer
	before := HeartbeatLog
	HeartbeatLog = slog.New(slog.NewJSONHandler(&buf, nil))
	t.Cleanup(func() { HeartbeatLog = before })
	now := time.Date(2099, 3, 1, 2, 0, 0, 0, time.UTC)
	p := &platform{beats: map[string]time.Time{}}
	if err := Beat(context.Background(), p, "tick", now, "1 started"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"event":"fyniq.heartbeat","job":"tick","at":"2099-03-01T02:00:00Z"`) {
		t.Fatalf("the heartbeat's line: %s", buf.String())
	}
	buf.Reset()
	if err := Beat(context.Background(), &platform{refuse: true}, "tick", now, ""); err == nil || buf.Len() != 0 {
		t.Fatalf("a refused heartbeat: %v, logged %q", err, buf.String())
	}
	schedules := []Schedule{{Job: "ledger.verify", Every: 24 * time.Hour, Stale: 26 * time.Hour}, {Job: "audit.anchor", Every: time.Hour, Stale: 3 * time.Hour}}
	p.beats["ledger.verify"] = now.Add(-25 * time.Hour)
	p.beats["audit.anchor"] = now.Add(-30 * time.Minute)
	if err := CheckHeartbeats(context.Background(), p, now, schedules); err != nil || buf.Len() != 0 {
		t.Fatalf("every heartbeat current: %v, logged %q", err, buf.String())
	}
	p.beats["ledger.verify"] = now.Add(-27 * time.Hour)
	delete(p.beats, "audit.anchor")
	err := CheckHeartbeats(context.Background(), p, now, schedules)
	if err == nil || !strings.Contains(err.Error(), "ledger.verify last beat 27h0m0s ago") || !strings.Contains(err.Error(), "audit.anchor has never beaten") {
		t.Fatalf("missing heartbeats: %v", err)
	}
	if n := strings.Count(buf.String(), `"event":"fyniq.heartbeat_missing"`); n != 2 {
		t.Errorf("the missing heartbeats' lines: %d\n%s", n, buf.String())
	}
}
