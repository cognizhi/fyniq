//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/journal"
)

// The capital allowance schedule through the API (FR-FA-04): an asset elected under section 19A(1) claims a third a year of its cost from the year of assessment its financial year
// ends in; one with no election is listed apart; an election is checked against the pack (a prescribed life of 6 years may not be elected over 16), a section 19 election is
// irrevocable, and the schedule reads the elections the register keeps.
// req: FR-FA-04
func TestTheCapitalAllowanceScheduleFollowsTheElectionsTheRegisterKeeps(t *testing.T) {
	e := newEnv(t)
	e.booksFrom()
	accountant := e.as(authz.Accountant)
	id, tag := e.registered("3000.00")
	idB, tagB := e.registered("1200.00")
	path := "/v1/fixed-assets/" + id + "/change"

	// A life the prescribed one does not allow, and a method that is not one.
	bad := e.call(http.MethodPost, path, accountant, map[string]any{"tax_election": map[string]any{"method": "s19", "prescribed_working_life_years": 6, "working_life_years": 16}}, "If-Match", tag)
	e.problemIs(bad, "changeFixedAsset", 422, asset.CodeTaxMethod)
	e.problemIs(e.call(http.MethodPost, path, accountant, map[string]any{"tax_election": map[string]any{"method": "s19a1", "working_life_years": 6}}, "If-Match", tag), "changeFixedAsset", 422, asset.CodeTaxMethod)

	r := e.call(http.MethodPost, path, accountant, map[string]any{"tax_election": map[string]any{"method": "s19a1"}}, "If-Match", tag)
	if r.Status != http.StatusOK || r.str("tax_election", "method") != "s19a1" {
		t.Fatalf("elect = %d: %s", r.Status, r.Raw)
	}
	for ya, want := range map[string][2]string{"2027": {"1000.00", "2000.00"}, "2028": {"1000.00", "1000.00"}, "2029": {"1000.00", "0.00"}} {
		s := e.call(http.MethodGet, "/v1/reports/capital-allowances?ya="+ya, accountant, nil)
		rows, _ := s.Body["rows"].([]any)
		if s.Status != http.StatusOK || len(rows) != 1 || s.str("pack") != "sg-capital-allowances@2026.2" {
			t.Fatalf("YA %s = %d: %s", ya, s.Status, s.Raw)
		}
		row := rows[0].(map[string]any)
		if row["allowance"].(map[string]any)["amount"] != want[0] || row["carried_forward"].(map[string]any)["amount"] != want[1] || row["first_ya"] != float64(2027) || row["method"] != "s19a1" {
			t.Errorf("YA %s: %v, want %v", ya, row, want)
		}
	}
	// The second asset has no election: listed apart, claiming nothing.
	s := e.call(http.MethodGet, "/v1/reports/capital-allowances?ya=2027", accountant, nil)
	apart, _ := s.Body["apart"].([]any)
	if len(apart) != 1 || apart[0].(map[string]any)["reason"] != "no_election" || s.str("totals", "allowance", "amount") != "1000.00" {
		t.Errorf("apart: %s", s.Raw)
	}

	// Section 19: 6 years of a 6-year life; once elected it never changes, and the same election sent again is not a change.
	pathB := "/v1/fixed-assets/" + idB + "/change"
	s19 := map[string]any{"tax_election": map[string]any{"method": "s19", "prescribed_working_life_years": 6, "working_life_years": 6}}
	rb := e.call(http.MethodPost, pathB, accountant, s19, "If-Match", tagB)
	if rb.Status != http.StatusOK || numberAt(rb, "tax_election", "working_life_years") != 6 {
		t.Fatalf("elect s19 = %d: %s", rb.Status, rb.Raw)
	}
	e.problemIs(e.call(http.MethodPost, pathB, accountant, map[string]any{"tax_election": map[string]any{"method": "s19a1"}}, "If-Match", rb.Header.Get("ETag")), "changeFixedAsset", 409, asset.CodeTaxFixed)
	same := e.call(http.MethodPost, pathB, accountant, s19, "If-Match", rb.Header.Get("ETag"))
	if same.Status != http.StatusOK {
		t.Errorf("the same election again = %d: %s", same.Status, same.Raw)
	}
	s = e.call(http.MethodGet, "/v1/reports/capital-allowances?ya=2027", accountant, nil)
	// 1,200: IA 240, the other 960 over 6 years is 160 each: 400 in the first year; and 1,000 from the third-year asset: 1,400.
	if s.str("totals", "initial_allowance", "amount") != "240.00" || s.str("totals", "allowance", "amount") != "1400.00" || s.str("totals", "carried_forward", "amount") != "2800.00" {
		t.Errorf("totals: %s", s.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/reports/capital-allowances?ya=1999", accountant, nil), "getCapitalAllowanceSchedule", 422, CodeInvalid)
}

// The movement schedule through the API (FR-FA-04): an asset bought in the run is an addition, the columns foot, and the register ties to the ledger through the balance service.
// req: FR-FA-04
func TestTheMovementScheduleFootsAndTiesToTheLedger(t *testing.T) {
	e := newEnv(t)
	e.booksFrom()
	accountant := e.as(authz.Accountant)
	e.registered("3600.00")
	m := e.call(http.MethodGet, "/v1/reports/fixed-asset-movement?from=2026-10&to=2026-12", accountant, nil)
	cats, _ := m.Body["categories"].([]any)
	if m.Status != http.StatusOK || len(cats) != 1 {
		t.Fatalf("movement = %d: %s", m.Status, m.Raw)
	}
	c := cats[0].(map[string]any)
	tot := c["total"].(map[string]any)
	amount := func(k string) string { return tot[k].(map[string]any)["amount"].(string) }
	if c["account_code"] != "1510" || amount("opening_cost") != "0.00" || amount("additions") != "3600.00" || amount("disposals") != "0.00" || amount("closing_cost") != "3600.00" ||
		amount("closing_nbv") != "3600.00" || m.Body["tied"] != true {
		t.Errorf("category: %s", m.Raw)
	}
	// A run after the purchase sees it in the opening.
	n := e.call(http.MethodGet, "/v1/reports/fixed-asset-movement?from=2026-12", accountant, nil)
	nc := n.Body["categories"].([]any)[0].(map[string]any)["total"].(map[string]any)
	if nc["opening_cost"].(map[string]any)["amount"] != "3600.00" || nc["additions"].(map[string]any)["amount"] != "0.00" {
		t.Errorf("December: %s", n.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/reports/fixed-asset-movement?from=2026-12&to=2026-10", accountant, nil), "getFixedAssetMovement", 422, CodeInvalid)
}

// The movement schedule with disposals, worked by hand (FR-FA-04): four S$3,600 laptops bought on 14 October 2026 over 36 months (S$100 a month), the books to 15 October 2027.
// For 2027: every laptop opens at cost 3,600 and accumulated 300 (October to December 2026). One sold whole for S$2,000 on 15 October: it takes off 3,600 and 1,200 (900 charged
// in January to September). One half sold: takes off 1,800 and 600. One kept. One sold on 10 September after September posted: September is reversed, so 800 charged (January to
// August) and 1,100 taken off. Together: cost 14,400 less 9,000 is 5,400; accumulated 1,200 + 3,500 charged - 2,900 taken off is 1,800; and the register ties to the ledger.
// For October to December 2026 the same laptops are additions of 14,400 with 1,200 charged.
// req: FR-FA-04
func TestTheMovementScheduleTakesWhatDisposalsTookOffAndTiesToTheLedger(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	e.disposalBooks()
	bank := e.chartMoneyAccount()
	var ids, tags [4]string
	for i := range ids {
		ids[i], _ = e.registered("3600.00")
	}
	e.clock.Set(time.Date(2027, 10, 15, 4, 0, 0, 0, time.UTC))
	if _, err := journal.DepreciateDueAll(context.Background(), e.store, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	for i := range ids {
		tags[i] = e.call(http.MethodGet, "/v1/fixed-assets/"+ids[i], accountant, nil).Header.Get("ETag")
	}
	dispose := func(i int, body map[string]any) {
		if r := e.call(http.MethodPost, "/v1/fixed-assets/"+ids[i]+"/disposals", accountant, body, "If-Match", tags[i]); r.Status != http.StatusCreated {
			t.Fatalf("dispose %d = %d: %s", i, r.Status, r.Raw)
		}
	}
	dispose(0, map[string]any{"date": "2027-10-15", "proceeds": "money", "money_account_id": bank.ID.String(), "amount": sgd("2000.00")})
	dispose(1, map[string]any{"date": "2027-10-15", "cost": sgd("1800.00"), "proceeds": "money", "money_account_id": bank.ID.String(), "amount": sgd("1000.00")})
	dispose(3, map[string]any{"date": "2027-09-10", "proceeds": "money", "money_account_id": bank.ID.String(), "amount": sgd("2000.00")})

	m := e.call(http.MethodGet, "/v1/reports/fixed-asset-movement?from=2027-01&to=2027-12", accountant, nil)
	if m.Status != http.StatusOK {
		t.Fatalf("movement = %d: %s", m.Status, m.Raw)
	}
	cat := m.Body["categories"].([]any)[0].(map[string]any)
	sum := func(row map[string]any, keys ...string) []string {
		var out []string
		for _, k := range keys {
			out = append(out, row[k].(map[string]any)["amount"].(string))
		}
		return out
	}
	cols := []string{"opening_cost", "additions", "disposals", "closing_cost", "opening_accumulated", "charge", "disposed_accumulated", "closing_accumulated", "closing_nbv"}
	want := func(got []string, w ...string) {
		t.Helper()
		for i := range w {
			if got[i] != w[i] {
				t.Errorf("%s = %s, want %s", cols[i], got[i], w[i])
			}
		}
	}
	want(sum(cat["total"].(map[string]any), cols...), "14400.00", "0.00", "9000.00", "5400.00", "1200.00", "3500.00", "2900.00", "1800.00", "3600.00")
	byNumber := map[string]map[string]any{}
	for _, a := range cat["assets"].([]any) {
		byNumber[a.(map[string]any)["number"].(string)] = a.(map[string]any)
	}
	want(sum(byNumber["FA-0001"], cols...), "3600.00", "0.00", "3600.00", "0.00", "300.00", "900.00", "1200.00", "0.00", "0.00")
	want(sum(byNumber["FA-0002"], cols...), "3600.00", "0.00", "1800.00", "1800.00", "300.00", "900.00", "600.00", "600.00", "1200.00")
	want(sum(byNumber["FA-0003"], cols...), "3600.00", "0.00", "0.00", "3600.00", "300.00", "900.00", "0.00", "1200.00", "2400.00")
	want(sum(byNumber["FA-0004"], cols...), "3600.00", "0.00", "3600.00", "0.00", "300.00", "800.00", "1100.00", "0.00", "0.00")
	if m.Body["tied"] != true {
		t.Errorf("the register does not tie to the ledger: %v", m.Body["ties"])
	}
	for _, tie := range m.Body["ties"].([]any) {
		if tie.(map[string]any)["unexplained"].(map[string]any)["amount"] != "0.00" {
			t.Errorf("unexplained: %v", tie)
		}
	}

	first := e.call(http.MethodGet, "/v1/reports/fixed-asset-movement?from=2026-10&to=2026-12", accountant, nil)
	f := first.Body["categories"].([]any)[0].(map[string]any)["total"].(map[string]any)
	want(sum(f, cols...), "0.00", "14400.00", "0.00", "14400.00", "0.00", "1200.00", "0.00", "1200.00", "13200.00")
}

// A computer elected under section 19A(2) claims its whole cost in the year of assessment of its purchase, and an asset sold whole takes no allowance in the year of its sale but a
// balancing allowance or charge (FR-FA-04, rev 0.603; IRAS, "Calculating BA/ BC"): a S$3,000 computer sold for S$200 in the year it was bought has a written down value of 3,000 and a
// balancing allowance of 2,800; a S$1,200 asset on the three-year write-off, written off, has one of 1,200; a computer kept claims its 1,600. The totals say the year's allowance
// and its balancing allowance, and the sale shows its day and net proceeds.
// req: FR-FA-04
func TestASoldAssetHasItsBalancingAllowanceAndAComputerClaimsItsCostInOneYear(t *testing.T) {
	e := newEnv(t)
	e.booksFrom()
	e.disposalBooks()
	bank := e.chartMoneyAccount()
	accountant := e.as(authz.Accountant)
	elect := func(id, tag, method string) {
		t.Helper()
		if r := e.call(http.MethodPost, "/v1/fixed-assets/"+id+"/change", accountant, map[string]any{"tax_election": map[string]any{"method": method}}, "If-Match", tag); r.Status != http.StatusOK {
			t.Fatalf("electing %s = %d: %s", method, r.Status, r.Raw)
		}
	}
	sold, soldTag := e.registered("3000.00")
	elect(sold, soldTag, "s19a2")
	off, offTag := e.registered("1200.00")
	elect(off, offTag, "s19a1")
	kept, keptTag := e.registered("1600.00")
	elect(kept, keptTag, "s19a2")
	tagOf := func(id string) string {
		return e.call(http.MethodGet, "/v1/fixed-assets/"+id, accountant, nil).Header.Get("ETag")
	}
	if r := e.call(http.MethodPost, "/v1/fixed-assets/"+sold+"/disposals", accountant, map[string]any{"date": "2026-10-15", "proceeds": "money", "money_account_id": bank.ID.String(),
		"amount": sgd("200.00")}, "If-Match", tagOf(sold)); r.Status != http.StatusCreated {
		t.Fatalf("the sale = %d: %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/fixed-assets/"+off+"/disposals", accountant, map[string]any{"date": "2026-10-15", "proceeds": "none"}, "If-Match", tagOf(off)); r.Status != http.StatusCreated {
		t.Fatalf("the write-off = %d: %s", r.Status, r.Raw)
	}
	s := e.call(http.MethodGet, "/v1/reports/capital-allowances?ya=2027", e.as(authz.Auditor), nil)
	rows, _ := s.Body["rows"].([]any)
	if s.Status != http.StatusOK || len(rows) != 3 || len(s.Body["apart"].([]any)) != 0 {
		t.Fatalf("YA 2027 = %d: %s", s.Status, s.Raw)
	}
	amount := func(m map[string]any, field string) string {
		if x, ok := m[field].(map[string]any); ok {
			return x["amount"].(string)
		}
		return ""
	}
	byID := map[string]map[string]any{}
	for _, x := range rows {
		byID[x.(map[string]any)["asset_id"].(string)] = x.(map[string]any)
	}
	if r := byID[sold]; r["sold_on"] != "2026-10-15" || amount(r, "proceeds") != "200.00" || amount(r, "allowance") != "0.00" || amount(r, "brought_forward") != "3000.00" ||
		amount(r, "balancing_allowance") != "2800.00" || amount(r, "balancing_charge") != "0.00" || r["method"] != "s19a2" {
		t.Errorf("the computer sold: %v", r)
	}
	if r := byID[off]; amount(r, "proceeds") != "0.00" || amount(r, "balancing_allowance") != "1200.00" || amount(r, "allowance") != "0.00" {
		t.Errorf("the asset written off: %v", r)
	}
	if r := byID[kept]; r["sold_on"] != nil || amount(r, "allowance") != "1600.00" || amount(r, "carried_forward") != "0.00" || r["balancing_allowance"] != nil {
		t.Errorf("the computer kept: %v", r)
	}
	tot := s.Body["totals"].(map[string]any)
	if amount(tot, "allowance") != "1600.00" || amount(tot, "balancing_allowance") != "4000.00" || amount(tot, "balancing_charge") != "0.00" {
		t.Errorf("the totals: %v", tot)
	}
	// The year after the sale says nothing of the sold assets.
	if n := e.call(http.MethodGet, "/v1/reports/capital-allowances?ya=2028", e.as(authz.Auditor), nil); len(n.Body["rows"].([]any)) != 0 {
		t.Errorf("YA 2028: %s", n.Raw)
	}
}
