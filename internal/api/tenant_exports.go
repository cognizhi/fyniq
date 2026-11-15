package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/reportexport"
)

// ------------------------------------------------------------------ the full tenant export (FR-MIG-04, design D10; rev 0.273)

// A person with C on Appendix D's "Tenant export" asks for the export; another, who holds its A, approves it (FM-1);
// the approval queues it for the worker, which writes the organisation whole as one ZIP (reportexport.WriteTenant). Only
// its requester saves it, through a link that works for 5 minutes, for 24 hours after it is ready; each link is
// recorded as a sensitive export (AUD-05).

// TenantExport is a full tenant export: asked for and waiting for its approval, then prepared by the worker.
type TenantExport struct {
	ApprovalID  string            `json:"approval_id" format:"uuid" doc:"The approval it waits for, or was prepared under." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d10"`
	ExportID    string            `json:"export_id,omitempty" format:"uuid" doc:"The export, once approved: what createTenantExportDownload saves." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d20"`
	Status      string            `json:"status" enum:"awaiting_approval,pending,ready,failed,expired" doc:"awaiting_approval until another person approves it; pending while the worker writes it; ready to save for 24 hours; failed with why; expired once its file is gone." example:"ready"`
	RequestedBy string            `json:"requested_by" doc:"Who asked for it; only they save it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d30"`
	RequestedAt time.Time         `json:"requested_at" doc:"When it was asked for, or approved and queued." example:"2026-11-13T09:00:00Z"`
	File        *TenantExportFile `json:"file,omitempty" doc:"Its file, once ready."`
	Failure     string            `json:"failure,omitempty" doc:"Why it failed." example:"the sealed fields' keys are not configured, so the bank account numbers cannot be exported"`
}

// TenantExportFile is a ready export's file.
type TenantExportFile struct {
	Name      string    `json:"name" doc:"The ZIP's name." example:"fyniq-export-2026-11-13.zip"`
	SHA256    string    `json:"sha256" doc:"The ZIP's SHA-256, in hexadecimal, to check what was saved." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Size      int64     `json:"size" doc:"Its size in bytes." example:"5242880"`
	Rows      int64     `json:"rows" doc:"The rows of the organisation's tables it holds." example:"125000"`
	Revision  int64     `json:"ledger_revision" doc:"The ledger revision it was read at." example:"4211"`
	ReadyAt   time.Time `json:"ready_at" doc:"When the worker finished it." example:"2026-11-13T09:05:00Z"`
	ExpiresAt time.Time `json:"expires_at" doc:"When its file is deleted: 24 hours after it was ready." example:"2026-11-14T09:05:00Z"`
}

// TenantExportList is a page of the organisation's full exports.
type TenantExportList struct {
	Items      []TenantExport `json:"items" nullable:"false" doc:"The exports, newest first: those awaiting approval, then those approved."`
	NextCursor string         `json:"next_cursor,omitempty" doc:"The cursor of the next page; none on the last." example:"eyJhZnRlciI6IjEwMDAifQ"`
}

// TenantExportDownload is a link that saves a ready export.
type TenantExportDownload struct {
	URL       string    `json:"url" doc:"Open it within expires_at: the ZIP is served as an attachment named file_name. A path starting /api/ is on the web app's own origin." example:"https://storage.googleapis.com/fyniq-prod-staging/exports/01928f6e/fyniq-export-2026-11-13.zip?X-Goog-Algorithm=GOOG4-RSA-SHA256&X-Goog-Expires=300&X-Goog-Signature=ab12"`
	ExpiresAt time.Time `json:"expires_at" doc:"When the link stops working: 5 minutes after it was made." example:"2026-11-13T09:10:00Z"`
	FileName  string    `json:"file_name" doc:"The ZIP's name." example:"fyniq-export-2026-11-13.zip"`
	SHA256    string    `json:"sha256" doc:"The ZIP's SHA-256, in hexadecimal." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
}

type (
	tenantExportRequestInput struct {
		KeyHeader
		PreferHeader
	}
	tenantExportRequestOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     Approval
	}
	tenantExportListOutput struct{ Body TenantExportList }
	// TenantExportPath names a full tenant export.
	TenantExportPath struct {
		ExportID string `path:"export_id" doc:"The export's ID, as listTenantExports gives it once approved." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d20"`
	}
	tenantExportDownloadOutput struct{ Body TenantExportDownload }
	// TenantExportDownloadPath names a download link's token.
	TenantExportDownloadPath struct {
		Token string `path:"token" maxLength:"512" pattern:"^[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+$" doc:"The link's token, as createTenantExportDownload gave it." example:"eyJ0IjoiMDE5MjhmNmUifQ.q1w2e3r4t5y6"`
	}
	tenantExportFile struct {
		ContentType        string `header:"Content-Type" doc:"application/zip."`
		ContentDisposition string `header:"Content-Disposition" doc:"attachment, with the ZIP's name."`
		CacheControl       string `header:"Cache-Control" doc:"no-store."`
		Body               []byte
	}
)

