package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the safe preview of a PDF (FR-DOC-08, rev 0.321)

// EvidencePreview is the first page of a PDF evidence file, drawn as a picture.
type EvidencePreview struct {
	ID            string `json:"id" format:"uuid" doc:"The file this is the preview of." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	MediaType     string `json:"media_type" enum:"image/png" doc:"The preview's type: a PNG, decided from its content." example:"image/png"`
	Size          int    `json:"size_bytes" doc:"The preview's size in bytes." example:"48213"`
	SourceSHA256  string `json:"source_sha256" doc:"The SHA-256 of the PDF as stored, which was checked against its record before it was drawn." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	PreviewSHA256 string `json:"preview_sha256" doc:"The SHA-256 of the picture, so a preview is as provable as the file it came from." example:"2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"`
	Content       string `json:"content_base64" doc:"The picture, in base64: show it as an image, never by the file's name." example:"iVBORw0KGgo="`
}

type evidencePreviewOutput struct {
	CacheControl string `header:"Cache-Control" doc:"no-store."`
	Body         EvidencePreview
}

func registerEvidencePreview(a *API) {
	register(a, authz.GetEvidencePreview, huma.Operation{
		OperationID: "getEvidencePreview", Method: http.MethodGet, Path: "/v1/evidence/{evidence_id}/preview", Tags: []string{"Evidence"},
		Summary: "Preview a PDF evidence file",
		Description: "The first page of a PDF evidence file as a PNG picture (FR-DOC-08; PRD-001 rev 0.321), drawn by the converter service in " +
			"its own sandbox, so no parser of a PDF runs in the API: nothing in the file runs, no script, form or link. The picture is checked as " +
			"any file is, its type decided from its content and scanned for malware, and comes with the SHA-256 of the PDF as stored (checked " +
			"against its record first) and of the picture. For whoever may open the file, as getEvidenceFile is: any other file, and a removed " +
			"one, is not found. A file that is not a PDF, or that the renderer cannot read (an encrypted one), is DOC-12; an installation with no " +
			"converter answers 503, and the file can still be saved. Previews are drawn when asked and not kept; the audit trail records each " +
			"as an opening, with both hashes. Side effects: an audit event of the opening, with both hashes. Never cached.",
	}, []string{CodeNotFound, CodeUnavailable, evidence.CodePreview, evidence.CodeMalware}, func(ctx context.Context, in *evidenceFilePath) (*evidencePreviewOutput, error) {
		id, err := parseID("evidence_id", in.EvidenceID)
		if err != nil {
			return nil, err
		}
		if a.deps.Converter == nil {
			return nil, problem(CodeUnavailable, "This installation has no converter, so a PDF cannot be previewed here.",
				"Save a copy of the file to open it, or ask the operator to configure the converter service.")
		}
		p, _ := authn.From(ctx)
		var out EvidencePreview
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			e, store, key, err := a.openableEvidence(ctx, u, p, id)
			if err != nil {
				return err
			}
			r, err := store.Get(ctx, key)
			if err != nil {
				return fmt.Errorf("evidence %s: %w", e.ID, err)
			}
			defer r.Close()
			data, err := io.ReadAll(io.LimitReader(r, evidence.MaxBytes+1))
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			if want, _ := hex.DecodeString(e.SHA256); !bytes.Equal(sum[:], want) {
				return fmt.Errorf("evidence %s: the stored file is not the one recorded (FR-DOC-07)", e.ID)
			}
			picture, problems, err := evidence.Preview(ctx, evidence.Checker{Scanner: a.deps.Scanner, Converter: a.deps.Converter}, data)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return err
				}
				if a.deps.Log != nil {
					a.deps.Log.ErrorContext(ctx, "evidence preview", "error", err.Error())
				}
				return problem(CodeUnavailable, "The preview could not be drawn just now.", "Try again in a moment, or save a copy of the file to open it.")
			}
			if len(problems) > 0 {
				return problem(problems[0].Code, problems[0].Message, problems[0].Fix)
			}
			shot := sha256.Sum256(picture)
			out = EvidencePreview{ID: e.ID.String(), MediaType: "image/png", Size: len(picture), SourceSHA256: e.SHA256,
				PreviewSHA256: hex.EncodeToString(shot[:]), Content: base64.StdEncoding.EncodeToString(picture)}
			// A preview is an opening of the file's first page, so it is the security event of one (AUD-05), with both hashes.
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "evidence.opened", ObjectType: "evidence", ObjectID: e.ID,
				Changes: map[string][2]any{"preview": {nil, map[string]any{"document_id": e.DocumentID.String(), "source_sha256": e.SHA256, "preview_sha256": out.PreviewSHA256}}}})
		})
		if err != nil {
			return nil, err
		}
		return &evidencePreviewOutput{CacheControl: "no-store", Body: out}, nil
	})
}
