// Package saveditem is a person's favourites and saved views (PRD-001 FR-UX-12): a page they pinned, or a page with the
// filters, columns and sort its address carries. An item holds only an address and the name the person gave it, never a
// record's content; the page it opens shows what the person's roles let them see (SEC-34), and the list leaves out an item
// whose area the person no longer holds a right in.
package saveditem

import (
	"regexp"
	"strings"

	"github.com/cognizhi/fyniq/internal/authz"
)

// The most items a person keeps of each kind.
const (
	MaxFavourites = 50
	MaxViews      = 50
)

// The codes of a saved item's rules.
const (
	CodePath  = "SVD-01" // an address that is not a page of the web app
	CodeArea  = "SVD-02" // a page in an area the person holds no right in
	CodeTitle = "SVD-03" // a name that is empty or too long
	CodeQuery = "SVD-04" // filters that are not an address's query
	CodeLimit = "SVD-05" // too many items
)

// Problem is one rule that failed.
type Problem struct{ Code, Field, Message, Fix string }

var (
	path  = regexp.MustCompile(`^/[A-Za-z0-9/_.$-]{0,199}$`)
	query = regexp.MustCompile(`^(\?[A-Za-z0-9_.%=&:,+~-]{0,254})?$`)
)

// areas are the pages' first segments and the Appendix D row whose view right opens them; a page of one of the personal or
// shell paths is open to every member. Unknown first segments are no page of the app.
var areas = map[string]authz.Capability{
	"sales":      authz.Invoices,
	"contacts":   authz.ContactsAndItems,
	"purchases":  authz.Bills,
	"banking":    authz.BankTransactions,
	"accounting": authz.Journals,
	"reports":    authz.Reports,
	"equity":     authz.CapTable,
	"planning":   authz.Budgets,
	"compliance": authz.ComplianceCenter,
	"projects":   authz.ProjectReports,
	"approvals":  authz.Approvals,
}

// open are the pages every member may open: the personal workspace, the Inbox, search, help and Settings' own pages.
var open = map[string]bool{"": true, "inbox": true, "me": true, "search": true, "help": true, "settings": true, "review-log": true}

// AreaOf is the Appendix D row whose view right a page needs, or Membership for a page every member may open; false for an
// address that is no page of the app. Settings' organisation pages are reached through their own screens' rights, so a
// favourite of one needs no more than membership here and the page itself refuses what the person may not open.
func AreaOf(p string) (authz.Capability, bool) {
	first := strings.SplitN(strings.TrimPrefix(p, "/"), "/", 2)[0]
	if c, ok := areas[first]; ok {
		return c, true
	}
	if open[first] {
		return authz.Membership, true
	}
	return "", false
}

// Validate refuses an item whose address, name or filters are not what a page's address holds.
func Validate(kind, title, p, q string) *Problem {
	if strings.TrimSpace(title) == "" || len([]rune(title)) > 80 {
		return &Problem{CodeTitle, "title", "The name must be 1 to 80 characters.", "Give it a short name"}
	}
	if !path.MatchString(p) || strings.Contains(p, "//") {
		return &Problem{CodePath, "path", "That is not the address of a page.", "Send the page's path, such as /sales/invoices"}
	}
	if _, ok := AreaOf(p); !ok {
		return &Problem{CodePath, "path", "That is not a page of Fyniq.", "Send the page's path, such as /sales/invoices"}
	}
	if kind == "favourite" && q != "" {
		return &Problem{CodeQuery, "query", "A favourite is a page, with no filters.", "Save it as a view to keep its filters"}
	}
	if !query.MatchString(q) {
		return &Problem{CodeQuery, "query", "The filters are not an address's query.", "Send them as ?name=value&name=value"}
	}
	return nil
}
