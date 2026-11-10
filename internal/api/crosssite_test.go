//go:build local

package api

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// The cross-site matrix (SEC-23, CSRF): every write operation of the contract, sent with a signed-in Owner's session
// cookie from another site, is refused before it does anything, and every answer is JSON the browser does not sniff.
// req: SEC-23
// asvs: V3.5.1, V3.2.1
func TestNoWriteFromAnotherSiteWithASession(t *testing.T) {
	s := startSessions(t)
	desk, _, _ := s.founders(t)
	param := regexp.MustCompile(`\{[^}]+\}`)
	var refused, other []string
	for path, item := range s.env.api.OpenAPI().Paths {
		for method, op := range map[string]*huma.Operation{http.MethodPost: item.Post, http.MethodPut: item.Put, http.MethodPatch: item.Patch,
			http.MethodDelete: item.Delete} {
			if op == nil {
				continue
			}
			u := param.ReplaceAllString(path, "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e99")
			for _, site := range []string{"cross-site", "same-site", ""} {
				r := desk.call(method, u, site, map[string]any{}, "Idempotency-Key", "crosssite-"+op.OperationID+"-"+site, "If-Match", "*")
				if r.Status < 400 {
					t.Errorf("%s %s from %q with a session: %d %s", method, path, site, r.Status, r.Raw)
				}
				if ct := r.Header.Get("Content-Type"); r.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(ct, "json") {
					t.Errorf("%s %s answered %q without nosniff or JSON", method, path, ct)
				}
				if r.problemCode() == CodeCrossSite {
					refused = append(refused, op.OperationID)
				} else if site == "cross-site" {
					other = append(other, op.OperationID+" "+strconv.Itoa(r.Status)+" "+r.problemCode())
				}
			}
		}
	}
	sort.Strings(other)
	// The operations a session does not authorise answer for another reason: a link's token, or a webhook's signature.
	for _, o := range other {
		if !strings.HasPrefix(o, "acceptQuoteByLink ") && !strings.HasPrefix(o, "declineQuoteByLink ") {
			t.Errorf("a write from another site with a session was not refused as cross-site: %s", o)
		}
	}
	if len(refused) < 3*400 {
		t.Fatalf("only %d writes were refused as cross-site", len(refused))
	}
}
