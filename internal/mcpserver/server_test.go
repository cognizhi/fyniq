package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/mcpsurface"
	"github.com/cognizhi/fyniq/internal/ports"
)

const (
	resource = "http://localhost:8080/mcp"
	issuer   = "http://localhost:8080/oauth2/"
	secret   = "opaque-token-that-must-never-be-logged-0123456789"
)

type fakeVerifier struct {
	tok Token
	err error
	saw string
}

func (f *fakeVerifier) Verify(_ context.Context, bearer string) (Token, error) {
	f.saw = bearer
	return f.tok, f.err
}

type fakeBackend struct {
	calls []Call
	res   Result
	err   error
}

func (f *fakeBackend) CallTool(_ context.Context, c Call) (Result, error) {
	f.calls = append(f.calls, c)
	return f.res, f.err
}

type rig struct {
	t   *testing.T
	v   *fakeVerifier
	b   *fakeBackend
	log *bytes.Buffer
	srv *httptest.Server
}

func newRig(t *testing.T, scopes ...string) *rig {
	t.Helper()
	m, _ := mcpsurface.Default()
	r := &rig{t: t, v: &fakeVerifier{tok: Token{Subject: "user-1", ClientID: "claude-code", Scopes: scopes}}, b: &fakeBackend{}, log: &bytes.Buffer{}}
	s, err := New(Config{Resource: resource, Issuer: issuer, Origins: []string{"http://localhost:8080"}, Manifest: m, Verify: r.v, Calls: r.b,
		Log: slog.New(slog.NewJSONHandler(r.log, nil)), Version: "0.0.0-test", MaxBody: 4096})
	if err != nil {
		t.Fatal(err)
	}
	r.srv = httptest.NewServer(s.Handler())
	t.Cleanup(r.srv.Close)
	return r
}

// modern is a well-formed request of the revision in force: its headers and its _meta agree.
func modern(method, name string, params map[string]any) (map[string]string, string) {
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{metaVersion: ProtocolVersion, "io.modelcontextprotocol/clientInfo": map[string]any{"name": "claude-code", "version": "2.1.327"}}
	if name != "" {
		params["name"] = name
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	h := map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream", "Authorization": "Bearer " + secret,
		"MCP-Protocol-Version": ProtocolVersion, "Mcp-Method": method}
	if name != "" {
		h["Mcp-Name"] = name
	}
	return h, string(body)
}

