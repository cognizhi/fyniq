package retention

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// HoldsJob is the platform job that keeps the evidence store's legal holds equal to the holds in force (AUD-11; 001300).
const HoldsJob = "retention.holds"

// Synced is what a reconcile did to an organisation's stored files.
type Synced struct {
	Held, Freed, Missing int
}

// SyncHolds makes each final evidence file's storage-level legal hold equal to whether a hold in force covers it
// (p2-security-certification design D2): a covered file is held, including one attached after the hold was placed; a file no hold in
// force covers is freed, so a lifted hold never leaves a file held for good. An organisation that never had a hold is left alone. A file
// the store no longer has is counted, not an error: the store's own retention checks answer for it (CTL-AUD-03).
func SyncHolds(ctx context.Context, store storage.Store, blobs ports.BlobStore, org uuid.UUID) (Synced, error) {
	var held, free []storage.HeldFile
	if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		holds, err := u.Retention().Holds(ctx, false)
		if err != nil || len(holds) == 0 {
			return err
		}
		if held, err = u.Retention().HeldFiles(ctx); err != nil {
			return err
		}
		free, err = u.Retention().FreeFiles(ctx)
		return err
	}); err != nil {
		return Synced{}, err
	}
	var out Synced
	set := func(files []storage.HeldFile, hold bool, n *int) error {
		for _, f := range files {
			r, err := blobs.RetentionOf(ctx, f.LockedKey)
			if errors.Is(err, ports.ErrNotFound) {
				out.Missing++
				continue
			}
			if err != nil {
				return err
			}
			if r.Held == hold {
				continue
			}
			if err := blobs.SetLegalHold(ctx, f.LockedKey, hold); err != nil && !errors.Is(err, ports.ErrNotFound) {
				return err
			}
			*n++
		}
		return nil
	}
	if err := set(held, true, &out.Held); err != nil {
		return out, err
	}
	return out, set(free, false, &out.Freed)
}

// SyncAllHolds runs SyncHolds in every organisation (nightly).
func SyncAllHolds(ctx context.Context, store storage.Store, blobs ports.BlobStore) error {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return fmt.Errorf("list the organisations: %w", err)
	}
	var errs []error
	for _, org := range orgs {
		if _, err := SyncHolds(ctx, store, blobs, org); err != nil {
			errs = append(errs, fmt.Errorf("sync the holds of organisation %s: %w", org, err))
		}
	}
	return errors.Join(errs...)
}
