package compliance

import (
	"strings"
	"testing"
	"time"

	registry "github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A high or critical finding fails CTL-RES-04 once it is open past its SLA, 30 and 7 days, and not on the day it falls
// due; a finding of another severity never does; the oldest is listed first (SEC-33).
// req: SEC-33
// req: CTL-RES-04
// asvs: V15.2.1
func TestPostureFindingsPastTheirSLA(t *testing.T) {
	now := time.Date(2099, 3, 1, 2, 0, 0, 0, time.UTC)
	open := []ports.PostureFinding{
		{Name: "critical-6", Severity: "CRITICAL", Category: "OPEN_FIREWALL", Resource: "fw", FirstSeen: now.AddDate(0, 0, -6)},
		{Name: "high-31", Severity: "HIGH", Category: "PUBLIC_BUCKET_ACL", Resource: "bucket", FirstSeen: now.AddDate(0, 0, -31)},
		{Name: "critical-8", Severity: "CRITICAL", Category: "OPEN_FIREWALL", Resource: "fw", FirstSeen: now.AddDate(0, 0, -8)},
		{Name: "high-30", Severity: "HIGH", Category: "SQL_NO_ROOT_PASSWORD", Resource: "db", FirstSeen: now.AddDate(0, 0, -30)},
		{Name: "medium-400", Severity: "MEDIUM", Category: "WEAK_SSL_POLICY", Resource: "lb", FirstSeen: now.AddDate(0, 0, -400)},
	}
	got := postureOverdue(open, now)
	if len(got) != 2 || got[0].ID != "high-31" || got[1].ID != "critical-8" {
		t.Fatalf("overdue: %+v", got)
	}
	if got[0].Detail != "A high finding (PUBLIC_BUCKET_ACL on bucket) is open past its 30 days" || got[0].At != "2099-01-29T02:00:00Z" ||
		got[1].Detail != "A critical finding (OPEN_FIREWALL on fw) is open past its 7 days" {
		t.Errorf("in words: %+v", got)
	}
}

// The log scan counts the entries of each kind it finds and keeps none of them: a finding never holds the value
// (CTL-DP-03, SEC-15).
// req: CTL-DP-03
// req: SEC-15
// asvs: V16.2.5
func TestTheLogScanCountsAndKeepsNothing(t *testing.T) {
	lines := []string{`{"msg":"journal posted"}`, "member S1234567D joined", "member T7654321J left", "Bearer " + strings.Repeat("t", 12), "fine"}
	got := logLeaks(lines)
	if len(got) != 2 || got[0].Detail != "2 log entries hold an NRIC or FIN number" || got[1].Detail != "1 log entry holds a bearer token" {
		t.Fatalf("found: %+v", got)
	}
	for _, f := range got {
		if strings.Contains(f.Detail+f.ID+f.At, "S1234567D") || strings.Contains(f.Detail+f.ID, "tttt") || f.ID != "" {
			t.Errorf("a finding keeps what it found: %+v", f)
		}
	}
	if got := logLeaks([]string{"clean", `{"level":"info"}`}); len(got) != 0 {
		t.Error("clean lines found something")
	}
}

// The platform's verifications apply in the installation's own organisation only; in every other one they are not
// applicable rather than never run (design D8).
// req: SEC-33
// req: CMP-01
// asvs: V15.2.1
func TestPlatformVerificationsApplyToTheInstallationOnly(t *testing.T) {
	now := time.Date(2099, 3, 1, 2, 0, 0, 0, time.UTC)
	c := registry.Control{ID: "CTL-RES-04", Checks: []registry.Check{{Frequency: "D", Status: "running", How: "verify posture.findings"}}}
	run := storage.ControlRun{Control: c.ID, Frequency: "D", Check: "verify posture.findings", RanAt: now.Add(-time.Hour), OK: false,
		Detail: "1 found in 3 open finding(s) of the last day: A critical finding is open past its 7 days"}
	in := Inputs{Now: now, Runs: map[string]storage.ControlRun{RunKey(c.ID, "D", "verify posture.findings"): run}}
	if st := StatusOf(c, in); st.Status != NotApplicable {
		t.Errorf("another organisation: %+v", st)
	}
	in.Installation = true
	if st := StatusOf(c, in); st.Status != Fail || st.Detail != run.Detail {
		t.Errorf("the installation's: %+v", st)
	}
	in.Runs = nil
	if st := StatusOf(c, in); st.Status != NotAssessed {
		t.Errorf("the installation's, not run yet: %+v", st)
	}
}
