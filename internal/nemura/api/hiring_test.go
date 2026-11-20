package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// Appendix B's row Hiring decides who may do what: the Recruiter, the HR administrator and the Owner ask and write (rev 0.69), the HR officer reads, the Owner alone approves (and may approve what they asked for); nobody else does anything, nobody without a session
// does anything, and everything is refused with the reason while Hiring is switched off (NM-REC-01, NM-FEAT-01).
// req: NM-REC-01, NM-REC-02, NM-REC-03, NM-FEAT-01
// asvs: V8.2.1, V8.2.2, V8.3.1
func TestHiringOperationsFollowAppendixBsHiringRow(t *testing.T) {
	const id = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3a01"
	type op struct{ method, path, body string }
	reads := []op{{"GET", "/v1/requisitions", ""}, {"GET", "/v1/careers-page", ""}, {"POST", "/v1/fair-wording-check", `{"text":"We want a young digital native."}`}}
	writes := []op{{"POST", "/v1/requisitions", `{"title":"Data scientist","department":"Engineering","reason":"new","salary_min":"6500.00","salary_max":"8200.00","target_start":"2027-02-01"}`},
		{"PUT", "/v1/requisitions/" + id + "/job-post", `{"title":"Data scientist","body":"You will analyse data."}`}, {"POST", "/v1/requisitions/" + id + "/job-post/publication", `{}`},
		{"POST", "/v1/requisitions/" + id + "/job-post/advert", `{"posted_on":"2026-10-05"}`}, {"POST", "/v1/requisitions/" + id + "/job-post/advert/renewal", `{"change":"salary","posted_on":"2026-11-02"}`}, {"PUT", "/v1/careers-page", `{"slug":"acme-careers","enabled":true,"company":"Acme"}`},
		{"POST", "/v1/requisitions/" + id + "/candidates", `{"name":"Chen Yu","email":"chen.yu@example.com"}`}, {"POST", "/v1/candidates/" + id + "/moves", `{"to":"screening"}`}, {"POST", "/v1/candidates/" + id + "/notes", `{"body":"Strong."}`},
		{"POST", "/v1/candidates/" + id + "/interviewers", `{"member_id":"` + id + `"}`}, {"POST", "/v1/candidates/" + id + "/future-consent", `{"given":true}`}, {"POST", "/v1/candidates/" + id + "/pass-application", `{"on":"2027-01-15"}`},
		{"PUT", "/v1/requisitions/" + id + "/interview-kit", `{"questions":["Why us?"],"criteria":["Skill"]}`},
		{"POST", "/v1/requisitions/" + id + "/job-post/exemption", `{"kind":"transferee","note":"Intra-company."}`},
		{"POST", "/v1/requisitions/" + id + "/job-post/closing", ``}}
	seen := []op{{"GET", "/v1/requisitions/" + id + "/candidates", ""}, {"GET", "/v1/candidates/" + id, ""}, {"GET", "/v1/requisitions/" + id + "/interview-kit", ""}} // the team and an interviewer
	scores := []op{{"POST", "/v1/candidates/" + id + "/scores", `{"criterion":"Skill","score":4}`}}                                                                    // an interviewer alone
	hires := []op{{"POST", "/v1/candidates/" + id + "/hire", `{"start_date":"2027-02-01","employment_type":"full_time"}`}}                                             // the HR administrator and the Owner alone
	approves := []op{{"POST", "/v1/requisitions/" + id + "/decision", `{"approve":true}`}}
	call := func(o op, cookie string) int {
		_, mux := testAPI(t)
		req := httptest.NewRequest(o.method, o.path, strings.NewReader(o.body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	ok := func(c int) bool { return c == 200 || c == 201 || c == 204 }
	defer func() { testRoles, featuresOff = []authz.Role{authz.HRAdmin}, false }()
	for _, o := range append(append(append(append(append(reads, writes...), approves...), seen...), scores...), hires...) {
		if code := call(o, ""); code != 401 {
			t.Errorf("%s %s with no session: %d", o.method, o.path, code)
		}
	}
	for name, tc := range map[string]struct {
		role                   authz.Role
		read, write, approving bool
	}{
		"a Recruiter":             {authz.Recruiter, true, true, false},
		"an HR officer":           {authz.HROfficer, true, false, false},
		"an HR administrator":     {authz.HRAdmin, true, true, false},
		"the Owner":               {authz.Owner, true, true, true},
		"an employee":             {authz.Employee, false, false, false},
		"a line manager":          {authz.LineManager, false, false, false},
		"an interviewer":          {authz.Interviewer, false, false, false},
		"a payroll administrator": {authz.PayrollAdmin, false, false, false},
		"an administrator":        {authz.Administrator, false, false, false},
	} {
		testRoles = []authz.Role{tc.role}
		for kind, set := range map[string]struct {
			ops  []op
			want bool
		}{"read": {reads, tc.read}, "write": {writes, tc.write}, "approve": {approves, tc.approving}, "seen": {seen, tc.read || tc.role == authz.Interviewer}, "score": {scores, tc.role == authz.Interviewer}, "hire": {hires, tc.role == authz.HRAdmin || tc.role == authz.Owner}} {
			for _, o := range set.ops {
				if got := ok(call(o, "nemura_session=good-token")); got != set.want {
					t.Errorf("%s: %s %s %s: allowed %v, want %v", name, kind, o.method, o.path, got, set.want)
				}
			}
		}
	}
	testRoles, featuresOff = []authz.Role{authz.Owner}, true
	if code := call(reads[0], "nemura_session=good-token"); code != 409 {
		t.Errorf("Hiring off: %d", code)
	}
}

// The careers page is read by anyone with no sign-in (and is the one hiring route that is), counted against a limit; everything else about it follows the Hiring row (NM-REC-02).
// req: NM-REC-02
// asvs: V4.2.1, V2.1.1
func TestTheCareersPageIsPublicAndSignedOutVisitorsReadNothingElseOfHiring(t *testing.T) {
	_, mux := testAPI(t)
	for path, want := range map[string]int{"/v1/careers/acme-careers": 200, "/v1/careers-page": 401, "/v1/requisitions": 401, "/v1/careers/Bad_Slug": 422, "/v1/careers/ab": 422} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("GET %s: %d, want %d", path, rec.Code, want)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/careers/acme-careers/applications", strings.NewReader(`{"post_id":"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d3b01","name":"Chen Yu","email":"chen.yu@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Errorf("an application with no sign-in: %d %s", rec.Code, rec.Body.String())
	}
}

// Only an Owner or an HR administrator lists and reviews the reports of records due for deletion; nobody signed out and no other role does (NM-SEC-05).
// req: NM-SEC-05
// asvs: V8.2.1, V8.2.2
func TestOnlyAnOwnerOrAnHRAdministratorSeesAndReviewsDeletionReports(t *testing.T) {
	const id = "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d6a01"
	defer func() { testRoles = []authz.Role{authz.HRAdmin} }()
	call := func(method, path, cookie string) int {
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, nil)
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, o := range [][2]string{{"GET", "/v1/deletion-reports"}, {"POST", "/v1/deletion-reports/" + id + "/review"}} {
		if code := call(o[0], o[1], ""); code != 401 {
			t.Errorf("%s %s signed out: %d", o[0], o[1], code)
		}
		for role, want := range map[authz.Role]bool{authz.Owner: true, authz.HRAdmin: true, authz.HROfficer: false, authz.Recruiter: false, authz.Employee: false, authz.Interviewer: false, authz.PayrollAdmin: false, authz.Administrator: false} {
			testRoles = []authz.Role{role}
			code := call(o[0], o[1], "nemura_session=good-token")
			if allowed := code == 200 || code == 409; allowed != want { // the fake answers its configured error for a review: an allowed role is not refused with 401 or 403
				t.Errorf("%s %s as %s: %d, allowed %v, want %v", o[0], o[1], role, code, allowed, want)
			}
		}
	}
}
