//go:build local

package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/member"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/wiring"
)

// documented reports whether the operation's x-fyniq-error-codes lists code.
func (e *env) documented(op, code string) bool {
	for _, item := range e.api.OpenAPI().Paths {
		for _, o := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if o != nil && o.OperationID == op {
				codes, _ := o.Extensions["x-fyniq-error-codes"].([]string)
				return slices.Contains(codes, code)
			}
		}
	}
	return false
}

// problemIs checks a problem details answer: status, code, media type, type URI, title, detail,
// fix, instance, and that the operation documents the code.
func (e *env) problemIs(r response, op string, status int, code string) {
	e.t.Helper()
	if r.Status != status || r.problemCode() != code {
		e.t.Fatalf("%s: want %d %s, got %d: %s", op, status, code, r.Status, r.Raw)
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		e.t.Errorf("%s: %s came as %q, not application/problem+json", op, code, ct)
	}
	if r.str("type") != "urn:fyniq:problem:"+code || r.str("title") == "" || r.str("detail") == "" || r.str("fix") == "" ||
		!strings.HasPrefix(r.str("instance"), "urn:fyniq:request:") {
		e.t.Errorf("%s: %s is missing a type, title, detail, fix or instance: %s", op, code, r.Raw)
	}
	if !e.documented(op, code) {
		e.t.Errorf("%s returned %s, which its x-fyniq-error-codes does not list", op, code)
	}
}

// fields returns the (field, code) pairs of a problem's errors.
func fields(r response) map[string]string {
	out := map[string]string{}
	list, _ := r.Body["errors"].([]any)
	for _, x := range list {
		m := x.(map[string]any)
		out[m["field"].(string)] = m["code"].(string)
		if m["message"] == "" || m["fix"] == "" {
			panic("a field problem without a message or fix")
		}
	}
	return out
}

// req: FR-INT-07
func TestErrorsAreProblemDetailsWithStableCodes(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)

	r := e.call(http.MethodGet, "/v1/organisation", "", nil)
	e.problemIs(r, "getOrganisation", 401, CodeUnauthenticated)
	if !strings.HasPrefix(r.Header.Get("WWW-Authenticate"), "Bearer") {
		t.Errorf("401 without WWW-Authenticate: %v", r.Header)
	}

	// Every invalid field at once: two lines wrong in two ways, plus a malformed date.
	bad := e.draft("100.00")
	lines := bad["lines"].([]map[string]any)
	lines[0]["amount"] = map[string]any{"amount": "100.005", "currency": "SGD"}
	lines[1]["amount"] = map[string]any{"amount": "-100.00", "currency": "XYZ"}
	r = e.call(http.MethodPost, "/v1/journals", accountant, bad)
	e.problemIs(r, "createJournal", 422, CodeInvalid)
	if f := fields(r); f["body.lines[0].amount.amount"] != "MONEY-02" || f["body.lines[1].amount.currency"] != "MONEY-04" {
		t.Errorf("want both fields listed with their codes, got %v", f)
	}
	// huma's own validation (pattern, format) comes as the same problem, with field paths.
	bad = e.draft("1e5")
	bad["date"] = "20 October"
	r = e.call(http.MethodPost, "/v1/journals", accountant, bad)
	e.problemIs(r, "createJournal", 422, CodeInvalid)
	if f := fields(r); f["body.lines[0].amount.amount"] == "" || f["body.date"] == "" || len(f) < 3 {
		t.Errorf("want the amounts and the date listed, got %v", f)
	}
	r = e.call(http.MethodPost, "/v1/journals", accountant, nil, "Content-Type", "application/json")
	if r.Status != 400 && r.Status != 422 {
		t.Errorf("an empty body: %d %s", r.Status, r.Raw)
	}

	e.problemIs(e.call(http.MethodGet, "/v1/journals/"+uuid.Must(uuid.NewV7()).String(), accountant, nil), "getJournal", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodGet, "/v1/accounts/"+uuid.Must(uuid.NewV7()).String(), accountant, nil), "getAccount", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodGet, "/v1/accounts?cursor=bm90LWEtY3Vyc29y", accountant, nil), "listAccounts", 422, CodeCursor)

	// The engine's validation: an unbalanced draft cannot post; every problem is listed.
	id, tag := e.createDraft("100.00")
	unbalanced := e.draft("100.00")
	unbalanced["lines"].([]map[string]any)[1]["amount"] = map[string]any{"amount": "-99.99", "currency": "SGD"}
	r = e.call(http.MethodPost, "/v1/journals", accountant, unbalanced)
	r = e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", accountant, nil, "If-Match", r.Header.Get("ETag"))
	e.problemIs(r, "postJournal", 422, "LEDGER-09")
	if f := fields(r); f["lines"] != "LEDGER-09" && len(f) == 0 {
		t.Errorf("the imbalance is not listed: %s", r.Raw)
	}

	// State conflicts.
	r = e.call(http.MethodPost, "/v1/journals/"+id+"/reverse", accountant, map[string]any{"date": "2026-10-21", "reason": "wrong"}, "If-Match", tag)
	e.problemIs(r, "reverseJournal", 409, "JOURNAL-03")
	r = e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, "If-Match", tag)
	if r.Status != 200 {
		t.Fatalf("post: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, "If-Match", r.Header.Get("ETag")), "postJournal", 409, "JOURNAL-02")

	// The database's invariants come through with their codes: a closed period refuses the posting.
	november := e.draft("10.00")
	november["date"] = "2026-11-03"
	r = e.call(http.MethodPost, "/v1/journals", accountant, november)
	nid, ntag := r.str("id"), r.Header.Get("ETag")
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Close(ctx, e.books.Nov.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+nid+"/post", accountant, nil, "If-Match", ntag), "postJournal", 409, storage.CodePeriodClosed)
}

// req: FR-INT-08
// req: data/data-model Idempotency keys and posting identity
func TestIdempotencyKeyReplaysAndRefusesReuse(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	revision := func() int64 {
		var rev int64
		_ = e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			rev, err = u.Ledger().Revision(ctx)
			return err
		})
		return rev
	}

	first := e.call(http.MethodPost, "/v1/journals", accountant, e.draft("100.00"), "Idempotency-Key", "create-1")
	again := e.call(http.MethodPost, "/v1/journals", accountant, e.draft("100.00"), "Idempotency-Key", "create-1")
	if first.Status != 201 || again.Status != 201 || again.str("id") != first.str("id") || again.Header.Get("Idempotent-Replayed") != "true" ||
		first.Header.Get("Idempotent-Replayed") != "" || again.Header.Get("ETag") != first.Header.Get("ETag") || again.Header.Get("Location") == "" {
		t.Fatalf("a repeat should replay the first answer:\n%d %v %s\n%d %v %s", first.Status, first.Header, first.Raw, again.Status, again.Header, again.Raw)
	}
	if other := e.call(http.MethodPost, "/v1/journals", accountant, e.draft("100.00")); other.str("id") == first.str("id") {
		t.Fatal("a request without the key made nothing new")
	}
	e.problemIs(e.call(http.MethodPost, "/v1/journals", accountant, e.draft("200.00"), "Idempotency-Key", "create-1"),
		"createJournal", 422, CodeIdempotencyReused)
	// Keys are per caller: another user's same key is a new request.
	if r := e.call(http.MethodPost, "/v1/journals", e.as(authz.Accountant), e.draft("200.00"), "Idempotency-Key", "create-1"); r.Status != 201 || r.str("id") == first.str("id") {
		t.Fatalf("another caller's key: %d %s", r.Status, r.Raw)
	}

	// A repeated post replays even though its If-Match is now stale, and posts nothing again.
	id, tag := first.str("id"), first.Header.Get("ETag")
	posted := e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, "If-Match", tag, "Idempotency-Key", "post-1")
	rev := revision()
	replayed := e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, "If-Match", tag, "Idempotency-Key", "post-1")
	if posted.Status != 200 || replayed.Status != 200 || replayed.str("entry", "id") != posted.str("entry", "id") || revision() != rev {
		t.Fatalf("a repeated post: %d %s / %d %s (revision %d → %d)", posted.Status, posted.Raw, replayed.Status, replayed.Raw, rev, revision())
	}

	// A failed command saves nothing: the key is free again.
	bad := e.draft("100.00")
	bad["lines"].([]map[string]any)[0]["amount"] = map[string]any{"amount": "1.001", "currency": "SGD"}
	failer := authn.Principal{Subject: "failer", Client: "web", Class: authn.ClassUser, Organisation: e.books.Org, Roles: []authz.Role{authz.Accountant}}
	ftok := e.principal(failer)
	e.problemIs(e.call(http.MethodPost, "/v1/journals", ftok, bad, "Idempotency-Key", "fails"), "createJournal", 422, CodeInvalid)
	if r := e.call(http.MethodPost, "/v1/journals", ftok, e.draft("1.00"), "Idempotency-Key", "fails"); r.Status != 201 {
		t.Fatalf("the key of a failed request should be free: %d %s", r.Status, r.Raw)
	}

	// Two identical requests at once: one runs, the other waits and replays it.
	var wg sync.WaitGroup
	results := make([]response, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = e.call(http.MethodPost, "/v1/journals", accountant, e.draft("5.00"), "Idempotency-Key", "race")
		}()
	}
	wg.Wait()
	if results[0].Status != 201 || results[1].Status != 201 || results[0].str("id") != results[1].str("id") {
		t.Fatalf("concurrent repeats: %d %s / %d %s", results[0].Status, results[0].Raw, results[1].Status, results[1].Raw)
	}

	// After 24 hours the key has expired: refused until the hourly purge deletes it, then free.
	e.clock.Advance(IdempotencyTTL + time.Minute)
	e.problemIs(e.call(http.MethodPost, "/v1/journals", accountant, e.draft("100.00"), "Idempotency-Key", "create-1"), "createJournal", 422, CodeIdempotencyOld)
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if n, err := wiring.PurgeIdempotencyKeys(context.Background(), worker, e.clock.Now()); err != nil || n < 5 {
		t.Fatalf("PurgeIdempotencyKeys = %d, %v", n, err)
	}
	if r := e.call(http.MethodPost, "/v1/journals", accountant, e.draft("100.00"), "Idempotency-Key", "create-1"); r.Status != 201 || r.str("id") == first.str("id") {
		t.Fatalf("a purged key should be free: %d %s", r.Status, r.Raw)
	}
}

