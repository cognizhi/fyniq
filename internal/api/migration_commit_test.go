//go:build local

package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

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
	resumed := e.run(id, e.as(authz.Controller))
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

	// Each record once: 16 contacts and the 2 founders, 5 items, 28 bank rules (the 29th, to a money account, is a
	// transfer, kept in the archive).
	if l := e.call(http.MethodGet, "/v1/contacts?limit=100", accountant, nil); strings.Count(l.Raw, `"legal_name":"Harbourline Logistics Pte Ltd"`) != 1 ||
		strings.Count(l.Raw, `"legal_name":"Alex Tan"`) != 1 || len(l.Body["items"].([]any)) != 18 {
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
}
