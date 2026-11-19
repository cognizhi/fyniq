package mcpregister

import (
	"strings"
	"testing"
)

func policy(t *testing.T) Policy {
	t.Helper()
	p, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func good() Request {
	return Request{ClientName: "Acme Books Assistant", RedirectURIs: []string{"https://acme.example/oauth/callback"}, GrantTypes: []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"}, TokenEndpointAuthMethod: "none", Scope: "openid fyniq.read.reports", ApplicationType: "web"}
}

func fields(ps []Problem) string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Field)
	}
	return strings.Join(out, ",")
}

// A client that registers itself becomes agent class, public, with the default read scope alone, whatever it asked for within the
// policy; a well-formed request is accepted exactly, and a Claude Code-style loopback registration is too (AI-90, AI-34, AI-37).
// req: AI-90
// req: AI-34
// req: AI-37
// asvs: V10.4.1, V10.4.4
func TestARegistrationWithinThePolicyBecomesTheNarrowestClient(t *testing.T) {
	p := policy(t)
	reg, problems := p.Check(good())
	if len(problems) != 0 || reg.Name != "Acme Books Assistant" || strings.Join(reg.Scopes, " ") != "openid offline_access fyniq.read.reports" {
		t.Fatalf("a good registration: %+v %v", reg, problems)
	}
	// It asked for no scope at all: it still gets the default and nothing more.
	noScope := good()
	noScope.Scope, noScope.GrantTypes, noScope.ResponseTypes, noScope.TokenEndpointAuthMethod = "", nil, nil, ""
	if reg, problems := p.Check(noScope); len(problems) != 0 || len(reg.Scopes) != 3 {
		t.Fatalf("a bare registration: %+v %v", reg, problems)
	}
	// What Claude Code registers itself with (the capture of task 1.2), as a loopback native client.
	cc := Request{ClientName: "Claude Code (fyniq-local)", RedirectURIs: []string{"http://localhost:3118/callback"}, GrantTypes: []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"}, TokenEndpointAuthMethod: "none", ApplicationType: "native"}
	if _, problems := p.Check(cc); len(problems) != 0 {
		t.Fatalf("Claude Code's own registration: %v", problems)
	}
	// A real Claude connector, on Anthropic's own host, may carry Anthropic's name.
	connector := Request{ClientName: "Claude", RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"}}
	if _, problems := p.Check(connector); len(problems) != 0 {
		t.Fatalf("Anthropic's own connector: %v", problems)
	}
}

// Every redirect URI is exact https on a public host the client names, or a loopback address: a wildcard, a fragment, a query,
// credentials, an IP address, an escape, a dot segment, another scheme, a look-alike host, a host on the deny list and too many
// or duplicate URIs are each refused naming the field (AI-90, consent phishing, open redirect).
// req: AI-90
// req: QA-MCP-01
// asvs: V10.4.1, V10.4.2, V10.1.2
func TestEveryRedirectIsExactOrRefused(t *testing.T) {
	p := policy(t)
	for _, u := range []string{
		"https://*.example.com/cb", "https://acme.example/cb#frag", "https://acme.example/cb?next=evil", "https://user:pw@acme.example/cb",
		"https://203.0.113.7/cb", "https://[2001:db8::1]/cb", "https://acme.example/%2e%2e/cb", "https://acme.example/a/../cb", "https://acme.example/a b",
		"javascript:alert(1)", "data:text/html,x", "file:///etc/passwd", "ftp://acme.example/cb", "claude://callback", "com.acme.app:/cb", "//acme.example/cb", "/relative",
		"http://acme.example/cb", "http://127.0.0.1.evil.example/cb", "http://localhost.evil.example/cb", "https://localhost/cb", "https://intranet/cb",
		"https://printer.local/cb", "https://acme.example", "https://acme.example./cb", "https://xn--e1afmkfd.example/cb", "https://аcme.example/cb",
		"https://abc.ngrok.io/cb", "https://evil.github.io/cb", "https://x.trycloudflare.com/cb", "http://0.0.0.0:80/cb", "http://example.com:8080/cb",
		"https://acme.example:99999/cb", "https://acme.example\\@evil.example/cb", strings.Repeat("https://a.example/", 20), "",
	} {
		r := good()
		r.RedirectURIs = []string{u}
		if _, problems := p.Check(r); len(problems) == 0 || !strings.HasPrefix(fields(problems), "redirect_uris") {
			t.Errorf("%q was accepted as a redirect URI", u)
		}
	}
	for _, u := range []string{"http://127.0.0.1:8123/callback", "http://[::1]:5000/cb", "http://localhost:3118/callback", "https://app.acme.example/oauth/cb"} {
		r := good()
		r.RedirectURIs = []string{u}
		if _, problems := p.Check(r); len(problems) != 0 {
			t.Errorf("%q was refused: %v", u, problems)
		}
	}
	r := good()
	r.RedirectURIs = nil
	if _, problems := p.Check(r); len(problems) == 0 {
		t.Error("no redirect URI was accepted")
	}
	r.RedirectURIs = []string{"https://a.example/1", "https://a.example/2", "https://a.example/3", "https://a.example/4", "https://a.example/5", "https://a.example/6"}
	if _, problems := p.Check(r); len(problems) == 0 {
		t.Error("six redirect URIs were accepted")
	}
	r.RedirectURIs = []string{"https://a.example/1", "https://a.example/1"}
	if _, problems := p.Check(r); len(problems) == 0 {
		t.Error("a duplicated redirect URI was accepted")
	}
}

// A name cannot pass for Fyniq, an app Fyniq already has, or a word of authority, however it is spelled; a brand's name only
// goes with that brand's own hosts; and a name holds plain ASCII, so no look-alike letter can stand in it (AI-90, AI-37).
// req: AI-90
// req: QA-MCP-01
// asvs: V10.4.1
func TestANameCannotImpersonate(t *testing.T) {
	p := policy(t)
	for _, name := range []string{"Fyniq", "FYNIQ Assistant", "Fyn1q", "F y n i q", "fyn-iq", "Fyn!q", "Fyniq (official)", "Official Fyniq", "Claude Code", "claude-code", "C1aude Desktop",
		"Verified Partner", "Administrator Tools", "Fуniq", "Fyniq​", "", strings.Repeat("a", 101), "Acme <script>", "Acme\nBooks", "Fyniq Bot"} {
		r := good()
		r.ClientName = name
		if _, problems := p.Check(r); len(problems) == 0 || !strings.Contains(fields(problems), "client_name") {
			t.Errorf("the name %q was accepted", name)
		}
	}
	// Claude Code's own label after its name is the user's (the server's name, which may be fyniq); the same label on another
	// name, or the first-party name on a host that is not its own, is not.
	loop := good()
	loop.RedirectURIs = []string{"http://localhost:3118/callback"}
	for name, want := range map[string]bool{"Claude Code (fyniq)": true, "Claude Code (my Fyniq server)": true, "Acme (Fyniq)": false, "Acme Fyniq (x)": false} {
		loop.ClientName = name
		if _, problems := p.Check(loop); (len(problems) == 0) != want {
			t.Errorf("%q: accepted %v, want %v: %v", name, len(problems) == 0, want, problems)
		}
	}
	evil := good()
	evil.ClientName, evil.RedirectURIs = "Claude Code (secure)", []string{"https://evil.example/cb"}
	if _, problems := p.Check(evil); len(problems) == 0 {
		t.Error("a first-party name with a label on another host was accepted")
	}
	// A brand's name on a host that is not the brand's is refused; on its own host, or loopback, accepted.
	for _, name := range []string{"Claude", "Claude for Acme", "Anthropic Connector", "C1aude"} {
		r := good()
		r.ClientName = name
		r.RedirectURIs = []string{"https://evil.example/cb"}
		if _, problems := p.Check(r); len(problems) == 0 {
			t.Errorf("%q on evil.example was accepted", name)
		}
		r.RedirectURIs = []string{"https://claude.ai/api/mcp/auth_callback", "https://evil.claude.ai.evil.example/cb"}
		if _, problems := p.Check(r); len(problems) == 0 {
			t.Errorf("%q with a look-alike host was accepted", name)
		}
	}
}

// Nothing a client names is ever fetched or trusted: keys, a key-set URL, a software statement, request URLs, a logo, a policy
// or terms URL, contacts, a secret, a client ID and every field the server does not know are refused by name before the policy
// runs; a secret-using client, other grants, other response types and any scope beyond the default are refused; a body that is
// not one object, or is too large, is refused (AI-90).
// req: AI-90
// req: QA-MCP-01
// asvs: V10.4.1, V1.3.1
func TestNothingTheClientNamesIsFetchedOrTrusted(t *testing.T) {
	p := policy(t)
	base := `{"client_name":"Acme","redirect_uris":["https://acme.example/cb"]`
	for _, extra := range []string{`"jwks_uri":"https://evil.example/keys"`, `"jwks":{"keys":[]}`, `"software_statement":"eyJ"`, `"request_uris":["https://evil.example/r"]`,
		`"sector_identifier_uri":"https://evil.example/s"`, `"logo_uri":"https://evil.example/l.png"`, `"client_uri":"https://evil.example"`,
		`"tos_uri":"https://evil.example/t"`, `"policy_uri":"https://evil.example/p"`, `"contacts":["a@b.c"]`, `"client_secret":"x"`, `"client_id":"claude-code"`,
		`"registration_access_token":"x"`, `"made_up":1`, `"jwks_uri":null`} {
		req, problems := Decode(strings.NewReader(base + "," + extra + "}"))
		if len(problems) == 0 || req.ClientName != "" {
			t.Errorf("%s was accepted", extra)
		}
	}
	if _, problems := Decode(strings.NewReader(base + `,"jwks_uri":"x"}`)); !strings.Contains(problems[0].Message, "key-set URL") {
		t.Errorf("the refusal does not say what it is: %v", problems)
	}
	for _, body := range []string{`[]`, `null`, `"x"`, `{"client_name":5}`, `{"redirect_uris":"https://acme.example/cb"}`, `not json`, ``, `{"client_name":"A"} {"client_name":"B"}`} {
		if _, problems := Decode(strings.NewReader(body)); len(problems) == 0 && body != `{"client_name":"A"} {"client_name":"B"}` {
			t.Errorf("%q was accepted", body)
		}
	}
	if _, problems := Decode(strings.NewReader(base + `,"client_name_x":"` + strings.Repeat("a", MaxBody) + `"}`)); len(problems) == 0 {
		t.Error("a body over 8 KiB was accepted")
	}
	if req, problems := Decode(strings.NewReader(base + `}`)); len(problems) != 0 || req.ClientName != "Acme" {
		t.Errorf("a plain registration: %v", problems)
	}
	for name, mutate := range map[string]func(*Request){
		"a secret":            func(r *Request) { r.TokenEndpointAuthMethod = "client_secret_basic" },
		"a key":               func(r *Request) { r.TokenEndpointAuthMethod = "private_key_jwt" },
		"the implicit grant":  func(r *Request) { r.GrantTypes = []string{"implicit"} },
		"the password grant":  func(r *Request) { r.GrantTypes = []string{"authorization_code", "password"} },
		"client credentials":  func(r *Request) { r.GrantTypes = []string{"client_credentials"} },
		"a token response":    func(r *Request) { r.ResponseTypes = []string{"token"} },
		"a hybrid response":   func(r *Request) { r.ResponseTypes = []string{"code", "id_token"} },
		"payroll":             func(r *Request) { r.Scope = "openid fyniq.read.payroll" },
		"another read area":   func(r *Request) { r.Scope = "openid fyniq.read.sales" },
		"a propose scope":     func(r *Request) { r.Scope = "fyniq.propose.sec.create_task" },
		"an admin scope":      func(r *Request) { r.Scope = "admin" },
		"an application type": func(r *Request) { r.ApplicationType = "service" },
	} {
		r := good()
		mutate(&r)
		if _, problems := p.Check(r); len(problems) == 0 {
			t.Errorf("%s was accepted", name)
		}
	}
	// Every problem comes at once, with a plain fix.
	bad := Request{ClientName: "Fyniq", RedirectURIs: []string{"http://evil.example/cb"}, Scope: "fyniq.read.payroll"}
	_, problems := p.Check(bad)
	if len(problems) < 3 {
		t.Errorf("not every problem at once: %v", problems)
	}
	for _, pr := range problems {
		if pr.Field == "" || pr.Message == "" || pr.Fix == "" || (pr.Code != InvalidMetadata && pr.Code != InvalidRedirect) {
			t.Errorf("a problem without its field, words or code: %+v", pr)
		}
	}
}
