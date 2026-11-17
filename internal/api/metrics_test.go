//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/semantic"
)

// The semantic layer (AI-03, AI-68; p1-ai-assistant design D6) on the KPIs' hand-worked October and November: each
// measure is the figure the report it names shows, exactly, read at the revision it returns, as typed facts with their
// IDs, periods, units, signs and links to that report. Flows sum over the months; stocks are at the last month's end; a
// KPI is one month's; accounts come from the trial balance. A measure, dimension, grain or member it does not offer is
// refused with its field; the assistant's delegated token may ask it, as it only reads.
// req: AI-03
// req: AI-68
// asvs: V8.2.1
func TestTheSemanticLayerIsTheReports(t *testing.T) {
	e := newEnv(t)
	acc := kpiBooks(e)
	reader := e.as(authz.Controller)
	query := func(as string, body map[string]any) response {
		t.Helper()
		return e.call(http.MethodPost, "/v1/metrics:query", as, body)
	}
	facts := func(r response) []map[string]any {
		t.Helper()
		if r.Status != http.StatusOK {
			t.Fatalf("queryMetrics: %d %s", r.Status, r.Raw)
		}
		var out []map[string]any
		for _, f := range r.Body["facts"].([]any) {
			out = append(out, f.(map[string]any))
		}
		return out
	}
	one := func(body map[string]any) map[string]any {
		t.Helper()
		fs := facts(query(reader, body))
		if len(fs) != 1 {
			t.Fatalf("%v: %d facts", body, len(fs))
		}
		return fs[0]
	}

	// November equals the KPI page's figures, and the totals equal the statements'.
	kpis := e.call(http.MethodGet, "/v1/kpis?month=2026-11", reader, nil)
	page := map[string]string{}
	for _, k := range kpis.Body["kpis"].([]any) {
		m := k.(map[string]any)
		if v, ok := m["month"].(map[string]any)["value"].(string); ok {
			page[m["key"].(string)] = v
		}
	}
	for measure, key := range map[string]string{"cash": "cash", "receivables": "owed_to_us", "payables": "owed_by_us", "revenue": "revenue",
		"net_burn": "net_burn", "spend_sm": "spend_sm", "runway": "runway", "gross_margin": "gross_margin", "dso": "dso"} {
		f := one(map[string]any{"measure": measure, "from": "2026-11", "to": "2026-11"})
		if f["value"] == nil || !same(f["value"].(string), page[key]) || f["ledger_revision"] != kpis.Body["ledger_revision"] {
			t.Errorf("%s: %v, the KPI page %s", measure, f, page[key])
		}
	}
	revenue := one(map[string]any{"measure": "revenue", "from": "2026-10", "to": "2026-11"})
	if revenue["value"] != "10000.00" || revenue["from"] != "2026-10-01" || revenue["to"] != "2026-11-30" || revenue["unit"] != "amount" ||
		revenue["currency"] != "SGD" || !strings.HasPrefix(revenue["id"].(string), "F") || !strings.HasPrefix(revenue["lineage"].(string), "/v1/reports/statements/pl?") {
		t.Errorf("October and November's revenue: %v", revenue)
	}
	months := facts(query(reader, map[string]any{"measure": "net_profit", "from": "2026-10", "to": "2026-11", "grain": "month"}))
	if len(months) != 2 || months[0]["value"] != "-4500.00" || months[1]["value"] != "-1600.00" {
		t.Errorf("net profit by month: %v", months)
	}
	if cash := one(map[string]any{"measure": "cash", "from": "2026-10", "to": "2026-11"}); cash["value"] != "92500.00" || cash["from"] != "2026-11-30" {
		t.Errorf("cash at November's end: %v", cash)
	}
	// An account's movement and balance from the trial balance, by ID or code.
	rent := one(map[string]any{"measure": "account_movement", "from": "2026-10", "to": "2026-11", "by": "account", "member": "6200"})
	if rent["value"] != "12000.0000" && rent["value"] != "12000.00" || rent["member"] != acc["6200"].ID.String() || rent["member_name"] != "6200 Office rent" {
		t.Errorf("rent's movement: %v", rent)
	}
	if bank := one(map[string]any{"measure": "account_balance", "to": "2026-11", "by": "account", "member": acc["1010"].ID.String()}); !same(bank["value"].(string), "92500.00") {
		t.Errorf("the bank's balance: %v", bank)
	}

	// What it does not offer is refused with its field.
	for code, body := range map[string]map[string]any{
		semantic.CodeDimension: {"measure": "cash", "by": "customer"},
		semantic.CodeGrain:     {"measure": "runway", "from": "2026-10", "to": "2026-11"},
		semantic.CodeMember:    {"measure": "account_movement", "by": "account", "member": "9999"},
	} {
		e.problemIs(query(reader, body), "queryMetrics", 422, code)
	}
	e.problemIs(query(reader, map[string]any{"measure": "account_balance"}), "queryMetrics", 422, semantic.CodeDimension)
	if r := query(reader, map[string]any{"measure": "revenue", "from": "2025-01", "to": "2025-01"}); r.Status != 422 || !strings.Contains(r.Raw, CodeNoPeriod) {
		t.Errorf("a month the books have no period for: %d %s", r.Status, r.Raw)
	}
	if r := query(e.as(authz.Employee), map[string]any{"measure": "revenue"}); r.Status != http.StatusForbidden {
		t.Errorf("an employee: %d", r.Status)
	}

	// The assistant's delegated token asks it as the person.
	f := foundersIn(e)
	var token string
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		token, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(f.ahkow), uuid.New())
		return err
	})
	if r := query(token, map[string]any{"measure": "revenue", "from": "2026-11", "to": "2026-11"}); r.Status != http.StatusOK {
		t.Errorf("the assistant asking: %d %s", r.Status, r.Raw)
	}
}

