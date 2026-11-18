//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Editing, rolling back and publishing an organisation's pages (AI-55, AI-56, FR-APR-01, FM-3; p1-knowledge-base design D8): an edit is
// a draft version, kept as it was made; a rollback is a new version; a publication is linted first (a number that is in none of its
// cited sources, a section with no source, a link to no page, an open finding each stop it) and then, in Founder mode, an Owner makes
// alone and it waits in the weekly review; in Team mode it is refused and another Owner signs a request that holds the pages and
// the snapshot, so a snapshot that moved voids it. The Administrator edits and asks but never signs. What the assistant reads
// changes only when a snapshot is made.
// asvs: V8.2.1, V8.2.2
func TestAPublicationIsLintedThenMadeByAnOwnerAloneOrSignedByAnother(t *testing.T) {
	e := newEnv(t)
	shippedKB(t)
	// Invited while the founders set up, so each keeps their role when they join after.
	codes := map[string]string{}
	for name, role := range map[string]string{"Ada": "administrator", "Bo": "owner"} {
		codes[name] = e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Owner), map[string]any{"display_name": name, "roles": []string{role}}).str("code")
	}
	f := foundersIn(e)
	users := map[string]string{}
	for name, code := range codes {
		j := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: name, Client: "web"}), map[string]any{"invitation_code": code, "display_name": name})
		if j.Status != 201 {
			t.Fatalf("%s joining: %d %s", name, j.Status, j.Raw)
		}
		users[name] = j.str("user_id")
	}
	owner := f.as(f.ahkow, authz.Owner)
	other := f.as(users["Bo"], authz.Owner)
	admin := f.as(users["Ada"], authz.Administrator)
	note := e.call(http.MethodPost, "/v1/knowledge-base/notes", owner, map[string]any{"key": "expense-policy", "kind": "policy", "title": "Expense policy", "body": "Claims are made within 14 days of the receipt date."})
	if note.Status != http.StatusCreated {
		t.Fatalf("the note: %d %s", note.Status, note.Raw)
	}
	page := func(body string) map[string]any {
		return map[string]any{"type": "policy", "title": "Expense policy", "summary": "How claims are made here.", "aliases": []string{"claims rule"}, "body": body,
			"sources": []map[string]any{{"kind": "note", "ref": "expense-policy", "title": "Expense policy"}}}
	}
	const good = "# Expense policy\n\n## Claims\n\nClaims are made within 14 days of the receipt date. (src: note:expense-policy#claims)\n"
	edit := func(who, key string, body map[string]any, tag string) response {
		return e.call(http.MethodPut, "/v1/knowledge-base/pages/"+key, who, body, "If-Match", tag)
	}
	// A draft that invents a number: saved (kept as written), refused at publication.
	v1 := edit(owner, "org.expense-policy", page("# Expense policy\n\n## Claims\n\nClaims are made within 30 days of the receipt date. (src: note:expense-policy#claims)\n"), etag("v0"))
	if v1.Status != http.StatusCreated || v1.Body["number"] != float64(1) || v1.Body["state"] != "draft" || v1.Body["origin"] != "human_edited" || v1.Body["human_edited"] != true {
		t.Fatalf("editKBPage: %d %s", v1.Status, v1.Raw)
	}
	publish := func(who string, versions map[string]int) response {
		var pages []map[string]any
		for k, v := range versions {
			pages = append(pages, map[string]any{"key": k, "version": v})
		}
		return e.call(http.MethodPost, "/v1/knowledge-base/publications", who, map[string]any{"pages": pages, "reason": "The policy changed."})
	}
	r := publish(owner, map[string]int{"org.expense-policy": 1})
	if r.Status != http.StatusUnprocessableEntity || r.str("code") != CodeKBLint || !strings.Contains(r.Raw, "30") {
		t.Errorf("a number in no source: %d %s", r.Status, r.Raw)
	}
	// A stale edit is refused; the fixed draft is version 2.
	e.problemIs(edit(owner, "org.expense-policy", page(good), etag("v0")), "editKBPage", 412, CodeStale)
	v2 := edit(owner, "org.expense-policy", page(good), v1.Header.Get("ETag"))
	if v2.Status != http.StatusCreated || v2.Body["number"] != float64(2) {
		t.Fatalf("the fixed draft: %d %s", v2.Status, v2.Raw)
	}
	// A page of no organisation key and no platform page is refused with the field; a secret in it too.
	if r := edit(owner, "somewhere.else", page(good), etag("v0")); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a key that is not a page's: %d %s", r.Status, r.Raw)
	}
	if r := edit(owner, "org.secret", page("# x\n\n## y\n\nThe key is sk-live-abcdefghijklmnop1234 (src: note:expense-policy#a)\n"), etag("v0")); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a secret in a page: %d %s", r.Status, r.Raw)
	}
	// The Administrator edits and asks, never signs or publishes alone.
	if r := publish(admin, map[string]int{"org.expense-policy": 2}); r.Status != http.StatusForbidden {
		t.Errorf("an Administrator publishing: %d %s", r.Status, r.Raw)
	}
	// Nothing the assistant reads has changed: a draft is not live.
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	var token string
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		token, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(f.ahkow), uuid.New())
		return err
	})
	reads := func() response {
		return e.call(http.MethodPost, "/internal/v1/tool-calls", token, map[string]any{"tool": "kb.read", "input": map[string]any{"key": "org.expense-policy"}}, ServiceTokenHeader, service)
	}
	if r := reads(); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a draft is read by the assistant: %d %s", r.Status, r.Raw)
	}
	// An open finding of the page stops its publication until a person resolves it.
	var finding uuid.UUID
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		fd, err := u.OrganisationKnowledge().AddFinding(ctx, storage.KBFinding{Kind: "contradiction", PageKey: "org.expense-policy", PageVersion: 2, Detail: "says 14 days, a setting says another", CreatedAt: e.clock.Now()})
		finding = fd.ID
		return err
	})
	if r := publish(owner, map[string]int{"org.expense-policy": 2}); r.Status != http.StatusUnprocessableEntity || r.str("code") != CodeKBLint || !strings.Contains(r.Raw, "open finding") {
		t.Errorf("an open finding: %d %s", r.Status, r.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		return u.OrganisationKnowledge().ResolveFinding(ctx, finding, uuid.MustParse(f.ahkow), "The page is right", e.clock.Now())
	})
	// Founder mode: the Owner publishes alone, it waits in the weekly review, and the assistant reads it.
	snap := publish(owner, map[string]int{"org.expense-policy": 2})
	if snap.Status != http.StatusCreated || snap.Body["number"] != float64(1) || snap.Body["approval_id"] != nil {
		t.Fatalf("publishKBPages: %d %s", snap.Status, snap.Raw)
	}
	if r := reads(); r.Status != http.StatusOK || !strings.Contains(r.Raw, "within 14 days") || !strings.Contains(r.Raw, "organisation snapshot 1, version 2") {
		t.Errorf("the assistant's read after the publication: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/review-log", f.as(f.weiling, authz.Owner), nil); r.Status != http.StatusOK || !strings.Contains(r.Raw, "knowledge base alone") {
		t.Errorf("the weekly review lacks the publication: %d %s", r.Status, r.Raw)
	}
	// A rollback is a new version, a draft, restoring the first's content; the older ones are untouched.
	rb := e.call(http.MethodPost, "/v1/knowledge-base/pages/org.expense-policy/rollback", owner, map[string]any{"version": 1})
	if rb.Status != http.StatusCreated || rb.Body["number"] != float64(3) || rb.Body["origin"] != "rollback" || rb.Body["restores"] != float64(1) || !strings.Contains(rb.str("body"), "30 days") {
		t.Errorf("rollbackKBPage: %d %s", rb.Status, rb.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/knowledge-base/pages/org.expense-policy/rollback", owner, map[string]any{"version": 9}), "rollbackKBPage", 404, CodeNotFound)

	// Team mode: refused alone; another Owner signs a request that holds the pages and the snapshot.
	ask := e.call(http.MethodPatch, "/v1/team-mode", admin, map[string]any{"mode": "standard"}, "If-Match", `"0"`)
	if ask.Status != 202 {
		t.Fatalf("asking for Standard mode: %d %s", ask.Status, ask.Raw)
	}
	if a := e.call(http.MethodPost, "/v1/approvals/"+ask.str("id")+"/approve", other, hashOf(ask), "If-Match", `"1"`); a.Status != 200 {
		t.Fatalf("approving Standard mode: %d %s", a.Status, a.Raw)
	}
	e.problemIs(publish(owner, map[string]int{"org.expense-policy": 2}), "publishKBPages", 409, CodeKBTeam)
	v4 := edit(owner, "org.expense-policy", page(strings.Replace(good, "14 days", "14 days, in writing", 1)), rb.Header.Get("ETag"))
	if v4.Status != http.StatusCreated {
		t.Fatalf("the next edit: %d %s", v4.Status, v4.Raw)
	}
	asks := func(who string, version int) response {
		return e.call(http.MethodPost, "/v1/knowledge-base/publications/approval", who, map[string]any{"pages": []map[string]any{{"key": "org.expense-policy", "version": version}}, "reason": "In writing."})
	}
	req := asks(owner, 4)
	if req.Status != http.StatusAccepted || req.str("object_type") != "kb_publication" || req.str("operation") != "kb_publication.publish" {
		t.Fatalf("requestKBPublication: %d %s", req.Status, req.Raw)
	}
	if r := f.decide(owner, req.str("id"), "approve", hashOf(req)); r.Status == http.StatusOK {
		t.Errorf("the requester signed their own publication: %s", r.Raw)
	}
	if r := f.decide(admin, req.str("id"), "approve", hashOf(req)); r.Status == http.StatusOK {
		t.Errorf("an Administrator signed: %s", r.Raw)
	}
	if r := f.decide(other, req.str("id"), "approve", hashOf(req)); r.Status != http.StatusOK {
		t.Fatalf("another Owner signing: %d %s", r.Status, r.Raw)
	}
	if r := reads(); !strings.Contains(r.Raw, "in writing") || !strings.Contains(r.Raw, "organisation snapshot 2, version 4") {
		t.Errorf("the assistant's read after the signed publication: %d %s", r.Status, r.Raw)
	}
	// A request holds the snapshot it replaces: a publication that lands first voids it.
	v5 := edit(owner, "org.expense-policy", page(strings.Replace(good, "receipt date", "date of the receipt", 1)), v4.Header.Get("ETag"))
	if v5.Status != http.StatusCreated {
		t.Fatalf("the edit: %d %s", v5.Status, v5.Raw)
	}
	first := asks(owner, 5)
	if first.Status != http.StatusAccepted {
		t.Fatalf("the request: %d %s", first.Status, first.Raw)
	}
	// One request waits at a time for the knowledge base: a second is refused until it is decided.
	if r := asks(owner, 5); r.Status != http.StatusConflict {
		t.Errorf("a second pending request: %d %s", r.Status, r.Raw)
	}
	if r := f.decide(other, first.str("id"), "approve", hashOf(first)); r.Status != http.StatusOK {
		t.Fatalf("the request: %d %s", r.Status, r.Raw)
	}
	if r := reads(); !strings.Contains(r.Raw, "date of the receipt") || !strings.Contains(r.Raw, "organisation snapshot 3, version 5") {
		t.Errorf("the assistant's read: %s", r.Raw)
	}
}
