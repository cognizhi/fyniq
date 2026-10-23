package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// TemplateContent is what a template version holds (FR-TPL-01, FR-TPL-02): a document's page, theme and blocks, or an
// email's subject and body. It is data, never code: texts hold words and fields from the catalogue
// (getDocumentTemplateCatalogue), written {{customer.name}} or {{document.date|long}}.
type TemplateContent struct {
	Schema  string          `json:"schema" enum:"fyniq.template/1" doc:"The content's format." example:"fyniq.template/1"`
	Page    *TemplatePage   `json:"page,omitempty" doc:"A document's paper and margins; an email has none."`
	Theme   *TemplateTheme  `json:"theme,omitempty" doc:"A document's look; an email has none."`
	Blocks  []TemplateBlock `json:"blocks,omitempty" maxItems:"30" doc:"A document's blocks, in order; an email has none."`
	Subject string          `json:"subject,omitempty" maxLength:"200" doc:"An email's subject, on one line; a document has none." example:"Invoice {{document.number}} from {{organisation.name}}"`
	Body    string          `json:"body,omitempty" maxLength:"10000" doc:"An email's text; a document has none." example:"Dear {{customer.name}},\n\nPlease find attached invoice {{document.number}}."`
}

// TemplatePage is a document's paper and margins.
type TemplatePage struct {
	Size    string          `json:"size" enum:"A4,Letter" doc:"The paper size." example:"A4"`
	Margins TemplateMargins `json:"margins_mm" doc:"The margins in whole millimetres, 10 to 40."`
}

// TemplateMargins are a page's margins in millimetres.
type TemplateMargins struct {
	Top    int `json:"top" doc:"The top margin." example:"20"`
	Right  int `json:"right" doc:"The right margin." example:"18"`
	Bottom int `json:"bottom" doc:"The bottom margin." example:"20"`
	Left   int `json:"left" doc:"The left margin." example:"18"`
}

// TemplateTheme is a document's look.
type TemplateTheme struct {
	Logo   bool   `json:"logo" doc:"Show the organisation's logo (FR-ORG-09) in the header." example:"true"`
	Accent string `json:"accent" pattern:"^#[0-9A-Fa-f]{6}$" doc:"The accent colour, #RRGGBB, at least 4.5:1 against white so text in it reads (TPL-05)." example:"#0F766E"`
	Font   string `json:"font" enum:"Inter,IBM Plex Sans,IBM Plex Serif" doc:"The font, from those the render service ships." example:"Inter"`
	Style  string `json:"style,omitempty" enum:"classic,band,minimal,compact" doc:"The look the renderer draws (FR-TPL-07, rev 0.98): classic, band (an accent band behind the header and the table's head), minimal or compact. Left out: classic." example:"band"`
}

// TemplateBlock is one block of a document.
type TemplateBlock struct {
	Kind  string         `json:"kind" enum:"header,our_details,recipient,document_details,line_items,totals,payment_details,notes,signature,footer,text" doc:"What the block is; the catalogue lists the ones each document type takes." example:"recipient"`
	Shown bool           `json:"shown" doc:"Shown, or hidden. A block carrying IRAS particulars on an invoice or credit note is always shown (FR-TPL-03)." example:"true"`
	Texts *TemplateTexts `json:"texts,omitempty" doc:"The block's texts; the rest of it comes from the document."`
}

// TemplateTexts are a block's texts: the catalogue lists which each block takes.
type TemplateTexts struct {
	Title string `json:"title,omitempty" maxLength:"80" doc:"The header's title. An invoice's is \"Tax Invoice\" when the organisation is GST-registered, never a text (FR-TPL-03)." example:"Quotation"`
	Label string `json:"label,omitempty" maxLength:"80" doc:"The block's label, on one line." example:"Bill to"`
	Body  string `json:"body,omitempty" maxLength:"2000" doc:"The block's text, with fields." example:"Please pay by {{document.due_date|long}}."`
}

// DocumentTemplate is a document or email template (FR-TPL-04, FR-TPL-05): its draft and where its versions stand.
type DocumentTemplate struct {
	ID               string          `json:"id" format:"uuid" doc:"The template's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f10"`
	Kind             string          `json:"kind" enum:"document,email" doc:"document: the PDF; email: the message that carries it." example:"document"`
	DocumentType     string          `json:"document_type" enum:"invoice,quote,credit_note,receipt,statement,reminder,remittance,purchase_order" doc:"The document it serves." example:"invoice"`
	Name             string          `json:"name" doc:"Its name." example:"Standard invoice"`
	Version          int             `json:"version" doc:"The version of the template, which saving the draft and publishing move forward; its ETag." example:"3"`
	Draft            TemplateContent `json:"draft" doc:"The draft: what saving changes and publishing makes a version of."`
	PublishedVersion int             `json:"published_version,omitempty" doc:"The number of the version in force; absent until the first is published." example:"2"`
	DraftChanged     bool            `json:"draft_changed" doc:"The draft differs from the version in force, or nothing is published yet." example:"true"`
	SavedBy          *Person         `json:"saved_by,omitempty" doc:"Who saved the draft last."`
	SavedAt          string          `json:"saved_at" format:"date-time" doc:"When." example:"2026-10-23T09:30:00Z"`
}

