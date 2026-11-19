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
	"github.com/cognizhi/fyniq/internal/nemura/authz"
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
	token string
	roles []authz.Role
}

func (f fakeSessions) Authenticate(_ context.Context, token string, _ time.Time) (Session, error) {
	if token != f.token || token == "" {
		return Session{}, errors.New("no such session")
	}
	return Session{Organisation: uuid.MustParse("0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44"), Member: uuid.MustParse("0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45")}, nil
}

func (f fakeSessions) RolesAt(context.Context, uuid.UUID, uuid.UUID, time.Time) ([]authz.Role, error) {
	return f.roles, nil
}

func testAPI(t *testing.T) (*API, *http.ServeMux) {
	t.Helper()
	mux := http.NewServeMux()
	d := Deps{Sessions: fakeSessions{token: "good-token", roles: []authz.Role{authz.HRAdmin}}, Clock: clock.NewFrozen(time.Date(2099, 1, 1, 9, 0, 0, 0, time.UTC)),
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
func TestEveryCodeInTheCatalogueIsTriggered(t *testing.T) {
	_, mux := testAPI(t)
	triggers := map[string]string{ // code -> a request that returns it
		CodeNotFound:        "/v1/errors/NM_NOPE",
		CodeInvalid:         "/v1/errors/not-a-code",
		CodeUnauthenticated: "/v1/session",
	}
	for _, c := range Catalogue {
		path, ok := triggers[c.Code]
		if !ok {
			t.Errorf("%s is in the catalogue and no test triggers it", c.Code)
			continue
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
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
