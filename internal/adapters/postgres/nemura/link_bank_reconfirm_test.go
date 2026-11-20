//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/link"
)

// After a restore of Nemura's database, Fyniq may hold a bank-detail version that Nemura lost. Fyniq answers an event it ignored with the version and the record it holds; Nemura compares them with its
// own record at that version. A repeat of an event, or a late older one, is not a restore and flags nothing; a version Nemura does not have, or has as another record, raises the person's version floor so
// the next confirmed change is numbered above Fyniq's, holds the person's payments, shows on N.1 #7, and is released only by the ordinary change confirmed with the employee's passkey (NM-INT-13).
// part of NM-INT-13 (the restore's other halves are tested with the restore)
// part of FR-PPL-19 (Fyniq's side is its own test)
// asvs: V8.2.1, V13.2.1, V11.4.1
func TestARestoreThatLostABankChangeIsFoundFromFyniqsAnswerAndHoldsTheirPaymentsUntilReconfirmed(t *testing.T) {
	ctx := context.Background()
	now := today.Add(10 * time.Hour)
	cipher, _ := local.NewFieldKeys(bytes.Repeat([]byte{7}, 64))
	lt := newLeaveTeam(t)
	n := 0
	confirm := func() uuid.UUID {
		n++
		var change uuid.UUID
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			change, err = nemura.ProposeBankChange(ctx, tx, cipher, lt.c.org, now, lt.benLogin, lt.ben, fmt.Sprintf("123-456-%04d", n))
			return err
		})
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.DecideBankChange(ctx, tx, lt.c.org, now, lt.c.owner, []authz.Role{authz.Owner}, change, true, "")
		})
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.ConfirmBankChange(ctx, tx, lt.c.org, now, lt.benLogin, change, true)
		})
		return change
	}
	c1 := confirm()
	offer, _, _ := linked(t, lt, now, &fakeOAuth{})
	fy := newFyniqInMiniature(t, offer, "/v1/people-link/projection-events", "/v1/people-link/hold-events")
	work := fy.sender(t, now).Work()
	sendLast := func() {
		rows := queuedRows(t, lt)
		if err := work(ctx, lt.c.org, rows[len(rows)-1]); err != nil {
			t.Fatal(err)
		}
	}
	var held struct {
		version int
		record  uuid.UUID
	}
	fy.answer = func(env link.Envelope) string {
		if env.Header.Type != nemura.TypeBankDetailsChanged || held.version == 0 {
			return `{"event_id":"x","outcome":"applied"}`
		}
		return fmt.Sprintf(`{"event_id":"x","outcome":"ignored_older","held_bank_version":%d,"held_bank_detail_id":%q}`, held.version, held.record)
	}
	flags := func() []nemura.BankReconfirmRow {
		var out []nemura.BankReconfirmRow
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			out, err = nemura.OpenBankReconfirms(ctx, tx)
			return err
		})
		return out
	}
	blocked := func() bool {
		var b bool
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			b, err = nemura.PaymentBlocked(ctx, tx, uuid.New(), lt.ben)
			return err
		})
		return b
	}
	bankVersions := func() []int {
		var vs []int
		for _, e := range eventLog(t, lt) {
			if e.Type == nemura.TypeBankDetailsChanged {
				vs = append(vs, e.Version)
			}
		}
		return vs
	}
	rows := queuedRows(t, lt) // the link's snapshot: the people, Ben's version 1
	for _, r := range rows {
		if err := work(ctx, lt.c.org, r); err != nil {
			t.Fatal(err)
		}
	}

	// A repeat: Fyniq already holds version 1 and says it is the same record. Nothing is wrong.
	held.version, held.record = 1, c1
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.RepairLink(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()}, lt.ben)
		return err
	})
	sendLast()
	if got := flags(); len(got) != 0 || blocked() {
		t.Fatalf("a repeated event is not a restore: %+v", got)
	}

	// A second change is version 2 and Fyniq takes it; a late version 1 is then ignored, naming version 2 and the record Nemura has at 2: still nothing wrong.
	held.version = 0
	c2 := confirm()
	sendLast()
	held.version, held.record = 2, c2
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		ok, err := nemura.NoteBankReceipt(ctx, tx, lt.c.org, now, lt.ben, 2, c2)
		if ok {
			t.Error("a record Nemura has at that version flagged the person")
		}
		return err
	})
	if got := flags(); len(got) != 0 {
		t.Fatalf("a late older event is not a restore: %+v", got)
	}

	// The restore: Fyniq holds version 3 for a record this database has never heard of (Nemura was restored from before it). Ben's version 2 is sent again and Fyniq ignores it.
	lost := uuid.New()
	held.version, held.record = 3, lost
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.RepairLink(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()}, lt.ben)
		return err
	})
	sendLast()
	got := flags()
	if len(got) != 1 || got[0].Person != lt.ben || got[0].HeldVersion != 3 || got[0].NemuraVersion != 2 {
		t.Fatalf("Fyniq's answer names a version Nemura lost: %+v", got)
	}
	if !blocked() {
		t.Error("a person whose bank details wait to be re-confirmed can still be paid")
	}
	var findings string
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		fs, err := nemura.LinkFindings(ctx, tx, now)
		for _, f := range fs {
			findings += f.Subject + "|" + f.Message + "\n"
		}
		return err
	})
	if !strings.Contains(findings, lt.ben.String()) || strings.Contains(findings, "123-456") {
		t.Errorf("N.1 #7 names the person by identity and no account: %q", findings)
	}
	// The same answer again flags nothing twice.
	sendLast()
	if len(flags()) != 1 {
		t.Error("the same answer flagged the person twice")
	}

	// The ordinary change releases them, numbered above what Fyniq holds: version 4, not 3.
	c3 := confirm()
	if flags(); len(flags()) != 0 || blocked() {
		t.Errorf("a confirmed change did not release the person: %+v", flags())
	}
	if vs := bankVersions(); vs[len(vs)-1] != 4 {
		t.Errorf("the change confirmed after the restore is numbered above Fyniq's 3: %v", vs)
	}
	held.version = 0
	sendLast()
	lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.RepairLink(ctx, tx, lt.c.org, now, audit.Actor{Type: "user", ID: lt.c.owner.String()}, lt.ben)
		return err
	})
	for _, env := range fy.calls {
		if env.Header.Type == nemura.TypeBankDetailsChanged && env.Header.ObjectVersion == 4 && !strings.Contains(string(env.Payload), c3.String()) {
			t.Errorf("version 4 names another record than the confirmed change: %s", env.Payload)
		}
	}
	_ = link.Envelope{}
}