// DocumentTemplatePage is one page of templates.
type DocumentTemplatePage struct {
	Items      []DocumentTemplate `json:"items" nullable:"false" doc:"The templates, in the order they were made."`
	NextCursor string             `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// DocumentTemplateDraft is a new template.
type DocumentTemplateDraft struct {
	Kind         string           `json:"kind" enum:"document,email" doc:"document: the PDF; email: the message that carries it." example:"document"`
	DocumentType string           `json:"document_type" enum:"invoice,quote,credit_note,receipt,statement,reminder,remittance,purchase_order" doc:"The document it serves." example:"invoice"`
	Name         string           `json:"name" maxLength:"100" doc:"Its name." example:"Standard invoice"`
	Content      *TemplateContent `json:"content,omitempty" doc:"The draft's content. Left out, the template starts from the starter named, or the classic starter."`
	Starter      string           `json:"starter,omitempty" enum:"classic,band,minimal,compact" doc:"The starter template to begin from (FR-TPL-07), when content is left out." example:"band"`
}

// DocumentTemplateChanges are a template's name and draft to save.
type DocumentTemplateChanges struct {
	Name    string          `json:"name" maxLength:"100" doc:"Its name." example:"Standard invoice"`
	Content TemplateContent `json:"content" doc:"The draft's content, whole."`
}

// TemplatePublication publishes a template's draft or, for a rollback, an earlier version again (FR-TPL-04).
type TemplatePublication struct {
	Note    string `json:"note,omitempty" maxLength:"500" doc:"Why, for the version history." example:"New PayNow details"`
	Version int    `json:"version,omitempty" minimum:"1" doc:"A rollback: publish this earlier version's content again, as the next version. Left out, the draft is published." example:"2"`
}

// TemplateVersion is a published version of a template, which never changes (FY176).
type TemplateVersion struct {
	Number          int              `json:"number" doc:"Its number, from 1." example:"2"`
	Content         *TemplateContent `json:"content,omitempty" doc:"What it holds; in a list, absent: read the version for it."`
	Hash            string           `json:"content_hash" doc:"The SHA-256 of its content's RFC 8785 JSON, in hex." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Note            string           `json:"note,omitempty" doc:"Why it was published." example:"New PayNow details"`
	RepublishedFrom int              `json:"republished_from,omitempty" doc:"A rollback: the earlier version whose content it holds." example:"1"`
	PublishedBy     *Person          `json:"published_by,omitempty" doc:"Who published it: the approver, when another person approved it."`
	PublishedAt     string           `json:"published_at" format:"date-time" doc:"When." example:"2026-10-23T10:00:00Z"`
}

// TemplateVersionPage is one page of a template's versions, newest first.
type TemplateVersionPage struct {
	Items      []TemplateVersion `json:"items" nullable:"false" doc:"The versions, newest first, without their content."`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIifQ"`
}

// TemplateCatalogue is what templates of a document type are made of (FR-TPL-01, FR-TPL-02, FR-TPL-03).
type TemplateCatalogue struct {
	DocumentType string                `json:"document_type" doc:"The document type." example:"invoice"`
	Blocks       []TemplateBlockKind   `json:"blocks" nullable:"false" doc:"The blocks it takes, in the starter order."`
	Fields       []TemplateField       `json:"fields" nullable:"false" doc:"The typed field catalogue its texts use."`
	Formats      []TemplateFormatGroup `json:"formats" nullable:"false" doc:"The formats each field type takes; the first is the default."`
	Fonts        []TemplateChoice      `json:"fonts" nullable:"false" doc:"The fonts the render service ships."`
	PageSizes    []TemplateChoice      `json:"page_sizes" nullable:"false" doc:"The paper sizes."`
	MarginMin    int                   `json:"margin_min_mm" doc:"The least margin, in millimetres." example:"10"`
	MarginMax    int                   `json:"margin_max_mm" doc:"The most margin, in millimetres." example:"40"`
	MinContrast  string                `json:"min_contrast" doc:"The least contrast of the accent colour against white." example:"4.5"`
	LockedTitle  bool                  `json:"locked_title" doc:"The renderer writes the title (an invoice's \"Tax Invoice\"), so the header takes none." example:"true"`
	Defaults     TemplateDefaults      `json:"defaults" doc:"The content a new template starts from."`
	Starters     []TemplateStarter     `json:"starters" nullable:"false" doc:"The starter gallery (FR-TPL-07): one per style, the first the default."`
}

// TemplateStarter is a starter template of the gallery, for a document type.
type TemplateStarter struct {
	Key         string          `json:"key" enum:"classic,band,minimal,compact" doc:"Its key, which createDocumentTemplate takes as starter." example:"band"`
	Name        string          `json:"name" doc:"Its name." example:"Band"`
	Description string          `json:"description" doc:"What it looks like." example:"IBM Plex Sans, a navy band behind the header and the table's head"`
	Content     TemplateContent `json:"content" doc:"Its content for the document type."`
}

// TemplateChoice is one name from a list of the catalogue: a font, a paper size, a text or a format.
type TemplateChoice string

// Schema implements huma.SchemaProvider: a string, with an example.
func (TemplateChoice) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "One name from the catalogue's list, such as a font, a paper size, a text or a format.", Examples: []any{"medium"}}
}

func choices(xs []string) []TemplateChoice {
	out := make([]TemplateChoice, len(xs))
	for i, x := range xs {
		out[i] = TemplateChoice(x)
	}
	return out
}

// TemplateBlockKind is a block a document type takes.
type TemplateBlockKind struct {
	Kind   string           `json:"kind" doc:"The block." example:"totals"`
	Texts  []TemplateChoice `json:"texts" nullable:"false" doc:"The texts it takes: title, label or body."`
	Locked bool             `json:"locked" doc:"It carries IRAS particulars, so it is always present and shown (FR-TPL-03)." example:"true"`
}

// TemplateField is one field of the catalogue.
type TemplateField struct {
	Path  string `json:"path" doc:"How a text writes it, inside {{ }}." example:"document.due_date"`
	Type  string `json:"type" enum:"text,date,money,number" doc:"What it is, which decides its formats." example:"date"`
	Label string `json:"label" doc:"What it is, in words." example:"Due date"`
}

// TemplateFormatGroup is the formats a field type takes.
type TemplateFormatGroup struct {
	Type    string           `json:"type" enum:"text,date,money,number" doc:"The field type." example:"money"`
	Formats []TemplateChoice `json:"formats" nullable:"false" doc:"Its formats, the default first; none for text and numbers."`
}

