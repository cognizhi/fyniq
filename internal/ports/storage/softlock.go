package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The modules a period can be soft-locked for, apart from the period as a whole (FR-GL-09).
const (
	ModuleAR      = "ar"
	ModuleAP      = "ap"
	ModuleBanking = "banking"
	ModulePayroll = "payroll"
)

// Modules lists the lockable modules.
var Modules = []string{ModuleAR, ModuleAP, ModuleBanking, ModulePayroll}

// ScopePeriod is the scope of an exception made in a period locked as a whole.
const ScopePeriod = "period"

// ModuleLock is whether one module of one period is locked.
type ModuleLock struct {
	ID        uuid.UUID
	PeriodID  uuid.UUID
	Module    string
	Locked    bool
	Version   int
	ChangedBy uuid.UUID
	ChangedAt time.Time
}

// SoftLockPass is a person's exception for a period: the Accountant who made it, with a reason, for at most eight hours.
type SoftLockPass struct {
	ID        uuid.UUID
	PeriodID  uuid.UUID
	UserID    uuid.UUID
	Reason    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// NewSoftLockPass is what SoftLocks.MakePass needs: the pass starts now by the store's clock, the same clock that decides whether it is live, and lasts Hours (1 to 8).
// CreatedAt and ExpiresAt, both set, name other times instead (a test of a pass that has ended).
type NewSoftLockPass struct {
	PeriodID  uuid.UUID
	UserID    uuid.UUID
	Reason    string
	Hours     int
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SoftLockException is one posting made under a pass, as the exceptions report lists it.
type SoftLockException struct {
	ID          uuid.UUID
	PassID      uuid.UUID
	PeriodID    uuid.UUID
	EntryID     uuid.UUID
	UserID      uuid.UUID
	Scope       string // ScopePeriod, or the locked module
	SourceType  string
	PostingDate time.Time
	Reason      string
	CreatedAt   time.Time
}

// SoftLocks are the soft locks of FR-GL-09: the weaker states the domain enforces while the database holds only the hard close (INV-03).
type SoftLocks interface {
	// OfDate returns the period that covers a date; false when none does (the posting then fails as it always did).
	OfDate(ctx context.Context, date time.Time) (Period, bool, error)
	// SoftLock moves an open period to soft-locked, and Unlock a soft-locked one back to open, each under the ledger lock first (ENG-47). CodePeriodState
	// when the period is in any other status.
	SoftLock(ctx context.Context, period uuid.UUID) (Period, error)
	Unlock(ctx context.Context, period uuid.UUID) (Period, error)
	// LockedModules are the modules locked for a period.
	LockedModules(ctx context.Context, period uuid.UUID) ([]string, error)
	// ModuleLocks are every module lock row of the organisation, for the periods' screen.
	ModuleLocks(ctx context.Context) ([]ModuleLock, error)
	// SetModule locks or releases a module of a period; audited.
	SetModule(ctx context.Context, period uuid.UUID, module string, locked bool, by uuid.UUID, at time.Time) (ModuleLock, error)
	// MakePass records a pass; audited. The database refuses a reason shorter than 10 characters and a pass longer than 8 hours (CodeInvalid).
	MakePass(ctx context.Context, n NewSoftLockPass) (SoftLockPass, error)
	// LivePass is the latest unexpired pass of a person for a period, by the store's clock; false when there is none.
	LivePass(ctx context.Context, period, user uuid.UUID) (SoftLockPass, bool, error)
	// RecordException keeps one posting made under a pass, stamped by the store's clock; audited.
	RecordException(ctx context.Context, e SoftLockException) (SoftLockException, error)
	// Exceptions are the postings made under passes, newest first, at most limit.
	Exceptions(ctx context.Context, limit int) ([]SoftLockException, error)
}
