//go:build local

package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/reportexport"
)

// prepareTenantExports runs the worker's job for every full export still pending, as the worker does.
func (e *env) prepareTenantExports() {
	e.t.Helper()
	worker := workerStore(e.t)
	p := reportexport.Preparer{Store: worker, Files: e.blobs, Outbox: outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: e.clock}, Clock: e.clock,
		Tenant: reportexport.TenantSources{Staging: e.blobs, Evidence: e.blobs, Fields: testFields(e.t)}}
	var pending []storage.ReportExport
	if err := worker.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		pending, err = u.ReportExports().OfReport(ctx, reportexport.Tenant, 100)
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	for _, x := range pending {
		if x.Status != storage.ExportPending {
			continue
		}
		payload, _ := json.Marshal(map[string]string{"export_id": x.ID.String()})
		if err := p.Work()(context.Background(), e.books.Org, storage.OutboxRow{Kind: reportexport.Kind, Payload: payload}); err != nil {
			e.t.Fatal(err)
		}
	}
}

// The full tenant export (FR-MIG-04, design D10; rev 0.273) is dual controlled (FM-1): an Administrator asks; they do
// not approve their own request; an Owner who did not ask approves it, and the approval queues it for the worker. The
// list shows it awaiting approval, pending, then ready. Only its requester gets a link: an Owner is refused
// (REPORT-07). The link saves the ZIP, whose manifest's hashes match its files, with the vendor's bank account number and
// the released payment file in clear; each link is audited as a sensitive export (AUD-05). The report exports never show
// it.
// req: FR-MIG-04
// req: AUD-05
// req: FM-1
// asvs: V8.2.1, V14.2.1
func TestTheFullExportIsDualControlledAndSavedByItsRequester(t *testing.T) {
	e := newEnv(t)
	vendor := e.vendor("Northwind Analytics Pte Ltd")
	e.vendorAccount(vendor, "0123456789")
	e.releasedBatch("bill:" + e.billOf(vendor, "100.00", "9.00"))
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner) // the requester holds A as well: FM-1 refuses it
	list := func(who string) []map[string]any {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/tenant-exports", who, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("listTenantExports: %d %s", r.Status, r.Raw)
		}
		var out []map[string]any
		for _, x := range r.Body["items"].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}

	asked := e.call(http.MethodPost, "/v1/tenant-exports", admin, nil)
	if asked.Status != http.StatusAccepted || asked.str("status") != "pending" || asked.str("object_type") != "tenant_export" {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	if l := list(owner); len(l) != 1 || l[0]["status"] != "awaiting_approval" || l[0]["approval_id"] != asked.str("id") ||
		l[0]["requested_by"] != "user:"+f.ahkow || l[0]["export_id"] != nil {
		t.Fatalf("awaiting approval: %v", l)
	}
	e.problemIs(f.decide(admin, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
	if a := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); a.Status != http.StatusOK || a.str("status") != "approved" {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}
	l := list(admin)
	if len(l) != 1 || l[0]["status"] != "pending" || l[0]["approval_id"] != asked.str("id") || l[0]["export_id"] == nil {
		t.Fatalf("approved: %v", l)
	}
	id := l[0]["export_id"].(string)
	e.problemIs(e.call(http.MethodPost, "/v1/tenant-exports/"+id+"/download", admin, nil), "createTenantExportDownload", 409, CodeExportNotReady)

	e.prepareTenantExports()
	l = list(admin)
	file, _ := l[0]["file"].(map[string]any)
	if l[0]["status"] != "ready" || file == nil || file["name"] == "" || file["rows"].(float64) == 0 {
		t.Fatalf("ready: %v", l)
	}
	other := f.as(f.weiling, authz.Administrator) // C on the row, but not the requester
	e.problemIs(e.call(http.MethodPost, "/v1/tenant-exports/"+id+"/download", other, nil), "createTenantExportDownload", 403, CodeExportNotYours)
	link := e.call(http.MethodPost, "/v1/tenant-exports/"+id+"/download", admin, nil)
	if link.Status != http.StatusOK || !strings.HasPrefix(link.str("url"), "/api/v1/tenant-export-downloads/") || link.str("sha256") != file["sha256"] {
		t.Fatalf("the link: %d %s", link.Status, link.Raw)
	}
	saved := e.call(http.MethodGet, strings.TrimPrefix(link.str("url"), "/api"), "", nil)
	if saved.Status != http.StatusOK || saved.Header.Get("Content-Type") != "application/zip" || !strings.Contains(saved.Header.Get("Content-Disposition"), file["name"].(string)) {
		t.Fatalf("saving: %d %v", saved.Status, saved.Header)
	}
	sum := sha256.Sum256([]byte(saved.Raw))
	if hex.EncodeToString(sum[:]) != file["sha256"] {
		t.Errorf("the saved ZIP's hash %x is not the export's %s", sum, file["sha256"])
	}
	zr, err := zip.NewReader(bytes.NewReader([]byte(saved.Raw)), int64(len(saved.Raw)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, zf := range zr.File {
		r, err := zf.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r)
		_ = r.Close()
		files[zf.Name] = string(b)
	}
	var m reportexport.Manifest
	if err := json.Unmarshal([]byte(files["manifest.json"]), &m); err != nil {
		t.Fatal(err)
	}
	for _, mf := range m.Files {
		s := sha256.Sum256([]byte(files[mf.Path]))
		if hex.EncodeToString(s[:]) != mf.SHA256 {
			t.Errorf("%s does not match the manifest", mf.Path)
		}
	}
	if !strings.Contains(files["tables/payee_bank_account.csv"], `"0123456789"`) {
		t.Errorf("the vendor's bank account number in clear: %s", files["tables/payee_bank_account.csv"])
	}
	released := false
	for name := range files {
		released = released || strings.HasPrefix(name, "files/payment-batch-releases/")
	}
	if !released {
		t.Error("the released payment file is not in the export")
	}
	// A token that is not the API's saves nothing.
	if r := e.call(http.MethodGet, "/v1/tenant-export-downloads/eyJrIjoidGVuYW50X2V4cG9ydCJ9.AAAA", "", nil); r.Status != http.StatusNotFound {
		t.Errorf("a forged token: %d", r.Status)
	}
	// The link is a sensitive export (AUD-05); the report exports never show the full export.
	if ev := e.call(http.MethodGet, "/v1/audit/events?action=tenant_export.download_linked", owner, nil); !strings.Contains(ev.Raw, id) {
		t.Errorf("the link's audit event: %d %s", ev.Status, ev.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/report-exports", admin, nil); strings.Contains(r.Raw, id) {
		t.Errorf("the report exports list the full export: %s", r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/report-exports/"+id, admin, nil), "getReportExport", 404, CodeNotFound)
}

// matrixTenantExport is a ready full export of the matrix's organisation asked for by tok's person, as the approval
// leaves it, prepared by the worker: its ID.
func matrixTenantExport(e *env, tok string) string {
	e.t.Helper()
	e.tokens.mu.Lock()
	p := e.tokens.m[tok]
	e.tokens.mu.Unlock()
	by := "user:" + p.Subject
	if p.UserID != uuid.Nil {
		by = "user:" + p.UserID.String()
	}
	var id string
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "matrix"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		params, _ := json.Marshal(reportexport.Parameters{To: "2026-10-15"})
		x, err := u.ReportExports().Ask(ctx, storage.NewReportExport{Report: reportexport.Tenant, Format: "zip", Parameters: params, RequestedBy: by,
			RequestedAt: e.clock.Now()})
		id = x.ID.String()
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	e.prepareTenantExports()
	return id
}

// matrixTenantExports are the authorisation matrix's rows of the full export (FR-MIG-04): asking under C on "Tenant
// export", stepped up; listing under any right on it; a link to the caller's own ready export under C; and the public
// link, its token the right.
var matrixTenantExports = map[string]func(e *env, tok string) (response, func() bool){
	"requestTenantExport": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/tenant-exports", steppedUp(e, tok), nil, "Prefer", "return=preview"), nil
	},
	"listTenantExports": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/tenant-exports", tok, nil), nil
	},
	"createTenantExportDownload": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/tenant-exports/"+matrixTenantExport(e, tok)+"/download", tok, nil), nil
	},
	"getTenantExportDownload": func(e *env, tok string) (response, func() bool) {
		admin := e.as(authz.Administrator)
		l := e.call(http.MethodPost, "/v1/tenant-exports/"+matrixTenantExport(e, admin)+"/download", admin, nil)
		return e.call(http.MethodGet, strings.TrimPrefix(l.str("url"), "/api"), tok, nil), nil
	},
}
