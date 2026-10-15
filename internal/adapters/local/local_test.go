//go:build local

package local

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/runtime"
)

// req: NFR-PORT-04
func TestSecretsConformance(t *testing.T) {
	porttest.Secrets(t, func(t *testing.T, seed map[string]string) ports.Secrets {
		return NewSecrets(func(k string) (string, bool) {
			v, ok := seed[k[len("FYNIQ_SECRET_"):]]
			return v, ok
		})
	})
}

// req: NFR-PORT-04
func TestBlobStoreConformance(t *testing.T) {
	porttest.BlobStore(t, func(t *testing.T) ports.BlobStore {
		d, err := NewDir(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return d
	})
}

// req: NFR-PORT-04
// req: CTL-AUD-02
func TestSignerConformance(t *testing.T) {
	s, err := NewEd25519(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	porttest.Signer(t, s)
	path := filepath.Join(t.TempDir(), "keys", "audit-anchor.ed25519")
	a, err := LoadOrCreateEd25519(path)
	if err != nil {
		t.Fatal(err)
	}
	porttest.Signer(t, a)
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the key file is %v, %v; want mode 0600", info.Mode(), err)
	}
	b, err := LoadOrCreateEd25519(path)
	if err != nil || b.KeyID() != a.KeyID() {
		t.Fatalf("reloading the key gave %q, %v; want the same key %q", b.KeyID(), err, a.KeyID())
	}
}

// req: NFR-PORT-04
// req: One outbox contract for every adapter
func TestQueueConformance(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		porttest.Queue(t, func(t *testing.T) (ports.Queue, func(string, int) []ports.Task) {
			m := NewMemory()
			return m, func(class string, n int) []ports.Task {
				var out []ports.Task
				for range n {
					task, err := m.Next(context.Background(), class)
					if err != nil {
						t.Fatalf("Next(%s): %v", class, err)
					}
					out = append(out, task)
				}
				return out
			}
		})
	})
	t.Run("push", func(t *testing.T) {
		tokens := NewHMACTokens([]byte("local-only-test-key"), clock.NewFrozen(time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)), time.Hour)
		var mu sync.Mutex
		got := map[string][]ports.Task{}
		targets := map[string]string{}
		for _, class := range []string{"worker", "executor", "jobs"} {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tokens.Verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), class) != nil {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				got[class] = append(got[class], ports.Task{Queue: class, Name: r.Header.Get("X-CloudTasks-TaskName"), Payload: body})
				mu.Unlock()
			}))
			t.Cleanup(srv.Close)
			targets[class] = srv.URL
		}
		p := NewPush(targets, tokens, 3, time.Millisecond)
		porttest.Queue(t, func(t *testing.T) (ports.Queue, func(string, int) []ports.Task) {
			return p, func(class string, n int) []ports.Task {
				p.Wait()
				mu.Lock()
				defer mu.Unlock()
				return got[class]
			}
		})
	})
}

// req: Proposal executor queue isolation
func TestTaskTokensConformance(t *testing.T) {
	c := clock.NewFrozen(time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC))
	tokens := NewHMACTokens([]byte("local-only-test-key"), c, time.Hour)
	porttest.TaskTokens(t, tokens)
	tok, _ := tokens.Token("worker")
	c.Advance(2 * time.Hour)
	if err := tokens.Verify(tok, "worker"); !errors.Is(err, ports.ErrUnauthorised) {
		t.Fatalf("an expired token was accepted: %v", err)
	}
}

// req: One outbox contract for every adapter
func TestPushRetriesUntilTheHandlerAccepts(t *testing.T) {
	tokens := NewHMACTokens([]byte("local-only-test-key"), clock.NewFrozen(time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)), time.Hour)
	var calls int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if r.Header.Get("X-CloudTasks-TaskRetryCount") != strconv.Itoa(n-1) {
			t.Errorf("attempt %d carried retry count %s", n, r.Header.Get("X-CloudTasks-TaskRetryCount"))
		}
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	p := NewPush(map[string]string{"worker": srv.URL}, tokens, 5, time.Millisecond)
	if err := p.Enqueue(context.Background(), ports.Task{Queue: "worker", Name: "retry-me", Payload: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	p.Wait()
	if calls != 3 {
		t.Fatalf("%d deliveries; want 3 (two failures, then success)", calls)
	}
}

// req: DEV-07
func TestLocalAdaptersRegisterAsLocal(t *testing.T) {
	var n int
	for _, a := range runtime.Adapters() {
		if !a.Local {
			t.Errorf("%s/%s registered from the local package without Local", a.Port, a.Name)
		}
		n++
	}
	if n < 3 {
		t.Fatalf("want the three local adapters registered, got %d", n)
	}
}

// req: DEV-07
func TestDevTokensCarryThePrincipalAndRefuseForgeries(t *testing.T) {
	c := clock.NewFrozen(time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC))
	issuer := NewDevTokens([]byte("local-only-test-key"), c)
	org := uuid.New()
	tok, err := issuer.Issue(authn.Principal{Subject: "user-1", Client: "web", Organisation: org, Roles: []authz.Role{authz.Accountant}}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p, err := issuer.Authenticate(ctx, authn.Credentials{Authorization: "Bearer " + tok})
	if err != nil || p.Subject != "user-1" || p.Organisation != org || len(p.Roles) != 1 || p.Roles[0] != authz.Accountant || p.Class != authn.ClassUser {
		t.Fatalf("Authenticate = %+v, %v", p, err)
	}
	other, _ := NewDevTokens([]byte("another-key"), c).Issue(authn.Principal{Subject: "user-1", Client: "web", Roles: []authz.Role{authz.Owner}}, time.Hour)
	body, sig, _ := strings.Cut(strings.TrimPrefix(tok, "fyniq-dev."), ".")
	for name, header := range map[string]string{
		"no credentials":             "",
		"another scheme":             "Basic " + tok,
		"another key's token":        "Bearer " + other,
		"roles changed in the token": "Bearer fyniq-dev." + body[:len(body)-2] + "xx." + sig,
		"a task token instead":       "Bearer worker.9999999999.abc",
	} {
		if _, err := issuer.Authenticate(ctx, authn.Credentials{Authorization: header}); !errors.Is(err, authn.ErrUnauthenticated) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
	c.Advance(2 * time.Hour)
	if _, err := issuer.Authenticate(ctx, authn.Credentials{Authorization: "Bearer " + tok}); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Errorf("an expired token was accepted: %v", err)
	}
}

// The issuer and the kind of a dev identity are the authenticator's, never the token's (FR-ADM-08).
// req: FR-ADM-08
func TestDevTokensStampTheirIssuer(t *testing.T) {
	c := clock.NewFrozen(time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC))
	key := []byte("local-only-test-key")
	claims := []byte(`{"sub":"u1","client":"web","class":"user","org":"` + uuid.New().String() +
		`","roles":["owner"],"exp":9999999999,"iss":"https://accounts.google.com","Issuer":"https://evil","Via":"token"}`)
	msg := "fyniq-dev." + base64.RawURLEncoding.EncodeToString(claims)
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	tok := msg + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
	p, err := NewDevTokens(key, c).Authenticate(context.Background(), authn.Credentials{Authorization: "Bearer " + tok})
	if err != nil || p.Issuer != DevIssuer || p.Via != authn.ViaSession || p.Subject != "u1" {
		t.Fatalf("a dev token claiming another issuer: %+v, %v", p, err)
	}
}
