//go:build local

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
)

// A HEIC photo is refused with a plain message while the installation has no converter, and converted to a JPEG once it
// has one (FR-DOC-08; PRD-001 rev 0.292, the owner's decision of 18 Nov 2026): the file kept is the JPEG, named .jpg, its
// SHA-256 and the SHA-256 of the photo as received both recorded (FR-DOC-07), and it opens as a picture. A file of no type kept is refused as before, and a malicious one is flagged before it is converted.
// req: FR-DOC-08
// req: FR-DOC-07
func TestAHEICPhotoIsConvertedOnceTheInstallationHasAConverter(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	photo := []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic\x00\x00\x00\x08meta photo bytes")
	id, tag := e.createDraft("100.00")
	attach := func(name string, data []byte) response {
		return e.upload("/v1/journals/"+id+"/evidence", acc, map[string]string{"evidence_type": "receipt"}, name, data, "If-Match", tag)
	}

	r := attach("IMG_2201.HEIC", photo)
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, evidence.CodeType) || !strings.Contains(r.Raw, "Export it as JPEG") {
		t.Fatalf("without a converter: %d %s", r.Status, r.Raw)
	}

	e.api.deps.Converter = local.StandInConverter{}
	r = attach("IMG_2201.HEIC", photo)
	if r.Status != http.StatusCreated {
		t.Fatalf("with a converter: %d %s", r.Status, r.Raw)
	}
	sum := sha256.Sum256(photo)
	if r.str("media_type") != "image/jpeg" || r.str("file_name") != "IMG_2201.jpg" || r.str("original_sha256") != hex.EncodeToString(sum[:]) ||
		r.str("sha256") == r.str("original_sha256") {
		t.Errorf("the converted file: %s", r.Raw)
	}

	if r := attach("EICAR.heic", append(append([]byte{}, photo...), local.EICAR...)); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, evidence.CodeMalware) {
		t.Errorf("a malicious HEIC: %d %s", r.Status, r.Raw)
	}
	if r := attach("tool.heic", []byte("MZ\x90\x00 an executable")); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, evidence.CodeType) {
		t.Errorf("a file that is not a photo or any type kept: %d %s", r.Status, r.Raw)
	}
}

// The largest file is 25 MB, and an organisation may lower it in its settings (FR-DOC-08: "up to 25 MB per file
// (setting)"): a file above the limit is refused saying the limit, one within it is kept, and a limit is never above 25 MB.
// req: FR-DOC-08
func TestAnOrganisationMayLowerTheLargestEvidenceFile(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	id, tag := e.createDraft("100.00")
	pdf := func(n int) []byte { return append([]byte("%PDF-1.7 "), make([]byte, n)...) }
	attach := func(data []byte) response {
		return e.upload("/v1/journals/"+id+"/evidence", acc, map[string]string{"evidence_type": "receipt"}, "scan.pdf", data, "If-Match", tag)
	}
	if r := attach(pdf(2 << 20)); r.Status != http.StatusCreated {
		t.Fatalf("2 MB under the default limit: %d %s", r.Status, r.Raw)
	}
	e.setEvidence("evidence.max_file_mb", "25", "1", 1)
	if r := attach(pdf(2 << 20)); r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, evidence.CodeSize) || !strings.Contains(r.Raw, "the limit is 1 MB") {
		t.Errorf("2 MB above a 1 MB limit: %d %s", r.Status, r.Raw)
	}
	if r := attach(pdf(500 << 10)); r.Status != http.StatusCreated {
		t.Errorf("500 KB within a 1 MB limit: %d %s", r.Status, r.Raw)
	}
}
