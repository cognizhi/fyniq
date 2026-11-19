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

// The key employment terms: the people team and the Owner write the base terms, the Payroll administrator and the Owner write the pay terms, each part only by its own roles; the
// people team asks for a letter about someone else; nobody else does (NM-LTR-06).
// req: NM-LTR-06
func TestKeyEmploymentTermsAreWrittenInPartsByTheirOwnRoles(t *testing.T) {
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
	id := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"
	base := `{"effective_from":"2026-09-01","base":{"main_duties":"Keeps the books.","notice_period":"One month"}}`
	pay := `{"effective_from":"2026-09-01","pay":{"salary_period":"monthly","basic_salary":"4500.00"}}`
	for _, tc := range []struct {
		roles                             []authz.Role
		read, writeBase, writePay, askFor int
	}{
		{[]authz.Role{authz.Employee}, 403, 403, 403, 403}, {[]authz.Role{authz.LineManager}, 403, 403, 403, 403},
		{[]authz.Role{authz.HROfficer}, 200, 201, 403, 201}, {[]authz.Role{authz.HRAdmin}, 200, 201, 403, 201},
		{[]authz.Role{authz.PayrollAdmin}, 200, 403, 201, 403}, {[]authz.Role{authz.Owner}, 200, 201, 201, 201},
	} {
		if got := do(tc.roles, "GET", "/v1/people/"+id+"/terms", ""); got != tc.read {
			t.Errorf("%v reading: %d, want %d", tc.roles, got, tc.read)
		}
		if got := do(tc.roles, "POST", "/v1/people/"+id+"/terms", base); got != tc.writeBase {
			t.Errorf("%v writing the base terms: %d, want %d", tc.roles, got, tc.writeBase)
		}
		if got := do(tc.roles, "POST", "/v1/people/"+id+"/terms", pay); got != tc.writePay {
			t.Errorf("%v writing the pay terms: %d, want %d", tc.roles, got, tc.writePay)
		}
		if got := do(tc.roles, "POST", "/v1/people/"+id+"/letters", `{"type":"kets","purpose":"The key employment terms"}`); got != tc.askFor {
			t.Errorf("%v asking for a letter about someone: %d, want %d", tc.roles, got, tc.askFor)
		}
	}
	if got := do([]authz.Role{authz.Owner}, "POST", "/v1/people/"+id+"/terms", `{"effective_from":"2026-09-01"}`); got != 422 {
		t.Errorf("terms with no part: %d", got)
	}
	if got := do([]authz.Role{authz.Owner}, "POST", "/v1/people/"+id+"/terms", `{"effective_from":"2026-09-01","pay":{"basic_salary":"4,500"}}`); got != 422 {
		t.Errorf("a salary that is not a decimal: %d", got)
	}
}

// Anyone signed in changes their own details and asks the people team; only the people team reads the queue, decides a change of details and answers a question; the evidence
// must be a file of a size the API accepts (NM-ESS-03, NM-ESS-04).
// req: NM-ESS-03, NM-ESS-04
func TestOwnDetailsAreEveryonesAndTheQueueIsThePeopleTeams(t *testing.T) {
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
	id := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0"
	change := `{"kind":"legal_name","legal_name":"Ben Lim Wei","note":"Deed poll.","evidence":{"title":"Deed poll","content_type":"application/pdf","content_base64":"JVBERi0x"}}`
	for _, tc := range []struct {
		roles                []authz.Role
		queue, decide, reply int
	}{
		{[]authz.Role{authz.Employee}, 403, 403, 403}, {[]authz.Role{authz.LineManager}, 403, 403, 403}, {[]authz.Role{authz.PayrollAdmin}, 403, 403, 403},
		{[]authz.Role{authz.HROfficer}, 200, 204, 204}, {[]authz.Role{authz.HRAdmin}, 200, 204, 204}, {[]authz.Role{authz.Owner}, 200, 204, 204},
	} {
		for _, call := range []struct{ method, path, body string }{{"GET", "/v1/me/details", ""}, {"PUT", "/v1/me/details", `{"phone":"+65 9123 4567"}`},
			{"PUT", "/v1/me/emergency-contacts", `{"contacts":[{"name":"Mei","relationship":"spouse","phone":"9123 4567"}]}`}, {"POST", "/v1/me/details-changes", change},
			{"POST", "/v1/me/questions", `{"question":"When?"}`}, {"GET", "/v1/me/requests", ""}, {"POST", "/v1/me/requests/" + id + "/withdrawal", ""}} {
			if got := do(tc.roles, call.method, call.path, call.body); got >= 300 {
				t.Errorf("%v could not %s %s: %d", tc.roles, call.method, call.path, got)
			}
		}
		if got := do(tc.roles, "GET", "/v1/requests/queue", ""); got != tc.queue {
			t.Errorf("%v reading the queue: %d, want %d", tc.roles, got, tc.queue)
		}
		if got := do(tc.roles, "POST", "/v1/details-changes/"+id+"/decision", `{"approve":false,"reason":"Not legible."}`); got != tc.decide {
			t.Errorf("%v deciding: %d, want %d", tc.roles, got, tc.decide)
		}
		if got := do(tc.roles, "POST", "/v1/questions/"+id+"/reply", `{"reply":"Soon."}`); got != tc.reply {
			t.Errorf("%v replying: %d, want %d", tc.roles, got, tc.reply)
		}
	}
	if got := do([]authz.Role{authz.Employee}, "POST", "/v1/me/details-changes", strings.Replace(change, "JVBERi0x", "not base64!", 1)); got != 422 {
		t.Errorf("evidence that is not base64: %d", got)
	}
	if got := do([]authz.Role{authz.Employee}, "POST", "/v1/me/details-changes", strings.Replace(change, `"kind":"legal_name"`, `"kind":"bank"`, 1)); got != 422 {
		t.Errorf("a kind that needs no approval path: %d", got)
	}
}