// TemplateDefaults are the contents a new template starts from.
type TemplateDefaults struct {
	Document TemplateContent `json:"document" doc:"A document template's."`
	Email    TemplateContent `json:"email" doc:"An email template's."`
}

// TemplateCodes are a template's rules (internal/domain/doctemplate).
var TemplateCodes = []string{doctemplate.CodeKind, doctemplate.CodeName, doctemplate.CodeSchema, doctemplate.CodePage, doctemplate.CodeTheme,
	doctemplate.CodeBlock, doctemplate.CodeLocked, doctemplate.CodeText, doctemplate.CodeField, doctemplate.CodeSubject, doctemplate.CodeBody}

// TemplateApprovalCodes are the codes an approval of a template's publication may answer: SOD-3, and the rules the
// content meets when it is published.
func TemplateApprovalCodes() []string {
	return append([]string{doctemplate.CodeOwnDraft, doctemplate.CodeUnchange, doctemplate.CodeVersion, doctemplate.CodeTest, doctemplate.CodeRenderer,
		storage.CodeTemplate}, TemplateCodes...)
}

// TemplatePath names a template.
type TemplatePath struct {
	TemplateID string `path:"template_id" format:"uuid" doc:"The template's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f10"`
}

type templateListInput struct {
	PageQuery
	Kind         string `query:"kind" enum:"document,email" doc:"Only templates of this kind." example:"document"`
	DocumentType string `query:"document_type" enum:"invoice,quote,credit_note,receipt,statement,reminder,remittance,purchase_order" doc:"Only templates of this document type." example:"invoice"`
}

type templateCreateInput struct {
	KeyHeader
	PreferHeader
	Body DocumentTemplateDraft
}

type templateReplaceInput struct {
	TemplatePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body DocumentTemplateChanges
}

type templatePublishInput struct {
	TemplatePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body *TemplatePublication `required:"false"`
}

type templateVersionsInput struct {
	TemplatePath
	PageQuery
}

type templateVersionInput struct {
	TemplatePath
	Number int `path:"number" minimum:"1" doc:"The version's number." example:"2"`
}

type templateCatalogueInput struct {
	DocumentType string `path:"document_type" enum:"invoice,quote,credit_note,receipt,statement,reminder,remittance,purchase_order" doc:"The document type." example:"invoice"`
}

type templateListOutput struct{ Body DocumentTemplatePage }

type templateReadOutput struct {
	ETag string `header:"ETag" doc:"The template's version, for If-Match."`
	Body DocumentTemplate
}

type templateWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The template's version, for If-Match."`
	Location string `header:"Location" doc:"The template's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     DocumentTemplate
}

type templateRequestOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was asked (FR-INT-14)."`
	Body     Approval
}

type templateVersionsOutput struct{ Body TemplateVersionPage }

type templateVersionOutput struct{ Body TemplateVersion }

type templateCatalogueOutput struct{ Body TemplateCatalogue }

func contentIn(c TemplateContent) doctemplate.Content {
	out := doctemplate.Content{Schema: c.Schema, Subject: c.Subject, Body: c.Body}
	if c.Page != nil {
		m := c.Page.Margins
		out.Page = &doctemplate.Page{Size: c.Page.Size, Margins: doctemplate.Margins{Top: m.Top, Right: m.Right, Bottom: m.Bottom, Left: m.Left}}
	}
	if c.Theme != nil {
		out.Theme = &doctemplate.Theme{Logo: c.Theme.Logo, Accent: c.Theme.Accent, Font: c.Theme.Font, Style: c.Theme.Style}
	}
	for _, b := range c.Blocks {
		x := doctemplate.Block{Kind: b.Kind, Shown: b.Shown}
		if b.Texts != nil {
			x.Texts = map[string]string{}
			for name, v := range map[string]string{doctemplate.TextTitle: b.Texts.Title, doctemplate.TextLabel: b.Texts.Label, doctemplate.TextBody: b.Texts.Body} {
				if v != "" {
					x.Texts[name] = v
				}
			}
		}
		out.Blocks = append(out.Blocks, x)
	}
	return out
}

func contentOut(c doctemplate.Content) TemplateContent {
	out := TemplateContent{Schema: c.Schema, Subject: c.Subject, Body: c.Body}
	if c.Page != nil {
		m := c.Page.Margins
		out.Page = &TemplatePage{Size: c.Page.Size, Margins: TemplateMargins{Top: m.Top, Right: m.Right, Bottom: m.Bottom, Left: m.Left}}
	}
	if c.Theme != nil {
		out.Theme = &TemplateTheme{Logo: c.Theme.Logo, Accent: c.Theme.Accent, Font: c.Theme.Font, Style: c.Theme.Style}
	}
	for _, b := range c.Blocks {
		x := TemplateBlock{Kind: b.Kind, Shown: b.Shown}
		if len(b.Texts) > 0 {
			x.Texts = &TemplateTexts{Title: b.Texts[doctemplate.TextTitle], Label: b.Texts[doctemplate.TextLabel], Body: b.Texts[doctemplate.TextBody]}
		}
		out.Blocks = append(out.Blocks, x)
	}
	return out
}

