package api

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// TemplateBundle is templates exported to move them to another organisation or installation (FR-TPL-07): each the
// version in force, or the draft of one never published, with its content's SHA-256. Images travel with the logo and
// signature work, which has none yet.
type TemplateBundle struct {
	Schema     string            `json:"schema" enum:"fyniq.template-bundle/1" doc:"The bundle's format." example:"fyniq.template-bundle/1"`
	ExportedAt string            `json:"exported_at" format:"date-time" doc:"When it was exported." example:"2026-10-23T10:00:00Z"`
	Templates  []BundledTemplate `json:"templates" minItems:"1" maxItems:"50" nullable:"false" doc:"The templates, one to 50."`
}

// BundledTemplate is one template of a bundle.
type BundledTemplate struct {
	Kind         string          `json:"kind" enum:"document,email" doc:"document: the PDF; email: the message that carries it." example:"document"`
	DocumentType string          `json:"document_type" enum:"invoice,quote,credit_note,receipt,statement,reminder,remittance,purchase_order" doc:"The document it serves." example:"invoice"`
	Name         string          `json:"name" maxLength:"100" doc:"Its name." example:"Standard invoice"`
	Version      int             `json:"version" minimum:"0" doc:"The version exported: the one in force, or 0 for the draft of a template never published." example:"3"`
	Content      TemplateContent `json:"content" doc:"Its content."`
	Hash         string          `json:"content_hash" pattern:"^[0-9a-f]{64}$" doc:"The SHA-256 of the content's RFC 8785 JSON, in hex: import refuses content that does not match it." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
}

// TemplateImport is the templates an import made, as drafts.
type TemplateImport struct {
	Templates []DocumentTemplate `json:"templates" nullable:"false" doc:"The drafts made, in the bundle's order."`
}

type templateExportInput struct {
	TemplateID []string `query:"template_id,explode" minItems:"1" maxItems:"50" doc:"The templates, one to 50, each as its own template_id parameter." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f10"`
}

type templateExportOutput struct {
	Body TemplateBundle
}

type templateImportInput struct {
	KeyHeader
	PreferHeader
	Body TemplateBundle
}

type templateImportOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     TemplateImport
}

func registerTemplateBundles(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ExportDocumentTemplates, huma.Operation{
		OperationID: "exportDocumentTemplates", Method: http.MethodGet, Path: "/v1/document-template-bundle", Tags: []string{"Templates"},
		Summary: "Export templates as a bundle",
		Description: "Exports one to 50 templates as a versioned bundle (FR-TPL-07): each the version in force, or the draft of one never " +
			"published, with its content's SHA-256, for importDocumentTemplates in another organisation or installation. Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeInvalid}, locks...), func(ctx context.Context, in *templateExportInput) (*templateExportOutput, error) {
		ids := make([]uuid.UUID, 0, len(in.TemplateID))
		for i, s := range in.TemplateID {
			id, err := uuid.Parse(s)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: fmt.Sprintf("query.template_id[%d]", i), Message: "is not an ID", Fix: "Send the template's ID"}})
			}
			ids = append(ids, id)
		}
		p, _ := authn.From(ctx)
		out := TemplateBundle{Schema: doctemplate.BundleSchema, ExportedAt: a.deps.Clock.Now().UTC().Format(time.RFC3339), Templates: []BundledTemplate{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			for _, id := range ids {
				t, err := u.DocumentTemplates().Get(ctx, id)
				if err != nil {
					return err
				}
				raw := t.Draft
				if t.Published > 0 {
					v, err := u.DocumentTemplates().Version(ctx, id, t.Published)
					if err != nil {
						return err
					}
					raw = v.Content
				}
				c, err := doctemplate.Read(raw)
				if err != nil {
					return err
				}
				h, err := doctemplate.Hash(c)
				if err != nil {
					return err
				}
				out.Templates = append(out.Templates, BundledTemplate{Kind: t.Kind, DocumentType: t.DocumentType, Name: t.Name, Version: t.Published,
					Content: contentOut(c), Hash: hex.EncodeToString(h)})
			}
			return nil
		})
		return &templateExportOutput{Body: out}, err
	})

	register(a, authz.ImportDocumentTemplates, huma.Operation{
		OperationID: "importDocumentTemplates", Method: http.MethodPost, Path: "/v1/document-template-bundles", Tags: []string{"Templates"},
		DefaultStatus: http.StatusCreated, Summary: "Import templates from a bundle",
		Description: "Makes a draft of each template of a bundle from exportDocumentTemplates (FR-TPL-07), all or none. Each content must " +
			"match its SHA-256 (TPL-18) and meet the template rules as they are here (TPL-01 to TPL-11); every problem comes back together. " +
			"Nothing imported is in force: each draft is published under approval like any other (FR-TPL-04). Side effects: the drafts " +
			"and their audit events. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		doctemplate.CodeBundle}, append(locks, TemplateCodes...)...),
		func(ctx context.Context, in *templateImportInput) (*templateImportOutput, error) {
			type draft struct {
				t   BundledTemplate
				raw []byte
			}
			var drafts []draft
			var problems []doctemplate.Problem
			for i, bt := range in.Body.Templates {
				at := fmt.Sprintf("templates[%d].", i)
				c := contentIn(bt.Content)
				for _, p := range append(doctemplate.CheckName(strings.TrimSpace(bt.Name)), doctemplate.Check(bt.Kind, bt.DocumentType, c)...) {
					p.Field = at + p.Field
					problems = append(problems, p)
				}
				raw, err := doctemplate.Canonical(c)
				if err != nil {
					return nil, err
				}
				if h, err := doctemplate.Hash(c); err != nil {
					return nil, err
				} else if hex.EncodeToString(h) != bt.Hash {
					problems = append(problems, doctemplate.Problem{Code: doctemplate.CodeBundle, Field: at + "content_hash",
						Message: "the content does not match its SHA-256: it changed after it was exported, or was exported in another form",
						Fix:     "Export the template again, and import the bundle unchanged"})
				}
				drafts = append(drafts, draft{t: bt, raw: raw})
			}
			if err := templateProblems(problems); err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (TemplateImport, error) {
				out := TemplateImport{Templates: []DocumentTemplate{}}
				var people []*Person
				for _, d := range drafts {
					t, err := u.DocumentTemplates().Create(ctx, storage.NewDocumentTemplate{Kind: d.t.Kind, DocumentType: d.t.DocumentType,
						Name: strings.TrimSpace(d.t.Name), Draft: d.raw})
					if err != nil {
						return TemplateImport{}, err
					}
					x, err := a.templateOf(ctx, u, t)
					if err != nil {
						return TemplateImport{}, err
					}
					out.Templates = append(out.Templates, x)
				}
				for i := range out.Templates {
					people = append(people, out.Templates[i].SavedBy)
				}
				return out, a.name(ctx, u, people...)
			})
			if err != nil {
				return nil, err
			}
			return &templateImportOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
