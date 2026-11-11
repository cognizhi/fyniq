package retention

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The ledger's purge after retention (AUD-10; p1-audit-explorer design D7b). With the switch on, the oldest financial
// year whose records' retention has passed goes, one a week, in one transaction: its boundary is signed and written to
// the anchors' bucket, then recorded; the carry-forward entries are posted, dated the day before the boundary, so every
// report from the boundary on reads them as its opening balance; the year's entries are deleted through
// fyniq.ledger_purge, which first checks that the carry-forward replaces them exactly, so every balance from the boundary
// on is unchanged; and the balances cache is compared with the lines (INV-08) before it commits.

// ActionLedgerPurged is a ledger purge's event.
const ActionLedgerPurged = "ledger.purged"

// LedgerBoundarySchema names a ledger boundary's signed form.
const LedgerBoundarySchema = "fyniq-ledger-boundary@1"

// SignedForm is a ledger boundary as it is signed.
type SignedForm struct {
	Schema         string           `json:"schema"`
	OrganisationID uuid.UUID        `json:"organisation_id"`
	Boundary       string           `json:"boundary"`
	RuleVersion    string           `json:"rule_version"`
	Approval       string           `json:"approval"`
	Counts         map[string]int64 `json:"counts"`
	PurgedHash     string           `json:"purged_hash"`
	CarriedHash    string           `json:"carried_hash"`
	WrittenAt      string           `json:"written_at"`
	KeyID          string           `json:"key_id"`
}

// SignedBoundary is a ledger boundary with its signature, as written to the bucket.
type SignedBoundary struct {
	Boundary  SignedForm `json:"boundary"`
	Signature []byte     `json:"signature"`
}

// BoundaryKey is where a ledger boundary is written, once: by its date and the start of its purged lines' hash, so a
// year whose lines changed before it went is never confused with an earlier attempt.
func BoundaryKey(org uuid.UUID, boundary time.Time, hash []byte) string {
	return fmt.Sprintf("ledger-boundaries/%s/%s-%s.json", org, boundary.Format(time.DateOnly), hex.EncodeToString(hash[:6]))
}

// Purged is what a ledger purge did.
type Purged struct {
	Boundary time.Time
	Counts   map[string]int64
}

// PurgeLedger purges org's oldest financial year past its retention, if the purge is on and a year may go. Purged is
// zero when nothing went.
func PurgeLedger(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, now time.Time, org uuid.UUID) (Purged, error) {
	var done Purged
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		on, approval, err := audit.PurgeOn(ctx, u, now)
		if err != nil || !on {
			return err
		}
		years, err := audit.RecordsYears(ctx, u, now)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(o.Profile.TimeZone)
		if err != nil || o.Profile.TimeZone == "" {
			loc = time.UTC
		}
		bounds, err := u.Retention().LedgerBoundaries(ctx)
		if err != nil {
			return err
		}
		var previous time.Time
		if len(bounds) > 0 {
			previous = bounds[len(bounds)-1].Boundary
		}
		earliest, ok, err := u.Retention().EarliestEntryFrom(ctx, previous)
		if err != nil || !ok {
			return err
		}
		cal, err := period.CalendarOf(o.Profile)
		if err != nil {
			return nil // without financial years, nothing folds at a year's end (FR-GL-10), so no year can go
		}
		periods, err := u.Periods().List(ctx)
		if err != nil {
			return err
		}
		plan := make([]Period, len(periods))
		for i, p := range periods {
			plan[i] = Period{StartsOn: p.StartsOn, EndsOn: p.EndsOn, Open: p.Status != storage.PeriodClosed}
		}
		year, ok := NextYear(cal, years, earliest, plan, nil, now) // records cannot be held yet, so none is passed
		if !ok {
			return nil
		}
		p, err := prepare(ctx, u, o, year.Boundary)
		if err != nil {
			return err
		}
		form := SignedForm{Schema: LedgerBoundarySchema, OrganisationID: org, Boundary: year.Boundary.Format(time.DateOnly), RuleVersion: RuleVersion,
			Approval: approval, Counts: p.counts, PurgedHash: hex.EncodeToString(p.purgedHash), CarriedHash: hex.EncodeToString(p.carriedHash),
			WrittenAt: now.UTC().Format(time.RFC3339Nano), KeyID: signer.KeyID()}
		key := BoundaryKey(org, year.Boundary, p.purgedHash)
		if err := writeSigned(ctx, blobs, signer, key, form); err != nil {
			return err
		}
		b, err := u.Retention().RecordLedgerBoundary(ctx, storage.NewLedgerBoundary{Boundary: year.Boundary, Approval: approval, Counts: p.counts,
			PurgedHash: p.purgedHash, AnchorKey: key, WrittenAt: now})
		if err != nil {
			return err
		}
		if err := journal.CarryForward(ctx, u, p.carried); err != nil { // the one posting path (§11.4)
			return err
		}
		if _, err := u.Retention().PurgeLedger(ctx, year.Boundary, p.purged); err != nil {
			return err
		}
		if n, err := balance.Differences(ctx, u); err != nil || n > 0 {
			return errors.Join(err, fmt.Errorf("retention: the balances cache disagrees with the lines after the purge (INV-08): %d rows", n))
		}
		for _, q := range periods {
			if q.EndsOn.Before(year.Boundary) && !q.EndsOn.Before(previous) {
				if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "period.purged", ObjectType: "period", ObjectID: q.ID,
					Changes: map[string][2]any{"purged_on": {nil, now.In(loc).Format(time.DateOnly)}, "boundary": {nil, form.Boundary}}}); err != nil {
					return err
				}
			}
		}
		done = Purged{Boundary: year.Boundary, Counts: p.counts}
		return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionLedgerPurged, ObjectType: "ledger_boundary", ObjectID: b.ID,
			Changes: map[string][2]any{"boundary": {nil, form.Boundary}, "counts": {nil, p.counts}, "purged_hash": {nil, form.PurgedHash},
				"carried_hash": {nil, form.CarriedHash}, "anchor_key": {nil, key}, "rule_version": {nil, RuleVersion}}})
	})
	return done, err
}

