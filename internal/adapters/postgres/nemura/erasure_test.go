//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/erasure"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

func erasureCode(err error) string {
	var r *erasure.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// A former employee's personal data is erased only after the retention periods, by one Owner or HR administrator asking and another approving, with no legal hold on them. The worker files a
// signed entry outside the database first, then empties the personal columns of the records that may change and leaves every append-only guard as it was; after a restore the entry is applied again,
// and one that does not verify is not (NM-SEC-08).
// part of NM-SEC-08 (the web page and the runbook are separate)
// part of NM-INT-13 (privacy deletions applied again after a restore)
// asvs: V8.3.2, V14.2.3, V16.2.5
func TestAFormerEmployeesDataIsErasedAfterItsRetentionByTwoPeopleAndErasedAgainAfterARestore(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	day := d("2099-03-01")
	at := day.Add(9 * time.Hour)
	owner, hr := c.owner, c.hr
	var dee, ben uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		job := people.Job{Title: "Analyst", Department: "Sales", Worksite: "SG", EmploymentType: "full_time"}
		cl := &people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"}
		if dee, err = nemura.AddPerson(ctx, tx, c.org, at, owner, people.Person{LegalName: "Dee Ng", PreferredName: "Dee", WorkEmail: "dee@example.test", Phone: "91234567", EmploymentStart: d("2080-02-02"), LeavingDate: ptr(d("2090-01-31")), LeavingReason: "resigned"}, job, cl); err != nil {
			return err
		}
		ben, err = nemura.AddPerson(ctx, tx, c.org, at, owner, people.Person{LegalName: "Ben Lim", EmploymentStart: d("2080-02-02")}, job, cl)
		return err
	})
	// Dee's own contact details, an emergency contact and an NRIC, as the records hold them.
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_contact (person_id, personal_email, address, postal_code, updated_at) VALUES ($1, 'dee@home.example', '1 Test Road', '123456', $2)`, dee, at); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.emergency_contact (person_id, position, contact_name, relationship, contact_phone, updated_at) VALUES ($1, 1, 'Mum Ng', 'mother', '81234567', $2)`, dee, at); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_secret (person_id, nric_sealed, nric_masked, nric_fingerprint) VALUES ($1, '\x0102'::bytea, 'S****567D', '\x03'::bytea)`, dee); err != nil {
			return err
		}
		for _, o := range []string{"person_contact", "emergency_contact", "person_secret"} {
			if err := nemura.Audit(ctx, tx, c.org, at, audit.Actor{Type: "user", ID: owner.String()}, "test.setup", audit.Object{Type: o, ID: dee}, nil, ""); err != nil {
				return err
			}
		}
		return nil
	})
	var ecID uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM nemura.emergency_contact WHERE person_id = $1`, dee).Scan(&ecID)
	})
	_ = ecID
	owners, hrAdmin := []authz.Role{authz.Owner}, []authz.Role{authz.HRAdmin}
	request := func(by uuid.UUID, held []authz.Role, person uuid.UUID, on time.Time) (uuid.UUID, error) {
		var id uuid.UUID
		err := c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			id, err = nemura.RequestErasure(ctx, tx, c.org, at, on, by, held, person, "left in 2090; the periods have run")
			return err
		})
		return id, err
	}
	// Too early, not left, no right, a legal hold, an unanswered access request: each refused, each with its own code.
	if _, err := request(hr, hrAdmin, dee, d("2090-06-01")); erasureCode(err) != erasure.CodeNotYet {
		t.Errorf("before the period ran: %v", err)
	}
	if _, err := request(hr, hrAdmin, ben, day); erasureCode(err) != erasure.CodeNotLeft {
		t.Errorf("a person who has not left: %v", err)
	}
	if _, err := request(c.pay, []authz.Role{authz.PayrollAdmin}, dee, day); erasureCode(err) != "NM_ROLE_REQUIRED" {
		t.Errorf("a payroll administrator: %v", err)
	}
	var hold uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		hold, err = nemura.PlaceHold(ctx, tx, c.org, at, owner, "people-records", &dee, "a claim may follow", day)
		return err
	})
	if _, err := request(hr, hrAdmin, dee, day); erasureCode(err) != erasure.CodeHeld {
		t.Errorf("a legal hold: %v", err)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ReleaseHold(ctx, tx, c.org, at, owner, hold, day)
	})

	order, err := request(hr, hrAdmin, dee, day)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideErasure(ctx, tx, c.org, at, hr, hrAdmin, order, true)
	}); erasureCode(err) != erasure.CodeOwnRequest {
		t.Errorf("the one who asked approves: %v", err)
	}
	// The worker refuses an order that is not approved, whatever it is asked.
	blobs, _ := local.NewDir(t.TempDir())
	signer, _ := local.NewEd25519(bytes.Repeat([]byte{5}, 32))
	runner := &nemura.ErasureRunner{Store: store(t, "nemura_worker"), Blobs: blobs, Signer: signer, Clock: clock.NewFrozen(at)}
	if err := runner.Execute(ctx, c.org, order); erasureCode(err) != erasure.CodeNotApproved {
		t.Errorf("an order not approved: %v", err)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideErasure(ctx, tx, c.org, at, owner, owners, order, true)
	})
	if err := runner.Execute(ctx, c.org, order); err != nil {
		t.Fatal(err)
	}

	read := func() (name, email string, contact, ec, nric int) {
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT legal_name, coalesce(work_email, '') FROM nemura.person WHERE id = $1`, dee).Scan(&name, &email); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.person_contact WHERE person_id = $1 AND (personal_email IS NOT NULL OR address IS NOT NULL OR postal_code IS NOT NULL)`, dee).Scan(&contact); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.emergency_contact WHERE person_id = $1`, dee).Scan(&ec); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.person_secret WHERE person_id = $1 AND nric_sealed IS NOT NULL`, dee).Scan(&nric)
		})
		return
	}
	if name, email, contact, ec, nric := read(); name != "Former employee" || email != "" || contact != 0 || ec != 0 || nric != 0 {
		t.Fatalf("the personal columns are emptied: %q %q %d %d %d", name, email, contact, ec, nric)
	}
	// The register holds a signed entry that names identities and dates and no name; it verifies.
	rc, err := blobs.Get(ctx, erasure.Key(c.org, dee))
	if err != nil {
		t.Fatalf("no register entry: %v", err)
	}
	raw, _ := io.ReadAll(rc)
	_ = rc.Close()
	if strings.Contains(string(raw), "Dee") || strings.Contains(string(raw), "dee@") {
		t.Errorf("the register names the person: %s", raw)
	}
	if _, err := erasure.Open(raw, signer, c.org, erasure.Key(c.org, dee)); err != nil {
		t.Errorf("the register entry does not verify: %v", err)
	}
	// The audit trail names the person by ID only.
	if err := store(t, "nemura_worker").InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE body::text LIKE '%nemura.erasure.%' AND (body::text LIKE '%Dee%' OR body::text LIKE '%dee@%')`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("%d erasure audit events name the person", n)
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE body::text LIKE '%nemura.erasure.executed%'`).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	// Running it again is safe; the order is executed and cannot be run or decided again.
	if err := runner.Execute(ctx, c.org, order); err != nil {
		t.Errorf("a second run: %v", err)
	}
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideErasure(ctx, tx, c.org, at, owner, owners, order, false)
	}); erasureCode(err) != erasure.CodeAlready {
		t.Errorf("an executed order decided again: %v", err)
	}
	if _, err := request(hr, hrAdmin, dee, day); erasureCode(err) != erasure.CodeAlready {
		t.Errorf("an erased person asked for again: %v", err)
	}

	// A restore brings Dee's data back from the backup. The register is applied again before the copy opens.
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		if err := nemura.UpdatePerson(ctx, tx, c.org, at, owner, dee, people.Person{LegalName: "Dee Ng", PreferredName: "Dee", WorkEmail: "dee@example.test", LeavingDate: ptr(d("2090-01-31"))}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE nemura.person_contact SET personal_email = 'dee@home.example', updated_at = $2 WHERE person_id = $1`, dee, at)
		if err != nil {
			return err
		}
		return nemura.Audit(ctx, tx, c.org, at, audit.Actor{Type: "user", ID: owner.String()}, "test.restore", audit.Object{Type: "person_contact", ID: dee}, nil, "")
	})
	// What the restore brings back is not erased_at: the column came back from the backup as null. (UpdatePerson leaves it as it was, so put the backup's value back through the migrator's own path.)
	if name, _, _, _, _ := read(); name == "Former employee" {
		t.Fatal("the restore did not bring the name back in this test")
	}
	// A tampered entry is not applied: it is reported, and the person is not touched by it.
	tamper := bytes.Replace(raw, []byte(`"erased_on"`), []byte(`"erased_on_"`), 1)
	if err := blobs.Put(ctx, erasure.Key(c.org, dee), bytes.NewReader(tamper)); err != nil {
		t.Fatal(err)
	}
	if n, err := runner.Reapply(ctx, c.org); n != 0 || erasureCode(errors.Unwrap(err)) == "" && err == nil {
		t.Errorf("a tampered entry was applied or not reported: %d %v", n, err)
	}
	if name, _, _, _, _ := read(); name == "Former employee" {
		t.Error("a tampered register entry erased a person")
	}
	if err := blobs.Put(ctx, erasure.Key(c.org, dee), bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	n, err := runner.Reapply(ctx, c.org)
	if err != nil || n != 1 {
		t.Fatalf("the register applied again: %d %v", n, err)
	}
	if name, email, contact, _, _ := read(); name != "Former employee" || email != "" || contact != 0 {
		t.Errorf("a restored field came back: %q %q %d", name, email, contact)
	}
	if n, err := runner.Reapply(ctx, c.org); err != nil || n != 0 {
		t.Errorf("applying again is safe and finds nothing more: %d %v", n, err)
	}
}

// The scrub is the database's own function: no role calls it directly, the worker cannot run an erasure from an order that is not approved, early, or for a person who has not left, and the
// append-only guards are as they were (NM-SEC-08).
// part of NM-SEC-08
// asvs: V8.2.1, V8.3.1, V8.3.2
func TestTheErasureFunctionsAreFencedAndNoAppendOnlyGuardMoved(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	day := d("2099-03-01")
	at := day.Add(9 * time.Hour)
	var dee uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		dee, err = nemura.AddPerson(ctx, tx, c.org, at, c.owner, people.Person{LegalName: "Dee Ng", EmploymentStart: d("2080-02-02"), LeavingDate: ptr(d("2090-01-31"))},
			people.Job{Title: "Analyst", Worksite: "SG", EmploymentType: "full_time"}, &people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"})
		return err
	})
	order, err := func() (uuid.UUID, error) {
		var id uuid.UUID
		err := c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			id, err = nemura.RequestErasure(ctx, tx, c.org, at, day, c.hr, []authz.Role{authz.HRAdmin}, dee, "periods have run")
			return err
		})
		return id, err
	}()
	if err != nil {
		t.Fatal(err)
	}
	pgCode := func(err error) string {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			return pg.Code
		}
		return ""
	}
	run := func(s *nemura.Store, want, sql string, args ...any) {
		t.Helper()
		err := s.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
		if pgCode(err) != want {
			t.Errorf("%s: want %s, got %v", sql, want, err)
		}
	}
	worker, api := store(t, "nemura_worker"), store(t, "nemura_api")
	run(api, "42501", `SELECT nemura.erase_person($1, '2099-03-01', '\x00'::bytea)`, order)
	run(api, "42501", `SELECT nemura.reapply_erasure($1, '2099-03-01')`, dee)
	run(worker, "42501", `SELECT nemura.scrub_person($1, now())`, dee)
	run(worker, "NM201", `SELECT nemura.erase_person($1, '2099-03-01', '\x00'::bytea)`, order)   // not approved yet
	run(api, "NM200", `UPDATE nemura.erasure_order SET state = 'executed' WHERE id = $1`, order) // the guard: requested cannot jump to executed
	run(api, "42501", `DELETE FROM nemura.erasure_order`)
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideErasure(ctx, tx, c.org, at, c.owner, []authz.Role{authz.Owner}, order, true)
	})
	run(worker, "NM202", `SELECT nemura.erase_person($1, '2091-01-01', '\x00'::bytea)`, order) // before its eligible date
	run(worker, "NM204", `SELECT nemura.reapply_erasure($1, '2090-06-01')`, dee)               // not a year after leaving
	// No append-only guard moved: a person is still never deleted, and a leave ledger entry or payslip still cannot be changed.
	run(api, "42501", `DELETE FROM nemura.person WHERE id = $1`, dee)
	run(worker, "42501", `UPDATE nemura.payslip SET sha256 = '\x00'::bytea`)
	run(api, "42501", `DELETE FROM nemura.letter_issued`)
}
