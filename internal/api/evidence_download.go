package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ evidence downloads (SEC-24)

// A file is saved through a short-lived link (p1-security-hardening design D5): whoever may open it asks for one, which
// works for 5 minutes and serves the file as an attachment with its name and type. In the cloud it is a V4 signed URL
// of the object, which Cloud Storage serves; locally an HMAC-signed path the API serves itself. Asking for a link is
// audited, as opening a file is. getEvidenceFile, which answers the file in base64, is deprecated for it.

// downloadTTL is how long a download link works.
const downloadTTL = 5 * time.Minute

// EvidenceDownload is a link that saves one evidence file.
type EvidenceDownload struct {
	URL       string    `json:"url" doc:"Open it within expires_at: the file is served as an attachment named file_name, of type media_type. A path starting /api/ is on the web app's own origin." example:"https://storage.googleapis.com/fyniq-prod-evidence/01928f6e/AU-100.pdf?X-Goog-Algorithm=GOOG4-RSA-SHA256&X-Goog-Expires=300&X-Goog-Signature=ab12"`
	ExpiresAt time.Time `json:"expires_at" doc:"When the link stops working: 5 minutes after it was made." example:"2026-11-10T09:05:00Z"`
	FileName  string    `json:"file_name" doc:"As the uploader named it: data, never a path (FR-DOC-08)." example:"AU-100.pdf"`
	MediaType string    `json:"media_type" doc:"Its type, decided from its content when it was attached." example:"application/pdf"`
	SHA256    string    `json:"sha256" doc:"The SHA-256 of the file as stored, in hexadecimal, to check what was saved." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
}

type evidenceDownloadOutput struct{ Body EvidenceDownload }

// EvidenceDownloadPath names a download link's token.
type EvidenceDownloadPath struct {
	Token string `path:"token" maxLength:"512" pattern:"^[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+$" doc:"The link's token, as createEvidenceDownload gave it." example:"eyJlIjoiMDE5MjhmNmUifQ.q1w2e3r4t5y6"`
}

type evidenceDownloadFile struct {
	ContentType        string `header:"Content-Type" doc:"The file's type."`
	ContentDisposition string `header:"Content-Disposition" doc:"attachment, with the file's name."`
	CacheControl       string `header:"Cache-Control" doc:"no-store."`
	Body               []byte
}

// downloadClaim is what a local download link's token carries.
type downloadClaim struct {
	Evidence     uuid.UUID `json:"e"`
	Organisation uuid.UUID `json:"o"`
	Expires      int64     `json:"x"`
}

// downloadToken signs c with the API's key.
func (a *API) downloadToken(c downloadClaim) string {
	raw, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, a.downloadKey)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// readDownloadToken is the claim of a token the API signed and that has not expired at now.
func (a *API) readDownloadToken(token string, now time.Time) (downloadClaim, bool) {
	var c downloadClaim
	body, sig, ok := strings.Cut(token, ".")
	raw, err1 := base64.RawURLEncoding.DecodeString(body)
	got, err2 := base64.RawURLEncoding.DecodeString(sig)
	if !ok || err1 != nil || err2 != nil {
		return c, false
	}
	mac := hmac.New(sha256.New, a.downloadKey)
	mac.Write(raw)
	if !hmac.Equal(got, mac.Sum(nil)) || json.Unmarshal(raw, &c) != nil || now.Unix() >= c.Expires {
		return c, false
	}
	return c, true
}

func registerEvidenceDownloads(a *API) {
	register(a, authz.CreateEvidenceDownload, exempt(exempt(huma.Operation{
		OperationID: "createEvidenceDownload", Method: http.MethodPost, Path: "/v1/evidence/{evidence_id}/download", Tags: []string{"Evidence"},
		Summary: "Get a link that saves an evidence file",
		Description: "A link that works for 5 minutes and serves the file as an attachment, with its name and type (SEC-24): a signed URL of " +
			"the stored object in the cloud, a signed path of the API locally. For whoever may open the file, as getEvidenceFile is: any other " +
			"file, and a removed one, is not found. Side effects: an audit event of the link, with the file's hash. Never cached.",
	}, "Idempotency-Key", "Each request gives a new link that expires in 5 minutes; nothing is stored, so a retry harms nothing."),
		"Prefer", "A link changes no record: a preview would be the link itself."),
		[]string{CodeNotFound, CodeUnavailable}, func(ctx context.Context, in *evidenceFilePath) (*evidenceDownloadOutput, error) {
			id, err := parseID("evidence_id", in.EvidenceID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			now := a.deps.Clock.Now()
			var out EvidenceDownload
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				e, store, key, err := a.openableEvidence(ctx, u, p, id)
				if err != nil {
					return err
				}
				out = EvidenceDownload{ExpiresAt: now.Add(downloadTTL).UTC(), FileName: e.FileName, MediaType: e.MediaType, SHA256: e.SHA256}
				if signer, ok := store.(ports.DownloadLinker); ok {
					if out.URL, err = signer.DownloadURL(ctx, key, ports.Download{FileName: e.FileName, MediaType: e.MediaType, TTL: downloadTTL}, now); err != nil {
						return err
					}
				} else {
					out.URL = a.deps.APIBase + "/v1/evidence-downloads/" +
						a.downloadToken(downloadClaim{Evidence: e.ID, Organisation: p.Organisation, Expires: now.Add(downloadTTL).Unix()})
				}
				return u.Audit().Record(ctx, storage.AuditRecord{Action: "evidence.download_linked", ObjectType: "evidence", ObjectID: e.ID,
					Changes: map[string][2]any{"link": {nil, map[string]any{"document_id": e.DocumentID.String(), "sha256": e.SHA256,
						"expires_at": out.ExpiresAt.Format(time.RFC3339)}}}})
			})
			if err != nil {
				return nil, err
			}
			return &evidenceDownloadOutput{Body: out}, nil
		})

	register(a, authz.GetEvidenceDownload, huma.Operation{
		OperationID: "getEvidenceDownload", Method: http.MethodGet, Path: "/v1/evidence-downloads/{token}", Tags: []string{"Evidence"},
		Summary: "Save an evidence file through its link",
		Description: "Public: no session; the link's token is the right, for 5 minutes (SEC-24). The file itself, served as an attachment " +
			"with its name and type, checked against the SHA-256 recorded when it was stored. A token that is not the API's, or expired, " +
			"and a file removed since, is not found. Only the links of an installation without signed URLs of its store come here. Never cached.",
		Responses: map[string]*huma.Response{"200": {Description: "The file itself, of its stored type, as an attachment with its name.",
			Content: map[string]*huma.MediaType{"application/octet-stream": {Schema: &huma.Schema{Type: "string", Format: "binary", Examples: []any{"%PDF-1.7 AU-100"}}}}}},
	}, []string{CodeNotFound, CodeUnavailable}, func(ctx context.Context, in *EvidenceDownloadPath) (*evidenceDownloadFile, error) {
		gone := problem(CodeNotFound, "That link does not open a file, or it expired.", "Ask for a new link from the file's Evidence panel.")
		c, ok := a.readDownloadToken(in.Token, a.deps.Clock.Now())
		if !ok {
			return nil, gone
		}
		var out *evidenceDownloadFile
		err := a.deps.Store.Do(ctx, c.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			e, err := u.Evidence().Get(ctx, c.Evidence)
			if err != nil || e.State == storage.EvidenceRemoved {
				return gone
			}
			store, key := a.deps.Staging, e.StagingKey
			if e.State == storage.EvidenceFinal {
				store, key = a.deps.Evidence, e.LockedKey
			}
			if store == nil {
				return problem(CodeUnavailable, "This installation has no evidence store to open files from.", "Ask the operator to configure the evidence stores.")
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
			name := e.FileName
			if name == "" {
				name = "file"
			}
			out = &evidenceDownloadFile{ContentType: e.MediaType, ContentDisposition: mime.FormatMediaType("attachment", map[string]string{"filename": name}),
				CacheControl: noStore, Body: data}
			return nil
		})
		return out, err
	})
}
