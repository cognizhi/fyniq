package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ soft locks (FR-GL-09; p2-mobile-approvals-automation design D6)
//
// A soft lock is weaker than the hard close and the domain's to enforce: a soft-locked period, or a locked module of an open one, takes postings only from an Accountant who holds a
// pass for the period, made with a reason and a second factor, for at most eight hours; each such posting is on the exceptions report. Locking is the Accountant's or the
// Controller's; releasing a lock needs the approving right. None of it is on the AI or MCP surface, and no job, integration or agent can hold a pass.

// The pass's refusals (ENG-23).
const (
	CodePassNeedsAccountant = "LOCK-01" // only the Accountant role makes a pass
	CodeLockReason          = "LOCK-02" // the reason is too short
	CodePassPeriod          = "LOCK-03" // a pass for a period that is closed
)

// SoftLockCodes are the codes of the soft-lock operations.
var SoftLockCodes = []string{CodePassNeedsAccountant, CodeLockReason, CodePassPeriod, storage.CodePeriodState, storage.CodeSoftLocked}

// MinLockReason is the shortest reason a pass or a release takes.
const MinLockReason = 10

// SoftLockRequest says why a period is locked or unlocked.
type SoftLockRequest struct {
	Reason string `json:"reason,omitempty" maxLength:"500" doc:"Why, in your words: kept in the audit trail. Required (10 characters or more) to release a lock; optional to lock." example:"The October accounts are with the auditors."`
}

// ModuleLockRequest says why a module of a period is released.
type ModuleLockRequest = SoftLockRequest

// SoftLockPassRequest asks for a pass.
type SoftLockPassRequest struct {
	Reason string `json:"reason" minLength:"10" maxLength:"500" doc:"Why the Accountant must post into the locked period: kept with every posting made under the pass and shown on the exceptions report." example:"A late supplier bill dated 30 Oct that the auditor asked to see in October."`
	Hours  int    `json:"hours,omitempty" minimum:"1" maximum:"8" default:"4" doc:"How long the pass lasts, from 1 to 8 hours." example:"4"`
}

// SoftLockPass is a pass made.
type SoftLockPass struct {
	ID        string    `json:"id" format:"uuid" doc:"The pass's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9b01"`
	PeriodID  string    `json:"period_id" format:"uuid" doc:"The period it is for." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	Reason    string    `json:"reason" doc:"The reason given." example:"A late supplier bill dated 30 Oct that the auditor asked to see in October."`
	CreatedAt time.Time `json:"created_at" doc:"When it was made." example:"2026-11-21T09:00:00Z"`
	ExpiresAt time.Time `json:"expires_at" doc:"When it ends." example:"2026-11-21T13:00:00Z"`
}

// SoftLockException is a posting made under a pass.
type SoftLockException struct {
	ID          string    `json:"id" format:"uuid" doc:"The exception's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9c01"`
	PeriodID    string    `json:"period_id" format:"uuid" doc:"The locked period." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d61"`
	EntryID     string    `json:"entry_id" format:"uuid" doc:"The journal entry posted." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9d01"`
	UserID      string    `json:"user_id" format:"uuid" doc:"The Accountant who posted it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"`
	Scope       string    `json:"scope" enum:"period,ar,ap,banking,payroll" doc:"period when the whole period was soft-locked, else the locked module." example:"period"`
	SourceType  string    `json:"source_type" doc:"The kind of document it came from." example:"bill"`
	PostingDate string    `json:"posting_date" format:"date" doc:"The date it posted on." example:"2026-10-30"`
	Reason      string    `json:"reason" doc:"The pass's reason." example:"A late supplier bill dated 30 Oct that the auditor asked to see in October."`
	CreatedAt   time.Time `json:"created_at" doc:"When it was posted." example:"2026-11-21T09:05:00Z"`
}

