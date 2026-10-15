package gcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

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
	s := NewStorage(srv.URL, "fyniq-anchors", oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "service-token"}), nil)
	r, err := s.Retention(context.Background())
	if err != nil || !r.Locked || r.Period != 315360000*time.Second || auth != "Bearer service-token" {
		t.Fatalf("Retention = %+v, %v (auth %q)", r, err, auth)
	}
	answer = `{}`
	if r, err := s.Retention(context.Background()); err != nil || r.Locked || r.Period != 0 {
		t.Fatalf("a bucket without a policy: %+v, %v", r, err)
	}
}
