package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

type fakeRunLink struct{ err error }

func (f fakeRunLink) Envelope(context.Context, Session, uuid.UUID, int) (RunApprovalEnvelope, error) {
	return RunApprovalEnvelope{Linked: true, Ready: true, Issues: []string{}, Envelope: RunEnvelopeView{Departments: []RunEnvelopeDepartment{}, Deductions: []RunEnvelopeDeduction{}}}, f.err
}
func (f fakeRunLink) Approve(context.Context, Session, uuid.UUID, LinkedApprovalInput) error {
	return f.err
}
func (f fakeRunLink) Submission(context.Context, Session, uuid.UUID) (RunSubmissionView, error) {
	return RunSubmissionView{State: "none"}, f.err
}
func (f fakeRunLink) Retry(context.Context, Session, uuid.UUID, int) error { return f.err }

// A pay run's proof to Fyniq is the Payroll administrator's and the Owner's (Appendix B: pay runs): the envelope, the approval with proof and the submission are theirs alone, the approval needs a passkey
// confirmed in the last five minutes, a refusal of the link keeps its own plain code, and a deployment with no link answers that it is not set up.
// req: NM-INT-06, NM-ROLE-04
// asvs: V8.2.1, V8.3.1, V6.4.1
func TestAPayRunsProofToFyniqIsThePayrollAdministratorsAndTheOwnersAndTheApprovalNeedsAFreshPasskey(t *testing.T) {
	run := "/v1/payroll/runs/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"
	body := `{"version":2,"digest":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08","envelope_sha256":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08","approval":{"authenticator_data":"AA","client_data":"AA","signature":"AA"}}`
	do := func(roles []authz.Role, stepUp bool, method, path, payload string) int {
		testRoles, testStepUp = roles, stepUp
		defer func() { testRoles, testStepUp = []authz.Role{authz.HRAdmin}, false }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	calls := []struct {
		method, path, body string
		stepUp             bool
		ok                 int
	}{
		{"GET", run + "/approval-envelope?version=2", "", false, 200},
		{"POST", run + "/linked-approval", body, true, 204},
		{"GET", run + "/submission", "", false, 200},
		{"POST", run + "/submission", `{"version":2}`, false, 204},
	}
	for _, roles := range [][]authz.Role{{authz.Employee}, {authz.LineManager}, {authz.HROfficer}, {authz.HRAdmin}, {authz.Administrator}} {
		for _, c := range calls {
			if got := do(roles, true, c.method, c.path, c.body); got != 403 {
				t.Errorf("%v %s %s: %d, want 403", roles, c.method, c.path, got)
			}
		}
	}
	for _, roles := range [][]authz.Role{{authz.PayrollAdmin}, {authz.Owner}} {
		for _, c := range calls {
			if got := do(roles, true, c.method, c.path, c.body); got != c.ok {
				t.Errorf("%v %s %s: %d, want %d", roles, c.method, c.path, got, c.ok)
			}
		}
		if got := do(roles, false, "POST", run+"/linked-approval", body); got != 403 {
			t.Errorf("%v approving with proof and no fresh passkey: %d, want 403", roles, got)
		}
	}
	// No session is no answer.
	_, mux := testAPI(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", run+"/approval-envelope?version=2", nil))
	if rec.Code != 401 {
		t.Errorf("without a session: %d", rec.Code)
	}
	// A refusal of the link is the pay's plain code.
	requestsErr = &payroll.Refusal{Code: payroll.CodeInvalid, Message: "Fyniq has no department named Sales."}
	if got := do([]authz.Role{authz.Owner}, true, "POST", run+"/linked-approval", body); got != http.StatusUnprocessableEntity {
		t.Errorf("a refused envelope: %d", got)
	}
	requestsErr = &payroll.Refusal{Code: payroll.CodeConflict, Message: "Nemura is linked to Fyniq."}
	if got := do([]authz.Role{authz.Owner}, true, "POST", run+"/submission", `{"version":2}`); got != http.StatusConflict {
		t.Errorf("nothing to send again: %d", got)
	}
	requestsErr = nil
}
