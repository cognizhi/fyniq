package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

type fakeLinkOps struct{ err error }

func (f fakeLinkOps) Health(context.Context, Session) (LinkHealthView, error) {
	return LinkHealthView{State: "active", Undelivered: []LinkEventView{}}, f.err
}
func (f fakeLinkOps) Events(_ context.Context, _ Session, after int64, limit int) ([]LinkEventView, error) {
	out := []LinkEventView{}
	for i := 0; i < limit; i++ { // the API asks for one more than the page to know whether there is a next one
		out = append(out, LinkEventView{EventID: uuid.NewString(), Sequence: after + int64(i) + 1, Type: "people.person.upserted", ObjectType: "person", ObjectID: uuid.NewString(), Version: 1, State: "sent", QueuedAt: "2026-11-25T09:00:00Z"})
	}
	return out, f.err
}
func (f fakeLinkOps) Repair(context.Context, Session, uuid.UUID) (int, error) { return 3, f.err }
func (f fakeLinkOps) DecideRestore(context.Context, Session, uuid.UUID, int, bool, string) (bool, error) {
	return true, f.err
}
func (f fakeLinkOps) Receipt(context.Context, Session, uuid.UUID) (RunReceiptView, error) {
	return RunReceiptView{Approvals: []ReceiptApproval{}, Submissions: []ReceiptSubmission{}, Postings: []RunPosting{}, Payments: []ReceiptPayment{}}, f.err
}

// The link's health, its repair and its restore are the Owner's and the Administrator's, a run's receipt the Payroll administrator's and the Owner's; reconciling a restore needs a passkey confirmed in the last
// five minutes; the events list is paged by a cursor and shows no payload.
// req: NM-INT-04, NM-INT-13
// asvs: V8.2.1, V8.3.1
func TestTheLinksHealthRepairAndRestoreAreTheOwnersAndAdministratorsAndTheReceiptThePayrollTeams(t *testing.T) {
	run := "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"
	do := func(roles []authz.Role, stepUp bool, method, path, payload string) (int, string) {
		testRoles, testStepUp = roles, stepUp
		defer func() { testRoles, testStepUp = []authz.Role{authz.HRAdmin}, false }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	decide := `{"run_id":"` + run + `","version":1,"posted":false}`
	link := []struct {
		method, path, body string
		stepUp             bool
		ok                 int
	}{
		{"GET", "/v1/link/health", "", false, 200}, {"GET", "/v1/link/events", "", false, 200}, {"POST", "/v1/link/repairs", `{}`, false, 200},
		{"POST", "/v1/link/restore/decisions", decide, true, 200},
	}
	for _, roles := range [][]authz.Role{{authz.Employee}, {authz.LineManager}, {authz.HROfficer}, {authz.HRAdmin}, {authz.PayrollAdmin}} {
		for _, c := range link {
			if got, _ := do(roles, true, c.method, c.path, c.body); got != 403 {
				t.Errorf("%v %s %s: %d, want 403", roles, c.method, c.path, got)
			}
		}
	}
	for _, roles := range [][]authz.Role{{authz.Owner}, {authz.Administrator}} {
		for _, c := range link {
			if got, _ := do(roles, true, c.method, c.path, c.body); got != c.ok {
				t.Errorf("%v %s %s: %d, want %d", roles, c.method, c.path, got, c.ok)
			}
		}
		if got, _ := do(roles, false, "POST", "/v1/link/restore/decisions", decide); got == 200 {
			t.Errorf("%v reconciled a restore without a fresh passkey", roles)
		}
	}
	for _, roles := range [][]authz.Role{{authz.Employee}, {authz.HROfficer}, {authz.HRAdmin}, {authz.Administrator}} {
		if got, _ := do(roles, true, "GET", "/v1/payroll/runs/"+run+"/receipt", ""); got != 403 {
			t.Errorf("%v reading a receipt: %d", roles, got)
		}
	}
	for _, roles := range [][]authz.Role{{authz.PayrollAdmin}, {authz.Owner}} {
		if got, _ := do(roles, false, "GET", "/v1/payroll/runs/"+run+"/receipt", ""); got != 200 {
			t.Errorf("%v reading a receipt: %d", roles, got)
		}
	}
	// The list is cursor-paged: one more than the page is asked for, and the page's last sequence is the next cursor.
	code, body := do([]authz.Role{authz.Owner}, false, "GET", "/v1/link/events?cursor=10&limit=3", "")
	if code != 200 || !strings.Contains(body, `"next_cursor":13`) || strings.Count(body, `"event_id"`) != 3 || strings.Contains(body, "payload") {
		t.Errorf("the events page: %d %s", code, body)
	}
	if code, _ := do([]authz.Role{authz.Owner}, true, "POST", "/v1/link/repairs", `{"person_id":"not an id"}`); code != 422 {
		t.Errorf("a repair of no person: %d", code)
	}
}
