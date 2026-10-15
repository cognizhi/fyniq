package gcp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
)

const queue = "projects/fyniq-staging/locations/asia-southeast1/queues/worker"

// An outbox row becomes one Cloud Tasks HTTP task to its class's /tasks, named after the row, with the
// OIDC token Cloud Tasks mints for the class's invoker; a second Enqueue of the row is taken as done.
// req: NFR-PORT-04
// req: SEC-03
// asvs: V13.2.1
func TestTasksEnqueueNamedOIDCTasks(t *testing.T) {
	var mu sync.Mutex
	created := map[string]map[string]any{}
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v2/"+queue+"/tasks" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Task map[string]any `json:"task"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		name := body.Task["name"].(string)
		if _, dup := created[name]; dup {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":409,"status":"ALREADY_EXISTS"}}`))
			return
		}
		created[name] = body.Task
		_ = json.NewEncoder(w).Encode(body.Task)
	}))
	defer srv.Close()
	q := NewTasks(srv.URL, map[string]TaskClass{"worker": {Queue: queue, URL: "https://worker.example.run.app/",
		ServiceAccount: "tasks-worker@fyniq-staging.iam.gserviceaccount.com", Audience: "https://worker.example.run.app"}},
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "api-sa-token"}), nil)
	ctx := context.Background()
	row := "0192f1a0-0000-7000-8000-0000000000a1"
	if err := q.Enqueue(ctx, ports.Task{Queue: "worker", Name: row, Payload: []byte(`{"kind":"oauth.subject.revoke"}`), Deadline: time.Minute}); err != nil {
		t.Fatal(err)
	}
	task := created[queue+"/tasks/"+row]
	req, _ := task["httpRequest"].(map[string]any)
	oidcTok, _ := req["oidcToken"].(map[string]any)
	payload, _ := base64.StdEncoding.DecodeString(req["body"].(string))
	if req["url"] != "https://worker.example.run.app/tasks" || task["dispatchDeadline"] != "60s" || string(payload) != `{"kind":"oauth.subject.revoke"}` ||
		oidcTok["serviceAccountEmail"] != "tasks-worker@fyniq-staging.iam.gserviceaccount.com" || oidcTok["audience"] != "https://worker.example.run.app" ||
		auth != "Bearer api-sa-token" {
		t.Fatalf("the task Cloud Tasks got: %v (auth %q)", task, auth)
	}
	if err := q.Enqueue(ctx, ports.Task{Queue: "worker", Name: row, Payload: []byte(`{}`)}); err != nil {
		t.Errorf("a second Enqueue of the same row: %v; want it taken as done", err)
	}
	for name, task := range map[string]ports.Task{
		"an unknown class":           {Queue: "payments", Name: row},
		"a name Cloud Tasks refuses": {Queue: "worker", Name: "row/1"},
		"a deadline over 30 min":     {Queue: "worker", Name: "r2", Deadline: time.Hour},
	} {
		if err := q.Enqueue(ctx, task); err == nil {
			t.Errorf("%s was enqueued", name)
		}
	}
}

// googleKey signs tokens like Google's OIDC tokens for a service account.
type googleKey struct{ key *rsa.PrivateKey }

func (g googleKey) mint(t *testing.T, claims map[string]any) string {
	t.Helper()
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: g.key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(claims)
	jws, _ := s.Sign(b)
	out, _ := jws.CompactSerialize()
	return out
}

