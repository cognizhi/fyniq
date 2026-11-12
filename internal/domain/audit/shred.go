package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Crypto-shredding at the end of retention (PRD-001 §11.8, rev 0.244; p1-pdpa-retention D9). Each day the job lists,
// for each organisation, the financial years whose retention has ended (the year's last day plus
// retention.records_years, at least 7) and whose person keys no run lists yet: one run per year, for the DPO to approve,
// stepped up. For each approved run it writes the signed fyniq-shredding@1 entry to the write-once register first, then
// deletes the run's keys in the database, which refuses any other deletion (FY317). The values those keys sealed no
// longer open; the events, the chain and the anchors stay as they were. A restore re-applies the register, and the
// anonymisation register (§11.9, D10), before the copy opens.

// RecordsSetting is the records' retention setting; a person key's year is shredded after it.
const RecordsSetting = "retention.records_years"

// The registers' formats and where their entries are written, once.
const (
	ShreddingSchema     = "fyniq-shredding@1"
	AnonymisationSchema = "fyniq-anonymisation@1"
)

// ShreddingPrefix lists an organisation's shredding register; ShreddingKey is a run's entry.
func ShreddingPrefix(org uuid.UUID) string     { return fmt.Sprintf("pdpa-shredding/%s/", org) }
func ShreddingKey(org, run uuid.UUID) string   { return ShreddingPrefix(org) + run.String() + ".json" }
func AnonymisationPrefix(org uuid.UUID) string { return fmt.Sprintf("pdpa-anonymisation/%s/", org) }
func AnonymisationKey(org, entry uuid.UUID) string {
	return AnonymisationPrefix(org) + entry.String() + ".json"
}

// Shredding is a run's register entry: the keys it destroys (never whose they are), the year, and who approved it.
type Shredding struct {
	Schema         string      `json:"schema"`
	OrganisationID uuid.UUID   `json:"organisation_id"`
	RunID          uuid.UUID   `json:"run_id"`
	Cohort         string      `json:"cohort"` // the year's last day
	Keys           []uuid.UUID `json:"keys"`
	ListedAt       string      `json:"listed_at"`
	ApprovedBy     string      `json:"approved_by"`
	ApprovedAt     string      `json:"approved_at"`
	WrittenAt      string      `json:"written_at"`
	KeyID          string      `json:"key_id"`
}

// Anonymisation is an anonymisation's register entry: the contact whose details were replaced, and who did it.
type Anonymisation struct {
	Schema         string     `json:"schema"`
	OrganisationID uuid.UUID  `json:"organisation_id"`
	EntryID        uuid.UUID  `json:"entry_id"`
	ContactID      uuid.UUID  `json:"contact_id"`
	RequestID      *uuid.UUID `json:"request_id"`
	AnonymisedBy   string     `json:"anonymised_by"`
	AnonymisedAt   string     `json:"anonymised_at"`
	WrittenAt      string     `json:"written_at"`
	KeyID          string     `json:"key_id"`
}

// signed is a register entry with its signature, as written to the bucket.
type signed[T any] struct {
	Entry     T      `json:"entry"`
	Signature []byte `json:"signature"`
}

func writeEntry[T any](ctx context.Context, blobs ports.BlobStore, signer ports.Signer, key string, e T) error {
	msg, err := json.Marshal(e)
	if err != nil {
		return err
	}
	sig, err := signer.Sign(ctx, msg)
	if err != nil {
		return err
	}
	b, err := json.Marshal(signed[T]{Entry: e, Signature: sig})
	if err != nil {
		return err
	}
	if err := blobs.Create(ctx, key, bytes.NewReader(b)); err != nil && !errors.Is(err, ports.ErrExists) {
		return err
	}
	return nil
}

func readEntry[T any](ctx context.Context, blobs ports.BlobStore, signer ports.Signer, key string, keyID func(T) string) (T, error) {
	var s signed[T]
	if err := readJSON(ctx, blobs, key, &s); err != nil {
		return s.Entry, err
	}
	msg, err := json.Marshal(s.Entry)
	if err != nil {
		return s.Entry, err
	}
	if err := signer.Verify(keyID(s.Entry), msg, s.Signature); err != nil {
		return s.Entry, fmt.Errorf("register entry %s is not signed: %w", key, err)
	}
	return s.Entry, nil
}

// RecordsYearsOn is an organisation's records' retention in years on a day: at least 7.
func RecordsYearsOn(ctx context.Context, u storage.UnitOfWork, day time.Time) (int, error) {
	def, _ := settings.Lookup(RecordsSetting)
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(settings.On(def, changes, day).Value)
	return max(n, 7), err
}

// TellShredding tells the DPO that a run waits for their approval.
type TellShredding func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, r storage.ShreddingRun) error

// ShredAll lists the years due and shreds the approved runs, in every organisation (the daily pdpa.shred). It returns
// how many keys it shredded.
func ShredAll(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, tell TellShredding, clock ports.Clock) (int, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, fmt.Errorf("list the organisations: %w", err)
	}
	total := 0
	for _, org := range orgs {
		n, err := Shred(ctx, store, blobs, signer, tell, clock.Now(), org)
		if err != nil {
			return total, fmt.Errorf("shred the person keys of organisation %s: %w", org, err)
		}
		total += n
	}
	return total, nil
}

