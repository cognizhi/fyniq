//go:build local

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/ports"
)

// brokenConverter answers a preview the way a failing or hostile converter would.
type brokenConverter struct {
	out ports.Converted
	err error
}

func (c brokenConverter) Convert(context.Context, ports.ConvertKind, []byte) (ports.Converted, error) {
	return c.out, c.err
}

// A PDF evidence file previews as a picture the converter drew (FR-DOC-08's safe preview; rev 0.321): for whoever may open
// the file, the PNG's type decided from its content and scanned, with the SHA-256 of the PDF as stored and of the picture,
// both audited; a file that is not a PDF, or that the renderer cannot read, is refused in plain words; an installation
// without a converter, or a converter that is down, answers 503 and the file can still be saved; a picture the scan flags
// is never sent. A role that does not view the record, the AI and an unknown file are refused as opening is.
// req: FR-DOC-08
// req: FR-DOC-01
// req: documents/attachments-and-evidence PDF preview through the converter (rev 0.321)
// asvs: V5.2.3, V8.2.2
func TestAPDFEvidenceFilePreviewsAsAPicture(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	preview := func(id, tok string) response {
		t.Helper()
		return e.call(http.MethodGet, "/v1/evidence/"+id+"/preview", tok, nil)
	}
	bill, tag := e.createBill("200.00", "18.00")
	pdf := []byte("%PDF-1.7 AU-100, Amazon Web Services, S$218.00")
	up := e.upload("/v1/bills/"+bill+"/evidence", acc, map[string]string{"evidence_type": "invoice"}, "AU-100.pdf", pdf, "If-Match", tag)
	if up.Status != http.StatusCreated {
		t.Fatalf("attaching: %d %s", up.Status, up.Raw)
	}
	file := up.str("id")

	// No converter: 503, whoever asks.
	e.problemIs(preview(file, acc), "getEvidencePreview", http.StatusServiceUnavailable, CodeUnavailable)

	e.api.deps.Converter = local.StandInConverter{}
	got := preview(file, acc)
	picture, err := base64.StdEncoding.DecodeString(got.str("content_base64"))
	if err != nil || got.Status != http.StatusOK || got.str("media_type") != "image/png" || !strings.HasPrefix(string(picture), "\x89PNG\r\n\x1a\n") ||
		got.Body["size_bytes"] != float64(len(picture)) || got.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("a preview: %d %.300s", got.Status, got.Raw)
	}
	src, shot := sha256.Sum256(pdf), sha256.Sum256(picture)
	if got.str("source_sha256") != hex.EncodeToString(src[:]) || got.str("preview_sha256") != hex.EncodeToString(shot[:]) {
		t.Errorf("the hashes: %s", got.Raw)
	}
	audited := false
	for _, ev := range strings.Split(auditTrail(t, e), "\n") {
		if strings.Contains(ev, `"action":"evidence.opened"`) && strings.Contains(ev, file) && strings.Contains(ev, hex.EncodeToString(src[:])) &&
			strings.Contains(ev, hex.EncodeToString(shot[:])) {
			audited = true
		}
	}
	if !audited {
		t.Error("the preview is not audited as an opening with both hashes")
	}

	// A file that is not a PDF is DOC-12; one the renderer cannot read, too.
	var till bytes.Buffer
	if err := png.Encode(&till, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	pic := e.upload("/v1/bills/"+bill+"/evidence", acc, map[string]string{"evidence_type": "photo"}, "till.png", till.Bytes(),
		"If-Match", e.call(http.MethodGet, "/v1/bills/"+bill, acc, nil).Header.Get("ETag"))
	if pic.Status != http.StatusCreated {
		t.Fatalf("attaching a picture: %d %s", pic.Status, pic.Raw)
	}
	e.problemIs(preview(pic.str("id"), acc), "getEvidencePreview", http.StatusUnprocessableEntity, evidence.CodePreview)
	e.api.deps.Converter = brokenConverter{err: ports.ErrNotConvertible}
	e.problemIs(preview(file, acc), "getEvidencePreview", http.StatusUnprocessableEntity, evidence.CodePreview)

	// A converter that is down is 503; a picture the scan flags, or that is no PNG, is never sent.
	e.api.deps.Converter = brokenConverter{err: errors.New("503 from the converter")}
	e.problemIs(preview(file, acc), "getEvidencePreview", http.StatusServiceUnavailable, CodeUnavailable)
	e.api.deps.Converter = brokenConverter{out: ports.Converted{MediaType: "image/png", Data: append([]byte("\x89PNG\r\n\x1a\n"), local.EICAR...)}}
	e.problemIs(preview(file, acc), "getEvidencePreview", http.StatusUnprocessableEntity, evidence.CodeMalware)
	e.api.deps.Converter = brokenConverter{out: ports.Converted{MediaType: "image/png", Data: []byte("MZ an executable")}}
	e.problemIs(preview(file, acc), "getEvidencePreview", http.StatusUnprocessableEntity, evidence.CodePreview)

	// Who does not view bills, the AI and an unknown file are refused as opening is.
	e.api.deps.Converter = local.StandInConverter{}
	e.problemIs(preview(file, e.as(authz.ARSpecialist)), "getEvidencePreview", 404, CodeNotFound)
	if r := preview(file, e.as(authz.AI)); r.Status != http.StatusForbidden {
		t.Errorf("the AI previewed a file: %d %s", r.Status, r.Raw)
	}
	e.problemIs(preview(uuid.NewString(), acc), "getEvidencePreview", 404, CodeNotFound)
}
