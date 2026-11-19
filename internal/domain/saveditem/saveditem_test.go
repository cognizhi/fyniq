package saveditem_test

import (
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/saveditem"
)

// An item holds an address and a name only: a link out, a script or filters that are no query are refused, and every page of
// the app maps to the right its area needs.
// req: FR-UX-12
// asvs: V2.2.1
func TestAnItemIsAPageOfTheAppAndItsFilters(t *testing.T) {
	long := strings.Repeat("x", 81)
	for _, c := range []struct {
		name, kind, title, path, query, code string
	}{
		{"a link out", "favourite", "x", "https://evil.example/", "", saveditem.CodePath},
		{"a protocol-relative link", "favourite", "x", "//evil.example", "", saveditem.CodePath},
		{"an unknown area", "favourite", "x", "/nowhere", "", saveditem.CodePath},
		{"a script in the path", "favourite", "x", "/sales/<b>", "", saveditem.CodePath},
		{"a script in the filters", "view", "x", "/sales/invoices", "?q=<script>", saveditem.CodeQuery},
		{"filters without a question mark", "view", "x", "/sales/invoices", "a=1", saveditem.CodeQuery},
		{"a favourite with filters", "favourite", "x", "/sales/invoices", "?a=1", saveditem.CodeQuery},
		{"an empty name", "view", "  ", "/sales/invoices", "?a=1", saveditem.CodeTitle},
		{"a name too long", "view", long, "/sales/invoices", "?a=1", saveditem.CodeTitle},
	} {
		if p := saveditem.Validate(c.kind, c.title, c.path, c.query); p == nil || p.Code != c.code || p.Message == "" || p.Fix == "" {
			t.Errorf("%s: %+v, want %s", c.name, p, c.code)
		}
	}
	if p := saveditem.Validate("view", "Overdue", "/sales/invoices", "?status=overdue&sort=due%20asc"); p != nil {
		t.Errorf("a view was refused: %+v", p)
	}
	for path, want := range map[string]authz.Capability{"/sales/invoices": authz.Invoices, "/banking/categorise": authz.BankTransactions, "/reports/statements/pl": authz.Reports,
		"/inbox": authz.Membership, "/": authz.Membership, "/me/claims": authz.Membership, "/settings/profile": authz.Membership} {
		if got, ok := saveditem.AreaOf(path); !ok || got != want {
			t.Errorf("%s: %q %v, want %q", path, got, ok, want)
		}
	}
}