// A leaver's account is narrow: it reads their own documents and letters and asks the people team a question, and every other operation answers NM_LEAVER_ACCOUNT, whatever
// roles the leaver once held; the session says it is a leaver's and until when (NM-ESS-06).
// req: NM-ESS-06
// asvs: V8.2.1, V8.3.1
func TestALeaversAccountKeepsOnlyTheirOwnDocumentsAndAQuestion(t *testing.T) {
	do := func(method, path, body string) (int, string) {
		testLeaver, testRoles = true, []authz.Role{authz.Owner}
		defer func() { testLeaver, testRoles = false, []authz.Role{authz.HRAdmin} }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	id := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0"
	person := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"
	for _, c := range []struct{ method, path, body string }{{"GET", "/v1/letters?mine=true", ""}, {"GET", "/v1/letters/" + id + "/file", ""}, {"GET", "/v1/me/payslips", ""}, {"GET", "/v1/me/payslips/" + id, ""}, {"GET", "/v1/me/payslips/" + id + "/file", ""}, {"POST", "/v1/letters/" + id + "/link", ""},
		{"GET", "/v1/people/" + person + "/documents", ""}, {"POST", "/v1/me/questions", `{"question":"Please send me my letter again."}`}, {"GET", "/v1/me/requests", ""}} {
		if code, _ := do(c.method, c.path, c.body); code >= 300 {
			t.Errorf("a leaver could not %s %s: %d", c.method, c.path, code)
		}
	}
	for _, c := range []struct{ method, path, body string }{{"GET", "/v1/me/details", ""}, {"GET", "/v1/me/leave/2026", ""}, {"GET", "/v1/people", ""}, {"GET", "/v1/people/" + person, ""},
		{"POST", "/v1/me/letters", `{"type":"employment_confirmation","purpose":"x"}`}, {"GET", "/v1/requests/queue", ""}, {"PUT", "/v1/me/details", `{"phone":"+65 9123 4567"}`},
		{"GET", "/v1/people/" + person + "/dependants", ""}, {"POST", "/v1/letters/" + id + "/check", ""}} {
		if code, body := do(c.method, c.path, c.body); code != 403 || !strings.Contains(body, "NM_LEAVER_ACCOUNT") {
			t.Errorf("a leaver %s %s: %d %s", c.method, c.path, code, body)
		}
	}
	if code, body := do("GET", "/v1/session", ""); code != 200 || !strings.Contains(body, `"leaver":true`) || !strings.Contains(body, `"leaver_until"`) {
		t.Errorf("the session: %d %s", code, body)
	}
}

// Only the Owner, who signs, delegates automatic issue (after a fresh passkey confirmation) and revokes; the HR administrator reads the delegations; nobody else does (NM-LTR-04).
// req: NM-LTR-04
// asvs: V8.2.1, V8.3.1
func TestOnlyTheSignatoryDelegatesAutomaticIssueAfterAFreshPasskey(t *testing.T) {
	do := func(roles []authz.Role, stepUp bool, method, path, body string) int {
		testRoles, testStepUp = roles, stepUp
		defer func() { testRoles, testStepUp = []authz.Role{authz.HRAdmin}, false }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	id := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f10"
	create := `{"type":"employment_confirmation","monthly_limit":50,"ends_on":"2027-06-30"}`
	for _, tc := range []struct {
		roles                            []authz.Role
		read, createFresh, revoke, plain int
	}{
		{[]authz.Role{authz.Employee}, 403, 403, 403, 403}, {[]authz.Role{authz.HROfficer}, 403, 403, 403, 403}, {[]authz.Role{authz.PayrollAdmin}, 403, 403, 403, 403},
		{[]authz.Role{authz.HRAdmin}, 200, 403, 403, 403}, {[]authz.Role{authz.Owner}, 200, 201, 204, 403},
	} {
		if got := do(tc.roles, false, "GET", "/v1/letters/delegations", ""); got != tc.read {
			t.Errorf("%v reading: %d, want %d", tc.roles, got, tc.read)
		}
		if got := do(tc.roles, true, "POST", "/v1/letters/delegations", create); got != tc.createFresh {
			t.Errorf("%v delegating with a fresh passkey: %d, want %d", tc.roles, got, tc.createFresh)
		}
		if got := do(tc.roles, false, "POST", "/v1/letters/delegations/"+id+"/revocation", `{"reason":"No longer needed."}`); got != tc.revoke {
			t.Errorf("%v revoking: %d, want %d", tc.roles, got, tc.revoke)
		}
	}
	if got := do([]authz.Role{authz.Owner}, false, "POST", "/v1/letters/delegations", create); got != 403 {
		t.Errorf("delegating without a fresh passkey: %d", got)
	}
	if got := do([]authz.Role{authz.Owner}, true, "POST", "/v1/letters/delegations", `{"type":"employment_confirmation","monthly_limit":5000,"ends_on":"2027-06-30"}`); got != 422 {
		t.Errorf("a limit out of range: %d", got)
	}
}

// The joiner's invitation task is the HR administrator's, the Administrator's and the Owner's; a person's leaving is the people team's; the setting is the HR administrator's and
// the Owner's (NM-ONB-02, NM-ONB-03).
// req: NM-ONB-02, NM-ONB-03
func TestInvitationTasksAndLeavingsAreForTheRolesThatHandleThem(t *testing.T) {
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
	id := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f20"
	person := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"
	leaving := `{"kind":"resignation","last_day":"2026-11-30","notice_given_on":"2026-10-01"}`
	for _, tc := range []struct {
		roles                                                 []authz.Role
		tasks, decide, settings, setSettings, leave, setLeave int
	}{
		{[]authz.Role{authz.Employee}, 403, 403, 403, 403, 403, 403}, {[]authz.Role{authz.LineManager}, 403, 403, 403, 403, 403, 403}, {[]authz.Role{authz.PayrollAdmin}, 403, 403, 403, 403, 403, 403},
		{[]authz.Role{authz.HROfficer}, 403, 403, 403, 403, 200, 204}, {[]authz.Role{authz.HRAdmin}, 200, 204, 200, 204, 200, 204},
		{[]authz.Role{authz.Administrator}, 200, 204, 200, 403, 403, 403}, {[]authz.Role{authz.Owner}, 200, 204, 200, 204, 200, 204},
	} {
		if got := do(tc.roles, "GET", "/v1/onboarding/tasks", ""); got != tc.tasks {
			t.Errorf("%v reading the tasks: %d, want %d", tc.roles, got, tc.tasks)
		}
		if got := do(tc.roles, "POST", "/v1/onboarding/tasks/"+id+"/decision", `{"state":"done"}`); got != tc.decide {
			t.Errorf("%v deciding a task: %d, want %d", tc.roles, got, tc.decide)
		}
		if got := do(tc.roles, "GET", "/v1/onboarding/settings", ""); got != tc.settings {
			t.Errorf("%v reading the setting: %d, want %d", tc.roles, got, tc.settings)
		}
		if got := do(tc.roles, "PUT", "/v1/onboarding/settings", `{"invite_to_fyniq":true}`); got != tc.setSettings {
			t.Errorf("%v setting it: %d, want %d", tc.roles, got, tc.setSettings)
		}
		if got := do(tc.roles, "GET", "/v1/offboardings", ""); got != tc.leave {
			t.Errorf("%v listing leavings: %d, want %d", tc.roles, got, tc.leave)
		}
		if got := do(tc.roles, "PUT", "/v1/people/"+person+"/offboarding", leaving); got != tc.setLeave {
			t.Errorf("%v recording a leaving: %d, want %d", tc.roles, got, tc.setLeave)
		}
	}
	if got := do([]authz.Role{authz.HROfficer}, "PUT", "/v1/people/"+person+"/offboarding", `{"kind":"quit","last_day":"2026-11-30"}`); got != 422 {
		t.Errorf("a kind that is not one: %d", got)
	}
	if got := do([]authz.Role{authz.HROfficer}, "POST", "/v1/onboarding/tasks/"+id+"/decision", `{"state":"done"}`); got != 403 {
		t.Errorf("an HR officer deciding an invitation task: %d", got)
	}
}

// Pay is for the Payroll administrator and the Owner alone: the HR officer and the HR administrator, a line manager and an employee are refused every pay operation, read or write, and
// a malformed amount is refused with the field named (Appendix B: Pay, pay history, pay runs; FR-PAY-13; NM-PAY-02).
// req: FR-PAY-13, NM-PAY-02
// asvs: V8.2.1, V8.3.1
func TestPayIsForThePayrollAdministratorAndTheOwnerAlone(t *testing.T) {
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
	calls := []struct{ method, path, body string }{
		{"GET", "/v1/payroll/groups", ""}, {"POST", "/v1/payroll/groups", `{"name":"Monthly","pay_day":25,"cutoff_days":7,"approval_days":3}`},
		{"GET", "/v1/people/" + person + "/pay", ""},
		{"POST", "/v1/people/" + person + "/pay", `{"effective_from":"2026-07-01","pay":{"pay_group_id":"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f30","basic_monthly":"4500.00"}}`},
		{"POST", "/v1/people/" + person + "/pr-election", `{"effective_from":"2026-02-10","evidence_document_id":"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef9"}`},
		{"POST", "/v1/people/" + person + "/pay/year-to-date", `{"year":2026,"ow":"25000.00","aw_subject":"0.00","aw_paid":"0.00","through_month":"2026-05-01"}`},
		{"GET", "/v1/payroll/one-offs?month=2026-06-01", ""},
		{"POST", "/v1/payroll/runs", `{"month":"2026-06-01","pay_group_id":"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f30"}`}, {"GET", "/v1/payroll/runs?month=2026-06-01", ""}, {"GET", "/v1/payroll/runs/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43", ""},
		{"POST", "/v1/payroll/runs/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43/versions", `{"reason":"A bonus was missed."}`},
		{"POST", "/v1/payroll/runs/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43/overrides", `{"person_id":"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61","field":"cpf_employee","value":"999.00","reason":"The CPF Board's own figure."}`},
		{"POST", "/v1/payroll/overrides/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f42/withdrawal", ""}, {"GET", "/v1/payroll/runs/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43/changes", ""},
		{"POST", "/v1/people/" + person + "/pay/one-offs", `{"month":"2026-06-01","item":{"key":"bonus-2026","name":"Annual bonus","nature":"bonus","amount":"5000.00","cpf_wages":true}}`},
	}
	for _, roles := range [][]authz.Role{{authz.Employee}, {authz.LineManager}, {authz.HROfficer}, {authz.HRAdmin}, {authz.Administrator}} {
		for _, c := range calls {
			if got := do(roles, c.method, c.path, c.body); got != 403 {
				t.Errorf("%v %s %s: %d, want 403", roles, c.method, c.path, got)
			}
		}
	}
	for _, roles := range [][]authz.Role{{authz.PayrollAdmin}, {authz.Owner}} {
		for _, c := range calls {
			if got := do(roles, c.method, c.path, c.body); got >= 300 {
				t.Errorf("%v could not %s %s: %d", roles, c.method, c.path, got)
			}
		}
	}
	approve := `{"version":2,"digest":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}`
	approval := "/v1/payroll/runs/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43/approval"
	doFresh := func(roles []authz.Role, fresh bool) int {
		testRoles, testStepUp = roles, fresh
		defer func() { testRoles, testStepUp = []authz.Role{authz.HRAdmin}, false }()
		_, mux := testAPI(t)
		req := httptest.NewRequest("POST", approval, strings.NewReader(approve))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, roles := range [][]authz.Role{{authz.Employee}, {authz.HROfficer}, {authz.HRAdmin}, {authz.Administrator}} {
		if got := doFresh(roles, true); got != 403 {
			t.Errorf("%v approving a pay run: %d, want 403", roles, got)
		}
	}
	for _, roles := range [][]authz.Role{{authz.PayrollAdmin}, {authz.Owner}} {
		if got := doFresh(roles, false); got != 403 {
			t.Errorf("%v approving without a fresh passkey: %d, want 403", roles, got)
		}
		if got := doFresh(roles, true); got != 204 {
			t.Errorf("%v approving with a fresh passkey: %d, want 204", roles, got)
		}
	}
	if got := do([]authz.Role{authz.PayrollAdmin}, "POST", "/v1/people/"+person+"/pay", `{"effective_from":"2026-07-01","pay":{"pay_group_id":"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f30","basic_monthly":"4500.005"}}`); got != 422 {
		t.Errorf("an amount with a fraction of a cent: %d", got)
	}
	if got := do([]authz.Role{authz.PayrollAdmin}, "POST", "/v1/payroll/groups", `{"name":"Monthly","pay_day":40,"cutoff_days":7,"approval_days":3}`); got != 422 {
		t.Errorf("a pay day that is not a day: %d", got)
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
