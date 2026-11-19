//go:build local

package nemura_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/roles"
)

var (
	today = time.Date(2099, 3, 1, 0, 0, 0, 0, time.UTC)
	later = today.AddDate(0, 1, 0)
)

type company struct {
	t     *testing.T
	org   uuid.UUID
	owner uuid.UUID
	hr    uuid.UUID // an HR administrator
	pay   uuid.UUID // someone who would be Payroll administrator
}

func (c *company) do(fn func(ctx context.Context, tx pgx.Tx) error) error {
	return store(c.t, "nemura_api").InOrg(context.Background(), c.org, fn)
}

func (c *company) must(fn func(ctx context.Context, tx pgx.Tx) error) {
	c.t.Helper()
	if err := c.do(fn); err != nil {
		c.t.Fatal(err)
	}
}

// newCompany is an organisation with its first Owner (made by setup), an HR administrator the Owner appointed, and a third member.
func newCompany(t *testing.T) *company {
	t.Helper()
	c := &company{t: t, org: uuid.New()}
	createOrg(t, c.org, "Roles "+c.org.String()[:8]+" Pte Ltd")
	now := today.Add(9 * time.Hour)
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if c.owner, err = nemura.AddMember(ctx, tx, c.org, now, actor, "Owen Owner"); err != nil {
			return err
		}
		if c.hr, err = nemura.AddMember(ctx, tx, c.org, now, actor, "Hana HR"); err != nil {
			return err
		}
		if c.pay, err = nemura.AddMember(ctx, tx, c.org, now, actor, "Pia Payroll"); err != nil {
			return err
		}
		if err := nemura.SetupOwner(ctx, tx, c.org, now, c.owner, today); err != nil {
			return err
		}
		_, err = nemura.GrantRole(ctx, tx, c.org, now, c.owner, c.hr, authz.HRAdmin, today, nil)
		return err
	})
	return c
}

func code(err error) string {
	var r *roles.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// An HR administrator who configures roles cannot give anyone, themselves included, pay or signing rights: a role is never one's
// own, a privileged role needs an Owner's approval first, and the approval is the Owner's, not the asker's.
// req: NM-ROLE-05
// asvs: V8.2.1, V8.3.1
func TestNobodyEscalatesTheirOwnRights(t *testing.T) {
	c := newCompany(t)
	now := today.Add(10 * time.Hour)
	// The HR administrator cannot give themselves any role.
	err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.GrantRole(ctx, tx, c.org, now, c.hr, c.hr, authz.PayrollAdmin, today, nil)
		return err
	})
	if code(err) != roles.CodeApprovalSelf {
		t.Errorf("an HR administrator granting themselves a role: %v", err)
	}
	// Nor grant Payroll administrator to someone else without an approval.
	err = c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.GrantRole(ctx, tx, c.org, now, c.hr, c.pay, authz.PayrollAdmin, today, nil)
		return err
	})
	if code(err) != roles.CodeApprovalRequired {
		t.Errorf("a privileged grant with no approval: %v", err)
	}
	// Even straight to the database, whoever asks: a privileged grant with no approval is refused (NM080), and so is one resting on
	// another member's approval or on a decision by someone who is not an Owner.
	raw := func(approval any) error {
		return c.do(func(ctx context.Context, tx pgx.Tx) error {
			id := uuid.New()
			if _, err := tx.Exec(ctx, `INSERT INTO nemura.role_assignment (id, member_id, role, effective_from, granted_by, approval_id) VALUES ($1, $2, 'payroll_admin', $3, $4, $5)`,
				id, c.pay, today.Format(time.DateOnly), c.hr, approval); err != nil {
				return err
			}
			return nemura.Audit(ctx, tx, c.org, now, actor, "nemura.role.granted", audit.Object{Type: "role_assignment", ID: id}, nil, "")
		})
	}
	if err := raw(nil); err == nil || !strings.Contains(err.Error(), "NM080") {
		t.Errorf("raw grant without an approval: %v", err)
	}
	// The HR administrator asks; an Owner decides; then the grant stands.
	var req uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		req, err = nemura.RequestRoleGrant(ctx, tx, c.org, now, c.hr, c.pay, authz.PayrollAdmin, today, later)
		return err
	})
	// A request that is still pending is no approval.
	if err := raw(req); err == nil || !strings.Contains(err.Error(), "NM080") {
		t.Errorf("a grant resting on a pending request: %v", err)
	}
	// The one who asked cannot decide it, nor the one who would receive the role, nor anyone who is not an Owner.
	for name, who := range map[string]uuid.UUID{"the requester": c.hr, "the recipient": c.pay} {
		err := c.do(func(ctx context.Context, tx pgx.Tx) error {
			return nemura.DecideRoleGrant(ctx, tx, c.org, now, who, req, true, "")
		})
		if code(err) != roles.CodeApprovalSelf && code(err) != roles.CodeApprovalNotOwner {
			t.Errorf("%s deciding: %v", name, err)
		}
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideRoleGrant(ctx, tx, c.org, now, c.owner, req, true, "")
	})
	var granted uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		granted, err = nemura.GrantRole(ctx, tx, c.org, now, c.hr, c.pay, authz.PayrollAdmin, today, &req)
		return err
	})
	if granted == uuid.Nil {
		t.Fatal("no grant")
	}
	// An approval is for one grant: it does not carry another member's.
	other := uuid.Nil
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		other, err = nemura.AddMember(ctx, tx, c.org, now, actor, "Otto Other")
		return err
	})
	err = c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.GrantRole(ctx, tx, c.org, now, c.hr, other, authz.PayrollAdmin, today, &req)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "NM080") {
		t.Errorf("one approval used for another member: %v", err)
	}
	// A decision is final: it cannot be decided again or edited.
	err = c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE nemura.approval_request SET state = 'refused', reason = 'changed my mind' WHERE id = $1", req)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "NM082") {
		t.Errorf("changing a decided request: %v", err)
	}
}