// SoftLockExceptionPage is a page of exceptions, newest first.
type SoftLockExceptionPage struct {
	Items      []SoftLockException `json:"items" nullable:"false" doc:"The postings made under passes."`
	NextCursor string              `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

type (
	// ModulePath names a period and a lockable module.
	ModulePath struct {
		PeriodPath
		Module string `path:"module" enum:"ar,ap,banking,payroll" doc:"The module: ar (invoices, credit notes, receipts), ap (bills, payments, claims), banking or payroll." example:"ar"`
	}
	softLockInput struct {
		PeriodPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body SoftLockRequest
	}
	moduleLockInput struct {
		ModulePath
		KeyHeader
		PreferHeader
		Body ModuleLockRequest
	}
	softLockPassInput struct {
		PeriodPath
		KeyHeader
		PreferHeader
		Body SoftLockPassRequest
	}
	softLockPassOutput struct {
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     SoftLockPass
	}
	softLockExceptionListOutput struct{ Body SoftLockExceptionPage }
)

// lockedModules are each period's locked modules.
func lockedModules(ctx context.Context, u storage.UnitOfWork) (map[uuid.UUID][]string, error) {
	all, err := u.SoftLocks().ModuleLocks(ctx)
	out := map[uuid.UUID][]string{}
	for _, l := range all {
		if l.Locked {
			out[l.PeriodID] = append(out[l.PeriodID], l.Module)
		}
	}
	return out, err
}

// periodState is a period with its ETag and the modules locked for it.
func periodState(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (Period, error) {
	x, err := u.Periods().Get(ctx, id)
	if err != nil {
		return Period{}, err
	}
	cal, err := calendar(ctx, u)
	if err != nil {
		return Period{}, err
	}
	locks, err := lockedModules(ctx, u)
	out := periodOf(x, cal)
	out.LockedModules = locks[id]
	return out, err
}

type periodWriteOutput struct {
	ETag     string `header:"ETag" doc:"The period's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Period
}

func lockReason(reason string, required bool) (string, error) {
	r := strings.TrimSpace(reason)
	if len(r) < MinLockReason && (required || r != "") {
		return "", problem(CodeLockReason, "The reason is too short: say why in at least 10 characters.", "Write the reason as you would to the auditor",
			FieldProblem{Code: CodeLockReason, Field: "body.reason", Message: "is shorter than 10 characters", Fix: "Say why in at least 10 characters"})
	}
	return r, nil
}