// Shred lists org's years due as runs, and shreds its approved runs, register first.
func Shred(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, tell TellShredding, now time.Time, org uuid.UUID) (int, error) {
	var approved []storage.ShreddingRun
	if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		years, err := RecordsYearsOn(ctx, u, now)
		if err != nil {
			return err
		}
		today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
		due, err := u.Compliance().DueKeyCohorts(ctx, today.AddDate(-years, 0, 0))
		if err != nil {
			return err
		}
		for _, c := range due {
			r, err := u.Compliance().ListShreddingRun(ctx, c.Cohort, c.Keys, now)
			if err != nil {
				return err
			}
			if tell != nil {
				if err := tell(ctx, u, org, r); err != nil {
					return err
				}
			}
		}
		runs, err := u.Compliance().ShreddingRuns(ctx)
		for _, r := range runs {
			if r.Status == "approved" {
				approved = append(approved, r)
			}
		}
		return err
	}); err != nil {
		return 0, err
	}
	total := 0
	for _, r := range approved {
		// The register first: a restore re-applies every entry, so an entry for a shredding that then failed only finishes it.
		e := Shredding{Schema: ShreddingSchema, OrganisationID: org, RunID: r.ID, Cohort: r.Cohort.Format(time.DateOnly), Keys: r.Keys,
			ListedAt: r.ListedAt.UTC().Format(time.RFC3339Nano), ApprovedBy: r.ApprovedBy, WrittenAt: now.UTC().Format(time.RFC3339Nano), KeyID: signer.KeyID()}
		if r.ApprovedAt != nil {
			e.ApprovedAt = r.ApprovedAt.UTC().Format(time.RFC3339Nano)
		}
		key, err := WriteShredding(ctx, blobs, signer, e)
		if err != nil {
			return total, err
		}
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			n, err := u.Compliance().ShredRun(ctx, r.ID, key)
			total += n
			return err
		}); err != nil {
			return total, err
		}
	}
	return total, nil
}

// WriteShredding writes a run's register entry, signed, and returns its key.
func WriteShredding(ctx context.Context, blobs ports.BlobStore, signer ports.Signer, e Shredding) (string, error) {
	e.Schema, e.KeyID = ShreddingSchema, signer.KeyID()
	key := ShreddingKey(e.OrganisationID, e.RunID)
	return key, writeEntry(ctx, blobs, signer, key, e)
}

// RecordAnonymisation writes an anonymisation's register entry; the caller then records the anonymisation with it.
func RecordAnonymisation(ctx context.Context, blobs ports.BlobStore, signer ports.Signer, e Anonymisation) (string, error) {
	e.Schema, e.KeyID = AnonymisationSchema, signer.KeyID()
	key := AnonymisationKey(e.OrganisationID, e.EntryID)
	return key, writeEntry(ctx, blobs, signer, key, e)
}

// ReapplyPDPA re-applies org's shredding and anonymisation registers after a restore (D9, D10), checking each entry's
// signature first: keys a backup brought back go again, and contacts anonymised since are anonymised again. It returns
// how many keys it shredded and contacts it anonymised.
func ReapplyPDPA(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, org uuid.UUID) (int, error) {
	total := 0
	keys, err := blobs.List(ctx, ShreddingPrefix(org))
	if err != nil {
		return 0, err
	}
	for _, k := range keys {
		e, err := readEntry(ctx, blobs, signer, k, func(e Shredding) string { return e.KeyID })
		if err != nil || e.OrganisationID != org || e.Schema != ShreddingSchema {
			return total, fmt.Errorf("shredding register %s is not signed for this organisation: %v", k, err)
		}
		cohort, err1 := time.Parse(time.DateOnly, e.Cohort)
		listed, err2 := time.Parse(time.RFC3339Nano, e.ListedAt)
		approved, err3 := time.Parse(time.RFC3339Nano, e.ApprovedAt)
		if err := errors.Join(err1, err2, err3); err != nil {
			return total, fmt.Errorf("shredding register %s: %w", k, err)
		}
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			n, err := u.Compliance().ReapplyShredding(ctx, storage.ShreddingRun{ID: e.RunID, Cohort: cohort, Keys: e.Keys, ListedAt: listed,
				ApprovedBy: e.ApprovedBy, ApprovedAt: &approved, Register: k})
			total += n
			return err
		}); err != nil {
			return total, err
		}
	}
	keys, err = blobs.List(ctx, AnonymisationPrefix(org))
	if err != nil {
		return total, err
	}
	for _, k := range keys {
		e, err := readEntry(ctx, blobs, signer, k, func(e Anonymisation) string { return e.KeyID })
		if err != nil || e.OrganisationID != org || e.Schema != AnonymisationSchema {
			return total, fmt.Errorf("anonymisation register %s is not signed for this organisation: %v", k, err)
		}
		at, err := time.Parse(time.RFC3339Nano, e.AnonymisedAt)
		if err != nil {
			return total, fmt.Errorf("anonymisation register %s: %w", k, err)
		}
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			done, err := u.Compliance().Anonymisations(ctx)
			if err != nil {
				return err
			}
			for _, a := range done {
				if a.Register == k {
					return nil // the copy holds it already
				}
			}
			total++
			_, err = u.Compliance().Anonymise(ctx, storage.Anonymisation{ContactID: e.ContactID, RequestID: e.RequestID,
				Reason: "Re-applied from the anonymisation register after a restore", Register: k, AnonymisedBy: e.AnonymisedBy, AnonymisedAt: at,
				Reapplied: true})
			return err
		}); err != nil {
			return total, err
		}
	}
	return total, nil
}
