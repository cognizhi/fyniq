package paymentlink_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The payment-link capability is a person's alone (design D1): the only code that calls Runtime.PaymentLinks is the API's command, never the
// worker, the executor, the AI or an MCP tool, with one reviewed exception: the outbox job that makes the link an organisation switched on automatic
// links for when an invoice is issued (FR-AR-13, rev 0.588), named below with its reason, `needs-human-review` in docs/security/p2-grants-for-human-review.md.
// This reads the sources, so a new caller fails here and is reviewed.
var reviewedCallers = map[string]string{
	"internal/api/payment_links.go": "a person's request for a payment link (design D1)",
	"internal/paymentlinkjob/paymentlinkjob.go": "the job queued by issuing an invoice when invoices.payment_link_on_issue is on: it keeps the provider call out of the request path, asks for the open balance read on the server, " +
		"and records the link only through the narrow function create_payment_link_by_platform; never the AI, an MCP tool or the executor (FR-AR-13, rev 0.588)",
}

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
	for _, c := range callers {
		if reviewedCallers[c] == "" {
			t.Errorf("the payment-link capability is called from %s, which is not a reviewed caller (%v)", c, reviewedCallers)
		}
	}
	if len(callers) != len(reviewedCallers) {
		t.Errorf("the reviewed callers are %v but the capability is called from %v: keep them equal", reviewedCallers, callers)
	}
}
