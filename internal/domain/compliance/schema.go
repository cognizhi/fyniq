package compliance

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// SchemaJob is the nightly comparison of the database's guards with the release's (design D4): the privileges of every
// role, the guard triggers such as require_audit, and row-level security. It proves CTL-FIN-02 (posted ledger and audit
// tables stay immutable), CTL-AUD-01 (every audited table keeps its trigger) and CTL-AI-01 (the AI's role keeps no write
// grant), and fails, naming each difference, when anything was changed by hand.
const SchemaJob = "schema.verify"

// VerifySchema compares the database's guards with want, the release's, and returns the differences; any difference is
// an error, which fails the job and its evidence.
func VerifySchema(ctx context.Context, db storage.Catalogue, want storage.Guards) ([]string, error) {
	got, err := db.Guards(ctx)
	if err != nil {
		return nil, err
	}
	diffs := CompareGuards(want, got)
	if len(diffs) > 0 {
		return diffs, fmt.Errorf("the database's guards differ from the release's (CTL-FIN-02, CTL-AUD-01, CTL-AI-01): %s",
			strings.Join(diffs, "; "))
	}
	return nil, nil
}

// CompareGuards names each guard of got that want lacks, and each of want that got lacks, in words.
func CompareGuards(want, got storage.Guards) []string {
	var out []string
	for _, part := range []struct {
		want, got     []string
		extra, absent func(line string) string
	}{
		{want.Grants, got.Grants, func(l string) string { return grant(l, "holds", "which the release does not grant") },
			func(l string) string { return grant(l, "no longer holds", "which the release grants") }},
		{want.Triggers, got.Triggers, func(l string) string { return trigger(l, "is not the release's") },
			func(l string) string { return trigger(l, "is missing") }},
		{want.Policies, got.Policies, func(l string) string { return policy(l, "is not the release's") },
			func(l string) string { return policy(l, "is missing or changed") }},
		{want.RowSecurity, got.RowSecurity, func(l string) string { return rowSecurity(l, "now") },
			func(l string) string { return rowSecurity(l, "should be") }},
	} {
		for _, l := range part.got {
			if !slices.Contains(part.want, l) {
				out = append(out, part.extra(l))
			}
		}
		for _, l := range part.want {
			if !slices.Contains(part.got, l) {
				out = append(out, part.absent(l))
			}
		}
	}
	return out
}

// grant reads a line of grants.golden: the role, the privilege, then the object and any columns.
func grant(line, verb, why string) string {
	f := strings.Fields(line)
	if len(f) < 3 {
		return "a grant " + verb + ": " + line
	}
	return fmt.Sprintf("%s %s %s on %s, %s", f[0], verb, f[1], strings.Join(f[2:], " "), why)
}

var triggerDef = regexp.MustCompile(`^CREATE (?:CONSTRAINT )?TRIGGER (\S+) .*? ON (?:fyniq\.)?(\S+) `)

// trigger reads a trigger's definition for its name and table.
func trigger(def, what string) string {
	if m := triggerDef.FindStringSubmatch(def); m != nil {
		return fmt.Sprintf("the trigger %s on %s %s", m[1], m[2], what)
	}
	return "the trigger " + def + " " + what
}

// policy reads a line of the policies: the table, the policy, its command and expressions.
func policy(line, what string) string {
	f := strings.SplitN(line, " | ", 3)
	if len(f) < 3 {
		return "the policy " + line + " " + what
	}
	return fmt.Sprintf("the row-level security policy %s on %s %s", f[1], f[0], what)
}

// rowSecurity reads a line of the tables: the table, enabled or disabled, forced or not forced.
func rowSecurity(line, tense string) string {
	f := strings.Split(line, " | ")
	if len(f) != 3 {
		return "row-level security " + tense + ": " + line
	}
	return fmt.Sprintf("row-level security on %s %s %s and %s", f[0], tense, f[1], f[2])
}
