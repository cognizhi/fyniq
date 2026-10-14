// Package archtest enforces the repository's architecture rules as tests, so they run anywhere
// `go test` runs: no cloud SDK outside internal/adapters/<provider>/ (NFR-PORT-03, ADR-T8), the
// domain and the ports never depend on an adapter, nothing but internal/clock reads the wall
// clock (DEV-05), SQL drivers live only in the PostgreSQL adapter (NFR-PORT-01), and only its
// unit of work begins or ends a transaction (NFR-PORT-07).
package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// Module is this repository's Go module path.
const Module = "github.com/cognizhi/fyniq"

// CloudSDKs are import-path prefixes of cloud provider SDKs.
var CloudSDKs = []string{
	"cloud.google.com/go",
	"google.golang.org/api",
	"firebase.google.com/",
	"github.com/aws/",
	"github.com/Azure/",
}

// Pkg is the part of `go list -deps -json` the rules read.
type Pkg struct {
	ImportPath string
	Imports    []string
	Deps       []string
}

// SQLPackages are import-path prefixes that carry SQL: drivers, database/sql, the migration
// library and the adapter's generated queries. Only internal/adapters/postgres may import them.
var SQLPackages = []string{
	"database/sql",
	"github.com/jackc/pgx/v5",
	"github.com/jackc/pgx/v4",
	"github.com/lib/pq",
	"github.com/pressly/goose",
	Module + "/internal/adapters/postgres/sqlcdb",
}

func isSQL(path string) bool {
	for _, p := range SQLPackages {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

func isCloudSDK(path string) bool {
	for _, p := range CloudSDKs {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func under(path, dir string) bool {
	full := Module + "/" + dir
	return path == full || strings.HasPrefix(path, full+"/")
}

// ImportViolations applies the import rules to the module's packages.
func ImportViolations(pkgs []Pkg) []string {
	var out []string
	for _, p := range pkgs {
		if !strings.HasPrefix(p.ImportPath, Module) || under(p.ImportPath, "internal/adapters/postgres") {
			continue
		}
		if under(p.ImportPath, "internal/adapters") {
			for _, imp := range p.Imports {
				if isSQL(imp) {
					out = append(out, fmt.Sprintf("%s imports %s: SQL lives only in the PostgreSQL adapter (NFR-PORT-01)", p.ImportPath, imp))
				}
			}
			continue
		}
		for _, imp := range p.Imports {
			if isCloudSDK(imp) {
				out = append(out, fmt.Sprintf("%s imports the cloud SDK %s: move the call into an adapter under internal/adapters/<provider>/ (NFR-PORT-03)", p.ImportPath, imp))
			}
		}
		for _, imp := range p.Imports {
			if isSQL(imp) {
				out = append(out, fmt.Sprintf("%s imports %s: SQL lives only in the PostgreSQL adapter; use the storage port (NFR-PORT-01)", p.ImportPath, imp))
			}
		}
		if under(p.ImportPath, "internal/domain") || under(p.ImportPath, "internal/ports") {
			for _, dep := range p.Deps {
				if under(dep, "internal/adapters") || isCloudSDK(dep) {
					out = append(out, fmt.Sprintf("%s depends on %s: the domain and the ports must not depend on an adapter or a cloud SDK (ADR-T8)", p.ImportPath, dep))
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// forbiddenTime are the time functions that read the wall clock.
var forbiddenTime = map[string]bool{"Now": true, "Since": true, "Until": true}

// WallClockCalls returns each place in src that reads the wall clock through package time.
func WallClockCalls(filename string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, imp := range f.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); path == "time" {
			name := "time"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			names[name] = true
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && names[id.Name] && forbiddenTime[sel.Sel.Name] {
			out = append(out, fmt.Sprintf("%s: time.%s reads the wall clock: take a ports.Clock instead (DEV-05)", fset.Position(sel.Pos()), sel.Sel.Name))
		}
		return true
	})
	return out, nil
}

// transactionMethods begin or end a transaction.
var transactionMethods = map[string]bool{"Begin": true, "BeginTx": true, "BeginFunc": true, "BeginTxFunc": true, "Commit": true, "Rollback": true}

// TransactionCalls returns each call in src that begins, commits or rolls back a transaction. In the
// PostgreSQL adapter only the unit of work may make them: a repository method never opens or
// commits its own transaction (NFR-PORT-07).
func TransactionCalls(filename string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && transactionMethods[sel.Sel.Name] {
			out = append(out, fmt.Sprintf("%s: %s begins or ends a transaction: only the unit of work in store.go may (NFR-PORT-07)", fset.Position(sel.Pos()), sel.Sel.Name))
		}
		return true
	})
	return out, nil
}

// MoneyPackages are the packages where money is computed: no binary floating point may appear in
// them (ENG-10).
var MoneyPackages = []string{
	"internal/domain/money", "internal/domain/rounding", "internal/domain/ledger",
	"internal/domain/journal", "internal/domain/balance",
}

// floatFuncs are the standard-library calls that produce or read binary floating point.
var floatFuncs = map[string]map[string]bool{
	"strconv": {"ParseFloat": true, "FormatFloat": true, "AppendFloat": true},
	"math":    {"Float64bits": true, "Float64frombits": true, "Float32bits": true, "Float32frombits": true},
}

// FloatUses returns each use of binary floating point in src: the float32 and float64 types,
// floating-point literals, the math package and the strconv float functions (ENG-10).
func FloatUses(filename string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	names := map[string]string{} // local name → import path
	var out []string
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		name := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			name = path[i+1:]
		}
		if imp.Name != nil {
			name = imp.Name.Name
		}
		names[name] = path
		if path == "math" || path == "math/big" {
			out = append(out, fmt.Sprintf("%s: imports %s: money code uses apd decimals only (ENG-10)", fset.Position(imp.Pos()), path))
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			if x.Name == "float32" || x.Name == "float64" || x.Name == "complex64" || x.Name == "complex128" {
				out = append(out, fmt.Sprintf("%s: %s in money code (ENG-10)", fset.Position(x.Pos()), x.Name))
			}
		case *ast.BasicLit:
			if x.Kind == token.FLOAT || x.Kind == token.IMAG {
				out = append(out, fmt.Sprintf("%s: floating-point literal %s in money code (ENG-10)", fset.Position(x.Pos()), x.Value))
			}
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && floatFuncs[names[id.Name]][x.Sel.Name] {
				out = append(out, fmt.Sprintf("%s: %s.%s in money code (ENG-10)", fset.Position(x.Pos()), id.Name, x.Sel.Name))
			}
		}
		return true
	})
	return out, nil
}

// BalanceReaders may call UnitOfWork.Balances(): the balance service, the storage adapter and the
// storage conformance suite. Everything else asks the balance service (ENG-04).
var BalanceReaders = []string{"internal/domain/balance", "internal/adapters/postgres", "internal/ports/porttest"}

// BalanceCalls returns each call of .Balances() in src.
func BalanceCalls(filename string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Balances" && len(call.Args) == 0 {
				out = append(out, fmt.Sprintf("%s: reads balances outside the balance service (ENG-04)", fset.Position(sel.Pos())))
			}
		}
		return true
	})
	return out, nil
}
