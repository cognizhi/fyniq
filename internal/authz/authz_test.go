package authz_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/authz/authztest"
)

// req: QA-SEC-03
// asvs: V8.1.1
func TestTheGrantsAreAppendixD(t *testing.T) {
	columns, rows := authztest.AppendixD(t)
	for capability, grants := range authz.Grants {
		row, ok := rows[string(capability)]
		if !ok {
			t.Errorf("Appendix D has no row %q", capability)
			continue
		}
		for _, col := range columns {
			want := authztest.ParseCell(row[col])
			got, ok := grants[col]
			if !ok {
				t.Errorf("%s: the policy table has no %s column (Appendix D: %q)", capability, col, row[col])
			} else if got != want {
				t.Errorf("%s / %s: the policy table says %+v, Appendix D %q (%+v)", capability, col, got, row[col], want)
			}
		}
		for col := range grants {
			if !slices.Contains(columns, col) {
				t.Errorf("%s: the policy table has a column %q that Appendix D does not", capability, col)
			}
		}
	}
}

// req: QA-SEC-03
// asvs: V8.1.1
func TestEveryRoleOfSection71HasItsColumn(t *testing.T) {
	columns, _ := authztest.AppendixD(t)
	labels := authztest.RoleLabels(t)
	seen := map[string]bool{}
	for role, facts := range authz.Roles {
		if !slices.Contains(labels, facts.Label) {
			t.Errorf("role %s: §7.1 has no role %q", role, facts.Label)
		}
		if seen[facts.Label] {
			t.Errorf("two roles are labelled %q", facts.Label)
		}
		seen[facts.Label] = true
		if facts.Column != "" && !slices.Contains(columns, facts.Column) {
			t.Errorf("role %s: Appendix D has no column %q", role, facts.Column)
		}
		if !slices.Contains(authz.Principals(), role) {
			t.Errorf("role %s is not run by the matrix test", role)
		}
	}
	for _, l := range labels {
		if !seen[l] {
			t.Errorf("§7.1 names the role %q, which the policy table does not know", l)
		}
	}
	// The two roles that borrow or lack a column, as the PRD states them.
	prd := authztest.PRD(t)
	for _, says := range []string{
		"The **Company secretary** role (FR-CS-08) has the Board viewer's rights",
		"The Super admin is not an organisation role: reading an organisation's books needs membership in it",
	} {
		if !strings.Contains(prd, says) {
			t.Errorf("the PRD no longer says %q: check authz.Roles", says)
		}
	}
}

// req: SEC-02
// asvs: V8.2.1
func TestDecide(t *testing.T) {
	cases := []struct {
		name  string
		p     authz.Policy
		roles []authz.Role
		ai    bool
		want  bool
	}{
		{"an Accountant posts a journal", authz.PostJournal, []authz.Role{authz.Accountant}, false, true},
		{"a Controller cannot post one (A, not P)", authz.PostJournal, []authz.Role{authz.Controller}, false, false},
		{"the Owner views journals", authz.GetJournal, []authz.Role{authz.Owner}, false, true},
		{"the Owner cannot draft one", authz.CreateJournal, []authz.Role{authz.Owner}, false, false},
		{"a Tax agent drafts one", authz.CreateJournal, []authz.Role{authz.TaxAgent}, false, true},
		{"roles add up", authz.PostJournal, []authz.Role{authz.Owner, authz.Accountant}, false, true},
		{"an Employee cannot see accounts", authz.ListAccounts, []authz.Role{authz.Employee}, false, false},
		{"the AI reads accounts for an Accountant", authz.ListAccounts, []authz.Role{authz.Accountant}, true, true},
		{"the AI never exceeds the user", authz.ListAccounts, []authz.Role{authz.Employee}, true, false},
		{"the AI cannot post for an Accountant (Proposal)", authz.PostJournal, []authz.Role{authz.Accountant}, true, false},
		{"the Super admin is not a member", authz.GetOrganisation, []authz.Role{authz.SuperAdmin}, false, false},
		{"an HR officer has no Fyniq rights", authz.GetOrganisation, []authz.Role{authz.HROfficer}, false, false},
		{"an Employee is a member", authz.GetOrganisation, []authz.Role{authz.Employee}, false, true},
		{"no policy, no access", authz.Policy{}, []authz.Role{authz.Owner}, false, false},
	}
	for _, c := range cases {
		if got := authz.Decide(c.p, c.roles, c.ai); got.Allowed != c.want {
			t.Errorf("%s: allowed %v, want %v (%s)", c.name, got.Allowed, c.want, got.Reason)
		}
	}
}

// QA-SEC-03 includes field masking. No P0 endpoint has an M cell, so a planted capability proves it.
// req: QA-SEC-03
// asvs: V8.2.3
func TestMaskedViewMasksTheFields(t *testing.T) {
	const planted authz.Capability = "Vendor bank details (planted)"
	authz.Grants[planted] = map[string]authz.Cell{"Auditor": {Rights: "M"}, "Accountant": {Rights: "C"}, "Tax agent": {Rights: "M"}}
	defer delete(authz.Grants, planted)
	p := authz.Policy{Operation: "getVendorBankDetails", Capability: planted, Need: authz.View, MaskedFields: []string{"account_number"}}
	if d := authz.Decide(p, []authz.Role{authz.Auditor}, false); !d.Allowed || len(d.Masked) != 1 || d.Masked[0] != "account_number" {
		t.Errorf("an Auditor (M) should see the record masked: %+v", d)
	}
	if d := authz.Decide(p, []authz.Role{authz.Accountant}, false); !d.Allowed || len(d.Masked) != 0 {
		t.Errorf("an Accountant (C) should see it whole: %+v", d)
	}
	if d := authz.Decide(p, []authz.Role{authz.Auditor, authz.Accountant}, false); len(d.Masked) != 0 {
		t.Errorf("the better right wins: %+v", d)
	}
	if d := authz.Decide(p, []authz.Role{authz.Employee}, false); d.Allowed {
		t.Errorf("an Employee (—) should be refused: %+v", d)
	}
	if allowed, masked := authztest.Allows(authztest.ParseCell("M"), authz.View); !allowed || !masked {
		t.Error("the PRD reading of M is a masked view")
	}
}

func TestParseCell(t *testing.T) {
	for in, want := range map[string]authz.Cell{
		"—": {}, "V": {Rights: "V"}, "C/P (limit)": {Rights: "CP", Qualifier: "limit"}, "Proposal": {Qualifier: "Proposal"},
		"A + second approver": {Rights: "A", Qualifier: "+ second approver"}, "V (sales)": {Rights: "V", Qualifier: "sales"},
		"Ask only": {Qualifier: "Ask only"}, "C/A": {Rights: "CA"},
	} {
		if got := authztest.ParseCell(in); got != want {
			t.Errorf("ParseCell(%q) = %+v, want %+v", in, got, want)
		}
	}
}
