//go:build local

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	userguide "github.com/cognizhi/fyniq/docs/user-guide"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/knowledge"
	"github.com/cognizhi/fyniq/internal/ports"
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

// kbSeam is the AI service's backend with the API's real tool operation behind it and a scripted model: the model reads a
// page, then answers with a know-how sentence built from what the tool actually returned to it. It is the seam the unit tests
// of the orchestrator cannot see: the API's result, as it leaves the API (screened, serialised), as the orchestrator parses it.
type kbSeam struct {
	t       *testing.T
	e       *env
	service string
	key     string
	read    string // what the model was given for the page
	calls   int
}

func (b *kbSeam) CallTool(_ context.Context, token, tool string, input json.RawMessage) (assistant.ToolResult, error) {
	r := b.e.call(http.MethodPost, "/internal/v1/tool-calls", token, map[string]any{"tool": tool, "input": input}, ServiceTokenHeader, b.service)
	if r.Status != http.StatusOK {
		return assistant.ToolResult{}, &assistant.Problem{Status: r.Status, Code: r.str("code"), Detail: r.str("detail")}
	}
	var out assistant.ToolResult
	if err := json.Unmarshal([]byte(r.Raw), &out); err != nil {
		b.t.Fatal(err)
	}
	return out, nil
}

func (b *kbSeam) Propose(context.Context, string, assistant.Proposal) (assistant.Proposed, error) {
	return assistant.Proposed{}, nil
}

var firstNumber = regexp.MustCompile(`[0-9][0-9,]*(?:\.[0-9]+)?`)

