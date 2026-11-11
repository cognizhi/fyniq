package compliance

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The platform's verifications (p1-security-hardening design D8) read the installation's own posture, not an
// organisation's records: they run in the installation's organisation only, whose Super admins see them, and every other
// organisation shows them as not applicable.

// platformVerification reads the platform's posture now.
type platformVerification struct {
	read func(ctx context.Context, r Runner, now time.Time) (found []Finding, read string, err error)
	// alert: a finding is also a security alert to the Super admins (SEC-31), beside the control's failure.
	alert bool
}

var platformVerifications = map[string]platformVerification{
	"posture.findings":     {read: postureFindings},
	"logs.scan":            {read: logsScan, alert: true},
	"scheduler.configured": {read: schedulerConfigured},
}

// Platform reports whether the verification name reads the installation's posture rather than an organisation's records.
func Platform(name string) bool { _, ok := platformVerifications[name]; return ok }

// PostureSLA is how many days a finding of each severity may stay open before CTL-RES-04 fails (SEC-33): the owner's
// decision of 11 Nov 2026 (PRD-001 rev 0.247).
var PostureSLA = map[string]int{"CRITICAL": 7, "HIGH": 30}

// LogSample is how many of the last day's log entries logs.scan reads.
const LogSample = 1000

// postureFindings (CTL-RES-04, SEC-33): no Security Command Center finding of high or critical severity open past its SLA.
func postureFindings(ctx context.Context, r Runner, now time.Time) ([]Finding, string, error) {
	if r.Posture == nil {
		return nil, "", errNoPlatform
	}
	open, err := r.Posture.Findings(ctx)
	if err != nil {
		return nil, "", err
	}
	return postureOverdue(open, now), fmt.Sprintf("%d open finding(s) of high or critical severity", len(open)), nil
}

// postureOverdue are the findings open longer than their severity's SLA, the oldest first.
func postureOverdue(open []ports.PostureFinding, now time.Time) []Finding {
	slices.SortStableFunc(open, func(a, b ports.PostureFinding) int { return a.FirstSeen.Compare(b.FirstSeen) })
	var out []Finding
	for _, f := range open {
		days, ok := PostureSLA[strings.ToUpper(f.Severity)]
		if !ok || !now.After(f.FirstSeen.AddDate(0, 0, days)) {
			continue
		}
		out = append(out, Finding{Kind: "posture_finding", ID: f.Name, At: day(f.FirstSeen),
			Detail: fmt.Sprintf("A %s finding (%s on %s) is open past its %d days", strings.ToLower(f.Severity), f.Category, f.Resource, days)})
	}
	return out
}

// logsScan (CTL-DP-03, SEC-15): none of a sample of the last day's log entries holds an NRIC or FIN number, a token or
// an API key. A finding counts the entries of each kind and never holds the entry.
func logsScan(ctx context.Context, r Runner, now time.Time) ([]Finding, string, error) {
	if r.Logs == nil {
		return nil, "", errNoPlatform
	}
	lines, err := r.Logs.Read(ctx, now.Add(-Window), LogSample)
	if err != nil {
		return nil, "", err
	}
	return logLeaks(lines), fmt.Sprintf("%d log entries", len(lines)), nil
}

// logLeaks are the kinds of value SEC-15 keeps out of the logs that lines hold, each with how many entries hold it.
func logLeaks(lines []string) []Finding {
	counts := map[string]int{}
	var kinds []string
	for _, l := range lines {
		for _, k := range audit.Leaks(l) {
			if counts[k] == 0 {
				kinds = append(kinds, k)
			}
			counts[k]++
		}
	}
	out := make([]Finding, len(kinds))
	for i, k := range kinds {
		out[i] = Finding{Kind: "log_entry", Detail: fmt.Sprintf("%d log %s %s", counts[k], plural(counts[k], "entry holds", "entries hold"), k)}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// HeartbeatPolicies are the heartbeat's alert policies scheduler.configured requires, by the end of their names
// (infra/gcp/modules/heartbeat): the tick's absence at 30 and 60 minutes, and a late platform job.
var HeartbeatPolicies = []string{"heartbeat tick absent 30 minutes", "heartbeat tick absent 60 minutes", "a platform job's heartbeat is late"}

// schedulerConfigured (CTL-RES-05, NFR-OBS-02): the tick's Scheduler job exists and runs, and the heartbeat's alert
// policies exist, are on and tell someone, so a stopped tick is noticed independently of the platform.
func schedulerConfigured(ctx context.Context, r Runner, _ time.Time) ([]Finding, string, error) {
	if r.Scheduling == nil {
		return nil, "", errNoPlatform
	}
	jobs, err := r.Scheduling.SchedulerJobs(ctx)
	if err != nil {
		return nil, "", err
	}
	policies, err := r.Scheduling.AlertPolicies(ctx)
	if err != nil {
		return nil, "", err
	}
	return schedulingFindings(jobs, policies), fmt.Sprintf("%d Scheduler job(s) and %d alert policies", len(jobs), len(policies)), nil
}

// schedulingFindings are what keeps a stopped tick from being noticed: no tick, a paused one, and a heartbeat alert
// policy missing, off or telling nobody.
func schedulingFindings(jobs []ports.SchedulerJob, policies []ports.AlertPolicy) []Finding {
	var out []Finding
	i := slices.IndexFunc(jobs, func(j ports.SchedulerJob) bool { return j.Name == "tick" })
	switch {
	case i < 0:
		out = append(out, Finding{Kind: "scheduler_job", ID: "tick", Detail: "The Scheduler has no tick job, so no platform job runs"})
	case !jobs[i].Enabled:
		out = append(out, Finding{Kind: "scheduler_job", ID: "tick", Detail: "The tick's Scheduler job is paused, so no platform job runs"})
	}
	for _, want := range HeartbeatPolicies {
		j := slices.IndexFunc(policies, func(p ports.AlertPolicy) bool { return strings.HasSuffix(p.Name, want) })
		switch {
		case j < 0:
			out = append(out, Finding{Kind: "alert_policy", ID: want, Detail: fmt.Sprintf("No alert policy %q: a stopped heartbeat would go unnoticed", want)})
		case !policies[j].Enabled:
			out = append(out, Finding{Kind: "alert_policy", ID: want, Detail: fmt.Sprintf("The alert policy %q is off", want)})
		case policies[j].Channels == 0:
			out = append(out, Finding{Kind: "alert_policy", ID: want, Detail: fmt.Sprintf("The alert policy %q tells nobody", want)})
		}
	}
	return out
}

// errNoPlatform: the deployment gave the runner no reader of the platform's posture or logs.
var errNoPlatform = errors.New("no reader of the platform's posture or logs is configured")

// installation reports whether org is the installation's own organisation.
func (r Runner) installation(ctx context.Context, org uuid.UUID) (bool, error) {
	if r.Installation == nil {
		return false, nil
	}
	inst, err := r.Installation.InstallationOrganisation(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return false, nil
	}
	return err == nil && inst == org, err
}
