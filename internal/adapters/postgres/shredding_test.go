//go:build local

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// refusing is a register bucket that refuses every write, as one that is down would.
type refusing struct{ ports.BlobStore }

func (refusing) Create(context.Context, string, io.Reader) error {
	return errors.New("the bucket is down")
}

// sealedOf returns the sealed values of org's events about object.
func sealedOf(t *testing.T, org, object uuid.UUID) []storage.SealedValue {
	t.Helper()
	var out []storage.SealedValue
	for _, e := range events(t, org) {
		var ev audit.Event
		if err := json.Unmarshal([]byte(e.Body), &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Object.ID != object {
			continue
		}
		for field, c := range ev.Changes {
			for _, v := range c {
				if k, ct, ok := audit.SealedOf(v); ok {
					out = append(out, storage.SealedValue{EventID: ev.EventID, Field: field, Key: k, Text: ct})
				}
			}
		}
	}
	return out
}

// Crypto-shredding at the end of retention (migration 000309; §11.8, design D9): a year past its retention is listed
// and nothing is shredded until the DPO approves it; the database refuses a shredding unapproved or unregistered, a run
// within retention, a key deleted outside its run, a second approval, and the API's or the worker's work done by the
// other; with the register down nothing is shredded; then the register is written first, that year's keys go, its
// values read as shredded while the person's later year still opens, and the chain verifies. A restore re-applies the
// signed register: a run the copy lacks is recreated and its keys go again, and an anonymisation the copy lacks is
// applied again; an entry signed by another key is refused. Another organisation sees none of it.
// req: data/backup-and-recovery Personal data in immutable records is crypto-shredded at the end of retention
// req: data/backup-and-recovery Shredded keys do not survive in backups or exports
// req: AUD-01
// req: QA-ENG-05
// asvs: V11.3.2, V8.2.2
func TestAttackShreddingNeedsTheDPOAndTheRegister(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Shred A Pte Ltd"), books(t, "Shred B Pte Ltd")
	fields := detailsKey(t)
	api := store(t, "fyniq_api", postgres.WithFingerprints(fields))
	old := store(t, "fyniq_api", postgres.WithFingerprints(fields), postgres.WithClock(clock.NewFrozen(time.Date(2015, 6, 1, 2, 0, 0, 0, time.UTC))))
	worker := store(t, "fyniq_worker", postgres.WithFingerprints(fields))
	blobs, signer := anchoring(t)
	now := clock.System{}.Now()
	dpo := "user:" + founder(t, api, a.Org, 1, now).String()

	var ada storage.Contact
	if err := old.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		ada, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Ada Tan", Customer: true, Individual: true, Phone: "+65 9123 4567"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d := ada.ContactDetails
		d.Phone = "+65 8000 0000"
		_, err := u.Contacts().Update(ctx, ada.ID, ada.Version, d, true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sealed := sealedOf(t, a.Org, ada.ID)
	early := sealed[0].Key // 2015's

	// Listed, never shredded unapproved.
	if n, err := audit.Shred(ctx, worker, blobs, signer, nil, now, a.Org); err != nil || n != 0 {
		t.Fatalf("listing: %d %v", n, err)
	}
	var run storage.ShreddingRun
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		runs, err := u.Compliance().ShreddingRuns(ctx)
		if err == nil && (len(runs) != 1 || runs[0].Status != "listed" || runs[0].Cohort.Year() != 2015 || len(runs[0].Keys) != 1 || runs[0].Keys[0] != early) {
			t.Fatalf("the runs: %+v", runs)
		}
		run = runs[0]
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if keys, _ := blobs.List(ctx, audit.ShreddingPrefix(a.Org)); len(keys) != 0 {
		t.Errorf("a register entry before the DPO approved: %v", keys)
	}
	for _, c := range []struct {
		what, role, stmt string
		args             []any
		states           []string
	}{
		{"shredding a listed run", "fyniq_worker", `SELECT fyniq.shred_person_keys($1)`, []any{run.ID}, []string{"FY318"}},
		{"the API shredding", "fyniq_api", `SELECT fyniq.shred_person_keys($1)`, []any{run.ID}, []string{"42501"}},
		{"the API re-applying a register", "fyniq_api", `SELECT fyniq.reapply_shredding($1, DATE '2015-12-31', ARRAY[$2]::uuid[], now(), 'user:x', now(), 'x')`,
			[]any{run.ID, early}, []string{"42501"}},
		{"the API listing a run", "fyniq_api", `INSERT INTO fyniq.pd_shredding_run (cohort, keys, listed_at) VALUES (DATE '2014-12-31', ARRAY[$1]::uuid[], now())`,
			[]any{early}, []string{"42501"}},
		{"the worker approving", "fyniq_worker", `UPDATE fyniq.pd_shredding_run SET status = 'approved', approved_by = 'user:x', approved_at = now()`, nil, []string{"42501"}},
		{"a run within retention", "fyniq_worker", `INSERT INTO fyniq.pd_shredding_run (cohort, keys, listed_at) VALUES (DATE '2026-12-31', ARRAY[$1]::uuid[], now())`,
			[]any{early}, []string{"FY318"}},
		{"a key deleted with an unapproved run's mark", "fyniq_migrator", `SELECT set_config('fyniq.shredding_run', $1::text, true); DELETE FROM fyniq.person_key WHERE id = $1`,
			[]any{run.ID.String(), early}, []string{"FY317"}},
		{"the owner deleting a run", "fyniq_migrator", `DELETE FROM fyniq.pd_shredding_run`, nil, []string{"FY318"}},
		{"the owner changing what a run lists", "fyniq_migrator", `UPDATE fyniq.pd_shredding_run SET keys = ARRAY[gen_random_uuid()]`, nil, []string{"FY318"}},
		{"the API registering", "fyniq_api", `UPDATE fyniq.pd_shredding_run SET register = 'x'`, nil, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, c.role), a.Org, func(tx pgx.Tx) error {
			for _, stmt := range strings.Split(c.stmt, "; ") {
				args := c.args
				if strings.HasPrefix(stmt, "DELETE") && len(args) == 2 {
					args = args[1:]
				} else if strings.HasPrefix(stmt, "SELECT set_config") {
					args = args[:1]
				}
				if _, err := tx.Exec(ctx, stmt, args...); err != nil {
					return err
				}
			}
			return nil
		})
		wantState(t, c.what, err, c.states...)
	}

	// The DPO approves, once.
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		run, err = u.Compliance().ApproveShreddingRun(ctx, run.ID, run.Version, dpo, now)
		return err
	}); err != nil || run.Status != "approved" || run.ApprovedBy != dpo {
		t.Fatalf("approving: %+v %v", run, err)
	}
	err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Compliance().ApproveShreddingRun(ctx, run.ID, run.Version, dpo, now)
		return err
	})
	if !storageCode(err, storage.CodeShredding) {
		t.Errorf("a second approval: %v", err)
	}

	// The register first: with it down, nothing is shredded.
	if _, err := audit.Shred(ctx, worker, refusing{blobs}, signer, nil, now, a.Org); err == nil {
		t.Error("shredding went on without its register entry")
	}
	opened := openAll(t, api, a.Org, sealed)
	if opened[0].Shredded {
		t.Fatal("a key went without its register entry")
	}
	if n, err := audit.Shred(ctx, worker, blobs, signer, nil, now, a.Org); err != nil || n != 1 {
		t.Fatalf("shredding: %d %v", n, err)
	}
	keys, _ := blobs.List(ctx, audit.ShreddingPrefix(a.Org))
	if len(keys) != 1 || keys[0] != audit.ShreddingKey(a.Org, run.ID) {
		t.Errorf("the register: %v", keys)
	}
	for i, o := range openAll(t, api, a.Org, sealed) {
		switch {
		case sealed[i].Key == early && (!o.Shredded || o.Cohort == nil || o.Cohort.Year() != 2015 || o.ShreddedOn == nil):
			t.Errorf("a value of 2015 still opens, or says not when it was shredded: %+v", o)
		case sealed[i].Key != early && (o.Shredded || o.Value == ""):
			t.Errorf("a later year's value does not open: %+v", o)
		}
	}
	if _, err := audit.Chain(events(t, a.Org)); err != nil {
		t.Errorf("the chain after shredding: %v", err)
	}
	if !strings.Contains(auditActions(t, a.Org), "pdpa.keys_shredded") {
		t.Error("the shredding is not in the trail")
	}
	if n, err := audit.ReapplyPDPA(ctx, worker, blobs, signer, a.Org); err != nil || n != 0 {
		t.Errorf("re-applying where the run is done already: %d %v", n, err)
	}

	// A restore: in a copy that lacks the run and the anonymisation, both are applied again from the register.
	c := books(t, "Shred C Pte Ltd")
	var ben storage.Contact
	if err := old.Do(ctx, c.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		ben, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Ben Lim", Customer: true, Individual: true, Phone: "+65 9000 0000",
			BillingEmail: "ben@example.com", Address: "3 Ben Road"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	benKey := sealedOf(t, c.Org, ben.ID)[0].Key
	approvedAt := now.Add(-time.Hour)
	if _, err := audit.WriteShredding(ctx, blobs, signer, audit.Shredding{OrganisationID: c.Org, RunID: uuid.New(), Cohort: "2015-12-31",
		Keys: []uuid.UUID{benKey}, ListedAt: approvedAt.UTC().Format(time.RFC3339Nano), ApprovedBy: dpo, ApprovedAt: approvedAt.UTC().Format(time.RFC3339Nano),
		WrittenAt: now.UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	if _, err := audit.RecordAnonymisation(ctx, blobs, signer, audit.Anonymisation{OrganisationID: c.Org, EntryID: uuid.New(), ContactID: ben.ID,
		AnonymisedBy: dpo, AnonymisedAt: approvedAt.UTC().Format(time.RFC3339Nano), WrittenAt: now.UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	if n, err := audit.ReapplyPDPA(ctx, worker, blobs, signer, c.Org); err != nil || n != 2 {
		t.Fatalf("re-applying in the copy: %d %v", n, err)
	}
	if o := openAll(t, api, c.Org, sealedOf(t, c.Org, ben.ID)[:1]); !o[0].Shredded {
		t.Error("a key the copy brought back still opens")
	}
	if err := api.Do(ctx, c.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		got, err := u.Contacts().Get(ctx, ben.ID)
		if err == nil && (got.Phone != "" || got.BillingEmail != "" || got.Address != "Anonymised" || got.LegalName != "Ben Lim") {
			t.Errorf("the anonymised contact: %+v", got.ContactDetails)
		}
		runs, _ := u.Compliance().ShreddingRuns(ctx)
		if len(runs) != 1 || !runs[0].Reapplied || runs[0].Status != "done" {
			t.Errorf("the recreated run: %+v", runs)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := audit.ReapplyPDPA(ctx, worker, blobs, signer, c.Org); err != nil || n != 0 {
		t.Errorf("re-applying twice: %d %v", n, err)
	}
	other, err := local.NewEd25519(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := audit.ReapplyPDPA(ctx, worker, blobs, other, c.Org); err == nil {
		t.Error("an entry signed by another key was re-applied")
	}

	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		runs, err := u.Compliance().ShreddingRuns(ctx)
		if err == nil && len(runs) != 0 {
			t.Errorf("another organisation sees %d runs", len(runs))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func openAll(t *testing.T, s storage.Store, org uuid.UUID, values []storage.SealedValue) []storage.OpenedValue {
	t.Helper()
	var out []storage.OpenedValue
	if err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		out, err = u.Audit().OpenSealed(ctx, values)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func auditActions(t *testing.T, org uuid.UUID) string {
	t.Helper()
	var b strings.Builder
	for _, e := range events(t, org) {
		var ev audit.Event
		_ = json.Unmarshal([]byte(e.Body), &ev)
		b.WriteString(ev.Action + "\n")
	}
	return b.String()
}
