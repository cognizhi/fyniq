package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

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
