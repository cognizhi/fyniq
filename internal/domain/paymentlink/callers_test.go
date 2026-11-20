package paymentlink_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The payment-link capability is a person's alone (design D1): the only code that calls Runtime.PaymentLinks is the API's command, never the
// worker, the executor, the AI or an MCP tool. This reads the sources, so a new caller fails here and is reviewed.
// req: FR-INT-27
// req: FR-AR-13
func TestOnlyTheAPICallsThePaymentLinkRuntime(t *testing.T) {
	root := "../../.."
	var callers []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "sdk") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "Runtime.PaymentLinks(") || strings.Contains(string(b), "rt.PaymentLinks(") {
			callers = append(callers, filepath.ToSlash(strings.TrimPrefix(path, root+"/")))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(callers) != 1 || callers[0] != "internal/api/payment_links.go" {
		t.Fatalf("the payment-link capability is called from %v; only internal/api/payment_links.go may call it", callers)
	}
}
