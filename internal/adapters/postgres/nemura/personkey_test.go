//go:build local

package nemura_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/erasure"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/ports"
)

// testCipher is a key service for tests that make payslips, whose detail is sealed under the person's own key.
func testCipher(t *testing.T) ports.Cipher {
	t.Helper()
	c, err := local.NewFieldKeys(bytes.Repeat([]byte{3}, 64))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// New payroll detail is sealed under a key of the person's own, which an erasure destroys: the sealed value opens before and never after, it opens nowhere else (another person, another purpose), the
// key cannot be changed, deleted by a role or made again for an erased person, and a restore that brought the key back is undone by the register's re-application (NM-SEC-08).
// part of NM-SEC-08 (the payslip detail; pay run lines are summed in SQL and stay)
// asvs: V8.3.2, V11.4.1, V14.2.3
func TestPayrollDetailSealedUnderAPersonsKeyIsLostWhenTheirKeyIsDestroyed(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	cipher := testCipher(t)
	day := d("2099-03-01")
	at := day.Add(9 * time.Hour)
	var dee, ben uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		job := people.Job{Title: "Analyst", Worksite: "SG", EmploymentType: "full_time"}
		cl := &people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"}
		if dee, err = nemura.AddPerson(ctx, tx, c.org, at, c.owner, people.Person{LegalName: "Dee Ng", EmploymentStart: d("2080-02-02"), LeavingDate: ptr(d("2090-01-31"))}, job, cl); err != nil {
			return err
		}
		ben, err = nemura.AddPerson(ctx, tx, c.org, at, c.owner, people.Person{LegalName: "Ben Lim", EmploymentStart: d("2080-02-02")}, job, cl)
		return err
	})
	detail, _ := json.Marshal(map[string]string{"net": "4300.00"})
	var sealedDee, sealedBen []byte
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if sealedDee, err = nemura.SealPersonDetail(ctx, tx, cipher, c.org, dee, "payslip", detail); err != nil {
			return err
		}
		sealedBen, err = nemura.SealPersonDetail(ctx, tx, cipher, c.org, ben, "payslip", detail)
		return err
	})
	if bytes.Contains(sealedDee, []byte("4300")) {
		t.Fatal("the sealed value holds the figure in the clear")
	}
	open := func(person uuid.UUID, purpose string, sealed []byte) ([]byte, error) {
		var out []byte
		err := c.do(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			out, err = nemura.OpenPersonDetail(ctx, tx, cipher, c.org, person, purpose, sealed)
			return err
		})
		return out, err
	}
	if got, err := open(dee, "payslip", sealedDee); err != nil || !bytes.Equal(got, detail) {
		t.Fatalf("it opens for its own person: %s %v", got, err)
	}
	if _, err := open(dee, "payslip", sealedBen); err == nil {
		t.Error("another person's sealed value opened under Dee's key")
	}
	if _, err := open(ben, "letter", sealedBen); err == nil {
		t.Error("a value sealed for one purpose opened for another")
	}
	// The key is a fence the database keeps.
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
	run(store(t, "nemura_api"), "42501", `DELETE FROM nemura.person_key`)
	run(store(t, "nemura_worker"), "42501", `DELETE FROM nemura.person_key`)
	run(store(t, "nemura_api"), "42501", `UPDATE nemura.person_key SET wrapped = '\x00'::bytea`)
	run(store(t, "nemura_worker"), "42501", `INSERT INTO nemura.person_key (person_id, wrapped) VALUES ($1, '\x00'::bytea)`, dee)

	// The register's entry for Dee, as a restored copy would find it: Dee has left, her key is back, her data is back. Applying it again destroys the key.
	blobs, _ := local.NewDir(t.TempDir())
	signer, _ := local.NewEd25519(bytes.Repeat([]byte{5}, 32))
	entry := erasure.Entry{Schema: erasure.Schema, Organisation: c.org, Person: dee, Order: uuid.New(), ErasedOn: "2099-03-01", EligibleOn: "2091-01-31", RequestedBy: c.hr, ApprovedBy: c.owner}
	msg, _ := entry.Canonical()
	sig, _ := signer.Sign(ctx, msg)
	raw, _ := json.Marshal(erasure.Signed{Entry: entry, KeyID: signer.KeyID(), Sig: sig})
	if err := blobs.Create(ctx, erasure.Key(c.org, dee), bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	runner := &nemura.ErasureRunner{Store: store(t, "nemura_worker"), Blobs: blobs, Signer: signer, Clock: clock.NewFrozen(at)}
	if n, err := runner.Reapply(ctx, c.org); err != nil || n != 1 {
		t.Fatalf("the register applied again: %d %v", n, err)
	}
	if _, err := open(dee, "payslip", sealedDee); !errors.Is(err, nemura.ErrErased) {
		t.Errorf("the sealed detail of an erased person: %v", err)
	}
	if got, err := open(ben, "payslip", sealedBen); err != nil || !bytes.Equal(got, detail) {
		t.Errorf("another person's detail was touched: %s %v", got, err)
	}
	// No key is made again for an erased person.
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.SealPersonDetail(ctx, tx, cipher, c.org, dee, "payslip", detail)
		return err
	}); pgCode(err) != "NM205" {
		t.Errorf("a key for an erased person: %v", err)
	}
}
