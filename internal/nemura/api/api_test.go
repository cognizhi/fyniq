package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/checks"
	"github.com/cognizhi/fyniq/internal/nemura/features"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
	"github.com/cognizhi/fyniq/internal/nemura/letters"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/nemura/policy"
)

var update = flag.Bool("update", false, "rewrite contracts/openapi/nemura/nemura.v1.yaml from the code (make openapi)")

const contractPath = "../../../contracts/openapi/nemura/nemura.v1.yaml"

const contractHeader = `# The contract of Nemura's public API (PRD-002 NM-ARC-03, ADR-N1): OpenAPI 3.1, as the code serves it at
# /v1/openapi.json. Reviewed in every pull request that changes it: CI fails when the code and this file
# differ. Regenerate it with ` + "`make openapi`" + ` and review the diff; never edit it by hand.
# info.version follows the platform version (release-please updates it).
`

// versionLine is info.version: the only line at that indentation named version.
var versionLine = regexp.MustCompile(`(?m)^  version: .*$`)

// fakeSessions knows one session token and the roles of its member.
type fakeSessions struct {
	token  string
	roles  []authz.Role
	stepUp bool
}

func (f fakeSessions) Authenticate(_ context.Context, token string, _ time.Time) (Session, error) {
	if token != f.token || token == "" {
		return Session{}, errors.New("no such session")
	}
	s := Session{ID: uuid.MustParse("0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e46"), Organisation: uuid.MustParse("0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44"), Member: uuid.MustParse("0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45")}
	if f.stepUp {
		at := testNow
		s.StepUpAt = &at
	}
	return s, nil
}

func (f fakeSessions) RolesAt(context.Context, uuid.UUID, uuid.UUID, time.Time) ([]authz.Role, error) {
	return f.roles, nil
}

// fakePasskeys answers each ceremony with the error the test sets (nil: it works).
type fakePasskeys struct{ err error }

func (f fakePasskeys) BeginRegistration(context.Context, Session) ([]byte, error) {
	return []byte(`{"publicKey":{"challenge":"abc"}}`), f.err
}
func (f fakePasskeys) CompleteRegistration(context.Context, Session, string, []byte) (uuid.UUID, error) {
	return uuid.MustParse("0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e50"), f.err
}
func (f fakePasskeys) BeginStepUp(context.Context, Session) ([]byte, error) {
	return []byte(`{"publicKey":{"challenge":"abc"}}`), f.err
}
func (f fakePasskeys) CompleteStepUp(context.Context, Session, []byte) error { return f.err }

type fakeSignIn struct{ done bool }

func (fakeSignIn) Providers() []Provider {
	return []Provider{{ID: "local", Name: "Local sign-in (development)"}}
}
func (f fakeSignIn) SetupDone(context.Context) (bool, error) { return f.done, nil }

// fakePeople answers the people record with whatever the test says, and remembers what it was asked.
type fakePeople struct {
	err         error
	own, report bool
	classified  *bool
}

