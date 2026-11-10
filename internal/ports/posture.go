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
