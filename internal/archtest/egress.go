package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
)

// The one egress path (SEC-36, QA-SEC-04; p1-security-hardening design D3): every outbound HTTP request goes through
// internal/egress, whose clients check each destination, dial the address they checked, follow no redirect and bound
// the answer. Outside that package (and tests) no code builds an HTTP client or transport, uses net/http's default
// client or its shortcuts, or lets go-oidc fetch keys with its default client; the metadata server's client is the gcp
// adapter's alone, and the loopback client the healthcheck's.

// httpForbidden are net/http's names that make a request outside the guard.
var httpForbidden = map[string]string{
	"Client":           "an http.Client",
	"Transport":        "an http.Transport",
	"DefaultClient":    "http.DefaultClient",
	"DefaultTransport": "http.DefaultTransport",
	"Get":              "http.Get",
	"Post":             "http.Post",
	"Head":             "http.Head",
	"PostForm":         "http.PostForm",
}

// EgressUses returns each place in src that reaches the network without internal/egress. metadataOK and loopbackOK say
// whether the file may use egress.Metadata and egress.Loopback.
func EgressUses(filename string, src []byte, metadataOK, loopbackOK bool) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	names := map[string]string{} // local name → import path
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		name := map[string]string{"net/http": "http", "github.com/coreos/go-oidc/v3/oidc": "oidc", Module + "/internal/egress": "egress"}[path]
		if name == "" {
			continue
		}
		if imp.Name != nil {
			name = imp.Name.Name
		}
		names[name] = path
	}
	if len(names) == 0 {
		return nil, nil
	}
	var out []string
	bad := func(n ast.Node, format string, args ...any) {
		out = append(out, fmt.Sprintf("%s: %s", fset.Position(n.Pos()), fmt.Sprintf(format, args...)))
	}
	is := func(e ast.Expr, path, sel string) bool {
		s, ok := e.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		id, ok := s.X.(*ast.Ident)
		return ok && names[id.Name] == path && s.Sel.Name == sel
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.StarExpr:
			// *http.Client as a type holds a client someone else made: allowed.
			if is(n.X, "net/http", "Client") || is(n.X, "net/http", "Transport") {
				return false
			}
		case *ast.Field:
			if is(n.Type, "net/http", "Client") || is(n.Type, "net/http", "Transport") {
				bad(n, "an http.Client held by value is a client of its own: hold a *http.Client from internal/egress (SEC-36)")
			}
		case *ast.CompositeLit:
			for _, sel := range []string{"Client", "Transport"} {
				if is(n.Type, "net/http", sel) {
					bad(n, "%s is built here: take one from internal/egress (SEC-36)", httpForbidden[sel])
				}
			}
		case *ast.CallExpr:
			if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "new" && len(n.Args) == 1 && (is(n.Args[0], "net/http", "Client") || is(n.Args[0], "net/http", "Transport")) {
				bad(n, "new(http.Client) builds a client: take one from internal/egress (SEC-36)")
			}
			if (is(n.Fun, "github.com/coreos/go-oidc/v3/oidc", "NewRemoteKeySet") || is(n.Fun, "github.com/coreos/go-oidc/v3/oidc", "NewProvider")) &&
				(len(n.Args) == 0 || !isCall(n.Args[0], "ClientContext", names)) {
				bad(n, "go-oidc fetches keys with its default client unless the context carries one: pass oidc.ClientContext(ctx, <a client from internal/egress>) (SEC-36)")
			}
			if is(n.Fun, Module+"/internal/egress", "Metadata") && !metadataOK {
				bad(n, "egress.Metadata reaches the metadata server: only the gcp adapter's token sources may use it")
			}
			if is(n.Fun, Module+"/internal/egress", "Loopback") && !loopbackOK {
				bad(n, "egress.Loopback reaches this machine: only the image's healthcheck (internal/app) may use it")
			}
		case *ast.ValueSpec:
			if n.Type != nil && (is(n.Type, "net/http", "Client") || is(n.Type, "net/http", "Transport")) {
				bad(n, "a zero-value %s is a client of its own: take one from internal/egress (SEC-36)", httpForbidden[n.Type.(*ast.SelectorExpr).Sel.Name])
			}
		case *ast.SelectorExpr:
			if id, ok := n.X.(*ast.Ident); ok && names[id.Name] == "net/http" {
				switch n.Sel.Name {
				case "DefaultClient", "DefaultTransport", "Get", "Post", "Head", "PostForm":
					bad(n, "%s sends outside the egress guard: take a client from internal/egress (SEC-36)", httpForbidden[n.Sel.Name])
				}
			}
		}
		return true
	})
	return out, nil
}

// isCall reports whether e calls oidc.<name>.
func isCall(e ast.Expr, name string, names map[string]string) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	s, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && names[id.Name] == "github.com/coreos/go-oidc/v3/oidc" && s.Sel.Name == name
}
