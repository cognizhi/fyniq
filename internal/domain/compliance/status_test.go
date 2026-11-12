package compliance

import (
	"strings"
	"testing"
	"time"

	registry "github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Each kind of check reads its own source (design D1): CI from the release, a job from its latest slot, a catalogue check
// from its state, a verification from its latest run, an attestation from its review; planned and cloud checks are not
// assessed, naming why. A control takes the worst, in the order fail, overdue, not assessed, pass.
// req: CMP-01
// req: compliance/compliance-center The Compliance Center shows controls and raises tasks
func TestAControlTakesTheWorstOfItsChecks(t *testing.T) {
	now := time.Date(2099, 1, 2, 20, 0, 0, 0, time.UTC)
	assets, err := registry.LoadReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	in := Inputs{Assets: assets, Now: now, Release: Release{Tag: "v1.2.0", Repository: "https://github.com/cognizhi/fyniq"},
		Jobs: map[string][]storage.JobSlot{
			"ledger.verify":   {{Job: "ledger.verify", Slot: now.Add(-2 * time.Hour), State: "done"}},
			"audit.verify":    {{Job: "audit.verify", Slot: now.Add(-2 * time.Hour), State: "failed", Detail: "the chain broke at 41"}},
			"evidence.verify": {{Job: "evidence.verify", Slot: now.Add(-50 * time.Hour), State: "done"}},
		},
		Checks: map[string]storage.CheckState{
			"ledger.sub-ledgers":          {Key: "ledger.sub-ledgers", Status: "ok", Detail: "Each sub-ledger agrees"},
			"controls.review-log-overdue": {Key: "controls.review-log-overdue", Status: "warning", Detail: "2 actions wait"},
			"close.checklist":             {Key: "close.checklist", Status: "error", Detail: "3 tasks open"},
			"gst.return-due":              {Key: "gst.return-due", Status: "not_assessed", Needs: "the GST registration"},
		},
		Reviews: map[string]Review{
			"access-review": {Kind: "access-review", Status: ReviewOverdue, Detail: "The access review of Jul–Sep 2098 waits for an Owner's attestation"},
			"close-signoff": {Kind: "close-signoff", Status: ReviewSigned, Detail: "Every month that ended is closed."},
			"review-log":    {Kind: "review-log", Status: ReviewNotApplicable, Detail: "Standard mode: approvals before the action replace the weekly review."},
		},
		Runs: map[string]storage.ControlRun{
			RunKey("CTL-X-01", "N", "verify payments.dual-control"): {RanAt: now.Add(-time.Hour), OK: false, Count: 1, Detail: "1 found"},
			RunKey("CTL-X-02", "N", "verify payments.dual-control"): {RanAt: now.Add(-time.Hour), OK: true, Detail: "Nothing found"},
			RunKey("CTL-X-03", "N", "verify payments.dual-control"): {RanAt: now.Add(-30 * time.Hour), OK: true, Detail: "Nothing found"},
		},
	}
	check := func(f, how string) registry.Check { return registry.Check{Frequency: f, Status: "running", How: how} }
	for _, c := range []struct {
		id     string
		checks []registry.Check
		want   Status
		says   string
	}{
		{"CTL-X-01", []registry.Check{check("CI", "make test"), check("N", "verify payments.dual-control")}, Fail, "1 found"},
		{"CTL-X-02", []registry.Check{check("CI", "make test"), check("N", "verify payments.dual-control")}, Pass, "Each of its 2 check(s) passed."},
		{"CTL-X-03", []registry.Check{check("N", "verify payments.dual-control")}, Overdue, "has not run since"},
		{"CTL-X-04", []registry.Check{check("N", "verify members.second-factor")}, NotAssessed, "has not run for this organisation"},
		{"CTL-X-05", []registry.Check{check("N", "job ledger.verify"), check("N", "check ledger.sub-ledgers")}, Pass, ""},
		{"CTL-X-06", []registry.Check{check("N", "job audit.verify")}, Fail, "the chain broke at 41"},
		{"CTL-X-07", []registry.Check{check("N", "job evidence.verify")}, Overdue, "has not run on time"},
		{"CTL-X-08", []registry.Check{check("N", "job no.such.job")}, Overdue, "never run"},
		{"CTL-X-09", []registry.Check{check("W", "check controls.review-log-overdue"), check("E", "check close.checklist")}, Fail, "3 tasks open"},
		{"CTL-X-10", []registry.Check{check("W", "check controls.review-log-overdue")}, Overdue, "2 actions wait"},
		{"CTL-X-11", []registry.Check{check("D", "check gst.return-due")}, NotAssessed, "needs the GST registration"},
		{"CTL-X-12", []registry.Check{check("D", "check compliance.deadlines")}, NotAssessed, "has not run for this organisation"},
		{"CTL-X-13", []registry.Check{{Frequency: "D", Status: "cloud", How: "tofu environment/spec"}, check("CI", "make test")}, NotAssessed, "Runs in the cloud"},
		{"CTL-X-14", []registry.Check{{Frequency: "N", Status: "planned", Arrives: "p1-ai-proposals (the reconciliation)"}}, NotAssessed, "arrives with p1-ai-proposals."},
		{"CTL-X-15", []registry.Check{check("Q", "attestation access-review")}, Overdue, "waits for an Owner's attestation"},
		{"CTL-X-18", []registry.Check{check("E", "attestation close-signoff")}, Pass, "Each of its 1 check(s) passed."},
		{"CTL-X-19", []registry.Check{check("W", "attestation review-log")}, NotApplicable, "None of its checks applies here."},
		{"CTL-X-20", []registry.Check{check("Q", "attestation risk-review")}, NotAssessed, "has not been read"},
		{"CTL-X-16", nil, NotApplicable, "None of its checks applies here."},
	} {
		got := StatusOf(registry.Control{ID: c.id, Checks: c.checks}, in)
		if got.Status != c.want || !strings.Contains(got.Detail, c.says) || len(got.Checks) != len(c.checks) {
			t.Errorf("%s: %s %q, want %s saying %q", c.id, got.Status, got.Detail, c.want, c.says)
		}
	}
	// In a development build a CI check is not assessed, so a control checked only in CI is not assessed either.
	dev := in
	dev.Release = Release{}
	if got := StatusOf(registry.Control{ID: "CTL-X-17", Checks: []registry.Check{check("CI", "make test")}}, dev); got.Status != NotAssessed ||
		!strings.Contains(got.Detail, "development build") {
		t.Errorf("a CI check in a development build: %+v", got)
	}
}

// A control that newly fails or falls overdue through a detective check notifies its owner, once; a recovery and a
// control already failing do not; a CI check never does (design D5).
// req: CMP-01
// req: compliance/compliance-center Failing controls block releases or page the owner
func TestWhenAFailingControlNotifies(t *testing.T) {
	detective := ControlStatus{Status: Fail, Checks: []CheckResult{{How: "verify payments.dual-control", Result: Result{Status: Fail}, Detective: true}}}
	overdue := ControlStatus{Status: Overdue, Checks: []CheckResult{{How: "check controls.review-log-overdue", Result: Result{Status: Overdue}, Detective: true}}}
	ci := ControlStatus{Status: Fail, Checks: []CheckResult{{How: "make test", Result: Result{Status: Fail}}}}
	for _, c := range []struct {
		what   string
		before Status
		after  ControlStatus
		want   bool
	}{
		{"a detective check newly failing", Pass, detective, true},
		{"a detective check newly overdue", NotAssessed, overdue, true},
		{"the first run finding it failing", "", detective, true},
		{"a control still failing", Fail, detective, false},
		{"a failing control now overdue", Fail, overdue, false},
		{"a CI check failing", Pass, ci, false},
		{"a recovery", Fail, ControlStatus{Status: Pass}, false},
		{"not assessed", Pass, ControlStatus{Status: NotAssessed}, false},
	} {
		if got := Notify(c.before, c.after); got != c.want {
			t.Errorf("%s: notify %v, want %v", c.what, got, c.want)
		}
	}
}

// Every control of the registry has a status, from its checks alone (design D1).
// req: CMP-01
func TestEveryControlHasAStatus(t *testing.T) {
	controls, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	assets, err := registry.LoadReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	in := Inputs{Assets: assets, Now: time.Date(2099, 1, 2, 20, 0, 0, 0, time.UTC)}
	for _, c := range controls {
		got := StatusOf(c, in)
		if got.Status == "" || got.Detail == "" || len(got.Checks) != len(c.Checks) {
			t.Errorf("%s: %+v", c.ID, got)
		}
	}
}