// templateProblems answers every rule a template breaks at once (ENG-23).
func templateProblems(ps []doctemplate.Problem) error {
	if len(ps) == 0 {
		return nil
	}
	fields := make([]FieldProblem, 0, len(ps))
	for _, x := range ps {
		fields = append(fields, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	return problem(ps[0].Code, fmt.Sprintf("The template cannot be saved: %d problem(s), each listed in errors.", len(ps)),
		"Correct the template and send it again.", fields...)
}

// templateOf is the template as the API shows it; draft_changed compares the draft's hash with the version in force.
func (a *API) templateOf(ctx context.Context, u storage.UnitOfWork, t storage.DocumentTemplate) (DocumentTemplate, error) {
	c, err := doctemplate.Read(t.Draft)
	if err != nil {
		return DocumentTemplate{}, err
	}
	out := DocumentTemplate{ID: t.ID.String(), Kind: t.Kind, DocumentType: t.DocumentType, Name: t.Name, Version: t.Version, Draft: contentOut(c),
		PublishedVersion: t.Published, DraftChanged: true, SavedBy: personOf(t.SavedBy), SavedAt: t.SavedAt.UTC().Format(time.RFC3339)}
	if t.Published > 0 {
		v, err := u.DocumentTemplates().Version(ctx, t.ID, t.Published)
		if err != nil {
			return DocumentTemplate{}, err
		}
		h, err := doctemplate.Hash(c)
		if err != nil {
			return DocumentTemplate{}, err
		}
		out.DraftChanged = !bytes.Equal(h, v.Hash)
	}
	return out, nil
}

func templateVersionOut(v storage.TemplateVersion, withContent bool) (TemplateVersion, error) {
	out := TemplateVersion{Number: v.Number, Hash: hex.EncodeToString(v.Hash), Note: v.Note, RepublishedFrom: v.RepublishedFrom,
		PublishedBy: personOf(v.PublishedBy), PublishedAt: v.PublishedAt.UTC().Format(time.RFC3339)}
	if withContent {
		c, err := doctemplate.Read(v.Content)
		if err != nil {
			return TemplateVersion{}, err
		}
		cc := contentOut(c)
		out.Content = &cc
	}
	return out, nil
}

// templateContent is what a publication of t would publish: the draft, or an earlier version's content again; with its
// hash. It refuses a version that does not exist.
func templateContent(ctx context.Context, u storage.UnitOfWork, t storage.DocumentTemplate, from int) (doctemplate.Content, []byte, error) {
	raw := t.Draft
	if from > 0 {
		v, err := u.DocumentTemplates().Version(ctx, t.ID, from)
		if errors.Is(err, ports.ErrNotFound) {
			return doctemplate.Content{}, nil, problem(doctemplate.CodeVersion, fmt.Sprintf("The template has no version %d.", from),
				"List its versions and choose one of them", FieldProblem{Code: doctemplate.CodeVersion, Field: "body.version",
					Message: fmt.Sprintf("version %d does not exist", from), Fix: "Choose a published version"})
		}
		if err != nil {
			return doctemplate.Content{}, nil, err
		}
		raw = v.Content
	}
	c, err := doctemplate.Read(raw)
	if err != nil {
		return doctemplate.Content{}, nil, err
	}
	h, err := doctemplate.Hash(c)
	return c, h, err
}

// toPublish is templateContent, refused when the content breaks a rule now or is the version in force.
func (a *API) toPublish(ctx context.Context, u storage.UnitOfWork, t storage.DocumentTemplate, from int) (doctemplate.Content, []byte, error) {
	c, h, err := templateContent(ctx, u, t, from)
	if err != nil {
		return doctemplate.Content{}, nil, err
	}
	if err := templateProblems(doctemplate.Check(t.Kind, t.DocumentType, c)); err != nil {
		return doctemplate.Content{}, nil, err
	}
	if t.Published > 0 {
		cur, err := u.DocumentTemplates().Version(ctx, t.ID, t.Published)
		if err != nil {
			return doctemplate.Content{}, nil, err
		}
		if bytes.Equal(cur.Hash, h) {
			return doctemplate.Content{}, nil, problem(doctemplate.CodeUnchange, fmt.Sprintf("That is the content of version %d, which is in force already.", t.Published),
				"Change the draft first, or roll back to another version")
		}
	}
	return c, h, a.testDocuments(ctx, t, c)
}

// testDocuments renders a template with IRAS particulars as test documents, a full and a simplified tax invoice or a
// credit note of the sample data, and refuses it when one lacks a particular (FR-TPL-03): proof in the compiled
// document, beside the rules that keep the blocks shown.
func (a *API) testDocuments(ctx context.Context, t storage.DocumentTemplate, c doctemplate.Content) error {
	if t.Kind != doctemplate.KindDocument || len(doctemplate.Locked[t.DocumentType]) == 0 {
		return nil
	}
	variants := []render.Data{render.Sample(t.DocumentType)}
	if t.DocumentType == doctemplate.Invoice {
		simplified := render.Sample(t.DocumentType)
		simplified.Simplified = true
		variants = append(variants, simplified)
	}
	for _, d := range variants {
		res, err := a.renderDocument(ctx, t.DocumentType, c, d, true)
		if err != nil {
			return err
		}
		if len(res.Missing) > 0 {
			what := "test " + strings.ReplaceAll(t.DocumentType, "_", " ")
			if d.Simplified {
				what = "simplified test invoice"
			}
			return problem(doctemplate.CodeTest, fmt.Sprintf("The %s rendered without %s, which IRAS requires.", what, strings.Join(res.Missing, ", ")),
				"Show the blocks that carry them; if they are shown, report the template, as the renderer dropped them")
		}
	}
	return nil
}

// sampleCreated is when a sample document was issued: its date, at midnight in UTC, so a preview is the same bytes.
func sampleCreated(d render.Data) time.Time {
	day, err := time.Parse(time.DateOnly, d.Fields["document.date"].Date)
	if err != nil {
		return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return day
}

// renderDocument renders c with d through the render service, finding the particulars when check is set.
func (a *API) renderDocument(ctx context.Context, documentType string, c doctemplate.Content, d render.Data, check bool) (render.Response, error) {
	if a.deps.Renderer == nil {
		return render.Response{}, problem(doctemplate.CodeRenderer, "No render service is configured, so the document cannot be rendered.",
			"Ask the operator to configure the render service; templates without IRAS particulars and emails publish without it")
	}
	res, err := a.deps.Renderer.Render(ctx, render.Request{DocumentType: documentType, Content: c, Data: d, Created: sampleCreated(d), Check: check})
	var rp *render.Problem
	switch {
	case err == nil:
		return res, nil
	case errors.As(err, &rp) && rp.Code == render.CodeTemplate && len(rp.Errors) > 0:
		return render.Response{}, templateProblems(rp.Errors)
	case errors.As(err, &rp) && rp.Code == render.CodeBusy:
		return render.Response{}, problem(doctemplate.CodeRenderer, "The render service is busy.", "Try again in a moment").withHeader("Retry-After", "1")
	default:
		if a.deps.Log != nil {
			a.deps.Log.ErrorContext(ctx, "render", "error", err.Error())
		}
		return render.Response{}, problem(doctemplate.CodeRenderer, "The render service did not render the document.", "Try again; if it persists, report it")
	}
}

// publishTemplate publishes the draft of the template id, or the earlier version from, as its next version, at the
// template's version read.
func (a *API) publishTemplate(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, version, from int, note string) (storage.DocumentTemplate, storage.TemplateVersion, error) {
	t, err := u.DocumentTemplates().Lock(ctx, id)
	if err != nil {
		return storage.DocumentTemplate{}, storage.TemplateVersion{}, err
	}
	c, h, err := a.toPublish(ctx, u, t, from)
	if err != nil {
		return storage.DocumentTemplate{}, storage.TemplateVersion{}, err
	}
	raw, err := doctemplate.Canonical(c)
	if err != nil {
		return storage.DocumentTemplate{}, storage.TemplateVersion{}, err
	}
	return u.DocumentTemplates().Publish(ctx, id, version, storage.NewTemplateVersion{Content: raw, Hash: h, Note: strings.TrimSpace(note), RepublishedFrom: from})
}

func publicationOf(b *TemplatePublication) (string, int) {
	if b == nil {
		return "", 0
	}
	return strings.TrimSpace(b.Note), b.Version
}

func registerDocumentTemplates(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListDocumentTemplates, huma.Operation{
		OperationID: "listDocumentTemplates", Method: http.MethodGet, Path: "/v1/document-templates", Tags: []string{"Templates"},
		Summary: "List document and email templates",
		Description: "The organisation's templates for invoices, quotes, credit notes, receipts, statements, reminders, remittances and " +
			"purchase orders, and the emails that carry them (FR-TPL-05), a page at a time, each with its draft and the version in force " +
			"(FR-TPL-04). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *templateListInput) (*templateListOutput, error) {
		after := uuid.Nil
		if in.Cursor != "" {
			s, err := decodeCursor(in.Cursor)
			if err != nil {
				return nil, err
			}
			if after, err = uuid.Parse(s); err != nil {
				return nil, badCursor()
			}
		}
		p, _ := authn.From(ctx)
		out := templateListOutput{Body: DocumentTemplatePage{Items: []DocumentTemplate{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			ts, err := u.DocumentTemplates().Page(ctx, after, in.PageSize+1, storage.TemplateFilter{Kind: in.Kind, DocumentType: in.DocumentType})
			if err != nil {
				return err
			}
			if len(ts) > in.PageSize {
				ts = ts[:in.PageSize]
				out.Body.NextCursor = encodeCursor(ts[len(ts)-1].ID.String())
			}
			var people []*Person
			for _, t := range ts {
				x, err := a.templateOf(ctx, u, t)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, x)
			}
			for i := range out.Body.Items {
				people = append(people, out.Body.Items[i].SavedBy)
			}
			return a.name(ctx, u, people...)
		})
		return &out, err
	})

	register(a, authz.CreateDocumentTemplate, huma.Operation{
		OperationID: "createDocumentTemplate", Method: http.MethodPost, Path: "/v1/document-templates", Tags: []string{"Templates"},
		DefaultStatus: http.StatusCreated, Summary: "Make a template",
		Description: "Makes a document or email template as a draft (FR-TPL-04, FR-TPL-05), from the content sent or, left out, the " +
			"catalogue's default for its kind and document type. Every rule the content breaks comes back together (TPL-01 to TPL-11): " +
			"blocks the type takes, the IRAS particulars kept on invoices and credit notes (FR-TPL-03), fields from the typed catalogue " +
			"(FR-TPL-02), a readable accent colour. Nothing is in force until a version is published. Side effects: the template and its " +
			"audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld},
		append(locks, TemplateCodes...)...),
		func(ctx context.Context, in *templateCreateInput) (*templateWriteOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (DocumentTemplate, error) {
				b := in.Body
				st, _ := doctemplate.StarterKey(cmpOr(b.Starter, doctemplate.Starters[0].Key))
				c := doctemplate.Starter(st, b.Kind, b.DocumentType)
				if b.Content != nil && b.Starter != "" {
					return DocumentTemplate{}, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.starter", Message: "is given with content",
						Fix: "Send the content, or the starter to begin from, not both"}})
				}
				if b.Content != nil {
					c = contentIn(*b.Content)
				}
				name := strings.TrimSpace(b.Name)
				if err := templateProblems(append(doctemplate.CheckName(name), doctemplate.Check(b.Kind, b.DocumentType, c)...)); err != nil {
					return DocumentTemplate{}, err
				}
				raw, err := doctemplate.Canonical(c)
				if err != nil {
					return DocumentTemplate{}, err
				}
				t, err := u.DocumentTemplates().Create(ctx, storage.NewDocumentTemplate{Kind: b.Kind, DocumentType: b.DocumentType, Name: name, Draft: raw})
				if err != nil {
					return DocumentTemplate{}, err
				}
				out, err := a.templateOf(ctx, u, t)
				if err != nil {
					return DocumentTemplate{}, err
				}
				return out, a.name(ctx, u, out.SavedBy)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &templateWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &templateWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Location: "/v1/document-templates/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetDocumentTemplate, huma.Operation{
		OperationID: "getDocumentTemplate", Method: http.MethodGet, Path: "/v1/document-templates/{template_id}", Tags: []string{"Templates"},
		Summary:     "Read a template",
		Description: "A template with its draft, the version in force and whether the draft differs from it (FR-TPL-04). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *struct{ TemplatePath }) (*templateReadOutput, error) {
		id, err := parseID("template_id", in.TemplateID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out DocumentTemplate
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			t, err := u.DocumentTemplates().Get(ctx, id)
			if err != nil {
				return err
			}
			if out, err = a.templateOf(ctx, u, t); err != nil {
				return err
			}
			return a.name(ctx, u, out.SavedBy)
		})
		if err != nil {
			return nil, err
		}
		return &templateReadOutput{ETag: etag(out.Version), Body: out}, nil
	})

	register(a, authz.ReplaceDocumentTemplate, huma.Operation{
		OperationID: "replaceDocumentTemplate", Method: http.MethodPut, Path: "/v1/document-templates/{template_id}", Tags: []string{"Templates"},
		Summary: "Save a template's draft",
		Description: "Saves a template's name and draft content, whole, as its next version (FR-TPL-04). The version in force does not " +
			"change: documents keep rendering from it until a new version is published. Every rule the content breaks comes back " +
			"together (TPL-01 to TPL-11). Side effects: the draft and its audit event, with the content before and after. Needs If-Match " +
			"with the template's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeVersion}, append(locks, TemplateCodes...)...),
		func(ctx context.Context, in *templateReplaceInput) (*templateWriteOutput, error) {
			id, err := parseID("template_id", in.TemplateID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DocumentTemplate, error) {
				t, err := u.DocumentTemplates().Lock(ctx, id)
				if err != nil {
					return DocumentTemplate{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(t.Version)); err != nil {
					return DocumentTemplate{}, err
				}
				c := contentIn(in.Body.Content)
				name := strings.TrimSpace(in.Body.Name)
				if err := templateProblems(append(doctemplate.CheckName(name), doctemplate.Check(t.Kind, t.DocumentType, c)...)); err != nil {
					return DocumentTemplate{}, err
				}
				raw, err := doctemplate.Canonical(c)
				if err != nil {
					return DocumentTemplate{}, err
				}
				if t, err = u.DocumentTemplates().SaveDraft(ctx, id, t.Version, name, raw); err != nil {
					return DocumentTemplate{}, err
				}
				out, err := a.templateOf(ctx, u, t)
				if err != nil {
					return DocumentTemplate{}, err
				}
				return out, a.name(ctx, u, out.SavedBy)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &templateWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &templateWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.PublishDocumentTemplate, huma.Operation{
		OperationID: "publishDocumentTemplate", Method: http.MethodPost, Path: "/v1/document-templates/{template_id}/publish", Tags: []string{"Templates"},
		Summary: "Publish a template",
		Description: "Publishes the draft as the template's next numbered version, which never changes and which documents then render " +
			"from (FR-TPL-04); with version, a rollback publishes that earlier version's content again as the next version, and leaves the " +
			"draft as it is. Publishing is an approval object (FR-APR-01): in Founder mode a person who holds the approve right publishes " +
			"alone and the weekly review lists it (FM-3); in Team mode it is refused (TPL-14): ask with requestDocumentTemplatePublication. " +
			"Refused when the content breaks a rule now (TPL-01 to TPL-11), names no version (TPL-13) or is the version in force (TPL-12). " +
			"Side effects: the version, the template's next version and their audit event. Needs If-Match with the template's ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and publishes nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeVersion, storage.CodeTemplate, doctemplate.CodeApproval, doctemplate.CodeUnchange,
		doctemplate.CodeVersion, doctemplate.CodeTest, doctemplate.CodeRenderer}, append(locks, TemplateCodes...)...),
		func(ctx context.Context, in *templatePublishInput) (*templateWriteOutput, error) {
			id, err := parseID("template_id", in.TemplateID)
			if err != nil {
				return nil, err
			}
			note, from := publicationOf(in.Body)
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (DocumentTemplate, error) {
				t, err := u.DocumentTemplates().Lock(ctx, id)
				if err != nil {
					return DocumentTemplate{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(t.Version)); err != nil {
					return DocumentTemplate{}, err
				}
				if _, _, err := a.toPublish(ctx, u, t, from); err != nil {
					return DocumentTemplate{}, err
				}
				mode, _, _, err := teamModeOf(ctx, u)
				if err != nil {
					return DocumentTemplate{}, err
				}
				if mode != team.Founder {
					return DocumentTemplate{}, problem(doctemplate.CodeApproval, "In Team mode another person approves a template's publication (FR-TPL-04).",
						"Ask for approval with requestDocumentTemplatePublication")
				}
				t, v, err := a.publishTemplate(ctx, u, id, t.Version, from, note)
				if err != nil {
					return DocumentTemplate{}, err
				}
				var actor *uuid.UUID
				if p.UserID != uuid.Nil {
					actor = &p.UserID
				}
				what := fmt.Sprintf("Published version %d of the template %q alone", v.Number, t.Name)
				if from > 0 {
					what = fmt.Sprintf("Rolled the template %q back to version %d, as version %d, alone", t.Name, from, v.Number)
				}
				if _, err := u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewSelfApproved, ObjectType: approval.ObjectTemplate,
					ObjectKey: id.String(), Summary: what, Actor: actor, LoggedAt: a.deps.Clock.Now()}); err != nil {
					return DocumentTemplate{}, err
				}
				out, err := a.templateOf(ctx, u, t)
				if err != nil {
					return DocumentTemplate{}, err
				}
				return out, a.name(ctx, u, out.SavedBy)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &templateWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &templateWriteOutput{Status: w.Status, ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.RequestDocumentTemplatePublication, huma.Operation{
		OperationID: "requestDocumentTemplatePublication", Method: http.MethodPost, Path: "/v1/document-templates/{template_id}/approval", Tags: []string{"Templates"},
		Summary: "Ask for approval to publish a template", DefaultStatus: http.StatusAccepted,
		Description: "Asks for another person's approval to publish the template's draft or, with version, to roll back to that earlier " +
			"version (FR-TPL-04, FR-APR-01). The answer is 202 with an approval request, which a person who holds A on \"Document " +
			"templates: edit, publish\", did not ask and, for the draft, did not save it (SOD-3) approves, stepped up; on the approval " +
			"the version is published, by the approver. The request holds the template's version, the SHA-256 of the content and the " +
			"version in force, so a change to any voids it. Refused like publishDocumentTemplate when there is nothing to publish. Asking " +
			"needs a person signed in who passed step-up, and If-Match with the template's ETag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval,
		doctemplate.CodeUnchange, doctemplate.CodeVersion, doctemplate.CodeTest, doctemplate.CodeRenderer}, append(locks, TemplateCodes...)...),
		func(ctx context.Context, in *templatePublishInput) (*templateRequestOutput, error) {
			id, err := parseID("template_id", in.TemplateID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			note, from := publicationOf(in.Body)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				t, err := u.DocumentTemplates().Lock(ctx, id)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(t.Version)); err != nil {
					return Approval{}, err
				}
				if _, _, err := a.toPublish(ctx, u, t, from); err != nil {
					return Approval{}, err
				}
				summary := fmt.Sprintf("Publish the template %q as version %d", t.Name, t.Published+1)
				if from > 0 {
					summary = fmt.Sprintf("Roll the template %q back to version %d, as version %d", t.Name, from, t.Published+1)
				}
				if note != "" {
					summary += ": " + note
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectTemplate, ObjectKey: id.String(), Operation: approval.OpTemplatePublish,
					Summary: summary, Execute: map[string]any{"from": from, "note": note},
					Diff: []ApprovalDiff{{Field: "published_version", Before: fmt.Sprint(t.Published), After: fmt.Sprint(t.Published + 1)}}})
			})
			if err != nil {
				return nil, err
			}
			return &templateRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.ListDocumentTemplateVersions, huma.Operation{
		OperationID: "listDocumentTemplateVersions", Method: http.MethodGet, Path: "/v1/document-templates/{template_id}/versions", Tags: []string{"Templates"},
		Summary:     "List a template's versions",
		Description: "A template's published versions, newest first, a page at a time, without their content (FR-TPL-04). Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeCursor}, locks...), func(ctx context.Context, in *templateVersionsInput) (*templateVersionsOutput, error) {
		id, err := parseID("template_id", in.TemplateID)
		if err != nil {
			return nil, err
		}
		before := 0
		if in.Cursor != "" {
			s, err := decodeCursor(in.Cursor)
			if err != nil {
				return nil, err
			}
			if _, err := fmt.Sscan(s, &before); err != nil || before < 1 {
				return nil, badCursor()
			}
		}
		p, _ := authn.From(ctx)
		out := templateVersionsOutput{Body: TemplateVersionPage{Items: []TemplateVersion{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.DocumentTemplates().Get(ctx, id); err != nil {
				return err
			}
			vs, err := u.DocumentTemplates().Versions(ctx, id, before, in.PageSize+1)
			if err != nil {
				return err
			}
			if len(vs) > in.PageSize {
				vs = vs[:in.PageSize]
				out.Body.NextCursor = encodeCursor(fmt.Sprint(vs[len(vs)-1].Number))
			}
			var people []*Person
			for _, v := range vs {
				x, err := templateVersionOut(v, false)
				if err != nil {
					return err
				}
				out.Body.Items = append(out.Body.Items, x)
			}
			for i := range out.Body.Items {
				people = append(people, out.Body.Items[i].PublishedBy)
			}
			return a.name(ctx, u, people...)
		})
		return &out, err
	})

	register(a, authz.GetDocumentTemplateVersion, huma.Operation{
		OperationID: "getDocumentTemplateVersion", Method: http.MethodGet, Path: "/v1/document-templates/{template_id}/versions/{number}", Tags: []string{"Templates"},
		Summary:     "Read a template version",
		Description: "One published version of a template with its content, which never changes (FR-TPL-04). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *templateVersionInput) (*templateVersionOutput, error) {
		id, err := parseID("template_id", in.TemplateID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out TemplateVersion
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := u.DocumentTemplates().Version(ctx, id, in.Number)
			if err != nil {
				return err
			}
			if out, err = templateVersionOut(v, true); err != nil {
				return err
			}
			return a.name(ctx, u, out.PublishedBy)
		})
		if err != nil {
			return nil, err
		}
		return &templateVersionOutput{Body: out}, nil
	})

	register(a, authz.GetDocumentTemplateCatalogue, huma.Operation{
		OperationID: "getDocumentTemplateCatalogue", Method: http.MethodGet, Path: "/v1/document-template-catalogue/{document_type}", Tags: []string{"Templates"},
		Summary: "Read what a template is made of",
		Description: "What templates of a document type are made of (FR-TPL-01 to FR-TPL-03): its blocks, with the texts each takes and " +
			"those locked because they carry IRAS particulars; the typed field catalogue and each type's formats; the fonts, paper sizes, " +
			"margins and least accent contrast; and the content a new document or email template starts from. Read-only; no side effects.",
	}, nil, func(_ context.Context, in *templateCatalogueInput) (*templateCatalogueOutput, error) {
		t := in.DocumentType
		out := TemplateCatalogue{DocumentType: t, Fonts: choices(doctemplate.Fonts), PageSizes: choices(doctemplate.PageSizes),
			MarginMin: doctemplate.MinMargin, MarginMax: doctemplate.MaxMargin, MinContrast: fmt.Sprintf("%d.%d", doctemplate.MinOnWhite/10, doctemplate.MinOnWhite%10),
			LockedTitle: doctemplate.LockedTitle(t), Defaults: TemplateDefaults{Document: contentOut(doctemplate.Default(doctemplate.KindDocument, t)),
				Email: contentOut(doctemplate.Default(doctemplate.KindEmail, t))}}
		for _, s := range doctemplate.Starters {
			out.Starters = append(out.Starters, TemplateStarter{Key: s.Key, Name: s.Name, Description: s.Description,
				Content: contentOut(doctemplate.Starter(s, doctemplate.KindDocument, t))})
		}
		for _, k := range doctemplate.Blocks(t) {
			out.Blocks = append(out.Blocks, TemplateBlockKind{Kind: k, Texts: choices(doctemplate.BlockTexts(t, k)), Locked: doctemplate.IsLocked(t, k)})
		}
		for _, f := range doctemplate.Fields(t) {
			out.Fields = append(out.Fields, TemplateField{Path: f.Path, Type: f.Type, Label: f.Label})
		}
		for _, typ := range []string{doctemplate.TypeText, doctemplate.TypeDate, doctemplate.TypeMoney, doctemplate.TypeNumber} {
			out.Formats = append(out.Formats, TemplateFormatGroup{Type: typ, Formats: choices(doctemplate.Formats[typ])})
		}
		return &templateCatalogueOutput{Body: out}, nil
	})
}

// templateExecute reads a template publication's change: the earlier version to publish again (0 for the draft) and the note.
func templateExecute(x map[string]any) (int, string, error) {
	var from int
	switch v := x["from"].(type) {
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, "", err
		}
		from = int(n)
	case int:
		from = v
	default:
		return 0, "", fmt.Errorf("approval: a template publication's from is %T", x["from"])
	}
	note, _ := x["note"].(string)
	return from, note, nil
}