// PurgeLedgers runs the ledger's purge in every organisation that switched it on (weekly), before the trail's.
func PurgeLedgers(ctx context.Context, store storage.Store, blobs ports.BlobStore, signer ports.Signer, clock ports.Clock) error {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return fmt.Errorf("list the organisations: %w", err)
	}
	for _, org := range orgs {
		if _, err := PurgeLedger(ctx, store, blobs, signer, clock.Now(), org); err != nil {
			return fmt.Errorf("purge the ledger of organisation %s: %w", org, err)
		}
	}
	return nil
}

// prepared is a year's purge worked out: the entries that go, the carry-forward to post, the counts and the hashes.
type prepared struct {
	purged      []uuid.UUID
	carried     []storage.NewEntry
	counts      map[string]int64
	purgedHash  []byte
	carriedHash []byte
}

func prepare(ctx context.Context, u storage.UnitOfWork, o storage.Organisation, boundary time.Time) (prepared, error) {
	var p prepared
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return p, err
	}
	list, err := u.Accounts().List(ctx)
	if err != nil {
		return p, err
	}
	accounts := map[string]Account{}
	var re string
	for _, a := range list { // the first retained-earnings account by code, as fyniq.ledger_purge folds into
		accounts[a.ID.String()] = Account{Code: a.Code, Type: string(a.Type), Role: string(a.Role)}
		if string(a.Role) == RetainedEarnings && re == "" {
			re = a.ID.String()
		}
	}
	lines, err := u.Retention().PurgeLines(ctx, boundary)
	if err != nil {
		return p, err
	}
	var refs []EntryRef
	named := map[string]bool{}
	seen := map[uuid.UUID]bool{}
	for _, l := range lines {
		if seen[l.EntryID] {
			named[l.EntryID.String()] = named[l.EntryID.String()] || l.Named
			continue
		}
		seen[l.EntryID] = true
		r := EntryRef{ID: l.EntryID.String(), Date: l.PostingDate}
		if l.ReversesEntryID != nil {
			r.Reverses = l.ReversesEntryID.String()
		}
		refs = append(refs, r)
		named[r.ID] = l.Named
	}
	links, err := u.Retention().ReversalLinks(ctx, boundary)
	if err != nil {
		return p, err
	}
	for _, l := range links { // kept by their date, and so are the entries before the boundary they are linked to
		r := EntryRef{ID: l.ID.String(), Date: l.PostingDate}
		if l.ReversesEntryID != nil {
			r.Reverses = l.ReversesEntryID.String()
		}
		refs = append(refs, r)
	}
	kept := Kept(refs, named, boundary)
	var rule []Line
	var canon []map[string]any
	keptLines := int64(0)
	for _, l := range lines {
		if kept[l.EntryID.String()] {
			keptLines++
			continue
		}
		cur, err := money.Lookup(l.Currency)
		if err != nil {
			return p, err
		}
		amount, err := stored(l.Amount, cur)
		if err != nil {
			return p, err
		}
		b, err := stored(l.BaseAmount, base)
		if err != nil {
			return p, err
		}
		rule = append(rule, Line{Source: Source{Type: l.Source.Type, ID: l.Source.ID}, Account: l.AccountID.String(),
			DimensionSet: setKey(l.DimensionValues), Party: idText(l.PartyID), Instrument: idText(l.InstrumentID), Amount: amount, Base: b})
		canon = append(canon, map[string]any{"entry_id": l.EntryID, "posting_date": l.PostingDate.Format(time.DateOnly),
			"source": []string{l.Source.Type, l.Source.ID}, "account_id": l.AccountID, "dimension_values": setKey(l.DimensionValues),
			"party_id": idText(l.PartyID), "instrument_id": idText(l.InstrumentID), "currency": l.Currency,
			"amount": amount.String(), "base_amount": b.String()})
	}
	for _, r := range refs {
		if !kept[r.ID] && r.Date.Before(boundary) {
			p.purged = append(p.purged, uuid.MustParse(r.ID))
		}
	}
	entries, err := CarryForward(Input{Base: base, Accounts: accounts, RetainedEarnings: re, Lines: rule})
	if err != nil {
		return p, err
	}
	var carriedCanon []any
	for _, e := range entries {
		ne, shown, err := newEntry(boundary, e)
		if err != nil {
			return p, err
		}
		p.carried = append(p.carried, ne)
		carriedCanon = append(carriedCanon, shown)
	}
	p.counts = map[string]int64{"entries_purged": int64(len(p.purged)), "lines_purged": int64(len(rule)),
		"entries_kept": int64(len(seen) - len(p.purged)), "lines_kept": keptLines, "carry_forward_entries": int64(len(entries))}
	if p.purgedHash, err = hashOf(canon); err != nil {
		return p, err
	}
	p.carriedHash, err = hashOf(carriedCanon)
	return p, err
}

