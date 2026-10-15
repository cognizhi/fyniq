package wiring

import (
	"context"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports"
)

// PlatformJobs are the functions behind the platform's schedules (outbox.PlatformSchedules), run by
// the worker locally and by the jobs image as Cloud Run jobs in production.
func (k *Kit) PlatformJobs(clock ports.Clock) map[string]func(ctx context.Context) error {
	return map[string]func(ctx context.Context) error{
		"audit.anchor": func(ctx context.Context) error {
			_, err := audit.AnchorAll(ctx, k.Store, k.Blobs, k.Signer, clock)
			return err
		},
		"audit.verify": func(ctx context.Context) error {
			_, err := audit.VerifyAll(ctx, k.Store, k.Blobs, k.Signer)
			return err
		},
	}
}