// Particular is one IRAS particular a tax invoice or credit note shows (FR-AR-05, FR-TPL-03).
type Particular string

// Schema implements huma.SchemaProvider: a string, with an example.
func (Particular) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "An IRAS particular, such as supplier_gst_number, total_with_gst or reason.", Examples: []any{"total_with_gst"}}
}

// TemplatePreviewRequest is content to preview: saved or not, as the editor has it.
type TemplatePreviewRequest struct {
	Kind         string          `json:"kind" enum:"document,email" doc:"document: the PDF; email: the message that carries it." example:"document"`
	DocumentType string          `json:"document_type" enum:"invoice,quote,credit_note,receipt,statement,reminder,remittance,purchase_order" doc:"The document it serves." example:"invoice"`
	Content      TemplateContent `json:"content" doc:"The content to preview, whole."`
}

// TemplatePreview is a template's content filled with sample data (FR-TPL-01's live preview).
type TemplatePreview struct {
	Kind         string       `json:"kind" enum:"document,email" doc:"What was previewed." example:"document"`
	DocumentType string       `json:"document_type" doc:"The document type." example:"invoice"`
	PDF          []byte       `json:"pdf,omitempty" doc:"A document's sample, as PDF/A-2a and PDF/UA-1, base64." example:"JVBERi0xLjc="`
	SHA256       string       `json:"sha256,omitempty" doc:"The PDF's SHA-256, in hex: the same content always gives the same bytes." example:"a18760db1dd480808a9d4b2fd9b460608c4312c8998317e5ef444d83c0061bda"`
	Missing      []Particular `json:"missing_particulars" nullable:"false" doc:"The IRAS particulars the sample lacks (FR-TPL-03); empty when none is missing or none is required."`
	Subject      string       `json:"subject,omitempty" doc:"An email's subject, filled." example:"Invoice INV-0042 from Example Analytics Pte Ltd"`
	Body         string       `json:"body,omitempty" doc:"An email's body, filled." example:"Dear Acme Trading Pte Ltd, …"`
}

