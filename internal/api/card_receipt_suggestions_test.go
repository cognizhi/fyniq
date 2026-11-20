//go:build local

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// readCaptureOf puts a read capture of a receipt of total on date in the inbox, owned by owner (the address it arrived at), as the model's reading is kept.
func (e *env) readCaptureOf(owner uuid.UUID, name, total, date string) uuid.UUID {
	e.t.Helper()
	data := []byte("%PDF-1.7 " + name + " " + uuid.NewString())
	sum := sha256.Sum256(data)
	key := captureKey(e.books.Org)
	if err := e.api.deps.Staging.Put(context.Background(), key, bytes.NewReader(data)); err != nil {
		e.t.Fatal(err)
	}
	reading, _ := json.Marshal(map[string]any{"kind": "receipt", "supplier": map[string]any{"value": "ComfortDelGro", "confidence": 0.9},
		"currency": map[string]any{"value": "SGD", "confidence": 1}, "date": map[string]any{"value": date, "confidence": 1}, "total": map[string]any{"value": total, "confidence": 1}})
	var id uuid.UUID
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.DocumentCaptures().Capture(ctx, storage.NewDocumentCapture{Source: storage.CaptureUpload, FileName: name, MediaType: "application/pdf", Size: int64(len(data)),
			SHA256: hex.EncodeToString(sum[:]), BlobKey: key, CapturedBy: "system:email-in", Status: storage.CaptureReceived, OwnerUserID: &owner, At: e.clock.Now()})
		if err != nil {
			return err
		}
		if c, err = u.DocumentCaptures().Status(ctx, c.ID, c.Version, storage.CaptureReading, ""); err != nil {
			return err
		}
		c, err = u.DocumentCaptures().Reading(ctx, c.ID, c.Version, storage.CapturedReading{Kind: "receipt", Reading: reading, Model: "m", Prompt: "document_capture@1",
			Checks: json.RawMessage(`{"checks":[],"confidence":{},"low":[]}`), At: e.clock.Now()})
		id = c.ID
		return err
	})
	return id
}

// FR-AP-08 (p2-bank-feeds-payments task 3.1): a receipt the holder's own address took in, read by the model, of the line's amount to the cent near its
// booking date is suggested for it; another member's capture, another amount and a far date are not. Attaching needs the line completed, takes only a
// suggestion, puts the file on what completing it made and dismisses the capture; it is not offered again.
// req: FR-AP-08
// req: FR-DOC-05
// asvs: V8.2.2
func TestACapturedReceiptIsSuggestedForACardLineAndAttachedByItsHolder(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	travel, _ := e.claimSetup()
	ahkow, mei := c.as(c.ahkow, authz.Employee), c.as(c.mei, authz.Employee)
	card := e.cardOf(c.ahkow)
	line := e.lineOn(card, "-27.30", nil) // booked 20 October 2026
	mine := e.readCaptureOf(uuid.MustParse(c.ahkow), "taxi.pdf", "27.30", "2026-10-19")
	theirs := e.readCaptureOf(uuid.MustParse(c.mei), "mei.pdf", "27.30", "2026-10-19")
	e.readCaptureOf(uuid.MustParse(c.ahkow), "dinner.pdf", "45.00", "2026-10-19")
	e.readCaptureOf(uuid.MustParse(c.ahkow), "old.pdf", "27.30", "2026-09-01")
	path := "/v1/card-lines/" + line.ID.String() + "/receipt-suggestions"

	got := e.call(http.MethodGet, path, ahkow, nil)
	list, _ := got.Body["items"].([]any)
	if got.Status != 200 || len(list) != 1 || list[0].(map[string]any)["capture_id"] != mine.String() || list[0].(map[string]any)["total"] != "27.30" {
		t.Fatalf("the suggestions: %d %s", got.Status, got.Raw)
	}
	if r := e.call(http.MethodGet, path, mei, nil); r.Status != 404 {
		t.Errorf("another member's read of the line's suggestions: %d %s", r.Status, r.Raw)
	}
	attach := func(who string, capture uuid.UUID, tag string) response {
		return e.call(http.MethodPost, path+"/attach", who, map[string]any{"capture_id": capture.String()}, "If-Match", tag)
	}
	e.problemIs(attach(ahkow, mine, etag(line.Version)), "attachCardReceiptSuggestion", 409, CodeCardNotDone)

	done := e.call(http.MethodPost, "/v1/card-lines/"+line.ID.String()+"/complete", ahkow, map[string]any{"category_id": travel, "gst_code": "NR", "description": "Taxi"},
		"If-Match", etag(line.Version))
	if done.Status != 200 {
		t.Fatalf("completing: %d %s", done.Status, done.Raw)
	}
	tag := done.Header.Get("ETag")
	for _, other := range []uuid.UUID{theirs, uuid.New()} {
		if r := attach(ahkow, other, tag); r.Status != 404 {
			t.Errorf("attaching a capture that is no suggestion: %d %s", r.Status, r.Raw)
		}
	}
	if r := attach(mei, mine, tag); r.Status != 404 {
		t.Errorf("another member attaching: %d %s", r.Status, r.Raw)
	}
	r := attach(ahkow, mine, tag)
	if r.Status != 201 || r.str("document_id") != done.str("document_id") || r.str("evidence_type") != "receipt" || r.str("file_name") != "taxi.pdf" {
		t.Fatalf("attaching: %d %s", r.Status, r.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/card-lines/"+line.ID.String()+"/evidence", ahkow, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("its receipt: %s", l.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		if cp, err := u.DocumentCaptures().Get(ctx, mine); err != nil || cp.Status != storage.CaptureDismissed {
			t.Errorf("the capture is %+v %v, want dismissed", cp.Status, err)
		}
		return nil
	})
	if again := e.call(http.MethodGet, path, ahkow, nil); len(again.Body["items"].([]any)) != 0 {
		t.Errorf("offered again: %s", again.Raw)
	}
	if r := attach(ahkow, mine, tag); r.Status != 404 {
		t.Errorf("attaching it twice: %d %s", r.Status, r.Raw)
	}
}