// newEntry is a document's carry-forward entry as the ledger posts it: under the document's own source, so its sub-ledger
// still reconciles with its account by source (INV-07, balance.Control), with the boundary in its posting identity.
func newEntry(boundary time.Time, e Entry) (storage.NewEntry, map[string]any, error) {
	day := boundary.Format(time.DateOnly)
	ne := storage.NewEntry{PostingDate: boundary.AddDate(0, 0, -1), PostingIdentity: fmt.Sprintf("%s:%s:%s:%s", BoundarySource, day, e.Source.Type, e.Source.ID),
		Source: storage.Source{Type: e.Source.Type, ID: e.Source.ID, Version: 1}, RuleVersion: RuleVersion,
		Memo: fmt.Sprintf("Carried forward to %s for %s %s, purged after retention", day, strings.ReplaceAll(e.Source.Type, "_", " "), e.Source.ID)}
	var lines []any
	for _, l := range e.Lines {
		account, err := uuid.Parse(l.Account)
		if err != nil {
			return ne, nil, err
		}
		nl := storage.NewLine{AccountID: account, Currency: l.Amount.Currency().Code, Amount: storage.Decimal(l.Amount.String()),
			FXRate: storage.Decimal(l.Rate.String()), FXRateSource: RateSource, BaseAmount: storage.Decimal(l.Base.String())}
		if nl.PartyID, err = optionalID(l.Party); err != nil {
			return ne, nil, err
		}
		if nl.InstrumentID, err = optionalID(l.Instrument); err != nil {
			return ne, nil, err
		}
		if l.DimensionSet != "" {
			for _, v := range strings.Split(l.DimensionSet, ",") {
				id, err := uuid.Parse(v)
				if err != nil {
					return ne, nil, err
				}
				nl.DimensionValues = append(nl.DimensionValues, id)
			}
		}
		ne.Lines = append(ne.Lines, nl)
		lines = append(lines, map[string]any{"account_id": l.Account, "dimension_values": l.DimensionSet, "party_id": l.Party,
			"instrument_id": l.Instrument, "currency": l.Amount.Currency().Code, "amount": l.Amount.String(), "rate": l.Rate.String(),
			"base_amount": l.Base.String()})
	}
	return ne, map[string]any{"posting_identity": ne.PostingIdentity, "lines": lines}, nil
}

func writeSigned(ctx context.Context, blobs ports.BlobStore, signer ports.Signer, key string, form SignedForm) error {
	msg, err := json.Marshal(form)
	if err != nil {
		return err
	}
	sig, err := signer.Sign(ctx, msg)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(SignedBoundary{Boundary: form, Signature: sig})
	if err != nil {
		return err
	}
	if err := blobs.Create(ctx, key, bytes.NewReader(raw)); err != nil && !errors.Is(err, ports.ErrExists) {
		return err
	}
	return nil
}

// stored reads an amount the ledger stored (NUMERIC(20,4)) into its currency's minor units.
func stored(d storage.Decimal, cur money.Currency) (money.Money, error) {
	x, _, err := apd.NewFromString(string(d))
	if err != nil {
		return money.Money{}, err
	}
	return money.FromRounded(x, cur)
}

// setKey is a dimension set as the rule keys it: its values, in their stored order, comma-separated.
func setKey(values []uuid.UUID) string {
	texts := make([]string, len(values))
	for i, v := range values {
		texts[i] = v.String()
	}
	return strings.Join(texts, ",")
}

func idText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func optionalID(s string) (*uuid.UUID, error) {
	if s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(s)
	return &id, err
}

func hashOf(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(b)
	return h[:], nil
}
