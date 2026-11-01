//go:build local

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// trail is an organisation's audit trail, as the read-only role reads it.
func trail(t *testing.T, org uuid.UUID) string {
	t.Helper()
	var out strings.Builder
	if err := store(t, "fyniq_readonly").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		for _, ev := range events {
			out.WriteString(ev.Body + "\n")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// FR-CTL-01 (p1-checks design D4): the worker writes a check's state at version 1 and replaces it only as its next
// version; its first result and each change of status are audited on the check, and a rerun that finds the same adds
// nothing to the trail; the API cannot write it, nothing deletes it (FY245), and another organisation never sees it.
// req: FR-CTL-01
// req: QA-ENG-05
// asvs: V8.2.2, V16.3.1
func TestACheckStateIsReplacedByEachRunAndItsStatusChangesAudited(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "service", ID: "checks"}})
	worker := store(t, "fyniq_worker")
	b := books(t, "Check states Pte Ltd")
	other := books(t, "Other checks Pte Ltd")
	at := time.Date(2026, 11, 1, 20, 15, 0, 0, time.UTC)
	save := func(s storage.CheckState, version int) (storage.CheckState, error) {
		var out storage.CheckState
		err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.Checks().Save(ctx, s, version)
			return err
		})
		return out, err
	}
	state := storage.CheckState{Key: "books.debits-equal-credits", Status: "ok", Detail: "Fine", RunAt: at, Trigger: storage.CheckNightly,
		LedgerRevision: 3, ChangedAt: at}
	first, err := save(state, 0)
	if err != nil || first.Version != 1 || first.Status != "ok" || string(first.Items) != "[]" {
		t.Fatalf("the first result: %+v, %v", first, err)
	}
	if _, err := save(state, 0); !refusedAs(err, storage.CodeVersion) {
		t.Errorf("a first result written twice: %v", err)
	}
	before := strings.Count(trail(t, b.Org), `"check.status_changed"`)
	state.RunAt = at.Add(time.Hour)
	again, err := save(state, 1)
	if err != nil || again.Version != 2 || !again.RunAt.Equal(at.Add(time.Hour)) {
		t.Fatalf("a rerun that finds the same: %+v, %v", again, err)
	}
	if n := strings.Count(trail(t, b.Org), `"check.status_changed"`); n != before {
		t.Errorf("a rerun that finds the same added %d audit event(s)", n-before)
	}
	state.Status, state.Count, state.Items = "error", 1, []byte(`[{"kind":"period","label":"October 2026"}]`)
	state.RunAt, state.ChangedAt = at.Add(2*time.Hour), at.Add(2*time.Hour)
	if changed, err := save(state, 2); err != nil || changed.Status != "error" || changed.Count != 1 {
		t.Fatalf("a change of status: %+v, %v", changed, err)
	}
	if _, err := save(state, 2); !refusedAs(err, storage.CodeVersion) {
		t.Errorf("a result from a stale read: %v", err)
	}
	tr := trail(t, b.Org)
	for _, want := range []string{`"action":"check.status_changed"`, `"status":["ok","error"]`, `"status":[null,"ok"]`} {
		if !strings.Contains(tr, want) {
			t.Errorf("the check's audit events lack %s", want)
		}
	}
	for name, bad := range map[string]storage.CheckState{
		"not assessed without what it needs": {Key: "books.x", Status: "not_assessed", RunAt: at, Trigger: storage.CheckNightly, ChangedAt: at},
		"an unknown status":                  {Key: "books.y", Status: "maybe", RunAt: at, Trigger: storage.CheckNightly, ChangedAt: at},
		"a key that is not a key":            {Key: "Books", Status: "ok", RunAt: at, Trigger: storage.CheckNightly, ChangedAt: at},
		"more items than it counted":         {Key: "books.z", Status: "error", Items: []byte(`[{"kind":"a"},{"kind":"b"}]`), Count: 1, RunAt: at, Trigger: storage.CheckNightly, ChangedAt: at},
	} {
		if _, err := save(bad, 0); !refusedAs(err, storage.CodeInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := worker.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		states, err := u.Checks().States(ctx)
		if len(states) != 0 {
			t.Errorf("another organisation reads the checks: %+v", states)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// FY245 and the grants: the worker writes the state, as its next version only; nobody deletes it, and the API never
	// writes it; a state written without its audit event is refused (FY061).
	for _, c := range []struct {
		role, sql, code string
	}{
		{"fyniq_worker", `UPDATE fyniq.check_state SET status = 'ok', version = 9 WHERE key = 'books.debits-equal-credits'`, "FY245"},
		{"fyniq_worker", `UPDATE fyniq.check_state SET version = version + 1, run_at = '2020-01-01' WHERE key = 'books.debits-equal-credits'`, "FY245"},
		{"fyniq_worker", `DELETE FROM fyniq.check_state WHERE key = 'books.debits-equal-credits'`, "42501"},
		{"fyniq_api", `UPDATE fyniq.check_state SET status = 'ok' WHERE key = 'books.debits-equal-credits'`, "42501"},
		{"fyniq_api", `INSERT INTO fyniq.check_state (key, status, item_count, run_at, run_trigger, ledger_revision, changed_at) VALUES ('books.a', 'ok', 0, now(), 'nightly', 0, now())`, "42501"},
		{"fyniq_worker", `INSERT INTO fyniq.check_state (key, status, item_count, run_at, run_trigger, ledger_revision, changed_at) VALUES ('books.a', 'ok', 0, now(), 'nightly', 0, now())`, "FY061"},
		{"fyniq_worker", `UPDATE fyniq.check_state SET status = 'warning', version = version + 1 WHERE key = 'books.debits-equal-credits'`, "FY061"},
	} {
		err := asOrg(context.Background(), conn(t, c.role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), c.sql)
			if err == nil {
				_, err = tx.Exec(context.Background(), "SET CONSTRAINTS ALL IMMEDIATE")
			}
			return err
		})
		if err == nil || !strings.Contains(err.Error(), c.code) {
			t.Errorf("%s: %s: want %s, got %v", c.role, c.sql, c.code, err)
		}
	}
}

