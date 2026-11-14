//go:build local

package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/migration"
)

// The generic import (FR-MIG-03, design D9; rev 0.270): each entity's template is its workbook sheet's columns, as CSV
// or XLSX; a dry run reports what the file would make and saves nothing; the import makes each row's record as entering
// it by hand does, the same contact as createContact makes; a file with a row refused imports nothing and names the row
// and column as the file numbers them; an item or an account of a code the organisation has refuses the file.
// req: FR-MIG-03
func TestTheGenericImportEntersEachRowAsByHand(t *testing.T) {
	e := newEnv(t)
	ctrl := e.as(authz.Controller)
	for _, entity := range migration.ImportEntities() {
		for format, want := range map[string]string{"csv": "text/csv", "xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"} {
			r := e.call(http.MethodGet, "/v1/imports/"+entity+"/template?format="+format, ctrl, nil)
			if r.Status != http.StatusOK || !strings.HasPrefix(r.Header.Get("Content-Type"), want) || !strings.Contains(r.Header.Get("Content-Disposition"), entity+"-template."+format) {
				t.Errorf("%s's %s template: %d %v", entity, format, r.Status, r.Header)
			}
		}
	}
	tpl := e.call(http.MethodGet, "/v1/imports/contacts/template", ctrl, nil).Raw
	contacts := func() int {
		return len(e.call(http.MethodGet, "/v1/contacts?limit=200", ctrl, nil).Body["items"].([]any))
	}
	before := contacts()
	file := tpl + "Acme Imports Pte Ltd,Customer,ar@acme.example,+65 6123 4567,1 Example Road,SGD,30,,SG,\r\n" +
		"\"Beta, Gamma LLP\",Supplier,ap@beta.example,,,USD,14,,US,\r\n"
	dry := e.upload("/v1/imports/contacts", ctrl, nil, "contacts.csv", []byte(file), "Prefer", "return=preview")
	if dry.Status != http.StatusOK || dry.Body["made"] != float64(2) || dry.Body["errors"] != float64(0) || contacts() != before {
		t.Fatalf("the dry run: %d %s, %d contacts", dry.Status, dry.Raw, contacts())
	}
	done := e.upload("/v1/imports/contacts", ctrl, nil, "contacts.csv", []byte(file), "Idempotency-Key", "imp-1")
	if done.Status != http.StatusCreated || done.Body["made"] != float64(2) || contacts() != before+2 {
		t.Fatalf("the import: %d %s", done.Status, done.Raw)
	}
	// The same contact entered by hand.
	hand := e.call(http.MethodPost, "/v1/contacts", ctrl, map[string]any{"legal_name": "Acme By Hand Pte Ltd", "customer": true, "billing_email": "ar@acme.example",
		"phone": "+65 6123 4567", "address": "1 Example Road", "currency": "SGD", "payment_terms_days": 30, "country": "SG"})
	if hand.Status != http.StatusCreated {
		t.Fatalf("by hand: %d %s", hand.Status, hand.Raw)
	}
	var imported map[string]any
	for _, c := range e.call(http.MethodGet, "/v1/contacts?limit=200", ctrl, nil).Body["items"].([]any) {
		if c.(map[string]any)["legal_name"] == "Acme Imports Pte Ltd" {
			imported = c.(map[string]any)
		}
	}
	for _, field := range []string{"customer", "vendor", "billing_email", "phone", "address", "currency", "payment_terms_days", "country"} {
		if imported == nil || imported[field] != hand.Body[field] {
			t.Errorf("the imported contact's %s: %v, by hand %v", field, imported[field], hand.Body[field])
		}
	}

	// A row refused refuses the file, naming its row as the file numbers it.
	bad := tpl + "Gamma Pte Ltd,Customer,,,,,30,,SG,\r\nDelta Pte Ltd,Customer,,,,,4000,,SG,\r\n"
	r := e.upload("/v1/imports/contacts", ctrl, nil, "bad.csv", []byte(bad))
	e.problemIs(r, "importContacts", 422, migration.CodeGeneric)
	if !strings.Contains(r.Raw, `"rows[3].payment_terms_days"`) || contacts() != before+3 {
		t.Errorf("the refused file: %s, %d contacts", r.Raw, contacts())
	}
	if d := e.upload("/v1/imports/contacts", ctrl, nil, "bad.csv", []byte(bad), "Prefer", "return=preview"); d.Status != http.StatusOK || d.Body["errors"] != float64(1) ||
		d.Body["made"] != float64(0) {
		t.Errorf("the refused file's dry run: %d %s", d.Status, d.Raw)
	}

	// Items and accounts, and their codes once.
	items := e.call(http.MethodGet, "/v1/imports/items/template", ctrl, nil).Raw
	if r := e.upload("/v1/imports/items", ctrl, nil, "items.csv", []byte(items+"IMP-1,Imported service,Hourly advice,Service,hour,150,,SR,Yes,\r\n")); r.Status != http.StatusCreated {
		t.Errorf("the items: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.upload("/v1/imports/items", ctrl, nil, "items.csv", []byte(items+"IMP-1,Again,,Service,hour,1,,SR,Yes,\r\n")), "importItems", 422, migration.CodeGeneric)
	acct := e.as(authz.Accountant) // C on the chart (Appendix D)
	accounts := e.call(http.MethodGet, "/v1/imports/accounts/template", acct, nil).Raw
	if r := e.upload("/v1/imports/accounts", acct, nil, "accounts.csv", []byte(accounts+"6995,Imported expense,Expense,Other expenses,,G&A,No,,Yes,\r\n")); r.Status != http.StatusCreated {
		t.Errorf("the accounts: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.upload("/v1/imports/accounts", acct, nil, "accounts.csv", []byte(accounts+"6995,Again,Expense,Other expenses,,G&A,No,,Yes,\r\n")), "importAccounts", 422,
		migration.CodeGeneric)
}

// matrixImports are the authorisation matrix's rows for the generic import (FR-MIG-03): each template under its entity's
// V, each import, as a dry run that saves nothing, under its C.
var matrixImports = map[string]func(e *env, tok string) (response, func() bool){}

func init() {
	files := map[string]string{
		"contacts":         "Matrix Imports Pte Ltd,Customer,,,,SGD,30,,SG,\r\n",
		"items":            "MTX-1,Matrix service,,Service,hour,10,,SR,Yes,\r\n",
		"accounts":         "6996,Matrix expense,Expense,Other expenses,,G&A,No,,Yes,\r\n",
		"journals":         "MJ-1,2026-10-01,6310,Matrix rent,10,,,,\r\nMJ-1,2026-10-01,1020,Matrix rent,,10,,,\r\n",
		"invoices":         "MINV-1,2026-10-01,Matrix Imports Pte Ltd,4010,Matrix service,,100,,,,,,,,,,\r\n",
		"bills":            "MB-1,2026-10-01,Matrix Imports Pte Ltd,6310,Matrix rent,,100,,,,,,,,,,\r\n",
		"fx-rates":         "USD,2026-10-01,1.30\r\n",
		"bank-rules":       "MATRIX PAYMENT,6310,\r\n",
		"opening-balances": "1020,Bank,100.00\r\n",
		"budgets":          "2026-01-01,6310,1,1,1,1,1,1,1,1,1,1,1,1,\r\n",
	}
	ops := map[string][2]string{"contacts": {"getContactsTemplate", "importContacts"}, "items": {"getItemsTemplate", "importItems"},
		"accounts": {"getAccountsTemplate", "importAccounts"}, "journals": {"getJournalsTemplate", "importJournals"},
		"invoices": {"getInvoicesTemplate", "importInvoices"}, "bills": {"getBillsTemplate", "importBills"},
		"fx-rates": {"getFxRatesTemplate", "importFxRates"}, "bank-rules": {"getBankRulesTemplate", "importBankRules"},
		"opening-balances": {"getOpeningBalancesTemplate", "importOpeningBalances"}, "budgets": {"getBudgetsTemplate", "importBudgets"}}
	for entity, op := range ops {
		matrixImports[op[0]] = func(e *env, tok string) (response, func() bool) {
			return e.call(http.MethodGet, "/v1/imports/"+entity+"/template", tok, nil), nil
		}
		matrixImports[op[1]] = func(e *env, tok string) (response, func() bool) {
			tpl := e.call(http.MethodGet, "/v1/imports/"+entity+"/template", e.as(authz.Owner), nil).Raw
			return e.upload("/v1/imports/"+entity, tok, nil, entity+".csv", []byte(tpl+files[entity]), "Prefer", "return=preview"), nil
		}
	}
}

// The generic import's documents (FR-MIG-03; rev 0.271): a journal, an invoice and a bill are made drafts, nothing
// posted, each keeping the file's number and naming the organisation's contacts by their names; a bill's GST is
// RND-GST-EXCL at its code's rate and its due date its row's terms; a row naming a customer the organisation does not
// have refuses the file, saying to import the contact first.
// req: FR-MIG-03
func TestTheGenericImportMakesDocumentsDrafts(t *testing.T) {
	e := newEnv(t)
	ctrl := e.as(authz.Accountant) // C on journals, invoices and bills (Appendix D)
	e.customer()
	e.supplier()
	find := func(list, field, value string) map[string]any {
		t.Helper()
		r := e.call(http.MethodGet, list+"?limit=200", ctrl, nil)
		for _, x := range r.Body["items"].([]any) {
			if m := x.(map[string]any); m[field] == value {
				return m
			}
		}
		t.Fatalf("%s has no %s %s: %s", list, field, value, r.Raw)
		return nil
	}

	tpl := e.call(http.MethodGet, "/v1/imports/journals/template", ctrl, nil).Raw
	file := tpl + "IJ-1,2026-10-20,6310,Imported rent,1200.00,,,,\r\nIJ-1,2026-10-20," + e.books.Bank.Code + ",Imported rent,,1200.00,,,\r\n"
	file = strings.ReplaceAll(file, "6310", e.books.Rent.Code)
	if r := e.upload("/v1/imports/journals", ctrl, nil, "journals.csv", []byte(file)); r.Status != http.StatusCreated || r.Body["made"] != float64(1) {
		t.Fatalf("the journals: %d %s", r.Status, r.Raw)
	}
	if j := find("/v1/journals", "number", "IJ-1"); j["status"] != "draft" {
		t.Errorf("the imported journal: %v", j)
	}

	tpl = e.call(http.MethodGet, "/v1/imports/invoices/template", ctrl, nil).Raw
	file = tpl + "IINV-1,2026-10-14,Acme Analytics Pte Ltd," + e.books.Sales.Code + ",Analytics platform,,1000.00,SR,30,,,,,,,,\r\n"
	if r := e.upload("/v1/imports/invoices", ctrl, nil, "invoices.csv", []byte(file)); r.Status != http.StatusCreated || r.Body["made"] != float64(1) {
		t.Fatalf("the invoices: %d %s", r.Status, r.Raw)
	}
	if inv := find("/v1/invoices", "number", "IINV-1"); inv["status"] != "draft" || inv["due_date"] != "2026-11-13" {
		t.Errorf("the imported invoice: %v", inv)
	}

	tpl = e.call(http.MethodGet, "/v1/imports/bills/template", ctrl, nil).Raw
	file = tpl + "SUP-77,2026-10-14,," + e.books.Rent.Code + ",Office rent,,1000.00,TX,14,,,,,,,,\r\n"
	sup := e.call(http.MethodGet, "/v1/contacts/"+e.supplier(), ctrl, nil).str("legal_name")
	file = strings.Replace(file, "2026-10-14,,", "2026-10-14,"+sup+",", 1)
	if r := e.upload("/v1/imports/bills", ctrl, nil, "bills.csv", []byte(file)); r.Status != http.StatusCreated || r.Body["made"] != float64(1) {
		t.Fatalf("the bills: %d %s", r.Status, r.Raw)
	}
	b := find("/v1/bills", "supplier_ref", "SUP-77")
	if b["status"] != "draft" || b["due_date"] != "2026-10-28" {
		t.Errorf("the imported bill: %v", b)
	}
	if full := e.call(http.MethodGet, "/v1/bills/"+b["id"].(string), ctrl, nil); !strings.Contains(full.Raw, `"gst":{"amount":"90.00"`) {
		t.Errorf("the imported bill's GST: %s", full.Raw)
	}

	// A customer the organisation does not have refuses the file.
	bad := e.call(http.MethodGet, "/v1/imports/invoices/template", ctrl, nil).Raw + "IINV-2,2026-10-14,Nobody Pte Ltd," + e.books.Sales.Code + ",x,,10.00,SR,,,,,,,,,\r\n"
	r := e.upload("/v1/imports/invoices", ctrl, nil, "bad.csv", []byte(bad))
	e.problemIs(r, "importInvoices", 422, migration.CodeGeneric)
	if !strings.Contains(r.Raw, `"rows[2].contact"`) || !strings.Contains(r.Raw, "Import the contact first") {
		t.Errorf("the unknown customer: %s", r.Raw)
	}
}

// The generic import's opening balances and plans (FR-MIG-03; rev 0.272): monthly FX rates recorded as by hand; bank
// rules after the organisation's own, each suggesting; the opening balances a draft, each on its account's own side,
// posted later through its approval; a financial year's budget a draft, in the organisation's years. A rule giving a
// money account, a balance on an account the chart does not have and a budget's months before the books start each
// refuse the file, naming their row.
// req: FR-MIG-03
func TestTheGenericImportEntersOpeningBalancesAndPlans(t *testing.T) {
	e := newEnv(t)
	ctrl, acct := e.as(authz.Controller), e.as(authz.Accountant)
	tag := e.call(http.MethodGet, "/v1/organisation/profile", ctrl, nil).Header.Get("ETag")
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", ctrl, map[string]any{"fy_start_month": 1, "books_start": "2026-10-01",
		"first_year_end": "2026-12-31"}, "If-Match", tag); p.Status != http.StatusOK {
		t.Fatalf("the books start: %d %s", p.Status, p.Raw)
	}
	tpl := func(entity, tok string) string {
		return e.call(http.MethodGet, "/v1/imports/"+entity+"/template", tok, nil).Raw
	}
	items := func(path, tok string) []map[string]any {
		t.Helper()
		var out []map[string]any
		for _, x := range e.call(http.MethodGet, path, tok, nil).Body["items"].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}
	rent, sales := e.books.Rent.Code, e.books.Sales.Code

	r := e.upload("/v1/imports/fx-rates", ctrl, nil, "rates.csv", []byte(tpl("fx-rates", ctrl)+"USD,2026-10-15,1.2950\r\nEUR,2026-10-01,1.4500\r\n"))
	if r.Status != http.StatusCreated || r.Body["made"] != float64(2) || r.Body["rows"] != float64(2) || len(items("/v1/fx/rates?currency=USD", ctrl)) != 1 {
		t.Errorf("the FX rates: %d %s", r.Status, r.Raw)
	}

	if own := e.call(http.MethodPost, "/v1/bank-rules", acct, e.ruleDraft("Own rule")); own.Status != http.StatusCreated {
		t.Fatalf("the organisation's own rule: %d %s", own.Status, own.Raw)
	}
	r = e.upload("/v1/imports/bank-rules", acct, nil, "rules.csv", []byte(tpl("bank-rules", acct)+"GOOGLE,"+rent+",TX\r\nZOOM,"+rent+",\r\n"))
	if r.Status != http.StatusCreated || r.Body["made"] != float64(2) {
		t.Fatalf("the bank rules: %d %s", r.Status, r.Raw)
	}
	positions := map[string]any{}
	for _, x := range items("/v1/bank-rules", acct) {
		positions[x["name"].(string)] = x["position"]
	}
	if positions["Own rule"] != float64(1) || positions["Contains GOOGLE"] != float64(2) || positions["Contains ZOOM"] != float64(3) {
		t.Errorf("the rules' places: %v", positions)
	}
	money := e.chartMoneyAccount().Code
	r = e.upload("/v1/imports/bank-rules", acct, nil, "rules.csv", []byte(tpl("bank-rules", acct)+"SWEEP,"+money+",\r\n"))
	e.problemIs(r, "importBankRules", 422, migration.CodeGeneric)
	if !strings.Contains(r.Raw, `"rows[2].account_id"`) {
		t.Errorf("a rule giving a money account: %s", r.Raw)
	}

	r = e.upload("/v1/imports/opening-balances", acct, nil, "opening.csv", []byte(tpl("opening-balances", acct)+e.books.Bank.Code+",Bank,10000.00\r\n"+
		sales+",Sales,10000.00\r\n"))
	if r.Status != http.StatusCreated || r.Body["made"] != float64(1) || r.Body["rows"] != float64(2) {
		t.Fatalf("the opening balances: %d %s", r.Status, r.Raw)
	}
	ob := e.call(http.MethodGet, "/v1/opening-balances", acct, nil)
	if ob.Body["status"] != "draft" || !strings.Contains(ob.Raw, `"amount":"10000.00"`) || !strings.Contains(ob.Raw, `"amount":"-10000.00"`) {
		t.Errorf("the opening balances' draft: %s", ob.Raw)
	}
	r = e.upload("/v1/imports/opening-balances", acct, nil, "opening.csv", []byte(tpl("opening-balances", acct)+e.books.Bank.Code+",Bank,5.00\r\n9999,None,5.00\r\n"))
	e.problemIs(r, "importOpeningBalances", 422, migration.CodeGeneric)
	if !strings.Contains(r.Raw, `"rows[3].account"`) {
		t.Errorf("a balance on an account the chart does not have: %s", r.Raw)
	}

	r = e.upload("/v1/imports/budgets", acct, nil, "budgets.csv", []byte(tpl("budgets", acct)+"2026-10-01,"+rent+",100.00,200.00,300.00,,,,,,,,,,\r\n"))
	if r.Status != http.StatusCreated || r.Body["made"] != float64(1) {
		t.Fatalf("the budgets: %d %s", r.Status, r.Raw)
	}
	if bs := items("/v1/budgets", acct); len(bs) != 1 || bs[0]["status"] != "draft" || !strings.Contains(fmt.Sprint(bs[0]["budget"]), "Budget FY2026 (imported)") {
		t.Errorf("the imported budget: %v", bs)
	}
	r = e.upload("/v1/imports/budgets", acct, nil, "budgets.csv", []byte(tpl("budgets", acct)+"2026-01-01,"+rent+",5.00,,,,,,,,,,,,\r\n"))
	e.problemIs(r, "importBudgets", 422, migration.CodeGeneric)
	if !strings.Contains(r.Raw, "fall before the books start") || !strings.Contains(r.Raw, `"rows[2].`) {
		t.Errorf("a budget before the books start: %s", r.Raw)
	}
}
