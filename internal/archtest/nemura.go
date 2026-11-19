package archtest

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Nemura, the optional people platform (PRD-002 ADR-N1, ADR-N4, NM-ARC-05), shares this repository and this module with
// Fyniq, and the shared rules, not the code paths: its code may use the platform's infrastructure and the pure money,
// rounding and audit-event libraries, and never Fyniq's ledger, API, storage port or domain packages. Neither app writes
// the other's tables or calls its private code. These rules keep that true at the import.

// NemuraPackages are the module's packages that are Nemura's: its entry point, its migrations, everything under
// internal/nemura and its PostgreSQL adapter (a sub-package of Fyniq's adapter by name, which imports nothing of it).
var NemuraPackages = []string{"apps/nemura-web", "cmd/nemura", "db/nemura", "internal/nemura", "internal/adapters/postgres/nemura"}

// NemuraShared is what Nemura's packages may import from the rest of the module: the infrastructure every service
// uses and the pure libraries that carry the shared rules, the goose runner both migration streams use, and the local substitutes (queue, task credentials) of the local build. An entry names
// one package, not its sub-packages (Fyniq's storage port is a sub-package of ports and is not shared by accident). The list is the one a separate Nemura
// repository would take as its shared module (ADR-N1). Widening it is a reviewed change to this file.
var NemuraShared = []string{
	"internal/app", "internal/httpserver", "internal/runtime", "internal/clock", "internal/telemetry", "internal/logging",
	"internal/egress", "internal/version", "internal/ports", "internal/domain/money", "internal/domain/rounding",
	"internal/domain/audit", "internal/adapters/postgres/gooserun", "internal/adapters/local", "internal/passkey", "internal/adapters/hydra",
	// The pack validator: a pure library for statutory parameter packs that both apps read the same way (owner decision O1, 19 Nov 2026).
	// It stays pure, which TestTheSharedPackLibraryStaysPure checks.
	"internal/domain/pack",
}

// PurePack are the standard-library packages the shared pack validator may not import: a pure library does no I/O, holds no clock and speaks to no
// network or database, so sharing it shares rules and no code path (NM-ARC-05).
var PurePack = []string{"net", "net/http", "database/sql", "os", "os/exec", "syscall"}

// PackViolations reports a way internal/domain/pack stops being pure: it imports another package of the module, or one of PurePack's.
func PackViolations(pkgs []Pkg) []string {
	var out []string
	for _, p := range pkgs {
		if p.ImportPath != Module+"/internal/domain/pack" {
			continue
		}
		for _, imp := range p.Imports {
			if strings.HasPrefix(imp, Module+"/") || slices.Contains(PurePack, imp) {
				out = append(out, fmt.Sprintf("%s imports %s: the shared pack validator is pure, with no I/O, storage or module code (owner decision O1)", p.ImportPath, imp))
			}
		}
	}
	return out
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
