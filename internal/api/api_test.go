//go:build local

package api

import (
	"context"
	"net/http"
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
func TestETagAndIfMatch(t *testing.T) {
	e := newEnv(t)
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
