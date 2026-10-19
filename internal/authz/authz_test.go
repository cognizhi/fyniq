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

// Only the installation's operations (setup, Super admins), the caller's own session and joining an
// organisation by invitation bypass the organisation, and only the setup status and the sign-in providers
// need no credential.
// req: SEC-02
// asvs: V8.2.1
func TestOnlySetupIsPublicOrOutsideAnOrganisation(t *testing.T) {
	for op, p := range authz.Policies {
		installation := p.Capability == authz.Setup || p.Capability == authz.SuperAdmins || p.Capability == authz.Session ||
			p.Capability == authz.Joining
		public := op == "getSetupStatus" || op == "listSignInProviders"
		noOrg := installation && !public
		if p.Public != public || p.NoOrganisation != noOrg {
			t.Errorf("%s: Public %v, NoOrganisation %v; want %v, %v", op, p.Public, p.NoOrganisation, public, noOrg)
		}
		if installation && authz.Decide(p, []authz.Role{authz.SuperAdmin}, true).Allowed {
			t.Errorf("%s: the AI may not set up the installation, change its Super admins or hold a session", op)
		}
	}
	if d := authz.Decide(authz.CompleteSetup, nil, true); d.Allowed {
		t.Error("the AI may not set up the installation")
	}
	if d := authz.Decide(authz.CompleteSetup, []authz.Role{authz.SuperAdmin}, false); !d.Allowed {
		t.Error("a person outside every organisation may complete setup (the code decides)")
	}
}

// The Owner may run what the founder bootstrap lets them (FR-ADM-01, rev 0.35), and the decision says the
// handler must check it; the Administrator holds the right itself; the AI and the other roles do not.
// req: FR-ADM-01
// req: SEC-02
// asvs: V8.2.1, V8.3.1
func TestTheOwnerMayActUnderTheFounderBootstrap(t *testing.T) {
	for _, p := range []authz.Policy{authz.CreateMemberInvitation, authz.AssignRole, authz.RevokeRole, authz.WithdrawMemberInvitation} {
		if d := authz.Decide(p, []authz.Role{authz.Owner}, false); !d.Allowed || !d.ByOwner {
			t.Errorf("%s: the Owner: %+v", p.Operation, d)
		}
		if d := authz.Decide(p, []authz.Role{authz.Administrator}, false); !d.Allowed || d.ByOwner {
			t.Errorf("%s: the Administrator: %+v", p.Operation, d)
		}
		for _, roles := range [][]authz.Role{{authz.Controller}, {authz.Auditor}, {authz.Employee}} {
			if d := authz.Decide(p, roles, false); d.Allowed {
				t.Errorf("%s: %v allowed", p.Operation, roles)
			}
		}
		if d := authz.Decide(p, []authz.Role{authz.Owner, authz.Administrator}, true); d.Allowed {
			t.Errorf("%s: the AI acting for an Owner allowed", p.Operation)
		}
	}
	if d := authz.Decide(authz.ListMembers, []authz.Role{authz.Owner}, false); !d.Allowed || d.ByOwner {
		t.Errorf("listing members: the Owner views with their own right: %+v", d)
	}
	if d := authz.Decide(authz.UpdateSetting, []authz.Role{authz.Owner}, false); d.Allowed {
		t.Errorf("the bootstrap reaches beyond members: %+v", d)
	}
}

// Evidence on a record needs the evidence right and the right to view the record: a Bookkeeper (C on
// evidence, nothing on journals) cannot reach a journal's evidence; the AI only proposes (doc.attach).
// req: FR-DOC-11
// req: SEC-02
// asvs: V8.2.2
func TestEvidenceNeedsTheRecordToo(t *testing.T) {
	for _, c := range []struct {
		roles []authz.Role
		ai    bool
		want  bool
	}{
		{[]authz.Role{authz.Accountant}, false, true},
		{[]authz.Role{authz.Bookkeeper}, false, false},
		{[]authz.Role{authz.Bookkeeper, authz.Auditor}, false, true}, // C on evidence, V on journals
		{[]authz.Role{authz.Auditor}, false, false},                  // V on evidence: views, never adds
		{[]authz.Role{authz.Owner, authz.Accountant}, true, false},   // the AI proposes only
	} {
		if d := authz.Decide(authz.AddJournalEvidence, c.roles, c.ai); d.Allowed != c.want {
			t.Errorf("%v (AI %v) adding a journal's evidence: %+v; want allowed %v", c.roles, c.ai, d, c.want)
		}
	}
	if d := authz.Decide(authz.ListJournalEvidence, []authz.Role{authz.Auditor}, false); !d.Allowed {
		t.Errorf("an Auditor views a journal's evidence: %+v", d)
	}
}
