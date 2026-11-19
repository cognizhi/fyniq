// Package mcpregister is the policy of client registration (PRD-001 AI-90; p1-mcp-first-party design D14): what a client that
// registers itself may ask for. The registration endpoint (the API's) is closed until an Owner enables it, limits requests by
// IP and calls the OAuth server's admin API only with what Check returns. The policy is deliberately narrow and refuses what it
// does not know: a client is public, uses the authorisation code with PKCE, and gets the default read scope alone, in the agent
// class; its redirect URIs are exact https or loopback; nothing it sends is ever fetched or trusted (no keys, no key-set URL,
// no software statement, no logo or policy URL); and its name cannot pass for Fyniq or, off its own hosts, for a known brand.
package mcpregister

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"

	contracts "github.com/cognizhi/fyniq/contracts/ai"
	"github.com/cognizhi/fyniq/internal/mcpgate"
)

// RFC 7591's error codes.
const (
	InvalidMetadata = "invalid_client_metadata"
	InvalidRedirect = "invalid_redirect_uri"
)

// Problem is one thing wrong with a registration, in plain words: the field, what is wrong and how to fix it (ENG-23).
type Problem struct {
	Code    string `json:"error"` // RFC 7591's error code
	Field   string `json:"field"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
}

func (p Problem) Error() string { return p.Field + ": " + p.Message }

// Policy is contracts/ai/mcp-registration-policy.json, with the first-party apps' names it must not be mistaken for.
type Policy struct {
	DefaultScopes    []string            `json:"default_scopes"`
	MaxRedirectURIs  int                 `json:"max_redirect_uris"`
	MaxNameLength    int                 `json:"max_name_length"`
	ReservedNames    []string            `json:"reserved_names"`
	DeniedNames      []string            `json:"denied_names"`
	DeniedHostSuffix []string            `json:"denied_redirect_host_suffixes"`
	Brands           map[string][]string `json:"brands"`
	Version          int                 `json:"version"`
	Note             string              `json:"note"`
	firstPartyFolded []string
}

// Load reads the release's policy strictly.
func Load() (Policy, error) {
	var p Policy
	d := json.NewDecoder(bytes.NewReader(contracts.MCPRegistrationPolicy))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return Policy{}, fmt.Errorf("mcp-registration-policy.json: %w", err)
	}
	if p.Version != 1 || p.MaxRedirectURIs < 1 || p.MaxNameLength < 1 || len(p.DefaultScopes) == 0 {
		return Policy{}, errors.New("mcp-registration-policy.json: a version, limits and a default scope are required")
	}
	clients, err := mcpgate.Clients()
	if err != nil {
		return Policy{}, err
	}
	for _, c := range clients {
		p.firstPartyFolded = append(p.firstPartyFolded, fold(c.Name), fold(c.ID))
	}
	for i := range p.ReservedNames {
		p.ReservedNames[i] = fold(p.ReservedNames[i])
	}
	return p, nil
}

// Request is a registration as the client sent it (RFC 7591, the subset this server takes).
type Request struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
	ApplicationType         string   `json:"application_type"`
}

// refusedFields are named so the refusal says why: each is something the server would fetch, trust or hand out.
var refusedFields = map[string]string{
	"jwks": "keys", "jwks_uri": "a key-set URL", "request_uris": "request URLs", "sector_identifier_uri": "a sector identifier URL",
	"software_statement": "a software statement", "software_id": "a software ID", "software_version": "a software version",
	"logo_uri": "a logo URL", "client_uri": "a home page URL", "tos_uri": "a terms URL", "policy_uri": "a policy URL",
	"contacts": "contacts", "client_secret": "a secret", "client_id": "a client ID", "client_secret_expires_at": "a secret expiry",
	"registration_access_token": "a registration token", "registration_client_uri": "a registration URL",
	"initiate_login_uri": "a login URL", "id_token_signed_response_alg": "an ID-token algorithm", "default_max_age": "a maximum age",
	"token_endpoint_auth_signing_alg": "a signing algorithm", "subject_type": "a subject type", "tls_client_auth_subject_dn": "a certificate subject",
}

// MaxBody is the most a registration body may hold.
const MaxBody = 8 << 10

// Decode reads a registration strictly: one JSON object of at most MaxBody bytes, no field the policy does not know. A field
// that carries something the server would fetch or trust is refused by name.
func Decode(r io.Reader) (Request, []Problem) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxBody+1))
	if err != nil || len(raw) > MaxBody {
		return Request{}, []Problem{{InvalidMetadata, "body", "The registration is larger than 8 KiB.", "Send only the fields of RFC 7591 this server takes"}}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Request{}, []Problem{{InvalidMetadata, "body", "The registration is not one JSON object.", "Send a JSON object"}}
	}
	var problems []Problem
	for _, name := range sortedKeys(fields) {
		switch name {
		case "client_name", "redirect_uris", "grant_types", "response_types", "token_endpoint_auth_method", "scope", "application_type":
		default:
			what, known := refusedFields[name]
			if !known {
				what = "a field this server does not know"
			}
			problems = append(problems, Problem{InvalidMetadata, name, "This server does not take " + what + ".", "Leave " + name + " out: nothing a client names is fetched or trusted"})
		}
	}
	if len(problems) > 0 {
		return Request{}, problems
	}
	var req Request
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		return Request{}, []Problem{{InvalidMetadata, "body", "A field has the wrong type.", "Send strings, and lists of strings for redirect_uris, grant_types and response_types"}}
	}
	return req, nil
}

func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Registration is what a policy-checked request becomes: always agent class, public, PKCE, the default scope.
type Registration struct {
	Name         string
	RedirectURIs []string
	Scopes       []string
}

// Check applies the policy: it returns the registration to make, or every problem at once.
func (p Policy) Check(req Request) (Registration, []Problem) {
	var problems []Problem
	bad := func(code, field, msg, fix string) { problems = append(problems, Problem{code, field, msg, fix}) }

	name := strings.TrimSpace(req.ClientName)
	switch {
	case name == "" || len(name) > p.MaxNameLength:
		bad(InvalidMetadata, "client_name", fmt.Sprintf("The client's name is empty or longer than %d characters.", p.MaxNameLength), "Name your app in 1 to 100 characters")
	case !printableASCII(name):
		bad(InvalidMetadata, "client_name", "The client's name uses characters other than letters, digits, spaces and . - _ ( ).", "Use plain letters and digits")
	}
	folded := fold(name)
	// Claude Code registers as "Claude Code (<what the user called the server>)": a trailing group after a name that is a
	// first-party app's or a brand's is the user's own label, so it may hold a word the name itself may not.
	core := folded
	if i := strings.Index(name, "("); i > 0 && strings.HasSuffix(name, ")") {
		if c := fold(name[:i]); slices.Contains(p.firstPartyFolded, c) || p.isBrand(c) {
			core = c
		}
	}
	if name != "" && printableASCII(name) {
		for _, r := range p.ReservedNames {
			if strings.Contains(core, r) {
				bad(InvalidMetadata, "client_name", "The name could pass for Fyniq or for someone with authority over it.", "Name your own app, without Fyniq's name or a word such as official or admin")
				break
			}
		}
		for _, f := range p.firstPartyFolded {
			if f != "" && folded == f {
				bad(InvalidMetadata, "client_name", "The name is the name of an app Fyniq already has.", "Name your own app")
				break
			}
		}
		for _, d := range p.DeniedNames {
			if strings.Contains(folded, fold(d)) {
				bad(InvalidMetadata, "client_name", "This name is not allowed.", "Choose another name")
			}
		}
	}

	switch {
	case len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > p.MaxRedirectURIs:
		bad(InvalidRedirect, "redirect_uris", fmt.Sprintf("Send 1 to %d redirect URIs.", p.MaxRedirectURIs), "List the exact URLs your app receives the sign-in answer on")
	default:
		seen := map[string]bool{}
		var hosts []string
		for i, u := range req.RedirectURIs {
			field := fmt.Sprintf("redirect_uris[%d]", i)
			host, why := checkRedirect(u, p.DeniedHostSuffix)
			if why != "" {
				bad(InvalidRedirect, field, why, "Use an exact https URL on your own host, or a loopback address such as http://127.0.0.1:8123/callback")
				continue
			}
			if seen[u] {
				bad(InvalidRedirect, field, "The redirect URI is listed twice.", "List each once")
				continue
			}
			seen[u] = true
			if host != "" {
				hosts = append(hosts, host)
			}
		}
		for brand, allowed := range p.Brands {
			if folded == "" || !strings.Contains(folded, fold(brand)) {
				continue
			}
			for _, h := range hosts {
				if !slices.ContainsFunc(allowed, func(a string) bool { return h == a || strings.HasSuffix(h, "."+a) }) {
					bad(InvalidRedirect, "redirect_uris", "The name is "+brand+"'s, and a redirect on "+h+" is not "+brand+"'s own host.", "Use the name of your own app, or only "+brand+"'s hosts")
					break
				}
			}
		}
	}

	if len(req.GrantTypes) > 0 && !subset(req.GrantTypes, []string{"authorization_code", "refresh_token"}) {
		bad(InvalidMetadata, "grant_types", "Only the authorisation code and refresh tokens are available.", "Ask for authorization_code and refresh_token")
	}
	if len(req.ResponseTypes) > 0 && !(len(req.ResponseTypes) == 1 && req.ResponseTypes[0] == "code") {
		bad(InvalidMetadata, "response_types", "Only the code response type is available.", "Ask for code")
	}
	if req.TokenEndpointAuthMethod != "" && req.TokenEndpointAuthMethod != "none" {
		bad(InvalidMetadata, "token_endpoint_auth_method", "Clients here are public: they hold no secret.", "Use none, with PKCE")
	}
	if req.ApplicationType != "" && req.ApplicationType != "native" && req.ApplicationType != "web" {
		bad(InvalidMetadata, "application_type", "The application type is native or web.", "Send native or web")
	}
	if req.Scope != "" {
		for _, s := range strings.Fields(req.Scope) {
			if !slices.Contains(p.DefaultScopes, s) {
				bad(InvalidMetadata, "scope", "The scope "+truncate(s, 60)+" is not available to a new client.", "Ask for fyniq.read.reports; an Owner can widen an approved app later")
			}
		}
	}
	if len(problems) > 0 {
		return Registration{}, problems
	}
	return Registration{Name: name, RedirectURIs: slices.Clone(req.RedirectURIs), Scopes: slices.Clone(p.DefaultScopes)}, nil
}

// isBrand reports whether a folded name is a brand's name as the policy lists it.
func (p Policy) isBrand(folded string) bool {
	for b := range p.Brands {
		if strings.Contains(folded, fold(b)) {
			return true
		}
	}
	return false
}

func subset(got, allowed []string) bool {
	for _, g := range got {
		if !slices.Contains(allowed, g) {
			return false
		}
	}
	return true
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// printableASCII reports whether s holds only letters, digits, spaces and . - _ ( ), so no look-alike letter from another
// script can stand in a name.
func printableASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '.' || r == '-' || r == '_' || r == '(' || r == ')') {
			return false
		}
	}
	return true
}

var confusables = strings.NewReplacer("0", "o", "1", "i", "l", "i", "|", "i", "3", "e", "4", "a", "5", "s", "7", "t", "8", "b", "@", "a", "$", "s", "!", "i")

// fold is a name as it reads: lower case, with digits that stand for letters turned into them, and everything that is no
// letter removed, so "Fyn1q", "F y n i q" and "fyniq" are the same.
func fold(s string) string {
	s = confusables.Replace(strings.ToLower(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) && r <= unicode.MaxASCII {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// checkRedirect is why a redirect URI is not allowed, or "" and its host when it is: an https URL on a DNS name of the client's
// own, or a loopback address. It is exact: no wildcard, no fragment, no query, no userinfo, no dot segments, no encoded host.
func checkRedirect(raw string, deniedSuffixes []string) (host, why string) {
	switch {
	case raw == "" || len(raw) > 255:
		return "", "The redirect URI is empty or longer than 255 characters."
	case strings.ContainsAny(raw, "*\\ \t\r\n\x00") || strings.Contains(raw, "%") || strings.Contains(raw, ".."):
		return "", "The redirect URI holds a wildcard, a space, an escape or a dot segment."
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" || strings.HasSuffix(raw, "#") || strings.Contains(raw, "?") {
		return "", "The redirect URI is not one exact URL without a query, a fragment or credentials."
	}
	hostname, port := u.Hostname(), u.Port()
	if port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return "", "The redirect URI's port is not a port."
		}
	}
	ip := net.ParseIP(hostname)
	switch u.Scheme {
	case "http":
		if !(hostname == "localhost" || (ip != nil && ip.IsLoopback())) {
			return "", "http is only for a loopback address; use https."
		}
		if strings.Contains(u.Host, "[") && ip == nil {
			return "", "The redirect URI's host is not an address."
		}
		return "", ""
	case "https":
		switch {
		case ip != nil || strings.Contains(u.Host, "["):
			return "", "An https redirect must name a host, not an IP address."
		case hostname == "localhost" || !strings.Contains(hostname, ".") || strings.HasSuffix(hostname, ".") || strings.HasSuffix(hostname, ".local") || strings.HasSuffix(hostname, ".internal"):
			return "", "An https redirect must be on a public host name."
		case strings.Contains(strings.ToLower(hostname), "xn--"):
			return "", "The redirect URI's host is an internationalised name, which can pass for another."
		case !asciiHost(hostname):
			return "", "The redirect URI's host uses characters other than ASCII letters, digits, dots and hyphens."
		case u.Path == "":
			return "", "The redirect URI has no path."
		}
		h := strings.ToLower(hostname)
		for _, d := range deniedSuffixes {
			if h == d || strings.HasSuffix(h, "."+d) {
				return "", "The host " + h + " is not allowed for a redirect."
			}
		}
		return h, ""
	}
	return "", "Only https, or http on a loopback address, can receive the sign-in answer."
}

func asciiHost(h string) bool {
	for _, r := range h {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return false
		}
	}
	return !strings.HasPrefix(h, "-") && !strings.Contains(h, "..")
}
