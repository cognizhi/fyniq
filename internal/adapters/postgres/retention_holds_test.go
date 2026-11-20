//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/retention"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A hold keeps what it covers (AUD-11; 001300). A hold on the financial year of the oldest events keeps the audit trail's
// purge from removing any of them, whatever the job does: the job stops before the first covered event, and the database
// refuses the delete itself (FY361). Lifted, the purge runs. Only the API places a hold and lifts it, by the lift's columns,
// once; a second hold on one subject, a hold lifted twice and any change or delete are refused, whoever asks.
// req: AUD-11
// req: AUD-09
// req: SEC-03
// req: QA-ENG-05
// asvs: V8.2.2, V16.4.2
func TestAHoldKeepsTheTrailFromThePurgeAndTheDatabaseRefusesIt(t *testing.T) {
	ctx := context.Background()
	b, old := oldBooks(t, "Held Trail Pte Ltd")
	api := store(t, "fyniq_api", postgres.WithClock(old))
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	dpo := "user:" + founder(t, api, b.Org, 1, now).String()
	var hold storage.Hold
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		hold, err = u.Retention().PlaceHold(ctx, storage.NewHold{Kind: storage.HoldFinancialYear, Reason: "A dispute over 2010 is open", PlacedBy: dpo, PlacedAt: now,
			Year: &storage.HoldYear{Number: 1, StartsOn: ymd(2010, 1, 1), EndsOn: ymd(2010, 12, 31)}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	switchPurgeOn(t, b.Org)
	all := events(t, b.Org)
	exec := func(role, stmt string, args ...any) error {
		return asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt, args...); return err })
	}
	worker := store(t, "fyniq_worker")
	blobs, signer := anchoring(t)
	later := clock.NewFrozen(time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC))

	// The job stops before the first covered event.
	if n, err := audit.PurgeAll(ctx, worker, blobs, signer, later); err != nil || n != 0 {
		t.Fatalf("a purge under a hold removed %d events: %v", n, err)
	}
	if got := len(events(t, b.Org)); got != len(all) {
		t.Fatalf("events went under a hold: %d of %d", got, len(all))
	}
	// And the database refuses it if the job is wrong: a boundary and the purge of the held events, in one transaction.
	boundary := "INSERT INTO fyniq.audit_boundary (last_seq, last_hash, last_occurred_at, approval, counts, anchor_key, written_at) VALUES (2, $1, now(), 'x', '{}', 'k', now())"
	wantState(t, "a purge of held events", asOrg(ctx, conn(t, "fyniq_worker"), b.Org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, boundary, all[1].Hash); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "SELECT fyniq.audit_purge(2)")
		return err
	}), "FY361")
	wantState(t, "the owner deleting a held event with the purge's flag", exec("fyniq_migrator",
		"SELECT set_config('fyniq.audit_purging', 'on', true); DELETE FROM fyniq.audit_event WHERE seq = 1"), "FY361")

	// The hold's own rules.
	place := `INSERT INTO fyniq.retention_hold (kind, year_number, year_starts_on, year_ends_on, reason, placed_by, placed_at)
	          VALUES ('financial_year', 1, DATE '2010-01-01', DATE '2010-12-31', 'again', 'user:x', now())`
	for _, c := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the worker placing a hold", "fyniq_worker", place, []string{"42501"}},
		{"a second hold on the year", "fyniq_api", place, []string{"23505"}},
		{"the API changing a reason", "fyniq_api", `UPDATE fyniq.retention_hold SET reason = 'nothing'`, []string{"42501"}},
		{"the owner changing a reason", "fyniq_migrator", `UPDATE fyniq.retention_hold SET reason = 'nothing'`, []string{"FY360"}},
		{"the owner deleting a hold", "fyniq_migrator", `DELETE FROM fyniq.retention_hold`, []string{"FY360"}},
		{"a hold lifted with no reason", "fyniq_api", `UPDATE fyniq.retention_hold SET lifted_by = 'user:x', lifted_at = now(), lift_reason = ''`, []string{"23514"}},
		{"a hold placed already lifted", "fyniq_api", `INSERT INTO fyniq.retention_hold (kind, year_number, year_starts_on, year_ends_on, reason, placed_by, placed_at, lifted_by,
		    lifted_at, lift_reason) VALUES ('financial_year', 2, DATE '2011-01-01', DATE '2011-12-31', 'why', 'user:x', now(), 'user:x', now(), 'because')`, []string{"42501", "FY360"}},
		{"a hold of no subject", "fyniq_api", `INSERT INTO fyniq.retention_hold (kind, reason, placed_by, placed_at) VALUES ('record', 'why', 'user:x', now())`, []string{"23514"}},
	} {
		wantState(t, c.what, exec(c.role, c.stmt), c.states...)
	}
	// A coverage question for another organisation is an error, never "none".
	other := books(t, "Held Trail Other Pte Ltd")
	wantState(t, "another organisation's holds asked about", asOrg(ctx, conn(t, "fyniq_worker"), other.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "SELECT fyniq.hold_covers_event($1, $2, now())", b.Org, uuid.New())
		return err
	}), "FY361")

	// Lifted once with a reason, it is audited, a second lift is refused, and the purge runs.
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Retention().LiftHold(ctx, hold.ID, dpo, "The dispute is settled", now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Retention().LiftHold(ctx, hold.ID, dpo, "Again", now)
		return err
	})
	var se *storage.Error
	if !errors.As(err, &se) || se.Code != storage.CodeHold {
		t.Errorf("a second lift: %v", err)
	}
	var actions []string
	if err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action FROM fyniq.audit_event WHERE object_type = 'retention_hold' ORDER BY seq`)
		if err != nil {
			return err
		}
		actions, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}); err != nil || len(actions) != 2 || actions[0] != "retention_hold.placed" || actions[1] != "retention_hold.lifted" {
		t.Errorf("the hold's audit events: %v %v", actions, err)
	}
	if n, err := audit.PurgeAll(ctx, worker, blobs, signer, later); err != nil || n == 0 {
		t.Errorf("a purge once the hold is lifted removed %d events: %v", n, err)
	}
}

// A hold on a financial year keeps the ledger's purge of that year: the plan keeps the year, and the database refuses a
// boundary after a covered date though every other rule of the purge holds. Lifted, the year goes.
// req: AUD-11
// req: AUD-10
// req: QA-ENG-05
// asvs: V8.2.2
func TestAHoldKeepsTheLedgerYearFromThePurge(t *testing.T) {
	ctx := context.Background()
	b := newPurgeBooks(t, "Held Ledger Pte Ltd")
	b.closeYears(t, 2010, 2011)
	switchPurgeOn(t, b.Org)
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	blobs, signer := anchoring(t)
	now := ymd(2026, 11, 9).Add(9 * time.Hour)
	var hold storage.Hold
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		hold, err = u.Retention().PlaceHold(ctx, storage.NewHold{Kind: storage.HoldFinancialYear, Reason: "An audit of 2010 is open", PlacedBy: "user:" + uuid.NewString(), PlacedAt: now,
			Year: &storage.HoldYear{Number: 1, StartsOn: ymd(2010, 1, 1), EndsOn: ymd(2010, 12, 31)}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := retention.PurgeLedger(ctx, worker, blobs, signer, now, b.Org); err != nil || !got.Boundary.IsZero() {
		t.Fatalf("a held year was purged to %s: %v", got.Boundary, err)
	}
	boundary := "INSERT INTO fyniq.ledger_boundary (boundary, approval, counts, purged_hash, anchor_key, written_at) VALUES ('2011-01-01', 'x', '{}', '\\x" +
		"0000000000000000000000000000000000000000000000000000000000000000', 'k', now())"
	in := func() error {
		return asOrg(ctx, conn(t, "fyniq_worker"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, boundary); return err })
	}
	wantState(t, "a boundary after a held year", in(), "FY361")
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Retention().LiftHold(ctx, hold.ID, "user:"+uuid.NewString(), "The audit is closed", now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	wantState(t, "the boundary once the hold is lifted fails the purge's own rule, not the hold's", in(), "FY305") // a boundary commits only with its purge
	if got, err := retention.PurgeLedger(ctx, worker, blobs, signer, now, b.Org); err != nil || !got.Boundary.Equal(ymd(2011, 1, 1)) {
		t.Errorf("the year was not purged once the hold was lifted: %+v %v", got, err)
	}
}

// What a record's hold and a contact's hold cover (AUD-11, design D1): a record's, its document's events and the entries posted
// from it; a contact's, the contact's events, every document that names it, and their entries; nothing of a document that neither
// names. A lifted hold covers nothing.
// req: AUD-11
// asvs: V8.2.2
func TestAHoldOnARecordOrAContactCoversItsEventsAndEntries(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Held Coverage Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	var contact storage.Contact
	var named, other storage.Document
	if err := api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if contact, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Held Customer Pte Ltd", Customer: true, Currency: "SGD"}); err != nil {
			return err
		}
		if named, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: []byte(`{"lines":[],"customer_id":"` + contact.ID.String() + `"}`)}); err != nil {
			return err
		}
		if other, err = u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: []byte(`{"lines":[]}`)}); err != nil {
			return err
		}
		for i, d := range []storage.Document{named, other} {
			if _, err = u.Ledger().Append(ctx, storage.NewEntry{PostingDate: []time.Time{ymd(2026, 10, 5), ymd(2026, 11, 6)}[i], PostingIdentity: "cover:" + d.ID.String(), RuleVersion: "test@1",
				Source: storage.Source{Type: "manual_journal", ID: d.ID.String(), Version: 1}, Lines: []storage.NewLine{
					{AccountID: b.Bank.ID, Currency: "SGD", Amount: "10.00", FXRate: "1", FXRateSource: "base", BaseAmount: "10.00"},
					{AccountID: b.Sales.ID, Currency: "SGD", Amount: "-10.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-10.00"}}}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	covered := func() (dates []time.Time, objects map[string]bool) {
		objects = map[string]bool{}
		if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			if dates, err = u.Retention().HeldLedgerDates(ctx); err != nil {
				return err
			}
			all := events(t, b.Org)
			seqs, err := u.Retention().HeldEvents(ctx, 0, all[len(all)-1].Seq)
			if err != nil {
				return err
			}
			held := map[int64]bool{}
			for _, s := range seqs {
				held[s] = true
			}
			for _, e := range all {
				if !held[e.Seq] {
					continue
				}
				var body struct {
					Object struct {
						ID string `json:"id"`
					} `json:"object"`
				}
				if err := json.Unmarshal([]byte(e.Body), &body); err != nil {
					return err
				}
				objects[body.Object.ID] = true
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return dates, objects
	}
	if dates, objects := covered(); len(dates) != 0 || len(objects) != 0 {
		t.Fatalf("no hold, yet covered: %v %v", dates, objects)
	}
	place := func(h storage.NewHold) storage.Hold {
		t.Helper()
		h.Reason, h.PlacedBy, h.PlacedAt = "A claim may follow", "user:"+uuid.NewString(), now
		var out storage.Hold
		if err := api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.Retention().PlaceHold(ctx, h)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	record := place(storage.NewHold{Kind: storage.HoldRecord, DocumentID: other.ID})
	dates, objects := covered()
	if len(dates) != 1 || !dates[0].Equal(ymd(2026, 11, 6)) || !objects[other.ID.String()] || objects[named.ID.String()] || objects[contact.ID.String()] {
		t.Errorf("a hold on a record: %v %v", dates, objects)
	}
	contactHold := place(storage.NewHold{Kind: storage.HoldContact, ContactID: contact.ID})
	dates, objects = covered()
	if len(dates) != 2 || !objects[named.ID.String()] || !objects[contact.ID.String()] || !objects[other.ID.String()] {
		t.Errorf("a hold on a contact too: %v %v", dates, objects)
	}
	for _, h := range []storage.Hold{record, contactHold} {
		if err := api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Retention().LiftHold(ctx, h.ID, "user:"+uuid.NewString(), "Settled", now)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if dates, objects := covered(); len(dates) != 0 || len(objects) != 0 {
		t.Errorf("lifted holds cover: %v %v", dates, objects)
	}
}

// The storage reconcile (AUD-11, design D2): the files of a held record are held in the store, including a file attached after the
// hold was placed; a file of another record is not; once the hold is lifted every file is freed; an organisation that never held
// anything is left alone.
// req: AUD-11
// req: FR-DOC-02
// asvs: V14.2.7
func TestTheStoredFilesOfAHeldRecordAreHeldAndFreedWithIt(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	b := books(t, "Held Files Pte Ltd")
	svc := journal.Service{Store: api}
	doc, err := svc.Create(ctx, b.Org, saleDraft(t, b, porttest.Day(10, 14), "250.00"))
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := svc.Create(ctx, b.Org, saleDraft(t, b, porttest.Day(10, 15), "75.00"))
	if err != nil {
		t.Fatal(err)
	}
	first, apart := attach(t, api, b.Org, doc.ID, "receipt"), attach(t, api, b.Org, elsewhere.ID, "receipt")
	for _, d := range []uuid.UUID{doc.ID, elsewhere.ID} {
		if _, err := svc.Post(ctx, b.Org, d); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	until := time.Date(2034, 1, 1, 0, 0, 0, 0, time.UTC)
	keep := func(e storage.Evidence) string {
		key := "evidence/" + b.Org.String() + "/" + e.ID.String()
		if err := dir.Create(ctx, key, strings.NewReader("%PDF-1.7 receipt")); err != nil {
			t.Fatal(err)
		}
		if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Evidence().Finalise(ctx, e.ID, key, until)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return key
	}
	held := func(key string) bool {
		t.Helper()
		r, err := dir.RetentionOf(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		return r.Held
	}
	k1, kApart := keep(first), keep(apart)
	synced := func() retention.Synced {
		t.Helper()
		got, err := retention.SyncHolds(ctx, worker, dir, b.Org)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := synced(); got != (retention.Synced{}) || held(k1) || held(kApart) {
		t.Fatalf("no hold ever, yet: %+v", got)
	}
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	var hold storage.Hold
	if err := api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		hold, err = u.Retention().PlaceHold(ctx, storage.NewHold{Kind: storage.HoldRecord, DocumentID: doc.ID, Reason: "A claim is expected", PlacedBy: "user:" + uuid.NewString(), PlacedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	late := keep(attach(t, api, b.Org, doc.ID, "receipt")) // attached to the held record after the hold was placed
	if got := synced(); got.Held != 2 || got.Freed != 0 || !held(k1) || !held(late) || held(kApart) {
		t.Errorf("the held record's files: %+v %v %v %v", got, held(k1), held(late), held(kApart))
	}
	if err := dir.Delete(ctx, k1); !errors.Is(err, ports.ErrRetained) {
		t.Errorf("a held file was deletable: %v", err)
	}
	if got := synced(); got.Held != 0 || got.Freed != 0 {
		t.Errorf("a second reconcile changed %+v", got)
	}
	if err := api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Retention().LiftHold(ctx, hold.ID, "user:"+uuid.NewString(), "The claim lapsed", now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := synced(); got.Freed != 2 || held(k1) || held(late) {
		t.Errorf("a lifted hold left files held: %+v", got)
	}
}

// A held contact's keys, and every key of a held year, are neither listed for shredding nor deleted (AUD-11, §11.8): the year waits
// until the hold is lifted, and a run that reaches a held key fails whole in the database, whoever approved it.
// req: AUD-11
// req: CMP-PD-05
// asvs: V11.3.2, V8.2.2
func TestAHeldPersonsKeysAreNotShredded(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Held Keys Pte Ltd")
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	var contact storage.Contact
	if err := api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		contact, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Held Person", Customer: true, Currency: "SGD", Individual: true})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	person := "contact:" + contact.ID.String()
	exec := func(role, stmt string, args ...any) error {
		return asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt, args...); return err })
	}
	for _, cohort := range []string{"2015-12-31", "2016-12-31"} {
		if err := exec("fyniq_api", "INSERT INTO fyniq.person_key (person, cohort, wrapped) VALUES ($1, $2::date, $3)", person, cohort, make([]byte, 32)); err != nil {
			t.Fatal(err)
		}
	}
	due := func() int {
		t.Helper()
		var n int
		if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := u.Compliance().DueKeyCohorts(ctx, ymd(2018, 1, 1))
			n = len(c)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := due(); n != 2 {
		t.Fatalf("two years of keys are due: %d", n)
	}
	place := func(h storage.NewHold) storage.Hold {
		t.Helper()
		h.Reason, h.PlacedBy, h.PlacedAt = "A claim is expected", "user:"+uuid.NewString(), now
		var out storage.Hold
		if err := api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.Retention().PlaceHold(ctx, h)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	lift := func(h storage.Hold) {
		t.Helper()
		if err := api.Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Retention().LiftHold(ctx, h.ID, "user:"+uuid.NewString(), "Settled", now)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	run := approvedRun(t, api, worker, b.Org, "2015-12-31", "user:"+uuid.NewString()) // listed and approved before the holds
	byContact := place(storage.NewHold{Kind: storage.HoldContact, ContactID: contact.ID})
	if n := due(); n != 0 {
		t.Errorf("a held contact's years are due: %d", n)
	}
	wantState(t, "shredding a held contact's key in an approved run", shredErr(ctx, worker, b.Org, run), "FY361")
	lift(byContact)
	byYear := place(storage.NewHold{Kind: storage.HoldFinancialYear, Year: &storage.HoldYear{Number: 6, StartsOn: ymd(2015, 1, 1), EndsOn: ymd(2015, 12, 31)}})
	if n := due(); n != 1 { // 2016's keys are the contact's, and the contact's hold is lifted
		t.Errorf("only the other year is due while 2015 is held: %d", n)
	}
	wantState(t, "shredding a held year's key in an approved run", shredErr(ctx, worker, b.Org, run), "FY361")
	lift(byYear)
	if err := shredErr(ctx, worker, b.Org, run); err != nil {
		t.Errorf("the run once the holds are lifted: %v", err)
	}
	if n := due(); n != 1 { // 2015's run is done; 2016 is due
		t.Errorf("the other year is due once the holds are lifted: %d", n)
	}
}

// approvedRun lists the keys of a cohort as a run, and has the DPO approve it.
func approvedRun(t *testing.T, api, worker *postgres.Store, org uuid.UUID, cohort, dpo string) storage.ShreddingRun {
	t.Helper()
	ctx := context.Background()
	day, _ := time.Parse(time.DateOnly, cohort)
	var run storage.ShreddingRun
	if err := worker.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		due, err := u.Compliance().DueKeyCohorts(ctx, ymd(2018, 1, 1))
		if err != nil {
			return err
		}
		for _, c := range due {
			if c.Cohort.Equal(day) {
				run, err = u.Compliance().ListShreddingRun(ctx, c.Cohort, c.Keys, clock.System{}.Now())
			}
		}
		return err
	}); err != nil || run.ID == uuid.Nil {
		t.Fatalf("listing the run of %s: %v", cohort, err)
	}
	if err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		run, err = u.Compliance().ApproveShreddingRun(ctx, run.ID, run.Version, dpo, clock.System{}.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return run
}

// shredErr runs the worker's shredding of an approved run and returns the database's refusal, if any.
func shredErr(ctx context.Context, worker *postgres.Store, org uuid.UUID, run storage.ShreddingRun) error {
	return worker.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Compliance().ShredRun(ctx, run.ID, "pdpa-shredding/"+org.String()+"/"+run.ID.String()+".json")
		return err
	})
}
