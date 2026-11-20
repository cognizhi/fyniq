package authz

// Soft locks (FR-GL-09; p2-mobile-approvals-automation design D6). Locking is tightening, so it is preparing (C on "Period close / reopen": the Accountant and the
// Controller); releasing a lock is loosening, so it is approving (A: the Owner and the Controller), never less than locking. A pass lets one Accountant post into a
// soft-locked period for a few hours, with a reason; the handler requires the Accountant role itself, a second factor and a person's own session. None is on the AI or MCP
// surface: the AI column of the row is empty.
var (
	SoftLockPeriod         = Policy{Operation: "softLockPeriod", Capability: PeriodClose, Need: Create}
	UnlockPeriod           = Policy{Operation: "unlockPeriod", Capability: PeriodClose, Need: Approve, Note: "Releasing a soft lock loosens a control: it needs the approving right and a second factor."}
	LockPeriodModule       = Policy{Operation: "lockPeriodModule", Capability: PeriodClose, Need: Create}
	ReleasePeriodModule    = Policy{Operation: "releasePeriodModule", Capability: PeriodClose, Need: Approve, Note: "Releasing a module lock loosens a control: it needs the approving right and a second factor."}
	MakeSoftLockPass       = Policy{Operation: "makeSoftLockPass", Capability: PeriodClose, Need: Create, Note: "Only a person with the Accountant role, signed in to Fyniq and stepped up in the last 5 minutes (FR-GL-09)."}
	ListSoftLockExceptions = Policy{Operation: "listSoftLockExceptions", Capability: PeriodClose, Need: View}
)

func init() {
	for _, p := range []Policy{SoftLockPeriod, UnlockPeriod, LockPeriodModule, ReleasePeriodModule, MakeSoftLockPass, ListSoftLockExceptions} {
		if _, dup := Policies[p.Operation]; dup {
			panic("authz: two policies for " + p.Operation)
		}
		Policies[p.Operation] = p
	}
}