// req: FR-INT-09
// req: data/data-model Versioning of records
func TestETagAndIfMatch(t *testing.T) {
	e := newEnv(t)
	standardMode(e) // a void in Founder mode needs a second person (FM-1)
	accountant := e.as(authz.Accountant)
	id, tag := e.createDraft("100.00")
	if tag != `"1-draft"` {
		t.Fatalf("ETag of a new draft = %s", tag)
	}
	if r := e.call(http.MethodGet, "/v1/journals/"+id, accountant, nil); r.Header.Get("ETag") != tag || r.str("status") != "draft" {
		t.Fatalf("getJournal: %v %s", r.Header, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil), "postJournal", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, "If-Match", `"1-posted"`), "postJournal", 412, CodeStale)
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, "If-Match", `W/"1-draft"`), "postJournal", 412, CodeStale)
	r := e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, "If-Match", `"0-draft", `+tag)
	if r.Status != 200 || r.Header.Get("ETag") != `"1-posted"` || r.str("journal", "status") != "posted" {
		t.Fatalf("post with a matching If-Match: %d %v %s", r.Status, r.Header, r.Raw)
	}
	reversal := map[string]any{"date": "2026-10-25", "reason": "Posted to the wrong account"}
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+id+"/reverse", accountant, reversal, "If-Match", tag), "reverseJournal", 412, CodeStale)
	r = e.call(http.MethodPost, "/v1/journals/"+id+"/reverse", accountant, reversal, "If-Match", "*")
	if r.Status != 200 || r.Header.Get("ETag") != `"1-reversed"` || r.str("entry", "reverses_entry_id") == "" {
		t.Fatalf("reverse with If-Match *: %d %v %s", r.Status, r.Header, r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/accounts/"+e.books.Bank.ID.String(), accountant, nil); r.Header.Get("ETag") != `"1"` {
		t.Errorf("getAccount ETag = %q", r.Header.Get("ETag"))
	}
	if r := e.call(http.MethodGet, "/v1/organisation", accountant, nil); r.Header.Get("ETag") == "" || r.str("base_currency") != "SGD" {
		t.Errorf("getOrganisation: %v %s", r.Header, r.Raw)
	}
}

// A preview runs the command and every check its commit would, and saves nothing: the answer is the
// command's own body, marked with Preference-Applied, or the refusal the commit would give.
// req: FR-INT-14
func TestPreviewBeforeCommit(t *testing.T) {
	e := newEnv(t)
	standardMode(e) // a void in Founder mode needs a second person (FM-1)
	accountant := e.as(authz.Accountant)
	preview := []string{"Prefer", "return=preview", "Idempotency-Key", uuid.NewString()}
	unchanged := func(r response) {
		t.Helper()
		if r.Header.Get("Preference-Applied") != "return=preview" || r.Header.Get("ETag") != "" || r.Header.Get("Location") != "" ||
			r.Header.Get("Idempotent-Replayed") != "" {
			t.Errorf("a preview's headers: %v", r.Header)
		}
	}

	// A draft previewed is not saved: its ID leads nowhere.
	r := e.call(http.MethodPost, "/v1/journals", accountant, e.draft("100.00"), preview...)
	if r.Status != http.StatusOK || r.str("status") != "draft" {
		t.Fatalf("preview a draft: %d %s", r.Status, r.Raw)
	}
	unchanged(r)
	e.problemIs(e.call(http.MethodGet, "/v1/journals/"+r.str("id"), accountant, nil), "getJournal", 404, CodeNotFound)

	// Posting previewed shows the entry it would write; the journal stays a draft, at the same ETag, and
	// the real posting writes the same lines.
	id, tag := e.createDraft("100.00")
	r = e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, append([]string{"If-Match", tag}, preview...)...)
	if r.Status != http.StatusOK {
		t.Fatalf("preview the posting: %d %s", r.Status, r.Raw)
	}
	unchanged(r)
	previewed := r.Body["entry"].(map[string]any)["lines"]
	if g := e.call(http.MethodGet, "/v1/journals/"+id, accountant, nil); g.str("status") != "draft" || g.Header.Get("ETag") != tag {
		t.Fatalf("after the preview the journal is %s at %s; want a draft at %s", g.str("status"), g.Header.Get("ETag"), tag)
	}
	// The same Idempotency-Key, after a preview, still runs the command: a preview claims no key.
	posted := e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, "If-Match", tag, "Idempotency-Key", preview[3])
	if posted.Status != http.StatusOK || posted.Header.Get("Idempotent-Replayed") != "" || posted.Header.Get("Preference-Applied") != "" {
		t.Fatalf("post after the preview: %d %v %s", posted.Status, posted.Header, posted.Raw)
	}
	if got := posted.Body["entry"].(map[string]any)["lines"]; !reflect.DeepEqual(got, previewed) {
		t.Errorf("the posting wrote %v; the preview showed %v", got, previewed)
	}

	// A reversal previewed leaves the journal posted.
	reversed := e.call(http.MethodPost, "/v1/journals/"+id+"/reverse", accountant, map[string]any{"date": "2026-10-21", "reason": "wrong month"},
		append([]string{"If-Match", posted.Header.Get("ETag")}, preview[:2]...)...)
	if reversed.Status != http.StatusOK || reversed.Body["entry"].(map[string]any)["reverses_entry_id"] == nil {
		t.Fatalf("preview the reversal: %d %s", reversed.Status, reversed.Raw)
	}
	unchanged(reversed)
	if g := e.call(http.MethodGet, "/v1/journals/"+id, accountant, nil); g.str("status") != "posted" {
		t.Errorf("after previewing the reversal the journal is %s", g.str("status"))
	}

	// The refusal is the commit's: an unbalanced draft cannot post, previewed or not; another preference
	// alone commits as usual.
	unbalanced := e.draft("100.00")
	unbalanced["lines"].([]map[string]any)[1]["amount"] = map[string]any{"amount": "-99.99", "currency": "SGD"}
	r = e.call(http.MethodPost, "/v1/journals", accountant, unbalanced, "Prefer", "return=minimal")
	if r.Status != http.StatusCreated || r.Header.Get("Preference-Applied") != "" {
		t.Fatalf("a draft with return=minimal: %d %v", r.Status, r.Header)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", accountant, nil, "If-Match", r.Header.Get("ETag"),
		"Prefer", "respond-async, return=preview"), "postJournal", 422, "LEDGER-09")
}

