//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Department scopes, the database's half (FR-ADM-01, rev 0.253; the owner's decisions of 12 and 20 Nov 2026): a person who holds
// the Approver role and no role besides Employee reaches only the journal lines and balances whose dimension set holds one of
// their scope's values, and with no scope none; any other role leaves the person unrestricted here (the API's policy table
// decides those); the restriction ANDs with the tenant policy and never widens it; it binds the API's role alone, so the
// worker, the executor and the read-only role stay unfiltered; a scope is fixed when the role is given and names department
// values of a live Approver assignment only.
// req: FR-ADM-01
// req: access/roles-and-segregation Permission model
// asvs: V8.2.1, V8.3.1, V8.2.2
func TestAttackADepartmentScopeBindsWhatTheAPIReads(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := porttest.NewBooks(t, s, "Department scope Pte Ltd")
	elsewhere := porttest.NewBooks(t, s, "Department scope elsewhere Pte Ltd")
	now := clock.System{}.Now()
	svc := journal.Service{Store: s}
	var dept, proj storage.Dimension
	var eng, ops, atlas storage.DimensionValue
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if dept, err = u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Department"}); err != nil {
			return err
		}
		if proj, err = u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionProject, Name: "Project"}); err != nil {
			return err
		}
		if eng, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "ENG", Name: "Engineering"}); err != nil {
			return err
		}
		if ops, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept.ID, Code: "OPS", Name: "Operations"}); err != nil {
			return err
		}
		atlas, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: proj.ID, Code: "ATLAS", Name: "Atlas"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	post := func(amt string, values ...uuid.UUID) {
		t.Helper()
		d := saleDraft(t, b, porttest.Day(10, 14), amt)
		d.Lines[1].Values = values
		doc, err := svc.Create(ctx, b.Org, d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
	}
	post("100.00", eng.ID, atlas.ID) // Engineering and the Atlas project
	post("40.00", ops.ID)            // Operations
	post("5.00")                     // no dimension: in no department
	// Another organisation's books, which no scope here may ever reach.
	d := saleDraft(t, elsewhere, porttest.Day(10, 14), "9.00")
	other, err := svc.Create(ctx, elsewhere.Org, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Post(ctx, elsewhere.Org, other.ID); err != nil {
		t.Fatal(err)
	}

	approver := joined(t, b.Org, 81, []string{"approver"}, now)
	both := joined(t, b.Org, 82, []string{"approver", "accountant"}, now)
	employee := joined(t, b.Org, 83, []string{"approver", "employee"}, now)

	// reads counts the sales lines and balance rows a role sees in the books, as the person (nil: no person named).
	reads := func(role string, person *uuid.UUID) (lines, balances int) {
		t.Helper()
		err := asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error {
			if person != nil {
				if _, err := tx.Exec(ctx, "SELECT set_config('app.user_id', $1, true), set_config('app.dept_reads', 'on', true)", person.String()); err != nil {
					return err
				}
			}
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM fyniq.journal_line WHERE account_id = $1", b.Sales.ID).Scan(&lines); err != nil {
				return err
			}
			return tx.QueryRow(ctx, "SELECT count(*) FROM fyniq.balance WHERE account_id = $1", b.Sales.ID).Scan(&balances)
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	check := func(what, role string, person *uuid.UUID, wantLines, wantBalances int) {
		t.Helper()
		if l, bal := reads(role, person); l != wantLines || bal != wantBalances {
			t.Errorf("%s: %d lines and %d balance rows, want %d and %d", what, l, bal, wantLines, wantBalances)
		}
	}
	// A request that names no person reads all three journals' lines; the tenant policy keeps the other organisation out.
	check("no person named", "fyniq_api", nil, 3, 3)
	// Restricted with no scope: no scope reaches none.
	check("an Approver with no scope", "fyniq_api", &approver, 0, 0)
	check("an Approver and Employee with no scope", "fyniq_api", &employee, 0, 0)
	// Any other role leaves the person unrestricted here.
	check("an Approver who is also an Accountant", "fyniq_api", &both, 3, 3)
	// A write, an approval or a posting is no scoped read: the engine runs on the whole books, so a trigger or an approval's
	// envelope never reads a filtered cache. Only a read binds the scope.
	if err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.user_id', $1, true), set_config('app.dept_reads', 'off', true)", approver.String()); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM fyniq.journal_line WHERE account_id = $1", b.Sales.ID).Scan(&n); err != nil {
			return err
		}
		if n != 3 {
			t.Errorf("a request that is no read saw %d lines, want all 3", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The worker, the executor and the read-only role stay unfiltered even when a person is named.
	for _, role := range []string{"fyniq_worker", "fyniq_executor", "fyniq_readonly"} {
		check(role+" names a person and is not filtered", role, &approver, 3, 3)
	}
	// Someone who is no member of this organisation is no department's person: the tenant policy alone decides, and it holds.
	stranger := uuid.New()
	check("a stranger to the organisation", "fyniq_api", &stranger, 3, 3)

	// Give the Approver's role a scope: Engineering. The line tagged Engineering and Atlas is theirs; Operations' and the
	// undimensioned one are not.
	assignment := func(user uuid.UUID, role string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := asOrg(ctx, conn(t, "fyniq_readonly"), b.Org, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT ra.id FROM fyniq.role_assignment ra JOIN fyniq.user_membership m ON m.organisation_id = ra.organisation_id AND m.id = ra.membership_id
WHERE m.user_id = $1 AND ra.role = $2 AND ra.revoked_at IS NULL`, user, role).Scan(&id)
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	narrow := func(assignment uuid.UUID, values ...uuid.UUID) error {
		return s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Members().Narrow(ctx, assignment, values)
		})
	}
	a1 := assignment(approver, "approver")
	if err := narrow(a1, eng.ID); err != nil {
		t.Fatal(err)
	}
	// The Engineering line (with the Atlas project beside it) is theirs: its balance row too. Operations' and the
	// undimensioned lines are not.
	check("an Approver scoped to Engineering", "fyniq_api", &approver, 1, 1)
	// The other Approver, who has no scope, still reaches nothing; and the Employee beside Approver is restricted the same.
	check("an Approver with no scope still", "fyniq_api", &employee, 0, 0)
	if err := narrow(assignment(employee, "approver"), eng.ID, ops.ID); err != nil {
		t.Fatal(err)
	}
	check("an Approver and Employee scoped to two departments", "fyniq_api", &employee, 2, 2)
	// A scope names department values of a live Approver assignment, once, and never changes.
	for _, x := range []struct {
		what string
		err  error
	}{
		{"another dimension's value", narrow(assignment(both, "approver"), atlas.ID)},
		{"another role", narrow(assignment(both, "accountant"), eng.ID)},
		{"a value that is no value", narrow(assignment(both, "approver"), uuid.New())},
	} {
		if x.err == nil {
			t.Errorf("a scope was saved for %s", x.what)
		}
	}
	for _, x := range []struct{ role, what, stmt, state string }{
		{"fyniq_migrator", "change a scope", "UPDATE fyniq.role_assignment_scope SET created_at = now()", "FY700"},
		{"fyniq_migrator", "delete a scope", "DELETE FROM fyniq.role_assignment_scope", "FY700"},
		{"fyniq_api", "change a scope as the API", "UPDATE fyniq.role_assignment_scope SET created_at = now()", "42501"},
		{"fyniq_api", "delete a scope as the API", "DELETE FROM fyniq.role_assignment_scope", "42501"},
		{"fyniq_worker", "give a scope as the worker", "INSERT INTO fyniq.role_assignment_scope (assignment_id, dimension_value_id) SELECT assignment_id, dimension_value_id FROM fyniq.role_assignment_scope LIMIT 1", "42501"},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.state)
	}
	// A scope inserted without its audit event is refused at commit.
	bare := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO fyniq.role_assignment_scope (assignment_id, dimension_value_id) VALUES ($1, $2)", assignment(both, "approver"), ops.ID)
		return err
	})
	if bare == nil {
		t.Error("a scope was saved without its audit event")
	}
	// The scope is another organisation's nothing: the policy for that organisation's rows holds.
	if n := count(t, elsewhere.Org, "role_assignment_scope"); n != 0 {
		t.Errorf("another organisation holds %d scopes", n)
	}
	_ = proj
}
