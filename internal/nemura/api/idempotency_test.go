package api

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// memIdem is the table of keys in memory, with the semantics of the database's: one claim wins, an answer is kept, a key expires.
type memIdem struct {
	mu   sync.Mutex
	rows map[string]*memRow
}

type memRow struct {
	hash    []byte
	done    bool
	rec     IdemRecord
	created time.Time
}

var testIdem *memIdem

func idemFor() Idempotency {
	if testIdem == nil {
		return nil
	}
	return testIdem
}

func newMemIdem() *memIdem { return &memIdem{rows: map[string]*memRow{}} }

func idemHash(method, uri, body string) []byte {
	h := sha256.New()
	h.Write([]byte(method + "\n" + uri + "\n" + body))
	return h.Sum(nil)
}

func (m *memIdem) seed(key string, hash []byte, rec IdemRecord) {
	m.rows[key] = &memRow{hash: hash, done: true, rec: rec, created: testNow}
}

func (m *memIdem) Claim(_ context.Context, _, _ uuid.UUID, key string, hash []byte, now time.Time) (IdemClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rows[key]; ok && !now.Before(r.created.Add(IdempotencyWindow)) {
		delete(m.rows, key)
	}
	r, ok := m.rows[key]
	switch {
	case !ok:
		m.rows[key] = &memRow{hash: hash, created: now}
		return IdemClaim{Owned: true}, nil
	case string(r.hash) != string(hash):
		return IdemClaim{Mismatch: true}, nil
	case !r.done:
		return IdemClaim{InFlight: true}, nil
	}
	return IdemClaim{Rec: r.rec}, nil
}

func (m *memIdem) Finish(_ context.Context, _, _ uuid.UUID, key string, rec IdemRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[key].done, m.rows[key].rec = true, rec
	return nil
}

func (m *memIdem) Release(_ context.Context, _, _ uuid.UUID, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, key)
	return nil
}

func send(mux *http.ServeMux, method, path, body, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "nemura_session=good-token")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// A write sent twice with one key runs once: the repeat returns the first answer with Idempotent-Replayed; the same key with another body is 422; a request with no key runs every time; two keys are two
// requests; and a key expires after 24 hours (golden rule 4, NM-ARC-03).
// req: NM-ARC-03
// asvs: V11.1.4, V8.2.1
func TestAWriteWithAKeyRunsOnceAndAReplayReturnsTheFirstAnswer(t *testing.T) {
	var runs atomic.Int32
	testPeopleCreates = &runs
	defer func() { testPeopleCreates = nil }()
	testIdem = newMemIdem()
	defer func() { testIdem = nil }()
	_, mux := testAPI(t)
	first := send(mux, "POST", "/v1/people", personBody, "key-1")
	if first.Code != http.StatusCreated || first.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	again := send(mux, "POST", "/v1/people", personBody, "key-1")
	if again.Code != first.Code || again.Body.String() != first.Body.String() || again.Header().Get("Idempotent-Replayed") != "true" || runs.Load() != 1 {
		t.Errorf("replay: %d %q replayed=%q runs=%d (first %q)", again.Code, again.Body.String(), again.Header().Get("Idempotent-Replayed"), runs.Load(), first.Body.String())
	}
	if other := send(mux, "POST", "/v1/people", strings.Replace(personBody, "Ana", "Eve", 1), "key-1"); other.Code != http.StatusUnprocessableEntity || !strings.Contains(other.Body.String(), CodeIdempotencyMismatch) || runs.Load() != 1 {
		t.Errorf("the same key with another body: %d %s runs=%d", other.Code, other.Body.String(), runs.Load())
	}
	send(mux, "POST", "/v1/people", personBody, "")
	send(mux, "POST", "/v1/people", personBody, "")
	if runs.Load() != 3 {
		t.Errorf("two requests with no key: runs=%d", runs.Load())
	}
	send(mux, "POST", "/v1/people", personBody, "key-2")
	if runs.Load() != 4 {
		t.Errorf("a new key: runs=%d", runs.Load())
	}
	if bad := send(mux, "POST", "/v1/people", personBody, "has a space"); bad.Code != http.StatusUnprocessableEntity || !strings.Contains(bad.Body.String(), "Idempotency-Key") {
		t.Errorf("a key with a space: %d %s", bad.Code, bad.Body.String())
	}
	// A key expires after 24 hours: the same key runs again.
	for _, r := range testIdem.rows {
		r.created = r.created.Add(-IdempotencyWindow - time.Second)
	}
	send(mux, "POST", "/v1/people", personBody, "key-1")
	if runs.Load() != 5 {
		t.Errorf("an expired key: runs=%d", runs.Load())
	}
	// No session: the operation answers 401 as it would without a key.
	req := httptest.NewRequest("POST", "/v1/people", strings.NewReader(personBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "key-3")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", rec.Code)
	}
}