type templatePreviewInput struct {
	Body TemplatePreviewRequest
}

type templatePreviewOutput struct {
	Body TemplatePreview
}

func registerTemplatePreviews(a *API) {
	op := huma.Operation{
		OperationID: "previewDocumentTemplate", Method: http.MethodPost, Path: "/v1/document-template-previews", Tags: []string{"Templates"},
		Summary: "Preview template content",
		Description: "Fills template content, saved or not, with made-up sample data of its document type (FR-TPL-01's live preview): " +
			"a document as PDF/A-2a and PDF/UA-1 from the render service, with the IRAS particulars it would lack (FR-TPL-03), or an " +
			"email's subject and body. Every rule the content breaks comes back together (TPL-01 to TPL-11). It saves nothing and has " +
			"no side effects. TPL-17 when the render service is not configured or busy.",
	}
	op = exempt(op, "Idempotency-Key", "A preview saves nothing and has no side effects, so a repeat is harmless and a stored answer would only keep a PDF.")
	op = exempt(op, "Prefer", "The operation is itself a preview: it saves nothing, so return=preview would answer the same.")
	register(a, authz.PreviewDocumentTemplate, op, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, doctemplate.CodeRenderer},
		TemplateCodes...), func(ctx context.Context, in *templatePreviewInput) (*templatePreviewOutput, error) {
		b := in.Body
		c := contentIn(b.Content)
		if err := templateProblems(doctemplate.Check(b.Kind, b.DocumentType, c)); err != nil {
			return nil, err
		}
		out := TemplatePreview{Kind: b.Kind, DocumentType: b.DocumentType, Missing: []Particular{}}
		d := render.Sample(b.DocumentType)
		if b.Kind == doctemplate.KindEmail {
			subject, body, err := render.Email(c, d)
			if err != nil {
				return nil, err
			}
			out.Subject, out.Body = subject, body
			return &templatePreviewOutput{Body: out}, nil
		}
		res, err := a.renderDocument(ctx, b.DocumentType, c, d, len(doctemplate.Locked[b.DocumentType]) > 0)
		if err != nil {
			return nil, err
		}
		out.PDF, out.SHA256 = res.PDF, res.SHA256
		for _, m := range res.Missing {
			out.Missing = append(out.Missing, Particular(m))
		}
		return &templatePreviewOutput{Body: out}, nil
	})
}
