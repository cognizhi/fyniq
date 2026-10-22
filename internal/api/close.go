package api

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the period close (FR-GL-07, FR-GL-08, FM-4, AUD-04)

// CloseCodes are the codes of the close.
var CloseCodes = []string{closing.CodeNotOpen, closing.CodeStale, closing.CodeOrder, closing.CodeNotClean, closing.CodeSelfAttest, closing.CodeWrongCheck,
	closing.CodeNotClosed, closing.CodeLater}

// CloseCheckItem is one close check's outcome.
type CloseCheckItem struct {
	Key      string `json:"key" doc:"Which check: earlier, balances, trial-balance, drafts, reversals, recurring, recurring-content, variances, checklist or opening; more join as their modules arrive." example:"drafts"`
	Severity string `json:"severity" enum:"ok,warning,error" doc:"ok; warning (⚠) lets the close go on; error (✖) refuses it." example:"warning"`
	Title    string `json:"title" doc:"What the check found." example:"Draft journals are dated in the period"`
	Detail   string `json:"detail,omitempty" doc:"More about it, and what to do." example:"2 draft journal(s): once the period closes they cannot post on those dates"`
}

// CloseChecks is a run of the close checks: who ran them, the ledger revision they saw, and what they found.
type CloseChecks struct {
	ID             string           `json:"id" format:"uuid" doc:"The run's ID, which the close names." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4da0"`
	CheckedBy      *Person          `json:"checked_by,omitempty" doc:"Who ran them: the close's preparer."`
	CheckedAt      time.Time        `json:"checked_at" doc:"When." example:"2026-11-03T09:30:00Z"`
	LedgerRevision int64            `json:"ledger_revision" doc:"The ledger revision they saw (ENG-46)." example:"412"`
	Clean          bool             `json:"clean" doc:"No check refused the close." example:"true"`
	Stale          bool             `json:"stale" doc:"An entry dated in or before the period posted after they ran: run them again before the close (FR-GL-07)." example:"false"`
	Checks         []CloseCheckItem `json:"checks" nullable:"false"`
}

// PeriodCloseRecord is a close: who attested it, when, the revision at the close and the snapshot's SHA-256.
type PeriodCloseRecord struct {
	ID             string    `json:"id" format:"uuid" doc:"The close's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4db0"`
	ChecksID       string    `json:"checks_id" format:"uuid" doc:"The run of the checks it rests on." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4da0"`
	AttestedBy     *Person   `json:"attested_by,omitempty" doc:"Who attested and closed it: never the one who ran the checks (FM-4)."`
	AttestedAt     time.Time `json:"attested_at" doc:"When." example:"2026-11-03T10:00:00Z"`
	Statement      string    `json:"statement" doc:"What they attested." example:"I have reviewed October 2026's close and its checks."`
	ClosedRevision int64     `json:"closed_revision" doc:"The ledger revision at the close (AUD-04)." example:"412"`
	SnapshotSHA256 string    `json:"snapshot_sha256" doc:"The SHA-256 of the trial balance at the period end, as canonical JSON (AUD-04)." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
}

// PeriodReopenRecord is a reopening: who approved it, when, why, and the snapshots before and after (FR-GL-08).
type PeriodReopenRecord struct {
	ID               string    `json:"id" format:"uuid" doc:"The reopening's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dc0"`
	ApprovalID       string    `json:"approval_id" format:"uuid" doc:"The approved request (FM-1)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd0"`
	ReopenedBy       *Person   `json:"reopened_by,omitempty" doc:"The final approver."`
	ReopenedAt       time.Time `json:"reopened_at" doc:"When." example:"2026-11-05T09:00:00Z"`
	Reason           string    `json:"reason" doc:"Why." example:"A supplier's October invoice arrived late"`
	ReopenedRevision int64     `json:"reopened_revision" doc:"The ledger revision when it reopened." example:"415"`
	BeforeSHA256     string    `json:"before_sha256" doc:"The close's snapshot (AUD-04)." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	AfterSHA256      string    `json:"after_sha256" doc:"The trial balance at the period end when it reopened." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
}

// PeriodCloseState is a period's close: the period, its latest checks and its latest close.
type PeriodCloseState struct {
	Period       Period              `json:"period"`
	Checks       *CloseChecks        `json:"checks,omitempty" doc:"The latest run of the checks, while the period is open; absent before the first."`
	Close        *PeriodCloseRecord  `json:"close,omitempty" doc:"The latest close; absent while it was never closed."`
	YouMayAttest bool                `json:"you_may_attest" doc:"The latest checks are clean and current and someone else ran them, so the caller may attest, if their role closes periods." example:"true"`
	Reopen       *PeriodReopenRecord `json:"reopen,omitempty" doc:"The latest reopening; absent while it was never reopened."`
	ReopenAsked  string              `json:"reopen_asked,omitempty" format:"uuid" doc:"The pending request to reopen it, while one waits for approval." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4dd0"`
}

