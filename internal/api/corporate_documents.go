package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/corporate"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
)

// ------------------------------------------------------------------ corporate documents (FR-CS-05)
//
// A directors' resolution, a members' written resolution, a notice of the AGM, its minutes or the notice that the
// statements were sent without one, drafted from one of Fyniq's templates and filled from the records on its day
// (p1-company-secretary task 4.1, design D6). A draft is a paper to review and download: Fyniq never signs or files it.

// DocumentCodes are the codes a corporate document answers besides the common ones.
var DocumentCodes = []string{corporate.CodeDocument, equity.CodeRecorded, storage.CodeCorporateDocument}

// CorporateDocumentFields are a document's fields: each kind asks for some of them, as getCorporateTemplates says.
type CorporateDocumentFields struct {
	FYEnd             string   `json:"fy_end,omitempty" format:"date" doc:"The financial year's last day: approving its statements, the AGM's notice and minutes, the notice that they were sent." example:"2026-12-31"`
	MembersApprovedOn string   `json:"members_approved_on,omitempty" format:"date" doc:"An allotment: the day the members approved the issue (s161)." example:"2027-01-15"`
	Allottee          string   `json:"allottee,omitempty" maxLength:"500" doc:"An allotment: whom the shares are allotted to." example:"Lion Rock Fund I"`
	Shares            string   `json:"shares,omitempty" pattern:"^[1-9][0-9]{0,14}$" doc:"An allotment: how many shares, a whole number with no commas." example:"10000"`
	ShareClass        string   `json:"share_class,omitempty" maxLength:"500" doc:"An allotment: the shares' class." example:"ordinary"`
	Price             string   `json:"price,omitempty" pattern:"^[0-9]{1,12}(\\.[0-9]{1,4})?$" doc:"An allotment: the price of each share in the base currency, a decimal string." example:"1.00"`
	Name              string   `json:"name,omitempty" maxLength:"500" doc:"An appointment: the officer's name." example:"Rachel Teo"`
	Role              string   `json:"role,omitempty" maxLength:"500" doc:"An appointment: the office, director, secretary, ceo or auditor." example:"secretary"`
	From              string   `json:"from,omitempty" format:"date" doc:"An appointment or a change of office: the day it takes effect." example:"2027-01-02"`
	OfficerID         string   `json:"officer_id,omitempty" maxLength:"500" doc:"A cessation: the secretary or CEO in office, by their first version's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7e01"`
	On                string   `json:"on,omitempty" format:"date" doc:"A cessation: the day the appointment ends." example:"2027-03-31"`
	Address           string   `json:"address,omitempty" maxLength:"500" doc:"A change of office: the new registered office." example:"71 Ayer Rajah Crescent, #03-11, Singapore 139951"`
	ResolutionKind    string   `json:"resolution_kind,omitempty" maxLength:"500" doc:"A members' written resolution: ordinary or special." example:"ordinary"`
	Resolutions       []string `json:"resolutions,omitempty" maxItems:"20" doc:"A members' written resolution or the AGM's minutes: the resolutions, one a line." example:"[\"That the financial statements be adopted.\"]"`
	CirculatedOn      string   `json:"circulated_on,omitempty" format:"date" doc:"A members' written resolution: the day it is sent to the members." example:"2027-04-20"`
	MeetingOn         string   `json:"meeting_on,omitempty" format:"date" doc:"The AGM's notice or minutes: the meeting's day." example:"2027-06-15"`
	Time              string   `json:"time,omitempty" pattern:"^([01][0-9]|2[0-3]):[0-5][0-9]$" doc:"The AGM's notice or minutes: its time, 24-hour." example:"10:30"`
	Place             string   `json:"place,omitempty" maxLength:"500" doc:"The AGM's notice or minutes: its place." example:"71 Ayer Rajah Crescent, #03-11, Singapore 139951"`
	NoticeOn          string   `json:"notice_on,omitempty" format:"date" doc:"The AGM's notice: the day it is given." example:"2027-05-28"`
	OtherBusiness     []string `json:"other_business,omitempty" maxItems:"20" doc:"The AGM's notice: other business, one a line." example:"[\"To approve the directors' fees.\"]"`
	Chair             string   `json:"chair,omitempty" maxLength:"500" doc:"The AGM's minutes: who chaired it." example:"Tan Mei Lin"`
	Present           []string `json:"present,omitempty" maxItems:"20" doc:"The AGM's minutes: who was present, one a line." example:"[\"Tan Mei Lin\",\"Ah Kow\"]"`
	SentOn            string   `json:"sent_on,omitempty" format:"date" doc:"The notice that the statements were sent: the day they were sent to the members (s175A)." example:"2027-04-20"`
}