func (r *rig) do(method, path string, headers map[string]string, body string) (*http.Response, string) {
	r.t.Helper()
	req, err := http.NewRequest(method, r.srv.URL+path, strings.NewReader(body))
	if err != nil {
		r.t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (r *rig) rpc(method, name string, params map[string]any) (*http.Response, map[string]any) {
	r.t.Helper()
	h, body := modern(method, name, params)
	resp, raw := r.do(http.MethodPost, "/mcp", h, body)
	var out map[string]any
	_ = json.Unmarshal([]byte(raw), &out)
	return resp, out
}

func errorCode(out map[string]any) float64 {
	e, _ := out["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

// A request is refused before anything else when its Origin is not allowed (DNS rebinding, AI-39); a client that is no
// browser sends no Origin and is not refused for it, and only POST reaches the endpoint: there is no GET stream and no
// session to delete.
// req: AI-39
// req: QA-MCP-01
// asvs: V3.7.1, V4.1.1
func TestBadOriginsAndVerbsAreRefused(t *testing.T) {
	r := newRig(t, "fyniq.read.reports")
	h, body := modern("tools/list", "", nil)
	h["Origin"] = "https://evil.example"
	if resp, raw := r.do(http.MethodPost, "/mcp", h, body); resp.StatusCode != http.StatusForbidden || !strings.Contains(raw, CodeOrigin) {
		t.Errorf("a bad Origin: %d %s", resp.StatusCode, raw)
	}
	h["Origin"] = "null"
	if resp, _ := r.do(http.MethodPost, "/mcp", h, body); resp.StatusCode != http.StatusForbidden {
		t.Errorf("Origin: null: %d", resp.StatusCode)
	}
	h["Origin"] = "http://localhost:8080"
	if resp, _ := r.do(http.MethodPost, "/mcp", h, body); resp.StatusCode != http.StatusOK {
		t.Errorf("an allowed Origin: %d", resp.StatusCode)
	}
	delete(h, "Origin")
	if resp, _ := r.do(http.MethodPost, "/mcp", h, body); resp.StatusCode != http.StatusOK {
		t.Errorf("no Origin (a native client): %d", resp.StatusCode)
	}
	if r.v.saw == "" {
		t.Error("the verifier was never asked")
	}
	for _, verb := range []string{http.MethodGet, http.MethodDelete, http.MethodPut, http.MethodPatch} {
		h["Last-Event-ID"] = "5"
		resp, _ := r.do(verb, "/mcp", h, "")
		if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != http.MethodPost {
			t.Errorf("%s: %d, want 405 allowing POST only", verb, resp.StatusCode)
		}
	}
	// A bad Origin is refused before the token is looked at: the verifier is not asked for it.
	r.v.saw = ""
	h["Origin"] = "https://evil.example"
	r.do(http.MethodPost, "/mcp", h, body)
	if r.v.saw != "" {
		t.Error("a request with a bad Origin reached the token check")
	}
}

// Without a valid token the server answers 401 with the metadata the client discovers the OAuth server from (RFC 9728) and
// the minimal scope; a token that is unknown, expired, revoked or for another audience gets the same answer and is never told
// which; a token in the URL is refused; and if the OAuth server cannot be asked the call fails closed (AI-36).
// req: AI-36
// req: QA-MCP-01
// asvs: V10.3.1, V10.4.6, V9.2.1
func TestAnUnauthenticatedCallGetsTheDiscoveryChallenge(t *testing.T) {
	r := newRig(t, "fyniq.read.reports")
	h, body := modern("tools/list", "", nil)
	delete(h, "Authorization")
	resp, raw := r.do(http.MethodPost, "/mcp", h, body)
	want := `Bearer resource_metadata="http://localhost:8080/.well-known/oauth-protected-resource/mcp", scope="fyniq.read.reports"`
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") != want || !strings.Contains(raw, CodeAuth) {
		t.Errorf("no token: %d %q %s", resp.StatusCode, resp.Header.Get("WWW-Authenticate"), raw)
	}
	r.v.err = ErrInvalidToken
	h["Authorization"] = "Bearer " + secret
	resp, raw = r.do(http.MethodPost, "/mcp", h, body)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(resp.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Errorf("an invalid token: %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	for _, leak := range []string{"expired", "revoked", "audience", "unknown", secret} {
		if strings.Contains(strings.ToLower(raw), leak) {
			t.Errorf("the refusal tells a probe %q: %s", leak, raw)
		}
	}
	for _, bad := range []string{"Basic abc", "Bearer", "bearer  two words", "Bearer " + strings.Repeat("x", 5000)} {
		h["Authorization"] = bad
		if resp, _ := r.do(http.MethodPost, "/mcp", h, body); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("Authorization %.20q: %d", bad, resp.StatusCode)
		}
	}
	h["Authorization"] = "Bearer " + secret
	if resp, _ := r.do(http.MethodPost, "/mcp?access_token="+secret, h, body); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a token in the URL: %d", resp.StatusCode)
	}
	r.v.err = errors.New("hydra is down")
	if resp, _ := r.do(http.MethodPost, "/mcp", h, body); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("the OAuth server down: %d, want 503 (fail closed)", resp.StatusCode)
	}
	if strings.Contains(r.log.String(), secret) {
		t.Error("the token is in the log (SEC-15)")
	}
}

// The protocol headers must match the body, as the revision requires (-32020), the revision must be the one served
// (-32022 with the versions supported), a legacy initialize is told what is served, a batch and an oversized body are
// refused, and the Base64 sentinel form of a header is decoded before the comparison (AI-39).
// req: AI-39
// req: QA-MCP-01
// asvs: V4.1.1, V13.2.1
func TestTheProtocolHeadersAreCheckedAgainstTheBody(t *testing.T) {
	r := newRig(t, "fyniq.read.reports")
	post := func(mutate func(h map[string]string, params map[string]any)) (*http.Response, map[string]any) {
		params := map[string]any{}
		h, _ := modern("tools/call", "metrics.query", params)
		mutate(h, params)
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": params})
		resp, raw := r.do(http.MethodPost, "/mcp", h, string(body))
		var out map[string]any
		_ = json.Unmarshal([]byte(raw), &out)
		return resp, out
	}
	meta := func(v string) map[string]any { return map[string]any{metaVersion: v} }
	cases := map[string]struct {
		mutate func(map[string]string, map[string]any)
		status int
		code   float64
	}{
		"no version header": {func(h map[string]string, p map[string]any) {
			delete(h, "MCP-Protocol-Version")
			p["_meta"] = meta(ProtocolVersion)
		}, 400, rpcHeaderMismatch},
		"another version": {func(h map[string]string, p map[string]any) {
			h["MCP-Protocol-Version"] = "2025-11-25"
			p["_meta"] = meta("2025-11-25")
		}, 400, rpcBadVersion},
		"meta differs": {func(h map[string]string, p map[string]any) { p["_meta"] = meta("2025-11-25") }, 400, rpcHeaderMismatch},
		"no meta":      {func(h map[string]string, p map[string]any) { delete(p, "_meta") }, 400, rpcHeaderMismatch},
		"method header differs": {func(h map[string]string, p map[string]any) {
			p["_meta"] = meta(ProtocolVersion)
			h["Mcp-Method"] = "tools/list"
		}, 400, rpcHeaderMismatch},
		"no method header": {func(h map[string]string, p map[string]any) {
			p["_meta"] = meta(ProtocolVersion)
			delete(h, "Mcp-Method")
		}, 400, rpcHeaderMismatch},
		"name header differs": {func(h map[string]string, p map[string]any) {
			p["_meta"] = meta(ProtocolVersion)
			h["Mcp-Name"] = "records.get"
		}, 400, rpcHeaderMismatch},
		"no name header": {func(h map[string]string, p map[string]any) { p["_meta"] = meta(ProtocolVersion); delete(h, "Mcp-Name") }, 400, rpcHeaderMismatch},
		"a bad base64 name": {func(h map[string]string, p map[string]any) {
			p["_meta"] = meta(ProtocolVersion)
			h["Mcp-Name"] = "=?base64?!!!?="
		}, 400, rpcHeaderMismatch},
	}
	for name, c := range cases {
		resp, out := post(c.mutate)
		if resp.StatusCode != c.status || errorCode(out) != c.code {
			t.Errorf("%s: %d %v, want %d %v", name, resp.StatusCode, out, c.status, c.code)
		}
	}
	// The unsupported version names what is supported.
	_, out := post(cases["another version"].mutate)
	data := out["error"].(map[string]any)["data"].(map[string]any)
	if got := data["supported"].([]any); len(got) != 1 || got[0] != ProtocolVersion || data["requested"] != "2025-11-25" {
		t.Errorf("the unsupported-version error's data: %v", data)
	}
	// The Base64 sentinel decodes to the same name and is accepted.
	resp, out := post(func(h map[string]string, p map[string]any) {
		p["_meta"] = meta(ProtocolVersion)
		h["Mcp-Name"] = "=?base64?" + base64.StdEncoding.EncodeToString([]byte("metrics.query")) + "?="
	})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("a Base64 Mcp-Name: %d %v", resp.StatusCode, out)
	}

	// A legacy client's initialize is told which revision is served and gets no session.
	h := map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + secret}
	resp, raw := r.do(http.MethodPost, "/mcp", h, `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}`)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(raw, ProtocolVersion) || resp.Header.Get("Mcp-Session-Id") != "" {
		t.Errorf("initialize: %d %s", resp.StatusCode, raw)
	}
	// A batch, a body that is not JSON-RPC, a non-JSON content type and a body over the limit are refused.
	h, _ = modern("tools/list", "", nil)
	for name, body := range map[string]string{"a batch": `[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`, "not JSON-RPC": `{"hello":1}`,
		"two messages": `{"jsonrpc":"2.0","id":1,"method":"ping"}{"jsonrpc":"2.0","id":2,"method":"ping"}`, "not JSON": `nope`} {
		if resp, _ := r.do(http.MethodPost, "/mcp", h, body); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d", name, resp.StatusCode)
		}
	}
	if resp, _ := r.do(http.MethodPost, "/mcp", h, `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"`+strings.Repeat("a", 5000)+`"}}`); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("an oversized body: %d", resp.StatusCode)
	}
	h["Content-Type"] = "text/plain"
	if resp, _ := r.do(http.MethodPost, "/mcp", h, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("a text body: %d", resp.StatusCode)
	}
	// A notification is accepted and runs nothing.
	h["Content-Type"] = "application/json"
	if resp, raw := r.do(http.MethodPost, "/mcp", h, `{"jsonrpc":"2.0","method":"notifications/anything"}`); resp.StatusCode != http.StatusAccepted || raw != "" {
		t.Errorf("a notification: %d %q", resp.StatusCode, raw)
	}
	// An unknown method is 404 with -32601, as the revision says.
	if resp, out := r.rpc("resources/list", "", nil); resp.StatusCode != http.StatusNotFound || errorCode(out) != rpcNoMethod {
		t.Errorf("an unknown method: %d %v", resp.StatusCode, out)
	}
}

