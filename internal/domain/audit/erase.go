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

// The security-log retention (AUD-06, the owner's decision of PRD-001 rev 0.230): a security event's IP address and
// user agent, kept beside the chain, are erased once they are older than retention.security_events_days. The event
// stays, so the chain still verifies. Each erasure is written first to a signed register outside the database, so a
// restore from an older backup can erase again what this erased (AUD-09, G4-070).

// RetentionSetting is the security-log retention's setting.
const RetentionSetting = "retention.security_events_days"

// ActionDetailsErased is the erasure's own event.
const ActionDetailsErased = "audit.details_erased"

// ErasureSchema names the register's entry format.
const ErasureSchema = "fyniq-audit-erasure@1"

// Erasure is one entry of the erasure register: in an organisation, every security event's details from before the
// cutoff are erased.
type Erasure struct {
	Schema         string    `json:"schema"`
	OrganisationID uuid.UUID `json:"organisation_id"`
	Cutoff         string    `json:"cutoff"` // RFC 3339, UTC
	Count          int64     `json:"count"`
	WrittenAt      string    `json:"written_at"`
	KeyID          string    `json:"key_id"`
}

// SignedErasure is an entry with its signature, as written to the bucket.
type SignedErasure struct {
	Erasure   Erasure `json:"erasure"`
	Signature []byte  `json:"signature"`
}

// ErasurePrefix lists an organisation's register; ErasureKey is where one entry is written, once.
func ErasurePrefix(org uuid.UUID) string { return fmt.Sprintf("audit-erasures/%s/", org) }

func ErasureKey(org uuid.UUID, cutoff time.Time) string {
	return ErasurePrefix(org) + cutoff.UTC().Format("20060102T150405.000000000Z") + ".json"
}

// Check verifies an entry's signature.
func (se SignedErasure) Check(s ports.Signer) error {
	msg, err := json.Marshal(se.Erasure)
	if err != nil {
		return err
	}
	return s.Verify(se.Erasure.KeyID, msg, se.Signature)
}

// RetentionOn is an organisation's security-log retention in days on a day.
func RetentionOn(ctx context.Context, u storage.UnitOfWork, day time.Time) (int, error) {
	def, _ := settings.Lookup(RetentionSetting)
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(settings.On(def, changes, day).Value)
}

// EraseAll erases, in every organisation, the security events' details older than its retention (daily). For an
// organisation with any, it writes the register's signed entry first, then erases, then records the erasure in the
// trail. It returns how many events' details it erased.
func EraseAll(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, clock ports.Clock) (int64, error) {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return 0, fmt.Errorf("list the organisations: %w", err)
	}
	var erased int64
	for _, org := range orgs {
		n, err := erase(ctx, store, blobs, signer, clock.Now(), org)
		if err != nil {
			return erased, fmt.Errorf("erase the security details of organisation %s: %w", org, err)
		}
		erased += n
	}
	return erased, nil
}

func erase(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, now time.Time, org uuid.UUID) (int64, error) {
	var cutoff time.Time
	var due int64
	if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		days, err := RetentionOn(ctx, u, now)
		if err != nil {
			return err
		}
		cutoff = now.AddDate(0, 0, -days)
		due, err = u.Audit().DetailsBefore(ctx, cutoff)
		return err
	}); err != nil || due == 0 {
		return 0, err
	}
	// The register first: a restore re-applies every entry, so an entry for an erasure that then failed is harmless.
	e := Erasure{Schema: ErasureSchema, OrganisationID: org, Cutoff: cutoff.UTC().Format(time.RFC3339Nano), Count: due,
		WrittenAt: now.UTC().Format(time.RFC3339Nano), KeyID: signer.KeyID()}
	msg, err := json.Marshal(e)
	if err != nil {
		return 0, err
	}
	sig, err := signer.Sign(ctx, msg)
	if err != nil {
		return 0, err
	}
	b, err := json.Marshal(SignedErasure{Erasure: e, Signature: sig})
	if err != nil {
		return 0, err
	}
	if err := blobs.Create(ctx, ErasureKey(org, cutoff), bytes.NewReader(b)); err != nil && !errors.Is(err, ports.ErrExists) {
		return 0, err
	}
	var done storage.AuditErasure
	err = store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if done, err = u.Audit().EraseDetails(ctx, cutoff); err != nil || done.Erased == 0 {
			return err
		}
		return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionDetailsErased, ObjectType: "audit_chain", ObjectID: org,
			Changes: map[string][2]any{"cutoff": {nil, e.Cutoff}, "erased": {nil, done.Erased}, "first_seq": {nil, done.FirstSeq},
				"last_seq": {nil, done.LastSeq}, "register": {nil, ErasureKey(org, cutoff)}}})
	})
	return done.Erased, err
}
