//go:build local

package api

import (
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
		"contacts": "Matrix Imports Pte Ltd,Customer,,,,SGD,30,,SG,\r\n",
		"items":    "MTX-1,Matrix service,,Service,hour,10,,SR,Yes,\r\n",
		"accounts": "6996,Matrix expense,Expense,Other expenses,,G&A,No,,Yes,\r\n",
	}
	ops := map[string][2]string{"contacts": {"getContactsTemplate", "importContacts"}, "items": {"getItemsTemplate", "importItems"},
		"accounts": {"getAccountsTemplate", "importAccounts"}}
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