// TransformSchema implements huma.SchemaTransformer: an example for each line (NFR-API-01).
func (CorporateDocumentFields) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"resolutions": "That the financial statements be adopted.", "other_business": "To approve the directors' fees.",
		"present": "Tan Mei Lin"})
	return s
}

// CorporateDocumentDraft is a document to draft: its kind, its fields and a note.
type CorporateDocumentDraft struct {
	Kind   string                  `json:"kind" enum:"agm.dispensation,agm.minutes,agm.notice,directors.allot_shares,directors.appoint_officer,directors.approve_statements,directors.change_office,directors.remove_officer,members.written_resolution" doc:"The kind of document, as getCorporateTemplates lists them." example:"directors.approve_statements"`
	Fields CorporateDocumentFields `json:"fields" doc:"The fields its kind asks for; nothing else."`
	Note   string                  `json:"note,omitempty" maxLength:"1000" doc:"A note for the record, not on the paper." example:"For the AGM on 15 June"`
}

// CorporateDocumentChange is a drafted document's fields drafted again from the records on today; its kind stays.
type CorporateDocumentChange struct {
	Fields CorporateDocumentFields `json:"fields" doc:"The fields its kind asks for; nothing else."`
	Note   string                  `json:"note,omitempty" maxLength:"1000" doc:"A note for the record, not on the paper." example:"For the AGM on 15 June"`
}

// PaperSection is a part of a document's paper.
type PaperSection struct {
	Heading    string   `json:"heading,omitempty" doc:"Its heading." example:"Resolved"`
	Paragraphs []string `json:"paragraphs" nullable:"false" doc:"Its paragraphs." example:"[\"We, being all the directors, pass the following resolutions in writing.\"]"`
	Items      []string `json:"items" nullable:"false" doc:"Its numbered items, such as the resolutions." example:"[\"That the financial statements be approved.\"]"`
}

// TransformSchema implements huma.SchemaTransformer: an example for each paragraph and item (NFR-API-01).
func (PaperSection) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"paragraphs": "We, being all the directors, pass the following resolutions in writing.",
		"items": "That the financial statements be approved."})
	return s
}

// PaperSignature is a line to sign on a document's paper.
type PaperSignature struct {
	Name string `json:"name" doc:"Who signs." example:"Tan Mei Lin"`
	Role string `json:"role" doc:"As what." example:"Director"`
}

// CorporatePaper is a document's text as filled from the records, as its PDF shows it.
type CorporatePaper struct {
	Title      string           `json:"title" doc:"Its title." example:"Directors' resolution in writing"`
	Company    string           `json:"company" doc:"The company's name at its head." example:"Cognizhi Pte. Ltd."`
	Lines      []string         `json:"lines" nullable:"false" doc:"Under the name: the UEN and the registered office." example:"[\"UEN 202612345K\"]"`
	Banner     string           `json:"banner" doc:"Boxed above it: that it is Fyniq's draft to review, never signed or filed by Fyniq." example:"Draft by Fyniq from the company's records on 5 January 2027."`
	Sections   []PaperSection   `json:"sections" nullable:"false" doc:"Its parts, in order."`
	Signatures []PaperSignature `json:"signatures" nullable:"false" doc:"A line to sign for each: the directors, the members, the secretary or the chair."`
	Footer     string           `json:"footer" doc:"Each page's foot: the template and its version." example:"directors.approve_statements@1"`
}

// TransformSchema implements huma.SchemaTransformer: an example for each line under the name (NFR-API-01).
func (CorporatePaper) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"lines": "UEN 202612345K"})
	return s
}

