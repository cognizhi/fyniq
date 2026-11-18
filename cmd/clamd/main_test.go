package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a log the sidecar and the test both touch.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// fake writes an executable script standing in for freshclam or clamd.
func fake(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil { // #nosec G306 -- a test's own script
		t.Fatal(err)
	}
	return path
}

// The sidecar fetches the signatures before clamd starts, starts clamd on loopback with its configuration, updates the
// signatures again on its tick, and stops clamd when asked (FR-DOC-08, SEC-24).
// req: SEC-24
// req: FR-DOC-08
// asvs: V5.2.1
func TestTheSidecarUpdatesThenRunsClamd(t *testing.T) {
	dir := t.TempDir()
	args := filepath.Join(dir, "clamd-args")
	p := Paths{
		Freshclam: fake(t, "freshclam", `echo x >> "`+dir+`/db/updates"`),
		Clamd:     fake(t, "clamd", `test -s "`+dir+`/db/updates" || exit 9; echo "$@" > "`+args+`"; exec sleep 60`),
		Dir:       dir,
	}
	ctx, cancel := context.WithCancel(context.Background())
	var log syncBuffer
	done := make(chan error, 1)
	go func() { done <- Run(ctx, p, 200*time.Millisecond, &log) }()
	for tries := 0; ; tries++ { // up to 60 seconds, for a loaded machine
		b, _ := os.ReadFile(filepath.Join(dir, "db", "updates"))
		if _, err := os.Stat(args); err == nil && strings.Count(string(b), "x") >= 2 {
			break // clamd is running, and freshclam ran again on its tick
		}
		select {
		case err := <-done:
			t.Fatalf("Run returned before it was stopped: %v; freshclam ran %d times", err, strings.Count(string(b), "x"))
		default:
		}
		if tries == 1200 {
			t.Fatalf("freshclam ran %d times, clamd's arguments %v", strings.Count(string(b), "x"), fileExists(args))
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("stopping: %v", err)
	}
	got, _ := os.ReadFile(args)
	if !strings.Contains(string(got), "--foreground --config-file="+filepath.Join(dir, "clamd.conf")) {
		t.Errorf("clamd's arguments: %q; log: %s", got, log.String())
	}
	conf, _ := os.ReadFile(filepath.Join(dir, "clamd.conf"))
	for _, want := range []string{"TCPAddr 127.0.0.1", "TCPSocket 3310", "DatabaseDirectory " + dir + "/db", "SelfCheck 600", "StreamMaxLength 33M"} {
		if !strings.Contains(string(conf), want) {
			t.Errorf("clamd.conf lacks %q:\n%s", want, conf)
		}
	}
}

// clamd never starts without signatures, and its stopping is the sidecar's failure, so Cloud Run starts another.
// req: SEC-24
// asvs: V5.2.1
func TestTheSidecarFailsWithoutSignaturesOrClamd(t *testing.T) {
	dir := t.TempDir()
	never := fake(t, "clamd", `echo started > "`+dir+`/started"`)
	err := Run(context.Background(), Paths{Freshclam: fake(t, "freshclam", "exit 1"), Clamd: never, Dir: dir}, time.Hour, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "the first signature update") {
		t.Errorf("no signatures: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
		t.Error("clamd started without signatures")
	}
	err = Run(context.Background(), Paths{Freshclam: fake(t, "freshclam", "true"), Clamd: fake(t, "clamd", "exit 3"), Dir: t.TempDir()}, time.Hour, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "clamd stopped") {
		t.Errorf("clamd stopping: %v", err)
	}
}
