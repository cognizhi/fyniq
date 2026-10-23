//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
)

// createTemplate makes an invoice email template as an Accountant and returns its ID and ETag.
func (e *env) createTemplate() (id, tag string) {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/document-templates", e.as(authz.Accountant), map[string]any{"kind": "email", "document_type": "invoice", "name": "Invoice email"})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createDocumentTemplate = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// blocks are a template's draft blocks, to change and send back.
func blocks(r response) []any { return r.Body["draft"].(map[string]any)["blocks"].([]any) }

// FR-TPL-04 and FR-TPL-05 through the API (rev 0.95): a template is made from the catalogue's default and saved as a
// draft; publishing makes numbered versions that never change, and a rollback publishes an earlier one again. In Founder
// mode an approver publishes alone, logged for the weekly review (FM-3); otherwise publishing is asked for, approved by
// another person who did not save the draft (SOD-3), and a change to the draft voids the request. The IRAS particulars
// of an invoice cannot be hidden (FR-TPL-03), and texts use only the typed catalogue (FR-TPL-02).
// req: FR-TPL-02
// req: FR-TPL-03
// req: FR-TPL-04
// req: FR-TPL-05
func TestDocumentTemplatesThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Owner, authz.Accountant)
	weiling := f.as(f.weiling, authz.Owner, authz.Accountant)
	auditor := e.as(authz.Auditor)

	c := e.call(http.MethodGet, "/v1/document-template-catalogue/invoice", auditor, nil)
	if c.Status != http.StatusOK || c.Body["locked_title"] != true || c.str("min_contrast") != "4.5" ||
		!strings.Contains(c.Raw, `{"kind":"totals","texts":[],"locked":true}`) || !strings.Contains(c.Raw, `{"kind":"header","texts":[],"locked":true}`) ||
		!strings.Contains(c.Raw, `{"path":"document.total_sgd","type":"money","label":"Total in SGD"}`) ||
		!strings.Contains(c.Raw, `{"type":"date","formats":["medium","long","short","iso"]}`) {
		t.Fatalf("the invoice catalogue: %d %s", c.Status, c.Raw)
	}
	if q := e.call(http.MethodGet, "/v1/document-template-catalogue/quote", auditor, nil); !strings.Contains(q.Raw, `{"kind":"header","texts":["title"],"locked":false}`) {
		t.Errorf("a quote's header takes a title: %s", q.Raw)
	}

	// Made from the default, as a draft: nothing is in force yet.
	r := e.call(http.MethodPost, "/v1/document-templates", ahkow, map[string]any{"kind": "document", "document_type": "invoice", "name": " Standard invoice "})
	if r.Status != http.StatusCreated || r.str("name") != "Standard invoice" || r.Body["version"] != float64(1) || r.Body["draft_changed"] != true ||
		r.Body["published_version"] != nil || r.str("saved_by", "name") != "Ah Kow" || r.Header.Get("Location") != "/v1/document-templates/"+r.str("id") ||
		r.Header.Get("ETag") != `"1"` || len(blocks(r)) != 11 {
		t.Fatalf("a new template: %d %s", r.Status, r.Raw)
	}
	id := r.str("id")
	draft := r.Body["draft"].(map[string]any)

	// Every problem at once: the totals hidden, a title on a tax invoice, a field not in the catalogue, a pale accent.
	bad := map[string]any{"schema": "fyniq.template/1", "page": draft["page"], "theme": map[string]any{"logo": true, "accent": "#FFFF00", "font": "Inter"},
		"blocks": []any{}}
	for i, b := range blocks(r) {
		b := b.(map[string]any)
		x := map[string]any{"kind": b["kind"], "shown": b["shown"], "texts": b["texts"]}
		switch i {
		case 0:
			x["texts"] = map[string]any{"title": "Invoice"}
		case 5:
			x["shown"] = false
		case 9:
			x["texts"] = map[string]any{"body": "{{customer.nmae}}"}
		}
		bad["blocks"] = append(bad["blocks"].([]any), x)
	}
	p := e.call(http.MethodPut, "/v1/document-templates/"+id, ahkow, map[string]any{"name": "Standard invoice", "content": bad}, "If-Match", `"1"`)
	e.problemIs(p, "replaceDocumentTemplate", 422, doctemplate.CodeTheme)
	for _, want := range []string{`"code":"TPL-07","field":"body.content.blocks[0].texts.title"`, `"code":"TPL-07","field":"body.content.blocks[5].shown"`,
		`"code":"TPL-09","field":"body.content.blocks[9].texts.body"`} {
		if !strings.Contains(p.Raw, want) {
			t.Errorf("the problems lack %s: %s", want, p.Raw)
		}
	}
	e.problemIs(e.call(http.MethodPost, "/v1/document-templates", ahkow, map[string]any{"kind": "email", "document_type": "invoice", "name": "x",
		"content": map[string]any{"schema": "fyniq.template/1", "subject": "Invoice\n{{document.number}}", "body": "Hi"}}),
		"createDocumentTemplate", 422, doctemplate.CodeSubject)

	// Saved: the draft changes at the next version; a stale ETag is refused.
	footer := blocks(r)[9].(map[string]any)
	footer["texts"] = map[string]any{"body": "{{organisation.name}} · GST {{organisation.gst_number}}"}
	save := func(who, tag string) response {
		return e.call(http.MethodPut, "/v1/document-templates/"+id, who, map[string]any{"name": "Standard invoice", "content": draft}, "If-Match", tag)
	}
	s := save(ahkow, `"1"`)
	if s.Status != http.StatusOK || s.Header.Get("ETag") != `"2"` || !strings.Contains(s.Raw, "GST {{organisation.gst_number}}") {
		t.Fatalf("saving the draft: %d %s", s.Status, s.Raw)
	}
	e.problemIs(save(ahkow, `"1"`), "replaceDocumentTemplate", 412, CodeStale)

	// Published alone in Founder mode, for the weekly review; the same content again is refused.
	pub := func(who, tag string, body any, headers ...string) response {
		return e.call(http.MethodPost, "/v1/document-templates/"+id+"/publish", who, body, append([]string{"If-Match", tag}, headers...)...)
	}
	if v := pub(ahkow, `"2"`, map[string]any{"note": "First"}, "Prefer", "return=preview"); v.Header.Get("Preference-Applied") != "return=preview" {
		t.Errorf("a preview: %s", v.Raw)
	}
	v1 := pub(ahkow, `"2"`, map[string]any{"note": "First"})
	if v1.Status != http.StatusOK || v1.Body["published_version"] != float64(1) || v1.Body["draft_changed"] != false || v1.Header.Get("ETag") != `"3"` {
		t.Fatalf("publishing: %d %s", v1.Status, v1.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/review-log", weiling, nil); !strings.Contains(l.Raw, `"kind":"self_approved","object_type":"document_template","object_key":"`+id+`"`) {
		t.Errorf("the publication is not in the review log: %s", l.Raw)
	}
	e.problemIs(pub(weiling, `"3"`, nil), "publishDocumentTemplate", 409, doctemplate.CodeUnchange)

	// A second version, then a rollback to the first as the third; a version that does not exist is refused.
	footer["texts"] = map[string]any{"body": "Thank you"}
	s = save(weiling, `"3"`)
	if v2 := pub(weiling, s.Header.Get("ETag"), map[string]any{"note": "Shorter footer"}); v2.Body["published_version"] != float64(2) {
		t.Fatalf("version 2: %s", v2.Raw)
	}
	e.problemIs(pub(weiling, `"5"`, map[string]any{"version": 9}), "publishDocumentTemplate", 422, doctemplate.CodeVersion)
	back := pub(weiling, `"5"`, map[string]any{"version": 1, "note": "Back to the GST footer"})
	if back.Status != http.StatusOK || back.Body["published_version"] != float64(3) || back.Body["draft_changed"] != true {
		t.Fatalf("rolling back: %d %s", back.Status, back.Raw)
	}
	vs := e.call(http.MethodGet, "/v1/document-templates/"+id+"/versions?page_size=2", auditor, nil)
	items, _ := vs.Body["items"].([]any)
	if vs.Status != http.StatusOK || len(items) != 2 || items[0].(map[string]any)["republished_from"] != float64(1) || vs.str("next_cursor") == "" ||
		strings.Contains(vs.Raw, `"content":`) {
		t.Fatalf("the versions: %d %s", vs.Status, vs.Raw)
	}
	if rest := e.call(http.MethodGet, "/v1/document-templates/"+id+"/versions?page_size=2&cursor="+vs.str("next_cursor"), auditor, nil); !strings.Contains(rest.Raw, `"number":1,`) ||
		strings.Contains(rest.Raw, `"next_cursor"`) {
		t.Errorf("the last page: %s", rest.Raw)
	}
	g := e.call(http.MethodGet, "/v1/document-templates/"+id+"/versions/3", auditor, nil)
	if g.Status != http.StatusOK || !strings.Contains(g.Raw, "GST {{organisation.gst_number}}") || g.str("note") != "Back to the GST footer" ||
		g.str("published_by", "name") != "Wei Ling" || len(g.str("content_hash")) != 64 {
		t.Errorf("version 3: %d %s", g.Status, g.Raw)
	}

	// Asked for: the one who saved the draft never approves it (SOD-3); a change to the draft voids the envelope.
	ask := func(who, tag string) response {
		return e.call(http.MethodPost, "/v1/document-templates/"+id+"/approval", who, map[string]any{"note": "Thank-you footer"}, "If-Match", tag)
	}
	asked := ask(ahkow, `"6"`)
	if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectTemplate || asked.str("operation") != approval.OpTemplatePublish ||
		!strings.Contains(asked.str("envelope"), `"published":3`) || !strings.Contains(asked.str("envelope"), `"template_version":6`) {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	weilingApprover := f.as(f.weiling, authz.Owner, authz.Controller)
	e.problemIs(f.decide(weilingApprover, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, doctemplate.CodeOwnDraft)
	e.call(http.MethodPost, "/v1/approvals/"+asked.str("id")+"/withdraw", ahkow, nil, "If-Match", `"1"`)

	s = save(ahkow, `"6"`) // Ah Kow saves the same draft: now his
	asked = ask(ahkow, s.Header.Get("ETag"))
	footer["texts"] = map[string]any{"body": "Thank you for your business"}
	s = save(ahkow, s.Header.Get("ETag"))
	e.problemIs(f.decide(weilingApprover, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 409, approval.CodeChanged)

	asked = ask(ahkow, s.Header.Get("ETag"))
	if d := f.decide(weilingApprover, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK || d.str("status") != "approved" {
		t.Fatalf("approving the publication: %d %s", d.Status, d.Raw)
	}
	got := e.call(http.MethodGet, "/v1/document-templates/"+id, auditor, nil)
	if got.Body["published_version"] != float64(4) || got.Body["draft_changed"] != false || got.Header.Get("ETag") != `"9"` {
		t.Fatalf("after the approval: %s", got.Raw)
	}
	if v4 := e.call(http.MethodGet, "/v1/document-templates/"+id+"/versions/4", auditor, nil); v4.str("published_by", "id") != f.weiling || v4.str("note") != "Thank-you footer" {
		t.Errorf("version 4 was published by its approver: %s", v4.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/document-templates?kind=document&document_type=invoice", auditor, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the invoice templates: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/document-templates?kind=email", auditor, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("no email templates: %s", l.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/document-templates/"+id, auditor, map[string]any{"name": "x", "content": draft}, "If-Match", `"9"`),
		"replaceDocumentTemplate", 403, CodeForbidden)
}

// In Standard mode a template's publication is always another person's approval (FR-TPL-04, FR-APR-01).
// req: FR-TPL-04
func TestTemplatesArePublishedByApprovalInStandardMode(t *testing.T) {
	e := newEnv(t)
	id, tag := e.createTemplate()
	standardMode(e)
	e.problemIs(e.call(http.MethodPost, "/v1/document-templates/"+id+"/publish", e.as(authz.Owner), nil, "If-Match", tag),
		"publishDocumentTemplate", 409, doctemplate.CodeApproval)
	if g := e.call(http.MethodGet, "/v1/document-templates/"+id, e.as(authz.Owner), nil); g.Body["published_version"] != nil {
		t.Errorf("published in Standard mode: %s", g.Raw)
	}
}

// FR-TPL-01's live preview through the API (rev 0.97): content, saved or not, filled with sample data, a document as a
// PDF from the render service with the particulars it lacks, an email as its subject and body; nothing is saved.
// req: FR-TPL-01
// req: FR-TPL-03
func TestTemplatesArePreviewed(t *testing.T) {
	e := newEnv(t)
	auditor := e.as(authz.Auditor)
	c := e.call(http.MethodGet, "/v1/document-template-catalogue/invoice", auditor, nil)
	defaults := c.Body["defaults"].(map[string]any)
	preview := func(kind string, content any) response {
		return e.call(http.MethodPost, "/v1/document-template-previews", auditor, map[string]any{"kind": kind, "document_type": "invoice", "content": content})
	}
	p := preview("document", defaults["document"])
	if p.Status != http.StatusOK || !strings.HasPrefix(p.str("pdf"), "JVBERi0") || len(p.str("sha256")) != 64 || p.Raw == "" ||
		!strings.Contains(p.Raw, `"missing_particulars":[]`) {
		t.Fatalf("a document preview: %d %.300s", p.Status, p.Raw)
	}
	if again := preview("document", defaults["document"]); again.str("sha256") != p.str("sha256") {
		t.Error("the same content previewed twice differs")
	}
	m := preview("email", defaults["email"])
	if m.Status != http.StatusOK || m.str("subject") != "Invoice INV-0042 from Example Analytics Pte Ltd" ||
		!strings.Contains(m.str("body"), "SGD 1,635.00, due on 22 November 2026") || m.Body["pdf"] != nil {
		t.Fatalf("an email preview: %d %s", m.Status, m.Raw)
	}
	e.problemIs(preview("email", map[string]any{"schema": "fyniq.template/1", "subject": "{{document.totl}}", "body": "x"}),
		"previewDocumentTemplate", 422, doctemplate.CodeField)

	// Without a render service no document previews, and no template with IRAS particulars publishes.
	id, tag := "", ""
	r := e.call(http.MethodPost, "/v1/document-templates", e.as(authz.Accountant), map[string]any{"kind": "document", "document_type": "credit_note", "name": "Credit note"})
	id, tag = r.str("id"), r.Header.Get("ETag")
	e.api.deps.Renderer = nil
	e.problemIs(preview("document", defaults["document"]), "previewDocumentTemplate", 503, doctemplate.CodeRenderer)
	e.problemIs(e.call(http.MethodPost, "/v1/document-templates/"+id+"/publish", e.as(authz.Owner), nil, "If-Match", tag),
		"publishDocumentTemplate", 503, doctemplate.CodeRenderer)
	// A render service that drops a particular fails the publication's test document.
	e.api.deps.Renderer = dropping{Renderer: testRenderer(t), missing: []string{"reason"}}
	e.problemIs(e.call(http.MethodPost, "/v1/document-templates/"+id+"/publish", e.as(authz.Owner), nil, "If-Match", tag),
		"publishDocumentTemplate", 422, doctemplate.CodeTest)
	e.api.deps.Renderer = testRenderer(t)
	if v := e.call(http.MethodPost, "/v1/document-templates/"+id+"/publish", e.as(authz.Owner), nil, "If-Match", tag); v.Status != http.StatusOK || v.Body["published_version"] != float64(1) {
		t.Fatalf("publishing the credit note: %d %s", v.Status, v.Raw)
	}
}