// same is whether two decimal texts are the same number, whatever their trailing zeros.
func same(a, b string) bool {
	trim := func(s string) string {
		if strings.Contains(s, ".") {
			s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		}
		return s
	}
	return a != "" && trim(a) == trim(b)
}

// By customer and by vendor, the sub-ledgers: what each customer owes and the organisation owes each vendor, as the AR
// and AP ageing show them; asking needs a view of invoices or bills.
// req: AI-03
// req: AI-68
func TestTheSemanticLayerReadsTheSubLedgers(t *testing.T) {
	e := newEnv(t)
	e.issuedInvoice("2", "500.00")
	e.postedBill("1000.00", "0.00")
	reader := e.as(authz.Controller)
	r := e.call(http.MethodPost, "/v1/metrics:query", reader, map[string]any{"measure": "receivables", "by": "customer", "to": "2026-10"})
	ar := e.call(http.MethodGet, "/v1/receivables?as_at=2026-10-31", reader, nil)
	fs, _ := r.Body["facts"].([]any)
	if r.Status != http.StatusOK || len(fs) != 1 || !strings.Contains(ar.Raw, fs[0].(map[string]any)["value"].(string)) ||
		!strings.Contains(ar.Raw, fs[0].(map[string]any)["member"].(string)) {
		t.Errorf("by customer: %d %s; the ageing %s", r.Status, r.Raw, ar.Raw)
	}
	p := e.call(http.MethodPost, "/v1/metrics:query", reader, map[string]any{"measure": "payables", "by": "vendor", "to": "2026-10"})
	ap := e.call(http.MethodGet, "/v1/payables?as_at=2026-10-31", reader, nil)
	ps, _ := p.Body["facts"].([]any)
	if p.Status != http.StatusOK || len(ps) != 1 || !strings.Contains(ap.Raw, ps[0].(map[string]any)["value"].(string)) {
		t.Errorf("by vendor: %d %s; the ageing %s", p.Status, p.Raw, ap.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/metrics:query", e.as(authz.BoardViewer), map[string]any{"measure": "receivables", "by": "customer"}),
		"queryMetrics", 403, CodeForbidden)
}

// By department, the profit and loss's pivot: each value's lines, and the lines with none (FR-DIM-02).
// req: AI-03
func TestTheSemanticLayerSplitsByDepartment(t *testing.T) {
	e := newEnv(t)
	acc := kpiBooks(e)
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	var eng storage.DimensionValue
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		dims, err := u.Dimensions().List(ctx)
		if err != nil {
			return err
		}
		var dept uuid.UUID
		for _, d := range dims {
			if d.Kind == storage.DimensionDepartment {
				dept = d.ID
			}
		}
		if dept == uuid.Nil {
			d, err := u.Dimensions().Create(ctx, storage.NewDimension{Kind: storage.DimensionDepartment, Name: "Department"})
			if err != nil {
				return err
			}
			dept = d.ID
		}
		if eng, err = u.Dimensions().CreateValue(ctx, storage.NewDimensionValue{DimensionID: dept, Code: "ENG", Name: "Engineering"}); err != nil {
			return err
		}
		id := uuid.NewString()
		_, err = u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(time.November, 26), PostingIdentity: "manual_journal:" + id + ":v1:test@1", RuleVersion: "test@1",
			Source: storage.Source{Type: "manual_journal", ID: id, Version: 1}, Lines: []storage.NewLine{
				{AccountID: acc["1100"].ID, Currency: "SGD", Amount: "700.00", FXRate: "1", FXRateSource: "base", BaseAmount: "700.00"},
				{AccountID: acc["4020"].ID, Currency: "SGD", Amount: "-700.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-700.00", DimensionValues: []uuid.UUID{eng.ID}},
			}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r := e.call(http.MethodPost, "/v1/metrics:query", e.as(authz.Controller), map[string]any{"measure": "revenue", "from": "2026-11", "to": "2026-11", "by": "department"})
	got := map[string]string{}
	for _, f := range r.Body["facts"].([]any) {
		m := f.(map[string]any)
		got[m["member"].(string)+"|"+m["member_name"].(string)] = m["value"].(string)
	}
	if r.Status != http.StatusOK || !same(got[eng.ID.String()+"|ENG Engineering"], "700.00") || len(got) != 2 {
		t.Errorf("revenue by department: %d %s", r.Status, r.Raw)
	}
	for k, v := range got {
		if strings.HasPrefix(k, "|") && !same(v, "6000.00") {
			t.Errorf("the lines with no department: %s %s", k, v)
		}
	}
}
