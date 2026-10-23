package gcp

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/clock"
)

// The api and the worker call the render service with an ID token of their own account for its audience, minted by
// the metadata server and reused until shortly before it expires (PRD-001 §12.3, rev 0.96).
// req: SEC-03
// asvs: V13.2.1
func TestIdentityTokensAreMintedForTheAudienceAndReused(t *testing.T) {
	var calls atomic.Int32
	exp := clock.System{}.Now().Add(time.Hour).Unix() // the token's own expiry, as the metadata server sets it
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"aud":"https://render.example.run.app","exp":` + strconv.FormatInt(exp, 10) + `}`))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Metadata-Flavor") != "Google" || r.URL.Query().Get("audience") != "https://render.example.run.app" || r.URL.Query().Get("format") != "full" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("h." + payload + ".s\n"))
	}))
	defer srv.Close()
	tok := func() func(context.Context) (string, error) {
		src := oauth2.ReuseTokenSourceWithExpiry(nil, identity{audience: "https://render.example.run.app", client: srv.Client(), endpoint: srv.URL}, time.Minute)
		return func(context.Context) (string, error) { t, err := src.Token(); return t.AccessToken, err }
	}()
	for range 3 {
		got, err := tok(context.Background())
		if err != nil || got != "h."+payload+".s" {
			t.Fatalf("the token: %q %v", got, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("the metadata server was asked %d times, not once", calls.Load())
	}
	if _, err := (identity{audience: "x", client: srv.Client(), endpoint: srv.URL}).Token(); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("another audience: %v", err)
	}
	if _, err := expiry("not-a-jwt"); err == nil {
		t.Error("a token that is not a JWT was read")
	}
}
