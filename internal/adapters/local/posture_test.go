//go:build local

package local

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// The local log reader reads the lines of the *.log files changed since a time, at most limit, and skips older files
// and other files (CTL-DP-03).
// req: CTL-DP-03
func TestLogDirReadsTheRecentLogFiles(t *testing.T) {
	dir := t.TempDir()
	since := time.Date(2099, 1, 2, 0, 0, 0, 0, time.UTC)
	write := func(name, body string, at time.Time) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write("api.log", "one\ntwo\n", since.Add(time.Hour))
	write("worker.log", "three\n", since.Add(2*time.Hour))
	write("old.log", "stale\n", since.Add(-time.Hour))
	write("notes.txt", "not a log\n", since.Add(time.Hour))
	got, err := LogDir{Dir: dir}.Read(context.Background(), since, 10)
	if err != nil || !slices.Equal(got, []string{"three", "one", "two"}) {
		t.Errorf("read %v, %v", got, err)
	}
	if got, err := (LogDir{Dir: dir}).Read(context.Background(), since, 2); err != nil || len(got) != 2 {
		t.Errorf("at most 2: %v, %v", got, err)
	}
	if got, err := (LogDir{Dir: filepath.Join(dir, "none")}).Read(context.Background(), since, 10); err != nil || len(got) != 0 {
		t.Errorf("no directory: %v, %v", got, err)
	}
}
