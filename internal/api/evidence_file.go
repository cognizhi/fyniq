package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ opening an evidence file (FR-DOC-01, rev 0.188)

// EvidenceFileContent is one evidence file, to open or save.
type EvidenceFileContent struct {
	ID        string `json:"id" format:"uuid" doc:"The file's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
	FileName  string `json:"file_name" doc:"As the uploader named it: data, never a path (FR-DOC-08). Save it under a name of your own choosing." example:"AU-100.pdf"`
	MediaType string `json:"media_type" doc:"Its type, decided from its content when it was attached." example:"application/pdf"`
	Size      int    `json:"size_bytes" doc:"Its size in bytes." example:"18432"`
	SHA256    string `json:"sha256" doc:"The SHA-256 of the file as stored, in hexadecimal: the content was checked against it before it was sent." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Content   string `json:"content_base64" doc:"The file, in base64." example:"JVBERi0xLjcgQVUtMTAw"`
}

type evidenceFilePath struct {
	EvidenceID string `path:"evidence_id" format:"uuid" doc:"The file." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e20"`
}

type evidenceFileOutput struct{ Body EvidenceFileContent }

// ownRecord reports whether doc is p's own expense claim or change of their own bank account, and p holds a right on
// expense claims: the files of their own records, which an Employee opens.
func ownRecord(p authn.Principal, doc storage.Document) bool {
	if doc.Type != storage.DocumentExpenseClaim && doc.Type != storage.DocumentClaimantBankChange {
		return false
	}
	if !authz.Decide(authz.Policy{Operation: "view", Capability: authz.ExpenseClaims, Need: authz.View}, p.Roles, p.AI()).Allowed {
		return false
	}
	var whose struct {
		Claimant string `json:"claimant_id"`
		Member   string `json:"member_user_id"`
	}
	if err := json.Unmarshal(doc.Payload, &whose); err != nil || p.UserID == uuid.Nil {
		return false
	}
	return whose.Claimant == p.UserID.String() || whose.Member == p.UserID.String()
}

// opens reports whether p may open a file on doc: they view its record's row without a qualifier and unmasked, as an
// evidence search lists it, or it is their own claim or bank change. A masked right (Appendix D's M, such as the DPO's
// on the cap table) lists a file and never opens it: a holder's identity document stays unread (FR-EQ-15).
func opens(p authn.Principal, doc storage.Document) bool {
	for _, act := range EvidenceActivities {
		for _, t := range act.Types {
			if t == doc.Type && views(p, act.Row) && !viewsMasked(p, act.Row) {
				return true
			}
		}
	}
	return ownRecord(p, doc)
}

// viewsMasked reports whether p's best view of row is masked.
func viewsMasked(p authn.Principal, row authz.Capability) bool {
	return authz.Decide(authz.Policy{Operation: "view", Capability: row, Need: authz.View, Unqualified: true, Qualified: authz.WholeRow},
		p.Roles, p.AI()).Right == authz.Masked
}

// openableEvidence is file id as p may open it in u: the file, the store it is in now and its key there. A file p may not
// open, a removed one and a payroll or identity file are not found.
func (a *API) openableEvidence(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID) (storage.Evidence, ports.BlobStore, string, error) {
	gone := problem(CodeNotFound, "No file you may open has that ID.", "Open it from its record's Evidence panel, or find it in Accounting › Evidence.")
	e, err := u.Evidence().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return e, nil, "", gone
	}
	if err != nil {
		return e, nil, "", err
	}
	if e.State == storage.EvidenceRemoved || e.DataClass == storage.ClassPayroll || e.DataClass == storage.ClassIdentity {
		return e, nil, "", gone // payroll and identity files wait for payroll's permissions (FR-DOC-09); none is attached yet
	}
	doc, err := u.Documents().Get(ctx, e.DocumentID)
	if err != nil {
		return e, nil, "", err
	}
	if !opens(p, doc) {
		return e, nil, "", gone
	}
	store, key := a.deps.Staging, e.StagingKey
	if e.State == storage.EvidenceFinal {
		store, key = a.deps.Evidence, e.LockedKey
	}
	if store == nil {
		return e, nil, "", problem(CodeUnavailable, "This installation has no evidence store to open files from.", "Ask the operator to configure the evidence stores.")
	}
	return e, store, key, nil
}

func registerEvidenceFile(a *API) {
	// Deprecated for createEvidenceDownload (SEC-24): a file is saved through a short-lived link with download headers.
	register(a, authz.GetEvidenceFile, deprecate(huma.Operation{
		OperationID: "getEvidenceFile", Method: http.MethodGet, Path: "/v1/evidence/{evidence_id}/file", Tags: []string{"Evidence"},
		Summary: "Open an evidence file",
		Description: "The file itself, in base64 with its name, type and SHA-256 (FR-DOC-01; PRD-001 rev 0.188): from the staging store " +
			"while its record is a draft, from the retention-locked store once it is final, and checked against the SHA-256 recorded when " +
			"it was stored before it is sent. Only for whoever views its record's row without a qualifier, as an evidence search lists " +
			"it, or for the person whose own expense claim or bank account change it is on; any other file, and a removed one, is not " +
			"found. Show a picture inline only from this answer, never by its name; a PDF is for saving until previews render (FR-DOC-08). " +
			"Side effects: an audit event of the opening, with the file's hash. Never cached. Deprecated: save a file through the link " +
			"createEvidenceDownload gives, served with download headers (SEC-24).",
	}, "2026-11-10", "2027-05-31", "https://github.com/cognizhi/fyniq/blob/main/docs/api/reference.md#createevidencedownload"),
		[]string{CodeNotFound, CodeUnavailable}, func(ctx context.Context, in *evidenceFilePath) (*evidenceFileOutput, error) {
			id, err := parseID("evidence_id", in.EvidenceID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			var out EvidenceFileContent
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
					return fmt.Errorf("evidence %s: the stored file is not the one recorded (FR-DOC-07)", e.ID) // nightly re-verification reports it too
				}
				if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "evidence.opened", ObjectType: "evidence", ObjectID: e.ID,
					Changes: map[string][2]any{"file": {nil, map[string]any{"document_id": e.DocumentID.String(), "sha256": e.SHA256}}}}); err != nil {
					return err
				}
				out = EvidenceFileContent{ID: e.ID.String(), FileName: e.FileName, MediaType: e.MediaType, Size: len(data), SHA256: e.SHA256,
					Content: base64.StdEncoding.EncodeToString(data)}
				return nil
			})
			if err != nil {
				return nil, err
			}
			return &evidenceFileOutput{Body: out}, nil
		})
}
