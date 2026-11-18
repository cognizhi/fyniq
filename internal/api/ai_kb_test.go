//go:build local

package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	userguide "github.com/cognizhi/fyniq/docs/user-guide"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/kb"
)

// shippedKB loads the release's own knowledge base into the test database, as the migrator does, and returns it.
func shippedKB(t *testing.T) knowledge.Corpus {
	t.Helper()
	c, err := knowledge.Load(kb.FS, userguide.FS)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := knowledge.NewRelease("v-test", c)
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.LoadPlatformKB(context.Background(), db.URL("fyniq_migrator"), rel, io.Discard); err != nil {
		t.Fatal(err)
	}
	return c
}

// The knowledge base's tools read know-how as the person, through the reader filter (AI-49, AI-51, AI-54, AI-58; p1-knowledge-base
// design D2, D3): the index lists the pages a person may open, in sections when it is long; a page comes with its sections
// and the guide pages each cites, never the compiled page; a long page as its outline and the section asked for; search finds
// by the words people use. A page the person may not open is not in their index, their search or their reads, and its
// refusal reads as a page that is not there. The result holds no figures, and names the page version it used.
// req: AI-49
// req: AI-51
// req: AI-54
// req: AI-58
// asvs: V8.2.1, V8.2.2
func TestTheKnowledgeBaseToolsReadThroughTheReaderFilter(t *testing.T) {
	e := newEnv(t)
	corpus := shippedKB(t)
	code := e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Owner), map[string]any{"display_name": "Staff", "roles": []string{"employee"}}).str("code")
	f := foundersIn(e)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	grant := func(user string) string {
		t.Helper()
		var token string
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			token, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(user), uuid.New())
			return err
		})
		return token
	}
	owner := grant(f.ahkow)
	staff := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "staff-" + uuid.NewString(), Client: "web"}),
		map[string]any{"invitation_code": code, "display_name": "Staff"}).str("user_id")
	employee := grant(staff)
	tool := func(token, name string, input map[string]any) response {
		t.Helper()
		return e.call(http.MethodPost, "/internal/v1/tool-calls", token, map[string]any{"tool": name, "input": input}, ServiceTokenHeader, service)
	}
	content := func(r response) map[string]any {
		t.Helper()
		if r.Status != http.StatusOK {
			t.Fatalf("callAITool: %d %s", r.Status, r.Raw)
		}
		if facts, _ := r.Body["facts"].([]any); len(facts) != 0 {
			t.Errorf("a knowledge-base tool returned figures: %s", r.Raw)
		}
		c, _ := r.Body["content"].(map[string]any)
		return c
	}
	guideKeys := map[string]bool{}
	for _, g := range corpus.Guide {
		guideKeys[g.Key] = true
	}
	// A page for every role, and one for fewer: found in the guide the pages cite.
	var open, restricted, long knowledge.Page
	for _, p := range corpus.Pages {
		all := true
		for _, s := range p.Sources {
			if g, ok := corpus.GuideAt(s.Path); ok && !(len(g.Roles) == 1 && g.Roles[0] == "all") {
				all = false
			}
		}
		switch {
		case all && open.Key == "":
			open = p
		case !all && restricted.Key == "" && len(p.Sources) == 1:
			restricted = p
		}
		if p.Key == "settings.organisation" {
			long = p
		}
	}
	if open.Key == "" || restricted.Key == "" || long.Key == "" {
		t.Fatalf("the shipped pages hold no open, restricted and long page: %q %q %q", open.Key, restricted.Key, long.Key)
	}

	// The Owner's index is long: its sections, then a section's pages.
	idx := content(tool(owner, "kb.index", map[string]any{}))
	if idx["sections"] == nil && idx["pages"] == nil {
		t.Errorf("the index: %v", idx)
	}
	sec := kbSection(restricted.Key)
	pages, _ := content(tool(owner, "kb.index", map[string]any{"section": sec}))["pages"].([]any)
	if !containsKey(pages, restricted.Key) {
		t.Errorf("the Owner's section %s lacks %s: %v", sec, restricted.Key, pages)
	}
	// The employee's index lacks what is not theirs.
	for _, section := range []string{sec} {
		got, _ := content(tool(employee, "kb.index", map[string]any{"section": section}))["pages"].([]any)
		if containsKey(got, restricted.Key) {
			t.Errorf("an employee's index lists %s", restricted.Key)
		}
	}
	// A page: the Owner reads it with its sections and the guide pages they cite, and its version.
	r := tool(owner, "kb.read", map[string]any{"key": restricted.Key})
	pg := content(r)
	if pg["key"] != restricted.Key || !strings.Contains(pg["version"].(string), "v-test#") {
		t.Errorf("kb.read: %s", r.Raw)
	}
	pagesRead, _ := r.Body["kb_pages"].([]any)
	if len(pagesRead) != 1 || pagesRead[0].(map[string]any)["key"] != restricted.Key || pagesRead[0].(map[string]any)["version"] != pg["version"] {
		t.Errorf("the page version used: %s", r.Raw)
	}
	cited := false
	for _, s := range pg["sections"].([]any) {
		for _, c := range s.(map[string]any)["cites"].([]any) {
			cited = true
			// A citation names a guide page by the guide's own key (a compiled page may share a key with its guide page, but the
			// citation is the guide's: its file is what the reader opens at /help).
			g, _ := c.(map[string]any)["guide"].(string)
			if !guideKeys[g] || c.(map[string]any)["title"] == "" || c.(map[string]any)["anchor"] == "" {
				t.Errorf("a citation resolves to %v: want a guide page the reader can open", c)
			}
		}
		if strings.Contains(s.(map[string]any)["text"].(string), "(src:") {
			t.Errorf("the page's own citation markers reached the model: %v", s)
		}
	}
	if !cited {
		t.Errorf("no section cites a guide page: %s", r.Raw)
	}
	// The same page, to a person whose roles the guide page is not for: not there, and no different from a missing page.
	refusedRead := tool(employee, "kb.read", map[string]any{"key": restricted.Key})
	missing := tool(employee, "kb.read", map[string]any{"key": "no.such-page"})
	if refusedRead.Status != http.StatusUnprocessableEntity || refusedRead.str("code") != CodeToolRefused || missing.str("code") != CodeToolRefused ||
		strings.Contains(refusedRead.Raw, restricted.Title) {
		t.Errorf("an employee reading %s: %d %s; a missing page: %d %s", restricted.Key, refusedRead.Status, refusedRead.Raw, missing.Status, missing.Raw)
	}
	// A page that is for everyone reads for both.
	for who, token := range map[string]string{"owner": owner, "employee": employee} {
		if r := tool(token, "kb.read", map[string]any{"key": open.Key}); r.Status != http.StatusOK {
			t.Errorf("the %s reading %s: %d %s", who, open.Key, r.Status, r.Raw)
		}
	}
	// A long page comes as its outline, then one section at a time, within the tool's result.
	lp := content(tool(owner, "kb.read", map[string]any{"key": long.Key}))
	outline, _ := lp["outline"].([]any)
	if len(outline) == 0 || len(lp["sections"].([]any)) != 0 {
		t.Fatalf("a long page: want its outline and no sections: %v", lp)
	}
	first := outline[0].(map[string]any)["section"].(string)
	one := content(tool(owner, "kb.read", map[string]any{"key": long.Key, "section": first}))
	if secs := one["sections"].([]any); len(secs) != 1 || secs[0].(map[string]any)["text"] == "" {
		t.Errorf("a section of a long page: %v", one)
	}
	// Search: by the words a person uses; the owner finds the restricted page by its title, the employee does not.
	title := strings.Fields(restricted.Title)
	query := strings.Join(title[:min(len(title), 4)], " ")
	if hits, _ := content(tool(owner, "kb.search", map[string]any{"query": query, "scope": "pages"}))["hits"].([]any); !containsKey(hits, restricted.Key) {
		t.Errorf("the Owner's search for %q misses %s: %v", query, restricted.Key, hits)
	}
	hits := content(tool(employee, "kb.search", map[string]any{"query": query, "scope": "both", "limit": 10}))["hits"].([]any)
	if containsKey(hits, restricted.Key) {
		t.Errorf("an employee's search found %s", restricted.Key)
	}
	for _, h := range hits {
		if strings.Contains(h.(map[string]any)["snippet"].(string), "<b>") {
			t.Errorf("markup in a snippet: %v", h)
		}
	}
	if none := content(tool(employee, "kb.search", map[string]any{"query": "zzzz qqqq"})); len(none["hits"].([]any)) != 0 || none["note"] == nil {
		t.Errorf("a search with no match: %v", none)
	}
	// Inputs the tools do not take.
	for name, in := range map[string]map[string]any{
		"kb.read":   {"key": "Not A Key"},
		"kb.search": {"query": "x", "scope": "everything"},
		"kb.index":  {"section": "../x"},
	} {
		if r := tool(owner, name, in); r.Status != http.StatusUnprocessableEntity || r.str("code") != CodeToolInput {
			t.Errorf("%s %v: %d %s", name, in, r.Status, r.Raw)
		}
	}
}

func containsKey(rows []any, key string) bool {
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok && m["key"] == key {
			return true
		}
	}
	return false
}
