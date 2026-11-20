package fyniq

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type seen struct {
	method, path, query, key, match, prefer, auth string
	body                                          string
}

func server(t *testing.T, answer func(n int, w http.ResponseWriter, r *http.Request)) (*Client, *[]seen) {
	var mu sync.Mutex
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, seen{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Idempotency-Key"), r.Header.Get("If-Match"), r.Header.Get("Prefer"), r.Header.Get("Authorization"), string(b)})
		n := len(got)
		mu.Unlock()
		answer(n, w, r)
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Token: func(context.Context) (string, error) { return "tok", nil }, Sleep: func(context.Context, time.Duration) error { return nil }}, &got
}

// FR-INT-17, FR-INT-08, FR-INT-16: a write that takes an Idempotency-Key gets one, the same on every retry of a 429; the token goes in the header; the path arguments fill the path in order; a retry
// is made after the Retry-After the API names, and a wait longer than MaxWait is returned; a write is not retried after a 503.
// req: FR-INT-17
// req: FR-INT-08
// req: FR-INT-16
func TestAWriteKeepsItsIdempotencyKeyAcrossRetries(t *testing.T) {
	c, got := server(t, func(n int, w http.ResponseWriter, r *http.Request) {
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	})
	op, ok := Operations["createWebhookEndpoint"]
	if ok {
		t.Fatalf("a person's operation is in the client: %+v", op)
	}
	r, err := c.CreateJournal(context.Background(), map[string]any{"memo": "x"})
	if err != nil || r.Status != 201 || len(*got) != 2 {
		t.Fatalf("%v %v (%d requests)", r, err, len(*got))
	}
	a, b := (*got)[0], (*got)[1]
	if a.key == "" || a.key != b.key || a.auth != "Bearer tok" || a.path != "/v1/journals" || a.method != "POST" || !strings.Contains(a.body, `"memo":"x"`) {
		t.Errorf("the requests: %+v %+v", a, b)
	}
	// A wait longer than MaxWait is the caller's to decide.
	long, _ := server(t, func(n int, w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		http.Error(w, "later", http.StatusTooManyRequests)
	})
	if _, err := long.CreateJournal(context.Background(), map[string]any{}); err == nil || !strings.Contains(err.Error(), "429") {
		t.Errorf("a long Retry-After: %v", err)
	}
	// A write is not retried after a 503: its outcome is unknown.
	down, calls := server(t, func(n int, w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "1"); http.Error(w, "down", 503) })
	if _, err := down.CreateJournal(context.Background(), map[string]any{}); err == nil || len(*calls) != 1 {
		t.Errorf("a write after 503: %v, %d calls", err, len(*calls))
	}
}

// FR-INT-07, FR-INT-09, FR-INT-14, FR-INT-10: a refusal is a *Problem with the stable code and the fix, every invalid field at once; the ETag of a read goes back in If-Match; a preview asks for
// Prefer: return=preview and sends no key; a list is walked by its next_cursor.
// req: FR-INT-07
// req: FR-INT-09
// req: FR-INT-10
// req: FR-INT-14
func TestRefusalsAreProblemsAndListsArePaged(t *testing.T) {
	c, got := server(t, func(n int, w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/journals") && r.Method == "GET" && r.URL.Query().Get("cursor") == "":
			_, _ = w.Write([]byte(`{"items":[{"id":"1"},{"id":"2"}],"next_cursor":"c2"}`))
		case strings.HasSuffix(r.URL.Path, "/journals") && r.Method == "GET":
			_, _ = w.Write([]byte(`{"items":[{"id":"3"}]}`))
		case r.Method == "POST":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(422)
			_, _ = w.Write([]byte(`{"status":422,"code":"API-INPUT-02","title":"A field is not valid","detail":"one field","fix":"Correct it","errors":[{"code":"X","field":"body.memo","message":"is empty","fix":"Say what it is"}]}`))
		default:
			w.Header().Set("ETag", `"3"`)
			w.Header().Set("Preference-Applied", "return=preview")
			_, _ = w.Write([]byte(`{}`))
		}
	})
	var ids []string
	err := Paginate(context.Background(), func(ctx context.Context, cursor string) (*Response, error) {
		var opts []CallOption
		if cursor != "" {
			opts = append(opts, WithQuery("cursor", cursor))
		}
		return c.ListJournals(ctx, opts...)
	}, func(item json.RawMessage) error {
		var x struct{ ID string }
		_ = json.Unmarshal(item, &x)
		ids = append(ids, x.ID)
		return nil
	})
	if err != nil || strings.Join(ids, ",") != "1,2,3" {
		t.Fatalf("%v %v", ids, err)
	}
	_, err = c.CreateJournal(context.Background(), map[string]any{})
	p, ok := err.(*Problem)
	if !ok || p.Code != "API-INPUT-02" || p.Fix != "Correct it" || len(p.Errors) != 1 || p.Errors[0].Field != "body.memo" {
		t.Fatalf("the refusal: %#v", err)
	}
	r, err := c.GetJournal(context.Background(), "abc/def", WithPreview(), WithIfMatch(`"2"`))
	last := (*got)[len(*got)-1]
	if err != nil || r.ETag() != `"3"` || !r.Previewed() || last.path != "/v1/journals/abc%2Fdef" && last.path != "/v1/journals/abc/def" || last.prefer != "return=preview" || last.match != `"2"` {
		t.Errorf("%+v %v %+v", r, err, last)
	}
}

// The operations are the ones a client's scopes can reach: no person's operation (an approval, the audit trail, a user), and each carries the scopes that reach it.
// req: FR-INT-17
// req: FR-INT-02
func TestOnlyTheOperationsAClientsScopesReachAreHere(t *testing.T) {
	for _, never := range []string{"approveApproval", "createWebhookEndpoint", "listAuditEvents", "createPersonalAccessToken", "listIntegrationClients", "requestTenantExport"} {
		if _, ok := Operations[never]; ok {
			t.Errorf("%s is in the client", never)
		}
	}
	if op := Operations["queryMetrics"]; op.Method != "POST" || len(op.Scopes) == 0 || op.Scopes[0] != "fyniq.read.reports" && op.Scopes[0] != "fyniq.write.reports" {
		t.Errorf("queryMetrics: %+v", op)
	}
	if len(Operations) < 50 {
		t.Errorf("%d operations", len(Operations))
	}
}