// FR-CTL-01, FR-CTL-05 (design D4): each run is recorded once, with the ledger revision and the audit event it read
// through, which is the watermark the next run on a change reads from; a nightly run keeps its results and the others
// do not; a run never changes, and is deleted only past its retention (FY245): 90 days, or 13 months for a nightly run.
// req: FR-CTL-01
// req: FR-CTL-05
// req: QA-ENG-05
// asvs: V8.2.2, V16.3.1
func TestEachRunOfTheChecksIsRecordedAndKeptForItsRetention(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "service", ID: "checks"}})
	worker := store(t, "fyniq_worker")
	b := books(t, "Check runs Pte Ltd")
	now := time.Date(2026, 11, 1, 20, 15, 0, 0, time.UTC)
	record := func(n storage.NewCheckRun) (storage.CheckRun, error) {
		var out storage.CheckRun
		err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.Checks().RecordRun(ctx, n)
			return err
		})
		return out, err
	}
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Checks().LatestRun(ctx, ""); !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("the latest run before any: %v", err)
		}
		mark, err := u.Checks().Watermark(ctx)
		if mark != 0 {
			t.Errorf("the watermark before any run: %d", mark)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	old := now.AddDate(-2, 0, 0)
	for _, n := range []storage.NewCheckRun{
		{Trigger: storage.CheckOnChange, StartedAt: old, FinishedAt: old, AuditSeq: 4, Keys: []string{"books.a"}},
		{Trigger: storage.CheckNightly, StartedAt: old, FinishedAt: old, AuditSeq: 5, Keys: []string{"books.a"}, Results: []byte(`[]`)},
		{Trigger: storage.CheckOnRequest, RequestedBy: "user:founder", StartedAt: now, FinishedAt: now.Add(time.Second), LedgerRevision: 2, AuditSeq: 9,
			Keys: []string{"books.a", "books.b"}},
	} {
		if _, err := record(n); err != nil {
			t.Fatalf("recording %s: %v", n.Trigger, err)
		}
	}
	for name, n := range map[string]storage.NewCheckRun{
		"a nightly run without its results":   {Trigger: storage.CheckNightly, StartedAt: now, FinishedAt: now},
		"a run on a change with results":      {Trigger: storage.CheckOnChange, StartedAt: now, FinishedAt: now, Results: []byte(`[]`)},
		"a run on request naming nobody":      {Trigger: storage.CheckOnRequest, StartedAt: now, FinishedAt: now},
		"a run that finished before it began": {Trigger: storage.CheckOnChange, StartedAt: now, FinishedAt: now.Add(-time.Second)},
	} {
		if _, err := record(n); !refusedAs(err, storage.CodeInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		latest, err := u.Checks().LatestRun(ctx, "")
		if err != nil || latest.Trigger != storage.CheckOnRequest || latest.RequestedBy != "user:founder" || len(latest.Keys) != 2 {
			t.Errorf("the latest run: %+v, %v", latest, err)
		}
		nightly, err := u.Checks().LatestRun(ctx, storage.CheckNightly)
		if err != nil || string(nightly.Results) != "[]" {
			t.Errorf("the latest nightly run: %+v, %v", nightly, err)
		}
		if mark, err := u.Checks().Watermark(ctx); err != nil || mark != 9 {
			t.Errorf("the watermark: %d, %v", mark, err)
		}
		// Past their retention, the old run on a change and the old nightly run go; the recent one stays.
		n, err := u.Checks().Purge(ctx, now.AddDate(0, 0, -91), now.AddDate(0, -13, -1))
		if err != nil || n != 2 {
			t.Errorf("the purge: %d, %v", n, err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		role, sql, code string
	}{
		{"fyniq_worker", `UPDATE fyniq.check_run SET audit_seq = 1`, "42501"},
		{"fyniq_worker", `DELETE FROM fyniq.check_run`, "FY245"},
		{"fyniq_migrator", `UPDATE fyniq.check_run SET audit_seq = 1`, "FY245"},
		{"fyniq_api", `INSERT INTO fyniq.check_run (run_trigger, started_at, finished_at, ledger_revision, audit_seq, keys) VALUES ('change', now(), now(), 0, 0, '{}')`, "42501"},
	} {
		err := asOrg(context.Background(), conn(t, c.role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), c.sql)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), c.code) {
			t.Errorf("%s: %s: want %s, got %v", c.role, c.sql, c.code, err)
		}
	}
}

// FR-CTL-01 (design D8): the API names a member as a check's owner, or gives it back to its role, at the owner's next
// version, audited with the person before and after; someone who is not a member is refused; the worker never writes it;
// nothing deletes it (FY245).
// req: FR-CTL-01
// req: QA-ENG-05
// asvs: V8.2.2, V16.3.1
func TestACheckOwnerIsAMemberNamedAsAVersion(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "founder"}})
	api := store(t, "fyniq_api")
	b := books(t, "Check owners Pte Ltd")
	day := porttest.Day(10, 1)
	member := founder(t, api, b.Org, 1, day)
	var stranger uuid.UUID
	if err := conn(t, "fyniq_migrator").QueryRow(ctx, "INSERT INTO fyniq.app_user (display_name) VALUES ('Not A Member') RETURNING user_id").Scan(&stranger); err != nil {
		t.Fatal(err)
	}
	set := func(person *uuid.UUID, version int) (storage.CheckOwner, error) {
		var out storage.CheckOwner
		err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.Checks().SetOwner(ctx, "books.debits-equal-credits", person, "user:founder", day, version)
			return err
		})
		return out, err
	}
	if _, err := set(&stranger, 0); !refusedAs(err, storage.CodeInvalid) {
		t.Errorf("naming someone who is not a member: %v", err)
	}
	named, err := set(&member, 0)
	if err != nil || named.Version != 1 || named.Person == nil || *named.Person != member {
		t.Fatalf("naming a member: %+v, %v", named, err)
	}
	if _, err := set(nil, 0); !refusedAs(err, storage.CodeVersion) {
		t.Errorf("naming twice from no owner: %v", err)
	}
	back, err := set(nil, 1)
	if err != nil || back.Version != 2 || back.Person != nil {
		t.Fatalf("giving it back to the role: %+v, %v", back, err)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		owners, err := u.Checks().Owners(ctx)
		if len(owners) != 1 || owners[0].Version != 2 {
			t.Errorf("the owners: %+v", owners)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if tr := trail(t, b.Org); !strings.Contains(tr, `"action":"check.owner_set"`) || !strings.Contains(tr, `"person":["`+member.String()+`",null]`) {
		t.Errorf("the owner's audit events: %s", tr)
	}
	for _, c := range []struct {
		role, sql, code string
	}{
		{"fyniq_api", `UPDATE fyniq.check_owner SET person_user_id = NULL, version = 7`, "FY245"},
		{"fyniq_api", `DELETE FROM fyniq.check_owner`, "42501"},
		{"fyniq_worker", `UPDATE fyniq.check_owner SET person_user_id = NULL`, "42501"},
	} {
		err := asOrg(context.Background(), conn(t, c.role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), c.sql)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), c.code) {
			t.Errorf("%s: %s: want %s, got %v", c.role, c.sql, c.code, err)
		}
	}
}

