package archtest

import (
	"fmt"
	"sort"
	"strings"
)

// Nemura, the optional people platform (PRD-002 ADR-N1, ADR-N4, NM-ARC-05), shares this repository and this module with
// Fyniq, and the shared rules, not the code paths: its code may use the platform's infrastructure and the pure money,
// rounding and audit-event libraries, and never Fyniq's ledger, API, storage port or domain packages. Neither app writes
// the other's tables or calls its private code. These rules keep that true at the import.

// NemuraPackages are the module's packages that are Nemura's: its entry point and everything under internal/nemura, and
// its adapters, which are sub-packages of the same name under internal/adapters.
var NemuraPackages = []string{"cmd/nemura", "internal/nemura", "internal/adapters/postgres/nemura"}

// NemuraShared is what Nemura's packages may import from the rest of the module: the infrastructure every service
// uses and the pure libraries that carry the shared rules. An entry names one package, not its sub-packages (the storage
// port and the migration runner are sub-packages and are not shared by accident). The list is the one a separate Nemura
// repository would take as its shared module (ADR-N1). Widening it is a reviewed change to this file.
var NemuraShared = []string{
	"internal/app", "internal/httpserver", "internal/runtime", "internal/clock", "internal/telemetry", "internal/logging",
	"internal/egress", "internal/version", "internal/ports", "internal/domain/money", "internal/domain/rounding",
	"internal/domain/audit",
}

func inNemura(importPath string) bool {
	for _, dir := range NemuraPackages {
		if under(importPath, dir) {
			return true
		}
	}
	return false
}

func nemuraMayImport(importPath string) bool {
	if inNemura(importPath) {
		return true
	}
	for _, p := range NemuraShared {
		if importPath == Module+"/"+p {
			return true
		}
	}
	return false
}

// NemuraViolations applies Nemura's boundary rules to the module's packages: what a Nemura package imports from this
// module is its own code or the shared list, and nothing outside Nemura imports Nemura's code.
func NemuraViolations(pkgs []Pkg) []string {
	var out []string
	for _, p := range pkgs {
		if !strings.HasPrefix(p.ImportPath, Module) {
			continue
		}
		if inNemura(p.ImportPath) {
			for _, imp := range p.Imports {
				if strings.HasPrefix(imp, Module+"/") && !nemuraMayImport(imp) {
					out = append(out, fmt.Sprintf("%s imports %s: Nemura's code uses only its own packages and the shared list (NemuraShared), never Fyniq's ledger, API, storage port or domain (ADR-N4, NM-ARC-05)", p.ImportPath, imp))
				}
			}
			continue
		}
		for _, imp := range p.Imports {
			if inNemura(imp) {
				out = append(out, fmt.Sprintf("%s imports %s: Nemura's packages are imported only by Nemura (ADR-N4)", p.ImportPath, imp))
			}
		}
	}
	sort.Strings(out)
	return out
}