// A worker class's handler accepts only a Google-signed OIDC token for its own audience, as its own
// invoker account: a token for the worker or minted by another account is refused at the executor, and
// only Cloud Scheduler's account can call the tick (Proposal executor queue isolation, SEC-03).
// req: SEC-03
// req: Proposal executor queue isolation
// asvs: V13.2.1, V10.3.1
func TestTaskTokensAreTheClassInvokersOnly(t *testing.T) {
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	g := googleKey{k}
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	tokens := &TaskTokens{Clock: clock.NewFrozen(now), Keys: &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&k.PublicKey}},
		Invokers: map[string]Invoker{
			"executor":  {ServiceAccount: "tasks-executor@fyniq.iam.gserviceaccount.com", Audience: "https://executor.example.run.app"},
			"worker":    {ServiceAccount: "tasks-worker@fyniq.iam.gserviceaccount.com", Audience: "https://worker.example.run.app"},
			"scheduler": {ServiceAccount: "scheduler@fyniq.iam.gserviceaccount.com", Audience: "https://api.example.run.app/internal/tick"},
		}}
	claims := func(email, aud string) map[string]any {
		return map[string]any{"iss": "https://accounts.google.com", "aud": aud, "sub": "1093", "email": email, "email_verified": true,
			"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(time.Hour).Unix()}
	}
	executor := g.mint(t, claims("tasks-executor@fyniq.iam.gserviceaccount.com", "https://executor.example.run.app"))
	if err := tokens.Verify(executor, "executor"); err != nil {
		t.Fatalf("the executor's own task: %v", err)
	}
	if err := tokens.Verify(g.mint(t, claims("scheduler@fyniq.iam.gserviceaccount.com", "https://api.example.run.app/internal/tick")), "scheduler"); err != nil {
		t.Fatalf("Cloud Scheduler's tick: %v", err)
	}
	unverified := claims("tasks-executor@fyniq.iam.gserviceaccount.com", "https://executor.example.run.app")
	unverified["email_verified"] = false
	forged, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: other}, nil)
	b, _ := json.Marshal(claims("tasks-executor@fyniq.iam.gserviceaccount.com", "https://executor.example.run.app"))
	jws, _ := forged.Sign(b)
	forgedToken, _ := jws.CompactSerialize()
	for name, check := range map[string]error{
		"the worker's task at the executor":        tokens.Verify(g.mint(t, claims("tasks-worker@fyniq.iam.gserviceaccount.com", "https://worker.example.run.app")), "executor"),
		"the executor's audience, another account": tokens.Verify(g.mint(t, claims("tasks-worker@fyniq.iam.gserviceaccount.com", "https://executor.example.run.app")), "executor"),
		"the executor's task at the tick":          tokens.Verify(executor, "scheduler"),
		"an unverified email":                      tokens.Verify(g.mint(t, unverified), "executor"),
		"a token another key signed":               tokens.Verify(forgedToken, "executor"),
		"no token":                                 tokens.Verify("", "executor"),
		"a class nobody may call":                  tokens.Verify(executor, "payments"),
	} {
		if !errors.Is(check, ports.ErrUnauthorised) {
			t.Errorf("%s: %v", name, check)
		}
	}
	if _, err := tokens.Token("worker"); err == nil {
		t.Error("the service minted a task token; Cloud Tasks does")
	}
}

// A platform job's slot starts as an execution of the Cloud Run job with `run-slot <job> <slot>`.
// req: NFR-PORT-04
func TestRunJobsStartsTheSlot(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/projects/fyniq-staging/locations/asia-southeast1/jobs/fyniq-jobs:run" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"name":"projects/fyniq-staging/locations/asia-southeast1/operations/op-1","metadata":{"name":"projects/fyniq-staging/locations/asia-southeast1/jobs/fyniq-jobs/executions/fyniq-jobs-x7k2p"}}`))
	}))
	defer srv.Close()
	j := NewRunJobs(srv.URL, "projects/fyniq-staging/locations/asia-southeast1/jobs/fyniq-jobs", nil, nil)
	exec, err := j.Start(context.Background(), "audit.anchor", time.Date(2026, 10, 15, 3, 0, 0, 0, time.FixedZone("SGT", 8*3600)))
	if err != nil || !strings.HasSuffix(exec, "/executions/fyniq-jobs-x7k2p") {
		t.Fatalf("Start = %q, %v", exec, err)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"args":["run-slot","audit.anchor","2026-10-14T19:00:00Z"]`) || !strings.Contains(string(b), `"taskCount":1`) {
		t.Fatalf("the run request: %s", b)
	}
}
