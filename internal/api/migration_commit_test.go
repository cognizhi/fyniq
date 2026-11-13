//go:build local

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/migration"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// freshFounders are the two founders of a new organisation with no chart, no periods and no contacts: the one a
// workbook is imported into (rev 0.254).
func freshFounders(t *testing.T) founders {
	e := newEnv(t)
	org, err := e.store.CreateOrganisation(context.Background(), storage.NewOrganisation{Name: "Imported Pte Ltd", BaseCurrency: "SGD"})
	if err != nil {
		t.Fatal(err)
	}
	e.books = porttest.Books{Org: org.ID}
	relaxEvidence(t, e.store, org.ID)
	return foundersIn(e)
}

// signedMigration is f's migration with the sample validated and its mapping signed by the Accountant, as accountant.
func signedMigration(f founders, accountant string) string {
	e := f.e
	e.t.Helper()
	id := e.startedMigration()
	if r := e.upload("/v1/migrations/"+id+"/workbook", e.as(authz.Controller), nil, "Cognizhi-Finance-Hub.xlsx", sampleWorkbook(e.t),
		"If-Match", e.migrationTag(id)); r.Status != http.StatusAccepted {
		e.t.Fatalf("uploading: %d %s", r.Status, r.Raw)
	}
	e.validate()
	m, tag := e.migrationMapping(id)
	if c := e.call(http.MethodPatch, "/v1/migrations/"+id+"/mapping", accountant, map[string]any{"natures": natures(m)}, "If-Match", tag); c.Status != http.StatusOK ||
		c.Body["errors"] != float64(0) {
		e.t.Fatalf("choosing the natures of payment: %d %s", c.Status, c.Raw)
	}
	_, tag = e.migrationMapping(id)
	if s := e.call(http.MethodPost, "/v1/migrations/"+id+"/mapping/sign-off", accountant, nil, "If-Match", tag); s.Status != http.StatusOK {
		e.t.Fatalf("signing the mapping: %d %s", s.Status, s.Raw)
	}
	return id
}

// committed asks for the migration's commit as accountant, and approves it as owner: the approval.
func committed(f founders, id, accountant, owner, path string) response {
	e := f.e
	e.t.Helper()
	_, tag := e.migrationMapping(id)
	asked := e.call(http.MethodPost, "/v1/migrations/"+id+"/commit", accountant, map[string]any{"path": path}, "If-Match", tag)
	if asked.Status != http.StatusAccepted || asked.str("object_type") != approval.ObjectMigrationCommit || asked.str("status") != "pending" {
		e.t.Fatalf("asking to commit: %d %s", asked.Status, asked.Raw)
	}
	if a := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); a.Status != http.StatusOK || a.str("status") != "approved" {
		e.t.Fatalf("approving the commit: %d %s", a.Status, a.Raw)
	}
	return asked
}

// run sends the commit's next batch as tok.
func (e *env) run(id, tok string) response {
	e.t.Helper()
	return e.call(http.MethodPost, "/v1/migrations/"+id+"/commit:run", tok, nil)
}

