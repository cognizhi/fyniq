package authz_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/authz/authztest"
)

// SOD-6 is derived from Appendix D: the Administrator (C on "Users, roles, team mode") with any role that posts,
// approves or releases transactions; the Owner's own A on users is dual-controlled and not counted.
// req: SOD-6
// req: access/roles-and-segregation Finance roles and role combinations
// asvs: V8.2.1
func TestRoleCombinationsThatConflict(t *testing.T) {
	var admin, trans []authz.Role
	for _, r := range authz.Principals() {
		if authz.Administers(r) {
			admin = append(admin, r)
		}
		if authz.Transacts(r) {
			trans = append(trans, r)
		}
	}
	if !slices.Equal(admin, []authz.Role{authz.Administrator}) {
		t.Errorf("roles that administer users or roles: %v, want only the Administrator", admin)
	}
	wantTrans := []authz.Role{authz.Approver, authz.Bookkeeper, authz.ARSpecialist, authz.PaymentReleaser, authz.Accountant,
		authz.Controller, authz.Owner}
	if !slices.Equal(trans, wantTrans) {
		t.Errorf("roles that post, approve or release transactions: %v, want %v", trans, wantTrans)
	}
	for _, c := range []struct {
		roles []authz.Role
		want  bool
	}{
		{[]authz.Role{authz.Administrator}, false},
		{[]authz.Role{authz.Owner}, false}, // both sides, but every Owner action on users is dual-controlled
		{[]authz.Role{authz.Owner, authz.Controller}, false},
		{[]authz.Role{authz.Administrator, authz.Controller}, true},
		{[]authz.Role{authz.Administrator, authz.Owner}, true},
		{[]authz.Role{authz.Owner, authz.Controller, authz.Administrator}, true}, // the founders' usual roles
		{[]authz.Role{authz.Administrator, authz.Accountant}, true},
		{[]authz.Role{authz.Administrator, authz.Bookkeeper}, true},
		{[]authz.Role{authz.Administrator, authz.PaymentReleaser}, true},
		{[]authz.Role{authz.Administrator, authz.Approver}, true},
		{[]authz.Role{authz.Administrator, authz.APSpecialist}, false}, // prepares, never posts or approves
		{[]authz.Role{authz.Administrator, authz.PayrollAdmin}, false},
		{[]authz.Role{authz.Administrator, authz.Auditor, authz.TaxAgent, authz.DPO}, false},
		{[]authz.Role{authz.Accountant, authz.Controller, authz.PaymentReleaser}, false}, // per-item conflicts, checked when acting
	} {
		got := authz.RoleConflicts(c.roles)
		if (len(got) > 0) != c.want {
			t.Errorf("%v: conflicts %v, want a conflict %v", c.roles, got, c.want)
		}
		if c.want && (got[0].SoD != authz.SoD6 || !slices.Contains(got[0].Roles, authz.Administrator)) {
			t.Errorf("%v: %+v names the wrong conflict or roles", c.roles, got[0])
		}
	}
	// The PRD still says what the derivation reads.
	prd := authztest.PRD(t)
	for _, says := range []string{
		"| SOD-6 | Administer users or roles **and** post or approve transactions | Privilege abuse |",
		"| **Administrator** | Users, roles, team mode, approval policies, integrations, settings | Post or approve financial transactions |",
		"This is allowed only because every Owner action on users, roles,\napproval policies and bank details is dual-controlled",
	} {
		if !strings.Contains(prd, says) {
			t.Errorf("the PRD no longer says %q: check authz.RoleConflicts", says)
		}
	}
	for _, row := range authz.TransactionRows {
		if _, ok := authz.Grants[row]; !ok {
			t.Errorf("transaction row %q is not in Appendix D", row)
		}
	}
}
