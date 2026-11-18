package compliance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The verifications that read an organisation's files as well as its records (p1-operations-dr design D3) read the
// locked store outside a unit of work, in every organisation, and do not run where the deployment gave the runner no
// store.

// fileVerification reads an organisation's records since the window began, and the files the locked store keeps for it.
type fileVerification func(ctx context.Context, r Runner, org uuid.UUID, since time.Time) (found []Finding, read string, err error)

var fileVerifications = map[string]fileVerification{
	"records.retention": recordsRetention,
}

// recordsRetention (CTL-AUD-03, NFR-RET-01, DR-07): every file committed since the window began is in the locked store,
// in Locked mode, kept at least until the retain-until its record holds (§11.9); and every file the store keeps for the
// organisation has its record. The nightly evidence.verify re-hashes every committed file (CTL-AUD-06).
func recordsRetention(ctx context.Context, r Runner, org uuid.UUID, since time.Time) ([]Finding, string, error) {
	var recent []storage.Evidence
	for after := uuid.Nil; ; {
		var page []storage.Evidence
		if err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			page, err = u.Evidence().Final(ctx, after, 500)
			return err
		}); err != nil {
			return nil, "", err
		}
		for _, e := range page {
			if e.FinalisedAt != nil && !e.FinalisedAt.Before(since) {
				recent = append(recent, e)
			}
		}
		if len(page) < 500 {
			break
		}
		after = page[len(page)-1].ID
	}
	var out []Finding
	for _, e := range recent {
		kept, err := r.Evidence.RetentionOf(ctx, e.LockedKey)
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return nil, "", err
		}
		if f, ok := lockFinding(e, kept, err); ok {
			out = append(out, f)
		}
	}
	keys, err := r.Evidence.List(ctx, "evidence/"+org.String()+"/") // evidence.LockedKey's prefix
	if err != nil {
		return nil, "", err
	}
	var known map[string]bool
	if err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		known, err = u.Evidence().Known(ctx, keys)
		return err
	}); err != nil {
		return nil, "", err
	}
	for _, k := range keys {
		if !known[k] {
			out = append(out, Finding{Kind: "file", ID: k, Detail: "Kept in the records store with no record"})
		}
	}
	return out, fmt.Sprintf("%d file(s) committed and %d kept", len(recent), len(keys)), nil
}

// lockFinding is what is wrong with how the store keeps a committed file, if anything: missing (err is ErrNotFound), not
// locked, or kept for less time than its record says.
func lockFinding(e storage.Evidence, kept ports.BlobRetention, err error) (Finding, bool) {
	f := Finding{Kind: "evidence", ID: e.ID.String()}
	if e.FinalisedAt != nil {
		f.At = day(*e.FinalisedAt)
	}
	switch {
	case err != nil:
		f.Detail = "Committed, but missing from the records store"
	case e.RetainUntil == nil:
		f.Detail = "Committed with no retain-until in its record"
	case !kept.Locked:
		f.Detail = "Kept without a locked retention, so it could be deleted before its time"
	case kept.RetainUntil.Before(*e.RetainUntil):
		f.Detail = fmt.Sprintf("Kept until %s, before its record's %s", dayOf(kept.RetainUntil), dayOf(*e.RetainUntil))
	default:
		return f, false
	}
	return f, true
}
