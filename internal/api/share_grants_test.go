//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/sharepay"
)

// A grant of shares for services (FR-EQ-13, RND-LINE, ALLOC-EVEN): 12,000 shares at 0.50 vesting on 31 Dec 2026 from a January start is 6,000.00 over twelve months, 500.00 a month. The Accountant
// records it with an expense account and a reserve that is not share capital; its schedule is exact; a month is proposed as a draft journal (Dr the expense, Cr the reserve) once; a month the grant does
// not run over, or that has not begun, is refused; with a month proposed its figures are fixed; a masked reader and an employee are refused.
// req: FR-EQ-13
// req: ALLOC-EVEN
func TestAGrantOfSharesForServicesIsScheduledAndProposedMonthByMonth(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	e.clock.Set(time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC))
	reserve := e.call(http.MethodPost, "/v1/accounts", accountant, map[string]any{"code": "3250", "name": "Share-based payment reserve", "type": "equity"})
	if reserve.Status != http.StatusCreated {
		t.Fatalf("the reserve: %d %s", reserve.Status, reserve.Raw)
	}
	body := func() map[string]any {
		return map[string]any{"recipient": "Wei Jie Ong", "fair_value": "0.50", "valuation": "Independent valuation of 30 Sep 2025", "service_start": "2026-01-01",
			"expense_account": e.books.Rent.ID.String(), "reserve_account": reserve.str("id"), "tranches": []map[string]any{{"shares": 12000, "vest_on": "2026-12-31"}}}
	}
	// Refused, every problem at once: the bank is no expense account, share capital is no reserve, the tranche vests before the service starts.
	bad := body()
	bad["expense_account"] = e.books.Bank.ID.String()
	bad["tranches"] = []map[string]any{{"shares": 10, "vest_on": "2025-06-30"}}
	r := e.call(http.MethodPost, "/v1/share-grants", accountant, bad)
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, "an active expense account") || !strings.Contains(r.Raw, "before its service starts") {
		t.Errorf("a bad grant: %d %s", r.Status, r.Raw)
	}
	made := e.call(http.MethodPost, "/v1/share-grants", accountant, body())
	if made.Status != http.StatusCreated || made.Header.Get("ETag") == "" {
		t.Fatalf("the grant: %d %s", made.Status, made.Raw)
	}
	id := made.str("id")

	// The schedule is exact.
	s := e.call(http.MethodGet, "/v1/share-grants/"+id+"/schedule", accountant, nil)
	if s.Status != http.StatusOK || s.str("total") != "6000.00" || strings.Count(s.Raw, `"expense":"500.00"`) != 12 || !strings.Contains(s.Raw, `"months":12`) {
		t.Fatalf("the schedule: %d %s", s.Status, s.Raw)
	}
	// March has begun: January, February and March can be proposed; April has not begun, and December is in the schedule but later.
	e.problemIs(e.call(http.MethodPost, "/v1/share-grant-months", accountant, map[string]any{"grant_id": id, "month": "2026-04"}), "proposeShareGrantMonth", 422, sharepay.CodeMonth)
	e.problemIs(e.call(http.MethodPost, "/v1/share-grant-months", accountant, map[string]any{"grant_id": id, "month": "2025-12"}), "proposeShareGrantMonth", 422, sharepay.CodeMonth)
	jan := e.call(http.MethodPost, "/v1/share-grant-months", accountant, map[string]any{"grant_id": id, "month": "2026-01"})
	if jan.Status != http.StatusCreated || jan.str("expense") != "500.00" || jan.str("journal_id") == "" {
		t.Fatalf("January: %d %s", jan.Status, jan.Raw)
	}
	j := e.call(http.MethodGet, "/v1/journals/"+jan.str("journal_id"), accountant, nil)
	if j.Status != http.StatusOK || j.str("status") != "draft" || !strings.Contains(j.Raw, "500.00") || !strings.Contains(j.Raw, "-500.00") || !strings.Contains(j.Raw, "Shares for services to Wei Jie Ong, January 2026") ||
		!strings.Contains(j.Raw, reserve.str("id")) || !strings.Contains(j.Raw, `"date":"2026-01-31"`) {
		t.Errorf("the draft journal: %d %s", j.Status, j.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/share-grant-months", accountant, map[string]any{"grant_id": id, "month": "2026-01"}), "proposeShareGrantMonth", 409, sharepay.CodeMonthRun)
	if l := e.call(http.MethodGet, "/v1/share-grant-months?grant_id="+id, accountant, nil); !strings.Contains(l.Raw, jan.str("journal_id")) {
		t.Errorf("the months proposed: %s", l.Raw)
	}
	if s := e.call(http.MethodGet, "/v1/share-grants/"+id+"/schedule", accountant, nil); !strings.Contains(s.Raw, jan.str("journal_id")) {
		t.Errorf("the schedule names the journal: %s", s.Raw)
	}
	// With a month proposed, the figures are fixed; the recipient and the archive may still change.
	changed := body()
	changed["fair_value"] = "0.75"
	e.problemIs(e.call(http.MethodPut, "/v1/share-grants/"+id, accountant, changed, "If-Match", made.Header.Get("ETag")), "replaceShareGrant", 422, sharepay.CodeMonth)
	renamed := body()
	renamed["recipient"] = "Wei Jie Ong (employee)"
	if r := e.call(http.MethodPut, "/v1/share-grants/"+id, accountant, renamed, "If-Match", made.Header.Get("ETag")); r.Status != http.StatusOK || r.str("recipient") != "Wei Jie Ong (employee)" {
		t.Errorf("renaming: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/share-grants/"+id, accountant, renamed), "replaceShareGrant", 428, CodeIfMatchRequired)
	// Nothing is shown to an employee or the Board viewer, and a masked reader (the DPO) gets the names and figures masked.
	for _, role := range []authz.Role{authz.Employee, authz.BoardViewer} {
		if r := e.call(http.MethodGet, "/v1/share-grants", e.as(role), nil); r.Status != http.StatusForbidden {
			t.Errorf("%s read the grants: %d %.200s", role, r.Status, r.Raw)
		}
	}
	if r := e.call(http.MethodGet, "/v1/share-grants", e.as(authz.DPO), nil); r.Status != http.StatusOK || strings.Contains(r.Raw, "Wei Jie") || strings.Contains(r.Raw, "0.50") || !strings.Contains(r.Raw, MaskedValue) {
		t.Errorf("the DPO read the grants: %d %.300s", r.Status, r.Raw)
	}
}