// CorporateDocumentView is a corporate document, drafted or recorded.
type CorporateDocumentView struct {
	ID         string                  `json:"id" format:"uuid" doc:"The document's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7c01"`
	Kind       string                  `json:"kind" doc:"Its kind." example:"directors.approve_statements"`
	Template   string                  `json:"template" doc:"The template it was drafted from, with its version." example:"directors.approve_statements@1"`
	Title      string                  `json:"title" doc:"Its title." example:"Directors' resolution in writing"`
	Status     string                  `json:"status" enum:"draft,recorded" doc:"draft: a paper to review and have signed; recorded: in the minute book with its signed copy." example:"draft"`
	Version    int                     `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	DraftedOn  string                  `json:"drafted_on" format:"date" doc:"The day it was drafted: the records it was filled from are that day's." example:"2027-01-05"`
	ProposalID string                  `json:"proposal_id,omitempty" format:"uuid" doc:"The AI's sec.draft_document proposal it came from, a person having approved it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7c11"`
	Masked     bool                    `json:"masked" doc:"true: its fields and text are hidden for your role (the DPO)." example:"false"`
	Fields     CorporateDocumentFields `json:"fields" doc:"The fields it was drafted with."`
	Paper      *CorporatePaper         `json:"paper,omitempty" doc:"Its text as filled from the records, as its PDF shows it; hidden when masked."`
	Note       string                  `json:"note,omitempty" doc:"A note for the record." example:"For the AGM on 15 June"`
}

// CorporateTemplateField is a field a template asks for.
type CorporateTemplateField struct {
	Name     string            `json:"name" doc:"The field, as CorporateDocumentFields names it." example:"fy_end"`
	Type     string            `json:"type" enum:"date,time,text,lines,choice,count,amount,officer" doc:"What it takes: a date, a time of day, a text, lines, one of its choices, a whole number, an amount, or an officer in office." example:"date"`
	Label    string            `json:"label" doc:"What to ask for." example:"The financial year's last day"`
	Optional bool              `json:"optional" doc:"true: it may be left out." example:"false"`
	Choices  map[string]string `json:"choices,omitempty" doc:"A choice's values and the words the paper writes for each." example:"{\"ordinary\":\"ordinary\",\"special\":\"special\"}"`
	Roles    []string          `json:"roles,omitempty" doc:"An officer field: the offices it accepts." example:"[\"secretary\",\"ceo\"]"`
}

// TransformSchema implements huma.SchemaTransformer: an example for each office (NFR-API-01).
func (CorporateTemplateField) TransformSchema(_ huma.Registry, s *huma.Schema) *huma.Schema {
	itemExamples(s, map[string]any{"roles": "secretary"})
	return s
}

// CorporateTemplate is one of Fyniq's corporate templates.
type CorporateTemplate struct {
	Kind    string                   `json:"kind" doc:"The kind of document it drafts." example:"directors.approve_statements"`
	Version int                      `json:"version" doc:"Its version: a changed template is a new one." example:"1"`
	Title   string                   `json:"title" doc:"The paper's title." example:"Directors' resolution in writing"`
	Summary string                   `json:"summary" doc:"What it is for." example:"The directors approve a financial year's statements and authorise a director to sign them."`
	Signers string                   `json:"signers" enum:"directors,members,secretary,chair" doc:"Who signs it: each director in office, each member holding shares, the secretary, or the meeting's chair." example:"directors"`
	Fields  []CorporateTemplateField `json:"fields" nullable:"false" doc:"The fields it asks for."`
}

// CorporateTemplates are the release's corporate templates.
type CorporateTemplates struct {
	Templates []CorporateTemplate `json:"templates" nullable:"false" doc:"Every template, by kind."`
	Note      string              `json:"note" doc:"Their status." example:"Fyniq's templates, which a lawyer has yet to confirm."`
}

type (
	CorporateDocumentPath struct {
		DocumentID string `path:"document_id" format:"uuid" doc:"The corporate document." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c7c01"`
	}
	documentDraftInput struct {
		KeyHeader
		PreferHeader
		Body CorporateDocumentDraft
	}
	documentReviseInput struct {
		CorporateDocumentPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body CorporateDocumentChange
	}
	documentReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body CorporateDocumentView
	}
	documentWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     CorporateDocumentView
	}
	documentFileOutput struct{ Body DocumentFile }
	templatesOutput    struct{ Body CorporateTemplates }
)

