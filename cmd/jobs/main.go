// Command jobs runs long work as Cloud Run jobs started by a task (ADR-T7). Scheduling (hourly,
// nightly) arrives with p0-outbox-background-work.
//
//	jobs audit-anchor   sign and write every organisation's audit chain head (hourly, CTL-AUD-02)
//	jobs audit-verify   recompute every chain and check it against its anchors (nightly, AUD-03);
//	                    exits non-zero when any trail fails
//
// FYNIQ_DATABASE_URL is the worker's role. The bucket and the signing key come from the build's
// adapters: local files in the local build; Cloud Storage and Cloud KMS arrive with p0-infra-opentofu.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports"
)

// auditAdapters returns the bucket and the signer; the build sets it (local.go, release.go).
var auditAdapters func() (ports.BlobStore, ports.Signer, error)

func main() {
	os.Exit(app.Run("jobs", func(ctx context.Context, deps app.Deps, args []string) error {
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "jobs: audit-anchor, audit-verify")
			return nil
		}
		if len(args) != 1 || (args[0] != "audit-anchor" && args[0] != "audit-verify") {
			return fmt.Errorf("usage: jobs audit-anchor | jobs audit-verify")
		}
		url := os.Getenv("FYNIQ_DATABASE_URL")
		if url == "" {
			return fmt.Errorf("FYNIQ_DATABASE_URL is not set: give the URL of the fyniq_worker role")
		}
		store, err := postgres.Open(ctx, url, postgres.WithClock(deps.Clock))
		if err != nil {
			return err
		}
		defer store.Close()
		blobs, signer, err := auditAdapters()
		if err != nil {
			return err
		}
		switch args[0] {
		case "audit-anchor":
			n, err := audit.AnchorAll(ctx, store, blobs, signer, deps.Clock)
			fmt.Fprintf(os.Stderr, "jobs: wrote %d audit anchor(s)\n", n)
			return err
		default:
			reports, err := audit.VerifyAll(ctx, store, blobs, signer)
			for _, r := range reports {
				status := "ok"
				if !r.OK() {
					status = "FAILED: " + r.Problem
				}
				fmt.Fprintf(os.Stderr, "jobs: audit trail of %s: %d events, %d anchors, %s\n", r.Organisation, r.Events, r.Anchors, status)
			}
			return err
		}
	}))
}
