//go:build local

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/migration"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
	oauthrevoke "github.com/cognizhi/fyniq/internal/tokens"
)

// sampleWorkbook is the workbook's pinned sample (p1-workbook-migration design D12).
func sampleWorkbook(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../test/migration/workbook-sample.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// validate runs the worker's validations of uploaded workbooks. The jobs a test's new member queues, a revocation of
// their tokens and the notices about them, are done with nothing to do here.
func (e *env) validate() {
	e.t.Helper()
	none := func(context.Context, uuid.UUID, storage.OutboxRow) error { return nil }
	e.work(map[string]outbox.Work{migration.ValidateKind: migration.Validator{Store: workerStore(e.t), Files: e.blobs, Clock: e.clock}.Work(),
		oauthrevoke.Kind: none, notify.FanoutKind: none, notify.RelayKind: none})
}

// migrationTag is a migration's ETag as the Auditor reads it.
func (e *env) migrationTag(id string) string {
	e.t.Helper()
	return e.call(http.MethodGet, "/v1/migrations/"+id, e.as(authz.Auditor), nil).Header.Get("ETag")
}

// startedMigration is e's migration, started by its Controller.
func (e *env) startedMigration() string {
	e.t.Helper()
	r := e.call(http.MethodPost, "/v1/migrations", e.as(authz.Controller), nil)
	if r.Status != http.StatusCreated && r.Status != http.StatusOK {
		e.t.Fatalf("starting the migration: %d %s", r.Status, r.Raw)
	}
	return r.str("id")
}

// A workbook goes only into an organisation without posted entries (rev 0.254): the Controller starts the migration
// once, and starting it again answers the same one; an organisation with a posted entry is refused; the Owner and the
// Auditor read it and do not start it; a Bookkeeper and the AI see none of it; another organisation does not find it.
// req: FR-MIG-01
// req: access/roles-and-segregation Permission matrix: Workbook import (migration)
// asvs: V8.2.1, V8.2.2
func TestAWorkbookImportGoesOnlyIntoAnOrganisationWithNoPostings(t *testing.T) {
	e := newEnv(t)
	ctrl := e.as(authz.Controller)
	if pv := e.call(http.MethodPost, "/v1/migrations", ctrl, nil, "Prefer", "return=preview"); pv.Status != 200 || pv.Header.Get("Location") != "" {
		t.Fatalf("a preview: %d %v %s", pv.Status, pv.Header, pv.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/migrations", ctrl, nil); l.Status != 200 || len(l.Body["items"].([]any)) != 0 {
		t.Fatalf("the preview saved a migration: %d %s", l.Status, l.Raw)
	}
	r := e.call(http.MethodPost, "/v1/migrations", ctrl, nil, "Idempotency-Key", "m-1")
	if r.Status != 201 || r.str("status") != "open" || r.Header.Get("Location") != "/v1/migrations/"+r.str("id") || !strings.HasPrefix(r.str("started_by"), "user:") ||
		r.Header.Get("ETag") != e.migrationTag(r.str("id")) {
		t.Fatalf("starting: %d %v %s", r.Status, r.Header, r.Raw)
	}
	again := e.call(http.MethodPost, "/v1/migrations", e.as(authz.Accountant), nil)
	if again.Status != 200 || again.str("id") != r.str("id") {
		t.Fatalf("starting again: %d %s", again.Status, again.Raw)
	}
	for _, role := range []authz.Role{authz.Owner, authz.Auditor} {
		if g := e.call(http.MethodGet, "/v1/migrations/"+r.str("id"), e.as(role), nil); g.Status != 200 || g.str("id") != r.str("id") {
			t.Errorf("%s reading it: %d %s", role, g.Status, g.Raw)
		}
		e.problemIs(e.call(http.MethodPost, "/v1/migrations", e.as(role), nil), "startMigration", 403, CodeForbidden)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/migrations", e.as(authz.Bookkeeper), nil), "listMigrations", 403, CodeForbidden)
	if !strings.Contains(auditTrail(t, e), `"action":"migration.started"`) {
		t.Error("no audit event migration.started")
	}

	live := newEnv(t)
	live.postedJournal("100.00")
	live.problemIs(live.call(http.MethodPost, "/v1/migrations", live.as(authz.Controller), nil), "startMigration", 409, migration.CodePosted)
	live.problemIs(live.call(http.MethodGet, "/v1/migrations/"+r.str("id"), live.as(authz.Controller), nil), "getMigration", 404, CodeNotFound)
	if l := live.call(http.MethodGet, "/v1/migrations", live.as(authz.Controller), nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("another organisation lists the migration: %s", l.Raw)
	}
}

// The validation report commits nothing (FR-MIG-01, design D2): the frozen workbook is uploaded as M1's evidence,
// scanned, hashed and kept as payroll data; the worker validates it inside a transaction that rolls back and keeps its
// report and the Checks export made from it; the books are as they were. A file that is not a workbook is refused; a
// second upload waits for the first's validation; a workbook without the sheets is invalid, each missing sheet named.
// req: FR-MIG-01
// req: data/migration Workbook freeze (M1)
// asvs: V5.2.2, V8.2.2
func TestTheValidationReportCommitsNothing(t *testing.T) {
	e := newEnv(t)
	ctrl := e.as(authz.Controller)
	id := e.startedMigration()
	books := func() string {
		var b strings.Builder
		for _, path := range []string{"/v1/accounts", "/v1/contacts", "/v1/items", "/v1/journals", "/v1/dimensions"} {
			r := e.call(http.MethodGet, path, ctrl, nil)
			b.WriteString(path + " " + r.Raw + "\n")
		}
		return b.String()
	}
	before := books()

	path := "/v1/migrations/" + id + "/workbook"
	tag := e.migrationTag(id)
	e.problemIs(e.upload(path, ctrl, nil, "notes.pdf", []byte("%PDF-1.4\n%…\n"), "Idempotency-Key", "w-0", "If-Match", tag), "uploadMigrationWorkbook", 422, CodeInvalid)
	data := sampleWorkbook(t)
	sum := sha256.Sum256(data)
	e.problemIs(e.upload(path, ctrl, nil, "Hub.xlsx", data, "Idempotency-Key", "w-s"), "uploadMigrationWorkbook", 428, CodeIfMatchRequired)
	e.problemIs(e.upload(path, ctrl, nil, "Hub.xlsx", data, "Idempotency-Key", "w-t", "If-Match", `"9-open"`), "uploadMigrationWorkbook", 412, CodeStale)
	if pv := e.upload(path, ctrl, nil, "Hub.xlsx", data, "Prefer", "return=preview", "If-Match", tag); pv.Status != 200 || pv.str("operation_id") != "" {
		t.Fatalf("a preview of the upload: %d %s", pv.Status, pv.Raw)
	}
	up := e.upload(path, ctrl, nil, "Cognizhi-Finance-Hub.xlsx", data, "Idempotency-Key", "w-1", "If-Match", tag)
	file, _ := up.Body["file"].(map[string]any)
	step, _ := up.Body["step"].(map[string]any)
	if up.Status != 202 || up.Header.Get("Location") != "/v1/operations/"+up.str("operation_id") || step["state"] != "validating" || step["step"] != "M1" ||
		file["sha256"] != hex.EncodeToString(sum[:]) || file["data_class"] != "payroll" || file["kind"] != "workbook" || file["file_name"] != "Cognizhi-Finance-Hub.xlsx" {
		t.Fatalf("uploading: %d %v %s", up.Status, up.Header, up.Raw)
	}
	if now := e.migrationTag(id); now == tag {
		t.Errorf("the upload left the ETag at %s", now)
	}
	e.problemIs(e.upload(path, ctrl, nil, "Again.xlsx", data, "Idempotency-Key", "w-2", "If-Match", e.migrationTag(id)), "uploadMigrationWorkbook", 409,
		"STORAGE-DUPLICATE-01")
	if r := e.call(http.MethodGet, "/v1/migrations/"+id+"/report", ctrl, nil); r.Status != 200 || r.str("state") != "validating" || r.Body["sheets"] != nil {
		t.Fatalf("the report while validating: %d %s", r.Status, r.Raw)
	}

	e.validate()
	if op := e.call(http.MethodGet, up.Header.Get("Location"), ctrl, nil); op.str("status") != "succeeded" || op.str("kind") != "migration_validation" ||
		op.str("resource") != "/v1/migrations/"+id+"/report" {
		t.Fatalf("the operation: %s", op.Raw)
	}
	rep := e.call(http.MethodGet, "/v1/migrations/"+id+"/report", e.as(authz.Auditor), nil)
	sheets, _ := rep.Body["sheets"].([]any)
	problems, _ := rep.Body["problems"].([]any)
	// Eight warnings: the Timesheets, the bank rule to a money account, the option pool and the 2 recurring bank
	// transactions kept in the archive, the sample's placeholder UEN, which the import leaves out, and the two US
	// suppliers' natures of payment, which the mapping asks for.
	var warned []string
	for _, p := range problems {
		warned = append(warned, fmt.Sprint(p.(map[string]any)["code"], " ", p.(map[string]any)["sheet"]))
	}
	slices.Sort(warned)
	if rep.Status != 200 || rep.str("state") != "valid" || rep.Body["errors"] != float64(0) || rep.Body["warnings"] != float64(8) ||
		rep.str("format") != migration.ReportFormat || rep.str("base_currency") != "SGD" || rep.str("books_start") != "2026-01-01" ||
		rep.str("as_of") != "2026-09-30" || rep.str("sha256") != hex.EncodeToString(sum[:]) || len(sheets) != 23 ||
		strings.Join(warned, ", ") != migration.CodeValue+" Settings, "+migration.CodeArchived+" Funding, "+migration.CodeArchived+" Lists, "+
			migration.CodeArchived+" Recurring, "+migration.CodeArchived+" Recurring, "+migration.CodeArchived+" Timesheets, "+
			migration.CodeMapping+" Contacts, "+migration.CodeMapping+" Contacts" {
		t.Fatalf("the report: %d %s", rep.Status, rep.Raw)
	}
	for _, s := range sheets {
		if c := s.(map[string]any); c["sheet"] == "Transactions" && c["rows"] != float64(178) {
			t.Errorf("Transactions: %v", c)
		}
	}
	if after := books(); after != before {
		t.Errorf("validating changed the books:\nbefore %s\nafter %s", before, after)
	}

	m := e.call(http.MethodGet, "/v1/migrations/"+id, ctrl, nil)
	steps, _ := m.Body["steps"].([]any)
	files, _ := m.Body["files"].([]any)
	if len(steps) != 1 || len(files) != 2 {
		t.Fatalf("the migration: %s", m.Raw)
	}
	s0, checks := steps[0].(map[string]any), files[1].(map[string]any)
	if s0["state"] != "valid" || s0["report_sha256"] != rep.str("report_sha256") || s0["checks_file_id"] != checks["id"] || checks["kind"] != "checks" ||
		checks["data_class"] != "general" || checks["media_type"] != "text/csv" || checks["added_by"] != "system:migration.validate" {
		t.Fatalf("the validated step and its Checks export: %s", m.Raw)
	}
	keys, err := e.blobs.List(context.Background(), "migrations/"+e.books.Org.String()+"/"+id+"/")
	if err != nil || len(keys) != 2 {
		t.Fatalf("the files kept: %v %v", keys, err)
	}
	found := false
	for _, k := range keys {
		rc, err := e.blobs.Get(context.Background(), k)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) == checks["sha256"] {
			found = true
			lines := strings.Split(strings.TrimSpace(string(b)), "\r\n")
			if len(lines) != 38 || lines[0] != `"#","Area","Check","Result","Status"` || !strings.HasPrefix(lines[1], `1,"Books","Debits equal credits (trial balance)"`) ||
				!strings.HasSuffix(lines[1], `"✔"`) {
				t.Errorf("the Checks export: %q", lines[:2])
			}
		}
	}
	if !found {
		t.Error("the Checks export is not kept")
	}
	trail := auditTrail(t, e)
	for _, action := range []string{"migration.workbook_received", "migration.validated"} {
		if !strings.Contains(trail, `"action":"`+action+`"`) {
			t.Errorf("no audit event %s", action)
		}
	}

	// A workbook without the sheets the import reads: invalid, each missing sheet named, and nothing committed.
	other, err := sheet.XLSX([]sheet.Sheet{{Name: "Notes", Header: []string{"Note"}, Rows: []sheet.Row{{Cells: []sheet.Cell{sheet.Text("x")}}}}},
		time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if r := e.upload(path, ctrl, nil, "Notes.xlsx", other, "Idempotency-Key", "w-3", "If-Match", e.migrationTag(id)); r.Status != 202 {
		t.Fatalf("uploading another workbook: %d %s", r.Status, r.Raw)
	}
	e.validate()
	bad := e.call(http.MethodGet, "/v1/migrations/"+id+"/report", ctrl, nil)
	missing := 0
	for _, p := range bad.Body["problems"].([]any) {
		if p := p.(map[string]any); p["code"] == migration.CodeSheet && p["severity"] == "error" && p["fix"] != "" {
			missing++
		}
	}
	if bad.str("state") != "invalid" || missing != 23 || bad.str("file_name") != "Notes.xlsx" {
		t.Fatalf("a workbook without its sheets: %d missing; %s", missing, bad.Raw)
	}
	if after := books(); after != before {
		t.Error("an invalid workbook changed the books")
	}
}

// migrationMapping is the migration's mapping and its ETag, as the Auditor reads it.
func (e *env) migrationMapping(id string) (response, string) {
	e.t.Helper()
	r := e.call(http.MethodGet, "/v1/migrations/"+id+"/mapping", e.as(authz.Auditor), nil)
	return r, r.Header.Get("ETag")
}

// The mapping (§17.1 M2, design D4) is proposed by the valid workbook, changed by the Controller or Accountant as its
// next version, and signed by the Accountant alone, stepped up, over its hash: the Controller and the Owner cannot sign
// it; a version with an error, or of a workbook that is not the latest valid upload, cannot be signed; a version is
// signed once, and a change after the sign-off needs a sign-off of its own.
// req: data/migration Set-up import and mapping review (M2)
// req: FR-MIG-01
// req: access/roles-and-segregation Permission matrix: Workbook import sign-offs
// asvs: V8.2.1, V6.4.2
func TestTheMappingIsSignedByTheAccountantAlone(t *testing.T) {
	e := newEnv(t)
	ctrl, acct := e.as(authz.Controller), e.as(authz.Accountant)
	id := e.startedMigration()
	mapping := "/v1/migrations/" + id + "/mapping"
	e.problemIs(e.call(http.MethodGet, mapping, ctrl, nil), "getMigrationMapping", 404, CodeNotFound) // no workbook yet
	if r := e.upload("/v1/migrations/"+id+"/workbook", ctrl, nil, "Hub.xlsx", sampleWorkbook(t), "If-Match", e.migrationTag(id)); r.Status != 202 {
		t.Fatalf("uploading: %d %s", r.Status, r.Raw)
	}
	e.validate()
	m, tag := e.migrationMapping(id)
	accounts, _ := m.Body["accounts"].([]any)
	// Its two errors are the US suppliers' natures of payment, which the Accountant chooses (FR-AP-19, rev 0.259).
	if m.Status != 200 || m.Body["version"] != float64(1) || m.Body["signed"] != false || m.Body["errors"] != float64(2) || len(accounts) != 83 ||
		m.str("made_by") != "system:migration.validate" || tag != `"1-open"` {
		t.Fatalf("the proposed mapping: %d %s %s", m.Status, tag, m.Raw)
	}

	change := map[string]any{"accounts": []map[string]any{{"workbook": "4030", "fyniq": "4031", "action": "create", "type": "revenue",
		"report_group": "Services & project revenue"}}, "natures": natures(m)}
	e.problemIs(e.call(http.MethodPatch, mapping, ctrl, change, "Idempotency-Key", "c-0"), "changeMigrationMapping", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPatch, mapping, ctrl, map[string]any{"projects": []map[string]any{{"workbook": "Nope", "code": "N", "action": "create"}}},
		"Idempotency-Key", "c-1", "If-Match", tag), "changeMigrationMapping", 422, CodeInvalid)
	c := e.call(http.MethodPatch, mapping, ctrl, change, "Idempotency-Key", "c-2", "If-Match", tag)
	if c.Status != 200 || c.Body["version"] != float64(2) || c.Header.Get("ETag") != `"2-open"` || !strings.HasPrefix(c.str("made_by"), "user:") {
		t.Fatalf("changing: %d %v %s", c.Status, c.Header, c.Raw)
	}
	_, tag = e.migrationMapping(id)
	sign := mapping + "/sign-off"
	for _, role := range []authz.Role{authz.Controller, authz.Owner} {
		e.problemIs(e.call(http.MethodPost, sign, steppedUp(e, e.as(role)), nil, "Idempotency-Key", "s-"+string(role), "If-Match", tag),
			"signMigrationMapping", 403, CodeForbidden)
	}
	e.problemIs(e.call(http.MethodPost, sign, acct, nil, "Idempotency-Key", "s-0", "If-Match", tag), "signMigrationMapping", 403, CodeStepUpRequired)
	me := steppedUp(e, acct)
	e.problemIs(e.call(http.MethodPost, sign, me, nil, "Idempotency-Key", "s-1", "If-Match", `"1-open"`), "signMigrationMapping", 412, CodeStale)
	s := e.call(http.MethodPost, sign, me, nil, "Idempotency-Key", "s-2", "If-Match", tag)
	if s.Status != 200 || s.Body["signed"] != true || s.Header.Get("ETag") != `"2-signed"` || !strings.HasPrefix(s.str("signed_by"), "user:") {
		t.Fatalf("signing: %d %v %s", s.Status, s.Header, s.Raw)
	}
	e.problemIs(e.call(http.MethodPost, sign, me, nil, "Idempotency-Key", "s-3", "If-Match", `"2-signed"`), "signMigrationMapping", 409, "STORAGE-DUPLICATE-01")
	if !strings.Contains(auditTrail(t, e), `"action":"migration.mapping_signed"`) || !strings.Contains(auditTrail(t, e), `"action":"migration.mapped"`) {
		t.Error("no audit events migration.mapped and migration.mapping_signed")
	}

	// A change after the sign-off is a version of its own, unsigned; one the chart cannot take cannot be signed.
	bad := map[string]any{"budget_lines": []map[string]any{{"line": "Subscription revenue", "account": "1010"}}}
	b := e.call(http.MethodPatch, mapping, acct, bad, "Idempotency-Key", "c-3", "If-Match", `"2-signed"`)
	problems, _ := b.Body["problems"].([]any)
	if b.Status != 200 || b.Body["version"] != float64(3) || b.Body["signed"] != false || b.Body["errors"] != float64(1) || len(problems) != 1 ||
		problems[0].(map[string]any)["code"] != migration.CodeMapping {
		t.Fatalf("a change the chart cannot take: %d %s", b.Status, b.Raw)
	}
	e.problemIs(e.call(http.MethodPost, sign, me, nil, "Idempotency-Key", "s-4", "If-Match", `"3-open"`), "signMigrationMapping", 422, migration.CodeMapping)

	// A newer workbook that is not valid leaves the mapping of the last valid one, which can no longer be signed.
	other, err := sheet.XLSX([]sheet.Sheet{{Name: "Notes", Header: []string{"Note"}}}, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if r := e.upload("/v1/migrations/"+id+"/workbook", ctrl, nil, "Notes.xlsx", other, "If-Match", e.migrationTag(id)); r.Status != 202 {
		t.Fatalf("uploading: %d %s", r.Status, r.Raw)
	}
	e.validate()
	fix := map[string]any{"budget_lines": []map[string]any{{"line": "Subscription revenue", "account": "4010"}}}
	if f := e.call(http.MethodPatch, mapping, acct, fix, "Idempotency-Key", "c-4", "If-Match", `"3-open"`); f.Status != 200 || f.Body["errors"] != float64(0) {
		t.Fatalf("fixing the mapping: %d %s", f.Status, f.Raw)
	}
	e.problemIs(e.call(http.MethodPost, sign, me, nil, "Idempotency-Key", "s-5", "If-Match", `"4-open"`), "signMigrationMapping", 409, migration.CodeNotSignable)
}

// matrixMigrations are the authorisation matrix's rows for the workbook migration (FR-MIG-01, Appendix D "Workbook
// import (migration)"). The matrix's organisation posts entries as it goes, so its migration is started, with the sample
// validated, before the matrix runs (seedMigration); starting it again answers that one.
var matrixMigrations = map[string]func(e *env, tok string) (response, func() bool){
	"listMigrations": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/migrations", tok, nil), nil
	},
	"startMigration": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/migrations", tok, nil, "Prefer", "return=preview"), nil
	},
	"getMigration": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/migrations/"+matrixMigration(e), tok, nil), nil
	},
	"uploadMigrationWorkbook": func(e *env, tok string) (response, func() bool) {
		id := matrixMigration(e)
		was := e.call(http.MethodGet, "/v1/migrations/"+id, e.as(authz.Auditor), nil).Raw
		r := e.upload("/v1/migrations/"+id+"/workbook", tok, nil, "Hub.xlsx", sampleWorkbook(e.t), "Prefer", "return=preview", "If-Match", e.migrationTag(id))
		return r, func() bool { return e.call(http.MethodGet, "/v1/migrations/"+id, e.as(authz.Auditor), nil).Raw == was }
	},
	"getMigrationReport": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/migrations/"+matrixMigration(e)+"/report", tok, nil), nil
	},
	"getMigrationMapping": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/migrations/"+matrixMigration(e)+"/mapping", tok, nil), nil
	},
	"changeMigrationMapping": func(e *env, tok string) (response, func() bool) {
		id := matrixMigration(e)
		was, tag := e.migrationMapping(id)
		r := e.call(http.MethodPatch, "/v1/migrations/"+id+"/mapping", tok, map[string]any{"budget_lines": []map[string]any{{"line": "Subscription revenue",
			"account": "4010"}}}, "Prefer", "return=preview", "If-Match", tag)
		return r, func() bool { now, _ := e.migrationMapping(id); return now.Raw == was.Raw }
	},
	"signMigrationMapping": func(e *env, tok string) (response, func() bool) {
		id := matrixMigration(e)
		signable(e, id)
		unsigned(e, id)
		was, tag := e.migrationMapping(id)
		r := e.call(http.MethodPost, "/v1/migrations/"+id+"/mapping/sign-off", steppedUp(e, tok), nil, "Prefer", "return=preview", "If-Match", tag)
		return r, func() bool { now, _ := e.migrationMapping(id); return now.Raw == was.Raw }
	},
	"requestMigrationCommit": func(e *env, tok string) (response, func() bool) {
		id := matrixMigration(e)
		signable(e, id)
		if m, tag := e.migrationMapping(id); m.Body["signed"] != true {
			if s := e.call(http.MethodPost, "/v1/migrations/"+id+"/mapping/sign-off", steppedUp(e, e.as(authz.Accountant)), nil, "If-Match", tag); s.Status != http.StatusOK {
				e.t.Fatalf("signing the matrix's mapping: %d %s", s.Status, s.Raw)
			}
		}
		_, tag := e.migrationMapping(id)
		r := e.call(http.MethodPost, "/v1/migrations/"+id+"/commit", steppedUp(e, tok), map[string]any{"path": "history"}, "Prefer", "return=preview", "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/migrations/"+id+"/commit", e.as(authz.Auditor), nil).str("status") == "none"
		}
	},
	"runMigrationCommit": func(e *env, tok string) (response, func() bool) {
		id := matrixMigration(e)
		return e.call(http.MethodPost, "/v1/migrations/"+id+"/commit:run", tok, nil, "Prefer", "return=preview"), nil
	},
	"getMigrationCommit": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/migrations/"+matrixMigration(e)+"/commit", tok, nil), nil
	},
	// The matrix's import is not committed: a role allowed to read gets the report with nothing in it.
	"getMigrationReconciliation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/migrations/"+matrixMigration(e)+"/reconciliation", tok, nil), nil
	},
}

