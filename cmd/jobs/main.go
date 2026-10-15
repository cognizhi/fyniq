// Command jobs runs the platform's long work as run-to-completion jobs (ADR-T7): Cloud Run jobs in
// production, started by the tick; a one-off command locally.
//
//	jobs audit-anchor            sign and write every organisation's audit chain head (CTL-AUD-02)
//	jobs audit-verify            recompute every chain against its anchors (AUD-03); fails on any mismatch
//	jobs run-slot <job> <slot>   run a platform job's slot (RFC 3339) and report it to its slot and heartbeat
//	jobs heartbeat-check         fail when the tick or a platform job has stopped beating (the absence alert)
//	jobs outbox-after-restore    after a database restore: every unfinished outbox row waits for reconciliation
//
// FYNIQ_DATABASE_URL is the worker's role; the local build defaults it.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/wiring"
)

const usage = "usage: jobs audit-anchor | audit-verify | run-slot <job> <slot> | heartbeat-check | outbox-after-restore"

func main() {
	os.Exit(app.Run("jobs", func(ctx context.Context, deps app.Deps, args []string) error {
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "jobs: audit-anchor, audit-verify, run-slot, heartbeat-check, outbox-after-restore")
			return nil
		}
		kit, err := wiring.Build(ctx, "jobs", deps.Clock)
		if err != nil {
			return err
		}
		defer kit.Store.Close()
		switch args[0] {
		case "audit-anchor":
			n, err := audit.AnchorAll(ctx, kit.Store, kit.Blobs, kit.Signer, deps.Clock)
			fmt.Fprintf(os.Stderr, "jobs: wrote %d audit anchor(s)\n", n)
			return err
		case "audit-verify":
			reports, err := audit.VerifyAll(ctx, kit.Store, kit.Blobs, kit.Signer)
			for _, r := range reports {
				status := "ok"
				if !r.OK() {
					status = "FAILED: " + r.Problem
				}
				fmt.Fprintf(os.Stderr, "jobs: audit trail of %s: %d events, %d anchors, %s\n", r.Organisation, r.Events, r.Anchors, status)
			}
			return err
		case "run-slot":
			if len(args) != 3 {
				return fmt.Errorf(usage)
			}
			slot, err := time.Parse(time.RFC3339, args[2])
			if err != nil {
				return fmt.Errorf("the slot %q is not an RFC 3339 time", args[2])
			}
			run, ok := kit.PlatformJobs(deps.Clock)[args[1]]
			if !ok {
				return fmt.Errorf("no platform job %q", args[1])
			}
			return outbox.RunSlot(ctx, kit.Store, deps.Clock, args[1], slot, run)
		case "heartbeat-check":
			alerts, err := outbox.Absences(ctx, kit.Store, deps.Clock.Now(), outbox.PlatformSchedules)
			if err != nil {
				return err
			}
			if len(alerts) > 0 {
				return fmt.Errorf("heartbeats missing: %s", strings.Join(alerts, "; "))
			}
			fmt.Fprintln(os.Stderr, "jobs: every heartbeat is current")
			return nil
		case "outbox-after-restore":
			orgs, err := kit.Store.Organisations(ctx)
			if err != nil {
				return err
			}
			var total int64
			for _, org := range orgs {
				err := kit.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
					n, err := u.Sweeping().AfterRestore(ctx)
					if err != nil || n == 0 {
						return err
					}
					total += n
					return u.Audit().Record(ctx, storage.AuditRecord{Action: "outbox.after_restore", ObjectType: "organisation", ObjectID: org,
						Changes: map[string][2]any{"rows_set_to_unknown_outcome": {nil, n}}})
				})
				if err != nil {
					return fmt.Errorf("organisation %s: %w", org, err)
				}
			}
			fmt.Fprintf(os.Stderr, "jobs: %d unfinished outbox row(s) now wait for reconciliation\n", total)
			return nil
		}
		return fmt.Errorf(usage)
	}))
}
