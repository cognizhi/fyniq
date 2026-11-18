package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Completion is when a record's related transactions complete (FR-DOC-02): the last day of the last month a line of
// an invoice or a bill spreads its revenue or cost into, such as a contract billed for 2027 to 2029; else the record's
// own date. Zero when the record names neither.
func Completion(d storage.Document) time.Time {
	var p struct {
		Date  string `json:"date"`
		Lines []struct {
			SpreadMonths int    `json:"spread_months"`
			SpreadFrom   string `json:"spread_from"`
		} `json:"lines"`
	}
	if json.Unmarshal(d.Payload, &p) != nil {
		return time.Time{}
	}
	on, _ := time.Parse(time.DateOnly, p.Date)
	out := on
	for _, l := range p.Lines {
		if l.SpreadMonths < 1 {
			continue
		}
		from := time.Date(on.Year(), on.Month(), 1, 0, 0, 0, 0, time.UTC)
		if l.SpreadFrom != "" { // a month, "2027-01", or a day in it, "2027-01-15"
			f, err := time.Parse("2006-01", l.SpreadFrom)
			if err != nil {
				if f, err = time.Parse(time.DateOnly, l.SpreadFrom); err != nil {
					continue
				}
			}
			from = time.Date(f.Year(), f.Month(), 1, 0, 0, 0, 0, time.UTC)
		}
		if from.IsZero() {
			continue
		}
		if last := from.AddDate(0, l.SpreadMonths, -1); last.After(out) {
			out = last
		}
	}
	return out
}

// retainUntilOf is a file's retain-until committed at now: from the later of now and when its record's transactions
// complete, so it is never short of the record's obligations (FR-DOC-02, G4-068).
func retainUntilOf(fyStart, years int, now time.Time, d storage.Document, zone *time.Location) time.Time {
	at := now
	if c := Completion(d); c.After(at) {
		at = c
	}
	return RetainUntil(fyStart, years, at, zone)
}

// ExtendKind is the nightly work that extends the retention of committed files whose record's transactions now complete
// later than when they were kept for (FR-DOC-02): a contract's spread lengthened, for instance.
const ExtendKind = "evidence.extend"

// Extend moves later the retain-until of each of the organisation's final files whose record now completes after what
// its retain-until was worked out from, the locked copy first and then its row, audited; a retain-until never moves
// earlier. It returns how many it extended.
func (c Committer) Extend(ctx context.Context, org uuid.UUID) (int, error) {
	zone := c.Zone
	if zone == nil {
		zone = singapore
	}
	type due struct {
		e     storage.Evidence
		until time.Time
	}
	var todo []due
	if err := c.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		for after := uuid.Nil; ; {
			page, err := u.Evidence().Final(ctx, after, 500)
			if err != nil {
				return err
			}
			for _, e := range page {
				after = e.ID
				if e.RetainUntil == nil {
					continue
				}
				d, err := u.Documents().Get(ctx, e.DocumentID)
				if err != nil {
					return err
				}
				if until := retainUntilOf(o.Profile.FYStartMonth, RecordsYears(), *e.FinalisedAt, d, zone); until.After(*e.RetainUntil) {
					todo = append(todo, due{e, until})
				}
			}
			if len(page) < 500 {
				return nil
			}
		}
	}); err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, t := range todo {
		if err := c.Locked.SetRetainUntil(ctx, t.e.LockedKey, t.until); err != nil && !errors.Is(err, ports.ErrRetentionShortened) {
			errs = append(errs, fmt.Errorf("evidence %s: %w", t.e.ID, err))
			continue
		}
		if err := c.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Evidence().Extend(ctx, t.e.ID, t.until)
			return err
		}); err != nil {
			errs = append(errs, fmt.Errorf("evidence %s: %w", t.e.ID, err))
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

// ExtendAll extends every organisation's files that need it.
func (c Committer) ExtendAll(ctx context.Context) (int, error) {
	orgs, err := c.Store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	var errs []error
	for _, org := range orgs {
		n, err := c.Extend(ctx, org)
		total += n
		if err != nil {
			errs = append(errs, err)
		}
	}
	return total, errors.Join(errs...)
}
