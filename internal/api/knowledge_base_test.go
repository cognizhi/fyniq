//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The organisation's notes (AI-50, AI-55; p1-knowledge-base design D9): the Owner and the Administrator write them, the Auditor
// reads them and nobody else does; a change needs the version read; a note holding what looks like an identity number, a bank
// account number or a secret is refused with the field; a key is taken once; each is audited. Nothing here reaches the
// assistant: a note is a source, and only a published page is read.
// asvs: V8.2.1, V8.2.2
func TestTheOrganisationsNotes(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	owner := f.as(f.ahkow, authz.Owner)
	body := map[string]any{"key": "expense-policy", "kind": "policy", "title": "Expense policy", "body": "Claims are made within 30 days of the receipt date."}
	r := e.call(http.MethodPost, "/v1/knowledge-base/notes", owner, body)
	if r.Status != http.StatusCreated || r.Body["version"] != float64(1) || r.Body["data_class"] != "general" || !strings.HasSuffix(r.Header.Get("Location"), "/expense-policy") {
		t.Fatalf("createKBNote: %d %s", r.Status, r.Raw)
	}
	etag := r.Header.Get("ETag")
	// A key is taken once.
	r = e.call(http.MethodPost, "/v1/knowledge-base/notes", owner, body)
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, "body.key") {
		t.Errorf("a key taken: %d %s", r.Status, r.Raw)
	}
	// What looks like a secret, an identity number or a bank account number is refused, with the field.
	for _, bad := range []string{"The director is S1234567D.", "Pay to account 123456789012.", "Use sk-live-abcdefghijklmnop1234 to sign."} {
		r = e.call(http.MethodPost, "/v1/knowledge-base/notes", owner, map[string]any{"key": "bad", "kind": "note", "title": "t", "body": bad})
		if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, "body.body") {
			t.Errorf("%q: %d %s", bad, r.Status, r.Raw)
		}
	}
	// A change needs the version read, and stale is refused.
	change := map[string]any{"kind": "policy", "title": "Expense policy", "body": "Claims are made within 14 days of the receipt date."}
	e.problemIs(e.call(http.MethodPut, "/v1/knowledge-base/notes/expense-policy", owner, change), "reviseKBNote", 428, CodeIfMatchRequired)
	r = e.call(http.MethodPut, "/v1/knowledge-base/notes/expense-policy", owner, change, "If-Match", etag)
	if r.Status != http.StatusOK || r.Body["version"] != float64(2) || r.Header.Get("ETag") == etag {
		t.Fatalf("reviseKBNote: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/knowledge-base/notes/expense-policy", owner, change, "If-Match", etag), "reviseKBNote", 412, CodeStale)
	e.problemIs(e.call(http.MethodPut, "/v1/knowledge-base/notes/no-such-note", owner, change, "If-Match", etag), "reviseKBNote", 404, CodeNotFound)
	// Who may read and who may write (Appendix D: Owner and Administrator VC, Auditor V, nobody else).
	for role, want := range map[authz.Role][2]int{authz.Administrator: {200, 201}, authz.Auditor: {200, 403}, authz.Accountant: {403, 403}, authz.Employee: {403, 403}, authz.Controller: {403, 403}} {
		read := e.call(http.MethodGet, "/v1/knowledge-base/notes", e.as(role), nil)
		wrote := e.call(http.MethodPost, "/v1/knowledge-base/notes", f.as(f.weiling, role), map[string]any{"key": "by-" + strings.ReplaceAll(string(role), "_", "-"), "kind": "note", "title": "t", "body": "b"})
		if read.Status != want[0] || wrote.Status != want[1] {
			t.Errorf("%s: read %d, wrote %d; want %v", role, read.Status, wrote.Status, want)
		}
	}
	list := e.call(http.MethodGet, "/v1/knowledge-base/notes", e.as(authz.Auditor), nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != http.StatusOK || len(items) != 2 || items[0].(map[string]any)["key"] != "by-administrator" || items[1].(map[string]any)["key"] != "expense-policy" {
		t.Errorf("the Auditor's list: %d %s", list.Status, list.Raw)
	}
	// Audited, with the text before and after.
	if r := e.call(http.MethodGet, "/v1/audit/events?action=kb_note.changed", owner, nil); r.Status != http.StatusOK || !strings.Contains(r.Raw, `"body"`) {
		t.Errorf("the change is not audited: %d %s", r.Status, r.Raw)
	}
	// A note is a source, not a page: the assistant, which reads published pages, finds nothing of it.
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	var token string
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		token, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(f.ahkow), uuid.New())
		return err
	})
	s := e.call(http.MethodPost, "/internal/v1/tool-calls", token, map[string]any{"tool": "kb.search", "input": map[string]any{"query": "expense policy receipt date"}}, ServiceTokenHeader, service)
	if strings.Contains(s.Raw, "within 14 days") || strings.Contains(s.Raw, "org.expense-policy") {
		t.Errorf("an unpublished note reached the assistant: %s", s.Raw)
	}
}

// matrixKnowledgeBase has one request per operation of the organisation's knowledge base for the authorisation matrix
// (QA-SEC-03). The matrix's people have no user to write as, so a write that policy allows answers "a person must be signed in"
// (pastPolicy); TestTheOrganisationsNotes proves the allowed path.
var matrixKnowledgeBase = map[string]func(e *env, tok string) (response, func() bool){
	"listKBNotes": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/knowledge-base/notes", tok, nil), nil
	},
	"createKBNote": func(e *env, tok string) (response, func() bool) {
		r := e.call(http.MethodPost, "/v1/knowledge-base/notes", tok, map[string]any{"key": "matrix-note", "kind": "note", "title": "t", "body": "b"}, "Prefer", "return=preview")
		return r, func() bool { return matrixNotesNone(e) }
	},
	"editKBPage": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/knowledge-base/pages/org.matrix", tok, map[string]any{"type": "policy", "title": "t", "summary": "s", "body": "# t", "sources": []any{}},
			"If-Match", "*", "Prefer", "return=preview"), nil
	},
	"rollbackKBPage": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/knowledge-base/pages/org.matrix/rollback", tok, map[string]any{"version": 1}, "Prefer", "return=preview"), nil
	},
	"publishKBPages": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/knowledge-base/publications", tok, map[string]any{"pages": []any{map[string]any{"key": "org.matrix", "version": 1}}}, "Prefer", "return=preview"), nil
	},
	"requestKBPublication": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/knowledge-base/publications/approval", tok, map[string]any{"pages": []any{map[string]any{"key": "org.matrix", "version": 1}}}, "Prefer", "return=preview"), nil
	},
	"reviseKBNote": func(e *env, tok string) (response, func() bool) {
		r := e.call(http.MethodPut, "/v1/knowledge-base/notes/matrix-note", tok, map[string]any{"kind": "note", "title": "t", "body": "b"}, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixNotesNone(e) }
	},
}

func matrixNotesNone(e *env) bool {
	items, _ := e.call(http.MethodGet, "/v1/knowledge-base/notes", e.as(authz.Owner), nil).Body["items"].([]any)
	return len(items) == 0
}