type closeStateOutput struct {
	ETag string `header:"ETag" doc:"The period's version, for If-Match."`
	Body PeriodCloseState
}

type runChecksInput struct {
	PeriodPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

// CloseRequest attests a period's close on a run of its checks.
type CloseRequest struct {
	ChecksID  string `json:"checks_id" format:"uuid" doc:"The run of the checks attested: the latest, clean and current, run by someone else." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4da0"`
	Statement string `json:"statement" minLength:"1" maxLength:"1000" doc:"What the attester attests, in their words or the page's." example:"I have reviewed October 2026's close and its checks, and the books for the month are complete as far as I know."`
}

type closePeriodInput struct {
	PeriodPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body CloseRequest
}

type closeWriteOutput struct {
	ETag     string `header:"ETag" doc:"The period's version after the command, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     PeriodCloseState
}

// closeState reads a period's close as the caller sees it.
func (a *API) closeState(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (PeriodCloseState, error) {
	p, err := u.Periods().Get(ctx, id)
	if err != nil {
		return PeriodCloseState{}, err
	}
	cal, err := calendar(ctx, u)
	if err != nil {
		return PeriodCloseState{}, err
	}
	out := PeriodCloseState{Period: periodOf(p, cal)}
	var people []*Person
	if p.Status == storage.PeriodOpen {
		c, err := u.Closes().LatestCheck(ctx, p.ID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
		case err != nil:
			return PeriodCloseState{}, err
		default:
			checks, err := closing.Read(c.Result)
			if err != nil {
				return PeriodCloseState{}, err
			}
			stale, err := u.Closes().ChangedSince(ctx, p.EndsOn, c.LedgerRevision)
			if err != nil {
				return PeriodCloseState{}, err
			}
			out.Checks = &CloseChecks{ID: c.ID.String(), CheckedBy: personOf(c.CheckedBy), CheckedAt: c.CheckedAt.UTC(), LedgerRevision: c.LedgerRevision,
				Clean: c.Clean, Stale: stale, Checks: make([]CloseCheckItem, 0, len(checks))}
			for _, x := range checks {
				out.Checks.Checks = append(out.Checks.Checks, CloseCheckItem{Key: x.Key, Severity: string(x.Severity), Title: x.Title, Detail: x.Detail})
			}
			me := storage.AuditFrom(ctx).Actor
			out.YouMayAttest = c.Clean && !stale && (me == nil || me.String() != c.CheckedBy)
			people = append(people, out.Checks.CheckedBy)
		}
	}
	cl, err := u.Closes().Latest(ctx, p.ID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
	case err != nil:
		return PeriodCloseState{}, err
	default:
		out.Close = &PeriodCloseRecord{ID: cl.ID.String(), ChecksID: cl.CheckID.String(), AttestedBy: personOf(cl.AttestedBy), AttestedAt: cl.AttestedAt.UTC(),
			Statement: cl.Statement, ClosedRevision: cl.ClosedRevision, SnapshotSHA256: hex.EncodeToString(cl.SnapshotHash)}
		people = append(people, out.Close.AttestedBy)
	}
	ro, err := u.Closes().LatestReopen(ctx, p.ID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
	case err != nil:
		return PeriodCloseState{}, err
	default:
		out.Reopen = &PeriodReopenRecord{ID: ro.ID.String(), ApprovalID: ro.ApprovalID.String(), ReopenedBy: personOf(ro.ReopenedBy), ReopenedAt: ro.ReopenedAt.UTC(),
			Reason: ro.Reason, ReopenedRevision: ro.ReopenedRevision, BeforeSHA256: hex.EncodeToString(ro.BeforeHash), AfterSHA256: hex.EncodeToString(ro.AfterHash)}
		people = append(people, out.Reopen.ReopenedBy)
	}
	if p.Status == storage.PeriodClosed {
		pending, err := u.Approvals().List(ctx, string(approval.Pending))
		if err != nil {
			return PeriodCloseState{}, err
		}
		now := a.deps.Clock.Now()
		for _, r := range pending {
			if r.ObjectType == approval.ObjectPeriod && r.ObjectKey == p.ID.String() && approval.StatusAt(requestOf(r), now) == approval.Pending {
				out.ReopenAsked = r.ID.String()
			}
		}
	}
	return out, a.name(ctx, u, people...)
}

// ReopenRequest asks to reopen a closed period.
type ReopenRequest struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why the period reopens: the audit trail and the reopening keep it." example:"A supplier's October invoice arrived late"`
}

type reopenInput struct {
	PeriodPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ReopenRequest
}

type reopenOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Approval
}