// FR-CTL-01 (design D5): a unit of work that recorded audit events tells the store's change hook their object types once
// its work is done, so it can ask for the checks' refresh in the same transaction; one that recorded none does not; and
// a kind's row is waiting while it is pending or dispatched, not once it runs.
// req: FR-CTL-01
func TestAChangeTellsTheHookAndARefreshWaitsOnce(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "founder"}})
	api := store(t, "fyniq_api")
	b := books(t, "Change hook Pte Ltd")
	var seen [][]string
	api.OnChange(func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, types []string) error {
		if org != b.Org {
			t.Errorf("the hook was told of organisation %s", org)
		}
		seen = append(seen, types)
		if waiting, err := u.Outbox().Waiting(ctx, "checks.refresh"); err != nil || waiting {
			return err
		}
		_, err := u.Outbox().Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: "checks.refresh"})
		return err
	})
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Accounts().List(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 {
		t.Fatalf("a read told the hook: %v", seen)
	}
	for i := range 2 {
		if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "690" + string(rune('0'+i)), Name: "Sundry", Type: storage.Expense})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 2 || !slices.Contains(seen[0], "account") {
		t.Fatalf("the hook heard %v", seen)
	}
	far := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC) // after the rows were written, whenever the test runs
	var rows []storage.OutboxRow
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		due, err := u.Sweeping().Due(ctx, far, far)
		for _, r := range due {
			if r.Kind == "checks.refresh" {
				rows = append(rows, r)
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("two changes while one refresh waits asked for %d", len(rows))
	}
	worker := store(t, "fyniq_worker")
	waiting := func() bool {
		var out bool
		if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.Outbox().Waiting(ctx, "checks.refresh")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Outbox().MarkDispatched(ctx, rows[0].ID, "task-1", far.Add(time.Hour))
	}); err != nil {
		t.Fatal(err)
	}
	if !waiting() {
		t.Error("a dispatched refresh does not count as waiting")
	}
	var leased bool
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		_, leased, err = u.Outbox().Lease(ctx, rows[0].ID, "test", far, far.Add(time.Minute))
		return err
	}); err != nil || !leased {
		t.Fatalf("leasing the refresh: %v, %v", leased, err)
	}
	if waiting() {
		t.Error("a running refresh still counts as waiting: a change now would be missed")
	}
}
