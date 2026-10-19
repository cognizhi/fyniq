//go:build local

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/pgtest"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

// The API's behaviour suites run against PostgreSQL in the local build (`make test`), like the
// adapter's. Callers authenticate with a fake authenticator: a token names a principal.

var db *pgtest.DB

func TestMain(m *testing.M) {
	var err error
	db, err = pgtest.Start(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "the API tests need Docker: %v\n", err) // never skip: QA-SEC-03 blocks merge
		os.Exit(1)
	}
	code := m.Run()
	db.Stop()
	os.Exit(code)
}

// tokens is the fake authenticator: a token is a key of the map.
type tokens struct {
	mu sync.Mutex
	m  map[string]authn.Principal
}

func (f *tokens) Authenticate(_ context.Context, c authn.Credentials) (authn.Principal, error) {
	authorization := c.Authorization
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.m[strings.TrimPrefix(authorization, "Bearer ")]
	if !ok || !strings.HasPrefix(authorization, "Bearer ") {
		return authn.Principal{}, authn.ErrUnauthenticated
	}
	return p, nil
}

// env is an API over a fresh organisation (porttest.Books), with a clock and a security log.
type env struct {
	t      *testing.T
	api    *API
	srv    *httptest.Server
	store  *postgres.Store
	books  porttest.Books
	clock  *clock.Frozen
	tokens *tokens
	log    *bytes.Buffer
	blobs  *local.Dir    // the evidence staging store
	queue  *local.Memory // the outbox's tasks
}

var orgs int

func newEnv(t *testing.T) *env {
	t.Helper()
	store, err := postgres.Open(context.Background(), db.URL("fyniq_api"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	orgs++
	e := &env{t: t, store: store, books: porttest.NewBooks(t, store, fmt.Sprintf("API %d Pte Ltd", orgs)),
		clock: clock.NewFrozen(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)), tokens: &tokens{m: map[string]authn.Principal{}}, log: &bytes.Buffer{}}
	if e.blobs, err = local.NewDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	e.queue = local.NewMemory()
	mux := http.NewServeMux()
	e.api = New(mux, Deps{Store: store, Installation: store, Outbox: &outbox.Dispatcher{Store: store, Queue: e.queue, Clock: e.clock},
		Clock: e.clock, Auth: e.tokens, Log: slog.New(slog.NewJSONHandler(e.log, nil)), Version: "0.0.0-test",
		Staging: e.blobs, Scanner: local.EICARScanner{}})
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e
}

// as returns a token for a principal in this environment's organisation holding roles.
func (e *env) as(roles ...authz.Role) string {
	return e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: e.books.Org, Roles: roles})
}

func (e *env) principal(p authn.Principal) string {
	e.tokens.mu.Lock()
	defer e.tokens.mu.Unlock()
	tok := uuid.NewString()
	if p.Subject == "" {
		p.Subject = "user-" + tok[:8]
	}
	if p.Via == "" {
		p.Via, p.Issuer = authn.ViaSession, "urn:fyniq:test"
	}
	e.tokens.m[tok] = p
	return tok
}

// response is an answer: status, headers and the JSON body.
type response struct {
	Status int
	Header http.Header
	Body   map[string]any
	Raw    string
}

// problemCode returns the body's problem code.
func (r response) problemCode() string { s, _ := r.Body["code"].(string); return s }

func (r response) str(path ...string) string {
	var v any = r.Body
	for _, p := range path {
		m, _ := v.(map[string]any)
		v = m[p]
	}
	s, _ := v.(string)
	return s
}

// call sends a request; headers are name, value pairs.
func (e *env) call(method, path, token string, body any, headers ...string) response {
	e.t.Helper()
	var rd io.Reader
	contentType := "application/json"
	if raw, ok := body.(rawBody); ok {
		rd, contentType = bytes.NewReader(raw.data), raw.contentType
	} else if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := response{Status: resp.StatusCode, Header: resp.Header, Raw: string(raw)}
	if len(raw) > 0 && raw[0] == '{' {
		if err := json.Unmarshal(raw, &out.Body); err != nil {
			e.t.Fatalf("%s %s: %v: %s", method, path, err, raw)
		}
	}
	return out
}

// upload sends a multipart form: fields, and file as the form field file named name; headers are name, value pairs.
func (e *env) upload(path, token string, fields map[string]string, name string, file []byte, headers ...string) response {
	e.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	if file != nil {
		part, err := w.CreateFormFile("file", name)
		if err != nil {
			e.t.Fatal(err)
		}
		_, _ = part.Write(file)
	}
	_ = w.Close()
	return e.call(http.MethodPost, path, token, rawBody{w.FormDataContentType(), body.Bytes()}, headers...)
}

// rawBody is a request body call sends as it is, with its own Content-Type.
type rawBody struct {
	contentType string
	data        []byte
}

// draft is a balanced journal in SGD: rent paid from the bank.
func (e *env) draft(amount string) map[string]any {
	return map[string]any{
		"date": "2026-10-20", "memo": "October rent",
		"lines": []map[string]any{
			{"account_id": e.books.Rent.ID.String(), "amount": map[string]any{"amount": amount, "currency": "SGD"}, "description": "Rent"},
			{"account_id": e.books.Bank.ID.String(), "amount": map[string]any{"amount": "-" + amount, "currency": "SGD"}},
		},
	}
}

// createDraft saves a draft as an Accountant and returns its ID and ETag.
func (e *env) createDraft(amount string) (id, etag string) {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/journals", e.as(authz.Accountant), e.draft(amount))
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createJournal = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// postedJournal saves and posts a draft as an Accountant and returns its ID and ETag.
func (e *env) postedJournal(amount string) (id, etag string) {
	e.t.Helper()
	id, tag := e.createDraft(amount)
	r := e.call(http.MethodPost, "/v1/journals/"+id+"/post", e.as(authz.Accountant), nil, "If-Match", tag)
	if r.Status != http.StatusOK {
		e.t.Fatalf("postJournal = %d: %s", r.Status, r.Raw)
	}
	return id, r.Header.Get("ETag")
}