func registerClose(a *API) {
	register(a, authz.GetPeriodClose, huma.Operation{
		OperationID: "getPeriodClose", Method: http.MethodGet, Path: "/v1/periods/{period_id}/close", Tags: []string{"Periods"},
		Summary: "Get a period's close",
		Description: "A period's close (FR-GL-07, FM-4): while it is open, the latest run of its close checks, with who ran them, the " +
			"ledger revision they saw and whether an entry dated in or before the period posted since (stale); once closed, who " +
			"attested it, the revision at the close and the SHA-256 of its trial-balance snapshot (AUD-04). With the period's ETag. " +
			"Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *PeriodPath) (*closeStateOutput, error) {
		id, err := parseID("period_id", in.PeriodID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out closeStateOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out.Body, err = a.closeState(ctx, u, id)
			out.ETag = periodETag(out.Body.Period)
			return err
		})
		return &out, err
	})

	register(a, authz.RunCloseChecks, huma.Operation{
		OperationID: "runCloseChecks", Method: http.MethodPost, Path: "/v1/periods/{period_id}/close-checks", Tags: []string{"Periods"},
		Summary: "Run a period's close checks",
		Description: "Runs the close checks of an open period and records them with the ledger revision they saw and who ran them: the " +
			"close's preparer (FR-GL-07; PRD-001 rev 0.50, provisional). Until the checks catalogue arrives they are: every earlier " +
			"period closed (✖), the balances agreeing with the ledger (✖), the trial balance at the period end netting to zero (✖), " +
			"draft journals dated in the period (⚠), automatic reversals not posted (⚠), recurring journals without their drafts (⚠, or ✖ " +
			"when the scheduler failed) or with content the posting rules refuse (⚠), variances against the previous month above both " +
			"thresholds with no explanation of their totals as they are (✖, FR-GL-14; listPeriodVariances), checklist tasks neither done " +
			"nor skipped with a reason (✖, FR-GL-07; getPeriodChecklist) and, in the period the books " +
			"start in, opening balances not posted (⚠). " +
			"Side effects: the record of the run and its audit event. Needs If-Match with the period's ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview runs them and records nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeRetry,
		storage.CodeLockTimeout}, CloseCodes...),
		func(ctx context.Context, in *runChecksInput) (*closeWriteOutput, error) {
			id, err := parseID("period_id", in.PeriodID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PeriodCloseState, error) {
				before, err := a.closeState(ctx, u, id)
				if err != nil {
					return PeriodCloseState{}, err
				}
				if err := checkIfMatch(in.IfMatch, periodETag(before.Period)); err != nil {
					return PeriodCloseState{}, err
				}
				if _, err := a.closing.RunChecks(ctx, p.Organisation, id); err != nil {
					return PeriodCloseState{}, err
				}
				return a.closeState(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			out := &closeWriteOutput{Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.ETag, out.Replayed = periodETag(w.Body.Period), replayedHeader(w.Replayed)
			}
			return out, nil
		})

	register(a, authz.ClosePeriod, huma.Operation{
		OperationID: "closePeriod", Method: http.MethodPost, Path: "/v1/periods/{period_id}/close", Tags: []string{"Periods"},
		Summary: "Attest and close a period",
		Description: "Attests a period's close on its latest checks and closes it, in one transaction (FR-GL-07, FR-GL-08, FM-4; " +
			"PRD-001 rev 0.50, provisional). The attester is a person who passed step-up in the last 5 minutes and did not run " +
			"the checks, in every team mode; the database refuses otherwise (FY153). The checks must be clean and of this period, " +
			"every earlier period closed, and, under the ledger lock, no entry dated on or before the period end may have posted " +
			"after the checks ran: a back-dated posting and the close never both win (ENG-47, G4-053). Side effects: the period's " +
			"hard lock, which the database enforces on every posting (INV-03); the close record with the trial balance at the period " +
			"end as canonical JSON and its SHA-256 (AUD-04); audit events. Needs If-Match with the period's ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and closes nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired, storage.CodePeriodState, storage.CodeClose, storage.CodeRetry,
		storage.CodeLockTimeout}, CloseCodes...),
		func(ctx context.Context, in *closePeriodInput) (*closeWriteOutput, error) {
			id, err := parseID("period_id", in.PeriodID)
			if err != nil {
				return nil, err
			}
			checks, err := parseID("checks_id", in.Body.ChecksID)
			if err != nil {
				return nil, err
			}
			statement := strings.TrimSpace(in.Body.Statement)
			if statement == "" {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.statement", Message: "is empty", Fix: "Say what you attest"}})
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, err := a.steppedUpPerson(ctx, "Attesting a close")
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PeriodCloseState, error) {
				before, err := u.Periods().Get(ctx, id)
				if err != nil {
					return PeriodCloseState{}, err
				}
				cal, err := calendar(ctx, u)
				if err != nil {
					return PeriodCloseState{}, err
				}
				if err := checkIfMatch(in.IfMatch, periodETag(periodOf(before, cal))); err != nil {
					return PeriodCloseState{}, err
				}
				if _, _, err := a.closing.Close(ctx, p.Organisation, id, checks, statement); err != nil {
					return PeriodCloseState{}, err
				}
				return a.closeState(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			out := &closeWriteOutput{Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.ETag, out.Replayed = periodETag(w.Body.Period), replayedHeader(w.Replayed)
			}
			return out, nil
		})

	register(a, authz.RequestPeriodReopen, huma.Operation{
		OperationID: "requestPeriodReopen", Method: http.MethodPost, Path: "/v1/periods/{period_id}/reopen", Tags: []string{"Periods"},
		Summary: "Ask to reopen a closed period", DefaultStatus: http.StatusAccepted,
		Description: "Asks to reopen the latest closed period, with a reason (FR-GL-08, FM-1; PRD-001 rev 0.51, provisional). It never " +
			"reopens at once: the answer is 202 with an approval request, which a person who holds A on \"Period close / reopen\" (the " +
			"Owner or the Controller) and did not ask approves, stepped up. On the approval, under the ledger lock, the period still " +
			"has to be the latest closed one; it reopens, and the reopening is recorded with the reason, the approval, and the " +
			"snapshots before (the close's) and after (the trial balance at the period end), whose SHA-256 the database recomputes. " +
			"Asking needs a person signed in who passed step-up. Needs If-Match with the period's ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval, storage.CodeRetry,
		storage.CodeLockTimeout},
		CloseCodes...),
		func(ctx context.Context, in *reopenInput) (*reopenOutput, error) {
			id, err := parseID("period_id", in.PeriodID)
			if err != nil {
				return nil, err
			}
			reason := strings.TrimSpace(in.Body.Reason)
			if reason == "" {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "is empty", Fix: "Say why the period reopens"}})
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				p, c, err := closing.Reopenable(ctx, u, id)
				if err != nil {
					return Approval{}, err
				}
				cal, err := calendar(ctx, u)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, periodETag(periodOf(p, cal))); err != nil {
					return Approval{}, err
				}
				month := p.StartsOn.Format("January 2006")
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectPeriod, ObjectKey: p.ID.String(), Operation: approval.OpPeriodReopen,
					Summary: fmt.Sprintf("Reopen %s: %s", month, reason),
					Execute: map[string]any{"period_id": p.ID.String(), "close_id": c.ID.String(), "reason": reason},
					Diff:    []ApprovalDiff{{Field: "status", Before: "closed", After: "open"}}})
			})
			if err != nil {
				return nil, err
			}
			return &reopenOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