// Nobody approves their own request: the Owner who asks cannot approve it either, so the Owner role itself needs a second Owner,
// and only first-run setup makes the first Owner (once).
// req: NM-ROLE-05
// asvs: V8.2.1
func TestNobodyApprovesTheirOwnRequestAndSetupRunsOnce(t *testing.T) {
	c := newCompany(t)
	now := today.Add(11 * time.Hour)
	var req uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		req, err = nemura.RequestRoleGrant(ctx, tx, c.org, now, c.owner, c.pay, authz.Owner, today, later)
		return err
	})
	err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideRoleGrant(ctx, tx, c.org, now, c.owner, req, true, "")
	})
	if code(err) != roles.CodeApprovalSelf {
		t.Errorf("an Owner approving their own request: %v", err)
	}
	// The database says the same, below the code.
	super := connect(t, db.SuperURL())
	if _, err := super.Exec(context.Background(), "UPDATE nemura.approval_request SET state = 'approved', decided_by = requested_by, decided_at = now() WHERE id = $1", req); err == nil ||
		!strings.Contains(err.Error(), "approval_request_not_self") {
		t.Errorf("the database let a request be approved by its requester: %v", err)
	}
	// A refusal says why.
	err = c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE nemura.approval_request SET state = 'refused', decided_by = $2, decided_at = now() WHERE id = $1", req, c.hr)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "approval_request_refusal_reason") {
		t.Errorf("a refusal with no reason: %v", err)
	}
	// Setup makes the first Owner only once.
	err = c.do(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.SetupOwner(ctx, tx, c.org, now, c.pay, today)
	})
	if err == nil || !strings.Contains(err.Error(), "NM080") {
		t.Errorf("a second first-run Owner: %v", err)
	}
	// Administrators manage roles but not pay or signing: the matrix lets them ask, never decide.
	if _, err := roles.CheckGrant([]authz.Role{authz.Administrator}, c.hr, c.pay, authz.PayrollAdmin); err != nil {
		t.Errorf("an Administrator asking for a privileged role: %v", err)
	}
	if err := roles.CheckDecision([]authz.Role{authz.Administrator}, c.hr, c.owner, c.pay); code(err) != roles.CodeApprovalNotOwner {
		t.Errorf("an Administrator deciding: %v", err)
	}
	if _, err := roles.CheckGrant([]authz.Role{authz.Employee}, c.pay, c.hr, authz.Employee); code(err) != roles.CodeRoleRequired {
		t.Errorf("an employee granting a role: %v", err)
	}
}