func (b *kbSeam) CallModel(_ context.Context, _ string, call assistant.ModelCall) (assistant.ModelResult, error) {
	b.calls++
	if b.calls == 1 {
		in, _ := json.Marshal(map[string]string{"key": b.key})
		return assistant.ModelResult{Run: "run-1", Response: ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartToolCall, CallID: "c1", Tool: "kb_read", Input: in}}}}, nil
	}
	msgs := call.Request.Messages
	b.read = msgs[len(msgs)-1].Parts[0].Text // the tool result as the model gets it
	var got struct {
		Content struct {
			Sections []struct{ Section, Text string } `json:"sections"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(b.read), &got); err != nil {
		b.t.Fatalf("the tool result the model got is not JSON: %v\n%s", err, b.read)
	}
	for _, s := range got.Content.Sections {
		if n := firstNumber.FindString(s.Text); n != "" {
			seg, _ := json.Marshal(assistant.Structured{Segments: []assistant.Segment{{Text: "The guide mentions " + strings.TrimRight(n, ",.") + " here.", Page: b.key, Section: s.Section}}})
			return assistant.ModelResult{Run: "run-2", Response: ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(seg)}}, StopReason: ports.StopEnd}}, nil
		}
	}
	b.t.Fatalf("no section of %s holds a number", b.key)
	return assistant.ModelResult{}, nil
}

// The whole know-how path with the real tool operation behind it (AI-51, AI-53, AI-54): what kb.read returns leaves the API
// unquoted (a page's words and the tool's own hints are not untrusted_text), the orchestrator keeps the page and sections it
// read, a sentence citing one of them passes the number check, and the answer shows the guide pages the section came from, with
// the page's version recorded on the tool call. A hand-written tool result in a unit test would not catch a mismatch here.
// asvs: V8.2.1
func TestAKnowHowAnswerWorksThroughTheRealToolPath(t *testing.T) {
	e := newEnv(t)
	corpus := shippedKB(t)
	f := foundersIn(e)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	var token string
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		token, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(f.ahkow), uuid.New())
		return err
	})
	guideKeys := map[string]bool{}
	for _, g := range corpus.Guide {
		guideKeys[g.Key] = true
	}
	// A page with a number in a section, as the tool returns it.
	key := ""
	for _, p := range corpus.Pages {
		for _, s := range knowledge.Sections(p.Body) {
			if firstNumber.MatchString(knowledge.Cited(s.Words())) && len(knowledge.Cited(s.Words())) < 2000 {
				key = p.Key
			}
		}
		if key != "" {
			break
		}
	}
	if key == "" {
		t.Fatal("no page of the release has a number in a section")
	}
	b := &kbSeam{t: t, e: e, service: service, key: key}
	prompt, err := assistant.DefaultPrompt(assistant.Agent)
	if err != nil {
		t.Fatal(err)
	}
	a := &assistant.Assistant{Backend: b, Prompt: prompt}
	out, err := a.Answer(context.Background(), assistant.Question{Token: token, Text: "What does the guide say?"}, func(assistant.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.read, "untrusted_text") {
		t.Errorf("a trusted page's words were quoted as untrusted text:\n%.600s", b.read)
	}
	if out.Stop != assistant.StopAnswered || out.Grounding.Regenerated || out.Grounding.Withheld != 0 || !strings.Contains(out.Text, "The guide mentions") {
		t.Fatalf("the answer was not accepted: %+v\n%.600s", out, b.read)
	}
	if len(out.Sources) == 0 {
		t.Fatalf("no guide source: %+v", out)
	}
	for _, s := range out.Sources {
		if !guideKeys[s.Guide] || s.Title == "" || s.Anchor == "" {
			t.Errorf("a source that is not a guide page: %+v", s)
		}
	}
	if len(out.ToolCalls) != 1 || len(out.ToolCalls[0].KBPages) != 1 || out.ToolCalls[0].KBPages[0].Key != key || !strings.HasPrefix(out.ToolCalls[0].KBPages[0].Version, "v-test#") {
		t.Errorf("the page version recorded: %+v", out.ToolCalls)
	}
}

// The organisation's published pages are read with the platform's (AI-50, AI-54, AI-56; p1-knowledge-base design D3, D5): the
// index lists them, a read returns them with their sources resolved to what the reader can open (a note only to those who may
// read the notes, a setting to anyone), a search finds them, and an overlay takes the place of the platform page it replaces
// for this organisation alone. A page in a class the reader may not read, and a draft, are not there. The page version an
// answer used names the snapshot and the version.
// asvs: V8.2.1, V8.2.2
func TestTheOrganisationsPublishedPagesAreReadWithThePlatformsPages(t *testing.T) {
	e := newEnv(t)
	corpus := shippedKB(t)
	code := e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Owner), map[string]any{"display_name": "Staff", "roles": []string{"employee"}}).str("code")
	f := foundersIn(e)
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	grant := func(user string) string {
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
		return e.call(http.MethodPost, "/internal/v1/tool-calls", token, map[string]any{"tool": name, "input": input}, ServiceTokenHeader, service)
	}
	person := uuid.MustParse(f.ahkow)
	platform := corpus.Pages[0]
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		k := u.OrganisationKnowledge()
		add := func(v storage.KBVersion) {
			v.CreatedAt, v.AuthorKind, v.AuthorUser, v.HumanEdited = e.clock.Now(), "person", person, true
			if _, err := k.AddVersion(ctx, v); err != nil {
				t.Fatal(err)
			}
		}
		add(storage.KBVersion{Key: "org.expense-policy", Type: "policy", Title: "Expense policy", Summary: "How claims are made here.", Aliases: []string{"claims rule"},
			Body:    "# Expense policy\n\n## Claims\n\nClaims are made within 14 days of the receipt date. (src: note:expense-policy#claims)\n\nA founder approves claims up to a limit. (src: setting:approvals.self_approval_limit#value)\n",
			Sources: []byte(`[{"kind":"note","ref":"expense-policy","title":"Expense policy"},{"kind":"setting","ref":"approvals.self_approval_limit","title":"Self-approval limit"}]`),
			Origin:  storage.KBOriginHumanEdited})
		add(storage.KBVersion{Key: "org.pay-rules", Type: "policy", Title: "Pay rules", Summary: "How pay is decided here.", Body: "# Pay rules\n\n## Rules\n\nPay is monthly. (src: note:pay#rules)\n", DataClass: "payroll", Origin: storage.KBOriginHumanEdited})
		add(storage.KBVersion{Key: "org.draft-only", Type: "policy", Title: "Not yet approved", Summary: "A draft.", Body: "# Draft\n\n## Text\n\nNot published. (src: note:x#text)\n", Origin: storage.KBOriginHumanEdited})
		add(storage.KBVersion{Key: platform.Key, Type: platform.Type, Title: "Our version of " + platform.Title, Summary: "The organisation's own wording.",
			Body: "# Ours\n\n## Our rule\n\nWe do it our way. (src: platform:" + platform.Key + "#our-rule)\n", Origin: storage.KBOriginOverlay, BaseKey: platform.Key, BaseDigest: platform.Digest})
		_, err := k.Publish(ctx, map[string]int{"org.expense-policy": 1, "org.pay-rules": 1, platform.Key: 1}, nil, &person, "first", e.clock.Now())
		return err
	})
	content := func(r response) map[string]any {
		t.Helper()
		if r.Status != http.StatusOK {
			t.Fatalf("callAITool: %d %s", r.Status, r.Raw)
		}
		c, _ := r.Body["content"].(map[string]any)
		return c
	}
	// The index lists the organisation's published pages, an overlay in place of its platform page, never a draft or a class the reader may not read.
	for who, token := range map[string]string{"owner": owner, "employee": employee} {
		got, _ := content(tool(token, "kb.index", map[string]any{"section": "org"}))["pages"].([]any)
		if !containsKey(got, "org.expense-policy") || containsKey(got, "org.pay-rules") || containsKey(got, "org.draft-only") {
			t.Errorf("the %s's org index: %v", who, got)
		}
	}
	// A page, with its sources resolved to what the reader can open.
	cites := func(token string) []any {
		r := tool(token, "kb.read", map[string]any{"key": "org.expense-policy"})
		pg := content(r)
		pages, _ := r.Body["kb_pages"].([]any)
		if len(pages) != 1 || pages[0].(map[string]any)["version"] != "organisation snapshot 1, version 1" {
			t.Errorf("the version used: %s", r.Raw)
		}
		var out []any
		for _, s := range pg["sections"].([]any) {
			out = append(out, s.(map[string]any)["cites"].([]any)...)
		}
		return out
	}
	byTitle := func(cs []any) map[string]map[string]any {
		m := map[string]map[string]any{}
		for _, c := range cs {
			m[c.(map[string]any)["title"].(string)] = c.(map[string]any)
		}
		return m
	}
	o := byTitle(cites(owner))
	if o["Note: Expense policy"]["href"] != "/settings/ai/knowledge-base#note-expense-policy" || o["Setting: Self-approval limit"]["href"] != "/settings/organisation#approvals.self_approval_limit" {
		t.Errorf("the Owner's sources: %v", o)
	}
	em := byTitle(cites(employee))
	if em["The organisation's policy"]["href"] != nil || em["The organisation's policy"] == nil || em["Setting: Self-approval limit"]["href"] == nil {
		t.Errorf("an employee's sources: %v", em)
	}
	if r := tool(employee, "kb.read", map[string]any{"key": "org.pay-rules"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("an employee read a payroll page: %d %s", r.Status, r.Raw)
	}
	if r := tool(owner, "kb.read", map[string]any{"key": "org.pay-rules"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("the Owner read a payroll page with AI payroll access off: %d %s", r.Status, r.Raw)
	}
	if r := tool(owner, "kb.read", map[string]any{"key": "org.draft-only"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a draft was read: %d %s", r.Status, r.Raw)
	}
	// An overlay replaces the platform page, for this organisation: its words, its version, its search.
	o2 := content(tool(owner, "kb.read", map[string]any{"key": platform.Key}))
	intro, _ := o2["intro"].(string)
	if !strings.Contains(intro+firstSectionText(o2), "our way") || !strings.Contains(o2["version"].(string), "organisation snapshot 1") {
		t.Errorf("the overlay did not replace the platform page: %v", o2)
	}
	hits, _ := content(tool(owner, "kb.search", map[string]any{"query": "claims rule receipt date", "scope": "pages"}))["hits"].([]any)
	if len(hits) == 0 || hits[0].(map[string]any)["key"] != "org.expense-policy" {
		t.Errorf("the search: %v", hits)
	}
	if hits, _ := content(tool(owner, "kb.search", map[string]any{"query": "pay is monthly", "scope": "pages"}))["hits"].([]any); containsKey(hits, "org.pay-rules") {
		t.Errorf("a payroll page was found: %v", hits)
	}
}

func firstSectionText(c map[string]any) string {
	secs, _ := c["sections"].([]any)
	if len(secs) == 0 {
		return ""
	}
	s, _ := secs[0].(map[string]any)["text"].(string)
	return s
}
