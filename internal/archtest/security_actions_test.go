package archtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/audit"
)

// Every audit action the code writes that looks like a security event's is one (AUD-06, PRD-001 rev 0.230;
// p1-audit-explorer design D2). A security event keeps its IP address and device beside the chain, erased after the
// security-log retention; a sign-in or an export written under any other action would keep them in its body for life.
// req: AUD-06
// req: SEC-15
// asvs: V16.2.5
func TestSecurityLookingActionsAreSecurityEvents(t *testing.T) {
	root := repoRoot(t)
	written := regexp.MustCompile(`(?:event\((?:as\([^)]*\)|ctx), *|Action: *|Action[A-Za-z]* *= *)"([a-z][a-z0-9_]*\.[a-z0-9_.]*[a-z0-9_])"`)
	seen := map[string]bool{}
	var wrong []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range written.FindAllSubmatch(src, -1) {
				action := string(m[1])
				if seen[action] {
					continue
				}
				seen[action] = true
				if audit.LooksSecurity.MatchString(action) && !audit.IsSecurity(action) {
					rel, _ := filepath.Rel(root, path)
					wrong = append(wrong, rel+": "+action)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) < 150 {
		t.Fatalf("only %d audit actions found: the pattern no longer finds where actions are written", len(seen))
	}
	for _, a := range []string{"session.started", "session.ended", "passkey.registered", "account.recovered", "report.exported",
		"document.bank_account_revealed", "report_export.downloaded", "super_admin.recovery_confirmed"} {
		if !seen[a] || !audit.IsSecurity(a) {
			t.Errorf("%s: written %v, a security event %v", a, seen[a], audit.IsSecurity(a))
		}
	}
	for _, a := range []string{"bank_reconciliation.signed", "accountant_review.signed_off", "role_assignment.created", "invoice.created"} {
		if audit.LooksSecurity.MatchString(a) || audit.IsSecurity(a) {
			t.Errorf("%s is not a security event", a)
		}
	}
	if len(wrong) > 0 {
		t.Errorf("actions that look like security events' but are not in audit.IsSecurity:\n%s", strings.Join(wrong, "\n"))
	}
}
