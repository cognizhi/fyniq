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

// What the knowledge-base screen reads and the one thing it writes (AI-55, AI-56; p1-knowledge-base task 8.1): the index lists
// Fyniq's pages and the organisation's with where each stands (unpublished, published, a newer draft waiting), a page's versions
// say which is live, the snapshots list what was published and by whom, and the findings can be listed and resolved once, each
// resolution audited. The Auditor reads all of it and resolves nothing.
// asvs: V8.2.1
func TestTheKnowledgeBaseScreenReadsPagesVersionsSnapshotsAndFindings(t *testing.T) {
	e := newEnv(t)
	shippedKB(t)
	f := foundersIn(e)
	owner := f.as(f.ahkow, authz.Owner)
	if r := e.call(http.MethodPost, "/v1/knowledge-base/notes", owner, map[string]any{"key": "expense-policy", "kind": "policy", "title": "Expense policy", "body": "Claims are made within 14 days of the receipt date."}); r.Status != http.StatusCreated {
		t.Fatalf("the note: %d %s", r.Status, r.Raw)
	}
	page := map[string]any{"type": "policy", "title": "Expense policy", "summary": "How claims are made here.", "aliases": []string{"claims rule"},
		"body":    "# Expense policy\n\n## Claims\n\nClaims are made within 14 days of the receipt date. (src: note:expense-policy#claims)\n",
		"sources": []map[string]any{{"kind": "note", "ref": "expense-policy", "title": "Expense policy"}}}
	index := func() map[string]map[string]any {
		r := e.call(http.MethodGet, "/v1/knowledge-base/pages", owner, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("listKBPages: %d %s", r.Status, r.Raw)
		}
		out := map[string]map[string]any{}
		for _, it := range r.Body["items"].([]any) {
			m := it.(map[string]any)
			out[m["key"].(string)] = m
		}
		return out
	}
	// The platform's pages are in the index from the start, and the organisation's has no published version.
	idx := index()
	if len(idx) < 2 || idx["org.expense-policy"] != nil {
		t.Fatalf("the index before any page: %d entries", len(idx))
	}
	for _, m := range idx {
		if m["source"] != "platform" || m["state"] != "platform" {
			t.Fatalf("a platform page in the index: %+v", m)
		}
		break
	}
	if r := e.call(http.MethodPut, "/v1/knowledge-base/pages/org.expense-policy", owner, page, "If-Match", etag("v0")); r.Status != http.StatusCreated {
		t.Fatalf("the edit: %d %s", r.Status, r.Raw)
	}
	if m := index()["org.expense-policy"]; m == nil || m["state"] != "unpublished" || m["newest_version"] != float64(1) || m["published_version"] != float64(0) || m["origin"] != "human_edited" || m["source"] != "organisation" {
		t.Errorf("an unpublished draft in the index: %+v", m)
	}
	if r := e.call(http.MethodPost, "/v1/knowledge-base/publications", owner, map[string]any{"pages": []map[string]any{{"key": "org.expense-policy", "version": 1}}, "reason": "First."}); r.Status != http.StatusCreated {
		t.Fatalf("the publication: %d %s", r.Status, r.Raw)
	}
	if m := index()["org.expense-policy"]; m["state"] != "published" || m["published_version"] != float64(1) {
		t.Errorf("a published page in the index: %+v", m)
	}
	page["summary"] = "How claims are made, as edited."
	if r := e.call(http.MethodPut, "/v1/knowledge-base/pages/org.expense-policy", owner, page, "If-Match", kbPageETag(1)); r.Status != http.StatusCreated {
		t.Fatalf("the second edit: %d %s", r.Status, r.Raw)
	}
	if m := index()["org.expense-policy"]; m["state"] != "draft" || m["published_version"] != float64(1) || m["newest_version"] != float64(2) {
		t.Errorf("a newer draft in the index: %+v", m)
	}
	// The versions say which is live and which waits.
	vs := e.call(http.MethodGet, "/v1/knowledge-base/pages/org.expense-policy/versions", owner, nil)
	items, _ := vs.Body["items"].([]any)
	if vs.Status != http.StatusOK || len(items) != 2 || items[0].(map[string]any)["state"] != "draft" || items[1].(map[string]any)["state"] != "published" || items[0].(map[string]any)["number"] != float64(2) {
		t.Errorf("listKBPageVersions: %d %s", vs.Status, vs.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/knowledge-base/pages/org.nothing/versions", owner, nil), "listKBPageVersions", 404, CodeNotFound)
	// The snapshots.
	sn := e.call(http.MethodGet, "/v1/knowledge-base/snapshots", owner, nil)
	snaps, _ := sn.Body["items"].([]any)
	if sn.Status != http.StatusOK || len(snaps) == 0 || snaps[0].(map[string]any)["reason"] == nil {
		t.Errorf("listKBSnapshots: %d %s", sn.Status, sn.Raw)
	}
	// A finding is listed, counted on its page, resolved once and audited.
	var finding uuid.UUID
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		fd, err := u.OrganisationKnowledge().AddFinding(ctx, storage.KBFinding{Kind: "human_edit_conflict", PageKey: "org.expense-policy", PageVersion: 2, Detail: "The note changed after a person edited the page.", CreatedAt: e.clock.Now()})
		finding = fd.ID
		return err
	})
	open := e.call(http.MethodGet, "/v1/knowledge-base/findings?status=open", owner, nil)
	found, _ := open.Body["items"].([]any)
	if open.Status != http.StatusOK || len(found) != 1 || found[0].(map[string]any)["id"] != finding.String() || found[0].(map[string]any)["page"] != "org.expense-policy" {
		t.Fatalf("listKBFindings: %d %s", open.Status, open.Raw)
	}
	if m := index()["org.expense-policy"]; m["open_findings"] != float64(1) {
		t.Errorf("the findings on the page: %+v", m)
	}
	resolve := func(id, how string) response {
		return e.call(http.MethodPost, "/v1/knowledge-base/findings/"+id+"/resolve", owner, map[string]any{"resolution": how})
	}
	r := resolve(finding.String(), "Kept the person's version.")
	if r.Status != http.StatusOK || r.str("status") != "resolved" || r.str("resolution") != "Kept the person's version." || r.str("resolved_by") == "" {
		t.Fatalf("resolveKBFinding: %d %s", r.Status, r.Raw)
	}
	if again := resolve(finding.String(), "Again."); again.Status < 400 {
		t.Errorf("a finding resolved twice: %d %s", again.Status, again.Raw)
	}
	e.problemIs(resolve(uuid.NewString(), "Nothing."), "resolveKBFinding", 404, CodeNotFound)
	if left := e.call(http.MethodGet, "/v1/knowledge-base/findings?status=open", owner, nil).Body["items"].([]any); len(left) != 0 {
		t.Errorf("the open findings after resolving: %v", left)
	}
	if m := index()["org.expense-policy"]; m["open_findings"] != float64(0) {
		t.Errorf("the findings on the page after resolving: %+v", m)
	}
	var audited bool
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		evs, err := u.Audit().Find(ctx, storage.AuditQuery{Action: "kb_finding.resolved", Limit: 10})
		audited = len(evs) == 1
		return err
	})
	if !audited {
		t.Errorf("the resolution is not audited once")
	}
	// The Auditor reads and resolves nothing.
	auditor := f.as(f.ahkow, authz.Auditor)
	if r := e.call(http.MethodGet, "/v1/knowledge-base/findings", auditor, nil); r.Status != http.StatusOK {
		t.Errorf("an Auditor reading the findings: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/knowledge-base/findings/"+finding.String()+"/resolve", auditor, map[string]any{"resolution": "No."}); r.Status != http.StatusForbidden {
		t.Errorf("an Auditor resolving: %d %s", r.Status, r.Raw)
	}
}