func (f fakePeople) Directory(context.Context, Session, time.Time) ([]DirectoryEntry, error) {
	return []DirectoryEntry{{ID: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", Name: "Ana Tan", JobTitle: "CEO"}}, f.err
}
func (f fakePeople) Relation(context.Context, Session, uuid.UUID) (bool, bool, error) {
	return f.own, f.report, f.err
}
func (f fakePeople) Get(_ context.Context, _ Session, id uuid.UUID, _ time.Time, classification bool) (Record, error) {
	if f.classified != nil {
		*f.classified = classification
	}
	return Record{ID: id.String(), LegalName: "Ana Tan", Jobs: []Job{}}, f.err
}
func (f fakePeople) Create(context.Context, Session, uuid.UUID, people.Person, people.Job, *people.Classification) error {
	return f.err
}
func (f fakePeople) Update(context.Context, Session, uuid.UUID, people.Person) error { return f.err }
func (f fakePeople) ChangeJob(context.Context, Session, uuid.UUID, time.Time, people.Job) error {
	return f.err
}
func (f fakePeople) SetClassification(context.Context, Session, uuid.UUID, time.Time, people.Classification) error {
	return f.err
}

// fakeFeatures answers the feature switches with whatever the test says.
type fakeFeatures struct {
	err error
	off bool // Leave is switched off
}

func (f fakeFeatures) List(context.Context, Session) (features.Switches, error) {
	var l *leave.Refusal
	if f.off {
		return features.Switches{}, nil
	}
	if errors.As(f.err, &l) { // a refusal of the leave service is not the feature switches'
		return features.Switches{"leave": {State: features.On}}, nil
	}
	return features.Switches{"leave": {State: features.On}}, f.err
}
func (f fakeFeatures) Set(context.Context, Session, string, string, string, []features.Arrangement) error {
	return f.err
}

// fakeRestricted answers the restricted people data with whatever the test says.
type fakeRestricted struct{ err error }

func (f fakeRestricted) SetNRIC(context.Context, Session, uuid.UUID, string) error { return f.err }
func (f fakeRestricted) Secret(context.Context, Session, uuid.UUID, string) (string, string, error) {
	return "S****567D", "masked", f.err
}
func (f fakeRestricted) AddPass(context.Context, Session, uuid.UUID, string, string, time.Time, time.Time) (uuid.UUID, error) {
	return uuid.New(), f.err
}
func (f fakeRestricted) Passes(context.Context, Session, uuid.UUID) ([]Pass, error) {
	return nil, f.err
}
func (f fakeRestricted) AddDependant(context.Context, Session, uuid.UUID, people.Dependant, time.Time) (uuid.UUID, error) {
	return uuid.New(), f.err
}
func (f fakeRestricted) Dependants(context.Context, Session, uuid.UUID) ([]Dependant, error) {
	return nil, f.err
}
func (f fakeRestricted) AddDocument(context.Context, Session, uuid.UUID, string, string, string, string, []byte, []string) (uuid.UUID, error) {
	return uuid.New(), f.err
}
func (f fakeRestricted) Documents(context.Context, Session, uuid.UUID) ([]Document, error) {
	return nil, f.err
}
func (f fakeRestricted) Download(context.Context, Session, uuid.UUID, uuid.UUID) (string, []byte, error) {
	return "application/pdf", []byte("x"), f.err
}
func (f fakeRestricted) ProposeBank(context.Context, Session, uuid.UUID, string) (uuid.UUID, error) {
	return uuid.New(), f.err
}
func (f fakeRestricted) DecideBank(context.Context, Session, uuid.UUID, bool, string) error {
	return f.err
}
func (f fakeRestricted) ConfirmBank(context.Context, Session, uuid.UUID, bool) error { return f.err }
func (f fakeRestricted) Bank(context.Context, Session, uuid.UUID) (BankChange, error) {
	return BankChange{ID: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e73", State: "proposed"}, f.err
}

// fakePacks answers the packs, the policy and the checks with whatever the test says.
type fakePacks struct{ err error }

func (f fakePacks) Versions(context.Context, Session) ([]PackVersion, error) {
	return []PackVersion{{ID: "sg-people@2026.1", Values: []PackValue{}, Sources: []PackSource{}}}, f.err
}
func (f fakePacks) Accept(context.Context, Session, string) error { return f.err }
func (f fakePacks) Decide(context.Context, Session, string, string, string, string) error {
	return f.err
}
func (f fakePacks) SetPolicy(context.Context, Session, time.Time, policy.Rules) ([]string, error) {
	return nil, f.err
}
func (f fakePacks) Policy(context.Context, Session, time.Time) (policy.Rules, string, bool, error) {
	return policy.Rules{}, "", false, f.err
}
func (f fakePacks) Checks(context.Context, Session, time.Time) ([]checks.Finding, error) {
	return []checks.Finding{{Check: 3, Area: "People", Status: checks.Fail, Subject: "x", Message: "m"}}, f.err
}

// fakeLeave answers leave with whatever the test says.
type fakeLeave struct{ err error }

func (f fakeLeave) Own(context.Context, Session) (uuid.UUID, error) {
	return uuid.MustParse("0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"), f.err
}
func (f fakeLeave) View(_ context.Context, _ Session, id uuid.UUID, year int) (LeaveView, error) {
	return LeaveView{PersonID: id.String(), Year: year, Types: []LeaveType{}}, f.err
}
func (f fakeLeave) SetPattern(context.Context, Session, uuid.UUID, time.Time, leave.Pattern) error {
	return f.err
}
func (f fakeLeave) Grant(context.Context, Session, uuid.UUID, int) (LeaveGrant, error) {
	return LeaveGrant{Days: "7.00", Written: "7.00", Basis: []string{}, Sources: []string{}}, f.err
}
func (f fakeLeave) CarryForward(context.Context, Session, uuid.UUID, int) (string, error) {
	return "0.00", f.err
}

// fakeRequests answers leave requests with whatever the test says, and remembers the scope it was called with.
type fakeRequests struct {
	err   error
	scope *LeaveScope
}

func (f fakeRequests) Preview(context.Context, Session, LeaveAsk) (RequestPreview, error) {
	return RequestPreview{Days: "5.00", Left: "9.00", Enough: true, Basis: "working_days", Counts: []DayCost{}, Away: []AwayPerson{}}, f.err
}
func (f fakeRequests) Apply(context.Context, Session, LeaveAsk) (string, string, error) {
	return "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e80", "5.00", f.err
}
func (f fakeRequests) List(_ context.Context, _ Session, sc LeaveScope, _ string) ([]LeaveRequest, error) {
	if f.scope != nil {
		*f.scope = sc
	}
	return []LeaveRequest{}, f.err
}
func (f fakeRequests) Get(context.Context, Session, LeaveScope, uuid.UUID) (LeaveRequest, error) {
	return LeaveRequest{ID: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e80", State: "pending"}, f.err
}
func (f fakeRequests) Decide(context.Context, Session, LeaveScope, uuid.UUID, bool, string) error {
	return f.err
}
func (f fakeRequests) Calendar(context.Context, Session, LeaveScope, bool, time.Time) (LeaveCalendar, error) {
	return LeaveCalendar{Month: "2026-05", People: []CalendarPerson{}}, f.err
}
func (f fakeRequests) Cancel(context.Context, Session, uuid.UUID) error { return f.err }
func (f fakeRequests) Settle(context.Context, Session, time.Time) (int, string, error) {
	return 1, "3.00", f.err
}

// fakeSick answers sick leave with whatever the test says.
type fakeSick struct{ err error }

func (f fakeSick) Notify(context.Context, Session, SickAsk) (SickNotified, error) {
	return SickNotified{ID: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90", Days: "3.00", Paid: "3.00", Unpaid: "0.00"}, f.err
}
func (f fakeSick) List(context.Context, Session, bool, bool) ([]SickNotification, error) {
	return []SickNotification{}, f.err
}
func (f fakeSick) Certificate(context.Context, Session, uuid.UUID, bool, string, string, []byte) error {
	return f.err
}
func (f fakeSick) Verify(context.Context, Session, uuid.UUID) error { return f.err }

// fakeGovernment answers government-paid leave with whatever the test says.
type fakeGovernment struct{ err error }

func (f fakeGovernment) Record(context.Context, Session, uuid.UUID, GovAsk) (string, error) {
	return "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e94", f.err
}
func (f fakeGovernment) List(context.Context, Session) ([]GovBlock, error) {
	return []GovBlock{}, f.err
}
func (f fakeGovernment) File(context.Context, Session, uuid.UUID, string, time.Time, string) (string, bool, error) {
	return "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e95", false, f.err
}
func (f fakeGovernment) Event(context.Context, Session, uuid.UUID, string, string, time.Time, string) error {
	return f.err
}

// fakeFlex answers flexible work requests with whatever the test says.
type fakeFlex struct{ err error }

func (f fakeFlex) Request(context.Context, Session, FlexAsk) (string, string, error) {
	return "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ea0", "2027-01-20", f.err
}
func (f fakeFlex) List(context.Context, Session, bool) ([]FlexRequest, error) {
	return []FlexRequest{}, f.err
}
func (f fakeFlex) Reply(context.Context, Session, uuid.UUID, string, string) error { return f.err }
func (f fakeFlex) Withdraw(context.Context, Session, uuid.UUID) error              { return f.err }

// fakeTime answers hours worked and pay inputs with whatever the test says.
type fakeTime struct{ err error }

func (f fakeTime) RecordOvertime(context.Context, Session, OvertimeAsk) (string, bool, error) {
	return "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1eb0", false, f.err
}
func (f fakeTime) Overtime(context.Context, Session, LeaveScope, string) ([]OvertimeRecord, error) {
	return []OvertimeRecord{}, f.err
}
func (f fakeTime) DecideOvertime(context.Context, Session, LeaveScope, uuid.UUID, bool, string) error {
	return f.err
}
func (f fakeTime) PayInputs(context.Context, Session, time.Time) ([]PayInput, error) {
	return []PayInput{}, f.err
}
func (f fakeTime) DecideUnpaid(context.Context, Session, uuid.UUID, string) (string, error) {
	return "2.00", f.err
}
func (f fakeTime) DraftAccrual(context.Context, Session, time.Time) (int, error) { return 1, f.err }
func (f fakeTime) Accruals(context.Context, Session, time.Time) ([]AccrualLine, error) {
	return []AccrualLine{}, f.err
}
func (f fakeTime) ApproveAccrual(context.Context, Session, time.Time) (int, error) { return 1, f.err }
func (f fakeTime) Availability(context.Context, Session, uuid.UUID, time.Time, time.Time) (Availability, error) {
	return Availability{WorkingDays: "21.00", AwayDays: "0.00", HoursPerDay: "8.80", Hours: "184.80"}, f.err
}
func (f fakeTime) Encash(context.Context, Session, uuid.UUID, int, string, time.Time, string) error {
	return f.err
}

// fakeLetters answers letters with whatever the test says.
type fakeLetters struct{ err error }

func (f fakeLetters) Settings(context.Context, Session) (LetterSettings, bool, error) {
	return LetterSettings{Name: "Acme", UEN: "1", Address: "x"}, true, f.err
}
func (f fakeLetters) SetSettings(context.Context, Session, LetterSettings) error { return f.err }
func (f fakeLetters) Types(context.Context, Session) ([]LetterType, error) {
	return []LetterType{{Key: "employment_confirmation", Title: "Employment confirmation", Fields: []string{}}}, f.err
}
func (f fakeLetters) SetType(context.Context, Session, string, bool, int) error { return f.err }
func (f fakeLetters) Templates(context.Context, Session, string) ([]LetterTemplate, error) {
	return []LetterTemplate{}, f.err
}
func (f fakeLetters) SaveDraft(context.Context, Session, string, []LetterBlock, string) (string, error) {
	return "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ee0", f.err
}
func (f fakeLetters) Publish(context.Context, Session, uuid.UUID) (int, error) { return 1, f.err }
func (f fakeLetters) Request(context.Context, Session, string, string) (string, string, error) {
	return "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0", "2026-11-25", f.err
}
func (f fakeLetters) List(context.Context, Session, bool, string) ([]LetterRequest, error) {
	return []LetterRequest{}, f.err
}
func (f fakeLetters) Check(context.Context, Session, uuid.UUID) (string, string, error) {
	return "EC-2026-0001", "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", f.err
}
func (f fakeLetters) Draft(context.Context, Session, uuid.UUID) (string, []byte, error) {
	return "EC-2026-0001", []byte("%PDF-1.4"), f.err
}
func (f fakeLetters) Sign(context.Context, Session, uuid.UUID, string) (string, error) {
	return "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f00", f.err
}
func (f fakeLetters) Refuse(context.Context, Session, uuid.UUID, string, bool) error { return f.err }
func (f fakeLetters) File(context.Context, Session, uuid.UUID, bool) (string, []byte, error) {
	return "EC-2026-0001", []byte("%PDF-1.4"), f.err
}
func (f fakeLetters) Link(context.Context, Session, uuid.UUID, bool) (string, string, error) {
	return "org.token", "2026-11-28T10:00:00Z", f.err
}
func (f fakeLetters) ByLink(context.Context, string) (string, []byte, error) {
	return "EC-2026-0001", []byte("%PDF-1.4"), f.err
}

// fakeClients registers whatever it is told and answers with clientsErr.
type fakeClients struct{ err error }

func (f fakeClients) Register(_ context.Context, _ Session, name string, scopes []string, _ string, _ json.RawMessage) (Client, error) {
	return Client{ID: "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e60", Name: name, Scopes: scopes, ClientID: "c-1", Audience: "nemura-api"}, f.err
}
func (f fakeClients) List(context.Context, Session) ([]Client, error)  { return nil, f.err }
func (f fakeClients) Revoke(context.Context, Session, uuid.UUID) error { return f.err }

var (
	passkeyErr, clientsErr, peopleErr, featuresErr, restrictedErr, packsErr, leaveErr, requestsErr error
	requestsScope                                                                                  *LeaveScope
	featuresOff                                                                                    bool
	peopleOwn, peopleReport                                                                        bool
	peopleClassified                                                                               *bool
	testRoles                                                                                      = []authz.Role{authz.HRAdmin}
	testStepUp                                                                                     bool
	testNow                                                                                        = time.Date(2099, 1, 1, 9, 0, 0, 0, time.UTC)
)

func testAPI(t *testing.T) (*API, *http.ServeMux) {
	t.Helper()
	mux := http.NewServeMux()
	d := Deps{Sessions: fakeSessions{token: "good-token", roles: testRoles, stepUp: testStepUp}, Passkeys: fakePasskeys{err: passkeyErr}, Clients: fakeClients{err: clientsErr}, SignIn: fakeSignIn{done: true}, Features: fakeFeatures{err: featuresErr, off: featuresOff}, Restricted: fakeRestricted{err: restrictedErr}, Packs: fakePacks{err: packsErr}, Leave: fakeLeave{err: leaveErr}, Requests: fakeRequests{err: requestsErr, scope: requestsScope}, Sick: fakeSick{err: requestsErr}, Government: fakeGovernment{err: requestsErr}, Flex: fakeFlex{err: requestsErr}, Time: fakeTime{err: requestsErr}, Letters: fakeLetters{err: requestsErr}, People: fakePeople{err: peopleErr, own: peopleOwn, report: peopleReport, classified: peopleClassified}, Clock: clock.NewFrozen(testNow),
		CookieNames: []string{"__Host-nemura_session", "nemura_session"}}
	return New(mux, d), mux
}

func document(t *testing.T) []byte {
	t.Helper()
	a, _ := testAPI(t)
	b, err := a.OpenAPI().YAML()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The reviewed contract and the code agree: the document huma generates from the registered operations is the file in
// contracts/, apart from the platform version. A change to the API is a change to that file, reviewed in the pull request.
// req: NM-ARC-03
func TestTheContractIsTheCode(t *testing.T) {
	got := document(t)
	if *update {
		version, err := os.ReadFile("../../../VERSION")
		if err != nil {
			t.Fatal(err)
		}
		out := versionLine.ReplaceAll(got, []byte("  version: "+strings.TrimSpace(string(version))+" # x-release-please-version"))
		if err := os.MkdirAll("../../../contracts/openapi/nemura", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(contractPath, append([]byte(contractHeader), out...), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("%v (run make openapi)", err)
	}
	normal := func(b []byte) []string {
		b = versionLine.ReplaceAll(bytes.TrimPrefix(b, []byte(contractHeader)), []byte("  version: (platform)"))
		return strings.Split(string(b), "\n")
	}
	g, w := normal(got), normal(want)
	for i := 0; i < max(len(g), len(w)); i++ {
		gl, wl := "", ""
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			t.Fatalf("the API's code and contracts/openapi/nemura/nemura.v1.yaml differ from line %d:\n  code:     %q\n  contract: %q\n"+
				"Run `make openapi` and review the change to the contract (a breaking change inside /v1 is refused).",
				i+1+strings.Count(contractHeader, "\n"), gl, wl)
		}
	}
}

// infrastructure are the routes outside the API's contract: none yet. The probes and the version belong to the HTTP shell of
// every service (cmd/nemura), not to this package.
var infrastructure = []string{"GET /v1/openapi.json", "GET /v1/openapi.yaml", "GET /v1/openapi-3.0.json", "GET /v1/openapi-3.0.yaml"}

type operation struct{ method, path string }

func operations(t *testing.T) map[operation]map[string]any {
	t.Helper()
	a, _ := testAPI(t)
	j, err := json.Marshal(a.OpenAPI())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(j, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[operation]map[string]any{}
	for path, methods := range doc.Paths {
		for m, op := range methods {
			out[operation{strings.ToUpper(m), path}] = op
		}
	}
	return out
}

// Every route the API serves is a documented operation or named infrastructure: no private endpoints (the server half of
// QA-API-03).
// req: NM-ARC-03
func TestEveryRouteIsDocumented(t *testing.T) {
	_, mux := testAPI(t)
	ops := operations(t)
	for op := range ops {
		req := httptest.NewRequest(op.method, strings.ReplaceAll(op.path, "{code}", "NM_NOT_FOUND"), nil)
		if _, pattern := mux.Handler(req); pattern == "" {
			t.Errorf("%s %s is documented but not served", op.method, op.path)
		}
	}
	// A path that is not an operation is not served.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/not-an-operation", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("an undocumented path answered %d", rec.Code)
	}
	for _, in := range infrastructure {
		method, path, _ := strings.Cut(in, " ")
		if _, pattern := mux.Handler(httptest.NewRequest(method, path, nil)); pattern == "" {
			t.Errorf("infrastructure %s is not served", in)
		}
	}
}

// Every write names an Idempotency-Key header, and a versioned record's update names If-Match (NM-ARC-03; FR-INT-08, FR-INT-09).
// There is no write operation yet, so the rule holds trivially here and fails the build when the first one lacks them; the
// planted case proves the rule can fail.
// req: NM-ARC-03
func TestEveryWriteAcceptsAnIdempotencyKey(t *testing.T) {
	violations := func(ops map[operation]map[string]any) []string {
		var out []string
		for op, def := range ops {
			if op.method == "GET" || op.method == "HEAD" || op.method == "OPTIONS" {
				continue
			}
			names := []string{}
			if params, ok := def["parameters"].([]any); ok {
				for _, p := range params {
					if m, ok := p.(map[string]any); ok && m["in"] == "header" {
						names = append(names, strings.ToLower(m["name"].(string)))
					}
				}
			}
			if _, exempt := IdempotencyExempt[op.method+" "+op.path]; exempt {
				continue
			}
			if !slices.Contains(names, "idempotency-key") {
				out = append(out, op.method+" "+op.path+" has no Idempotency-Key header")
			}
		}
		return out
	}
	if v := violations(operations(t)); len(v) > 0 {
		t.Fatalf("writes without an Idempotency-Key:\n%s", strings.Join(v, "\n"))
	}
	planted := map[operation]map[string]any{
		{"POST", "/v1/things"}:     {"parameters": []any{}},
		{"PUT", "/v1/things/{id}"}: {"parameters": []any{map[string]any{"in": "header", "name": "Idempotency-Key"}}},
		{"GET", "/v1/things"}:      {},
	}
	if v := violations(planted); len(v) != 1 || !strings.Contains(v[0], "POST /v1/things") {
		t.Fatalf("the rule did not catch the planted write: %v", v)
	}
}

// Every code of the catalogue is triggered by an operation, returns RFC 9457 problem details with the code, a plain detail and a
// fix, and the catalogue is served as the document says.
// req: NM-ARC-03
const policyBody = `{"effective_from":"2026-12-01","rules":{"annual_leave_days":[7],"sick_outpatient_days":[5,8,11,14],"sick_hospital_days":[15,30,45,60],"childcare_citizen_days":6,"extended_childcare_days":2,"childcare_non_citizen_days":2}}`
const personBody = `{"id":"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61","person":{"legal_name":"Ana Tan","employment_start":"2026-01-05"},"job":{"job_title":"CEO","worksite":"SG","employment_type":"full_time"}}`
const patternBody = `{"effective_from":"2026-01-05","weekdays":[1,2,3,4,5],"hours_per_week":"44.0"}`
const jobBody = `{"effective_from":"2026-09-01","job":{"job_title":"CEO","worksite":"SG","employment_type":"full_time"}}`

const clientBody = `{"name":"Payroll bureau","scopes":["directory.read"],"algorithm":"ES256","public_key":{"kty":"EC","crv":"P-256","x":"a","y":"b"}}`

func TestEveryCodeInTheCatalogueIsTriggered(t *testing.T) {
	type trigger struct {
		method, path, body string
		signedIn           bool
		err                error // what the passkey or client service answers
		owner, stepUp      bool  // the person is the Owner, and confirmed with a passkey
		featuresOff        bool  // Leave is switched off
	}
	triggers := map[string]trigger{ // code -> a request that returns it
		CodeNotFound:           {method: "GET", path: "/v1/errors/NM_NOPE"},
		CodeInvalid:            {method: "GET", path: "/v1/errors/not-a-code"},
		CodeUnauthenticated:    {method: "GET", path: "/v1/session"},
		CodePasskeyRefused:     {method: "POST", path: "/v1/session/step-up/confirm", body: `{"credential":{}}`, signedIn: true, err: ErrPasskeyRefused},
		CodeNoCeremony:         {method: "POST", path: "/v1/session/step-up/confirm", body: `{"credential":{}}`, signedIn: true, err: ErrNoCeremony},
		CodeNoPasskey:          {method: "POST", path: "/v1/session/step-up", signedIn: true, err: ErrNoPasskey},
		CodeRoleRequired:       {method: "GET", path: "/v1/api-clients", signedIn: true},
		CodeStepUp:             {method: "DELETE", path: "/v1/api-clients/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e60", signedIn: true, owner: true},
		CodeClientInvalid:      {method: "POST", path: "/v1/api-clients", body: clientBody, signedIn: true, owner: true, stepUp: true, err: &apiclient.Refusal{Code: apiclient.CodeInvalid, Message: "The scope \"x\" does not exist."}},
		CodeUnavailable:        {method: "GET", path: "/v1/api-clients", signedIn: true, owner: true, err: ErrUnavailable},
		CodePolicyBelow:        {method: "PUT", path: "/v1/policy", body: policyBody, signedIn: true, err: &people.Refusal{Code: policy.CodeBelowStatute, Message: "The policy gives less than the statute."}},
		CodeFeatureRequired:    {method: "PUT", path: "/v1/features/leave", body: `{"state":"off"}`, signedIn: true, err: &features.Refusal{Code: features.CodeRequired, Message: "Payroll needs Leave."}},
		CodeFeatureUnavailable: {method: "PUT", path: "/v1/features/hiring", body: `{"state":"on"}`, signedIn: true, err: &features.Refusal{Code: features.CodeUnavailable, Message: "Hiring is built in P2."}},
		CodeFeatureObligations: {method: "PUT", path: "/v1/features/leave", body: `{"state":"off"}`, signedIn: true, err: &features.Refusal{Code: features.CodeObligations, Message: "Work is still open."}},
		CodeFeatureInvalid:     {method: "PUT", path: "/v1/features/magic", body: `{"state":"on"}`, signedIn: true, err: &features.Refusal{Code: features.CodeInvalid, Message: "No such feature."}},
		CodePersonInvalid:      {method: "POST", path: "/v1/people", body: personBody, signedIn: true, err: &people.Refusal{Code: people.CodeInvalid, Message: "The legal name is required: enter it."}},
		CodePersonNotFound:     {method: "GET", path: "/v1/people/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61", signedIn: true, err: &people.Refusal{Code: people.CodeNotFound, Message: "There is no such person in this organisation."}},
		CodePersonHistory:      {method: "POST", path: "/v1/people/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61/job", body: jobBody, signedIn: true, err: &people.Refusal{Code: people.CodeHistory, Message: "That record is history."}},
		CodePersonExists:       {method: "POST", path: "/v1/people", body: personBody, signedIn: true, err: ErrPersonExists},
		CodeLeaveInvalid:       {method: "PUT", path: "/v1/people/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61/working-pattern", body: patternBody, signedIn: true, err: &leave.Refusal{Code: leave.CodeInvalid, Message: "The working days are required."}},
		CodeLeaveBalance:       {method: "POST", path: "/v1/people/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61/leave/2026/carry-forward", signedIn: true, err: &leave.Refusal{Code: leave.CodeBalance, Message: "That is more than the balance."}},
		CodeLeaveConflict:      {method: "PUT", path: "/v1/people/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61/working-pattern", body: patternBody, signedIn: true, err: &leave.Refusal{Code: leave.CodeConflict, Message: "Overlaps."}},
		CodeFeatureOff:         {method: "GET", path: "/v1/people/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61/leave/2026", signedIn: true, featuresOff: true},
		CodeLetterInvalid:      {method: "POST", path: "/v1/me/letters", body: `{"type":"employment_confirmation","purpose":"x"}`, signedIn: true, err: &letters.Refusal{Code: letters.CodeInvalid, Message: "Say what the letter is for."}},
		CodeLetterConflict:     {method: "POST", path: "/v1/letters/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1ef0/check", signedIn: true, err: &letters.Refusal{Code: letters.CodeConflict, Message: "That request is checked."}},
		CodeClientExists:       {method: "POST", path: "/v1/api-clients", body: clientBody, signedIn: true, owner: true, stepUp: true, err: &apiclient.Refusal{Code: apiclient.CodeExists, Message: "A client with that name is registered already."}},
	}
	for _, c := range Catalogue {
		tr, ok := triggers[c.Code]
		if !ok {
			t.Errorf("%s is in the catalogue and no test triggers it", c.Code)
			continue
		}
		passkeyErr, clientsErr, peopleErr, featuresErr, packsErr, leaveErr, requestsErr = tr.err, tr.err, tr.err, tr.err, tr.err, tr.err, tr.err
		if tr.owner {
			testRoles = []authz.Role{authz.Owner}
		}
		testStepUp, featuresOff = tr.stepUp, tr.featuresOff
		_, mux := testAPI(t)
		passkeyErr, clientsErr, peopleErr, featuresErr, packsErr, leaveErr, requestsErr, testRoles, testStepUp, featuresOff = nil, nil, nil, nil, nil, nil, nil, []authz.Role{authz.HRAdmin}, false, false
		req := httptest.NewRequest(tr.method, tr.path, strings.NewReader(tr.body))
		if tr.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if tr.signedIn {
			req.Header.Set("Cookie", "nemura_session=good-token")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var p Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || rec.Code != c.Status || p.Code != c.Code ||
			!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/problem+json") || p.Detail == "" || p.Fix == "" || p.Title != c.Title {
			t.Errorf("%s: %d %s -> %+v (%v)", c.Code, rec.Code, rec.Header().Get("Content-Type"), p, err)
		}
	}
	for code := range triggers {
		if !slices.ContainsFunc(Catalogue, func(c Code) bool { return c.Code == code }) {
			t.Errorf("%s is triggered but not in the catalogue", code)
		}
	}
	// The invalid field is named, with its path and the fix.
	_, mux := testAPI(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/errors/not-a-code", nil))
	var p Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if len(p.Errors) != 1 || p.Errors[0].Field != "path.code" || p.Errors[0].Fix == "" {
		t.Errorf("the invalid field: %+v", p.Errors)
	}
	// The catalogue is served, in order.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/errors", nil))
	var list struct{ Codes []Code }
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Codes) != len(Catalogue) {
		t.Errorf("GET /v1/errors: %d codes, %v", len(list.Codes), err)
	}
}

// Every operation has a row in the authorisation matrix, and an operation without one fails the build: closed by default
// (golden rule 6, SEC-02). The matrix of PRD-002 Appendix B arrives with the roles slice; until then these operations are the
// ones anyone may call, with no sign-in and no data of an organisation.
// req: NM-ARC-03
func TestEveryOperationHasARowInTheAuthorisationMatrix(t *testing.T) {
	missing := func(ops map[operation]map[string]any, matrix map[string]string) []string {
		var out []string
		for op := range ops {
			if _, ok := matrix[op.method+" "+op.path]; !ok {
				out = append(out, op.method+" "+op.path)
			}
		}
		return out
	}
	if m := missing(operations(t), Matrix); len(m) > 0 {
		t.Fatalf("operations with no row in the authorisation matrix (api.Matrix): %v", m)
	}
	if m := missing(map[operation]map[string]any{{"GET", "/v1/new-thing"}: {}}, Matrix); len(m) != 1 {
		t.Fatalf("the rule did not catch a planted operation: %v", m)
	}
	for row := range Matrix {
		method, path, _ := strings.Cut(row, " ")
		if _, ok := operations(t)[operation{method, path}]; !ok {
			t.Errorf("the matrix names %s, which is not an operation", row)
		}
	}
}

// The session operation says who is signed in, the roles they hold now and where they land; without a valid session it is
// NM_UNAUTHENTICATED, whichever of Nemura's cookie names carries it (NM-ROLE-01, NM-ROLE-02).
// req: NM-ARC-03
// asvs: V7.2.1, V8.2.1
func TestTheSessionOperationSaysWhoIsSignedInAndWhereTheyLand(t *testing.T) {
	get := func(cookie string) *httptest.ResponseRecorder {
		_, mux := testAPI(t)
		req := httptest.NewRequest(http.MethodGet, "/v1/session", nil)
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	for _, c := range []string{"nemura_session=good-token", "other=1; __Host-nemura_session=good-token"} {
		rec := get(c)
		var body struct {
			Organisation string   `json:"organisation_id"`
			Member       string   `json:"member_id"`
			Roles        []string `json:"roles"`
			Landing      string   `json:"landing"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK || len(body.Roles) != 1 || body.Roles[0] != "hr_admin" || body.Landing != "/people" ||
			body.Member != "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45" {
			t.Errorf("%q: %d %s", c, rec.Code, rec.Body.String())
		}
	}
	for _, c := range []string{"", "nemura_session=wrong", "fyniq_session=good-token", "nemura_session="} {
		if rec := get(c); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), CodeUnauthenticated) {
			t.Errorf("%q: %d %s", c, rec.Code, rec.Body.String())
		}
	}
}

// Landing follows the highest role: the people team on People home, everyone else on My home (PRD-002 §3).
// req: NM-ROLE-02
func TestEachRoleLandsWhereThePRDSays(t *testing.T) {
	for _, tc := range []struct {
		roles []authz.Role
		want  string
	}{
		{[]authz.Role{authz.Employee}, "/me"}, {[]authz.Role{authz.Employee, authz.LineManager}, "/me"}, {[]authz.Role{authz.HROfficer}, "/people"},
		{[]authz.Role{authz.HRAdmin}, "/people"}, {[]authz.Role{authz.PayrollAdmin}, "/people"}, {[]authz.Role{authz.Owner}, "/people"},
		{[]authz.Role{authz.Administrator}, "/people"}, {nil, "/me"}, {[]authz.Role{authz.Recruiter}, "/me"},
	} {
		if got := Landing(tc.roles); got != tc.want {
			t.Errorf("%v lands on %s, want %s", tc.roles, got, tc.want)
		}
	}
}

// The passkey operations need a session, answer the options and ceremonies of the service, and every operation that takes no Idempotency-Key
// is named, with its reason, in IdempotencyExempt and is a real operation (NM-ARC-06, NM-ARC-03).
// req: NM-ARC-03
// asvs: V6.4.1, V7.2.1
func TestThePasskeyOperationsNeedASessionAndEveryExemptionIsReal(t *testing.T) {
	_, mux := testAPI(t)
	post := func(path, body, cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	for path, body := range map[string]string{"/v1/passkeys/registration": "", "/v1/passkeys": `{"name":"x","credential":{}}`, "/v1/session/step-up": "", "/v1/session/step-up/confirm": `{"credential":{}}`} {
		if rec := post(path, body, ""); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), CodeUnauthenticated) {
			t.Errorf("POST %s with no session: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if rec := post("/v1/passkeys/registration", "", "nemura_session=good-token"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "challenge") {
		t.Errorf("registration options: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("/v1/passkeys", `{"name":"My laptop","credential":{"id":"x"}}`, "nemura_session=good-token"); rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e50") {
		t.Errorf("registering: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("/v1/session/step-up/confirm", `{"credential":{"id":"x"}}`, "nemura_session=good-token"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"confirmed":true`) {
		t.Errorf("confirming: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("/v1/passkeys", `{"name":"","credential":{}}`, "nemura_session=good-token"); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a passkey with no name: %d", rec.Code)
	}
	ops := operations(t)
	for key, why := range IdempotencyExempt {
		method, path, _ := strings.Cut(key, " ")
		if _, ok := ops[operation{method, path}]; !ok || len(why) < 20 {
			t.Errorf("the exemption %q names no operation, or no reason", key)
		}
	}
}

// Only the Owner registers, lists and revokes API clients, and a change needs a passkey confirmation in the last five minutes; a registered
// client's answer names its audience and never a key (NM-ARC-06).
// req: NM-ARC-06
// asvs: V8.2.1, V6.4.1, V13.2.1
func TestOnlyTheOwnerManagesAPIClientsWithAFreshPasskeyConfirmation(t *testing.T) {
	do := func(owner, stepUp bool, method, path, body string) *httptest.ResponseRecorder {
		if owner {
			testRoles = []authz.Role{authz.Owner}
		}
		testStepUp = stepUp
		defer func() { testRoles, testStepUp = []authz.Role{authz.HRAdmin}, false }()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	const id = "/v1/api-clients/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e60"
	for _, tc := range []struct {
		owner, stepUp bool
		method, path  string
		body          string
		want          int
	}{
		{false, true, "POST", "/v1/api-clients", clientBody, http.StatusForbidden},
		{false, true, "GET", "/v1/api-clients", "", http.StatusForbidden},
		{false, true, "DELETE", id, "", http.StatusForbidden},
		{true, false, "POST", "/v1/api-clients", clientBody, http.StatusForbidden},
		{true, false, "DELETE", id, "", http.StatusForbidden},
		{true, false, "GET", "/v1/api-clients", "", http.StatusOK},
		{true, true, "POST", "/v1/api-clients", clientBody, http.StatusCreated},
		{true, true, "DELETE", id, "", http.StatusNoContent},
	} {
		if rec := do(tc.owner, tc.stepUp, tc.method, tc.path, tc.body); rec.Code != tc.want {
			t.Errorf("owner=%v stepUp=%v %s %s: %d %s, want %d", tc.owner, tc.stepUp, tc.method, tc.path, rec.Code, rec.Body.String(), tc.want)
		}
	}
	rec := do(true, true, "POST", "/v1/api-clients", clientBody)
	if !strings.Contains(rec.Body.String(), `"audience":"nemura-api"`) || strings.Contains(rec.Body.String(), "public_key") || strings.Contains(rec.Body.String(), `"x"`) {
		t.Errorf("a registered client shows its audience and no key: %s", rec.Body.String())
	}
}

// The sign-in page's information needs no session: the providers in order and whether setup is done.
// req: NM-ARC-01
func TestTheSignInPageLearnsTheProvidersAndWhetherSetupIsDone(t *testing.T) {
	_, mux := testAPI(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/sign-in", nil))
	var body struct {
		Providers []Provider `json:"providers"`
		SetupDone bool       `json:"setup_done"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK || len(body.Providers) != 1 || body.Providers[0].ID != "local" || !body.SetupDone {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	// With no provider wired (the release flavour before Identity Platform), the page gets none, not an error.
	mux2 := http.NewServeMux()
	New(mux2, Deps{Clock: clock.NewFrozen(testNow)})
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/sign-in", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"providers":[]`) {
		t.Errorf("no provider wired: %d %s", rec.Code, rec.Body.String())
	}
}

// The directory is for everyone and shows nothing restricted; a person's record is the people team's (with the classification), their own, or
// their manager's for a report (without it), and anyone else is told there is no such person; only the HR officer and administrator write
// (NM-PPL-01, NM-PPL-06, NM-PPL-08, NM-SEC-02).
// req: NM-PPL-06
// asvs: V8.2.1, V8.3.1
func TestThePeopleRecordIsReadByTheRightPeopleAndWrittenByHR(t *testing.T) {
	const id = "/v1/people/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"
	do := func(roles []authz.Role, own, report bool, method, path, body string) (*httptest.ResponseRecorder, bool) {
		testRoles, peopleOwn, peopleReport = roles, own, report
		var classified bool
		peopleClassified = &classified
		defer func() {
			testRoles, peopleOwn, peopleReport, peopleClassified = []authz.Role{authz.HRAdmin}, false, false, nil
		}()
		_, mux := testAPI(t)
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "nemura_session=good-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec, classified
	}
	employee, manager := []authz.Role{authz.Employee}, []authz.Role{authz.LineManager}
	for _, tc := range []struct {
		name         string
		roles        []authz.Role
		own, report  bool
		method, path string
		body         string
		want         int
		classified   bool
	}{
		{"the directory, an employee", employee, false, false, "GET", "/v1/people", "", 200, false},
		{"a stranger's record, an employee", employee, false, false, "GET", id, "", 404, false},
		{"their own record, an employee", employee, true, false, "GET", id, "", 200, false},
		{"a report's record, a manager", manager, false, true, "GET", id, "", 200, false},
		{"another's record, a manager", manager, false, false, "GET", id, "", 404, false},
		{"anyone's record, HR officer", []authz.Role{authz.HROfficer}, false, false, "GET", id, "", 200, true},
		{"anyone's record, Payroll administrator", []authz.Role{authz.PayrollAdmin}, false, false, "GET", id, "", 200, true},
		{"anyone's record, Owner", []authz.Role{authz.Owner}, false, false, "GET", id, "", 200, true},
		{"adding a person, an employee", employee, false, false, "POST", "/v1/people", personBody, 403, false},
		{"adding a person, a manager", manager, false, false, "POST", "/v1/people", personBody, 403, false},
		{"adding a person, Payroll administrator", []authz.Role{authz.PayrollAdmin}, false, false, "POST", "/v1/people", personBody, 403, false},
		{"adding a person, HR officer", []authz.Role{authz.HROfficer}, false, false, "POST", "/v1/people", personBody, 201, false},
		{"adding a person, HR administrator", []authz.Role{authz.HRAdmin}, false, false, "POST", "/v1/people", personBody, 201, false},
		{"changing a job, an employee", employee, false, false, "POST", id + "/job", jobBody, 403, false},
		{"changing a job, HR administrator", []authz.Role{authz.HRAdmin}, false, false, "POST", id + "/job", jobBody, 201, false},
		{"a bad date, HR administrator", []authz.Role{authz.HRAdmin}, false, false, "POST", id + "/job", `{"effective_from":"1 Sept","job":{"job_title":"CEO","worksite":"SG","employment_type":"full_time"}}`, 422, false},
	} {
		rec, classified := do(tc.roles, tc.own, tc.report, tc.method, tc.path, tc.body)
		if rec.Code != tc.want || (tc.want == 200 && tc.method == "GET" && tc.path != "/v1/people" && classified != tc.classified) {
			t.Errorf("%s: %d %s (classification asked: %v)", tc.name, rec.Code, rec.Body.String(), classified)
		}
	}
	rec, _ := do(employee, false, false, "GET", "/v1/people", "")
	if strings.Contains(rec.Body.String(), "birth") || strings.Contains(rec.Body.String(), "classification") || strings.Contains(rec.Body.String(), "phone") {
		t.Errorf("the directory shows more than it should: %s", rec.Body.String())
	}
}

// The feature switches are seen by whoever may see the settings and changed only by the HR administrator and the Owner (Appendix B).
// req: NM-FEAT-01
// asvs: V8.2.1
func TestOnlyTheHRAdministratorAndTheOwnerSwitchFeatures(t *testing.T) {
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
	for _, tc := range []struct {
		roles []authz.Role
		list  int
		set   int
	}{
		{[]authz.Role{authz.Employee}, 403, 403}, {[]authz.Role{authz.LineManager}, 403, 403},
		{[]authz.Role{authz.HROfficer}, 200, 403}, {[]authz.Role{authz.PayrollAdmin}, 200, 403}, {[]authz.Role{authz.Administrator}, 200, 403},
		{[]authz.Role{authz.HRAdmin}, 200, 204}, {[]authz.Role{authz.Owner}, 200, 204},
	} {
		if got := do(tc.roles, "GET", "/v1/features", ""); got != tc.list {
			t.Errorf("%v listing: %d, want %d", tc.roles, got, tc.list)
		}
		if got := do(tc.roles, "PUT", "/v1/features/leave", `{"state":"on"}`); got != tc.set {
			t.Errorf("%v switching: %d, want %d", tc.roles, got, tc.set)
		}
	}
}

// The restricted people data is reached through the right roles only: the NRIC is set by the HR officer and administrator, passes are listed for the people
// team, a bank account is proposed by the person, decided by an approver, and confirmed only with a fresh passkey (NM-PPL-02 to NM-PPL-04).
// req: NM-PPL-02, NM-PPL-04
// asvs: V8.2.1, V8.3.2
func TestRestrictedPeopleDataIsReachedByTheRightRolesAndConfirmedWithAPasskey(t *testing.T) {
	const id = "/v1/people/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"
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
	employee, officer, admin := []authz.Role{authz.Employee}, []authz.Role{authz.HROfficer}, []authz.Role{authz.HRAdmin}
	const nric = `{"nric":"S1234567D"}`
	const pass = `{"pass_type":"employment_pass","number":"EP12345678","issued_on":"2026-01-01","expires_on":"2027-12-31"}`
	const decide = `{"approve":true}`
	for _, tc := range []struct {
		name         string
		roles        []authz.Role
		stepUp       bool
		method, path string
		body         string
		want         int
	}{
		{"setting an NRIC, an employee", employee, false, "PUT", id + "/nric", nric, 403},
		{"setting an NRIC, the HR officer", officer, false, "PUT", id + "/nric", nric, 204},
		{"setting an NRIC, the HR administrator", admin, false, "PUT", id + "/nric", nric, 204},
		{"reading a secret, an employee", employee, false, "GET", id + "/secrets/nric", "", 200},
		{"adding a pass, an employee", employee, false, "POST", id + "/work-passes", pass, 403},
		{"adding a pass, the HR officer", officer, false, "POST", id + "/work-passes", pass, 201},
		{"listing passes, an employee", employee, false, "GET", id + "/work-passes", "", 403},
		{"listing passes, the Payroll administrator", []authz.Role{authz.PayrollAdmin}, false, "GET", id + "/work-passes", "", 200},
		{"adding a document, an employee", employee, false, "POST", id + "/documents", `{"doc_type":"contract","title":"C","retention_class":"x","content_type":"application/pdf","content_base64":"eA==","viewers":["hr_admin"]}`, 403},
		{"adding a document, the HR administrator", admin, false, "POST", id + "/documents", `{"doc_type":"contract","title":"C","retention_class":"x","content_type":"application/pdf","content_base64":"eA==","viewers":["hr_admin"]}`, 201},
		{"a document that is not base64", admin, false, "POST", id + "/documents", `{"doc_type":"contract","title":"C","retention_class":"x","content_type":"application/pdf","content_base64":"!!","viewers":["hr_admin"]}`, 422},
		{"confirming a bank account with no passkey", employee, false, "POST", "/v1/bank-account-changes/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e73/confirm", "", 403},
		{"confirming a bank account with a fresh passkey", employee, true, "POST", "/v1/bank-account-changes/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e73/confirm", "", 204},
		{"proposing a bank account", employee, false, "POST", id + "/bank-account-changes", `{"account":"1234567890"}`, 201},
		{"deciding a bank account change", admin, false, "POST", "/v1/bank-account-changes/0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e73/decision", decide, 204},
	} {
		if got := do(tc.roles, tc.stepUp, tc.method, tc.path, tc.body); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
	// A deployment with no key service answers in plain words.
	restrictedErr = ErrUnavailable
	defer func() { restrictedErr = nil }()
	if got := do(admin, false, "PUT", id+"/nric", nric); got != http.StatusServiceUnavailable {
		t.Errorf("no key service: %d", got)
	}
}

// Packs, policy and checks are seen by whoever may see the settings and changed only by the HR administrator and the Owner; a policy below the statute is
// refused with its code (NM-PACK-04).
// req: NM-PACK-04
// asvs: V8.2.1
func TestOnlyTheHRAdministratorAndTheOwnerChangePacksAndPolicies(t *testing.T) {
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
	for _, tc := range []struct {
		roles       []authz.Role
		read, write int
	}{
		{[]authz.Role{authz.Employee}, 403, 403}, {[]authz.Role{authz.LineManager}, 403, 403}, {[]authz.Role{authz.HROfficer}, 200, 403},
		{[]authz.Role{authz.PayrollAdmin}, 200, 403}, {[]authz.Role{authz.HRAdmin}, 200, 204}, {[]authz.Role{authz.Owner}, 200, 204},
	} {
		for _, p := range []string{"/v1/packs", "/v1/policy", "/v1/checks"} {
			if got := do(tc.roles, "GET", p, ""); got != tc.read {
				t.Errorf("%v reading %s: %d, want %d", tc.roles, p, got, tc.read)
			}
		}
		if got := do(tc.roles, "POST", "/v1/packs/sg-people@2026.1/acceptances", ""); got != map[int]int{204: 201, 403: 403}[tc.write] {
			t.Errorf("%v accepting: %d", tc.roles, got)
		}
		if got := do(tc.roles, "PUT", "/v1/policy", policyBody); got != map[int]int{204: 200, 403: 403}[tc.write] {
			t.Errorf("%v setting a policy: %d", tc.roles, got)
		}
	}
}