// DocumentFile is a corporate document's PDF.
type DocumentFile struct {
	FileName  string `json:"file_name" doc:"The file's name." example:"directors.approve_statements-2027-01-05.pdf"`
	MediaType string `json:"media_type" doc:"Its type." example:"application/pdf"`
	SHA256    string `json:"sha256" doc:"The SHA-256 of the content, in hexadecimal; masked for the DPO." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Size      int    `json:"size_bytes" doc:"Its size, in bytes; 0 for the DPO." example:"30877"`
	Content   string `json:"content_base64" doc:"The PDF, in base64: archival (PDF/A-2a) and accessible (PDF/UA-1); masked for the DPO." example:"JVBERi0xLjc="`
}

// documentValues are a document's fields as the domain reads them: each field given, a text or lines.
func documentValues(f CorporateDocumentFields) corporate.Values {
	raw, _ := json.Marshal(f)
	out := corporate.Values{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// documentFieldsOf are a document's fields as the API shows them.
func documentFieldsOf(v corporate.Values) CorporateDocumentFields {
	raw, _ := json.Marshal(v)
	var out CorporateDocumentFields
	_ = json.Unmarshal(raw, &out)
	return out
}

func paperOf(p render.Paper) *CorporatePaper {
	out := &CorporatePaper{Title: p.Title, Company: p.Author, Lines: append([]string{}, p.Lines...), Banner: p.Banner, Footer: p.Footer,
		Sections: []PaperSection{}, Signatures: []PaperSignature{}}
	for _, s := range p.Sections {
		out.Sections = append(out.Sections, PaperSection{Heading: s.Heading, Paragraphs: append([]string{}, s.Paragraphs...), Items: append([]string{}, s.Items...)})
	}
	for _, s := range p.Signatures {
		out.Signatures = append(out.Signatures, PaperSignature{Name: s.Name, Role: s.Role})
	}
	return out
}

func documentViewOf(ctx context.Context, x equity.Recorded[corporate.Document]) CorporateDocumentView {
	status := "draft"
	if x.Doc.Status == storage.DocumentPosted {
		status = "recorded"
	}
	out := CorporateDocumentView{ID: x.Doc.ID.String(), Kind: x.Value.Kind, Template: x.Value.Template, Title: x.Value.Paper.Title, Status: status,
		Version: x.Doc.Version, DraftedOn: x.Value.Date.Format(time.DateOnly), Fields: documentFieldsOf(x.Value.Values), Paper: paperOf(x.Value.Paper),
		Note: x.Value.Note}
	if x.Value.Proposal != nil {
		out.ProposalID = x.Value.Proposal.String()
	}
	if masked(ctx) { // the DPO: the papers name people, so only what each is
		out.Masked, out.Fields, out.Paper, out.Note = true, CorporateDocumentFields{}, nil, ""
	}
	return out
}

// shownDocument is a corporate document by ID: not found for anything else.
func shownDocument(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (CorporateDocumentView, storage.Document, error) {
	doc, err := equityRecordOfType(ctx, u, storage.DocumentCorporateDocument, id)
	if err != nil {
		return CorporateDocumentView{}, doc, err
	}
	d, err := corporate.ReadDocument(doc)
	if err != nil {
		return CorporateDocumentView{}, doc, err
	}
	x := equity.Recorded[corporate.Document]{Doc: doc, Value: d}
	if doc.PostedAt != nil {
		x.RecordedAt = *doc.PostedAt
	}
	return documentViewOf(ctx, x), doc, nil
}

// documentsOf are every corporate document, the latest drafted first, as the profile lists them.
func documentsOf(ctx context.Context, u storage.UnitOfWork) ([]CorporateDocumentView, error) {
	docs, err := corporate.Documents(ctx, u)
	if err != nil {
		return nil, err
	}
	out := []CorporateDocumentView{}
	for _, x := range docs {
		v := documentViewOf(ctx, x)
		v.Paper = nil // the profile lists them; getCorporateDocument has the text
		out = append(out, v)
	}
	return out, nil
}

func documentAnswer(w written[CorporateDocumentView]) *documentWriteOutput {
	if w.Previewed {
		return &documentWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &documentWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, "draft"), Location: "/v1/corporate-profile/documents/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

// renderPaper renders a document's paper through the render service as made at created: REPORT-08 when no render
// service is configured, it is busy, or it fails.
func (a *API) renderPaper(ctx context.Context, p render.Paper, created time.Time) ([]byte, error) {
	if a.deps.Renderer == nil {
		return nil, problem(CodeReportRender, "No render service is configured, so the PDF cannot be made.",
			"Read the document's text in Fyniq, or ask the operator to configure the render service")
	}
	res, err := a.deps.Renderer.Render(ctx, render.Request{Paper: &p, Created: created})
	var rp *render.Problem
	switch {
	case err == nil:
		return res.PDF, nil
	case errors.As(err, &rp) && rp.Code == render.CodeBusy:
		return nil, problem(CodeReportRender, "The render service is busy.", "Try again in a moment").withHeader("Retry-After", "1")
	}
	if a.deps.Log != nil {
		a.deps.Log.ErrorContext(ctx, "render", "error", err.Error())
	}
	return nil, problem(CodeReportRender, "The render service did not make the PDF.", "Try again; if it persists, report it")
}

func registerCorporateDocuments(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)
	tags := []string{"Corporate profile"}

	register(a, authz.GetCorporateTemplates, huma.Operation{
		OperationID: "getCorporateTemplates", Method: http.MethodGet, Path: "/v1/corporate-profile/templates", Tags: tags,
		Summary: "Get the corporate document templates",
		Description: "Returns Fyniq's corporate templates (FR-CS-05): directors' resolutions approving the financial statements, allotting " +
			"shares, appointing an officer, ending the secretary's or CEO's appointment or changing the registered office; a members' " +
			"written resolution; the AGM's notice and minutes; and the notice that the statements were sent without an AGM (s175A). Each " +
			"with its version, who signs it and the fields it asks for. The templates are Fyniq's, which a lawyer has yet to confirm. " +
			"Read-only; no side effects.",
	}, []string{}, func(ctx context.Context, _ *struct{}) (*templatesOutput, error) {
		out := &templatesOutput{Body: CorporateTemplates{Templates: []CorporateTemplate{}, Note: "Fyniq's templates, which a lawyer has yet to confirm."}}
		for _, k := range corporate.Kinds {
			t := corporate.Templates[k]
			x := CorporateTemplate{Kind: t.Kind, Version: t.Version, Title: t.Title, Summary: t.Summary, Signers: t.Signers, Fields: []CorporateTemplateField{}}
			for _, f := range t.Fields {
				x.Fields = append(x.Fields, CorporateTemplateField{Name: f.Name, Type: f.Type, Label: f.Label, Optional: f.Optional, Choices: f.Choices, Roles: f.Roles})
			}
			out.Body.Templates = append(out.Body.Templates, x)
		}
		return out, nil
	})

	register(a, authz.CreateCorporateDocument, huma.Operation{
		OperationID: "createCorporateDocument", Method: http.MethodPost, Path: "/v1/corporate-profile/documents", Tags: tags,
		DefaultStatus: http.StatusCreated, Summary: "Draft a corporate document",
		Description: "Drafts a corporate document from one of Fyniq's templates (FR-CS-05; DraftCorporateDocument): its kind, with the " +
			"fields getCorporateTemplates says it asks for, filled from the records on today: the company's name, UEN and registered " +
			"office, and the directors, the secretary or the members who sign it. The draft is a paper to review, download as PDF with " +
			"getCorporateDocumentPDF and have signed outside Fyniq: Fyniq never signs or files it. The Owner drafts one too, as they " +
			"approve the AI's sec.draft_document. Side effects: the draft, audit events. Every problem comes back together, each naming " +
			"its field. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing and shows the paper.",
	}, append(writes, DocumentCodes...), func(ctx context.Context, in *documentDraftInput) (*documentWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (CorporateDocumentView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return CorporateDocumentView{}, err
			}
			doc, err := corporate.DraftDocumentIn(ctx, u, in.Body.Kind, documentValues(in.Body.Fields), strings.TrimSpace(in.Body.Note), nil, today)
			if err != nil {
				return CorporateDocumentView{}, equityProblems(err)
			}
			v, _, err := shownDocument(ctx, u, doc.ID)
			return v, err
		})
		if err != nil {
			return nil, err
		}
		return documentAnswer(w), nil
	})

	register(a, authz.GetCorporateDocument, huma.Operation{
		OperationID: "getCorporateDocument", Method: http.MethodGet, Path: "/v1/corporate-profile/documents/{document_id}", Tags: tags,
		Summary:     "Get a corporate document",
		Description: "Returns a corporate document with its ETag: its kind, template, fields and text as filled from the records on the day it was drafted (FR-CS-05). The DPO sees what it is, not its fields or text. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *CorporateDocumentPath) (*documentReadOutput, error) {
		id, err := parseID("document_id", in.DocumentID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out documentReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var doc storage.Document
			out.Body, doc, err = shownDocument(ctx, u, id)
			out.ETag = recordETag(doc)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceCorporateDocument, huma.Operation{
		OperationID: "replaceCorporateDocument", Method: http.MethodPut, Path: "/v1/corporate-profile/documents/{document_id}", Tags: tags,
		Summary: "Draft a corporate document again",
		Description: "Saves a new version of a drafted corporate document: its fields filled again from the records on today (FR-CS-05); " +
			"its kind stays. A recorded one changes no more (EQUITY-05). Side effects: the new version, audit events. Needs If-Match with " +
			"its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), DocumentCodes...), func(ctx context.Context, in *documentReviseInput) (*documentWriteOutput, error) {
		id, err := parseID("document_id", in.DocumentID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (CorporateDocumentView, error) {
			doc, err := equity.Drafted(ctx, u, storage.DocumentCorporateDocument, id)
			if err != nil {
				return CorporateDocumentView{}, err
			}
			if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
				return CorporateDocumentView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return CorporateDocumentView{}, err
			}
			if _, err := corporate.ReviseDocumentIn(ctx, u, id, documentValues(in.Body.Fields), strings.TrimSpace(in.Body.Note), today); err != nil {
				return CorporateDocumentView{}, equityProblems(err)
			}
			v, _, err := shownDocument(ctx, u, id)
			return v, err
		})
		if err != nil {
			return nil, err
		}
		return documentAnswer(w), nil
	})

	register(a, authz.GetCorporateDocumentPDF, huma.Operation{
		OperationID: "getCorporateDocumentPDF", Method: http.MethodGet, Path: "/v1/corporate-profile/documents/{document_id}/pdf", Tags: tags,
		Summary: "Get a corporate document as PDF",
		Description: "The document's text as filled on the day it was drafted, as a PDF to print and sign (FR-CS-05): archival (PDF/A-2a) " +
			"and accessible (PDF/UA-1), with a line to sign for each signer and a banner saying it is Fyniq's draft, never signed or filed " +
			"by Fyniq. The same version gives the same bytes. Returned in base64 with its SHA-256. A PDF the render service cannot make now " +
			"is REPORT-08. The DPO, who sees no text, gets its name with the content masked, and nothing is rendered. Side effects: an " +
			"audit event of the download, with the file's hash (AUD-05). Never cached.",
	}, append([]string{CodeNotFound, CodeReportRender}, locks...), func(ctx context.Context, in *CorporateDocumentPath) (*documentFileOutput, error) {
		id, err := parseID("document_id", in.DocumentID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var d corporate.Document
		var version int
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := equityRecordOfType(ctx, u, storage.DocumentCorporateDocument, id)
			if err != nil {
				return err
			}
			version = doc.Version
			d, err = corporate.ReadDocument(doc)
			return err
		})
		if err != nil {
			return nil, err
		}
		name := d.Kind + "-" + d.Date.Format(time.DateOnly) + ".pdf"
		if masked(ctx) { // the DPO sees what it is, not its text: nothing is rendered
			return &documentFileOutput{Body: DocumentFile{FileName: name, MediaType: mediaPDF, SHA256: MaskedValue, Content: MaskedValue}}, nil
		}
		pdf, err := a.renderPaper(ctx, d.Paper, d.Date)
		if err != nil {
			return nil, err
		}
		h := sha256.Sum256(pdf)
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error { // AUD-05: a file that names people
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "corporate_document.pdf_downloaded", ObjectType: "corporate_document", ObjectID: id,
				Changes: map[string][2]any{"download": {nil, map[string]any{"file": name, "template": d.Template, "version": version,
					"sha256": hex.EncodeToString(h[:])}}}})
		})
		if err != nil {
			return nil, err
		}
		return &documentFileOutput{Body: DocumentFile{FileName: name, MediaType: mediaPDF,
			SHA256: hex.EncodeToString(h[:]), Size: len(pdf), Content: base64.StdEncoding.EncodeToString(pdf)}}, nil
	})
}
