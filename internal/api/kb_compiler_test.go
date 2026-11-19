//go:build local

package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/kbcompile"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The knowledge-base compiler holds its own service identity and the API's five operations for it, and nothing else (AI-52, AI-57;
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

// A person's Recompile queues the compiler job for the organisation, and the weekly lint finds what a person should look at without a
// model (AI-52, AI-55, AI-57): the request is audited and queued, refused where no compiler job is connected, refused to anyone but
// the Owner and the Administrator, and queues nothing as a preview; the job's lint raises a finding for a note with no page once, and
// the compiler's token opens it, and another service's does not.
// asvs: V8.2.1, V8.2.2
// req: AI-55
// req: AUD-01
func TestARecompileQueuesTheCompilerAndTheWeeklyLintRaisesFindings(t *testing.T) {
	e := newEnv(t)
	shippedKB(t)
	f := foundersIn(e)
	owner := f.as(f.ahkow)
	for _, n := range []map[string]any{
		{"key": "expense-policy", "kind": "policy", "title": "Expense policy", "body": "Claims are made within 14 days of the receipt date."},
	} {
		if r := e.call(http.MethodPost, "/v1/knowledge-base/notes", owner, n); r.Status != http.StatusCreated {
			t.Fatalf("the note: %d %s", r.Status, r.Raw)
		}
	}
	waiting := func() *storage.OutboxRow {
		var row *storage.OutboxRow
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			row, err = u.Outbox().Waiting(ctx, kbcompile.Kind)
			return err
		})
		return row
	}
	// No compiler job connected: refused with its code, and nothing queued.
	e.problemIs(e.call(http.MethodPost, "/v1/knowledge-base/recompile", owner, nil, "Idempotency-Key", "rc-0"), "recompileKnowledgeBase", http.StatusServiceUnavailable, CodeKBCompilerOff)
	e.api.deps.KBCompiler = true
	// Only a person with a right to the knowledge base asks, and a preview queues nothing.
	if r := e.call(http.MethodPost, "/v1/knowledge-base/recompile", f.as(f.ahkow, authz.Accountant), nil, "Idempotency-Key", "rc-1"); r.Status != http.StatusForbidden {
		t.Errorf("an Accountant's recompile: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/knowledge-base/recompile", owner, nil, "Idempotency-Key", "rc-2", "Prefer", "return=preview"); r.Status != http.StatusOK || r.Header.Get("Preference-Applied") != "return=preview" || waiting() != nil {
		t.Errorf("a preview: %d %s", r.Status, r.Raw)
	}
	// The AI is off: the request says only the lint will run. The same key replays the saved answer and queues one job.
	off := e.call(http.MethodPost, "/v1/knowledge-base/recompile", owner, nil, "Idempotency-Key", "rc-3")
	if off.Status != http.StatusAccepted || off.Body["queued"] != true || off.Body["compiles"] != false {
		t.Fatalf("the recompile with the AI off: %d %s", off.Status, off.Raw)
	}
	if again := e.call(http.MethodPost, "/v1/knowledge-base/recompile", owner, nil, "Idempotency-Key", "rc-3"); again.Status != http.StatusAccepted || again.Header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("the replay: %d %s", again.Status, again.Raw)
	}
	row := waiting()
	if row == nil || row.Class != storage.ClassWorker || !strings.Contains(string(row.Payload), `"compile": false`) {
		t.Fatalf("the queued job: %+v", row)
	}
	aiSwitchedOn(e)
	if on := e.call(http.MethodPost, "/v1/knowledge-base/recompile", owner, nil, "Idempotency-Key", "rc-4"); on.Status != http.StatusAccepted || on.Body["compiles"] != true {
		t.Errorf("the recompile with the AI on: %d %s", on.Status, on.Raw)
	}
	// The request is audited, by the person, in the same transaction that queued the job (AUD-01).
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		evs, err := u.Audit().Find(ctx, storage.AuditQuery{Action: "kb.recompile_requested", Limit: 10})
		if err != nil || len(evs) != 2 || !strings.Contains(evs[0].Body, `"type":"user"`) {
			t.Errorf("the audit events of the recompile: %+v %v", evs, err)
		}
		return nil
	})
	// The job's work as the compiler: the lint finds the note with no page and raises it once; nothing is published.
	token, err := e.services.Token(kbcompile.Class)
	if err != nil {
		t.Fatal(err)
	}
	client := kbcompile.APIClient{URL: e.srv.URL + "/internal/v1", Token: func(context.Context) (string, error) { return token, nil }, HTTP: e.srv.Client()}
	if err := kbcompile.Work(client, slog.Default())(context.Background(), e.books.Org, *row); err != nil {
		t.Fatal(err)
	}
	if err := kbcompile.Work(client, slog.Default())(context.Background(), e.books.Org, *row); err != nil {
		t.Fatal(err)
	}
	var open []storage.KBFinding
	var published map[string]int
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if open, err = u.OrganisationKnowledge().Findings(ctx, "open", 20); err != nil {
			return err
		}
		published, err = u.OrganisationKnowledge().Published(ctx)
		return err
	})
	if len(open) != 1 || open[0].Kind != "source_without_page" || open[0].PageKey != "org.expense-policy" || len(published) != 0 {
		t.Errorf("after the weekly lint: findings %+v published %v", open, published)
	}
	// The lint's advice does not stop the page that answers it: the person writes the page and publishes it with the finding still open.
	const good = "# Expense policy\n\n## Claims\n\nClaims are made within 14 days of the receipt date. (src: note:expense-policy#claims)\n"
	if r := e.call(http.MethodPut, "/v1/knowledge-base/pages/org.expense-policy", owner, map[string]any{"type": "policy", "title": "Expense policy", "summary": "How claims are made here.",
		"body": good, "sources": []map[string]any{{"kind": "note", "ref": "expense-policy"}}}, "If-Match", kbPageETag(0)); r.Status != http.StatusCreated {
		t.Fatalf("the page: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/knowledge-base/publications", owner, map[string]any{"pages": []any{map[string]any{"key": "org.expense-policy", "version": 1}}}); r.Status != http.StatusCreated {
		t.Errorf("a publication with the weekly lint's finding open: %d %s", r.Status, r.Raw)
	}
	// Another service's token does not open the lint.
	aiToken, _ := e.services.Token(ServiceAI)
	if r := e.call(http.MethodPost, "/internal/v1/kb-compiler/lint", "", map[string]any{"organisation_id": e.books.Org.String()}, ServiceTokenHeader, aiToken); r.Status != http.StatusUnauthorized {
		t.Errorf("the AI service's token on the lint: %d %s", r.Status, r.Raw)
	}
}
