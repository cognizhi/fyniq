package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// Leave requests are read and decided by scope: everyone has their own, a line manager their reports' and decides on them, the people team and the Owner decide on
// anyone's, Payroll reads anyone's and decides none, and an employee decides nothing; deciding one's own request is refused (NM-LV-04, NM-LV-05).
// req: NM-LV-04, NM-LV-05
// asvs: V8.2.1, V8.3.1
func TestLeaveRequestsAreReadAndDecidedByScope(t *testing.T) {
	var got LeaveScope
	requestsScope = &got
	defer func() { requestsScope = nil }()
	do := func(roles []authz.Role, method, path, body string) int {
		testRoles = roles
		defer func() { testRoles = []authz.Role{authz.HRAdmin} }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	id := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e80"
	for _, tc := range []struct {
		roles   []authz.Role
		scope   LeaveScope
		decide  int
		settle  int
		message string
	}{
		{[]authz.Role{authz.Employee}, LeaveScope{}, 403, 403, "an employee reads their own and decides nothing"},
		{[]authz.Role{authz.LineManager}, LeaveScope{Reports: true, Decide: true}, 204, 403, "a manager decides on their reports"},
		{[]authz.Role{authz.HROfficer}, LeaveScope{All: true, Decide: true}, 204, 200, "the HR officer decides on anyone's"},
		{[]authz.Role{authz.HRAdmin}, LeaveScope{All: true, Decide: true}, 204, 200, "the HR administrator likewise"},
		{[]authz.Role{authz.PayrollAdmin}, LeaveScope{All: true}, 403, 403, "Payroll reads and decides none"},
		{[]authz.Role{authz.Owner}, LeaveScope{All: true, Decide: true}, 204, 403, "the Owner decides but settling is the people team's"},
		{[]authz.Role{authz.Administrator}, LeaveScope{}, 403, 403, "the Administrator has no leave rights"},
	} {
		if got = (LeaveScope{}); do(tc.roles, "GET", "/v1/leave-requests", "") != 200 || got != tc.scope {
			t.Errorf("%s: scope %+v, want %+v", tc.message, got, tc.scope)
		}
		if code := do(tc.roles, "POST", "/v1/leave-requests/"+id+"/decision", `{"decision":"approve"}`); code != tc.decide {
			t.Errorf("%s: deciding %d, want %d", tc.message, code, tc.decide)
		}
		if code := do(tc.roles, "POST", "/v1/leave/settlement", `{}`); code != tc.settle {
			t.Errorf("%s: settling %d, want %d", tc.message, code, tc.settle)
		}
	}
	// Every signed-in person asks for leave for themselves and cancels their own.
	for _, role := range []authz.Role{authz.Employee, authz.PayrollAdmin, authz.Owner} {
		if do([]authz.Role{role}, "POST", "/v1/me/leave-requests", `{"type":"annual","from":"2026-04-06","to":"2026-04-10"}`) != 201 {
			t.Errorf("%s could not ask for leave", role)
		}
		if do([]authz.Role{role}, "POST", "/v1/leave-requests/"+id+"/cancellation", "") != 204 {
			t.Errorf("%s could not cancel", role)
		}
	}
}

// Sick leave is told by the person, who gives their own certificate; the HR officer and the HR administrator verify it and may give one for anyone; nobody else, the
// Owner and a line manager included, verifies; and everyone signed in sees the calendar, which keeps the type from those who may not see it (NM-LV-06, NM-LV-12).
// req: NM-LV-06, NM-LV-12, NM-LV-05
// asvs: V8.2.1
func TestSickLeaveIsToldByThePersonAndVerifiedByThePeopleTeam(t *testing.T) {
	do := func(roles []authz.Role, method, path, body string) int {
		testRoles = roles
		defer func() { testRoles = []authz.Role{authz.HRAdmin} }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	id := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90"
	for _, tc := range []struct {
		roles  []authz.Role
		verify int
	}{
		{[]authz.Role{authz.Employee}, 403}, {[]authz.Role{authz.LineManager}, 403}, {[]authz.Role{authz.PayrollAdmin}, 403}, {[]authz.Role{authz.Owner}, 403},
		{[]authz.Role{authz.Administrator}, 403}, {[]authz.Role{authz.HROfficer}, 204}, {[]authz.Role{authz.HRAdmin}, 204},
	} {
		if got := do(tc.roles, "POST", "/v1/sick-leave/"+id+"/verification", ""); got != tc.verify {
			t.Errorf("%v verifying: %d, want %d", tc.roles, got, tc.verify)
		}
		if got := do(tc.roles, "POST", "/v1/me/sick-leave", `{"kind":"outpatient","from":"2026-05-04","to":"2026-05-06"}`); got != 201 {
			t.Errorf("%v could not notify: %d", tc.roles, got)
		}
		if got := do(tc.roles, "GET", "/v1/sick-leave", ""); got != 200 {
			t.Errorf("%v could not list their own: %d", tc.roles, got)
		}
		if got := do(tc.roles, "GET", "/v1/leave-calendar?month=2026-05", ""); got != 200 {
			t.Errorf("%v could not read the calendar: %d", tc.roles, got)
		}
		if got := do(tc.roles, "POST", "/v1/sick-leave/"+id+"/certificate", `{"issuer":"Clinic","content_type":"image/jpeg","content_base64":"/9j/4AAQ"}`); got != 201 {
			t.Errorf("%v could not give a certificate: %d", tc.roles, got)
		}
	}
	if got := do([]authz.Role{authz.Employee}, "POST", "/v1/sick-leave/"+id+"/certificate", `{"issuer":"Clinic","content_type":"image/jpeg","content_base64":"not base64!"}`); got != 422 {
		t.Errorf("a certificate that is not base64: %d", got)
	}
	if got := do([]authz.Role{authz.Employee}, "GET", "/v1/leave-calendar?month=May", ""); got != 422 {
		t.Errorf("a month that is not a month: %d", got)
	}
}

// Government-paid leave is recorded and its claims tracked by the HR officer and the HR administrator only; the people team, the Owner and Payroll read it; nobody else
// does, a line manager included (NM-LV-07, NM-LV-14).
// req: NM-LV-07, NM-LV-14
// asvs: V8.2.1
func TestGovernmentPaidLeaveIsWrittenByHRAndReadByThePeopleTeamOwnerAndPayroll(t *testing.T) {
	do := func(roles []authz.Role, method, path, body string) int {
		testRoles = roles
		defer func() { testRoles = []authz.Role{authz.HRAdmin} }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	person := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"
	block := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e94"
	record := `{"scheme":"paternity","dependant_id":"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e71","child_order":1,"from":"2026-06-10","to":"2026-07-07"}`
	for _, tc := range []struct {
		roles       []authz.Role
		read, write int
	}{
		{[]authz.Role{authz.Employee}, 403, 403}, {[]authz.Role{authz.LineManager}, 403, 403}, {[]authz.Role{authz.Administrator}, 403, 403},
		{[]authz.Role{authz.PayrollAdmin}, 200, 403}, {[]authz.Role{authz.Owner}, 200, 403},
		{[]authz.Role{authz.HROfficer}, 200, 201}, {[]authz.Role{authz.HRAdmin}, 200, 201},
	} {
		if got := do(tc.roles, "GET", "/v1/government-paid-leave", ""); got != tc.read {
			t.Errorf("%v reading: %d, want %d", tc.roles, got, tc.read)
		}
		if got := do(tc.roles, "POST", "/v1/people/"+person+"/government-paid-leave", record); got != tc.write {
			t.Errorf("%v recording: %d, want %d", tc.roles, got, tc.write)
		}
		if got := do(tc.roles, "POST", "/v1/government-paid-leave/"+block+"/claims", `{"amount":"6000.00","filed_on":"2026-07-20","reference":"GPL-001"}`); got != tc.write {
			t.Errorf("%v filing a claim: %d, want %d", tc.roles, got, tc.write)
		}
		if got := do(tc.roles, "POST", "/v1/government-paid-claims/"+block+"/events", `{"kind":"receipt","amount":"2500.00","on":"2026-08-10"}`); got != tc.write {
			t.Errorf("%v recording a receipt: %d, want %d", tc.roles, got, tc.write)
		}
	}
	if got := do([]authz.Role{authz.HRAdmin}, "POST", "/v1/government-paid-leave/"+block+"/claims", `{"amount":"6000.00","filed_on":"yesterday"}`); got != 422 {
		t.Errorf("a claim with a day that is not a date: %d", got)
	}
}

// Flexible work requests are made by the person for themselves, answered in writing by the HR officer, the HR administrator or the Owner, and read by the person and
// the people team only (NM-LV-10).
// req: NM-LV-10
// asvs: V8.2.1
func TestFlexibleWorkRequestsAreMadeByThePersonAndAnsweredByThePeopleTeam(t *testing.T) {
	do := func(roles []authz.Role, method, path, body string) int {
		testRoles = roles
		defer func() { testRoles = []authz.Role{authz.HRAdmin} }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	id := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea0"
	for _, tc := range []struct {
		roles []authz.Role
		reply int
	}{
		{[]authz.Role{authz.Employee}, 403}, {[]authz.Role{authz.LineManager}, 403}, {[]authz.Role{authz.PayrollAdmin}, 403}, {[]authz.Role{authz.Administrator}, 403},
		{[]authz.Role{authz.HROfficer}, 204}, {[]authz.Role{authz.HRAdmin}, 204}, {[]authz.Role{authz.Owner}, 204},
	} {
		if got := do(tc.roles, "POST", "/v1/me/flexible-work-requests", `{"arrangement":"flexi_time","from":"2027-01-04","reason":"School run."}`); got != 201 {
			t.Errorf("%v could not ask: %d", tc.roles, got)
		}
		if got := do(tc.roles, "GET", "/v1/flexible-work-requests", ""); got != 200 {
			t.Errorf("%v could not list: %d", tc.roles, got)
		}
		if got := do(tc.roles, "POST", "/v1/flexible-work-requests/"+id+"/reply", `{"outcome":"accepted","written_reply":"Approved from January."}`); got != tc.reply {
			t.Errorf("%v replying: %d, want %d", tc.roles, got, tc.reply)
		}
		if got := do(tc.roles, "POST", "/v1/flexible-work-requests/"+id+"/withdrawal", ""); got != 204 {
			t.Errorf("%v could not withdraw their own: %d", tc.roles, got)
		}
	}
}

// Hours worked on special days are recorded by the person, decided by their manager or the people team (never their own), and what leave and time give a pay run is read
// by the people team, the Owner and Payroll only; the decision on unpaid sick days and the encashment of leave are the HR officer's and administrator's (NM-TIM-04, NM-LV-08).
// req: NM-TIM-04, NM-LV-08
// asvs: V8.2.1
func TestHoursWorkedAndPayInputsAreScopedByRole(t *testing.T) {
	do := func(roles []authz.Role, method, path, body string) int {
		testRoles = roles
		defer func() { testRoles = []authz.Role{authz.HRAdmin} }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	person := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"
	rec := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1eb0"
	sick := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90"
	for _, tc := range []struct {
		roles                 []authz.Role
		decide, inputs, write int
	}{
		{[]authz.Role{authz.Employee}, 403, 403, 403}, {[]authz.Role{authz.LineManager}, 204, 403, 403}, {[]authz.Role{authz.Administrator}, 403, 403, 403},
		{[]authz.Role{authz.PayrollAdmin}, 403, 200, 403}, {[]authz.Role{authz.Owner}, 204, 200, 403},
		{[]authz.Role{authz.HROfficer}, 204, 200, 200}, {[]authz.Role{authz.HRAdmin}, 204, 200, 200},
	} {
		if got := do(tc.roles, "POST", "/v1/me/overtime", `{"date":"2026-09-01","kind":"overtime","hours":"2.50"}`); got != 201 {
			t.Errorf("%v could not record: %d", tc.roles, got)
		}
		if got := do(tc.roles, "GET", "/v1/overtime", ""); got != 200 {
			t.Errorf("%v could not list: %d", tc.roles, got)
		}
		if got := do(tc.roles, "POST", "/v1/overtime/"+rec+"/decision", `{"decision":"approve"}`); got != tc.decide {
			t.Errorf("%v deciding: %d, want %d", tc.roles, got, tc.decide)
		}
		if got := do(tc.roles, "GET", "/v1/pay-inputs?to=2026-12-31", ""); got != tc.inputs {
			t.Errorf("%v reading the inputs: %d, want %d", tc.roles, got, tc.inputs)
		}
		want := 201
		if tc.write == 403 {
			want = 403
		}
		if got := do(tc.roles, "POST", "/v1/people/"+person+"/leave/2026/encashment", `{"days":"2.00","on":"2026-11-30"}`); got != want {
			t.Errorf("%v encashing: %d, want %d", tc.roles, got, want)
		}
		if got := do(tc.roles, "POST", "/v1/sick-leave/"+sick+"/unpaid-decision", `{"treatment":"unpaid"}`); got != tc.write {
			t.Errorf("%v deciding unpaid days: %d, want %d", tc.roles, got, tc.write)
		}
	}
}

// The accrual is drafted by the HR officer and administrator, read by the people team, the Owner and Payroll, and approved by the people team and the Owner (never the draft
// they made); availability is the person's, their manager's and the people team's (NM-LV-09, NM-TIM-01).
// req: NM-LV-09, NM-TIM-01
// asvs: V8.2.1
func TestTheAccrualAndAvailabilityAreScopedByRole(t *testing.T) {
	do := func(roles []authz.Role, method, path, body string) int {
		testRoles = roles
		defer func() { testRoles = []authz.Role{authz.HRAdmin} }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	month := `{"month":"2026-05"}`
	for _, tc := range []struct {
		roles                []authz.Role
		draft, read, approve int
	}{
		{[]authz.Role{authz.Employee}, 403, 403, 403}, {[]authz.Role{authz.LineManager}, 403, 403, 403}, {[]authz.Role{authz.Administrator}, 403, 403, 403},
		{[]authz.Role{authz.PayrollAdmin}, 403, 200, 403}, {[]authz.Role{authz.Owner}, 403, 200, 200},
		{[]authz.Role{authz.HROfficer}, 201, 200, 200}, {[]authz.Role{authz.HRAdmin}, 201, 200, 200},
	} {
		if got := do(tc.roles, "POST", "/v1/leave/accruals", month); got != tc.draft {
			t.Errorf("%v drafting: %d, want %d", tc.roles, got, tc.draft)
		}
		if got := do(tc.roles, "GET", "/v1/leave/accruals?month=2026-05", ""); got != tc.read {
			t.Errorf("%v reading: %d, want %d", tc.roles, got, tc.read)
		}
		if got := do(tc.roles, "POST", "/v1/leave/accruals/approval", month); got != tc.approve {
			t.Errorf("%v approving: %d, want %d", tc.roles, got, tc.approve)
		}
	}
	if got := do([]authz.Role{authz.HRAdmin}, "POST", "/v1/leave/accruals", `{"month":"May"}`); got != 422 {
		t.Errorf("a month that is not a month: %d", got)
	}
	if got := do([]authz.Role{authz.HRAdmin}, "GET", "/v1/people/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61/availability?from=2026-04-01&to=2026-04-30", ""); got != 200 {
		t.Errorf("availability for the people team: %d", got)
	}
	if got := do([]authz.Role{authz.HRAdmin}, "GET", "/v1/people/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61/availability?from=April", ""); got != 422 {
		t.Errorf("a bad day: %d", got)
	}
}

// Letters are asked for by the person, checked by the people team (never their own), signed only by the signatory after a fresh passkey confirmation, and read by the person
// and the people team; the people team writes templates, the HR administrator and the Owner approve them and set the letterhead and the types; a delivery link needs no
// sign-in (NM-LTR-01, NM-LTR-02, NM-LTR-03).
// req: NM-LTR-01, NM-LTR-02, NM-LTR-03
// asvs: V8.2.1, V8.3.1
func TestLettersAreScopedByRoleAndSigningNeedsAFreshPasskey(t *testing.T) {
	do := func(roles []authz.Role, stepUp bool, method, path, body string, signedIn bool) int {
		testRoles, testStepUp = roles, stepUp
		defer func() { testRoles, testStepUp = []authz.Role{authz.HRAdmin}, false }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if signedIn {
			req.Header.Set("Cookie", "nemura_session=good-token")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	id := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0"
	for _, tc := range []struct {
		roles                                        []authz.Role
		settingsRead, settingsWrite, template, check int
		sign, signFresh                              int
	}{
		{[]authz.Role{authz.Employee}, 403, 403, 403, 403, 403, 403}, {[]authz.Role{authz.LineManager}, 403, 403, 403, 403, 403, 403},
		{[]authz.Role{authz.PayrollAdmin}, 403, 403, 403, 403, 403, 403},
		{[]authz.Role{authz.HROfficer}, 200, 403, 200, 200, 403, 403}, {[]authz.Role{authz.HRAdmin}, 200, 204, 200, 200, 403, 403},
		{[]authz.Role{authz.Owner}, 200, 204, 200, 200, 403, 200},
	} {
		if got := do(tc.roles, false, "GET", "/v1/letters/settings", "", true); got != tc.settingsRead {
			t.Errorf("%v reading the letterhead: %d, want %d", tc.roles, got, tc.settingsRead)
		}
		if got := do(tc.roles, false, "PUT", "/v1/letters/settings", `{"company_name":"Acme","uen":"1","address":"x"}`, true); got != tc.settingsWrite {
			t.Errorf("%v setting it: %d, want %d", tc.roles, got, tc.settingsWrite)
		}
		if got := do(tc.roles, false, "PUT", "/v1/letters/types/employment_confirmation/template", `{"blocks":[{"kind":"paragraph","text":"x"}]}`, true); got != tc.template {
			t.Errorf("%v writing a template: %d, want %d", tc.roles, got, tc.template)
		}
		if got := do(tc.roles, false, "POST", "/v1/letters/"+id+"/check", "", true); got != tc.check {
			t.Errorf("%v checking: %d, want %d", tc.roles, got, tc.check)
		}
		if got := do(tc.roles, false, "POST", "/v1/letters/"+id+"/signature", `{"sha256":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}`, true); got != tc.sign {
			t.Errorf("%v signing without a fresh passkey: %d, want %d", tc.roles, got, tc.sign)
		}
		if got := do(tc.roles, true, "POST", "/v1/letters/"+id+"/signature", `{"sha256":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}`, true); got != tc.signFresh {
			t.Errorf("%v signing with a fresh passkey: %d, want %d", tc.roles, got, tc.signFresh)
		}
		// Every signed-in person asks for a letter, reads the types, lists their own and withdraws their own.
		for _, call := range []struct{ method, path, body string }{{"POST", "/v1/me/letters", `{"type":"employment_confirmation","purpose":"A bank loan."}`}, {"GET", "/v1/letters/types", ""}, {"GET", "/v1/letters", ""}, {"POST", "/v1/letters/" + id + "/withdrawal", `{"reason":"x"}`}} {
			if got := do(tc.roles, false, call.method, call.path, call.body, true); got >= 300 {
				t.Errorf("%v could not %s %s: %d", tc.roles, call.method, call.path, got)
			}
		}
	}
	// The delivery link needs no sign-in; a letter file needs one.
	if got := do(nil, false, "GET", "/v1/letter-links/download?token=org.token", "", false); got != 200 {
		t.Errorf("the delivery link: %d", got)
	}
	if got := do(nil, false, "GET", "/v1/letters/"+id+"/file", "", false); got != 401 {
		t.Errorf("a letter file with no session: %d", got)
	}
}
