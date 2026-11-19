//go:build local

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A payroll entry has a receipt or the database refuses it (FR-PPL-09, golden rule 2): an entry whose source is a pay run, posted without the receipt of its
// verification from the same transaction, fails at commit, whatever code posts it; with its receipt it posts. A receipt is kept once per envelope and per run
// version, by two different people, and the mapping and the receipts are append-only and the organisation's own.
// asvs: V8.2.1, V4.2.1, V2.3.4
func TestAttackAPayrollEntryNeedsItsReceipt(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Payroll receipts Pte Ltd")
	other := books(t, "Payroll receipts elsewhere Pte Ltd")
	now := time.Date(2026, time.November, 2, 3, 0, 0, 0, time.UTC)
	owner := founder(t, store(t, "fyniq_api"), b.Org, 93, now)
	_ = owner
	run := uuid.New()
	entryOf := func(identity string, version int) storage.NewEntry {
		e := porttest.Sale(identity, porttest.Day(10, 31), b, "100.00")
		e.Source = storage.Source{Type: "payroll_run", ID: run.String(), Version: version}
		return e
	}
	var link storage.PeopleLinkRow
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		link, err = u.PeopleLinks().Link(ctx, storage.PeopleLinkRow{NemuraOrganisationID: uuid.New(), LinkKey: make([]byte, 32), ApprovalID: uuid.New(), CreatedBy: "user:x", CreatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	receipt := func(version int, sha string, approver, preparer string, entry uuid.UUID) storage.PayrollRunReceipt {
		return storage.PayrollRunReceipt{LinkID: link.ID, Epoch: 1, SourceSystem: "nemura", RunID: run, Version: version, WageMonth: porttest.Day(10, 1), EnvelopeSHA256: sha,
			ApproverIssuer: "urn:test", ApproverSubject: approver, PreparerIssuer: "urn:test", PreparerSubject: preparer, MappingVersion: 1, EntryID: entry, Totals: map[string]string{}, ReceivedAt: now}
	}
	sha := func(c string) string { return strings.Repeat("0", 63) + c }
	post := func(identity string, version int, withReceipt bool, approver, preparer, digest string) error {
		return store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			e, err := u.Ledger().Append(ctx, entryOf(identity, version))
			if err != nil {
				return err
			}
			if !withReceipt {
				return nil
			}
			_, err = u.PayrollRuns().Receive(ctx, receipt(version, digest, approver, preparer, e.ID))
			return err
		})
	}
	var se *storage.Error
	if err := post("payroll-a", 1, false, "", "", ""); !errors.As(err, &se) || se.Code != storage.CodePayrollRun {
		t.Fatalf("a payroll entry with no receipt: %v", err)
	}
	if n := count(t, b.Org, "journal_entry"); n != 0 {
		t.Errorf("the refused entry stayed: %d", n)
	}
	if err := post("payroll-b", 1, true, "wei-jie", "mei-ling", sha("1")); err != nil {
		t.Fatalf("an entry with its receipt: %v", err)
	}
	if err := post("payroll-c", 2, true, "wei-jie", "wei-jie", sha("2")); err == nil {
		t.Error("a run prepared and approved by the same person")
	}
	if err := post("payroll-d", 2, true, "wei-jie", "mei-ling", sha("1")); err == nil {
		t.Error("an approval envelope used twice")
	}
	if err := post("payroll-e", 1, true, "wei-jie", "mei-ling", sha("3")); err == nil {
		t.Error("a version received twice")
	}
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if used, err := u.PayrollRuns().EnvelopeUsed(ctx, sha("1")); err != nil || !used {
			t.Errorf("an envelope used: %v %v", used, err)
		}
		m, err := u.PayrollRuns().SetMapping(ctx, map[string]uuid.UUID{"deduction:advance_recovery": b.Bank.ID}, "user:x", now)
		if err != nil || m.Version != 1 {
			t.Errorf("the first mapping: %+v %v", m, err)
		}
		m, err = u.PayrollRuns().SetMapping(ctx, map[string]uuid.UUID{"deduction:advance_recovery": b.Bank.ID, "department:" + uuid.NewString(): b.Rent.ID}, "user:x", now)
		if err != nil || m.Version != 2 {
			t.Errorf("the next mapping: %+v %v", m, err)
		}
		cur, err := u.PayrollRuns().Mapping(ctx)
		if err != nil || cur.Version != 2 || len(cur.Entries) != 2 {
			t.Errorf("the current mapping: %+v %v", cur, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store(t, "fyniq_api").Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if used, _ := u.PayrollRuns().EnvelopeUsed(ctx, sha("1")); used {
			t.Error("another organisation sees an envelope")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A slot only points at the accounts it may: a department at an expense account, a deduction at an asset or liability that is not money.
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PayrollRuns().SetMapping(ctx, map[string]uuid.UUID{"department:" + uuid.NewString(): b.Bank.ID}, "user:x", now)
		return err
	}); err == nil {
		t.Error("a department's salary mapped to a bank account")
	}
	if err := store(t, "fyniq_api").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PayrollRuns().SetMapping(ctx, map[string]uuid.UUID{"deduction:advance_recovery": b.Rent.ID}, "user:x", now)
		return err
	}); err == nil {
		t.Error("a deduction recovered through an expense account")
	}
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "change a receipt", "UPDATE fyniq.payroll_run_receipt SET version = 9", "FY189"},
		{"fyniq_migrator", "delete a receipt", "DELETE FROM fyniq.payroll_run_receipt", "FY189"},
		{"fyniq_migrator", "change a mapping", "UPDATE fyniq.payroll_mapping SET version = 9", "FY189"},
		{"fyniq_migrator", "delete a mapping entry", "DELETE FROM fyniq.payroll_mapping_entry", "FY189"},
		{"fyniq_api", "delete a receipt as the API", "DELETE FROM fyniq.payroll_run_receipt", "42501"},
		{"fyniq_worker", "write a mapping as the worker", "INSERT INTO fyniq.payroll_mapping (version, set_by, set_at) VALUES (9, 'user:x', now())", "42501"},
		{"fyniq_api", "map a department to a nonsense key", "INSERT INTO fyniq.payroll_mapping_entry (mapping_id, key, account_id) SELECT m.id, 'everyone', a.id FROM fyniq.payroll_mapping m, fyniq.account a LIMIT 1", "23514"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.state)
	}
}
