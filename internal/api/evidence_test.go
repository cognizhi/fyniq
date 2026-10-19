//go:build local

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func workerStore(t *testing.T) *postgres.Store {
	t.Helper()
	s, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func parseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// nextWork returns the kind of the next outbox row dispatched to the worker class, "" when there is none.
func nextWork(t *testing.T, e *env) string {
	t.Helper()
	task, err := e.queue.Next(context.Background(), "worker")
	if err != nil {
		return ""
	}
	var m outbox.Message
	if err := json.Unmarshal(task.Payload, &m); err != nil {
		t.Fatal(err)
	}
	var row storage.OutboxRow
	if err := e.store.Do(context.Background(), m.OrganisationID, func(ctx context.Context, u storage.UnitOfWork) error {
		row, err = u.Outbox().Get(ctx, m.OutboxID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return row.Kind
}

// uuidV7At is a UUIDv7 whose timestamp is at, as an upload made then would be named.
func uuidV7At(t *testing.T, at time.Time) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	ms := uint64(at.UnixMilli()) // #nosec G115 -- a time after 1970
	for i := range 6 {
		id[i] = byte(ms >> (8 * (5 - i)))
	}
	return id
}

// A file attached to a draft journal through the API: checked, staged, recorded with its hashes and
// uploader, listed, read with its ETag, removable while the journal is a draft; Idempotency-Key replays it,
// and a preview stores nothing.
// req: FR-DOC-11
// req: FR-DOC-05
// req: FR-DOC-13
// asvs: V5.2.1
func TestEvidenceOnADraftJournal(t *testing.T) {
	e := newEnv(t)
	id, tag := e.createDraft("120.00")
	path := "/v1/journals/" + id + "/evidence"
	acc := e.as(authz.Accountant)
	pdf := []byte("%PDF-1.7 the October rent receipt")
	fields := map[string]string{"evidence_type": "receipt", "note": "Paid by PayNow", "relates_on": "2026-10-20"}

	pre := e.upload(path, acc, fields, "rent.pdf", pdf, "If-Match", tag, "Prefer", "return=preview")
	if pre.Status != http.StatusOK || pre.Header.Get("Preference-Applied") != "return=preview" {
		t.Fatalf("preview = %d: %s", pre.Status, pre.Raw)
	}
	if keys, _ := e.blobs.List(context.Background(), ""); len(keys) != 0 {
		t.Fatalf("a preview stored %v", keys)
	}
	r := e.upload(path, acc, fields, "../../rent.pdf", pdf, "If-Match", tag, "Idempotency-Key", "ev-1")
	if r.Status != http.StatusCreated {
		t.Fatalf("addJournalEvidence = %d: %s", r.Status, r.Raw)
	}
	if r.str("file_name") != "rent.pdf" || r.str("media_type") != evidence.PDF || r.str("state") != "attached" || r.str("source") != "upload" ||
		r.str("data_class") != "general" || r.str("relates_on") != "2026-10-20" || len(r.str("sha256")) != 64 ||
		!strings.HasPrefix(r.str("uploaded_by"), "user:") {
		t.Errorf("the file: %s", r.Raw)
	}
	again := e.upload(path, acc, fields, "../../rent.pdf", pdf, "If-Match", tag, "Idempotency-Key", "ev-1")
	if again.Status != http.StatusCreated || again.str("id") != r.str("id") || again.Header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("replay = %d %s", again.Status, again.Raw)
	}
	if keys, _ := e.blobs.List(context.Background(), "staging/"); len(keys) != 1 {
		t.Errorf("staged files: %v; want the one attached (a replay keeps no copy)", keys)
	}
	l := e.call(http.MethodGet, path, acc, nil)
	if items, _ := l.Body["items"].([]any); l.Status != http.StatusOK || len(items) != 1 {
		t.Fatalf("listJournalEvidence = %d: %s", l.Status, l.Raw)
	}
	g := e.call(http.MethodGet, path+"/"+r.str("id"), e.as(authz.Auditor), nil)
	if g.Status != http.StatusOK || g.Header.Get("ETag") != `"1"` {
		t.Fatalf("getJournalEvidence = %d %v: %s", g.Status, g.Header, g.Raw)
	}
	stale := e.upload(path, acc, fields, "rent.pdf", pdf, "If-Match", `"9-draft"`)
	if stale.Status != http.StatusPreconditionFailed {
		t.Errorf("a stale If-Match = %d: %s", stale.Status, stale.Raw)
	}
	rm := e.call(http.MethodDelete, path+"/"+r.str("id"), acc, nil, "If-Match", `"1"`)
	if rm.Status != http.StatusOK || rm.str("state") != "removed" {
		t.Fatalf("removeJournalEvidence = %d: %s", rm.Status, rm.Raw)
	}
}

// Hostile and unacceptable uploads are refused before anything is stored: the EICAR test file, a type
// that is not accepted whatever its name says, an empty file, and payroll data.
// req: FR-DOC-01
// req: FR-DOC-08
// req: FR-DOC-09
// asvs: V5.2.1, V5.2.2
func TestEvidenceRefusals(t *testing.T) {
	e := newEnv(t)
	id, tag := e.createDraft("12.00")
	path := "/v1/journals/" + id + "/evidence"
	acc := e.as(authz.Accountant)
	for _, c := range []struct {
		name, file string
		fields     map[string]string
		code       string
	}{
		{"eicar.pdf", local.EICAR, nil, evidence.CodeMalware},
		{"invoice.pdf", "<html><script>alert(1)</script></html>", nil, evidence.CodeType},
		{"tool.pdf", "MZ\x90\x00\x03", nil, evidence.CodeType},
		{"empty.pdf", "", nil, evidence.CodeSize},
		{"payslip.pdf", "%PDF-1.7", map[string]string{"data_class": "payroll"}, ""},
	} {
		fields := map[string]string{"evidence_type": "receipt"}
		for k, v := range c.fields {
			fields[k] = v
		}
		r := e.upload(path, acc, fields, c.name, []byte(c.file), "If-Match", tag)
		if r.Status != http.StatusUnprocessableEntity || (c.code != "" && !strings.Contains(r.Raw, c.code)) {
			t.Errorf("%s: %d %s; want 422 %s", c.name, r.Status, r.Raw, c.code)
		}
	}
	if keys, _ := e.blobs.List(context.Background(), ""); len(keys) != 0 {
		t.Errorf("a refused upload was stored: %v", keys)
	}
	big := e.upload(path, acc, map[string]string{"evidence_type": "receipt"}, "big.pdf", append([]byte("%PDF-"), make([]byte, evidence.MaxBytes+1<<20)...), "If-Match", tag)
	if big.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("a 26 MB upload = %d: %s", big.Status, big.Raw)
	}
	other := e.upload("/v1/journals/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70/evidence", acc, map[string]string{"evidence_type": "receipt"},
		"r.pdf", []byte("%PDF-1.7"), "If-Match", "*")
	if other.Status != http.StatusNotFound {
		t.Errorf("a journal that does not exist = %d: %s", other.Status, other.Raw)
	}
}

// AI and MCP clients never upload, list or remove evidence: they propose doc.attach (FR-DOC-11, golden rule 5).
// An integration's file is recorded with source api.
// req: FR-DOC-11
// req: AI-34
// asvs: V8.2.1
func TestEvidenceAIProposesAndIntegrationsUpload(t *testing.T) {
	e := newEnv(t)
	id, tag := e.createDraft("12.00")
	path := "/v1/journals/" + id + "/evidence"
	every := []authz.Role{authz.Owner, authz.Controller, authz.Accountant}
	ai := e.principal(authn.Principal{Client: "claude-desktop", Class: authn.ClassAgent, Organisation: e.books.Org, Roles: every})
	if r := e.upload(path, ai, map[string]string{"evidence_type": "receipt"}, "r.pdf", []byte("%PDF-1.7"), "If-Match", tag); r.Status != http.StatusForbidden {
		t.Errorf("an MCP client uploaded: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodGet, path, ai, nil); r.Status != http.StatusForbidden {
		t.Errorf("an MCP client listed evidence: %d", r.Status)
	}
	bot := e.principal(authn.Principal{Client: "erp-sync", Class: authn.ClassIntegration, Organisation: e.books.Org, Roles: []authz.Role{authz.Accountant}})
	r := e.upload(path, bot, map[string]string{"evidence_type": "receipt", "source": "paste"}, "r.pdf", []byte("%PDF-1.7"), "If-Match", tag)
	if r.Status != http.StatusCreated || r.str("source") != "api" || !strings.HasPrefix(r.str("uploaded_by"), "integration:") {
		t.Errorf("an integration's upload = %d: %s", r.Status, r.Raw)
	}
}

// Once the journal is posted, its evidence is append-only: removal is refused; a new file can still be
// added, and the commit work moves it to the locked store with its retain-until, verified by hash.
// req: FR-DOC-07
// req: FR-DOC-13
// req: FR-DOC-02
// asvs: V16.2.1
func TestEvidenceOnAPostedJournalIsCommitted(t *testing.T) {
	e := newEnv(t)
	id, tag := e.createDraft("75.00")
	path := "/v1/journals/" + id + "/evidence"
	acc := e.as(authz.Accountant)
	first := e.upload(path, acc, map[string]string{"evidence_type": "invoice"}, "inv.pdf", []byte("%PDF-1.7 invoice"), "If-Match", tag)
	if first.Status != http.StatusCreated {
		t.Fatalf("upload = %d: %s", first.Status, first.Raw)
	}
	posted := e.call(http.MethodPost, "/v1/journals/"+id+"/post", acc, nil, "If-Match", tag)
	if posted.Status != http.StatusOK {
		t.Fatalf("post = %d: %s", posted.Status, posted.Raw)
	}
	// Posting queued the commit of its files, in its own transaction.
	if kind := nextWork(t, e); kind != evidence.CommitKind {
		t.Fatalf("posting a journal with files queued %q; want %s", kind, evidence.CommitKind)
	}
	rm := e.call(http.MethodDelete, path+"/"+first.str("id"), acc, nil, "If-Match", "*")
	if rm.Status != http.StatusConflict || rm.problemCode() != storage.CodeEvidence {
		t.Errorf("removing from a posted journal = %d: %s", rm.Status, rm.Raw)
	}
	second := e.upload(path, acc, map[string]string{"evidence_type": "proof of payment"}, "paid.txt", []byte("Paid 75.00 by PayNow\n"), "If-Match", posted.Header.Get("ETag"))
	if second.Status != http.StatusCreated || second.str("media_type") != evidence.Text {
		t.Fatalf("a second file on the posted journal = %d: %s", second.Status, second.Raw)
	}
	if kind := nextWork(t, e); kind != evidence.CommitKind {
		t.Fatalf("a file on a posted journal queued %q; want %s", kind, evidence.CommitKind)
	}
	lockedRoot := t.TempDir()
	locked, err := local.NewDir(lockedRoot)
	if err != nil {
		t.Fatal(err)
	}
	locked.Clock = e.clock
	c := evidence.Committer{Store: e.store, Staging: e.blobs, Locked: locked, Clock: e.clock, Zone: clock.Singapore}
	n, err := c.Commit(context.Background(), e.books.Org) // as the API's role, which cannot commit
	if err == nil || n != 0 {
		t.Fatalf("the API's role committed %d file(s): %v; only the worker may", n, err)
	}
	ws := workerStore(t)
	c.Store = ws
	if n, err := c.Commit(context.Background(), e.books.Org); err != nil || n != 2 {
		t.Fatalf("Commit = %d, %v; want both files", n, err)
	}
	g := e.call(http.MethodGet, path+"/"+first.str("id"), acc, nil)
	want := time.Date(2034, 1, 1, 0, 0, 0, 0, clock.Singapore).UTC().Format(time.RFC3339)
	if g.str("state") != "final" || g.str("retain_until") != want {
		t.Errorf("committed: %s; want final until %s", g.Raw, want)
	}
	key := evidence.LockedKey(e.books.Org, parseUUID(t, first.str("id")))
	rc, err := locked.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(b, []byte("%PDF-1.7 invoice")) {
		t.Errorf("the locked copy: %q", b)
	}
	if err := locked.Delete(context.Background(), key); err == nil {
		t.Error("a committed file was deleted from the locked store before its retain-until")
	}
	if keys, _ := e.blobs.List(context.Background(), "staging/"); len(keys) != 0 {
		t.Errorf("staged copies left after the commit: %v", keys)
	}
	// The nightly re-verification finds a committed file changed behind the store's back (C.2 #31).
	firstID := parseUUID(t, first.str("id"))
	mine := func(r evidence.VerifyReport) bool { return slices.Contains(r.Mismatched, firstID) }
	if rep, _ := evidence.Verify(context.Background(), ws, locked); rep.Checked < 2 || mine(rep) {
		t.Fatalf("Verify of intact files: %+v", rep)
	}
	if err := os.WriteFile(filepath.Join(lockedRoot, filepath.FromSlash(key)), []byte("%PDF-1.7 forged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rep, err := evidence.Verify(context.Background(), ws, locked); !mine(rep) || !errors.Is(err, evidence.ErrHashMismatch) {
		t.Errorf("Verify of a changed file: %+v, %v", rep, err)
	}
	// The sweep deletes a day-old staged file no row uses, and reports a locked file no row knows.
	stray := evidence.StagingKey(e.books.Org, uuidV7At(t, e.clock.Now().Add(-25*time.Hour)))
	if err := e.blobs.Put(context.Background(), stray, strings.NewReader("abandoned")); err != nil {
		t.Fatal(err)
	}
	orphan := evidence.LockedKey(e.books.Org, uuidV7At(t, e.clock.Now()))
	if err := locked.Put(context.Background(), orphan, strings.NewReader("orphan")); err != nil {
		t.Fatal(err)
	}
	rep, err := evidence.Sweep(context.Background(), ws, e.blobs, locked, e.clock.Now())
	if rep.Deleted != 1 || len(rep.Orphans) != 1 || rep.Orphans[0] != orphan || err == nil {
		t.Errorf("Sweep = %+v, %v; want the stray deleted and the orphan reported", rep, err)
	}
}