// The commit is one approved request, then batches that resume where they stopped (FR-MIG-01 rev 0.258, design D3).
// The Accountant asks, with the mapping's ETag, for the sample's history; the request names the workbook, its report,
// the signed mapping and the records it plans by kind. Nothing is imported before an Owner who did not ask approves it,
// and the asker cannot. Approved, each batch imports at most 50 records: a preview lists the next batch and imports
// nothing; a bank rule whose account was put out of use stops a batch at its Lists row, with what is wrong and how to
// fix it, the records before it imported; once the account is back in use, the next batch resumes at that rule, and the
// last commits the import. Every record is imported once, the approval is the import's one entry in the weekly review,
// and the workbook and the mapping are fixed from then on.
// req: FR-MIG-01
// req: data/migration History or opening balances import (M3)
// asvs: V8.2.2, V11.1.1
func TestACommitResumesWhereItStopped(t *testing.T) {
	f := freshFounders(t)
	e := f.e
	accountant, owner := f.as(f.ahkow, authz.Accountant), f.as(f.weiling, authz.Owner)
	id := signedMigration(f, accountant)
	commit := "/v1/migrations/" + id + "/commit"

	e.problemIs(e.run(id, accountant), "runMigrationCommit", 409, migration.CodeCommit) // not asked yet
	if g := e.call(http.MethodGet, commit, accountant, nil); g.Status != http.StatusOK || g.str("status") != "none" {
		t.Fatalf("the commit before it is asked for: %d %s", g.Status, g.Raw)
	}
	if pv := e.call(http.MethodPost, commit+":run", accountant, nil, "Prefer", "return=preview"); pv.Status != http.StatusOK ||
		len(pv.Body["planned"].([]any)) != migration.BatchSize || pv.str("status") != "none" {
		t.Fatalf("the first batch's preview before the request: %d %s", pv.Status, pv.Raw)
	}
	e.problemIs(e.call(http.MethodPost, commit, accountant, map[string]any{"path": "history"}), "requestMigrationCommit", 428, CodeIfMatchRequired)
	_, tag := e.migrationMapping(id)
	e.problemIs(e.call(http.MethodPost, commit, e.as(authz.Bookkeeper), map[string]any{"path": "history"}, "If-Match", tag), "requestMigrationCommit", 403, CodeForbidden)
	if pv := e.call(http.MethodPost, commit, accountant, map[string]any{"path": "history"}, "If-Match", tag, "Prefer", "return=preview"); pv.Status != http.StatusAccepted ||
		pv.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("a preview of the request: %d %s", pv.Status, pv.Raw)
	}
	asked := e.call(http.MethodPost, commit, accountant, map[string]any{"path": "history"}, "If-Match", tag, "Idempotency-Key", "commit-1")
	if asked.Status != http.StatusAccepted || !strings.Contains(asked.Raw, `{"field":"path","before":"","after":"history"}`) ||
		!strings.Contains(asked.Raw, `{"field":"records.contact","before":"","after":"16"}`) || !strings.Contains(asked.Raw, `{"field":"records.bank_rule","before":"","after":"28"}`) ||
		!strings.Contains(asked.Raw, `{"field":"books_start","before":"","after":"2026-01-01"}`) || !strings.Contains(asked.str("summary"), "Cognizhi-Finance-Hub.xlsx (history)") {
		t.Fatalf("asking to commit: %d %s", asked.Status, asked.Raw)
	}
	e.problemIs(e.call(http.MethodPost, commit, accountant, map[string]any{"path": "history"}, "If-Match", tag), "requestMigrationCommit", 409, approval.CodePending)
	e.problemIs(e.run(id, accountant), "runMigrationCommit", 409, migration.CodeCommit) // asked, not approved
	if g := e.call(http.MethodGet, commit, e.as(authz.Auditor), nil); g.Status != http.StatusOK || g.str("status") != "pending" ||
		g.str("approval_id") != asked.str("id") || g.str("path") != "history" || g.Body["done"] != float64(0) {
		t.Fatalf("the commit waiting for its approval: %d %s", g.Status, g.Raw)
	}
	e.problemIs(f.decide(accountant, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
	if a := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); a.Status != http.StatusOK || a.str("status") != "approved" {
		t.Fatalf("approving: %d %s", a.Status, a.Raw)
	}
	c := e.call(http.MethodGet, commit, e.as(authz.Auditor), nil)
	total := int(c.Body["total"].(float64))
	if c.Status != http.StatusOK || c.str("status") != "running" || c.str("approval_id") != asked.str("id") || c.str("path") != "history" ||
		c.Body["done"] != float64(0) || total < 100 || c.str("approved_by") != "user:"+f.weiling {
		t.Fatalf("the approved commit: %d %s", c.Status, c.Raw)
	}
	if contacts := e.call(http.MethodGet, "/v1/contacts", accountant, nil); strings.Contains(contacts.Raw, "Harbourline") {
		t.Fatal("the approval imported records: only the batches do")
	}

	pv := e.call(http.MethodPost, "/v1/migrations/"+id+"/commit:run", accountant, nil, "Prefer", "return=preview")
	planned, _ := pv.Body["planned"].([]any)
	if pv.Status != http.StatusOK || pv.Header.Get("Preference-Applied") != "return=preview" || len(planned) != migration.BatchSize || pv.Body["imported"] != float64(0) ||
		planned[0].(map[string]any)["key"] != "settings" {
		t.Fatalf("a preview of the first batch: %d %s", pv.Status, pv.Raw)
	}
	first := e.run(id, accountant)
	next, _ := first.Body["next"].(map[string]any)
	if first.Status != http.StatusOK || first.Body["imported"] != float64(50) || first.Body["done"] != float64(50) || first.str("status") != "running" || next == nil {
		t.Fatalf("the first batch: %d %s", first.Status, first.Raw)
	}

	// A bank rule's account put out of use stops the batch at the rule's row; the records before it stay imported. The
	// rule is the last whose account no other rule gives and is not a system account (FR-COA-03), so the first batch has
	// not imported it.
	var rule int
	var acct storage.Account
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		m, _, err := migration.LatestMapping(ctx, u, uuid.MustParse(id))
		if err != nil {
			return err
		}
		uses := map[string]int{}
		for _, r := range m.BankRules {
			uses[r.Account]++
		}
		for _, r := range slices.Backward(m.BankRules) {
			if uses[r.Account] != 1 {
				continue
			}
			if acct, err = u.Accounts().ByCode(ctx, r.Account); err != nil {
				return err
			}
			if acct.Role == "" {
				rule = r.Order
				break
			}
		}
		_, err = u.Accounts().SetActive(storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}}), acct.ID, false)
		return err
	}); err != nil || rule == 0 {
		t.Fatalf("putting rule %d's account %s out of use: %v", rule, acct.Code, err)
	}
	var stopped response
	for i := 0; i < 10; i++ {
		if stopped = e.run(id, accountant); stopped.Status != http.StatusOK {
			break
		}
	}
	e.problemIs(stopped, "runMigrationCommit", 422, migration.CodeImport)
	if !strings.Contains(stopped.str("detail"), "Lists row") || !strings.Contains(stopped.str("detail"), "bank-rule:"+strconv.Itoa(rule)+")") ||
		!strings.Contains(stopped.str("detail"), "is not an active account") {
		t.Fatalf("the stop names no row of rule %d: %s", rule, stopped.Raw)
	}
	held := e.call(http.MethodGet, commit, accountant, nil)
	done := held.Body["done"].(float64)
	if held.str("status") != "running" || done <= 50 || done >= float64(total) {
		t.Fatalf("the records before the stop: %s", held.Raw)
	}
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Accounts().SetActive(storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}}), acct.ID, true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Anyone the policy lets run it resumes it, but only a person signed in to Fyniq (rev 0.262).
	e.problemIs(e.run(id, e.as(authz.Controller)), "runMigrationCommit", 409, migration.CodeCommit)
	resumed := e.run(id, f.as(f.weiling, authz.Controller))
	if resumed.Status != http.StatusOK || resumed.Body["imported"].(float64) < 1 || resumed.Body["done"].(float64) <= done {
		t.Fatalf("resuming: %d %s", resumed.Status, resumed.Raw)
	}
	last := resumed
	for i := 0; i < 10 && last.str("status") == "running"; i++ {
		last = e.run(id, accountant)
	}
	if last.str("status") != "committed" || last.Body["done"] != float64(total) || last.str("committed_at") == "" || last.Body["next"] != nil {
		t.Fatalf("the last batch: %s", last.Raw)
	}
	if again := e.run(id, accountant); again.Status != http.StatusOK || again.Body["imported"] != float64(0) || again.str("status") != "committed" {
		t.Fatalf("a batch after the commit: %d %s", again.Status, again.Raw)
	}

	// Each record once: 16 contacts, the 2 founders and the Funding sheet's 2 other holders and SAFE investor (rev
	// 0.262), 5 items, 28 bank rules (the 29th, to a money account, is a transfer, kept in the archive).
	if l := e.call(http.MethodGet, "/v1/contacts?limit=100", accountant, nil); strings.Count(l.Raw, `"legal_name":"Harbourline Logistics Pte Ltd"`) != 1 ||
		strings.Count(l.Raw, `"legal_name":"Alex Tan"`) != 1 || len(l.Body["items"].([]any)) != 21 {
		t.Errorf("the contacts: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/items?limit=100", accountant, nil); len(l.Body["items"].([]any)) != 5 {
		t.Errorf("the items: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/bank-rules", accountant, nil); len(l.Body["items"].([]any)) != 28 {
		t.Errorf("the bank rules: %s", l.Raw)
	}

	// The approval is the import's one entry in the weekly review (FM-3); its records are not reviewed one by one.
	log := e.call(http.MethodGet, "/v1/review-log", owner, nil)
	if strings.Count(log.Raw, `"object_type":"migration_commit"`) != 1 || strings.Contains(log.Raw, `"kind":"self_approved"`) {
		t.Errorf("the weekly review: %s", log.Raw)
	}
	trail := auditTrail(t, e)
	for _, action := range []string{"migration.commit_approved", "migration.record_imported", "migration.committed"} {
		if !strings.Contains(trail, `"action":"`+action+`"`) {
			t.Errorf("no audit event %s", action)
		}
	}

	// Committed, the workbook and its mapping are fixed.
	e.problemIs(e.upload("/v1/migrations/"+id+"/workbook", accountant, nil, "Hub.xlsx", sampleWorkbook(t), "If-Match", e.migrationTag(id)),
		"uploadMigrationWorkbook", 409, migration.CodeCommit)
	_, tag = e.migrationMapping(id)
	e.problemIs(e.call(http.MethodPatch, "/v1/migrations/"+id+"/mapping", accountant, map[string]any{"budget_lines": []map[string]any{{"line": "Subscription revenue",
		"account": "4010"}}}, "If-Match", tag), "changeMigrationMapping", 409, migration.CodeCommit)
	e.problemIs(e.call(http.MethodPost, commit, accountant, map[string]any{"path": "history"}, "If-Match", tag), "requestMigrationCommit", 409, migration.CodeCommit)
}

// Every imported record names its row (design D3): each migration record names the commit, whose approval it carries
// out, the business key of its row, the sheet and the row, and the record it made. The set-up is the workbook's: the
// profile and its financial year from Settings, the registration, the accounts the mapping creates, the departments
// and projects, each contact from its Contacts row with its role, terms and category, each item with its price, the
// FX rates by month and the bank rules in their order; the contact, account and rule audit events carry the approval.
// req: FR-MIG-01
// req: data/migration Set-up import and mapping review (M2)
func TestEveryImportedRecordNamesItsRow(t *testing.T) {
	f := freshFounders(t)
	e := f.e
	accountant, owner := f.as(f.ahkow, authz.Accountant), f.as(f.weiling, authz.Owner)
	id := signedMigration(f, accountant)
	asked := committed(f, id, accountant, owner, "history")
	for i := 0; i < 10; i++ {
		r := e.run(id, accountant)
		if r.Status != http.StatusOK {
			t.Fatalf("a batch: %d %s", r.Status, r.Raw)
		}
		if r.str("status") == "committed" {
			break
		}
	}
	w, err := migration.Read(sampleWorkbook(t))
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]migration.Origin{}
	for _, c := range w.Contacts {
		rows["contact:"+strings.ToLower(c.Name)] = c.Origin
	}
	for _, it := range w.Items {
		rows["item:"+it.Code] = it.Origin
	}
	for _, r := range w.Rates {
		rows["fx-rate:"+r.Currency+":"+r.Month.Format("2006-01")] = r.Origin
	}
	for i, r := range w.BankRules {
		rows["bank-rule:"+strconv.Itoa(i+1)] = r.Origin
	}
	var records []storage.MigrationRecord
	var c storage.MigrationCommit
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if c, err = u.Migrations().Commit(ctx, uuid.MustParse(id)); err != nil {
			return err
		}
		if records, err = u.Migrations().Records(ctx, c.ID); err != nil {
			return err
		}
		for _, r := range records {
			x, err := u.Migrations().RecordOf(ctx, r.RecordType, r.RecordID)
			if err != nil || (x.Key != r.Key && r.RecordType != "organisation") {
				t.Errorf("%s %s traces to %+v: %v", r.RecordType, r.RecordID, x, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if c.Status != storage.CommitCommitted || c.ApprovalID.String() != asked.str("id") || len(records) != c.Records {
		t.Fatalf("the commit: %+v, %d records", c, len(records))
	}
	kinds := map[string]int{}
	for _, r := range records {
		kinds[r.Kind]++
		if r.CommitID != c.ID || r.Sheet == "" || r.RecordID == uuid.Nil || !strings.HasPrefix(r.ImportedBy, "user:") {
			t.Errorf("a record without its origin: %+v", r)
		}
		if want, ok := rows[r.Key]; ok && (r.Sheet != want.Sheet || r.Row != want.Row) {
			t.Errorf("%s names %s row %d; the workbook has it at %s row %d", r.Key, r.Sheet, r.Row, want.Sheet, want.Row)
		}
		switch r.Kind {
		case migration.KindAccount, migration.KindContact, migration.KindItem, migration.KindFXRate, migration.KindBankRule, migration.KindDepartment:
			if r.Row < 6 {
				t.Errorf("%s names no row: %+v", r.Key, r)
			}
		}
	}
	if kinds[migration.KindContact] != 16 || kinds[migration.KindItem] != 5 || kinds[migration.KindFXRate] != 9 || kinds[migration.KindBankRule] != 28 ||
		kinds[migration.KindDepartment] != 5 || kinds[migration.KindSettings] != 1 || kinds[migration.KindFinancialYear] != 1 || kinds[migration.KindGSTRegistration] != 1 {
		t.Errorf("the records by kind: %v", kinds)
	}

	// The set-up as the workbook keeps it.
	accountantView := e.call(http.MethodGet, "/v1/organisation/profile", accountant, nil)
	if accountantView.str("legal_name") != w.Settings.Company || accountantView.str("books_start") != "2026-01-01" || accountantView.str("first_year_end") != "2026-12-31" {
		t.Errorf("the profile: %s", accountantView.Raw)
	}
	if p := e.call(http.MethodGet, "/v1/periods", accountant, nil); !strings.Contains(p.Raw, `"starts_on":"2026-01-01"`) || !strings.Contains(p.Raw, `"starts_on":"2026-12-01"`) {
		t.Errorf("the year's periods: %s", p.Raw)
	}
	contacts := e.call(http.MethodGet, "/v1/contacts?limit=100", accountant, nil)
	if !strings.Contains(contacts.Raw, `"legal_name":"Pacific Rim Analytics Inc"`) || !strings.Contains(contacts.Raw, `"country":"US"`) ||
		!strings.Contains(contacts.Raw, `"billing_email":"accounts@harbourline.example"`) || !strings.Contains(contacts.Raw, `"payment_terms_days":14`) {
		t.Errorf("the contacts: %s", contacts.Raw)
	}
	if items := e.call(http.MethodGet, "/v1/items?limit=100", accountant, nil); !strings.Contains(items.Raw, `"code":"SVC-IMPL"`) ||
		!strings.Contains(items.Raw, `"unit_price":"1800.00"`) {
		t.Errorf("the items: %s", items.Raw)
	}
	trail := auditTrail(t, e)
	if !strings.Contains(trail, asked.str("id")) {
		t.Error("the imported records' audit events name no approval")
	}
}

// committedAs is a fresh organisation with the sample committed by path, as the accountant asked and the owner approved.
func committedAs(t *testing.T, path string) (founders, string) {
	t.Helper()
	f := freshFounders(t)
	accountant, owner := f.as(f.ahkow, authz.Accountant), f.as(f.weiling, authz.Owner)
	id := signedMigration(f, accountant)
	committed(f, id, accountant, owner, path)
	for i := 0; i < 20; i++ {
		r := f.e.run(id, accountant)
		if r.Status != http.StatusOK {
			t.Fatalf("a batch: %d %s", r.Status, r.Raw)
		}
		if r.str("status") == "committed" {
			return f, id
		}
	}
	t.Fatal("the import did not commit in 20 batches")
	return f, id
}

// importedSample is a fresh organisation with the sample committed, its history included, as the accountant asked and
// the owner approved.
func importedSample(t *testing.T) (founders, string) {
	t.Helper()
	f := freshFounders(t)
	accountant, owner := f.as(f.ahkow, authz.Accountant), f.as(f.weiling, authz.Owner)
	id := signedMigration(f, accountant)
	committed(f, id, accountant, owner, "history")
	for i := 0; i < 20; i++ {
		r := f.e.run(id, accountant)
		if r.Status != http.StatusOK {
			t.Fatalf("a batch: %d %s", r.Status, r.Raw)
		}
		if r.str("status") == "committed" {
			return f, id
		}
	}
	t.Fatal("the import did not commit in 20 batches")
	return f, id
}

// byNumber are a list's items by their field key.
func byNumber(r response, key string) map[string]map[string]any {
	out := map[string]map[string]any{}
	items, _ := r.Body["items"].([]any)
	for _, x := range items {
		m := x.(map[string]any)
		out[fmt.Sprint(m[key])] = m
	}
	return out
}

// decimalIn is an API money object's amount, as a decimal.
func decimalIn(t *testing.T, v any) *apd.Decimal {
	t.Helper()
	m, _ := v.(map[string]any)
	d, _, err := apd.NewFromString(fmt.Sprint(m["amount"]))
	if err != nil {
		t.Fatalf("not an amount: %v", v)
	}
	return d
}

// The history imports as the workbook posted it (§17.1 M3, design D5; rev 0.257, 0.259): every invoice and bill at the
// total the workbook worked out, in its currency; each invoice and journal keeping the workbook's number, the invoice
// sequence continuing past the highest; the credit note as a credit note; the draft as a draft; the opening invoice and
// bill at the books start, whose receivable and payable the opening balances carry on 3900, which nets to zero.
// req: data/migration History or opening balances import (M3)
// req: FR-MIG-01
// req: FR-GL-06
func TestTheHistoryPostsAsTheWorkbookDid(t *testing.T) {
	f, _ := importedSample(t)
	e := f.e
	accountant := f.as(f.ahkow, authz.Accountant)
	w, err := migration.Read(sampleWorkbook(t))
	if err != nil {
		t.Fatal(err)
	}
	invoices := byNumber(e.call(http.MethodGet, "/v1/invoices", accountant, nil), "number")
	for n, x := range byNumber(e.call(http.MethodGet, "/v1/invoices?kind=credit_note", accountant, nil), "number") {
		invoices[n] = x
	}
	for _, d := range w.Invoices {
		x, ok := invoices[d.Number]
		switch {
		case !ok:
			t.Errorf("invoice %s is not imported", d.Number)
		case d.Draft != (x["status"] == "draft"):
			t.Errorf("invoice %s: status %v, draft %v", d.Number, x["status"], d.Draft)
		case d.Total != nil && decimalIn(t, x["total"]).Cmp(new(apd.Decimal).Abs(d.Total)) != 0:
			t.Errorf("invoice %s: total %v; the workbook's %s", d.Number, x["total"], d.Total.Text('f'))
		case d.OpeningItem != (x["opening_item"] == true):
			t.Errorf("invoice %s: opening item %v", d.Number, x["opening_item"])
		}
	}
	if invoices["CN-0001"]["kind"] != "credit_note" || invoices["INV-0098"]["date"] != "2025-12-15" && invoices["INV-0098"]["opening_item"] != true {
		t.Errorf("the credit note and the opening invoice: %v %v", invoices["CN-0001"], invoices["INV-0098"])
	}
	bills := byNumber(e.call(http.MethodGet, "/v1/bills", accountant, nil), "supplier_ref")
	for _, d := range w.Bills {
		x, ok := bills[d.Number]
		switch {
		case !ok:
			t.Errorf("bill %s is not imported", d.Number)
		case d.Total != nil && decimalIn(t, x["total"]).Cmp(d.Total) != 0:
			t.Errorf("bill %s: total %v; the workbook's %s", d.Number, x["total"], d.Total.Text('f'))
		}
	}
	journals := byNumber(e.call(http.MethodGet, "/v1/journals", accountant, nil), "number")
	if journals["J-0001"]["status"] != "posted" || journals["J-0002"]["status"] != "posted" {
		t.Errorf("the journals: %v", journals)
	}
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		seq, err := u.Sequences().Get(ctx, storage.DocumentInvoice)
		if err == nil && seq.NextNumber != 125 {
			t.Errorf("the invoice sequence continues at %d, not past INV-0124", seq.NextNumber)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.checked("books.opening-clearing"), "ok")

	// Quotes and orders in the workbook's states, their numbers kept; nothing of them posts.
	quotes := byNumber(e.call(http.MethodGet, "/v1/quotes", accountant, nil), "number")
	if quotes["Q-0001"]["status"] != "accepted" || quotes["Q-0002"]["status"] != "sent" || quotes["Q-0003"]["status"] != "declined" {
		t.Errorf("the quotes: %v", quotes)
	}
	if orders := byNumber(e.call(http.MethodGet, "/v1/purchase-orders", accountant, nil), "number"); orders["PO-0001"]["status"] != "closed" {
		t.Errorf("the purchase order: %v", orders)
	}
	// The assets: the MacBooks bought before the books start with the S$3,000.00 the opening balances carry on 1590; the
	// laptops and monitors from the lines their cost posted on; each depreciated to 30 September as the workbook did.
	assets := byNumber(e.call(http.MethodGet, "/v1/fixed-assets", accountant, nil), "name")
	for name, want := range map[string][2]string{"MacBook Pro fleet (bought 2025)": {"3000.00", "6000.00"}, "Laptops x3 (engineering)": {"0.00", "1950.00"},
		"Monitors & docking stations": {"0.00", "333.33"}} {
		a, ok := assets[name]
		if !ok {
			t.Errorf("the asset %s is not registered: %v", name, assets)
			continue
		}
		amt := func(v any) string {
			if m, ok := v.(map[string]any); ok {
				return fmt.Sprint(m["amount"])
			}
			return "0.00" // none
		}
		if amt(a["opening_accumulated"]) != want[0] || amt(a["accumulated_depreciation"]) != want[1] {
			t.Errorf("%s: opening %v, accumulated %v; want %v", name, a["opening_accumulated"], a["accumulated_depreciation"], want)
		}
	}
}

// trialBalance is the organisation's trial balance through a month, each account's balance with debits positive, as the
// statement shows it.
func trialBalance(t *testing.T, e *env, tok, to string) map[string]*apd.Decimal {
	t.Helper()
	return trialBalanceFrom(t, e, tok, "2026-01", to)
}

func trialBalanceFrom(t *testing.T, e *env, tok, from, to string) map[string]*apd.Decimal {
	t.Helper()
	r := e.call(http.MethodGet, "/v1/reports/statements/tb?from="+from+"&to="+to, tok, nil)
	var tb struct {
		Blocks []struct {
			Key    string
			Groups []struct {
				Lines []struct {
					Code    string
					Amounts []struct{ Amount string }
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(r.Raw), &tb); r.Status != http.StatusOK || err != nil {
		t.Fatalf("the trial balance: %d %v %s", r.Status, err, r.Raw)
	}
	out := map[string]*apd.Decimal{}
	for _, b := range tb.Blocks {
		for _, g := range b.Groups {
			for _, l := range g.Lines {
				d, _, err := apd.NewFromString(l.Amounts[0].Amount)
				if err != nil {
					t.Fatal(err)
				}
				out[l.Code] = d
			}
		}
	}
	return out
}

// The Transactions sheet brings every account to the workbook's own balance (design D5, D8): at 30 September, the
// sample's "as of", each account's balance in Fyniq equals the workbook's trial balance to the cent, with the pay runs,
// the depreciation and the Funding sheet's records (rev 0.262): the seed round's S$500,000 to share application monies
// and on to 3020 by its cash issue, the SAFE's S$200,000 to 2700 naming its instrument. Each Transactions row is a line
// of its money account's statement, a transfer's two sides a line each, and none is left unreconciled. The register of
// members holds what the cap table does: the founders' and angels' 9,800,000 shares before the books start, the seed
// round's 2,000,000 from 15 June.
// req: data/migration History or opening balances import (M3)
// req: FR-MIG-01
// req: FR-BNK-09
func TestTheTransactionsBringEveryAccountToTheWorkbooksBalance(t *testing.T) {
	f, id := importedSample(t)
	e := f.e
	w, err := migration.Read(sampleWorkbook(t))
	if err != nil {
		t.Fatal(err)
	}
	accountant := f.as(f.ahkow, authz.Accountant)
	tb := trialBalance(t, e, accountant, "2026-09")
	for _, x := range w.TrialBalance {
		want := x.Closing[8]
		if want == nil {
			want = new(apd.Decimal)
		}
		got := tb[x.Code]
		if got == nil {
			got = new(apd.Decimal)
		}
		diff := new(apd.Decimal)
		if _, err := apd.BaseContext.WithPrecision(40).Sub(diff, got, want); err != nil {
			t.Fatal(err)
		}
		if !diff.IsZero() {
			t.Errorf("account %s %s: %s in Fyniq; the workbook's %s", x.Code, x.Name, got.Text('f'), want.Text('f'))
		}
	}
	for at, issued := range map[string]string{"2025-12-31": "9800000", "2026-06-14": "9800000", "2026-06-15": "11800000", "2026-09-30": "11800000"} {
		if c := e.call(http.MethodGet, "/v1/cap-table?at="+at, accountant, nil); c.Status != http.StatusOK || c.str("issued") != issued {
			t.Errorf("the cap table at %s: %d %s", at, c.Status, c.Raw)
		}
	}
	if x := tb["2040"]; x != nil && !x.IsZero() {
		t.Errorf("share application monies hold %s", x.Text('f'))
	}
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{})
		if err != nil {
			return err
		}
		transfers, waitingLines := 0, 0
		for _, l := range lines {
			switch {
			case strings.HasSuffix(l.Reference, "-to") || strings.HasSuffix(l.Reference, "-from"):
				transfers++
			case l.Status == storage.LineUnreconciled:
				waitingLines++
			}
		}
		if len(lines) != len(w.Transactions)+transfers || transfers != 23 || waitingLines != 0 {
			t.Errorf("the lines: %d for %d rows, %d transfers' other sides, %d waiting", len(lines), len(w.Transactions), transfers, waitingLines)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = id
}

// Imported pay runs keep the amounts paid (FR-MIG-02; design D7, rev 0.254): each month of the Payroll sheet is one
// posted summary journal on its last day, its credit to the CPF payable what was due to the CPF Board, employee and
// employer CPF, SDL and SHG, and its salary lines in their employees' departments; the per-employee rows stay in the
// archived workbook. With the history's Transactions, which paid them, the payroll accounts end each month as the
// workbook's do (TestTheTransactionsBringEveryAccountToTheWorkbooksBalance).
// req: FR-MIG-02
// req: FR-MIG-01
func TestImportedPayRunsKeepTheAmountsPaid(t *testing.T) {
	f, _ := importedSample(t)
	e := f.e
	accountant := f.as(f.ahkow, authz.Accountant)
	w, err := migration.Read(sampleWorkbook(t))
	if err != nil {
		t.Fatal(err)
	}
	due := map[string]*apd.Decimal{} // the month's amount due to the CPF Board, as a credit
	for _, p := range w.Payroll {
		k := p.MonthEnd.Format(time.DateOnly)
		if due[k] == nil {
			due[k] = new(apd.Decimal)
		}
		for _, x := range []*apd.Decimal{p.EmployeeCPF, p.EmployerCPF, p.SDL, p.SHG} {
			if x != nil {
				if _, err := apd.BaseContext.WithPrecision(40).Sub(due[k], due[k], x); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	accounts := byNumber(e.call(http.MethodGet, "/v1/accounts?page_size=500", accountant, nil), "code")
	cpf, salaries := accounts["2110"]["id"], map[any]bool{accounts["6011"]["id"]: true, accounts["6012"]["id"]: true, accounts["6013"]["id"]: true}
	runs := 0
	for _, x := range e.call(http.MethodGet, "/v1/journals?page_size=200", accountant, nil).Body["items"].([]any) {
		j := x.(map[string]any)
		if !strings.HasPrefix(fmt.Sprint(j["memo"]), "Payroll ") {
			continue
		}
		runs++
		if j["status"] != "posted" {
			t.Errorf("%v is %v", j["memo"], j["status"])
		}
		want, ok := due[fmt.Sprint(j["date"])]
		if !ok {
			t.Errorf("%v is dated %v, no pay month's last day", j["memo"], j["date"])
			continue
		}
		credit := new(apd.Decimal)
		j = e.call(http.MethodGet, "/v1/journals/"+fmt.Sprint(j["id"]), accountant, nil).Body
		for _, y := range j["lines"].([]any) {
			l := y.(map[string]any)
			if salaries[l["account_id"]] && len(l["dimension_value_ids"].([]any)) != 1 {
				t.Errorf("%v: a salary line in no department: %v", j["memo"], l)
			}
			if l["account_id"] == cpf {
				a := l["amount"]
				if m, ok := a.(map[string]any); ok {
					a = m["amount"]
				}
				d, _, err := apd.NewFromString(fmt.Sprint(a))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := apd.BaseContext.WithPrecision(40).Add(credit, credit, d); err != nil {
					t.Fatal(err)
				}
			}
		}
		if credit.Cmp(want) != 0 {
			t.Errorf("%v: %s to the CPF payable, the workbook's due to the CPF Board %s", j["memo"], credit.Text('f'), want.Text('f'))
		}
	}
	if runs != len(due) || runs != 9 {
		t.Errorf("%d pay runs for the workbook's %d months", runs, len(due))
	}
}

// A clean start carries the balances forward (§17.1 M3, design D5; rev 0.261): the books start the day after the
// workbook's "as of", 1 October 2026; every account opens at the Carry Forward sheet's balance, the receivables and
// payables held by the open invoices, bills and the credit note as opening items, so 3900 nets to zero; the assets open
// with the depreciation the workbook worked to 30 September; none of the history is imported.
// req: data/migration History or opening balances import (M3)
// req: FR-MIG-01
// req: FR-GL-06
func TestACleanStartCarriesTheBalancesForward(t *testing.T) {
	f, _ := committedAs(t, "clean_start")
	e := f.e
	accountant := f.as(f.ahkow, authz.Accountant)
	w, err := migration.Read(sampleWorkbook(t))
	if err != nil {
		t.Fatal(err)
	}
	if p := e.call(http.MethodGet, "/v1/organisation/profile", accountant, nil); p.str("books_start") != "2026-10-01" || p.str("first_year_end") != "2026-12-31" {
		t.Fatalf("the profile: %s", p.Raw)
	}
	// Its budget is the first year's three months of the Budget sheet's 2026, its recurring rows those of the history
	// (rev 0.263): income 34,900 of subscriptions, 60,000 of services and 900 of interest; costs the eleven lines' months.
	if b := e.call(http.MethodGet, "/v1/budgets", accountant, nil); len(b.Body["items"].([]any)) != 1 {
		t.Errorf("the budgets: %s", b.Raw)
	} else if x := b.Body["items"].([]any)[0].(map[string]any); x["budget"].(map[string]any)["financial_year"] != "2026-10-01" || len(x["months"].([]any)) != 3 ||
		x["totals"].(map[string]any)["income_year"] != "95800.00" || x["totals"].(map[string]any)["costs_year"] != "145560.00" {
		t.Errorf("the clean start's budget: %v", x)
	}
	// Its forecast reads the books from the last closed month, as they begin at the cut-over (rev 0.264).
	if fc := e.call(http.MethodGet, "/v1/forecasts", accountant, nil); len(fc.Body["items"].([]any)) != 1 ||
		fc.Body["items"].([]any)[0].(map[string]any)["plan"].(map[string]any)["last_actual"] != nil {
		t.Errorf("the clean start's forecast: %s", fc.Raw)
	}
	for _, path := range []string{"/v1/recurring-invoices", "/v1/recurring-bills", "/v1/recurring-journals"} {
		if r := e.call(http.MethodGet, path, accountant, nil); len(r.Body["items"].([]any)) != 1 {
			t.Errorf("%s: %s", path, r.Raw)
		}
	}
	// The register of members holds every share of the cap table from before the books start (rev 0.262).
	if c := e.call(http.MethodGet, "/v1/cap-table?at=2026-09-30", accountant, nil); c.Status != http.StatusOK || c.str("issued") != "11800000" {
		t.Errorf("the cap table: %d %s", c.Status, c.Raw)
	}
	tb := trialBalanceFrom(t, e, accountant, "2026-10", "2026-10")
	types := map[string]string{}
	for _, a := range w.Accounts {
		types[a.Code] = a.Type
	}
	credit := map[string]bool{"Liability": true, "Equity": true, "Revenue": true, "Other Income": true}
	for _, b := range w.CarryForward.Balances {
		want := new(apd.Decimal)
		if b.Balance != nil {
			want.Set(b.Balance)
		}
		if credit[types[b.Code]] {
			want.Neg(want)
		}
		got := tb[b.Code]
		if got == nil {
			got = new(apd.Decimal)
		}
		if got.Cmp(want) != 0 {
			t.Errorf("account %s opens at %s; Carry Forward carries %s", b.Code, got.Text('f'), want.Text('f'))
		}
	}
	if x := tb["3900"]; x != nil && !x.IsZero() {
		t.Errorf("3900 holds %s", x.Text('f'))
	}
	invoices := byNumber(e.call(http.MethodGet, "/v1/invoices", accountant, nil), "number")
	credits := byNumber(e.call(http.MethodGet, "/v1/invoices?kind=credit_note", accountant, nil), "number")
	if invoices["INV-0109"]["opening_item"] != true || credits["CN-0001"]["opening_item"] != true || len(invoices) != 5 {
		t.Errorf("the open items: %v %v", invoices, credits)
	}
	if j := e.call(http.MethodGet, "/v1/journals", accountant, nil); len(j.Body["items"].([]any)) != 0 {
		t.Errorf("a clean start imported journals: %s", j.Raw)
	}
	assets := byNumber(e.call(http.MethodGet, "/v1/fixed-assets", accountant, nil), "name")
	if a := assets["Laptops x3 (engineering)"]; a == nil || fmt.Sprint(a["opening_accumulated"].(map[string]any)["amount"]) != "1950.00" {
		t.Errorf("the laptops carried: %v", a)
	}
}

// The Recurring rows and the Budget carry on in Fyniq (design D5, task 4.6; rev 0.263): the retainer invoice, the
// bookkeeper's quarterly bill and the audit fee accrual each recur from their first occurrence after the workbook's
// "as of", the bill with the GST its code charges; the 2 bank transactions stay in the archive. The Budget sheet's 2026
// is the year's original budget, recorded, each account's months as the sheet's P&L lines that map onto it.
// req: FR-MIG-01
// req: FR-AR-07
// req: FR-GL-02
// req: FR-PLN-01
func TestTheRecurringRowsAndTheBudgetCarryOn(t *testing.T) {
	f, id := importedSample(t)
	e := f.e
	accountant := f.as(f.ahkow, authz.Accountant)
	w, err := migration.Read(sampleWorkbook(t))
	if err != nil {
		t.Fatal(err)
	}
	only := func(path string) map[string]any {
		r := e.call(http.MethodGet, path, accountant, nil)
		items, _ := r.Body["items"].([]any)
		if r.Status != http.StatusOK || len(items) != 1 {
			t.Fatalf("%s: %d %s", path, r.Status, r.Raw)
		}
		return items[0].(map[string]any)
	}
	amountOf := func(x any) string { return fmt.Sprint(x.(map[string]any)["amount"]) }
	inv := only("/v1/recurring-invoices")
	if inv["next_on"] != "2026-10-31" || inv["status"] != "active" || amountOf(inv["net"]) != "12000.00" ||
		inv["schedule"].(map[string]any)["every_months"] != float64(1) || inv["name"] != "Harbourline Logistics Pte Ltd – AI route-optimisation retainer" {
		t.Errorf("the recurring invoice: %v", inv)
	}
	bill := only("/v1/recurring-bills")
	line := bill["lines"].([]any)[0].(map[string]any)
	if bill["next_on"] != "2026-12-31" || bill["schedule"].(map[string]any)["every_months"] != float64(3) || amountOf(line["net"]) != "1500.00" ||
		amountOf(line["gst"]) != "135.00" || line["gst_code"] != "TX" {
		t.Errorf("the recurring bill: %v", bill)
	}
	jnl := only("/v1/recurring-journals")
	if jnl["next_on"] != "2026-10-31" || jnl["reverses"] != false || amountOf(jnl["debits"].([]any)[0]) != "500.00" || jnl["memo"] != "Accrue audit fee" {
		t.Errorf("the recurring journal: %v", jnl)
	}

	b := only("/v1/budgets")
	body := b["budget"].(map[string]any)
	totals := b["totals"].(map[string]any)
	if b["status"] != "recorded" || body["kind"] != "original" || body["financial_year"] != "2026-01-01" || len(b["months"].([]any)) != 12 ||
		totals["income_year"] != "367650.00" || totals["costs_year"] != "513500.00" {
		t.Errorf("the budget: %v", b)
	}
	// Each line's months are the sheet's, by the account its P&L lines map onto.
	codes := map[string]string{}
	accounts := e.call(http.MethodGet, "/v1/accounts?limit=500", accountant, nil)
	for _, a := range accounts.Body["items"].([]any) {
		codes[a.(map[string]any)["id"].(string)] = a.(map[string]any)["code"].(string)
	}
	mapping, _ := e.migrationMapping(id)
	lineTo := map[string]string{}
	for _, x := range mapping.Body["budget_lines"].([]any) {
		lineTo[x.(map[string]any)["line"].(string)] = x.(map[string]any)["account"].(string)
	}
	want := map[string][12]*apd.Decimal{}
	for _, l := range w.Budget {
		code := lineTo[l.Line]
		months := want[code]
		for i, x := range l.Months {
			if x == nil {
				continue
			}
			if months[i] == nil {
				months[i] = new(apd.Decimal)
			}
			if _, err := apd.BaseContext.WithPrecision(40).Add(months[i], months[i], x); err != nil {
				t.Fatal(err)
			}
		}
		want[code] = months
	}
	lines := body["lines"].([]any)
	for _, l := range lines {
		l := l.(map[string]any)
		code := codes[l["account_id"].(string)]
		for i, a := range l["amounts"].([]any) {
			x := want[code][i]
			if x == nil {
				x = new(apd.Decimal)
			}
			if got, _, _ := apd.NewFromString(a.(string)); got.Cmp(x) != 0 {
				t.Errorf("account %s month %d: %s, the sheet's %s", code, i+1, a, x.Text('f'))
			}
		}
		delete(want, code)
	}
	for code, months := range want {
		for _, x := range months {
			if x != nil && !x.IsZero() {
				t.Errorf("account %s's budget is not imported", code)
				break
			}
		}
	}
}

// The Forecast sheet is the company's forecast (design D5, task 4.6; rev 0.264): recorded, with the sheet's drivers,
// its 11 people and hires, 3 purchases, 3 funding events and its Base, Best and Worst levers, from the workbook's last
// month of actuals. Its books' figures at August's end are the ones the sheet shows: Fyniq's imported books give the
// forecast the workbook's cash, receivables and payables (FR-MIG-02).
// req: FR-MIG-01
// req: FR-MIG-02
// req: FR-PLN-02
func TestTheForecastStartsFromTheWorkbooksBooks(t *testing.T) {
	f, _ := importedSample(t)
	e := f.e
	accountant := f.as(f.ahkow, authz.Accountant)
	w, err := migration.Read(sampleWorkbook(t))
	if err != nil {
		t.Fatal(err)
	}
	l := e.call(http.MethodGet, "/v1/forecasts", accountant, nil)
	items, _ := l.Body["items"].([]any)
	if l.Status != http.StatusOK || len(items) != 1 {
		t.Fatalf("the forecasts: %d %s", l.Status, l.Raw)
	}
	fc := items[0].(map[string]any)
	plan := fc["plan"].(map[string]any)
	scenarios := plan["scenarios"].([]any)
	if fc["status"] != "recorded" || plan["horizon"] != float64(36) || plan["last_actual"] != "2026-08" || plan["buffer_months"] != float64(6) ||
		len(plan["headcount"].([]any)) != 11 || len(plan["capex"].([]any)) != 3 || len(plan["funding"].([]any)) != 3 || len(plan["opex"].([]any)) != 7 ||
		len(scenarios) != 3 || scenarios[2].(map[string]any)["levers"].(map[string]any)["churn"] != "1.3" ||
		plan["revenue"].(map[string]any)["arpa"] != "120.00" || plan["tax"].(map[string]any)["start_up_years"] != float64(3) {
		t.Errorf("the forecast: %v", fc)
	}
	r := e.call(http.MethodGet, "/v1/forecasts/"+fc["id"].(string)+"/results", accountant, nil)
	books, _ := r.Body["books"].(map[string]any)
	cents := func(d *apd.Decimal) string {
		x := new(apd.Decimal)
		if _, err := apd.BaseContext.WithPrecision(40).Quantize(x, d, -2); err != nil {
			t.Fatal(err)
		}
		return x.Text('f')
	}
	if r.Status != http.StatusOK || books["start"] != "2026-09" || books["cash"] != cents(w.Forecast.Cash) ||
		books["receivables"] != cents(w.Forecast.Receivables) || books["payables"] != cents(w.Forecast.Payables) {
		t.Errorf("the forecast's books: %d, the sheet's cash %s, receivables %s, payables %s: %v", r.Status, cents(w.Forecast.Cash),
			cents(w.Forecast.Receivables), cents(w.Forecast.Payables), books)
	}
}
