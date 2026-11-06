//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// A file whose text shows an NRIC or FIN in full is flagged when it is attached, and stays flagged; a masked one is
// not (FR-DOC-09; PRD-001 rev 0.183).
// req: FR-DOC-09
// req: documents/attachments-and-evidence Unmasked NRICs are flagged (rev 0.183)
func TestAnUnmaskedNRICIsFlagged(t *testing.T) {
	e := newEnv(t)
	id, tag := e.createDraft("100.00")
	acc := e.as(authz.Accountant)
	path := "/v1/journals/" + id + "/evidence"
	up := func(name, body string) response {
		t.Helper()
		r := e.upload(path, acc, map[string]string{"evidence_type": "schedule"}, name, []byte(body), "If-Match", tag)
		if r.Status != http.StatusCreated {
			t.Fatalf("attaching %s: %d %s", name, r.Status, r.Raw)
		}
		return r
	}
	if r := up("staff.csv", "name,nric\nTan Ah Kow,S1234567D\n"); r.Body["unmasked_nric"] != true {
		t.Errorf("an unmasked NRIC: %s", r.Raw)
	}
	if r := up("masked.csv", "name,nric\nTan Ah Kow,S****567D\n"); r.Body["unmasked_nric"] != nil {
		t.Errorf("a masked NRIC: %s", r.Raw)
	}
	l := e.call(http.MethodGet, path, e.as(authz.Auditor), nil)
	items := l.Body["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["unmasked_nric"] != true || items[1].(map[string]any)["unmasked_nric"] != nil {
		t.Errorf("the files as listed: %s", l.Raw)
	}
}
