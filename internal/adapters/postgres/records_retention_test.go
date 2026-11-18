//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	registry "github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The daily records.retention check, on the local store: each file committed in the last day is kept locked until its
// record's retain-until, and every kept file has its record. A file kept as its record says passes; a file kept without
// a retention, one kept too short, one missing and one with no record are each found; an organisation with nothing kept
// finds nothing; and without a store the check does not run (CTL-AUD-03, NFR-RET-01, DR-07; p1-operations-dr D3).
// req: CTL-AUD-03
// req: NFR-RET-01
// req: DR-07
// asvs: V14.2.7
func TestTheRecordsRetentionCheck(t *testing.T) {
	ctx := context.Background()
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	b, other := books(t, "Records Pte Ltd"), books(t, "Nothing Kept Pte Ltd")
	svc := journal.Service{Store: api}
	doc, err := svc.Create(ctx, b.Org, saleDraft(t, b, porttest.Day(10, 14), "250.00"))
	if err != nil {
		t.Fatal(err)
	}
	good, unlocked, short, missing := attach(t, api, b.Org, doc.ID, "receipt"), attach(t, api, b.Org, doc.ID, "receipt"),
		attach(t, api, b.Org, doc.ID, "receipt"), attach(t, api, b.Org, doc.ID, "receipt")
	if _, err := svc.Post(ctx, b.Org, doc.ID); err != nil {
		t.Fatal(err)
	}
	dir, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	until := time.Date(2034, 1, 1, 0, 0, 0, 0, time.UTC)
	key := func(e storage.Evidence) string { return "evidence/" + b.Org.String() + "/" + e.ID.String() }
	for _, f := range []struct {
		e    storage.Evidence
		kept *time.Time // nil: stored without a retention
		skip bool       // never stored
	}{{good, &until, false}, {unlocked, nil, false}, {short, new(until.AddDate(-1, 0, 0)), false}, {missing, nil, true}} {
		if !f.skip {
			if err := dir.Create(ctx, key(f.e), strings.NewReader("%PDF-1.7 receipt")); err != nil {
				t.Fatal(err)
			}
		}
		if f.kept != nil {
			if err := dir.SetRetainUntil(ctx, key(f.e), *f.kept); err != nil {
				t.Fatal(err)
			}
		}
		if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Evidence().Finalise(ctx, f.e.ID, key(f.e), until)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	orphan := "evidence/" + b.Org.String() + "/" + uuid.Must(uuid.NewV7()).String()
	if err := dir.Create(ctx, orphan, strings.NewReader("no record")); err != nil {
		t.Fatal(err)
	}
	controls, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	runOf := func(r compliance.Runner, org uuid.UUID) (storage.ControlRun, bool) {
		t.Helper()
		rep, err := r.Verify(ctx, org)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(rep.Runs, func(r storage.ControlRun) bool { return r.Check == "verify records.retention" })
		if i < 0 {
			return storage.ControlRun{}, false
		}
		return rep.Runs[i], true
	}
	runner := compliance.Runner{Store: worker, Clock: clock.System{}, Controls: controls, Evidence: dir}
	run, ok := runOf(runner, b.Org)
	if !ok || run.OK || run.Count != 4 || run.Control != "CTL-AUD-03" || run.Frequency != "D" {
		t.Fatalf("the run: %+v", run)
	}
	var found []compliance.Finding
	if err := json.Unmarshal(run.Items, &found); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range found {
		got[f.ID] = f.Detail
	}
	want := map[string]string{
		unlocked.ID.String(): "Kept without a locked retention, so it could be deleted before its time",
		short.ID.String():    "Kept until 1 Jan 2033, before its record's 1 Jan 2034",
		missing.ID.String():  "Committed, but missing from the records store",
		orphan:               "Kept in the records store with no record",
	}
	if len(got) != len(want) {
		t.Fatalf("found %v", got)
	}
	for id, d := range want {
		if got[id] != d {
			t.Errorf("%s: %q, want %q", id, got[id], d)
		}
	}
	if !strings.Contains(run.Detail, "4 file(s) committed and 4 kept") {
		t.Errorf("in words: %s", run.Detail)
	}
	if run, ok := runOf(runner, other.Org); !ok || !run.OK {
		t.Errorf("an organisation with nothing kept: %+v", run)
	}
	if _, ok := runOf(compliance.Runner{Store: worker, Clock: clock.System{}, Controls: controls}, b.Org); ok {
		t.Error("the check ran without a store")
	}
}
