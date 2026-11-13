package backup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The monthly restore drill (DR-06, NFR-DR-02, NFR-DR-03, CTL-RES-02 M; p1-operations-dr design D2): a copy of the
// database as it was an hour ago, the scenario of database corruption or operator error, made by point-in-time
// recovery in production's own project where no service reaches it; the ledger's and the audit chain's verifications
// run on the copy; the drill is timed against NFR-DR-02's four hours, recorded in the installation's trail, and the copy
// removed whatever happens. The quarterly full drill of the weekly export into an isolated project is a person's
// (docs/runbooks/restore-drill.md).

// DrillJob is the platform job of the monthly drill.
const DrillJob = "restore.drill"

// DrillBack is how far back the drill's copy goes: within point-in-time recovery's 7 days of logs.
const DrillBack = time.Hour

// DrillTarget is NFR-DR-02's recovery time for database corruption or operator error at P1: a drill that takes longer
// fails.
const DrillTarget = 4 * time.Hour

// ActionDrilled is the drill's audit event, in the installation's trail.
const ActionDrilled = "restore.drilled"

// Drill is one restore drill.
type Drill struct {
	Clone ports.DatabaseClone
	// Open opens the copy as the jobs' own role; close releases it.
	Open func(ctx context.Context, t ports.CloneTarget) (store storage.Store, close func(), err error)
	// Store is production's database, where the drill is recorded in the installation's trail.
	Store        storage.Store
	Installation interface {
		InstallationOrganisation(ctx context.Context) (uuid.UUID, error)
	}
	// The anchors' store and key, and the deployment's keyed hash, which the audit chain is verified against.
	Blobs  ports.BlobStore
	Signer ports.Signer
	Keyed  audit.Keyed
	Clock  ports.Clock
}

// DrillReport is what a drill found.
type DrillReport struct {
	Copy          string        // the copy's name
	PointInTime   time.Time     // the time the copy is of
	Took          time.Duration // from asking for the copy until both verifications ended
	Organisations int
	LedgerFailed  int // organisations whose ledger and balances disagree
	ChainsFailed  int // organisations whose audit chain fails against its anchors
	Ahead         int // anchors written after the copy's point in time, not checked
	Removed       bool
	Problem       string // empty when the drill passed
}

// OK reports whether the drill passed.
func (r DrillReport) OK() bool { return r.Problem == "" }

// Run makes the drill: the copy, both verifications on it, the copy's removal, and the record.
func (d Drill) Run(ctx context.Context) (DrillReport, error) {
	start := d.Clock.Now()
	rep := DrillReport{PointInTime: start.Add(-DrillBack).UTC().Truncate(time.Second)}
	target, err := d.Clone.Clone(ctx, rep.PointInTime)
	rep.Copy = target.Name
	if err == nil {
		err = d.verify(ctx, target, &rep)
	}
	rep.Took = d.Clock.Now().Sub(start)
	var errs []error
	if err != nil {
		rep.Problem = err.Error()
		errs = append(errs, err)
	} else if rep.Took > DrillTarget {
		rep.Problem = fmt.Sprintf("the drill took %s, longer than the %s NFR-DR-02 allows", rep.Took.Round(time.Minute), DrillTarget)
	}
	if target.Name != "" {
		if rerr := d.Clone.Remove(context.WithoutCancel(ctx), target.Name); rerr != nil {
			errs = append(errs, fmt.Errorf("the drill's copy %s was not removed: %w", target.Name, rerr))
		} else {
			rep.Removed = true
		}
	}
	errs = append(errs, d.record(ctx, rep))
	if rep.Problem != "" && err == nil {
		errs = append(errs, errors.New(rep.Problem))
	}
	return rep, errors.Join(errs...)
}

// verify runs the ledger's and the audit chain's verifications on the copy (NFR-DR-03), through the code the nightly jobs
// run.
func (d Drill) verify(ctx context.Context, target ports.CloneTarget, rep *DrillReport) error {
	store, closeCopy, err := d.Open(ctx, target)
	if err != nil {
		return fmt.Errorf("open the drill's copy: %w", err)
	}
	defer closeCopy()
	ledgers, lerr := balance.VerifyAll(ctx, store)
	rep.Organisations = len(ledgers)
	for _, v := range ledgers {
		if v.Differences > 0 {
			rep.LedgerFailed++
		}
	}
	chains, cerr := audit.VerifyRestored(ctx, store, d.Blobs, d.Signer, d.Keyed)
	first := ""
	for _, c := range chains {
		rep.Ahead += c.Ahead
		if !c.OK() {
			rep.ChainsFailed++
			if first == "" {
				first = fmt.Sprintf("; organisation %s: %s", c.Organisation, c.Problem)
			}
		}
	}
	if lerr != nil || cerr != nil {
		return fmt.Errorf("on the copy of %s: %w%s", rep.PointInTime.Format(time.RFC3339), errors.Join(lerr, cerr), first)
	}
	return nil
}

// record writes the drill into the installation's trail; a deployment with no installation yet records nothing.
func (d Drill) record(ctx context.Context, rep DrillReport) error {
	if d.Installation == nil {
		return nil
	}
	org, err := d.Installation.InstallationOrganisation(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	status := "passed"
	if !rep.OK() {
		status = "failed"
	}
	write := func() error {
		return d.Store.Do(context.WithoutCancel(ctx), org, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionDrilled, ObjectType: "restore_drill", ObjectID: org,
				Changes: map[string][2]any{"status": {nil, status}, "copy": {nil, rep.Copy}, "point_in_time": {nil, rep.PointInTime.Format(time.RFC3339)},
					"took_seconds": {nil, int64(rep.Took.Seconds())}, "organisations": {nil, rep.Organisations}, "ledger_failed": {nil, rep.LedgerFailed},
					"chains_failed": {nil, rep.ChainsFailed}, "anchors_ahead": {nil, rep.Ahead}, "copy_removed": {nil, rep.Removed}, "problem": {nil, rep.Problem}}})
		})
	}
	// A copy made locally ends every connection to the database, this job's too: the second try connects again.
	if err := write(); err != nil {
		return write()
	}
	return nil
}