// A session ID a client sends is ignored: it grants nothing, it is never echoed and none is minted, so there is nothing to
// fixate or hijack, and the token is what is checked on every request (AI-39).
// req: AI-39
// req: QA-MCP-01
// asvs: V7.2.1, V7.4.1
func TestASessionIDIsIgnoredAndNeverMinted(t *testing.T) {
	r := newRig(t, "fyniq.read.reports")
	h, body := modern("tools/list", "", nil)
	h["Mcp-Session-Id"] = "attacker-chosen-session"
	resp, _ := r.do(http.MethodPost, "/mcp", h, body)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Mcp-Session-Id") != "" {
		t.Errorf("a session ID: %d, echoed %q", resp.StatusCode, resp.Header.Get("Mcp-Session-Id"))
	}
	delete(h, "Authorization")
	if resp, _ := r.do(http.MethodPost, "/mcp", h, body); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a session ID without a token: %d, want 401: a session is never authentication", resp.StatusCode)
	}
	resp, _ = r.do(http.MethodPost, "/mcp", map[string]string{"Authorization": "Bearer " + secret, "Content-Type": "application/json", "MCP-Protocol-Version": ProtocolVersion, "Mcp-Method": "ping"},
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"_meta":{"`+metaVersion+`":"`+ProtocolVersion+`"}}}`)
	if resp.Header.Get("Mcp-Session-Id") != "" {
		t.Error("the server minted a session ID")
	}
}

// server/discover names the one revision served and the tools capability alone: no resources, prompts, logging, sampling or
// elicitation, and no list-changed notifications. Its text is static (AI-40, AI-42).
// req: AI-39
// req: AI-42
// req: AI-40
func TestDiscoverNamesWhatIsServedAndOffersNoClientFeatures(t *testing.T) {
	r := newRig(t)
	resp, out := r.rpc("server/discover", "", nil)
	res, _ := out["result"].(map[string]any)
	caps, _ := res["capabilities"].(map[string]any)
	if resp.StatusCode != http.StatusOK || res["resultType"] != "complete" || len(caps) != 1 || caps["tools"] == nil {
		t.Fatalf("discover: %d %v", resp.StatusCode, out)
	}
	if v, _ := res["supportedVersions"].([]any); len(v) != 1 || v[0] != ProtocolVersion {
		t.Errorf("supportedVersions: %v", v)
	}
	if tools := caps["tools"].(map[string]any); len(tools) != 0 {
		t.Errorf("the tools capability promises more than tools: %v", tools)
	}
	if !strings.Contains(res["instructions"].(string), "cannot approve, pay, export or change anything") {
		t.Error("the instructions do not say what the server cannot do")
	}
	// tools/call never takes the client's answer to a question this server never asks.
	for _, k := range []string{"inputResponses", "requestState"} {
		if _, out := r.rpc("tools/call", "metrics.query", map[string]any{k: map[string]any{"x": 1}}); errorCode(out) != rpcInvalidParams {
			t.Errorf("tools/call with %s: %v", k, out)
		}
	}
}

// tools/list shows a token the tools its scopes allow and no others, from the pinned manifest, in its order, with nothing the
// manifest does not hold; a token with no matching scope sees none, and the never-list's tools are not there at all (AI-37,
// AI-40, CTL-MCP-02).
// req: AI-40
// req: AI-37
// req: CTL-MCP-02
// req: QA-MCP-01
// asvs: V8.2.1
func TestAPersonSeesOnlyTheToolsTheirScopesAllow(t *testing.T) {
	names := func(r *rig) []string {
		_, out := r.rpc("tools/list", "", nil)
		var got []string
		for _, tl := range out["result"].(map[string]any)["tools"].([]any) {
			got = append(got, tl.(map[string]any)["name"].(string))
		}
		return got
	}
	if got := names(newRig(t, "fyniq.read.reports")); !slices.Equal(got, []string{"lineage.explain", "metrics.query"}) {
		t.Errorf("reports only: %v", got)
	}
	if got := names(newRig(t, "fyniq.read.purchases")); !slices.Contains(got, "records.get") || slices.Contains(got, "checks.list") || slices.Contains(got, "calc") {
		t.Errorf("purchases only: %v", got)
	}
	if got := names(newRig(t, "openid", "offline_access")); len(got) != 0 {
		t.Errorf("no read scope: %v", got)
	}
	if got := names(newRig(t, "fyniq.read.payroll", "fyniq.read.equity")); len(got) != 0 {
		t.Errorf("payroll and equity: %v", got)
	}
	all := names(newRig(t, "fyniq.read.reports", "fyniq.read.sales", "fyniq.read.purchases", "fyniq.read.accounting", "fyniq.read.compliance", "fyniq.read.proposals"))
	m, _ := mcpsurface.Default()
	proposes := 0
	for _, d := range m.Tools {
		if strings.HasPrefix(d.Name, "propose_") {
			proposes++
		}
	}
	if proposes == 0 || len(all) != len(m.Tools)-proposes {
		t.Errorf("every read scope: %v of %d tools, %d of them proposals", all, len(m.Tools), proposes)
	}
	for _, n := range all {
		if strings.HasPrefix(n, "propose_") {
			t.Errorf("%s is listed to a token with no propose scope", n)
		} else if !mcpsurface.IsProposalTool(n) {
			if err := aitool.ReadName(n); err != nil {
				t.Errorf("%s is listed: %v", n, err)
			}
		}
	}
	// A propose scope lists the tool of its task type and no other (AI-16, AI-37).
	if got := names(newRig(t, "fyniq.propose.sec.create_task")); !slices.Equal(got, []string{"propose_sec_create_task"}) {
		t.Errorf("a propose scope: %v", got)
	}
	// What is shown is what the manifest pins, word for word.
	r := newRig(t, "fyniq.read.reports")
	_, out := r.rpc("tools/list", "", nil)
	for _, tl := range out["result"].(map[string]any)["tools"].([]any) {
		got := tl.(map[string]any)
		for _, d := range m.Tools {
			if d.Name == got["name"] && (d.Description != got["description"] || d.Title != got["title"]) {
				t.Errorf("%s is not shown as the manifest pins it", d.Name)
			}
		}
		if _, ok := got["sha256"]; ok {
			t.Errorf("%s leaks the pin", got["name"])
		}
	}
}

// tools/call reaches the Backend only for a tool the token may see, and an unknown tool and one the token cannot use are
// answered alike; the client's token goes to the Backend for its own delegation and is never in a log or a result; the
// Backend's refusals become the HTTP answers the revision defines: 403 with an insufficient_scope challenge, 429 with
// Retry-After, 401 challenge, and 503 when the data path fails (AI-16, AI-36, AI-43).
// req: AI-16
// req: AI-36
// req: AI-43
// req: QA-MCP-01
// asvs: V8.2.1, V16.3.2
func TestACallReachesTheBackendOnlyForAToolTheTokenMayUse(t *testing.T) {
	r := newRig(t, "fyniq.read.reports")
	r.b.res = Result{Structured: json.RawMessage(`{"facts":[]}`), Text: `{"facts":[]}`}
	resp, out := r.rpc("tools/call", "metrics.query", map[string]any{"arguments": map[string]any{"measure": "revenue"}})
	res, _ := out["result"].(map[string]any)
	if resp.StatusCode != http.StatusOK || res["isError"] != false || res["structuredContent"] == nil || len(r.b.calls) != 1 {
		t.Fatalf("a call: %d %v", resp.StatusCode, out)
	}
	c := r.b.calls[0]
	if c.Tool != "metrics.query" || c.Args["measure"] != "revenue" || c.Token.ClientID != "claude-code" || c.Bearer != secret {
		t.Errorf("what the backend got: %+v", c)
	}
	_, unknown := r.rpc("tools/call", "ledger.post", nil)
	_, hidden := r.rpc("tools/call", "records.get", map[string]any{"arguments": map[string]any{"kind": "bill"}}) // purchases: not held
	_, calc := r.rpc("tools/call", "calc", nil)
	for name, out := range map[string]map[string]any{"unknown": unknown, "not allowed": hidden, "not exposed": calc} {
		if errorCode(out) != rpcInvalidParams {
			t.Errorf("%s: %v", name, out)
		}
	}
	if m1, m2 := unknown["error"].(map[string]any)["message"].(string), hidden["error"].(map[string]any)["message"].(string); strings.TrimPrefix(m1, "Unknown tool: ledger.post") != strings.TrimPrefix(m2, "Unknown tool: records.get") {
		t.Errorf("an unknown tool and one the token cannot use are told apart: %q %q", m1, m2)
	}
	if len(r.b.calls) != 1 {
		t.Errorf("the backend was reached for a tool the token may not use: %d calls", len(r.b.calls))
	}

	r.b.err = &Refusal{Status: 403, Code: "AI-TOOL-03", Detail: "Needs a scope", Fix: "Allow it", Scopes: []string{"fyniq.read.sales"}}
	resp, _ = r.rpc("tools/call", "metrics.query", nil)
	if resp.StatusCode != 403 || !strings.Contains(resp.Header.Get("WWW-Authenticate"), `error="insufficient_scope", scope="fyniq.read.sales"`) {
		t.Errorf("an insufficient scope: %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	r.b.err = &Refusal{Status: 429, Code: "MCP-LIMIT-01", Detail: "Too many", Fix: "Wait", RetryAfter: 1500 * time.Millisecond}
	if resp, _ = r.rpc("tools/call", "metrics.query", nil); resp.StatusCode != 429 || resp.Header.Get("Retry-After") != "2" {
		t.Errorf("a limit: %d Retry-After %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	r.b.err = &Refusal{Status: 401, Code: "x", Detail: "x", Fix: "x"}
	if resp, _ = r.rpc("tools/call", "metrics.query", nil); resp.StatusCode != 401 || resp.Header.Get("WWW-Authenticate") == "" {
		t.Errorf("a refused token: %d", resp.StatusCode)
	}
	r.b.err = errors.New("the API is down")
	if resp, _ = r.rpc("tools/call", "metrics.query", nil); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("the data path failing: %d", resp.StatusCode)
	}
	if strings.Contains(r.log.String(), secret) || strings.Contains(r.log.String(), `"measure"`) {
		t.Error("a log line holds the token or the arguments (SEC-15)")
	}
}

// The protected-resource metadata names the resource, the OAuth server and the minimal scope (RFC 9728, AI-36), at the root
// well-known path and the one suffixed with the endpoint's path.
// req: AI-36
// asvs: V10.1.1
func TestTheProtectedResourceMetadataIsPublished(t *testing.T) {
	r := newRig(t)
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		resp, raw := r.do(http.MethodGet, path, nil, "")
		var m map[string]any
		_ = json.Unmarshal([]byte(raw), &m)
		as, _ := m["authorization_servers"].([]any)
		scopes, _ := m["scopes_supported"].([]any)
		if resp.StatusCode != 200 || m["resource"] != resource || len(as) != 1 || as[0] != issuer || len(scopes) != 1 || scopes[0] != "fyniq.read.reports" {
			t.Errorf("%s: %d %s", path, resp.StatusCode, raw)
		}
	}
	if resp, _ := r.do(http.MethodPost, "/.well-known/oauth-protected-resource", nil, ""); resp.StatusCode == 200 {
		t.Error("the metadata answered a POST")
	}
}

// The server is built only for a canonical resource URL and with what it needs; a manifest with no tools is refused.
// req: AI-36
func TestTheServerIsBuiltOnlyForACanonicalResource(t *testing.T) {
	m, _ := mcpsurface.Default()
	ok := Config{Resource: resource, Issuer: issuer, Manifest: m, Verify: &fakeVerifier{}, Calls: &fakeBackend{}, Log: slog.New(slog.DiscardHandler)}
	if _, err := New(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"mcp.example.com", "http://mcp.example.com/mcp", "https://mcp.example.com/mcp#frag", "https://mcp.example.com/mcp?x=1", "ftp://x/mcp", ""} {
		c := ok
		c.Resource = bad
		if _, err := New(c); err == nil {
			t.Errorf("%q was accepted as the MCP resource", bad)
		}
	}
	if _, err := New(Config{Resource: "https://mcp.example.com/mcp", Issuer: issuer, Manifest: m, Verify: &fakeVerifier{}, Calls: &fakeBackend{}, Log: slog.New(slog.DiscardHandler)}); err != nil {
		t.Errorf("an https resource: %v", err)
	}
	c := ok
	c.Manifest = mcpsurface.Manifest{}
	if _, err := New(c); err == nil {
		t.Error("a server with no tools was built")
	}
	c = ok
	c.Verify = nil
	if _, err := New(c); err == nil {
		t.Error("a server with no verifier was built")
	}
}

type fakeOAuth struct{ in ports.Introspection }

func (f fakeOAuth) Introspect(context.Context, string) (ports.Introspection, error) { return f.in, nil }

type frozen struct{ t time.Time }

func (f frozen) Now() time.Time { return f.t }

// Only a token issued for this server alone, to an agent-class client, live now and consented within 90 days is accepted:
// not one for another audience, not one that is also good for the API (it could be passed through), not an expired, inactive
// or user-class one, and not one whose consent is older than 90 days (AI-36, AI-34, AI-37).
// req: AI-36
// req: AI-34
// req: AI-37
// req: QA-MCP-01
// asvs: V10.3.1, V10.3.2, V10.4.6
func TestOnlyATokenForThisServerIsAccepted(t *testing.T) {
	now := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	good := ports.Introspection{Active: true, Subject: "user-1", ClientID: "claude-code", Audiences: []string{resource}, Scopes: []string{"fyniq.read.reports"},
		ExpiresAt: now.Add(time.Hour), Claims: map[string]any{"class": "agent", "org": "org-1", "granted_at": float64(now.Add(-24 * time.Hour).Unix())}}
	verify := func(mutate func(*ports.Introspection)) error {
		in := good
		in.Claims = map[string]any{}
		for k, v := range good.Claims {
			in.Claims[k] = v
		}
		mutate(&in)
		_, err := OAuthVerifier{OAuth: fakeOAuth{in}, Resource: resource, Clock: frozen{now}}.Verify(context.Background(), "t")
		return err
	}
	if err := verify(func(*ports.Introspection) {}); err != nil {
		t.Fatalf("a good token: %v", err)
	}
	for name, mutate := range map[string]func(*ports.Introspection){
		"inactive":              func(in *ports.Introspection) { in.Active = false },
		"another audience":      func(in *ports.Introspection) { in.Audiences = []string{"http://localhost:8080/api"} },
		"the API too":           func(in *ports.Introspection) { in.Audiences = []string{resource, "http://localhost:8080/api"} },
		"no audience":           func(in *ports.Introspection) { in.Audiences = nil },
		"a path suffix":         func(in *ports.Introspection) { in.Audiences = []string{resource + "/x"} },
		"expired":               func(in *ports.Introspection) { in.ExpiresAt = now.Add(-time.Second) },
		"no subject":            func(in *ports.Introspection) { in.Subject = "" },
		"no client":             func(in *ports.Introspection) { in.ClientID = "" },
		"a user-class client":   func(in *ports.Introspection) { in.Claims["class"] = "user" },
		"an integration client": func(in *ports.Introspection) { in.Claims["class"] = "integration" },
		"no class":              func(in *ports.Introspection) { delete(in.Claims, "class") },
		"consent past 90 days":  func(in *ports.Introspection) { in.Claims["granted_at"] = float64(now.Add(-91 * 24 * time.Hour).Unix()) },
		"no consent date":       func(in *ports.Introspection) { delete(in.Claims, "granted_at") },
	} {
		if err := verify(mutate); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("%s: %v, want the token refused", name, err)
		}
	}
	if _, err := (OAuthVerifier{OAuth: fakeOAuth{good}, Resource: resource, Clock: frozen{now}}).Verify(context.Background(), ""); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("no token: %v", err)
	}
	// 90 days to the second is still inside; a day short of that is.
	if err := verify(func(in *ports.Introspection) { in.Claims["granted_at"] = float64(now.Add(-89 * 24 * time.Hour).Unix()) }); err != nil {
		t.Errorf("a consent 89 days old: %v", err)
	}
}