// A re-confirmation is a fence the database keeps, not only the code: the worker finds one and raises a floor, the API alone ends one, and nobody lowers a floor, deletes a flag, rewrites what it
// found, or ends it with a change that is not the same person's confirmed one numbered above the version Fyniq held (NM-INT-13).
// part of NM-INT-13
// asvs: V8.2.1, V8.3.1, V11.4.1
func TestABankReconfirmationCanOnlyBeEndedByTheChangeThatReconfirms(t *testing.T) {
	now := today.Add(10 * time.Hour)
	cipher, _ := local.NewFieldKeys(bytes.Repeat([]byte{9}, 64))
	lt := newLeaveTeam(t)
	var changes []uuid.UUID
	for i := 0; i < 2; i++ {
		var change uuid.UUID
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			change, err = nemura.ProposeBankChange(ctx, tx, cipher, lt.c.org, now, lt.benLogin, lt.ben, fmt.Sprintf("555-000-%04d", i))
			return err
		})
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.DecideBankChange(ctx, tx, lt.c.org, now, lt.c.owner, []authz.Role{authz.Owner}, change, true, "")
		})
		lt.c.must(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.ConfirmBankChange(ctx, tx, lt.c.org, now, lt.benLogin, change, true)
		})
		changes = append(changes, change)
	}
	ctx := context.Background()
	worker := store(t, "nemura_worker")
	api := store(t, "nemura_api")
	if err := worker.InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
		ok, err := nemura.NoteBankReceipt(ctx, tx, lt.c.org, now, lt.ben, 3, uuid.New())
		if err == nil && !ok {
			t.Error("Fyniq's version 3 was not found to be one Nemura lost")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	pgCode := func(err error) string {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			return pg.Code
		}
		return ""
	}
	refused := func(name string, s *nemura.Store, want, sql string, args ...any) {
		t.Helper()
		err := s.InOrg(ctx, lt.c.org, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
		if pgCode(err) != want {
			t.Errorf("%s: want %s, got %v", name, want, err)
		}
	}
	refused("the API finds a flag itself", api, "42501", `INSERT INTO nemura.bank_reconfirm (person_id, held_version, held_record_id, nemura_version, found_at) VALUES ($1, 9, $2, 1, now())`, lt.ben, uuid.New())
	refused("the worker deletes a flag", worker, "42501", `DELETE FROM nemura.bank_reconfirm`)
	refused("the worker ends a flag", worker, "42501", `UPDATE nemura.bank_reconfirm SET state = 'resolved', resolved_at = now(), resolved_change_id = $1`, changes[1])
	refused("the API rewrites what Fyniq held", api, "42501", `UPDATE nemura.bank_reconfirm SET held_version = 1`)
	refused("the API ends it with a change numbered at or below Fyniq's version", api, "NM195", `UPDATE nemura.bank_reconfirm SET state = 'resolved', resolved_at = now(), resolved_change_id = $1`, changes[1])
	refused("the worker lowers the floor", worker, "NM194", `UPDATE nemura.bank_version_floor SET floor = 1`)
	refused("the API raises the floor", api, "42501", `UPDATE nemura.bank_version_floor SET floor = 99`)
	refused("the worker deletes the floor", worker, "42501", `DELETE FROM nemura.bank_version_floor`)
	refused("a version is set on a change already confirmed", api, "NM126", `UPDATE nemura.bank_account_change SET bank_version = 40 WHERE id = $1`, changes[0])
}
