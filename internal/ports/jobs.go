package ports

import (
	"context"
	"time"
)

// JobRunner starts long work as a job (ADR-T7: Cloud Run jobs, task timeout up to 7 days). Start
// returns once the job is launched, with its execution's name; a launched job is not done: it
// reports its own completion. Production: Cloud Run jobs (p0-infra-opentofu). Local: the worker
// service runs the job in the background.
type JobRunner interface {
	Start(ctx context.Context, job string, slot time.Time) (execution string, err error)
}
