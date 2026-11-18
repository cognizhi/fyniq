package ports

import (
	"context"
	"time"
)

// The platform's own security posture (SEC-33, CTL-RES-04, CTL-DP-03; p1-security-hardening design D8): the cloud's open
// findings, and a sample of the platform's own logs. Both are of the installation, not of an organisation.

// PostureFinding is an open finding about the platform's cloud: Security Command Center's in the cloud.
type PostureFinding struct {
	Name      string    // the finding's resource name
	Severity  string    // CRITICAL, HIGH, MEDIUM or LOW
	Category  string    // such as PUBLIC_BUCKET_ACL
	Resource  string    // what it is about
	FirstSeen time.Time // when it was first found
}

// Posture lists the cloud's open findings of high or critical severity.
type Posture interface {
	Findings(ctx context.Context) ([]PostureFinding, error)
}

// LogReader reads a sample of the platform's application log entries since a time, newest first, at most limit, each
// as one line of text.
type LogReader interface {
	Read(ctx context.Context, since time.Time, limit int) ([]string, error)
}

// Scheduling reads how the platform's schedule and its heartbeat's alerts are configured (NFR-OBS-02, CTL-RES-05;
// p1-operations-dr design D6): the Scheduler's jobs, and the alert policies. Production: Cloud Scheduler's and Cloud
// Monitoring's APIs, as the jobs' account; local: the local stack's own tick and its stand-in alerts.
type Scheduling interface {
	SchedulerJobs(ctx context.Context) ([]SchedulerJob, error)
	AlertPolicies(ctx context.Context) ([]AlertPolicy, error)
}

// SchedulerJob is one of the Scheduler's jobs: its name (the last part of its resource name) and whether it runs.
type SchedulerJob struct {
	Name     string
	Enabled  bool
	Schedule string
}

// AlertPolicy is one of the alert policies: its display name, whether it is on, and how many channels it tells.
type AlertPolicy struct {
	Name     string
	Enabled  bool
	Channels int
}