// tenantExportClaim is what a local tenant export link's token carries: its kind keeps it from opening an evidence file.
type tenantExportClaim struct {
	Kind         string    `json:"k"`
	Export       uuid.UUID `json:"t"`
	Organisation uuid.UUID `json:"o"`
	Expires      int64     `json:"x"`
}

// tenantExportKind is a tenant export link's kind.
const tenantExportKind = "tenant_export"

func (a *API) tenantExportToken(c tenantExportClaim) string {
	raw, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, a.downloadKey)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// readTenantExportToken is the claim of a tenant export token the API signed and that has not expired at now.
func (a *API) readTenantExportToken(token string, now time.Time) (tenantExportClaim, bool) {
	var c tenantExportClaim
	body, sig, ok := strings.Cut(token, ".")
	raw, err1 := base64.RawURLEncoding.DecodeString(body)
	got, err2 := base64.RawURLEncoding.DecodeString(sig)
	if !ok || err1 != nil || err2 != nil {
		return c, false
	}
	mac := hmac.New(sha256.New, a.downloadKey)
	mac.Write(raw)
	if !hmac.Equal(got, mac.Sum(nil)) || json.Unmarshal(raw, &c) != nil || c.Kind != tenantExportKind || now.Unix() >= c.Expires {
		return c, false
	}
	return c, true
}

func tenantExportOut(x storage.ReportExport) TenantExport {
	var p reportexport.Parameters
	_ = json.Unmarshal(x.Parameters, &p)
	out := TenantExport{ApprovalID: p.Approval, ExportID: x.ID.String(), Status: x.Status, RequestedBy: x.RequestedBy, RequestedAt: x.RequestedAt,
		Failure: x.Failure}
	if x.File != nil && x.Status == storage.ExportReady {
		out.File = &TenantExportFile{Name: x.File.FileName, SHA256: hex.EncodeToString(x.File.SHA256), Size: x.File.Size, Rows: x.File.Rows,
			Revision: x.File.Revision, ReadyAt: x.File.ReadyAt, ExpiresAt: x.File.ExpiresAt}
	}
	return out
}

// myTenantExport is a ready tenant export the caller asked for.
func (a *API) myTenantExport(ctx context.Context, u storage.UnitOfWork, path TenantExportPath) (storage.ReportExport, error) {
	id, err := parseID("export_id", path.ExportID)
	if err != nil {
		return storage.ReportExport{}, err
	}
	x, err := u.ReportExports().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && x.Report != reportexport.Tenant) {
		return storage.ReportExport{}, problem(CodeNotFound, "No full export has that ID.", "Take the export's ID from listTenantExports.")
	}
	if err != nil {
		return storage.ReportExport{}, err
	}
	if x.RequestedBy != uploader(ctx) {
		return storage.ReportExport{}, problem(CodeExportNotYours, "Another person asked for this export, and only they save it.",
			"Ask for the export yourself; another person approves it")
	}
	switch {
	case x.Status == storage.ExportExpired || (x.File != nil && !a.deps.Clock.Now().Before(x.File.ExpiresAt)):
		return x, problem(CodeExportExpired, "The export's 24 hours are up, and its file is gone.", "Ask for the export again")
	case x.Status != storage.ExportReady || x.File == nil:
		return x, problem(CodeExportNotReady, "The export is "+x.Status+", so it has no file to save.", "Wait until it is ready, or ask again if it failed")
	}
	return x, nil
}

