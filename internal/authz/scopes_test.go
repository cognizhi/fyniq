package authz

import "testing"

// An API client's scopes narrow its member's roles to whole areas (FR-INT-02, AI-34): a read scope views its area, a
// write scope prepares and posts on its writable rows and views its area, the organisation is readable with any scope,
// and nothing approves or releases. Rows outside every area are never reached, whatever the scopes.
// req: FR-INT-02
// req: AI-34
// asvs: V8.2.1, V8.2.2
func TestScopesNarrowAClientToWholeAreas(t *testing.T) {
	read, write := []string{"fyniq.read.accounting"}, []string{"fyniq.write.sales"}
	cases := []struct {
		name   string
		scopes []string
		p      Policy
		want   bool
	}{
		{"read views its area", read, ListAccounts, true},
		{"read never writes", read, CreateJournal, false},
		{"read never posts", read, PostJournal, false},
		{"write prepares", write, CreateInvoice, true},
		{"write posts", write, IssueInvoice, true},
		{"write views its area", write, Policy{Operation: "x", Capability: Invoices, Need: View}, true},
		{"write never approves", write, Policy{Operation: "x", Capability: Invoices, Need: Approve}, false},
		{"another area", write, CreateJournal, false},
		{"the organisation with any scope", read, GetOrganisation, true},
		{"no scope, no organisation", nil, GetOrganisation, false},
		{"users and roles: never", []string{"fyniq.write.accounting", "fyniq.read.reports"}, Policy{Operation: "x", Capability: UsersAndRoles, Need: View}, false},
		{"connected apps: never", ClientScopes(), ListIntegrationClients, false},
		{"vendor bank details: never", ClientScopes(), Policy{Operation: "x", Capability: VendorBankDetails, Need: View}, false},
		{"payment batches: never", ClientScopes(), Policy{Operation: "x", Capability: PaymentBatches, Need: Release}, false},
		{"approvals: never", ClientScopes(), Policy{Operation: "x", Capability: Approvals, Need: Approve}, false},
		{"the AI's providers: never", ClientScopes(), CreateAIConnection, false},
		{"a record under a row outside the area", write, Policy{Operation: "x", Capability: Invoices, Need: View, Parent: Journals}, false},
		{"a record under a row in the area", []string{"fyniq.write.sales", "fyniq.read.accounting"},
			Policy{Operation: "x", Capability: Invoices, Need: View, Parent: Journals}, true},
		{"a wildcard is nothing", []string{"fyniq.write.*"}, CreateInvoice, false},
	}
	for _, c := range cases {
		if got := ScopeAllows(c.scopes, c.p); got != c.want {
			t.Errorf("%s: %v on %q %c = %v, want %v", c.name, c.scopes, c.p.Capability, c.p.Need, got, c.want)
		}
	}
	for _, s := range []string{"fyniq.read.payroll", "fyniq.read.equity", "fyniq.write.reports", "fyniq.approve.sales", "fyniq.propose.gl.post_journal", "openid", ""} {
		if ClientScope(s) {
			t.Errorf("%q must not be grantable to an API client", s)
		}
	}
	for _, s := range ClientScopes() {
		if !ClientScope(s) {
			t.Errorf("%q is listed but not grantable", s)
		}
	}
}
