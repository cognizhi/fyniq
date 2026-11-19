//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/kbcompile"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The knowledge-base compiler holds its own service identity and the API's four operations for it, and nothing else (AI-52, AI-57;
// design D7): it lists the notes that changed, has the model make a page of each, stores a page that passes the lint as a draft of the
// compiler's own authorship, and raises a finding when none does. A draft is never read by the assistant; only a person's publication
// makes a snapshot, and no token of the compiler's opens it, or the tools, or any operation of the AI service's. A person's version is
// never overwritten: the recompile waits beside it with a finding. Another service's token opens none of the compiler's operations.
// asvs: V8.2.1, V8.2.2
func TestTheCompilerMakesDraftsThroughItsOwnIdentityAndNothingElse(t *testing.T) {
	e := newEnv(t)
	shippedKB(t)
	f := foundersIn(e)
	owner := f.as(f.ahkow)
	aiSwitchedOn(e)
	sg := routable(e, "Compiler", "SG", []string{"claude-opus-5-5"})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "kb_compiler", Version: 1, Classes: []string{"know-how"}, MadeBy: "user:test", MadeAt: e.clock.Now(),
			Steps: []storage.AIRouteStep{{ConnectionID: sg, Model: "claude-opus-5-5"}}})
		return err
	})
	page := func(body string) ports.ModelResponse {
		b, _ := json.Marshal(map[string]any{"title": "Expense policy", "summary": "How claims are made here.", "aliases": []string{"claims rule"}, "body": body})
		return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(b)}}}
	}
	const good = "# Expense policy\n\n## Claims\n\nClaims are made within 14 days of the receipt date. (src: note:expense-policy#claims)\n"
	model := &scripted{answers: []ports.ModelResponse{page(strings.Replace(good, "14 days", "30 days", 1)), page(good)}}
	e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return model, nil
	}
	for _, n := range []map[string]any{
		{"key": "expense-policy", "kind": "policy", "title": "Expense policy", "body": "Claims are made within 14 days of the receipt date."},
		{"key": "pay-rules", "kind": "policy", "title": "Pay rules", "body": "Pay is monthly.", "data_class": "payroll"},
	} {
		if r := e.call(http.MethodPost, "/v1/knowledge-base/notes", owner, n); r.Status != http.StatusCreated {
			t.Fatalf("the note: %d %s", r.Status, r.Raw)
		}
	}
	token, err := e.services.Token(kbcompile.Class)
	if err != nil {
		t.Fatal(err)
	}
	client := kbcompile.APIClient{URL: e.srv.URL + "/internal/v1", Token: func(context.Context) (string, error) { return token, nil }, HTTP: e.srv.Client()}
	call := func(tok, path string, body map[string]any) response {
		return e.call(http.MethodPost, "/internal/v1/kb-compiler/"+path, "", body, ServiceTokenHeader, tok)
	}
	// Whose token: another service's opens none of them, and the compiler's opens nothing of the AI service's.
	aiToken, _ := e.services.Token(ServiceAI)
	workerToken, _ := e.services.Token(ServiceAgentRuns)
	for _, bad := range []string{aiToken, workerToken, "nonsense"} {
		if r := call(bad, "work", map[string]any{"organisation_id": e.books.Org.String()}); r.Status != http.StatusUnauthorized {
			t.Errorf("a token that is not the compiler's: %d %s", r.Status, r.Raw)
		}
	}
	if r := e.call(http.MethodPost, "/internal/v1/tool-calls", "", map[string]any{"tool": "kb.read", "input": map[string]any{"key": "org.expense-policy"}}, ServiceTokenHeader, token); r.Status != http.StatusUnauthorized {
		t.Errorf("the compiler's token on the tools: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/knowledge-base/publications", token, map[string]any{"pages": []any{map[string]any{"key": "org.expense-policy", "version": 1}}}); r.Status != http.StatusUnauthorized && r.Status != http.StatusForbidden {
		t.Errorf("the compiler's token on a publication: %d %s", r.Status, r.Raw)
	}
	// The work: the general note, with its text and the keys a link may name; the payroll note is left out.
	items, err := client.Work(context.Background(), e.books.Org)
	if err != nil || len(items) != 1 || items[0].NoteKey != "expense-policy" || !strings.Contains(items[0].Text, "within 14 days") || len(items[0].Keys) == 0 {
		t.Fatalf("the work: %+v %v", items, err)
	}
	// The run: the model's first page invents a number, so the job asks again with what the lint found, and the second page is a draft.
	rep, err := kbcompile.Run(context.Background(), client, e.books.Org)
	if err != nil || rep.Drafts != 1 || rep.Findings != 0 || len(rep.Failed) != 0 || len(model.requests) != 2 ||
		!strings.Contains(model.requests[1].Messages[0].Parts[0].Text, "30") {
		t.Fatalf("the run: %+v %v requests %d", rep, err, len(model.requests))
	}
	if !strings.Contains(model.requests[0].System, "untrusted_text") && !strings.Contains(model.requests[0].Messages[0].Parts[0].Text, "untrusted_text") {
		t.Errorf("the note reached the model as something other than untrusted text: %v", model.requests[0])
	}
	var vs []storage.KBVersion
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		vs, err = u.OrganisationKnowledge().Versions(ctx, "org.expense-policy")
		return err
	})
	if len(vs) != 1 || vs[0].Origin != "compiled" || vs[0].AuthorKind != "compiler" || vs[0].HumanEdited || vs[0].PromptVersion != "kb_compile@1" || !strings.Contains(string(vs[0].Sources), `"version": 1`) {
		t.Fatalf("the draft: %+v", vs)
	}
	// A draft is not live: the assistant cannot read it, and the work lists nothing more.
	service, _ := e.services.Token(ServiceAI)
	var grant string
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		grant, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(f.ahkow), uuid.New())
		return err
	})
	if r := e.call(http.MethodPost, "/internal/v1/tool-calls", grant, map[string]any{"tool": "kb.read", "input": map[string]any{"key": "org.expense-policy"}}, ServiceTokenHeader, service); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a compiled draft is read by the assistant: %d %s", r.Status, r.Raw)
	}
	if items, _ := client.Work(context.Background(), e.books.Org); len(items) != 0 {
		t.Errorf("the work after it was compiled: %+v", items)
	}
	// The draft endpoint runs the lint again: a page that does not pass is refused, whatever the job says.
	r := call(token, "drafts", map[string]any{"organisation_id": e.books.Org.String(), "note_key": "expense-policy", "note_version": 1,
		"draft": map[string]any{"title": "x", "summary": "s", "body": strings.Replace(good, "14 days", "99 days", 1)}})
	if r.Status != http.StatusUnprocessableEntity || r.str("code") != CodeKBLint {
		t.Errorf("a draft that fails the lint: %d %s", r.Status, r.Raw)
	}
	// Nor can it write for a note that changed, or one of another class, or a key that is no note's.
	if r := call(token, "drafts", map[string]any{"organisation_id": e.books.Org.String(), "note_key": "pay-rules", "note_version": 1, "draft": map[string]any{"title": "x", "summary": "s", "body": good}}); r.Status != http.StatusConflict {
		t.Errorf("a payroll note's draft: %d %s", r.Status, r.Raw)
	}
	if r := call(token, "drafts", map[string]any{"organisation_id": e.books.Org.String(), "note_key": "expense-policy", "note_version": 7, "draft": map[string]any{"title": "x", "summary": "s", "body": good}}); r.Status != http.StatusConflict {
		t.Errorf("a stale note's draft: %d %s", r.Status, r.Raw)
	}
	// A person edits the page, the note changes, and the recompile waits beside the person's version with a finding.
	edit := e.call(http.MethodPut, "/v1/knowledge-base/pages/org.expense-policy", owner, map[string]any{"type": "policy", "title": "Ours", "summary": "Edited.", "body": good,
		"sources": []map[string]any{{"kind": "note", "ref": "expense-policy"}}}, "If-Match", kbPageETag(1))
	if edit.Status != http.StatusCreated {
		t.Fatalf("the edit: %d %s", edit.Status, edit.Raw)
	}
	note := e.call(http.MethodGet, "/v1/knowledge-base/notes", owner, nil).Body["items"].([]any)[0].(map[string]any)
	if r := e.call(http.MethodPut, "/v1/knowledge-base/notes/expense-policy", owner, map[string]any{"kind": "policy", "title": "Expense policy", "body": "Claims are made within 14 days of the receipt date, in writing."},
		"If-Match", etag("v"+jsonNumber(note["version"]))); r.Status != http.StatusOK {
		t.Fatalf("the note's change: %d %s", r.Status, r.Raw)
	}
	model.answers = []ports.ModelResponse{page(strings.Replace(good, "receipt date.", "receipt date, in writing.", 1))}
	rep, err = kbcompile.Run(context.Background(), client, e.books.Org)
	if err != nil || rep.Drafts != 1 || rep.Findings != 1 {
		t.Fatalf("the recompile: %+v %v", rep, err)
	}
	var findings []storage.KBFinding
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		vs, err = u.OrganisationKnowledge().Versions(ctx, "org.expense-policy")
		if err != nil {
			return err
		}
		findings, err = u.OrganisationKnowledge().Findings(ctx, "open", 10)
		return err
	})
	if len(vs) != 3 || vs[0].Origin != "compiled" || vs[1].Origin != "human_edited" || vs[1].Title != "Ours" || len(findings) != 1 || findings[0].Kind != "human_edit_conflict" || findings[0].PageVersion != 2 {
		t.Errorf("the person's version beside the recompile: %+v %+v", vs, findings)
	}
	_ = authz.Owner
}

func jsonNumber(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
