package compliance

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Readiness by framework (PRD-001 CMP-08, rev 0.617; p2-security-certification design D5): what the registry's mappings say about how far
// each framework is covered. Never a certificate.

// FrameworkStages are the stages of PRD-001 §14.5 and §14.8 each framework belongs to, in words.
var FrameworkStages = map[string]string{
	"sg-statute": "Always",
	"pdpa":       "Always",
	"csa-ce":     "Stage 0, before go-live or shortly after",
	"csa-ct":     "Stage 1, with the first employees",
	"iso27001":   "Stage 2, for others' data",
	"soc2":       "Stage 2, after ISO 27001 or alongside",
	"imda-ai":    "From the AI assistant",
	"owasp":      "From P0",
	"coso":       "Always",
	"isa":        "What external auditors expect",
}

// AnnexA are the 93 control numbers of ISO/IEC 27001:2022 Annex A: 5.1 to 5.37, 6.1 to 6.8, 7.1 to 7.14 and 8.1 to 8.34. Only the numbers are
// kept here: the titles are the standard's text, which waits for the certification body ([live-verify]).
var AnnexA = func() []string {
	var out []string
	for _, theme := range []struct{ n, last int }{{5, 37}, {6, 8}, {7, 14}, {8, 34}} {
		for i := 1; i <= theme.last; i++ {
			out = append(out, fmt.Sprintf("%d.%d", theme.n, i))
		}
	}
	return out
}()

var isoNumber = regexp.MustCompile(`([5-8])\.(\d{1,2})(?:\s*[–-]\s*([5-8])\.(\d{1,2}))?`)

// ISOClauses are the Annex A control numbers a Mapping cell names, with each range such as "5.24–5.28" spelled out, in Annex A's order.
// Only a part of the cell that starts with "ISO" counts, so "SOC2 CC6.1" and "COSO P10" name none.
func ISOClauses(mapping string) []string {
	seen := map[string]bool{}
	for _, part := range splitMapping(mapping) {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "ISO") {
			continue
		}
		for _, m := range isoNumber.FindAllStringSubmatch(part, -1) {
			from, _ := strconv.Atoi(m[2])
			to := from
			if m[3] != "" && m[3] == m[1] {
				to, _ = strconv.Atoi(m[4])
			}
			for i := from; i <= to; i++ {
				seen[m[1]+"."+strconv.Itoa(i)] = true
			}
		}
	}
	var out []string
	for _, c := range AnnexA {
		if seen[c] {
			out = append(out, c)
		}
	}
	return out
}

// splitMapping splits a Mapping cell at the semicolons outside brackets.
func splitMapping(cell string) []string {
	var parts []string
	depth, start := 0, 0
	for i, r := range cell {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ';':
			if depth == 0 {
				parts = append(parts, cell[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, cell[start:])
}
