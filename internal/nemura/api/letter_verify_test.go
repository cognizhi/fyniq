package api

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// verifyLimit is the test double's counter: the real limit is counted in the database (TestThePublicLimitIsSharedByEveryInstance in the adapter).
type verifyLimit struct {
	mu   sync.Mutex
	hits map[string]int
}

func (l *verifyLimit) hit(scope, key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string]int{}
	}
	k := scope + "|" + strings.ToUpper(key)
	l.hits[k]++
	if scope != "letter-ref" && scope != "letter-file" && scope != "letter-token" {
		return l.hits[k] <= 300
	}
	return l.hits[k] <= limit
}

var verifyLimiter verifyLimit

// testDisclosure is the disclosure link the test double knows: an organisation, a dot and 43 plain characters.
var testDisclosure = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e11." + strings.Repeat("x", 43)

// The look-up needs no sign-in and shows only the letter's status, type, day and company; a wrong code answers found false like an unknown reference; it is limited to ten tries for a reference an hour;
// the withdrawal needs a session.
// part of NM-LTR-05
// asvs: V2.3.1, V8.2.1, V13.2.1
func TestTheLetterLookUpIsPublicRateLimitedAndShowsOnlyItsOwnFacts(t *testing.T) {
	verifyLimiter = verifyLimit{}
	defer func() { verifyLimiter = verifyLimit{} }()
	_, mux := testAPI(t)
	call := func(method, path, body, cookie string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	code, body := call("POST", "/v1/letter-verification", `{"reference":"EC-2026-0001","code":"K7QX-2M4D-9WTB-HP3R"}`, "")
	if code != 200 || !strings.Contains(body, `"found":true`) || !strings.Contains(body, `"status":"valid"`) || !strings.Contains(body, "Acme") || strings.Contains(body, "person") {
		t.Errorf("a valid letter: %d %s", code, body)
	}
	code, body = call("POST", "/v1/letter-verification", `{"reference":"EC-2026-0001","code":"AAAA-AAAA-AAAA-AAAA"}`, "")
	if code != 200 || !strings.Contains(body, `"found":false`) || strings.Contains(body, "Acme") {
		t.Errorf("a wrong code: %d %s", code, body)
	}
	for i := 0; i < 8; i++ { // ten in all so far with the two above
		call("POST", "/v1/letter-verification", `{"reference":"EC-2026-0001","code":"AAAA-AAAA-AAAA-AAAA"}`, "")
	}
	if code, _ := call("POST", "/v1/letter-verification", `{"reference":"EC-2026-0001","code":"K7QX-2M4D-9WTB-HP3R"}`, ""); code != 429 {
		t.Errorf("the eleventh try for one reference: %d", code)
	}
	if code, _ := call("POST", "/v1/letter-verification", `{"reference":"EC-2026-0002","code":"AAAA-AAAA-AAAA-AAAA"}`, ""); code != 200 {
		t.Errorf("another reference is not limited by the first: %d", code)
	}
	if code, _ := call("POST", "/v1/me/letters/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71/verification/withdrawal", ``, ""); code != 401 {
		t.Errorf("withdrawing with no session: %d", code)
	}
	if code, _ := call("POST", "/v1/me/letters/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71/verification/withdrawal", ``, "nemura_session=good-token"); code != 200 {
		t.Errorf("withdrawing as the person: %d", code)
	}
}

// A document is looked up by its hash with no sign-in and a disclosure link is read with no sign-in, both answering found false alike for what they do not know; making, listing and revoking a link needs a
// session; a limit counted across instances refuses the look-up when it is spent, whichever instance counts.
// part of NM-LTR-05
// asvs: V2.3.1, V8.2.1, V13.2.1
func TestADocumentAndADisclosureLinkAreLookedUpWithNoSessionAndLimited(t *testing.T) {
	verifyLimiter = verifyLimit{}
	defer func() { verifyLimiter = verifyLimit{} }()
	_, mux := testAPI(t)
	call := func(method, path, body, cookie string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	file := `{"sha256":"` + strings.Repeat("ab", 32) + `"}`
	if code, body := call("POST", "/v1/letter-verification/file", file, ""); code != 200 || !strings.Contains(body, `"found":true`) || strings.Contains(body, "person") {
		t.Errorf("a known document: %d %s", code, body)
	}
	if code, body := call("POST", "/v1/letter-verification/file", `{"sha256":"`+strings.Repeat("cd", 32)+`"}`, ""); code != 200 || !strings.Contains(body, `"found":false`) {
		t.Errorf("an unknown document: %d %s", code, body)
	}
	if code, _ := call("POST", "/v1/letter-verification/file", `{"sha256":"abc"}`, ""); code != 422 {
		t.Errorf("a malformed hash: %d", code)
	}
	live := `{"token":"` + testDisclosure + `"}`
	if code, body := call("POST", "/v1/letter-disclosure", live, ""); code != 200 || !strings.Contains(body, `"found":true`) || !strings.Contains(body, "Job title") {
		t.Errorf("a live link: %d %s", code, body)
	}
	if code, body := call("POST", "/v1/letter-disclosure", `{"token":"`+testDisclosure+`x"}`, ""); code != 200 || !strings.Contains(body, `"found":false`) || strings.Contains(body, "Job title") {
		t.Errorf("an unknown link: %d %s", code, body)
	}
	const id = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71"
	for _, c := range [][3]string{{"POST", "/v1/me/letters/" + id + "/disclosure-links", `{}`}, {"GET", "/v1/me/letters/" + id + "/disclosure-links", ``}, {"POST", "/v1/me/letter-disclosure-links/" + id + "/revocation", ``}} {
		if code, _ := call(c[0], c[1], c[2], ""); code != 401 {
			t.Errorf("%s %s with no session: %d", c[0], c[1], code)
		}
		if code, _ := call(c[0], c[1], c[2], "nemura_session=good-token"); code != 200 && code != 201 {
			t.Errorf("%s %s as the person: %d", c[0], c[1], code)
		}
	}
	for i := 0; i < 30; i++ { // the thirtieth is the last for one document in an hour
		call("POST", "/v1/letter-verification/file", file, "")
	}
	if code, _ := call("POST", "/v1/letter-verification/file", file, ""); code != 429 {
		t.Errorf("a document past its limit: %d", code)
	}
	for i := 0; i < 300; i++ { // 300 in all in a minute: the first of them were counted above too
		call("POST", "/v1/letter-disclosure", live, "")
	}
	if code, _ := call("POST", "/v1/letter-verification", `{"reference":"EC-2026-0009","code":"AAAA-AAAA-AAAA-AAAA"}`, ""); code != 429 {
		t.Errorf("any look-up once the limit for all is spent: %d", code)
	}
}