// Two identical requests in flight at once run once: the second waits for the first and replays its answer (golden rule 4).
// req: NM-ARC-03
// asvs: V11.1.4
func TestTwoIdenticalRequestsInFlightRunOnce(t *testing.T) {
	var runs atomic.Int32
	testPeopleCreates = &runs
	testPeopleDelay = 150 * time.Millisecond
	defer func() { testPeopleCreates, testPeopleDelay = nil, 0 }()
	testIdem = newMemIdem()
	defer func() { testIdem = nil }()
	_, mux := testAPI(t)
	var wg sync.WaitGroup
	out := make([]*httptest.ResponseRecorder, 6)
	for i := range out {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = send(mux, "POST", "/v1/people", personBody, "same-key")
		}()
	}
	wg.Wait()
	replayed := 0
	for _, r := range out {
		if r.Code != http.StatusCreated || r.Body.String() != out[0].Body.String() {
			t.Errorf("an answer: %d %s", r.Code, r.Body.String())
		}
		if r.Header().Get("Idempotent-Replayed") == "true" {
			replayed++
		}
	}
	if runs.Load() != 1 || replayed != 5 {
		t.Errorf("six identical requests: ran %d times, %d replayed", runs.Load(), replayed)
	}
}

// What may carry pay, a bank account, an NRIC or a question is never stored: a replay of such a write that returned a body is refused with 409 and runs nothing again; one that returned no body replays by
// its status (NM-AI-06, NM-SEC-02).
// req: NM-ARC-03, NM-SEC-02
// asvs: V8.3.4, V11.1.4
func TestTheAnswerOfASensitiveWriteIsNeverStored(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		want    bool
	}{
		{"POST /v1/payroll/runs", true}, {"POST /v1/people/{id}/pay", true}, {"PUT /v1/people/{id}/nric", true}, {"POST /v1/people/{id}/bank-account-changes", true}, {"POST /v1/assistant/ask", true},
		{"POST /v1/me/questions", true}, {"POST /v1/people/{id}/tax-hold", true}, {"POST /v1/payroll/runs/{id}/exports", true}, {"PUT /v1/me/details", true},
		{"POST /v1/people", false}, {"POST /v1/leave-requests/{id}/decision", false}, {"PUT /v1/features/{feature}", false},
	} {
		if got := sensitiveOperation(tc.pattern); got != tc.want {
			t.Errorf("%s: sensitive %v", tc.pattern, got)
		}
	}
	testIdem = newMemIdem()
	defer func() { testIdem = nil }()
	_, mux := testAPI(t)
	first := send(mux, "POST", "/v1/payroll/groups", `{"name":"Monthly","pay_day":25,"cutoff_days":7,"approval_days":3}`, "pay-key")
	if first.Code >= 500 {
		t.Fatalf("the first: %d %s", first.Code, first.Body.String())
	}
	for _, r := range testIdem.rows {
		if len(r.rec.Body) != 0 || r.rec.BodyKept {
			t.Error("the answer of a pay operation was stored")
		}
	}
}