// Roles are effective-dated and kept as history: a grant counts from its day, an ended one stops, a grant can only be ended (once)
// and never changed or deleted, and every change is audited (NM-ROLE-01, NM-ROLE-05).
// asvs: V8.2.2
func TestRolesAreEffectiveDatedAndHistoryIsKept(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	now := today.Add(12 * time.Hour)
	var assignment uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		assignment, err = nemura.GrantRole(ctx, tx, c.org, now, c.owner, c.pay, authz.LineManager, later, nil)
		return err
	})
	rolesOn := func(on time.Time) []authz.Role {
		var out []authz.Role
		c.must(func(ctx context.Context, tx pgx.Tx) error {
			var err error
			out, err = nemura.ActiveRoles(ctx, tx, c.pay, on)
			return err
		})
		return out
	}
	if got := rolesOn(today); len(got) != 0 {
		t.Errorf("before its day the grant holds: %v", got)
	}
	if got := rolesOn(later); len(got) != 1 || got[0] != authz.LineManager {
		t.Errorf("on its day: %v", got)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.EndRole(ctx, tx, c.org, now, c.owner, assignment, later.AddDate(0, 1, 0))
	})
	if got := rolesOn(later.AddDate(0, 1, 0)); len(got) != 0 {
		t.Errorf("on its end date the grant still holds: %v", got)
	}
	if got := rolesOn(later.AddDate(0, 0, 3)); len(got) != 1 {
		t.Errorf("before its end date: %v", got)
	}
	// Ended once; history is not edited or deleted.
	super := connect(t, db.SuperURL())
	for _, sql := range []string{
		"UPDATE nemura.role_assignment SET effective_to = effective_to + 1 WHERE id = '" + assignment.String() + "'",
		"UPDATE nemura.role_assignment SET role = 'owner' WHERE id = '" + assignment.String() + "'",
		"DELETE FROM nemura.role_assignment WHERE id = '" + assignment.String() + "'",
		"DELETE FROM nemura.member WHERE id = '" + c.pay.String() + "'",
	} {
		if _, err := super.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "NM081") {
			t.Errorf("%s: %v", sql, err)
		}
	}
	// Every change is audited: setup, the grants and the revocation, and the chain holds.
	var n int
	if err := super.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE organisation_id = $1 AND action IN ('nemura.role.granted', 'nemura.role.revoked', 'nemura.member.joined')`, c.org).Scan(&n); err != nil || n != 7 {
		t.Errorf("audit events for members and roles: %d, %v", n, err)
	}
	if _, err := store(t, "nemura_worker").VerifyAudit(ctx, c.org); err != nil {
		t.Errorf("the chain: %v", err)
	}
	_ = clock.System{}
}

// Roles are Nemura's own, assigned per organisation: the database accepts exactly the roles of Appendix B (the AI is a
// principal that acts as the asking person, never a role anyone holds), role assignments sit under the organisation's row-level
// security, and one organisation cannot see or use another's.
// req: NM-ROLE-01
// asvs: V8.2.2
func TestTheDatabaseKnowsExactlyNemurasRoles(t *testing.T) {
	ctx := context.Background()
	var def string
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'role_assignment_role_known'").Scan(&def); err != nil {
		t.Fatal(err)
	}
	for _, r := range authz.Roles {
		has := strings.Contains(def, "'"+string(r)+"'")
		if r == authz.AI && has {
			t.Errorf("the AI is a role in the database")
		}
		if r != authz.AI && !has {
			t.Errorf("the database does not know the %s role: %s", r, def)
		}
	}
	a, b := newCompany(t), newCompany(t)
	var n int
	if err := store(t, "nemura_api").InOrg(ctx, b.org, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM nemura.role_assignment WHERE member_id = $1", a.owner).Scan(&n)
	}); err != nil || n != 0 {
		t.Errorf("another organisation sees %d of this one's role assignments, %v", n, err)
	}
	// Another organisation's member cannot hold a role here: the foreign keys are composite.
	err := a.do(func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.New()
		if _, err := tx.Exec(ctx, "INSERT INTO nemura.role_assignment (id, member_id, role, effective_from, granted_by) VALUES ($1, $2, 'employee', $3, $4)",
			id, b.hr, today.Format(time.DateOnly), a.owner); err != nil {
			return err
		}
		return nemura.Audit(ctx, tx, a.org, today, actor, "nemura.role.granted", audit.Object{Type: "role_assignment", ID: id}, nil, "")
	})
	if err == nil {
		t.Error("a member of another organisation was given a role")
	}
}