func registerSoftLocks(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	people := []string{CodeSessionRequired, CodeStepUpRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}

	moveOp := func(ctx context.Context, in *softLockInput, what string, move func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Period, error), reasonRequired, stepUp bool) (*periodWriteOutput, error) {
		id, err := parseID("period_id", in.PeriodID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		reason, err := lockReason(in.Body.Reason, reasonRequired)
		if err != nil {
			return nil, err
		}
		if stepUp {
			if _, err := a.steppedUpPerson(ctx, what); err != nil {
				return nil, err
			}
		} else if _, err := signedIn(ctx, what); err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Period, error) {
			before, err := u.Periods().Get(ctx, id)
			if err != nil {
				return Period{}, err
			}
			cal, err := calendar(ctx, u)
			if err != nil {
				return Period{}, err
			}
			if err := checkIfMatch(in.IfMatch, periodETag(periodOf(before, cal))); err != nil {
				return Period{}, err
			}
			if reason != "" {
				ctx = storage.WithReason(ctx, reason)
			}
			if _, err := move(ctx, u, id); err != nil {
				return Period{}, err
			}
			return periodState(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		out := &periodWriteOutput{Applied: applied(w), Body: w.Body}
		if !w.Previewed {
			out.ETag, out.Replayed = periodETag(w.Body), replayedHeader(w.Replayed)
		}
		return out, nil
	}

	register(a, authz.SoftLockPeriod, huma.Operation{
		OperationID: "softLockPeriod", Method: http.MethodPost, Path: "/v1/periods/{period_id}/soft-lock", Tags: []string{"Periods"},
		Summary: "Soft-lock an open period",
		Description: "Moves an open period to soft-locked (FR-GL-09): from then on only an Accountant who holds a pass for the period, made with a reason, can post into it, and every such posting " +
			"is on the exceptions report. It is weaker than the close: the hard lock of INV-03 is the database's and is not changed, and a soft-locked period is still closed in the usual way. The " +
			"reason is optional. Needs If-Match with the period's ETag and a person signed in to Fyniq. Side effects: the status and an audit event. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview checks it and locks nothing. Not on the AI or MCP surface.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeInvalid}, people...), append(SoftLockCodes, locks...)...),
		func(ctx context.Context, in *softLockInput) (*periodWriteOutput, error) {
			return moveOp(ctx, in, "Soft-locking a period", func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Period, error) {
				return u.SoftLocks().SoftLock(ctx, id)
			}, false, false)
		})

	register(a, authz.UnlockPeriod, huma.Operation{
		OperationID: "unlockPeriod", Method: http.MethodPost, Path: "/v1/periods/{period_id}/unlock", Tags: []string{"Periods"},
		Summary: "Release the soft lock of a period",
		Description: "Moves a soft-locked period back to open (FR-GL-09). It loosens a control, so it needs the approving right (an Owner or a Controller), a second factor in the last 5 minutes and a " +
			"reason of at least 10 characters, which is kept in the audit trail. A closed period is reopened only through its own approved request (FR-GL-08), never here. Needs If-Match with the " +
			"period's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and changes nothing. Not on the AI or MCP surface.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeInvalid}, people...), append(SoftLockCodes, locks...)...),
		func(ctx context.Context, in *softLockInput) (*periodWriteOutput, error) {
			return moveOp(ctx, in, "Releasing a soft lock", func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Period, error) {
				return u.SoftLocks().Unlock(ctx, id)
			}, true, true)
		})

	moduleOp := func(ctx context.Context, in *moduleLockInput, what string, lock bool) (*periodWriteOutput, error) {
		id, err := parseID("period_id", in.PeriodID)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(storage.Modules, in.Module) {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "path.module", Message: "is not a module that can be locked", Fix: "Use ar, ap, banking or payroll"}})
		}
		reason, err := lockReason(in.Body.Reason, !lock)
		if err != nil {
			return nil, err
		}
		var p authn.Principal
		if lock {
			p, err = signedIn(ctx, what)
		} else {
			p, err = a.steppedUpPerson(ctx, what)
		}
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Period, error) {
			if reason != "" {
				ctx = storage.WithReason(ctx, reason)
			}
			if _, err := u.SoftLocks().SetModule(ctx, id, in.Module, lock, p.UserID, a.deps.Clock.Now()); err != nil {
				if errors.Is(err, ports.ErrNotFound) {
					return Period{}, problem(CodeNotFound, "No period with that ID exists.", "List the periods to find their IDs")
				}
				return Period{}, err
			}
			return periodState(ctx, u, id)
		})
		if err != nil {
			return nil, err
		}
		out := &periodWriteOutput{Applied: applied(w), Body: w.Body}
		if !w.Previewed {
			out.ETag, out.Replayed = periodETag(w.Body), replayedHeader(w.Replayed)
		}
		return out, nil
	}

	register(a, authz.LockPeriodModule, huma.Operation{
		OperationID: "lockPeriodModule", Method: http.MethodPost, Path: "/v1/periods/{period_id}/modules/{module}/lock", Tags: []string{"Periods"},
		Summary: "Lock one module of a period",
		Description: "Locks one module (ar, ap, banking or payroll) of a period that is not closed (FR-GL-09): its postings into the period are refused unless the person holds a pass, while the " +
			"other modules stay open, as when sales are final and purchases are not. Which documents belong to which module is in the user guide. Needs a person signed in to Fyniq. " +
			"Side effects: the lock and an audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and locks nothing. Not on the AI or MCP surface.",
	}, append(append([]string{CodeNotFound, CodeInvalid}, people...), append(SoftLockCodes, locks...)...),
		func(ctx context.Context, in *moduleLockInput) (*periodWriteOutput, error) {
			return moduleOp(ctx, in, "Locking a module", true)
		})

	register(a, authz.ReleasePeriodModule, huma.Operation{
		OperationID: "releasePeriodModule", Method: http.MethodPost, Path: "/v1/periods/{period_id}/modules/{module}/release", Tags: []string{"Periods"},
		Summary: "Release the lock of one module of a period",
		Description: "Releases one module's lock (FR-GL-09). It loosens a control, so it needs the approving right (an Owner or a Controller), a second factor in the last 5 minutes and a reason of at " +
			"least 10 characters, kept in the audit trail. Idempotent with Idempotency-Key; Prefer: return=preview checks it and changes nothing. Not on the AI or MCP surface.",
	}, append(append([]string{CodeNotFound, CodeInvalid}, people...), append(SoftLockCodes, locks...)...),
		func(ctx context.Context, in *moduleLockInput) (*periodWriteOutput, error) {
			return moduleOp(ctx, in, "Releasing a module lock", false)
		})

	register(a, authz.MakeSoftLockPass, huma.Operation{
		OperationID: "makeSoftLockPass", Method: http.MethodPost, Path: "/v1/periods/{period_id}/soft-lock-passes", Tags: []string{"Periods"}, DefaultStatus: http.StatusCreated,
		Summary: "Make a pass to post into a locked period",
		Description: "Lets the Accountant who asks post into a soft-locked period, or a locked module of one, for 1 to 8 hours (default 4), with a reason of at least 10 characters (FR-GL-09): `{ \"reason\": " +
			"\"A late supplier bill dated 30 Oct that the auditor asked to see in October.\", \"hours\": 4 }`. Only a person with the Accountant role, signed in to Fyniq and stepped up in the last 5 " +
			"minutes, can make one, and it is theirs alone: no job, integration, agent or AI proposal holds a pass, whatever the person's role. Every posting made under it is on the exceptions " +
			"report with the reason. The hard close is not affected: a closed period takes nothing. Idempotent with Idempotency-Key; Prefer: return=preview checks it and makes nothing. " +
			"Not on the AI or MCP surface.",
	}, append(append([]string{CodeNotFound, CodeInvalid}, people...), append(SoftLockCodes, locks...)...),
		func(ctx context.Context, in *softLockPassInput) (*softLockPassOutput, error) {
			id, err := parseID("period_id", in.PeriodID)
			if err != nil {
				return nil, err
			}
			p, err := a.steppedUpPerson(ctx, "Making a pass")
			if err != nil {
				return nil, err
			}
			reason, err := lockReason(in.Body.Reason, true)
			if err != nil {
				return nil, err
			}
			hours := in.Body.Hours
			if hours == 0 {
				hours = 4
			}
			if hours < 1 || hours > 8 {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.hours", Message: "is not from 1 to 8", Fix: "Ask for a pass of 1 to 8 hours"}})
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (SoftLockPass, error) {
				before, err := u.Periods().Get(ctx, id)
				if err != nil {
					return SoftLockPass{}, err
				}
				if !slices.Contains(p.Roles, authz.Accountant) { // after the period is found, so the answer to a period that is not there is the same for every role
					return SoftLockPass{}, problem(CodePassNeedsAccountant, "Only a member with the Accountant role can make a pass to post into a locked period.",
						"Ask an Accountant to make it, or ask an Owner or Controller to release the lock")
				}
				if before.Status == storage.PeriodClosed {
					return SoftLockPass{}, problem(CodePassPeriod, "The period is closed, and a pass does not open a closed period.", "Ask for the period to be reopened (FR-GL-08)")
				}
				x, err := u.SoftLocks().MakePass(ctx, storage.NewSoftLockPass{PeriodID: id, UserID: p.UserID, Reason: reason, Hours: hours})
				if err != nil {
					return SoftLockPass{}, err
				}
				return SoftLockPass{ID: x.ID.String(), PeriodID: x.PeriodID.String(), Reason: x.Reason, CreatedAt: x.CreatedAt.UTC(), ExpiresAt: x.ExpiresAt.UTC()}, nil
			})
			if err != nil {
				return nil, err
			}
			return &softLockPassOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.ListSoftLockExceptions, huma.Operation{
		OperationID: "listSoftLockExceptions", Method: http.MethodGet, Path: "/v1/soft-lock-exceptions", Tags: []string{"Periods"},
		Summary: "List the postings made into locked periods",
		Description: "The exceptions report of FR-GL-09: every posting an Accountant made into a soft-locked period, or a locked module, under a pass, newest first, a page at a time (up to the last 1,000), " +
			"each with the period, the entry, who posted it, what scope was locked, the document's kind, the posting date and the reason. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *PageQuery) (*softLockExceptionListOutput, error) {
		p, _ := authn.From(ctx)
		out := SoftLockExceptionPage{Items: []SoftLockException{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.SoftLocks().Exceptions(ctx, 1000)
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(e storage.SoftLockException) string { return e.ID.String() }, *in)
			for _, e := range page {
				out.Items = append(out.Items, SoftLockException{ID: e.ID.String(), PeriodID: e.PeriodID.String(), EntryID: e.EntryID.String(), UserID: e.UserID.String(),
					Scope: e.Scope, SourceType: e.SourceType, PostingDate: e.PostingDate.Format(time.DateOnly), Reason: e.Reason, CreatedAt: e.CreatedAt.UTC()})
			}
			out.NextCursor = next
			return err
		})
		return &softLockExceptionListOutput{Body: out}, err
	})
}
