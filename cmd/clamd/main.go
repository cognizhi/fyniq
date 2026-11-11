// Command clamd is the entry point of the clamd sidecar in the scanner service (FR-DOC-08, SEC-24; PRD-001 rev 0.247).
// The image is distroless, with no shell, so this writes clamd's and freshclam's configuration, fetches the signatures
// with freshclam, starts clamd on 127.0.0.1:3310 for the scanner's HTTP front beside it, and runs freshclam again every
// two hours; clamd reloads the signatures as they change (SelfCheck). A failed update keeps the signatures clamd has;
// clamd stopping stops the container, so Cloud Run starts another.
package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// Paths are the binaries and the working directory, an in-memory volume in Cloud Run.
type Paths struct {
	Freshclam, Clamd, Dir string
}

// Every is how often freshclam runs after the first time: within ClamAV's limit of once an hour per address.
const Every = 2 * time.Hour

func main() {
	p := Paths{Freshclam: cmp.Or(os.Getenv("FYNIQ_FRESHCLAM"), "/usr/bin/freshclam"), Clamd: cmp.Or(os.Getenv("FYNIQ_CLAMD"), "/usr/sbin/clamd"),
		Dir: cmp.Or(os.Getenv("FYNIQ_CLAMAV_DIR"), "/tmp/clamav")}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if err := Run(ctx, p, Every, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "clamd:", err)
		os.Exit(1)
	}
}

// clamdConf is clamd's configuration: in the foreground, which logs to stderr (a LogFile there is refused: clamd opens it
// without following links), on loopback only, the scan limits above the
// service's 32 MB body, and a check for new signatures every 10 minutes.
func clamdConf(dir string) string {
	return fmt.Sprintf(`Foreground yes
LogTime yes
DatabaseDirectory %[1]s/db
TemporaryDirectory %[1]s/tmp
TCPSocket 3310
TCPAddr 127.0.0.1
StreamMaxLength 33M
MaxFileSize 33M
MaxScanSize 200M
SelfCheck 600
`, dir)
}

// freshclamConf is freshclam's: the official mirror, the same directory, in the foreground.
func freshclamConf(dir string) string {
	return fmt.Sprintf(`DatabaseDirectory %s/db
DatabaseMirror database.clamav.net
Foreground yes
LogTime yes
ConnectTimeout 30
ReceiveTimeout 300
`, dir)
}

// Run writes the configuration, updates the signatures, and runs clamd until ctx ends or clamd stops.
func Run(ctx context.Context, p Paths, every time.Duration, log io.Writer) error {
	for _, d := range []string{filepath.Join(p.Dir, "db"), filepath.Join(p.Dir, "tmp")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	clamdFile, freshFile := filepath.Join(p.Dir, "clamd.conf"), filepath.Join(p.Dir, "freshclam.conf")
	if err := os.WriteFile(clamdFile, []byte(clamdConf(p.Dir)), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(freshFile, []byte(freshclamConf(p.Dir)), 0o600); err != nil {
		return err
	}
	update := func() error {
		c := exec.CommandContext(ctx, p.Freshclam, "--config-file="+freshFile, "--stdout") // #nosec G204 -- the image's own binary
		c.Stdout, c.Stderr = log, log
		return c.Run()
	}
	// clamd starts only with signatures: the first update must succeed.
	if err := update(); err != nil {
		return fmt.Errorf("the first signature update: %w", err)
	}
	clamd := exec.CommandContext(ctx, p.Clamd, "--foreground", "--config-file="+clamdFile) // #nosec G204 -- the image's own binary
	clamd.Stdout, clamd.Stderr = log, log
	clamd.Cancel = func() error { return clamd.Process.Signal(syscall.SIGTERM) }
	clamd.WaitDelay = 20 * time.Second
	if err := clamd.Start(); err != nil {
		return fmt.Errorf("clamd did not start: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- clamd.Wait() }()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			if ctx.Err() != nil {
				return nil // stopped as asked
			}
			return fmt.Errorf("clamd stopped: %w", err)
		case <-tick.C:
			if err := update(); err != nil && ctx.Err() == nil {
				fmt.Fprintln(log, "clamd: a signature update failed; clamd keeps the signatures it has:", err)
			}
		}
	}
}
