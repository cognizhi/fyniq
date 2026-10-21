//go:build local

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// refusedAs says whether err is the storage refusal with code.
func refusedAs(err error, code string) bool {
	var se *storage.Error
	return errors.As(err, &se) && se.Code == code
}

// FR-ORG-10 (the founders' decision of rev 0.62): a framework or policy version never changes or goes; its confirmation
// or approval is recorded once, and never by whoever wrote it, whatever the API does. Each change is audited.
// req: FR-ORG-10
func TestAccountingVersionsNeverChangeAndTheirWriterNeverSignsThem(t *testing.T) {
	writer := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "policy-writer"}})
	checker := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	b := books(t, "Accounting policies Pte Ltd")
	s := store(t, "fyniq_api")
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var f storage.FrameworkVersion
	var p storage.PolicyVersion
	if err := s.Do(writer, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if f, err = u.AccountingBasis().RecordFramework(ctx, from, "sfrs_se", "Third Edition"); err != nil {
			return err
		}
		p, err = u.AccountingBasis().RecordPolicy(ctx, "leases", from, "No leases.")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if f.Version != 1 || f.WrittenBy != "user:policy-writer" || p.Version != 1 || p.Area != "leases" {
		t.Fatalf("recorded: %+v %+v", f, p)
	}
	// Its writer is refused by the database itself; another member signs it, once.
	if err := s.Do(writer, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AccountingBasis().ApprovePolicy(ctx, "leases", 1)
		return err
	}); !refusedAs(err, storage.CodeAccounting) {
		t.Fatalf("the writer approves: %v", err)
	}
	if err := s.Do(checker, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.AccountingBasis().ConfirmFramework(ctx, 1); err != nil {
			return err
		}
		_, err := u.AccountingBasis().ApprovePolicy(ctx, "leases", 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(checker, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AccountingBasis().ApprovePolicy(ctx, "leases", 1)
		return err
	}); !refusedAs(err, storage.CodeAccounting) {
		t.Fatalf("approved twice: %v", err)
	}
	if err := store(t, "fyniq_readonly").Do(writer, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		if err != nil {
			return err
		}
		var trail strings.Builder
		for _, ev := range events {
			trail.WriteString(ev.Body + "\n")
		}
		for _, want := range []string{`"action":"accounting_framework.recorded"`, `"action":"accounting_framework.confirmed"`,
			`"action":"accounting_policy.recorded"`, `"action":"accounting_policy.approved"`} {
			if !strings.Contains(trail.String(), want) {
				t.Errorf("the audit trail lacks %s", want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.Do(writer, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AccountingBasis().RecordPolicy(ctx, "impairment", from, "Receivables are reviewed at each year end.")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ role, stmt, state string }{
		// The API's role changes nothing but the signature.
		{"fyniq_api", "UPDATE fyniq.accounting_policy SET wording = 'x' WHERE area = 'leases'", "42501"},
		{"fyniq_api", "UPDATE fyniq.accounting_framework SET edition = 'x'", "42501"},
		{"fyniq_api", "DELETE FROM fyniq.accounting_policy", "42501"},
		// A signature is never withdrawn or replaced.
		{"fyniq_api", "UPDATE fyniq.accounting_policy SET approved_by = NULL, approved_at = NULL WHERE area = 'leases'", "FY160"},
		{"fyniq_api", "UPDATE fyniq.accounting_framework SET confirmed_by = 'user:someone', confirmed_at = now()", "FY160"},
		// Not even the owner of the tables rewrites a version.
		{"fyniq_migrator", "UPDATE fyniq.accounting_policy SET wording = 'x' WHERE area = 'leases'", "FY160"},
		{"fyniq_migrator", "DELETE FROM fyniq.accounting_framework", "FY160"},
		// Its writer never signs it.
		{"fyniq_api", "UPDATE fyniq.accounting_policy SET approved_by = 'user:policy-writer', approved_at = now() WHERE area = 'impairment'", "FY161"},
		// An area or a framework Fyniq does not know is refused.
		{"fyniq_api", "INSERT INTO fyniq.accounting_policy (area, version, from_on, wording, written_by) VALUES ('goodwill', 1, '2026-01-01', 'x', 'user:w')", "23514"},
		{"fyniq_api", "INSERT INTO fyniq.accounting_framework (version, from_on, framework, edition, written_by) VALUES (9, '2026-01-01', 'ifrs', 'x', 'user:w')", "23514"},
	} {
		err := asOrg(writer, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(writer, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}