// An overlay made before Fyniq changed the platform page it replaces is flagged (AI-56; p1-knowledge-base task 7.2): the index
// says so for an administrator, and the assistant is told to say the page may be out of date. One made against the page as it
// is now carries no flag.
// asvs: V8.2.1
func TestAnOverlayOfAPlatformPageThatChangedIsFlagged(t *testing.T) {
	e := newEnv(t)
	corpus := shippedKB(t)
	f := foundersIn(e)
	owner := f.as(f.ahkow, authz.Owner)
	person := uuid.MustParse(f.ahkow)
	fresh, stale := corpus.Pages[0], corpus.Pages[1]
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		k := u.OrganisationKnowledge()
		for _, o := range []struct {
			p      string
			digest string
			page   storage.KBPlatformPage
		}{{fresh.Key, fresh.Digest, storage.KBPlatformPage{}}, {stale.Key, strings.Repeat("0", 64), storage.KBPlatformPage{}}} {
			if _, err := k.AddVersion(ctx, storage.KBVersion{Key: o.p, Type: "concept", Title: "Ours", Summary: "The organisation's wording.", Origin: storage.KBOriginOverlay, BaseKey: o.p, BaseDigest: o.digest,
				Body: "# Ours\n\n## Rule\n\nOur way. (src: platform:" + o.p + "#rule)\n", CreatedAt: e.clock.Now(), AuthorKind: "person", AuthorUser: person, HumanEdited: true}); err != nil {
				return err
			}
		}
		_, err := k.Publish(ctx, map[string]int{fresh.Key: 1, stale.Key: 1}, nil, &person, "overlays", e.clock.Now())
		return err
	})
	moved := map[string]bool{}
	for _, it := range e.call(http.MethodGet, "/v1/knowledge-base/pages", owner, nil).Body["items"].([]any) {
		m := it.(map[string]any)
		if m["source"] == "organisation" {
			moved[m["key"].(string)] = m["base_changed"] == true
		}
	}
	if len(moved) != 2 || moved[fresh.Key] || !moved[stale.Key] {
		t.Errorf("the overlays in the index: %v", moved)
	}
	service, _ := e.services.Token(ServiceAI)
	var token string
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		token, err = e.api.issueAIGrant(ctx, u, e.books.Org, person, uuid.New())
		return err
	})
	note := func(key string) string {
		r := e.call(http.MethodPost, "/internal/v1/tool-calls", token, map[string]any{"tool": "kb.read", "input": map[string]any{"key": key}}, ServiceTokenHeader, service)
		c, _ := r.Body["content"].(map[string]any)
		n, _ := c["note"].(string)
		return n
	}
	if n := note(stale.Key); !strings.Contains(n, "may be out of date") {
		t.Errorf("the note of a stale overlay: %q", n)
	}
	if n := note(fresh.Key); strings.Contains(n, "out of date") {
		t.Errorf("the note of a current overlay: %q", n)
	}
}
