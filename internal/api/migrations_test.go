//go:build local

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/migration"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/sheet"
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

// validate runs the worker's validations of uploaded workbooks.
func (e *env) validate() {
	e.t.Helper()
	e.work(map[string]outbox.Work{migration.ValidateKind: migration.Validator{Store: workerStore(e.t), Files: e.blobs, Clock: e.clock}.Work()})
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
	if rep.Status != 200 || rep.str("state") != "valid" || rep.Body["errors"] != float64(0) || rep.Body["warnings"] != float64(1) ||
		rep.str("format") != migration.ReportFormat || rep.str("base_currency") != "SGD" || rep.str("books_start") != "2026-01-01" ||
		rep.str("as_of") != "2026-09-30" || rep.str("sha256") != hex.EncodeToString(sum[:]) || len(sheets) != 22 || len(problems) != 1 ||
		problems[0].(map[string]any)["code"] != migration.CodeArchived {
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
	if bad.str("state") != "invalid" || missing != 22 || bad.str("file_name") != "Notes.xlsx" {
		t.Fatalf("a workbook without its sheets: %d missing; %s", missing, bad.Raw)
	}
	if after := books(); after != before {
		t.Error("an invalid workbook changed the books")
	}
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
