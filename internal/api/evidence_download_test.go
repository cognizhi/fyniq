//go:build local

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
)

// An evidence file is saved through a link that works for 5 minutes and serves it as an attachment with its name and type
// (SEC-24): for whoever may open the file, audited with its hash; a link that is not the API's, or expired, opens
// nothing, nor does one to a file removed since. Locally the link is the API's own signed path. getEvidenceFile, which
// answers in base64, announces its sunset.
// req: SEC-24
// req: FR-DOC-01
// req: FR-INT-06
// asvs: V5.3.2, V8.2.2
func TestAnEvidenceFileIsSavedThroughAShortLivedLink(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	acc := e.as(authz.Accountant)
	pdf := []byte("%PDF-1.7 AU-100, Amazon Web Services, S$218.00")
	bill, tag := e.createBill("200.00", "18.00")
	up := e.upload("/v1/bills/"+bill+"/evidence", acc, map[string]string{"evidence_type": "invoice"}, "../../etc/AU-100.pdf", pdf, "If-Match", tag)
	if up.Status != http.StatusCreated {
		t.Fatalf("attaching: %d %s", up.Status, up.Raw)
	}
	file := up.str("id")
	sum := hex.EncodeToString(func() []byte { s := sha256.Sum256(pdf); return s[:] }())
	link := func(tok string) response {
		t.Helper()
		return e.call(http.MethodPost, "/v1/evidence/"+file+"/download", tok, nil)
	}
	l := link(acc)
	if l.Status != http.StatusOK || !strings.HasPrefix(l.str("url"), "/api/v1/evidence-downloads/") || l.str("sha256") != sum ||
		l.str("file_name") != "AU-100.pdf" || l.str("expires_at") != e.clock.Now().Add(5*time.Minute).UTC().Format(time.RFC3339) {
		t.Fatalf("a download link: %d %s", l.Status, l.Raw)
	}
	path := strings.TrimPrefix(l.str("url"), "/api")
	got := e.call(http.MethodGet, path, "", nil)
	if got.Status != http.StatusOK || got.Raw != string(pdf) || got.Header.Get("Content-Type") != "application/pdf" ||
		got.Header.Get("Content-Disposition") != `attachment; filename=AU-100.pdf` || got.Header.Get("Cache-Control") != "no-store" ||
		got.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("saving through the link: %d %v %q", got.Status, got.Header, got.Raw)
	}
	linked := 0
	for _, ev := range strings.Split(auditTrail(t, e), "\n") {
		if strings.Contains(ev, `"action":"evidence.download_linked"`) && strings.Contains(ev, file) && strings.Contains(ev, sum) {
			linked++
		}
	}
	if linked != 1 {
		t.Errorf("%d links audited with the file's hash; want 1", linked)
	}

	// A link someone changed, or one that expired, opens nothing.
	token := strings.TrimPrefix(path, "/v1/evidence-downloads/")
	e.problemIs(e.call(http.MethodGet, "/v1/evidence-downloads/"+strings.Replace(token, ".", "x.", 1), "", nil), "getEvidenceDownload", 404, CodeNotFound)
	e.clock.Advance(5*time.Minute + time.Second)
	e.problemIs(e.call(http.MethodGet, path, "", nil), "getEvidenceDownload", 404, CodeNotFound)

	// Who may not open the file gets no link; the AI is refused; an unknown file is not found.
	for _, tok := range []string{e.as(authz.ARSpecialist), c.as(c.mei, authz.Employee)} {
		e.problemIs(link(tok), "createEvidenceDownload", 404, CodeNotFound)
	}
	if r := link(e.as(authz.AI)); r.Status != http.StatusForbidden {
		t.Errorf("the AI got a link: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/evidence/"+uuid.NewString()+"/download", acc, nil), "createEvidenceDownload", 404, CodeNotFound)

	// A file removed after its link was made is gone.
	fresh := link(acc)
	if r := e.call(http.MethodDelete, "/v1/bills/"+bill+"/evidence/"+file, acc, nil, "If-Match", "*"); r.Status >= 300 {
		t.Fatalf("removing the file: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, strings.TrimPrefix(fresh.str("url"), "/api"), "", nil), "getEvidenceDownload", 404, CodeNotFound)

	// The base64 answer announces its sunset (FR-INT-06).
	if r := e.call(http.MethodGet, "/v1/evidence/"+uuid.NewString()+"/file", acc, nil); r.Header.Get("Sunset") != "Mon, 31 May 2027 00:00:00 GMT" {
		t.Errorf("getEvidenceFile's sunset: %q", r.Header.Get("Sunset"))
	}
}
