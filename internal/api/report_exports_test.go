//go:build local

package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/reportexport"
)

// The accountant exports through the API (FR-RPT-09, FR-RPT-10): asked for, 202 and pending; refused without the first
// day of a range; listed for the person who asked; another person's is REPORT-07; not ready yet is REPORT-05. Once the
// worker has prepared it, it downloads with its hash, audited; past its 24 hours it is REPORT-06.
// req: FR-RPT-09
// req: FR-RPT-10
func TestAccountantExportsThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	me := e.as(authz.Accountant)
	r := e.call(http.MethodPost, "/v1/report-exports", me, map[string]any{"report": "trial_balance", "to": "2026-10-31", "format": "csv"})
	if r.Status != 202 || r.str("status") != "pending" || r.str("to") != "2026-10-31" {
		t.Fatalf("asking: %d %s", r.Status, r.Raw)
	}
	id := r.str("id")
	if bad := e.call(http.MethodPost, "/v1/report-exports", me, map[string]any{"report": "general_ledger", "to": "2026-10-31"}); bad.Status != 422 ||
		!strings.Contains(bad.Raw, `"field":"body.from"`) {
		t.Errorf("a range without its first day: %d %s", bad.Status, bad.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/report-exports", me, nil); l.Status != 200 || !strings.Contains(l.Raw, `"id":"`+id+`"`) {
		t.Errorf("my exports: %s", l.Raw)
	}
	if other := e.call(http.MethodGet, "/v1/report-exports/"+id, e.as(authz.Accountant), nil); other.Status != 403 || !strings.Contains(other.Raw, CodeExportNotYours) {
		t.Errorf("another person's: %d %s", other.Status, other.Raw)
	}
	if early := e.call(http.MethodGet, "/v1/report-exports/"+id+"/file", me, nil); early.Status != 409 || !strings.Contains(early.Raw, CodeExportNotReady) {
		t.Errorf("before it is ready: %d %s", early.Status, early.Raw)
	}

	worker := workerStore(t)
	p := reportexport.Preparer{Store: worker, Files: e.blobs, Outbox: outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: e.clock}, Clock: e.clock}
	payload, _ := json.Marshal(map[string]string{"export_id": id})
	if err := p.Work()(context.Background(), e.books.Org, storage.OutboxRow{Kind: reportexport.Kind, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if g := e.call(http.MethodGet, "/v1/report-exports/"+id, me, nil); g.str("status") != "ready" || g.str("file_name") != "trial-balance-2026-10-31.csv" {
		t.Errorf("once prepared: %s", g.Raw)
	}
	f := e.call(http.MethodGet, "/v1/report-exports/"+id+"/file", me, nil)
	content, _ := base64.StdEncoding.DecodeString(f.str("content_base64"))
	if f.Status != 200 || !strings.Contains(string(content), "Total, at 2026-10-31") {
		t.Errorf("the download: %d %s", f.Status, f.Raw)
	}
	if trail := auditTrail(t, e); !strings.Contains(trail, `"action":"report_export.downloaded"`) || !strings.Contains(trail, f.str("sha256")) {
		t.Errorf("the download's audit event: %s", trail)
	}
	e.clock.Set(e.clock.Now().Add(25 * time.Hour))
	if late := e.call(http.MethodGet, "/v1/report-exports/"+id+"/file", me, nil); late.Status != 410 || !strings.Contains(late.Raw, CodeExportExpired) {
		t.Errorf("past its 24 hours: %d %s", late.Status, late.Raw)
	}
}