// Each client's requests in an organisation are counted a minute at a time, reads and writes apart;
// the headers say what is left, and a full window is refused with 429 and Retry-After until it ends.
// req: FR-INT-16
func TestRateLimits(t *testing.T) {
	e := newEnv(t)
	mux := http.NewServeMux()
	New(mux, Deps{Store: e.store, Installation: e.store, Clock: e.clock, Auth: e.tokens, Log: slog.New(slog.DiscardHandler),
		Version: "0.0.0-test", Limits: Limits{Reads: 2, Writes: 1}})
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	e.clock.Set(time.Date(2026, 10, 15, 9, 0, 20, 0, time.UTC)) // 40 seconds before the minute ends
	alice, bob := e.as(authz.Accountant), e.as(authz.Accountant)

	for i, want := range []string{`"reads";r=1;t=40`, `"reads";r=0;t=40`} {
		r := e.call(http.MethodGet, "/v1/organisation", alice, nil)
		if r.Status != 200 || r.Header.Get("RateLimit-Policy") != `"reads";q=2;w=60` || r.Header.Get("RateLimit") != want {
			t.Fatalf("read %d: %d, RateLimit-Policy %q, RateLimit %q; want %s", i+1, r.Status, r.Header.Get("RateLimit-Policy"), r.Header.Get("RateLimit"), want)
		}
	}
	r := e.call(http.MethodGet, "/v1/organisation", alice, nil)
	e.problemIs(r, "getOrganisation", 429, CodeRateLimited)
	if r.Header.Get("Retry-After") != "40" || r.Header.Get("RateLimit") != `"reads";r=0;t=40` {
		t.Errorf("the refusal's headers: %v", r.Header)
	}

	// Another client has its own window; writes are counted apart from reads; previews are writes.
	if r := e.call(http.MethodGet, "/v1/organisation", bob, nil); r.Status != 200 {
		t.Errorf("another client was refused: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/journals", alice, e.draft("10.00"), "Prefer", "return=preview"); r.Status != 200 ||
		r.Header.Get("RateLimit") != `"writes";r=0;t=40` {
		t.Fatalf("a write after the reads ran out: %d %q %s", r.Status, r.Header.Get("RateLimit"), r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/journals", alice, e.draft("10.00")), "createJournal", 429, CodeRateLimited)

	// A public operation is not counted here (the load balancer limits by IP, SEC-23).
	if r := e.call(http.MethodGet, "/v1/sign-in-providers", "", nil); r.Status != 200 || r.Header.Get("RateLimit") != "" {
		t.Errorf("a public operation: %d %v", r.Status, r.Header)
	}

	// The next minute is a new window.
	e.clock.Advance(40 * time.Second)
	if r := e.call(http.MethodGet, "/v1/organisation", alice, nil); r.Status != 200 || r.Header.Get("RateLimit") != `"reads";r=1;t=60` {
		t.Errorf("in the next minute: %d %q", r.Status, r.Header.Get("RateLimit"))
	}

	// The worker's hourly job deletes the windows that ended, with a few minutes' margin.
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if n, err := wiring.PurgeRateLimitWindows(context.Background(), worker, e.clock.Now().Add(10*time.Minute)); err != nil || n < 4 {
		t.Errorf("PurgeRateLimitWindows = %d, %v; want this test's 4 windows at least", n, err)
	}
}

// A bulk request saves each item on its own and answers with a result per item, in order: an item the
// ledger refuses is reported with its problem and takes no other item with it. MaxBulkItems items answer
// well within FR-INT-11's 10 seconds, so the request stays synchronous.
// req: FR-INT-11
func TestBulkCreateJournals(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	bad := e.draft("10.00")
	bad["lines"].([]map[string]any)[0]["amount"] = map[string]any{"amount": "10.005", "currency": "SGD"}
	body := map[string]any{"items": []any{e.draft("10.00"), bad, e.draft("20.00")}}
	results := func(r response) []map[string]any {
		var out []map[string]any
		for _, x := range r.Body["results"].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}

	r := e.call(http.MethodPost, "/v1/journals:bulk", accountant, body, "Idempotency-Key", "bulk-1")
	if r.Status != http.StatusOK || r.Body["saved"] != float64(2) || r.Body["refused"] != float64(1) {
		t.Fatalf("a bulk with one bad item: %d %s", r.Status, r.Raw)
	}
	got := results(r)
	for i, want := range []float64{201, 422, 201} {
		if got[i]["index"] != float64(i) || got[i]["status"] != want {
			t.Errorf("result %d: %v; want status %v", i, got[i], want)
		}
	}
	if p, _ := got[1]["problem"].(map[string]any); p["code"] != CodeInvalid || got[1]["id"] != nil ||
		p["errors"].([]any)[0].(map[string]any)["field"] != "body.items[1].lines[0].amount.amount" ||
		p["errors"].([]any)[0].(map[string]any)["code"] != "MONEY-02" {
		t.Errorf("the refused item: %v", got[1])
	}
	saved := e.call(http.MethodGet, "/v1/journals/"+got[0]["id"].(string), accountant, nil)
	if saved.str("status") != "draft" || saved.Header.Get("ETag") != got[0]["etag"] {
		t.Errorf("the saved item reads as %s at %s; the result said %v", saved.str("status"), saved.Header.Get("ETag"), got[0]["etag"])
	}

	// The key covers the whole request: a repeat runs nothing and answers the same.
	again := e.call(http.MethodPost, "/v1/journals:bulk", accountant, body, "Idempotency-Key", "bulk-1")
	if again.Header.Get("Idempotent-Replayed") != "true" || results(again)[2]["id"] != got[2]["id"] {
		t.Errorf("the repeat: %v %s", again.Header, again.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/journals:bulk", accountant, map[string]any{"items": []any{e.draft("1.00")}},
		"Idempotency-Key", "bulk-1"), "bulkCreateJournals", 422, CodeIdempotencyReused)

	// A preview checks every item and saves none.
	pv := e.call(http.MethodPost, "/v1/journals:bulk", accountant, body, "Prefer", "return=preview")
	if pv.Status != http.StatusOK || pv.Header.Get("Preference-Applied") != "return=preview" || pv.Body["saved"] != float64(2) {
		t.Fatalf("a bulk preview: %d %v %s", pv.Status, pv.Header, pv.Raw)
	}
	if p := results(pv); p[0]["status"] != float64(200) || p[0]["id"] != nil || p[1]["status"] != float64(422) {
		t.Errorf("the previewed results: %v", p)
	}

	// No items, or more than MaxBulkItems, are refused whole.
	e.problemIs(e.call(http.MethodPost, "/v1/journals:bulk", accountant, map[string]any{"items": []any{}}), "bulkCreateJournals", 422, CodeInvalid)
	many := make([]any, MaxBulkItems+1)
	for i := range many {
		many[i] = e.draft("1.00")
	}
	e.problemIs(e.call(http.MethodPost, "/v1/journals:bulk", accountant, map[string]any{"items": many}), "bulkCreateJournals", 422, CodeInvalid)

	// The most items within 10 seconds (FR-INT-11).
	start := clock.System{}.Now()
	r = e.call(http.MethodPost, "/v1/journals:bulk", accountant, map[string]any{"items": many[:MaxBulkItems]})
	took := clock.System{}.Now().Sub(start)
	t.Logf("%d drafts in %v", MaxBulkItems, took)
	if r.Status != http.StatusOK || r.Body["saved"] != float64(MaxBulkItems) {
		t.Fatalf("%d items: %d saved %v", MaxBulkItems, r.Status, r.Body["saved"])
	}
	if took > 10*time.Second {
		t.Errorf("%d items took %v: past FR-INT-11's 10 seconds, so the request must answer 202 with an operation", MaxBulkItems, took)
	}
}

// The organisation's profile and financial year: read by those the matrix lets view them, changed by
// the Admin or the Controller at the version they read, every broken rule reported at once.
// req: FR-ORG-01
// req: FR-ORG-02
func TestOrganisationProfile(t *testing.T) {
	e := newEnv(t)
	controller := e.as(authz.Controller)
	r := e.call(http.MethodGet, "/v1/organisation/profile", controller, nil)
	if r.Status != 200 || r.Body["time_zone"] != "Asia/Singapore" || r.Body["fy_start_month"] != float64(1) || r.Body["legal_name"] != nil {
		t.Fatalf("a new organisation's profile: %d %s", r.Status, r.Raw)
	}
	tag := r.Header.Get("ETag")
	e.problemIs(e.call(http.MethodPatch, "/v1/organisation/profile", controller, map[string]any{"legal_name": "X"}), "updateOrganisationProfile", 428, CodeIfMatchRequired)

	change := map[string]any{"legal_name": "Northwind Analytics Pte. Ltd.", "uen": "202312345K", "registered_address": "1 Raffles Place, #20-01, Singapore 048616",
		"email": "accounts@northwind.example", "bank_name": "DBS Bank", "bank_account_number": "012-345678-9", "paynow_uen": "202312345K",
		"document_footer": "Thank you for your business.", "fy_start_month": 4, "books_start": "2026-03-15", "first_year_end": "2027-03-31"}

	// A preview saves nothing.
	pv := e.call(http.MethodPatch, "/v1/organisation/profile", controller, change, "If-Match", tag, "Prefer", "return=preview")
	if pv.Status != 200 || pv.Header.Get("Preference-Applied") != "return=preview" || pv.Body["uen"] != "202312345K" {
		t.Fatalf("the preview: %d %v %s", pv.Status, pv.Header, pv.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/organisation/profile", controller, nil); g.Header.Get("ETag") != tag || g.Body["uen"] != nil {
		t.Fatalf("after the preview: %s", g.Raw)
	}

	r = e.call(http.MethodPatch, "/v1/organisation/profile", controller, change, "If-Match", tag)
	if r.Status != 200 || r.Header.Get("ETag") == tag || r.Body["first_year_end"] != "2027-03-31" || r.Body["bank_account_number"] != "012-345678-9" {
		t.Fatalf("the change: %d %s", r.Status, r.Raw)
	}
	next := r.Header.Get("ETag")
	e.problemIs(e.call(http.MethodPatch, "/v1/organisation/profile", controller, map[string]any{"phone": "+65 6000 0000"}, "If-Match", tag),
		"updateOrganisationProfile", 412, CodeStale)

	// A field left out keeps its value; "" clears it.
	r = e.call(http.MethodPatch, "/v1/organisation/profile", controller, map[string]any{"document_footer": ""}, "If-Match", next)
	if r.Status != 200 || r.Body["document_footer"] != nil || r.Body["uen"] != "202312345K" {
		t.Fatalf("clearing the footer: %d %s", r.Status, r.Raw)
	}

	// Every broken rule at once.
	bad := map[string]any{"uen": "abc", "email": "not an address", "time_zone": "Mars/Olympus", "first_year_end": "31/03/2027"}
	r = e.call(http.MethodPatch, "/v1/organisation/profile", controller, bad, "If-Match", r.Header.Get("ETag"))
	e.problemIs(r, "updateOrganisationProfile", 422, CodeInvalid)
	if f := fields(r); f["body.uen"] != "ORG-02" || f["body.email"] != "ORG-03" || f["body.time_zone"] != "ORG-04" || f["body.first_year_end"] != CodeInvalid {
		t.Errorf("want each field with its code, got %v", f)
	}

	// The matrix: the Admin changes it too; the Owner approves (in Founder mode a founder also holds the
	// Controller's role); the Employee sees nothing of it.
	e.problemIs(e.call(http.MethodGet, "/v1/organisation/profile", e.as(authz.Employee), nil), "getOrganisationProfile", 403, CodeForbidden)
	e.problemIs(e.call(http.MethodPatch, "/v1/organisation/profile", e.as(authz.Owner), map[string]any{"phone": "+65 6000 0000"}, "If-Match", "*"),
		"updateOrganisationProfile", 403, CodeForbidden)
	if r := e.call(http.MethodPatch, "/v1/organisation/profile", e.as(authz.Owner, authz.Controller), map[string]any{"phone": "+65 6000 0000"},
		"If-Match", "*"); r.Status != 200 || r.Body["phone"] != "+65 6000 0000" {
		t.Errorf("a founder who is Owner and Controller: %d %s", r.Status, r.Raw)
	}
}

// The GST status follows the registrations' effective dates, its return periods their frequency and
// quarters, and only invoice basis is offered until the Cash Accounting Scheme's rules are pinned.
// req: FR-ORG-03
func TestGSTRegistrationAndStatus(t *testing.T) {
	e := newEnv(t)
	controller := e.as(authz.Controller)
	status := func(date string) response {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/organisation/gst-status?date="+date, controller, nil)
		if r.Status != 200 {
			t.Fatalf("the status on %s: %d %s", date, r.Status, r.Raw)
		}
		return r
	}
	if r := status("2026-05-10"); r.Body["registered"] != false || r.Body["registration"] != nil {
		t.Fatalf("before any registration: %s", r.Raw)
	}

	// Cash basis is refused with its reason; everything wrong is listed at once.
	r := e.call(http.MethodPost, "/v1/organisation/gst-registrations", controller,
		map[string]any{"registered_from": "2026-04-01", "gst_number": " ", "accounting_basis": "cash", "quarter_start_month": 5})
	e.problemIs(r, "createGSTRegistration", 422, CodeInvalid)
	if f := fields(r); f["body.accounting_basis"] != "GST-05" || f["body.gst_number"] != "GST-02" || f["body.quarter_start_month"] != "GST-04" {
		t.Errorf("want each field with its code, got %v", f)
	}

	r = e.call(http.MethodPost, "/v1/organisation/gst-registrations", controller,
		map[string]any{"registered_from": "2026-04-01", "gst_number": "202312345K"}, "Idempotency-Key", uuid.NewString())
	if r.Status != 201 || r.Body["return_frequency"] != "quarterly" || r.Body["quarter_start_month"] != float64(1) ||
		r.Body["accounting_basis"] != "invoice" || r.Header.Get("Location") != "/v1/organisation/gst-registrations/"+r.str("id") {
		t.Fatalf("a registration with the defaults: %d %v %s", r.Status, r.Header, r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")
	s := status("2026-05-10")
	period, _ := s.Body["return_period"].(map[string]any)
	if s.Body["registered"] != true || period["starts_on"] != "2026-04-01" || period["ends_on"] != "2026-06-30" {
		t.Errorf("registered on 10 May: %s", s.Raw)
	}
	if s := status("2026-03-31"); s.Body["registered"] != false {
		t.Errorf("the day before registration: %s", s.Raw)
	}

	// Deregistering is recording the date; a preview saves nothing; the status follows the dates.
	dereg := map[string]any{"deregistered_from": "2027-02-15", "return_frequency": "monthly"}
	if pv := e.call(http.MethodPatch, "/v1/organisation/gst-registrations/"+id, controller, dereg, "If-Match", tag, "Prefer", "return=preview"); pv.Status != 200 ||
		pv.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}
	if s := status("2027-03-01"); s.Body["registered"] != true {
		t.Fatalf("the preview deregistered it: %s", s.Raw)
	}
	r = e.call(http.MethodPatch, "/v1/organisation/gst-registrations/"+id, controller, dereg, "If-Match", tag)
	if r.Status != 200 || r.Body["version"] != float64(2) || r.Body["deregistered_from"] != "2027-02-15" {
		t.Fatalf("deregistering: %d %s", r.Status, r.Raw)
	}
	s = status("2027-02-10")
	period, _ = s.Body["return_period"].(map[string]any)
	if period["starts_on"] != "2027-02-01" || period["ends_on"] != "2027-02-14" {
		t.Errorf("the last, short monthly period: %s", s.Raw)
	}
	if s := status("2027-02-15"); s.Body["registered"] != false {
		t.Errorf("the day of deregistration: %s", s.Raw)
	}
	e.problemIs(e.call(http.MethodPatch, "/v1/organisation/gst-registrations/"+id, controller, dereg, "If-Match", tag),
		"updateGSTRegistration", 412, CodeStale)

	// A registration overlapping another is refused.
	e.problemIs(e.call(http.MethodPost, "/v1/organisation/gst-registrations", controller,
		map[string]any{"registered_from": "2027-01-01", "gst_number": "202312345K"}), "createGSTRegistration", 409, storage.CodeOverlap)
	if l := e.call(http.MethodGet, "/v1/organisation/gst-registrations", controller, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the list: %d %s", l.Status, l.Raw)
	}
}

// Journals are numbered from their sequence when they post: concurrent postings on independent
// connections take every number once, a posting that fails takes none, a preview takes none, and the
// sequence can move forward but never back.
// req: FR-ORG-04
func TestDocumentNumbers(t *testing.T) {
	e := newEnv(t)
	accountant, controller := e.as(authz.Accountant), e.as(authz.Controller)

	// A preview shows the number the posting would take, and takes none.
	id, tag := e.createDraft("10.00")
	pv := e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, "If-Match", tag, "Prefer", "return=preview")
	if pv.Status != 200 || pv.Body["journal"].(map[string]any)["number"] != "JNL-0001" {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}

	// Twenty postings at once, a third of them into a closed period: the ones that post take
	// JNL-0001 onwards with no gap and no repeat; the ones that fail take nothing.
	november := e.books.Nov.ID
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Close(ctx, november)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	type draft struct{ id, tag string }
	drafts := []draft{{id, tag}}
	for i := range 19 {
		d := e.draft("1.00")
		if i%3 == 0 {
			d["date"] = "2026-11-05"
		}
		r := e.call(http.MethodPost, "/v1/journals", accountant, d)
		if r.Status != 201 || r.str("number") != "" {
			t.Fatalf("a gap-free sequence numbered a draft: %d %s", r.Status, r.Raw)
		}
		drafts = append(drafts, draft{r.str("id"), r.Header.Get("ETag")})
	}
	var wg sync.WaitGroup
	numbers := make([]string, len(drafts))
	for i, d := range drafts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := e.call(http.MethodPost, "/v1/journals/"+d.id+"/post", accountant, nil, "If-Match", d.tag)
			if r.Status == 200 {
				numbers[i] = r.Body["journal"].(map[string]any)["number"].(string)
			}
		}()
	}
	wg.Wait()
	var posted []string
	for i, n := range numbers {
		if n != "" {
			posted = append(posted, n)
		} else if g := e.call(http.MethodGet, "/v1/journals/"+drafts[i].id, accountant, nil); g.str("number") != "" {
			t.Errorf("a journal whose posting failed has the number %s", g.str("number"))
		}
	}
	slices.Sort(posted)
	if len(posted) != 13 {
		t.Fatalf("%d postings committed; want the 13 outside November: %v", len(posted), posted)
	}
	for i, n := range posted {
		if want := fmt.Sprintf("JNL-%04d", i+1); n != want {
			t.Fatalf("the numbers are %v; want JNL-0001 to JNL-0013 with no gap or repeat", posted)
		}
	}

	// The sequence: forward only; a new prefix and padding; not gap-free numbers a journal when saved.
	s := e.call(http.MethodGet, "/v1/organisation/sequences/journal", controller, nil)
	if s.Status != 200 || s.Body["next_number"] != float64(14) || s.Body["example"] != "JNL-0014" {
		t.Fatalf("the sequence after 13 postings: %d %s", s.Status, s.Raw)
	}
	r := e.call(http.MethodPatch, "/v1/organisation/sequences/journal", controller, map[string]any{"next_number": 5}, "If-Match", s.Header.Get("ETag"))
	e.problemIs(r, "updateSequence", 422, CodeInvalid)
	if f := fields(r); f["body.next_number"] != "ORG-09" {
		t.Errorf("moving the next number back: %v", f)
	}
	r = e.call(http.MethodPatch, "/v1/organisation/sequences/journal", controller,
		map[string]any{"prefix": "JV/", "next_number": 100, "digits": 5, "gap_free": false}, "If-Match", s.Header.Get("ETag"))
	if r.Status != 200 || r.Body["example"] != "JV/00100" || r.Body["gap_free"] != false {
		t.Fatalf("changing the sequence: %d %s", r.Status, r.Raw)
	}
	if d := e.call(http.MethodPost, "/v1/journals", accountant, e.draft("2.00")); d.Status != 201 || d.str("number") != "JV/00100" {
		t.Errorf("a draft saved under a sequence that is not gap-free: %d %s", d.Status, d.Raw)
	}
}

// The standard chart loads once into an organisation with no account: 88 accounts with every attribute, and the
// expense policy's starter claim categories; a second load, or a load into an organisation with accounts, is refused.
// req: FR-COA-01
// req: FR-COA-02
// req: FR-AP-15
// req: ledger/chart-of-accounts Default useful life (rev 0.151)
func TestStandardChart(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	org, err := e.store.CreateOrganisation(ctx, storage.NewOrganisation{Name: "Standard Chart Pte Ltd", BaseCurrency: "SGD"})
	if err != nil {
		t.Fatal(err)
	}
	accountant := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: org.ID, Roles: []authz.Role{authz.Accountant}})
	list := func() map[string]map[string]any {
		r := e.call(http.MethodGet, "/v1/accounts?page_size=500", accountant, nil)
		out := map[string]map[string]any{}
		for _, x := range r.Body["items"].([]any) {
			a := x.(map[string]any)
			out[a["code"].(string)] = a
		}
		return out
	}

	if r := e.call(http.MethodPost, "/v1/accounts:load-standard-chart", accountant, nil, "Prefer", "return=preview"); r.Status != 200 ||
		r.Body["accounts"] != float64(88) || r.Body["claim_categories"] != float64(7) || len(list()) != 0 {
		t.Fatalf("the preview: %d %s", r.Status, r.Raw)
	}
	r := e.call(http.MethodPost, "/v1/accounts:load-standard-chart", accountant, nil, "Idempotency-Key", uuid.NewString())
	if r.Status != 201 || r.Body["accounts"] != float64(88) || r.Body["claim_categories"] != float64(7) || r.Header.Get("Location") != "/v1/accounts" {
		t.Fatalf("loading the chart: %d %v %s", r.Status, r.Header, r.Raw)
	}
	accounts := list()
	if len(accounts) != 88 {
		t.Fatalf("%d accounts after loading", len(accounts))
	}
	// The expense policy's starter categories come with it, each on its standard account with no limit (FR-AP-15).
	if err := e.store.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
		cats, err := u.ClaimCategories().List(ctx)
		if err != nil {
			return err
		}
		got := map[string]string{}
		for _, c := range cats {
			if c.LineLimit != nil || c.ReceiptAbove != nil || c.AttendeesAbove != nil || !c.Active || c.Currency != "SGD" {
				t.Errorf("starter category %s: %+v", c.Name, c)
			}
			got[c.Name] = c.AccountID.String()
			if c.Mileage {
				got[c.Name] += " mileage"
			}
		}
		for name, code := range map[string]string{"Local travel": "6510", "Overseas travel": "6520", "Meals & entertainment": "6530",
			"Software & subscriptions": "6210", "Telephone & internet": "6330", "Office supplies": "6340", "Mileage": "6510 mileage"} {
			id, mileage, _ := strings.Cut(code, " ")
			want := accounts[id]["id"].(string)
			if mileage != "" {
				want += " mileage"
			}
			if got[name] != want {
				t.Errorf("starter category %s: %q, want %q", name, got[name], want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for code, want := range map[string]map[string]any{
		"1020": {"type": "asset", "report_group": "Cash & bank", "cash_flow_line": "Cash & cash equivalents", "money_account": true, "default_gst_code": "NA"},
		"1100": {"type": "asset", "control": true, "money_account": false, "revalue": true},
		"1210": {"type": "asset", "revalue": false},
		"3010": {"type": "equity", "role": "share-capital"},
		"5010": {"type": "expense", "function": "COGS", "default_gst_code": "TX"},
		"6020": {"type": "expense", "payroll_cost": true, "function": "G&A"},
		"7040": {"type": "revenue", "name": "Unrealised FX gain or loss"},
		"7050": {"name": "Gain / (loss) on disposal of assets"},
		"1160": {"type": "asset", "report_group": "Other current assets", "name": "Asset disposal clearing"},
		"1510": {"useful_life_months": float64(36)},
		"1520": {"useful_life_months": float64(60)},
	} {
		for field, v := range want {
			if accounts[code][field] != v {
				t.Errorf("account %s: %s is %v; want %v", code, field, accounts[code][field], v)
			}
		}
		if g, _ := accounts[code]["guidance"].(string); g == "" {
			t.Errorf("account %s has no guidance", code)
		}
	}

	// Revaluation is for monetary items only (FR-GL-04; rev 0.72): marking an income account is refused, a bank account takes it.
	rent, bank := accounts["6110"], accounts["1030"]
	e.problemIs(e.call(http.MethodPatch, "/v1/accounts/"+rent["id"].(string), accountant, map[string]any{"revalue": true}, "If-Match", fmt.Sprintf(`"%v"`, rent["version"])),
		"updateAccount", 422, CodeInvalid)
	if r := e.call(http.MethodPatch, "/v1/accounts/"+bank["id"].(string), accountant, map[string]any{"revalue": false}, "If-Match", fmt.Sprintf(`"%v"`, bank["version"])); r.Status != 200 || r.Body["revalue"] != false {
		t.Errorf("unmarking a bank account: %d %s", r.Status, r.Raw)
	}
	// A default useful life is for the register's accounts only (rev 0.151): rent refuses one (COA-11), 1520 takes 48 months,
	// and 0 clears it.
	if r := e.call(http.MethodPatch, "/v1/accounts/"+rent["id"].(string), accountant, map[string]any{"useful_life_months": 36}, "If-Match", fmt.Sprintf(`"%v"`, rent["version"])); r.Status != 422 || !strings.Contains(r.Raw, "COA-11") {
		t.Errorf("a life on rent: %d %s", r.Status, r.Raw)
	}
	furniture := accounts["1520"]
	r = e.call(http.MethodPatch, "/v1/accounts/"+furniture["id"].(string), accountant, map[string]any{"useful_life_months": 48}, "If-Match", fmt.Sprintf(`"%v"`, furniture["version"]))
	if r.Status != 200 || r.Body["useful_life_months"] != float64(48) {
		t.Errorf("48 months on 1520: %d %s", r.Status, r.Raw)
	}
	if c := e.call(http.MethodPatch, "/v1/accounts/"+furniture["id"].(string), accountant, map[string]any{"useful_life_months": 0}, "If-Match", r.Header.Get("ETag")); c.Status != 200 || c.Body["useful_life_months"] != nil {
		t.Errorf("clearing 1520's life: %d %s", c.Status, c.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/accounts:load-standard-chart", accountant, nil), "loadStandardChart", 409, CodeChartLoaded)
	e.problemIs(e.call(http.MethodPost, "/v1/accounts:load-standard-chart", e.as(authz.Accountant), nil), "loadStandardChart", 409, CodeChartLoaded)
	if n := len(list()); n != 88 {
		t.Errorf("a refused load changed the chart: %d accounts", n)
	}
}

// The chart is editable: accounts are added under a group, renamed and deactivated at their version;
// a deactivated account takes no new postings; the protected flags wait for approval; a system account
// stays active.
// req: FR-COA-01
// req: FR-COA-03
// req: FR-COA-04
// req: FR-COA-05
func TestEditingTheChart(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	parent := e.books.Expenses.ID.String()
	draft := map[string]any{"code": "6115", "name": "Co-working memberships", "type": "expense", "parent_id": parent,
		"report_group": "Premises & office", "function": "G&A", "default_gst_code": "TX", "guidance": "Hot-desk and co-working memberships."}
	if pv := e.call(http.MethodPost, "/v1/accounts", accountant, draft, "Prefer", "return=preview"); pv.Status != 200 ||
		pv.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("the preview: %d %s", pv.Status, pv.Raw)
	}
	r := e.call(http.MethodPost, "/v1/accounts", accountant, draft, "Idempotency-Key", uuid.NewString())
	if r.Status != 201 || r.str("parent_id") != parent || r.Body["function"] != "G&A" || r.Header.Get("Location") != "/v1/accounts/"+r.str("id") {
		t.Fatalf("adding an account: %d %v %s", r.Status, r.Header, r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")
	e.problemIs(e.call(http.MethodPost, "/v1/accounts", accountant, draft), "createAccount", 409, storage.CodeDuplicate)
	r = e.call(http.MethodPost, "/v1/accounts", accountant, map[string]any{"code": "-1", "name": "X", "type": "income", "function": "Ops"})
	e.problemIs(r, "createAccount", 422, CodeInvalid)
	if f := fields(r); f["body.code"] != "COA-01" || f["body.type"] != "COA-03" || f["body.function"] != "COA-04" {
		t.Errorf("every rule at once: %v", f)
	}

	// Rename it; the protected flags wait for approval; a stale ETag is refused.
	r = e.call(http.MethodPatch, "/v1/accounts/"+id, accountant, map[string]any{"name": "Co-working", "guidance": ""}, "If-Match", tag)
	if r.Status != 200 || r.Body["name"] != "Co-working" || r.Body["guidance"] != nil || r.Body["code"] != "6115" {
		t.Fatalf("renaming: %d %s", r.Status, r.Raw)
	}
	next := r.Header.Get("ETag")
	r = e.call(http.MethodPatch, "/v1/accounts/"+id, accountant, map[string]any{"control": true, "payroll_cost": true}, "If-Match", next)
	if f := fields(r); r.Status != 422 || f["body.control"] != "COA-08" || f["body.payroll_cost"] != "COA-08" {
		t.Errorf("changing the protected flags: %d %v", r.Status, f)
	}
	e.problemIs(e.call(http.MethodPatch, "/v1/accounts/"+id, accountant, map[string]any{"name": "X"}, "If-Match", tag), "updateAccount", 412, CodeStale)

	// A group cannot sit under its own child.
	r = e.call(http.MethodGet, "/v1/accounts/"+parent, accountant, nil)
	e.problemIs(e.call(http.MethodPatch, "/v1/accounts/"+parent, accountant, map[string]any{"parent_id": id}, "If-Match", r.Header.Get("ETag")),
		"updateAccount", 409, storage.CodeAccountTree)

	// Deactivated, it takes no new postings (FR-COA-04).
	r = e.call(http.MethodPatch, "/v1/accounts/"+id, accountant, map[string]any{"active": false}, "If-Match", next)
	if r.Status != 200 || r.Body["active"] != false {
		t.Fatalf("deactivating: %d %s", r.Status, r.Raw)
	}
	d := e.draft("10.00")
	d["lines"].([]map[string]any)[0]["account_id"] = id
	j := e.call(http.MethodPost, "/v1/journals", accountant, d)
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+j.str("id")+"/post", accountant, nil, "If-Match", j.Header.Get("ETag")),
		"postJournal", 422, ledger.CodeInactiveAccount)

	// A system account stays active (FR-COA-03).
	org, err := e.store.CreateOrganisation(context.Background(), storage.NewOrganisation{Name: "System Accounts Pte Ltd", BaseCurrency: "SGD"})
	if err != nil {
		t.Fatal(err)
	}
	other := e.principal(authn.Principal{Client: "web", Class: authn.ClassUser, Organisation: org.ID, Roles: []authz.Role{authz.Accountant}})
	if r := e.call(http.MethodPost, "/v1/accounts:load-standard-chart", other, nil); r.Status != 201 {
		t.Fatalf("loading the chart: %d %s", r.Status, r.Raw)
	}
	var ar map[string]any
	for _, x := range e.call(http.MethodGet, "/v1/accounts?page_size=500", other, nil).Body["items"].([]any) {
		if a := x.(map[string]any); a["code"] == "1100" {
			ar = a
		}
	}
	if ar["role"] != "accounts-receivable" {
		t.Fatalf("1100's role: %v", ar)
	}
	r = e.call(http.MethodPatch, "/v1/accounts/"+ar["id"].(string), other, map[string]any{"active": false}, "If-Match", "*")
	if f := fields(r); r.Status != 422 || f["body.active"] != "COA-09" {
		t.Errorf("deactivating AR: %d %v", r.Status, f)
	}
}

// Contacts: added with their details, flagged when they may duplicate another, changed at their version,
// listed by role.
// req: FR-CT-01
// req: FR-CT-04
func TestContacts(t *testing.T) {
	e := newEnv(t)
	ar := e.as(authz.ARSpecialist)
	customer := map[string]any{"legal_name": "Northwind Analytics Pte. Ltd.", "customer": true, "uen_or_tax_id": "202312345K",
		"peppol_id": "0195:SGUEN202312345K", "billing_email": "accounts@northwind.example", "country": "SG", "currency": "SGD",
		"payment_terms_days": 30, "default_account_id": e.books.Sales.ID.String(), "default_gst_code": "SR"}
	r := e.call(http.MethodPost, "/v1/contacts", ar, customer, "Idempotency-Key", uuid.NewString())
	if r.Status != 201 || r.Body["payment_terms_days"] != float64(30) || len(r.Body["possible_duplicates"].([]any)) != 0 ||
		r.Header.Get("Location") != "/v1/contacts/"+r.str("id") {
		t.Fatalf("adding a customer: %d %v %s", r.Status, r.Header, r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")

	// The same name with other spacing and case, previewed: flagged, not refused, and not saved.
	twin := map[string]any{"legal_name": "NORTHWIND  Analytics Pte. Ltd.", "vendor": true}
	pv := e.call(http.MethodPost, "/v1/contacts", ar, twin, "Prefer", "return=preview")
	dups, _ := pv.Body["possible_duplicates"].([]any)
	if pv.Status != 200 || len(dups) != 1 || dups[0].(map[string]any)["id"] != id || dups[0].(map[string]any)["same_name"] != true {
		t.Fatalf("a possible duplicate, previewed: %d %s", pv.Status, pv.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/contacts", ar, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the preview saved a contact: %s", l.Raw)
	}

	// Every broken rule at once; a group account is no default category.
	r = e.call(http.MethodPost, "/v1/contacts", ar, map[string]any{"legal_name": "X", "billing_email": "nope", "country": "Singapore",
		"default_account_id": e.books.Expenses.ID.String()})
	e.problemIs(r, "createContact", 422, CodeInvalid)
	if f := fields(r); f["body.customer"] != "CT-02" || f["body.billing_email"] != "CT-03" || f["body.country"] != "CT-04" ||
		f["body.default_account_id"] != "CT-10" {
		t.Errorf("every rule at once: %v", f)
	}

	// A change at its version: now a vendor too, terms cleared.
	r = e.call(http.MethodPatch, "/v1/contacts/"+id, ar, map[string]any{"vendor": true, "payment_terms_days": -1, "phone": "+65 6123 4567"}, "If-Match", tag)
	if r.Status != 200 || r.Body["vendor"] != true || r.Body["payment_terms_days"] != nil || r.Body["phone"] != "+65 6123 4567" {
		t.Fatalf("changing the contact: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPatch, "/v1/contacts/"+id, ar, map[string]any{"phone": "1"}, "If-Match", tag), "updateContact", 412, CodeStale)

	// Listed by role.
	e.call(http.MethodPost, "/v1/contacts", ar, map[string]any{"legal_name": "Acme Cloud Pte Ltd", "vendor": true})
	for role, want := range map[string]int{"": 2, "customer": 1, "vendor": 2} {
		path := "/v1/contacts"
		if role != "" {
			path += "?role=" + role
		}
		if l := e.call(http.MethodGet, path, ar, nil); l.Status != 200 || len(l.Body["items"].([]any)) != want {
			t.Errorf("the list for %q: %d %s", role, l.Status, l.Raw)
		}
	}
	if g := e.call(http.MethodGet, "/v1/contacts/"+id, e.as(authz.Auditor), nil); g.Status != 200 || g.Header.Get("ETag") == "" {
		t.Errorf("the auditor reading a contact: %d %s", g.Status, g.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/contacts/"+id, e.as(authz.Employee), nil), "getContact", 403, CodeForbidden)
}

// Items: added with a price per currency as exact decimals, refused when a rule breaks, changed at their
// version with their prices replaced, listed by code.
// req: FR-ITM-01
func TestItems(t *testing.T) {
	e := newEnv(t)
	ar := e.as(authz.ARSpecialist)
	consult := map[string]any{"code": "CONSULT-HR", "name": "HR consulting", "description": "HR consulting, by the hour", "type": "service",
		"unit": "hour", "revenue_account_id": e.books.Sales.ID.String(), "gst_code": "SR",
		"prices": []map[string]any{{"currency": "USD", "unit_price": "135.5"}, {"currency": "SGD", "unit_price": "180.123456"}}}
	r := e.call(http.MethodPost, "/v1/items", ar, consult, "Idempotency-Key", uuid.NewString())
	if r.Status != 201 || r.Header.Get("Location") != "/v1/items/"+r.str("id") ||
		!strings.Contains(r.Raw, `"prices":[{"currency":"SGD","unit_price":"180.123456"},{"currency":"USD","unit_price":"135.50"}]`) {
		t.Fatalf("adding an item: %d %v %s", r.Status, r.Header, r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")
	e.problemIs(e.call(http.MethodPost, "/v1/items", ar, map[string]any{"code": "CONSULT-HR", "name": "Again", "type": "service"}),
		"createItem", 409, storage.CodeDuplicate)

	// Previewed: checked and not saved.
	pv := e.call(http.MethodPost, "/v1/items", ar, map[string]any{"code": "BOOK-01", "name": "The handbook", "type": "product"}, "Prefer", "return=preview")
	if pv.Status != 200 || pv.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview: %d %s", pv.Status, pv.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/items", ar, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("the preview saved an item: %s", l.Raw)
	}

	// Every broken rule at once: the code, two prices in one currency, a group as the revenue account.
	r = e.call(http.MethodPost, "/v1/items", ar, map[string]any{"code": "-X", "name": "X", "type": "service", "gst_code": "sr",
		"revenue_account_id": e.books.Expenses.ID.String(),
		"prices":             []map[string]any{{"currency": "SGD", "unit_price": "1"}, {"currency": "SGD", "unit_price": "2"}}})
	e.problemIs(r, "createItem", 422, CodeInvalid)
	if f := fields(r); f["body.code"] != "ITM-01" || f["body.gst_code"] != "ITM-07" || f["body.prices[1].currency"] != "ITM-05" ||
		f["body.revenue_account_id"] != "ITM-08" {
		t.Errorf("every rule at once: %v", f)
	}
	// An expense account is no revenue account.
	r = e.call(http.MethodPost, "/v1/items", ar, map[string]any{"code": "RENT", "name": "Rent", "type": "service", "revenue_account_id": e.books.Rent.ID.String()})
	if f := fields(r); r.Status != 422 || f["body.revenue_account_id"] != "ITM-08" {
		t.Errorf("an expense account as the revenue account: %d %v", r.Status, f)
	}

	// A change at its version replaces every price; the rest is kept.
	r = e.call(http.MethodPatch, "/v1/items/"+id, ar, map[string]any{"prices": []map[string]any{{"currency": "JPY", "unit_price": "15000"}}, "active": false},
		"If-Match", tag)
	if r.Status != 200 || r.Body["active"] != false || r.Body["unit"] != "hour" || r.Body["version"] != float64(2) ||
		!strings.Contains(r.Raw, `"prices":[{"currency":"JPY","unit_price":"15000"}]`) {
		t.Fatalf("changing the item: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPatch, "/v1/items/"+id, ar, map[string]any{"unit": "day"}, "If-Match", tag), "updateItem", 412, CodeStale)

	// Listed by code, a page at a time.
	e.call(http.MethodPost, "/v1/items", ar, map[string]any{"code": "BOOK-01", "name": "The handbook", "type": "product"})
	l := e.call(http.MethodGet, "/v1/items?page_size=1", ar, nil)
	if l.Status != 200 || len(l.Body["items"].([]any)) != 1 || l.Body["items"].([]any)[0].(map[string]any)["code"] != "BOOK-01" || l.str("next_cursor") == "" {
		t.Fatalf("the first page: %d %s", l.Status, l.Raw)
	}
	l = e.call(http.MethodGet, "/v1/items?page_size=1&cursor="+l.str("next_cursor"), ar, nil)
	if l.Status != 200 || len(l.Body["items"].([]any)) != 1 || l.Body["items"].([]any)[0].(map[string]any)["id"] != id || l.Body["next_cursor"] != nil {
		t.Errorf("the next page: %d %s", l.Status, l.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/items/"+id, e.as(authz.Auditor), nil); g.Status != 200 || g.Header.Get("ETag") == "" {
		t.Errorf("the auditor reading an item: %d %s", g.Status, g.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/items/"+id, e.as(authz.Employee), nil), "getItem", 403, CodeForbidden)
}

// Organisation settings: every one listed at its recommended value; a change recorded from today or later,
// at the setting's version, with earlier values kept; a reset records the recommended value; the platform's
// settings and those under dual control (FM-1) are refused and nothing changes; the feature switches reach
// the session.
// req: FR-ORG-04
// req: FR-ORG-06
// req: FR-ORG-07
// req: FR-ORG-08
func TestOrganisationSettings(t *testing.T) {
	e := newEnv(t)
	ctl := e.as(authz.Controller)
	path := "/v1/organisation/settings/sales.payment_terms_days"
	setting := func(r response) map[string]any { return r.Body }

	l := e.call(http.MethodGet, "/v1/organisation/settings?page_size=500", e.as(authz.Auditor), nil)
	items, _ := l.Body["items"].([]any)
	if l.Status != 200 || l.str("on") != "2026-10-15" || len(items) != len(settings.Catalogue) {
		t.Fatalf("the list: %d %s", l.Status, l.Raw)
	}
	for _, it := range items {
		s := it.(map[string]any)
		if s["status"] != "recommended" || s["value"] != s["recommended_value"] || s["version"] != float64(0) || s["guide"] == "" {
			t.Errorf("%s does not ship at its recommended value: %v", s["key"], s)
		}
		if s["key"] == "data.region" && (s["set_by_platform"] != true || s["value"] != "asia-southeast1") {
			t.Errorf("the data region: %v", s)
		}
		if s["key"] == "retention.records_years" && (s["needs_second_approver"] != true || s["lengthen_only"] != true || s["value"] != "7") {
			t.Errorf("the records' retention: %v", s)
		}
	}

	// A change from today at version 0; the stale ETag refused; a preview saves nothing.
	r := e.call(http.MethodPatch, path, ctl, map[string]any{"value": "45"}, "If-Match", `"0"`)
	if r.Status != 200 || r.str("value") != "45" || r.str("status") != "changed" || r.str("effective_from") != "2026-10-15" ||
		r.Header.Get("ETag") != `"1"` || len(setting(r)["changes"].([]any)) != 1 {
		t.Fatalf("changing the payment terms: %d %v %s", r.Status, r.Header, r.Raw)
	}
	e.problemIs(e.call(http.MethodPatch, path, ctl, map[string]any{"value": "14"}, "If-Match", `"0"`), "updateSetting", 412, CodeStale)
	pv := e.call(http.MethodPatch, path, ctl, map[string]any{"value": "14"}, "If-Match", `"1"`, "Prefer", "return=preview")
	if pv.Status != 200 || pv.str("value") != "14" || pv.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview: %d %s", pv.Status, pv.Raw)
	}

	// A change scheduled for later shows as scheduled, and takes effect on its day; the earlier value is kept.
	r = e.call(http.MethodPatch, path, ctl, map[string]any{"value": "60", "effective_from": "2026-11-01"}, "If-Match", `"1"`)
	if r.Status != 200 || r.str("value") != "45" || len(setting(r)["scheduled"].([]any)) != 1 {
		t.Fatalf("a scheduled change: %d %s", r.Status, r.Raw)
	}
	for on, want := range map[string]string{"2026-10-14": "30", "2026-10-31": "45", "2026-11-01": "60"} {
		if g := e.call(http.MethodGet, path+"?on="+on, ctl, nil); g.Status != 200 || g.str("value") != want || g.Header.Get("ETag") != `"2"` {
			t.Errorf("the value on %s: %d %s; want %s", on, g.Status, g.Raw, want)
		}
	}

	// Back-dated, not a value, both a value and a reset: every rule at once where it can be.
	r = e.call(http.MethodPatch, path, ctl, map[string]any{"value": "400", "effective_from": "2026-10-14"}, "If-Match", `"2"`)
	e.problemIs(r, "updateSetting", 422, CodeInvalid)
	if f := fields(r); f["body.value"] != settings.CodeValue {
		t.Errorf("a value out of range: %v", f)
	}
	r = e.call(http.MethodPatch, path, ctl, map[string]any{"value": "14", "effective_from": "2026-10-14"}, "If-Match", `"2"`)
	if f := fields(r); r.Status != 422 || f["body.effective_from"] != settings.CodeBackdated {
		t.Errorf("a back-dated change: %d %v", r.Status, f)
	}
	r = e.call(http.MethodPatch, path, ctl, map[string]any{"value": "14", "reset": true}, "If-Match", `"2"`)
	if f := fields(r); r.Status != 422 || f["body.value"] != settings.CodeValueOrReset {
		t.Errorf("a value and a reset: %d %v", r.Status, f)
	}

	// Reset to recommended: a new change holding the recommended value.
	r = e.call(http.MethodPatch, path, ctl, map[string]any{"reset": true}, "If-Match", `"2"`)
	if r.Status != 200 || r.str("value") != "30" || r.str("status") != "recommended" || r.Body["version"] != float64(3) {
		t.Fatalf("a reset: %d %s", r.Status, r.Raw)
	}

	// The platform's settings are refused; those under dual control wait for a second approver, which asking needs a
	// person who passed step-up for (FM-1, rev 0.36): without one, nothing changes.
	e.problemIs(e.call(http.MethodPatch, "/v1/organisation/settings/data.region", ctl, map[string]any{"value": "europe-west1"}, "If-Match", `"0"`),
		"updateSetting", 409, settings.CodeFixed)
	for key, value := range map[string]string{"bank.tolerance_base": "2.00", "retention.records_years": "8", "features.ai_assistant": "true",
		"assets.capitalisation_threshold": "2000.00"} {
		e.problemIs(e.call(http.MethodPatch, "/v1/organisation/settings/"+key, ctl, map[string]any{"value": value}, "If-Match", `"0"`),
			"updateSetting", 403, CodeStepUpRequired)
		if g := e.call(http.MethodGet, "/v1/organisation/settings/"+key, ctl, nil); g.Body["version"] != float64(0) || g.str("status") != "recommended" {
			t.Errorf("%s changed: %s", key, g.Raw)
		}
	}
	e.problemIs(e.call(http.MethodGet, "/v1/organisation/settings/features.nothing", ctl, nil), "getSetting", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodGet, "/v1/organisation/settings", e.as(authz.Employee), nil), "listSettings", 403, CodeForbidden)

	// A feature switched on reaches the session.
	if s := e.call(http.MethodGet, "/v1/session", ctl, nil); s.Status != 200 || !strings.Contains(s.Raw, `"features":{"multi_currency":true,"payroll":false,"projects":false`) {
		t.Fatalf("the session's features: %d %s", s.Status, s.Raw)
	}
	e.call(http.MethodPatch, "/v1/organisation/settings/features.projects", ctl, map[string]any{"value": "true"}, "If-Match", `"0"`)
	if s := e.call(http.MethodGet, "/v1/session", ctl, nil); !strings.Contains(s.Raw, `"projects":true`) {
		t.Errorf("projects switched on, the session: %s", s.Raw)
	}
}

// Members: until a second member joins, the Owner invites people with roles and changes roles alone (the
// founder bootstrap); an invitation is a one-time code, kept without it, withdrawable; one made before keeps its
// roles when accepted after; the last Owner keeps the role; from the second member on, role changes wait for a
// second approver (approvals_test.go), and nothing changes without one.
// req: FR-ADM-01
// req: FM-1
func TestMembersAndTheFounderBootstrap(t *testing.T) {
	e := newEnv(t)
	owner := e.as(authz.Owner)
	person := func(subject string) string { return e.principal(authn.Principal{Subject: subject, Client: "web"}) }
	invite := func(name string, roles []string, extra ...any) response {
		body := map[string]any{"display_name": name, "roles": roles}
		for i := 0; i+1 < len(extra); i += 2 {
			body[extra[i].(string)] = extra[i+1]
		}
		return e.call(http.MethodPost, "/v1/members/invitations", owner, body)
	}
	join := func(subject, code string) response {
		return e.call(http.MethodPost, "/v1/members", person(subject), map[string]any{"invitation_code": code, "display_name": subject})
	}
	roles := func() map[string][]string {
		out := map[string][]string{}
		for _, it := range e.call(http.MethodGet, "/v1/members", e.as(authz.Auditor), nil).Body["items"].([]any) {
			m := it.(map[string]any)
			for _, r := range m["roles"].([]any) {
				out[m["display_name"].(string)] = append(out[m["display_name"].(string)], r.(map[string]any)["role"].(string))
			}
		}
		return out
	}

	// Invitations while the organisation has no second member: a code shown once, valid 7 days.
	cofounder := invite("weiling", []string{"controller", "accountant"})
	if cofounder.Status != 201 || len(cofounder.str("code")) < 20 || cofounder.str("expires_at") != "2026-10-22T09:00:00Z" {
		t.Fatalf("inviting a co-founder: %d %s", cofounder.Status, cofounder.Raw)
	}
	third := invite("ahkow", []string{"bookkeeper"})
	auditor := invite("auditor", []string{"auditor"}, "roles_expire_at", "2027-03-31T00:00:00Z")
	r := invite("x", []string{"super_admin", "owner", "owner"}, "roles_expire_at", "2026-10-01T00:00:00Z")
	e.problemIs(r, "createMemberInvitation", 422, CodeInvalid)
	if f := fields(r); f["body.roles[0]"] != member.CodeRole || f["body.roles[2]"] != member.CodeRoles || f["body.roles_expire_at"] != member.CodeExpiry {
		t.Errorf("every rule at once: %v", f)
	}
	l := e.call(http.MethodGet, "/v1/members/invitations", e.as(authz.Auditor), nil)
	if l.Status != 200 || len(l.Body["items"].([]any)) != 3 || strings.Contains(l.Raw, cofounder.str("code")) || strings.Contains(l.Raw, `"code"`) {
		t.Fatalf("the pending invitations, without their codes: %d %s", l.Status, l.Raw)
	}

	// Withdrawn: its code stops working, and a wrong code is refused the same way.
	if w := e.call(http.MethodDelete, "/v1/members/invitations/"+auditor.str("id"), owner, nil); w.Status != 200 || w.str("withdrawn_at") == "" {
		t.Fatalf("withdrawing: %d %s", w.Status, w.Raw)
	}
	e.problemIs(join("auditor", auditor.str("code")), "acceptMemberInvitation", 409, CodeCodeRefused)
	e.problemIs(join("guesser", "NEVER-A-VALID-INVITATION-0"), "acceptMemberInvitation", 409, CodeCodeRefused)

	// The co-founder joins with the invitation's roles; the code works once.
	j := join("weiling", cofounder.str("code"))
	if j.Status != 201 || !strings.Contains(j.Raw, `"roles":["accountant","controller"]`) || j.str("organisation_id") != e.books.Org.String() {
		t.Fatalf("joining: %d %s", j.Status, j.Raw)
	}
	e.problemIs(join("someone", cofounder.str("code")), "acceptMemberInvitation", 409, CodeCodeRefused)
	mid := j.str("member_id")

	// Still one member: the Owner changes roles alone; the last Owner keeps the role.
	if a := e.call(http.MethodPost, "/v1/members/"+mid+"/roles", owner, map[string]any{"role": "owner"}); a.Status != 201 {
		t.Fatalf("making the co-founder Owner: %d %s", a.Status, a.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/members/"+mid+"/roles", owner, map[string]any{"role": "owner"}), "assignRole", 409, member.CodeHeld)
	e.problemIs(e.call(http.MethodDelete, "/v1/members/"+mid+"/roles/owner", owner, nil), "revokeRole", 409, member.CodeLastOwner)
	if d := e.call(http.MethodDelete, "/v1/members/"+mid+"/roles/accountant", owner, nil); d.Status != 200 || strings.Contains(d.Raw, "accountant") {
		t.Fatalf("ending a role: %d %s", d.Status, d.Raw)
	}

	// The third founder's invitation, made before, keeps its roles; now role changes wait for a second approver, which
	// asking needs a person who passed step-up for (rev 0.36): without one, nothing changes.
	if j := join("ahkow", third.str("code")); j.Status != 201 || !strings.Contains(j.Raw, `"roles":["bookkeeper"]`) {
		t.Fatalf("joining as the second member: %d %s", j.Status, j.Raw)
	}
	before := roles()
	e.problemIs(e.call(http.MethodPost, "/v1/members/"+mid+"/roles", owner, map[string]any{"role": "accountant"}), "assignRole", 403, CodeStepUpRequired)
	e.problemIs(e.call(http.MethodDelete, "/v1/members/"+mid+"/roles/controller", e.as(authz.Administrator), nil), "revokeRole", 403, CodeStepUpRequired)
	e.problemIs(invite("late", []string{"employee"}), "createMemberInvitation", 403, CodeStepUpRequired)
	if after := roles(); fmt.Sprint(after) != fmt.Sprint(before) || fmt.Sprint(after["weiling"]) != "[controller owner]" {
		t.Errorf("roles changed after the bootstrap: %v, then %v", before, after)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/members", e.as(authz.Employee), nil), "listMembers", 403, CodeForbidden)

	// Every change is in the organisation's trail, and no invitation code reaches the trail or a log (SEC-15).
	reader, err := postgres.Open(context.Background(), db.URL("fyniq_readonly"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var trail strings.Builder
	if err := reader.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 1000)
		for _, ev := range events {
			trail.WriteString(ev.Body + "\n")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"member_invitation.created", "member_invitation.withdrawn", "member_invitation.accepted", "user_membership.created",
		"role_assignment.created", "role_assignment.revoked"} {
		if !strings.Contains(trail.String(), `"action":"`+action+`"`) {
			t.Errorf("no %s in the trail", action)
		}
	}
	for _, c := range []string{cofounder.str("code"), third.str("code"), auditor.str("code")} {
		bare := strings.ReplaceAll(c, "-", "")
		for where, text := range map[string]string{"trail": trail.String(), "log": e.log.String()} {
			if strings.Contains(text, c) || strings.Contains(text, bare) {
				t.Errorf("an invitation code reached the %s (SEC-15)", where)
			}
		}
	}
}

// req: FR-INT-10
func TestCursorListsAndValueFormats(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	var codes []string
	cursor, pages := "", 0
	for {
		r := e.call(http.MethodGet, "/v1/accounts?page_size=1"+cursor, accountant, nil)
		if r.Status != 200 {
			t.Fatalf("listAccounts: %d %s", r.Status, r.Raw)
		}
		pages++
		for _, item := range r.Body["items"].([]any) {
			codes = append(codes, item.(map[string]any)["code"].(string))
		}
		next := r.str("next_cursor")
		if next == "" {
			break
		}
		cursor = "&cursor=" + next
	}
	if len(codes) != 4 || !slices.IsSorted(codes) || pages != 4 {
		t.Fatalf("pages of one: %v in %d pages", codes, pages)
	}
	if r := e.call(http.MethodGet, "/v1/accounts", accountant, nil); len(r.Body["items"].([]any)) != 4 || r.str("next_cursor") != "" {
		t.Fatalf("one page of the default size: %s", r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/accounts?page_size=501", accountant, nil), "listAccounts", 422, CodeInvalid)

	id, tag := e.createDraft("1234.50")
	r := e.call(http.MethodPost, "/v1/journals/"+id+"/post", accountant, nil, "If-Match", tag)
	line := r.Body["entry"].(map[string]any)["lines"].([]any)[0].(map[string]any)
	amount := line["amount"].(map[string]any)
	if amount["amount"] != "1234.50" || amount["currency"] != "SGD" || line["base_amount"].(map[string]any)["amount"] != "1234.50" || line["fx_rate"] != "1" {
		t.Errorf("money must be decimal strings with their currency at its minor units: %v", line)
	}
	if r.str("entry", "posting_date") != "2026-10-20" {
		t.Errorf("dates are ISO 8601: %q", r.str("entry", "posting_date"))
	}
	if _, err := time.Parse(time.RFC3339, r.str("entry", "recorded_at")); err != nil {
		t.Errorf("recorded_at: %v", err)
	}
	for _, v := range []string{r.str("journal", "id"), r.str("entry", "id")} {
		if u, err := uuid.Parse(v); err != nil || u.Version() != 7 {
			t.Errorf("IDs are UUIDv7 strings: %q", v)
		}
	}
	if !strings.Contains(r.Raw, `"amount":"1234.50"`) {
		t.Errorf("an amount came as a JSON number: %s", r.Raw)
	}
}

// req: SEC-02
// asvs: V8.2.1, V8.4.1, V16.3.2
func TestTheAPIRefusesWhatNoPolicyAllows(t *testing.T) {
	e := newEnv(t)
	other := newEnv(t)

	// An operation registered without a policy (around register) is refused before its handler runs.
	ran := false
	huma.Register(e.api.API, huma.Operation{OperationID: "plantedWithoutPolicy", Method: http.MethodGet, Path: "/v1/planted"},
		func(context.Context, *struct{}) (*struct{}, error) { ran = true; return nil, nil })
	r := e.call(http.MethodGet, "/v1/planted", e.as(authz.Owner), nil)
	if r.Status != 403 || r.problemCode() != CodeNoPolicy || ran {
		t.Fatalf("an operation without a policy: %d %s (handler ran: %v)", r.Status, r.Raw, ran)
	}

	// Another organisation's records do not exist for this caller (the database's RLS backs this up).
	theirs, theirTag := other.createDraft("10.00")
	mine := e.as(authz.Accountant, authz.Owner)
	e.problemIs(e.call(http.MethodGet, "/v1/journals/"+theirs, mine, nil), "getJournal", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+theirs+"/post", mine, nil, "If-Match", theirTag), "postJournal", 404, CodeNotFound)
	e.problemIs(e.call(http.MethodGet, "/v1/accounts/"+other.books.Bank.ID.String(), mine, nil), "getAccount", 404, CodeNotFound)
	if r := other.call(http.MethodGet, "/v1/journals/"+theirs, other.as(authz.Accountant), nil); r.str("status") != "draft" {
		t.Fatalf("the other organisation's draft changed: %s", r.Raw)
	}

	// A caller outside any organisation, and a role without the right, are refused and logged.
	nowhere := e.principal(authn.Principal{Subject: "super", Client: "web", Roles: []authz.Role{authz.SuperAdmin}})
	e.problemIs(e.call(http.MethodGet, "/v1/organisation", nowhere, nil), "getOrganisation", 403, CodeNoOrganisation)
	e.problemIs(e.call(http.MethodPost, "/v1/journals", e.as(authz.Owner), e.draft("1.00")), "createJournal", 403, CodeForbidden)
	if log := e.log.String(); !strings.Contains(log, `"msg":"api.refused"`) || !strings.Contains(log, `"operation":"createJournal"`) {
		t.Errorf("the refusal is not in the security log: %s", log)
	}

	// An allowed write records the caller as the actor (AUD-01).
	writer := authn.Principal{Subject: "accountant-7", Client: "web", Class: authn.ClassUser, Organisation: e.books.Org, Roles: []authz.Role{authz.Accountant}}
	created := e.call(http.MethodPost, "/v1/journals", e.principal(writer), e.draft("3.00"), "X-Request-Id", "chosen-by-the-caller")
	requestID := created.Header.Get("X-Request-Id")
	if u, err := uuid.Parse(requestID); err != nil || u.Version() != 7 || created.Status != http.StatusCreated {
		t.Fatalf("X-Request-Id = %q; want the API's own UUIDv7", requestID)
	}
	reader, err := postgres.Open(context.Background(), db.URL("fyniq_readonly")) // the API's role cannot read the trail
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var found bool
	err = reader.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 1000)
		for _, ev := range events {
			if strings.Contains(ev.Body, `"accountant-7"`) && strings.Contains(ev.Body, `"`+requestID+`"`) {
				found = true
			}
			if strings.Contains(ev.Body, "chosen-by-the-caller") {
				t.Error("the audit trail carries a request ID the caller chose")
			}
		}
		return err
	})
	if err != nil || !found {
		t.Errorf("no audit event names the API caller and the request (%v)", err)
	}
}

// The masked view (Appendix D's M) through the HTTP layer, on a planted capability: no P0 endpoint
// has an M cell yet.
// req: QA-SEC-03
// asvs: V8.2.3, V15.3.1
func TestMaskedFieldsThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	const planted authz.Capability = "Vendor bank details (planted)"
	authz.Grants[planted] = map[string]authz.Cell{"Auditor": {Rights: "M"}, "Accountant": {Rights: "C"}}
	defer delete(authz.Grants, planted)
	type out struct {
		Body struct {
			Name          string `json:"name"`
			AccountNumber string `json:"account_number"`
		}
	}
	register(e.api, authz.Policy{Operation: "getPlantedBankDetails", Capability: planted, Need: authz.View, MaskedFields: []string{"account_number"}},
		huma.Operation{OperationID: "getPlantedBankDetails", Method: http.MethodGet, Path: "/v1/planted-bank-details"}, nil,
		func(context.Context, *struct{}) (*out, error) {
			o := &out{}
			o.Body.Name, o.Body.AccountNumber = "Acme Supplies", "123-456789-001"
			return o, nil
		})
	if r := e.call(http.MethodGet, "/v1/planted-bank-details", e.as(authz.Auditor), nil); r.str("account_number") != MaskedValue || r.str("name") != "Acme Supplies" {
		t.Errorf("an Auditor (M) sees: %s", r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/planted-bank-details", e.as(authz.Accountant), nil); r.str("account_number") != "123-456789-001" {
		t.Errorf("an Accountant (C) sees: %s", r.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/planted-bank-details", e.as(authz.Employee), nil); r.Status != 403 {
		t.Errorf("an Employee (—): %d", r.Status)
	}
}