func registerTenantExports(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.RequestTenantExport, huma.Operation{
		OperationID: "requestTenantExport", Method: http.MethodPost, Path: "/v1/tenant-exports", Tags: []string{"Data"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask for the organisation's full export",
		Description: "Asks for everything the organisation keeps in Fyniq as one ZIP (FR-MIG-04, design D10; rev 0.273): each table as CSV, " +
			"each document's versions as JSON, the evidence and payment files, the audit trail with its hashes, and a manifest of every " +
			"file's SHA-256, for exit and portability. Bank account numbers are in clear [counsel to confirm]. It is dual controlled " +
			"(FM-1): the answer is the approval request, which another person who holds A on \"Tenant export\" approves; the approval " +
			"queues the export for the worker, and its requester saves it with createTenantExportDownload. Side effects: the approval " +
			"request and its audit event. Idempotent with Idempotency-Key.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired, approval.CodePending}, locks...),
		func(ctx context.Context, in *tenantExportRequestInput) (*tenantExportRequestOutput, error) {
			requester := uploader(ctx)
			resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, struct{}{}, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				key := uuid.Must(uuid.NewV7())
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectTenantExport, ObjectKey: key.String(),
					Operation: approval.OpTenantExportPrepare, Summary: "Export all of the organisation's data, documents, files and audit trail",
					Execute: map[string]any{"export_key": key.String(), "requested_by": requester},
					Diff: []ApprovalDiff{{Field: "export", After: "every table, document, file and the audit trail, bank account numbers in clear"},
						{Field: "saved_by", After: requester}}})
			})
			if err != nil {
				return nil, err
			}
			return &tenantExportRequestOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
		})

	register(a, authz.ListTenantExports, huma.Operation{
		OperationID: "listTenantExports", Method: http.MethodGet, Path: "/v1/tenant-exports", Tags: []string{"Data"},
		Summary: "List the organisation's full exports",
		Description: "The organisation's full exports (FR-MIG-04), newest first, a page at a time (FR-INT-10): those awaiting approval, then " +
			"those approved, each with who asked, its status and, once ready, its file's name, size, hash and expiry. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *PageQuery) (*tenantExportListOutput, error) {
		out := TenantExportList{Items: []TenantExport{}}
		p, _ := authn.From(ctx)
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			pending, err := u.Approvals().List(ctx, string(approval.Pending))
			if err != nil {
				return err
			}
			var all []TenantExport
			now := a.deps.Clock.Now()
			for _, r := range pending {
				if r.ObjectType != approval.ObjectTenantExport || approval.StatusAt(requestOf(r), now) != approval.Pending {
					continue
				}
				s, err := storedOf(r)
				if err != nil {
					return err
				}
				by, _ := s.Execute["requested_by"].(string)
				all = append(all, TenantExport{ApprovalID: r.ID.String(), Status: "awaiting_approval", RequestedBy: by, RequestedAt: r.RequestedAt})
			}
			slices.SortFunc(all, func(x, y TenantExport) int { return y.RequestedAt.Compare(x.RequestedAt) })
			xs, err := u.ReportExports().OfReport(ctx, reportexport.Tenant, maxExportsListed)
			if err != nil {
				return err
			}
			for _, x := range xs {
				all = append(all, tenantExportOut(x))
			}
			page, next, err := pageOf(all, func(x TenantExport) string { return x.ApprovalID + "/" + x.ExportID }, *in)
			out.Items, out.NextCursor = append(out.Items, page...), next
			return err
		})
		return &tenantExportListOutput{Body: out}, err
	})

	register(a, authz.CreateTenantExportDownload, exempt(exempt(huma.Operation{
		OperationID: "createTenantExportDownload", Method: http.MethodPost, Path: "/v1/tenant-exports/{export_id}/download", Tags: []string{"Data"},
		Summary: "Get a link that saves the full export",
		Description: "A link that works for 5 minutes and serves the export's ZIP as an attachment (FR-MIG-04, SEC-24): a signed URL of the " +
			"stored file in the cloud, a signed path of the API locally. Only the person who asked for the export gets one (REPORT-07), " +
			"while it is ready (REPORT-05) and within its 24 hours (REPORT-06). Side effects: an audit event of the link, with the file's " +
			"hash, as a sensitive export (AUD-05). Never cached.",
	}, "Idempotency-Key", "Each request gives a new link that expires in 5 minutes; nothing is stored, so a retry harms nothing."),
		"Prefer", "A link changes no record: a preview would be the link itself."),
		append([]string{CodeNotFound, CodeExportNotReady, CodeExportExpired, CodeExportNotYours, CodeUnavailable}, locks...),
		func(ctx context.Context, in *TenantExportPath) (*tenantExportDownloadOutput, error) {
			p, _ := authn.From(ctx)
			now := a.deps.Clock.Now()
			var out TenantExportDownload
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				x, err := a.myTenantExport(ctx, u, *in)
				if err != nil {
					return err
				}
				if a.deps.Staging == nil {
					return problem(CodeUnavailable, "This installation has no store that keeps the exports.", "Ask the operator to configure the staging store.")
				}
				out = TenantExportDownload{ExpiresAt: now.Add(downloadTTL).UTC(), FileName: x.File.FileName, SHA256: hex.EncodeToString(x.File.SHA256)}
				key := reportexport.Key(p.Organisation, x.ID, x.File.FileName)
				if signer, ok := a.deps.Staging.(ports.DownloadLinker); ok {
					if out.URL, err = signer.DownloadURL(ctx, key, ports.Download{FileName: x.File.FileName, MediaType: x.File.MediaType, TTL: downloadTTL}, now); err != nil {
						return err
					}
				} else {
					out.URL = a.deps.APIBase + "/v1/tenant-export-downloads/" +
						a.tenantExportToken(tenantExportClaim{Kind: tenantExportKind, Export: x.ID, Organisation: p.Organisation, Expires: now.Add(downloadTTL).Unix()})
				}
				return u.Audit().Record(ctx, storage.AuditRecord{Action: "tenant_export.download_linked", ObjectType: "report_export", ObjectID: x.ID,
					Changes: map[string][2]any{"link": {nil, map[string]any{"file": x.File.FileName, "sha256": out.SHA256,
						"expires_at": out.ExpiresAt.Format(time.RFC3339)}}}})
			})
			if err != nil {
				return nil, err
			}
			return &tenantExportDownloadOutput{Body: out}, nil
		})

	register(a, authz.GetTenantExportDownload, huma.Operation{
		OperationID: "getTenantExportDownload", Method: http.MethodGet, Path: "/v1/tenant-export-downloads/{token}", Tags: []string{"Data"},
		Summary: "Save the full export through its link",
		Description: "Public: no session; the link's token is the right, for 5 minutes, and only its requester is given one (SEC-24, FR-MIG-04). " +
			"The export's ZIP itself, served as an attachment and checked against the SHA-256 recorded when it was written. A token that is " +
			"not the API's, or expired, and an export past its 24 hours, is not found. Only the links of an installation without signed URLs " +
			"of its store come here. Never cached.",
		Responses: map[string]*huma.Response{"200": {Description: "The export's ZIP, as an attachment with its name.",
			Content: map[string]*huma.MediaType{"application/zip": {Schema: &huma.Schema{Type: "string", Format: "binary", Examples: []any{"PK\u0003\u0004 manifest.json"}}}}}},
	}, []string{CodeNotFound, CodeUnavailable}, func(ctx context.Context, in *TenantExportDownloadPath) (*tenantExportFile, error) {
		gone := problem(CodeNotFound, "That link does not open an export, or it expired.", "Ask for a new link from Settings › Data.")
		now := a.deps.Clock.Now()
		c, ok := a.readTenantExportToken(in.Token, now)
		if !ok {
			return nil, gone
		}
		var out *tenantExportFile
		err := a.deps.Store.Do(ctx, c.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			x, err := u.ReportExports().Get(ctx, c.Export)
			if err != nil || x.Report != reportexport.Tenant || x.Status != storage.ExportReady || x.File == nil || !now.Before(x.File.ExpiresAt) {
				return gone
			}
			if a.deps.Staging == nil {
				return problem(CodeUnavailable, "This installation has no store that keeps the exports.", "Ask the operator to configure the staging store.")
			}
			r, err := a.deps.Staging.Get(ctx, reportexport.Key(c.Organisation, x.ID, x.File.FileName))
			if errors.Is(err, ports.ErrNotFound) {
				return gone
			}
			if err != nil {
				return err
			}
			defer r.Close()
			data, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			if sum := sha256.Sum256(data); !hmac.Equal(sum[:], x.File.SHA256) {
				return errors.New("api: the stored export is not the one recorded")
			}
			out = &tenantExportFile{ContentType: "application/zip", CacheControl: noStore,
				ContentDisposition: mime.FormatMediaType("attachment", map[string]string{"filename": x.File.FileName}), Body: data}
			return nil
		})
		return out, err
	})
}

// executeTenantExport queues an approved full tenant export for the worker, for the person who asked for it.
func (a *API) executeTenantExport(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	by, _ := s.Execute["requested_by"].(string)
	if by == "" {
		by = "user:" + r.RequestedBy.String()
	}
	now := a.deps.Clock.Now()
	params, err := json.Marshal(reportexport.Parameters{To: now.UTC().Format(time.DateOnly), Approval: r.ID.String()})
	if err != nil {
		return err
	}
	x, err := u.ReportExports().Ask(ctx, storage.NewReportExport{Report: reportexport.Tenant, Format: "zip", Parameters: params, RequestedBy: by,
		RequestedAt: now})
	if err != nil {
		return err
	}
	if a.deps.Outbox != nil {
		p, _ := authn.From(ctx)
		_, err = reportexport.Queue(ctx, *a.deps.Outbox, u, p.Organisation, x)
		return err
	}
	item, err := reportexport.Item(x) // no dispatcher here: the tick sends it
	if err != nil {
		return err
	}
	_, err = u.Outbox().Enqueue(ctx, item)
	return err
}