// unsigned gives the matrix's mapping a version of its own when the latest is signed, as another row signed it: a
// version is signed once.
func unsigned(e *env, id string) {
	e.t.Helper()
	m, tag := e.migrationMapping(id)
	if m.Body["signed"] != true {
		return
	}
	lines, _ := m.Body["budget_lines"].([]any)
	l := lines[0].(map[string]any)
	if r := e.call(http.MethodPatch, "/v1/migrations/"+id+"/mapping", e.as(authz.Accountant), map[string]any{"budget_lines": []map[string]any{{"line": l["line"],
		"account": l["account"]}}}, "If-Match", tag); r.Status != 200 {
		e.t.Fatalf("a new version of the matrix's mapping: %d %s", r.Status, r.Raw)
	}
}

// natures choose the nature of payment of each non-resident supplier a mapping lists, and enter the tax agent's decision,
// as the Accountant does.
func natures(m response) []map[string]any {
	out := []map[string]any{}
	for _, x := range m.Body["natures"].([]any) {
		out = append(out, map[string]any{"contact": x.(map[string]any)["contact"], "nature": "services_outside_singapore", "outcome": "not_applicable",
			"tax_agent": "Tally & Co Accounting"})
	}
	return out
}

// signable makes the matrix's mapping signable when it lists errors: the matrix's other rows create accounts under the
// workbook's codes, of other types, so every account is created under a code of its own, W and the workbook's, and the
// budget lines and bank rules follow it.
func signable(e *env, id string) {
	e.t.Helper()
	m, tag := e.migrationMapping(id)
	if m.Body["errors"] == float64(0) {
		return
	}
	change := map[string][]map[string]any{"natures": natures(m)}
	for _, x := range m.Body["accounts"].([]any) {
		a := x.(map[string]any)
		a["fyniq"], a["action"] = "W"+a["workbook"].(string), "create"
		if a["type"] == "" || a["type"] == nil {
			a["type"] = "expense"
		}
		change["accounts"] = append(change["accounts"], a)
	}
	for _, x := range m.Body["budget_lines"].([]any) {
		l := x.(map[string]any)
		change["budget_lines"] = append(change["budget_lines"], map[string]any{"line": l["line"], "account": "W" + l["account"].(string)})
	}
	for _, x := range m.Body["bank_rules"].([]any) {
		r := x.(map[string]any)
		change["bank_rules"] = append(change["bank_rules"], map[string]any{"order": r["order"], "account": "W" + r["account"].(string), "gst": r["gst"]})
	}
	if r := e.call(http.MethodPatch, "/v1/migrations/"+id+"/mapping", e.as(authz.Accountant), change, "If-Match", tag); r.Status != 200 || r.Body["errors"] != float64(0) {
		e.t.Fatalf("making the matrix's mapping signable: %d %s", r.Status, r.Raw)
	}
}

// seedMigration starts e's migration and validates the sample in it, before anything is posted.
func seedMigration(e *env) {
	e.t.Helper()
	id := e.startedMigration()
	if r := e.upload("/v1/migrations/"+id+"/workbook", e.as(authz.Controller), nil, "Hub.xlsx", sampleWorkbook(e.t), "If-Match", e.migrationTag(id)); r.Status != http.StatusAccepted {
		e.t.Fatalf("seeding the migration: %d %s", r.Status, r.Raw)
	}
	e.validate()
}

// matrixMigration is e's migration, which seedMigration started.
func matrixMigration(e *env) string {
	r := e.call(http.MethodGet, "/v1/migrations", e.as(authz.Auditor), nil)
	if items, _ := r.Body["items"].([]any); len(items) == 1 {
		return items[0].(map[string]any)["id"].(string)
	}
	e.t.Fatalf("the matrix's organisation has no migration: %s", r.Raw)
	return ""
}
