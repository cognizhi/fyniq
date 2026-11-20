//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
)

// An Owner opens the full cap table to a Board viewer, with a reason, and ends it (FR-EQ-24): by default the viewer reads their own holding only and is
// refused the whole table (CAPGRANT-02); while a grant stands they read it; after it is revoked they are refused again; only an Owner grants or ends; the
// person must be a Board viewer; one live grant each; and an Employee or another role is not opened to it by anything here.
// req: FR-EQ-24
// req: QA-SEC-03
// asvs: V4.2.1
func TestAnOwnersGrantOpensTheFullCapTableToABoardViewerUntilItIsRevoked(t *testing.T) {
	e := newEnv(t)
	ivy := e.joined("Ivy", "board_viewer")
	owner := e.personAs(uuid.New(), authz.Owner)
	board := e.person(ivy, authz.BoardViewer)

	// By default the viewer's own holding only.
	e.problemIs(e.call(http.MethodGet, "/v1/cap-table", board, nil), "getCapTable", 403, "API-AUTHZ-01")
	e.problemIs(e.call(http.MethodGet, "/v1/cap-table/granted", board, nil), "getGrantedCapTable", 403, CodeCapGrantNone)
	if r := e.call(http.MethodGet, "/v1/me/holding", board, nil); r.Status != http.StatusOK {
		t.Fatalf("their own holding = %d: %s", r.Status, r.Raw)
	}
	// Only an Owner grants, and only to a Board viewer.
	body := map[string]any{"user_id": ivy.String(), "reason": "Lead investor's board seat"}
	e.problemIs(e.call(http.MethodPost, "/v1/cap-table-grants", e.person(uuid.New(), authz.Controller), body), "grantCapTableAccess", 403, CodeCapGrantOwner)
	other := e.joined("Eve", "board_viewer")
	_ = other
	e.problemIs(e.call(http.MethodPost, "/v1/cap-table-grants", owner, map[string]any{"user_id": uuid.NewString(), "reason": "No such member"}), "grantCapTableAccess", 422, "API-INPUT-02")
	e.problemIs(e.call(http.MethodPost, "/v1/cap-table-grants", owner, map[string]any{"user_id": ivy.String(), "reason": "x"}), "grantCapTableAccess", 422, "API-INPUT-02")
	g := e.call(http.MethodPost, "/v1/cap-table-grants", owner, body, "Idempotency-Key", "cg-1")
	if g.Status != http.StatusCreated || g.Body["live"] != true || g.str("user_id") != ivy.String() {
		t.Fatalf("granting = %d: %s", g.Status, g.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/cap-table-grants", owner, body), "grantCapTableAccess", 409, storage409())
	// While it stands, the whole table; the full table is not an employee's by it.
	if r := e.call(http.MethodGet, "/v1/cap-table/granted", board, nil); r.Status != http.StatusOK {
		t.Fatalf("granted = %d: %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/cap-table", board, nil), "getCapTable", 403, "API-AUTHZ-01") // the grant opens only its own operation
	e.problemIs(e.call(http.MethodGet, "/v1/cap-table/granted", e.person(uuid.New(), authz.Employee), nil), "getGrantedCapTable", 403, "API-AUTHZ-01")
	// Ending it needs a reason and an Owner, once.
	id := g.str("id")
	e.problemIs(e.call(http.MethodPost, "/v1/cap-table-grants/"+id+"/revoke", e.person(uuid.New(), authz.Controller), map[string]any{"reason": "The seat ended"}), "revokeCapTableAccess", 403, CodeCapGrantOwner)
	rv := e.call(http.MethodPost, "/v1/cap-table-grants/"+id+"/revoke", owner, map[string]any{"reason": "The seat ended"})
	if rv.Status != http.StatusOK || rv.Body["live"] != false || rv.str("revoke_reason") != "The seat ended" {
		t.Fatalf("revoking = %d: %s", rv.Status, rv.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/cap-table/granted", board, nil), "getGrantedCapTable", 403, CodeCapGrantNone)
	e.problemIs(e.call(http.MethodPost, "/v1/cap-table-grants/"+id+"/revoke", owner, map[string]any{"reason": "Again"}), "revokeCapTableAccess", 409, "STORAGE-CAPGRANT-01")
	if l := e.call(http.MethodGet, "/v1/cap-table-grants", owner, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the grants: %s", l.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/cap-table-grants", board, nil), "listCapTableGrants", 403, "API-AUTHZ-01")
}

func storage409() string { return "STORAGE-CAPGRANT-02" }
