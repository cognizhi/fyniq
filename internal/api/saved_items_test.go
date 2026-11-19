//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
)

// A person's favourites and saved views are their own, kept on the server (FR-UX-12): saved, renamed, taken away without
// deleting anything, limited to pages their roles may open, and listed only while they still hold the right (SEC-34); each
// change is the item and its audit event and nothing else.
// req: FR-UX-12
// req: SEC-34
func TestFavouritesAndViewsAreTheirOwnersOnly(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Owner, authz.Controller)
	weiling := f.as(f.weiling, authz.Owner, authz.Controller)
	drainWork(t, e)
	_, seq := auditActionsAfter(t, e, 0)

	fav := e.call(http.MethodPost, "/v1/saved-items", ahkow, map[string]any{"kind": "favourite", "title": "Invoices", "path": "/sales/invoices"})
	if fav.Status != 200 || fav.str("id") != "/sales/invoices" {
		t.Fatalf("a favourite: %d %s", fav.Status, fav.Raw)
	}
	view := e.call(http.MethodPost, "/v1/saved-items", ahkow, map[string]any{"kind": "view", "title": "Overdue", "path": "/sales/invoices", "query": "?status=overdue&sort=due"})
	if view.Status != 200 || view.str("id") == "" || view.str("query") != "?status=overdue&sort=due" {
		t.Fatalf("a view: %d %s", view.Status, view.Raw)
	}
	renamed := e.call(http.MethodPost, "/v1/saved-items", ahkow, map[string]any{"kind": "view", "id": view.str("id"), "title": "Overdue now", "path": "/sales/invoices", "query": "?status=overdue"})
	if renamed.Status != 200 || renamed.str("id") != view.str("id") {
		t.Fatalf("renaming: %d %s", renamed.Status, renamed.Raw)
	}
	list := e.call(http.MethodGet, "/v1/saved-items", ahkow, nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != 200 || len(items) != 2 {
		t.Fatalf("the list: %d %s", list.Status, list.Raw)
	}
	if only := e.call(http.MethodGet, "/v1/saved-items?kind=view", ahkow, nil); len(only.Body["items"].([]any)) != 1 {
		t.Fatalf("only views: %s", only.Raw)
	}
	// Another person's are their own.
	if theirs := e.call(http.MethodGet, "/v1/saved-items", weiling, nil); len(theirs.Body["items"].([]any)) != 0 {
		t.Fatalf("another person's items: %s", theirs.Raw)
	}
	// Refused: an address that is no page, filters that are no query, a favourite with filters, a name too long, a missing one.
	for _, bad := range []map[string]any{
		{"kind": "favourite", "title": "x", "path": "https://evil.example/"},
		{"kind": "favourite", "title": "x", "path": "/nowhere/else"},
		{"kind": "view", "title": "x", "path": "/sales/invoices", "query": "?q=<script>"},
		{"kind": "favourite", "title": "x", "path": "/sales/invoices", "query": "?a=1"},
		{"kind": "view", "title": " ", "path": "/sales/invoices", "query": "?a=1"},
	} {
		if r := e.call(http.MethodPost, "/v1/saved-items", ahkow, bad); r.Status != 422 {
			t.Fatalf("%v: %d %s", bad, r.Status, r.Raw)
		}
	}
	// A page in an area the roles may not open is refused; an Employee holds no right in Sales.
	employee := f.as(f.weiling, authz.Employee)
	if r := e.call(http.MethodPost, "/v1/saved-items", employee, map[string]any{"kind": "favourite", "title": "Invoices", "path": "/sales/invoices"}); r.Status != 422 {
		t.Fatalf("a page outside the roles: %d %s", r.Status, r.Raw)
	}
	// Taking one away deletes nothing, and the list follows the roles held now: an Employee Wei Ling no longer sees Sales items.
	if r := e.call(http.MethodPost, "/v1/saved-items", weiling, map[string]any{"kind": "favourite", "title": "Invoices", "path": "/sales/invoices"}); r.Status != 200 {
		t.Fatalf("saving as Wei Ling: %d %s", r.Status, r.Raw)
	}
	if mine := e.call(http.MethodGet, "/v1/saved-items", employee, nil); len(mine.Body["items"].([]any)) != 0 {
		t.Fatalf("an item in an area the roles no longer hold is listed: %s", mine.Raw)
	}
	gone := e.call(http.MethodDelete, "/v1/saved-items/favourite?id=%2Fsales%2Finvoices", ahkow, nil)
	if gone.Status != 200 {
		t.Fatalf("taking away: %d %s", gone.Status, gone.Raw)
	}
	if again := e.call(http.MethodDelete, "/v1/saved-items/favourite?id=%2Fsales%2Finvoices", ahkow, nil); again.Status != 404 {
		t.Fatalf("taking away twice: %d %s", again.Status, again.Raw)
	}
	if left := e.call(http.MethodGet, "/v1/saved-items", ahkow, nil); len(left.Body["items"].([]any)) != 1 {
		t.Fatalf("after taking one away: %s", left.Raw)
	}
	// Each change wrote its item and its audit event, nothing else did.
	actions, _ := auditActionsAfter(t, e, seq)
	for _, a := range actions {
		if a != "user_saved_item.set" {
			t.Fatalf("a save wrote %q: %v", a, actions)
		}
	}
	if len(actions) != 5 {
		t.Fatalf("the writes: %v", actions)
	}
	client := e.principal(authn.Principal{Subject: "client-1", Client: "integration", Class: authn.ClassIntegration, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}})
	if r := e.call(http.MethodPost, "/v1/saved-items", client, map[string]any{"kind": "favourite", "title": "Home", "path": "/"}); r.Status != 422 || r.str("code") != CodeSavedNoUser {
		t.Fatalf("a client's save: %d %s", r.Status, r.Raw)
	}
}
