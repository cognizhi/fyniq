package gcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

// What Cloud Storage receives for retention (FR-DOC-02): a retain-until in Locked mode, which the bucket
// then refuses to shorten, and a temporary hold for legal hold, which does not restart the clock.
// req: FR-DOC-02
// asvs: V14.2.7
func TestStorageSendsLockedRetentionAndTemporaryHold(t *testing.T) {
	var patches []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			b, _ := io.ReadAll(r.Body)
			patches = append(patches, string(b))
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	s := NewStorage(srv.URL, "fyniq-evidence", nil, http.DefaultClient)
	until := time.Date(2034, 3, 31, 16, 0, 0, 0, time.UTC)
	if err := s.SetRetainUntil(context.Background(), "evidence/o/1", until); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLegalHold(context.Background(), "evidence/o/1", true); err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 || patches[0] != `{"retention":{"mode":"Locked","retainUntilTime":"2034-03-31T16:00:00Z"}}` ||
		!strings.Contains(patches[1], `"temporaryHold":true`) {
		t.Fatalf("patches = %q", patches)
	}
}

// The anchors' bucket must be locked (CTL-AUD-02): the adapter reads the retention policy as Cloud
// Storage reports it, with the service's token, and an unlocked or missing policy reads as such.
// req: CTL-AUD-02
// asvs: V16.4.2
func TestStorageReadsTheRetentionLock(t *testing.T) {
	answer := `{"retentionPolicy":{"retentionPeriod":"315360000","effectiveTime":"2026-10-15T00:00:00Z","isLocked":true}}`
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/storage/v1/b/fyniq-anchors" || r.URL.Query().Get("fields") != "retentionPolicy" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(answer))
	}))
	defer srv.Close()
	s := NewStorage(srv.URL, "fyniq-anchors", oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "service-token"}), http.DefaultClient)
	r, err := s.Retention(context.Background())
	if err != nil || !r.Locked || r.Period != 315360000*time.Second || auth != "Bearer service-token" {
		t.Fatalf("Retention = %+v, %v (auth %q)", r, err, auth)
	}
	answer = `{}`
	if r, err := s.Retention(context.Background()); err != nil || r.Locked || r.Period != 0 {
		t.Fatalf("a bucket without a policy: %+v, %v", r, err)
	}
}

// An object's retention as Cloud Storage reports it (CTL-AUD-03's daily check): Locked mode only when the mode says so;
// an Unlocked retention, which a privileged account could shorten, or none, reads as not locked.
// req: CTL-AUD-03
// asvs: V14.2.7
func TestStorageReadsAnObjectsRetention(t *testing.T) {
	answer := `{"retention":{"mode":"Locked","retainUntilTime":"2034-03-31T16:00:00Z"},"temporaryHold":true}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/storage/v1/b/fyniq-evidence/o/evidence/o/1" || r.URL.Query().Get("fields") != "retention,temporaryHold,metadata" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(answer))
	}))
	defer srv.Close()
	s := NewStorage(srv.URL, "fyniq-evidence", nil, http.DefaultClient)
	r, err := s.RetentionOf(context.Background(), "evidence/o/1")
	if err != nil || !r.Locked || !r.Held || !r.RetainUntil.Equal(time.Date(2034, 3, 31, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("RetentionOf = %+v, %v", r, err)
	}
	answer = `{"retention":{"mode":"Unlocked","retainUntilTime":"2034-03-31T16:00:00Z"}}`
	if r, err := s.RetentionOf(context.Background(), "evidence/o/1"); err != nil || r.Locked || r.Held {
		t.Fatalf("an unlocked retention = %+v, %v", r, err)
	}
	if _, err := s.RetentionOf(context.Background(), "evidence/o/2"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("a missing object: %v", err)
	}
}

// A blob longer than a chunk goes as a resumable upload, a chunk at a time, so it is never held whole (NFR-PERF-05, rev
// 0.250): every chunk but the last says its range of an unknown total and is kept (308), the last names the size; one
// that fits goes in one request. A reader that fails cancels the session, so the store keeps none of it; a session on
// another host is refused, so the token goes nowhere else.
// req: NFR-PERF-05
func TestStorageUploadsALongBlobAChunkAtATime(t *testing.T) {
	defer func(n int) { Chunk = n }(Chunk)
	Chunk = 4
	var calls []string
	var kept strings.Builder
	session := "/upload/session/1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == http.MethodGet: // Put reads whether the blob is retained: none is yet
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Query().Get("uploadType") == "media":
			calls = append(calls, "media "+string(b))
		case r.Method == http.MethodPost && r.URL.Query().Get("uploadType") == "resumable":
			calls = append(calls, "start "+r.URL.Query().Get("name"))
			w.Header().Set("Location", "http://"+r.Host+session)
		case r.Method == http.MethodPut && r.URL.Path == session:
			rng := r.Header.Get("Content-Range")
			calls = append(calls, rng)
			kept.Write(b)
			if strings.HasSuffix(rng, "/*") {
				w.WriteHeader(http.StatusPermanentRedirect)
			}
		case r.Method == http.MethodDelete && r.URL.Path == session:
			calls = append(calls, "cancel")
			w.WriteHeader(499)
		}
	}))
	defer srv.Close()
	s := NewStorage(srv.URL, "fyniq-exports", nil, http.DefaultClient)
	ctx := context.Background()

	if err := s.Put(ctx, "exports/1/gl.csv", strings.NewReader("hello, world!")); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls, "; "); got != "start exports/1/gl.csv; bytes 0-3/*; bytes 4-7/*; bytes 8-11/*; bytes 12-12/13" || kept.String() != "hello, world!" {
		t.Errorf("a long blob: %s, kept %q", got, kept.String())
	}
	calls, kept = nil, strings.Builder{}
	if err := s.Put(ctx, "exports/1/eight", strings.NewReader("12345678")); err != nil || strings.Join(calls, "; ") != "start exports/1/eight; bytes 0-3/*; bytes 4-7/8" {
		t.Errorf("a blob of whole chunks: %v %s", err, strings.Join(calls, "; "))
	}
	calls = nil
	if err := s.Put(ctx, "exports/1/short", strings.NewReader("abc")); err != nil || strings.Join(calls, "; ") != "media abc" {
		t.Errorf("a blob that fits: %v %s", err, strings.Join(calls, "; "))
	}
	calls = nil
	broken := io.MultiReader(strings.NewReader("12345678"), iotest.ErrReader(errors.New("the export stopped")))
	if err := s.Put(ctx, "exports/1/broken", broken); err == nil || calls[len(calls)-1] != "cancel" {
		t.Errorf("a reader that fails: %v %s", err, strings.Join(calls, "; "))
	}

	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Location", "https://attacker.example/upload/1")
	}))
	defer elsewhere.Close()
	if err := NewStorage(elsewhere.URL, "fyniq-exports", nil, http.DefaultClient).Put(ctx, "exports/1/gl.csv", strings.NewReader("hello, world!")); err == nil {
		t.Error("a session on another host was used")
	}
}
