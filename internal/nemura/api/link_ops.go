package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// The link's health, its repair and its restore, and a run's receipt (PRD-002 NM-INT-04, 10, 13; p1-nemura-fyniq-link 5.1, 5.2). Every one is by identity: the events' IDs, sequence numbers, types, objects,
// versions and states, never a payload, a figure or an account.

// LinkEventView is an event Nemura queued for Fyniq, by identity.
type LinkEventView struct {
	EventID    string `json:"event_id" format:"uuid" doc:"The event's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Sequence   int64  `json:"sequence" doc:"The link's running number." example:"41"`
	Type       string `json:"type" doc:"The event type." example:"people.person.upserted"`
	ObjectType string `json:"object_type" doc:"The kind of object it is about." example:"person"`
	ObjectID   string `json:"object_id" doc:"The object's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Version    int    `json:"object_version" doc:"The object's version." example:"2"`
	State      string `json:"state" enum:"queued,sent,failed,refused" doc:"queued: waiting; sent: Fyniq took it; failed: not delivered yet, tried again; refused: Fyniq refused it for good." example:"sent"`
	Code       string `json:"code,omitempty" doc:"The stable code of the last failure or refusal." example:"PPL-19"`
	Attempts   int    `json:"attempts" doc:"How many times it was tried." example:"1"`
	QueuedAt   string `json:"queued_at" format:"date-time" doc:"When it was queued." example:"2026-11-25T09:00:00Z"`
}

// LinkRestoreRun is a run a restore holds as unknown.
type LinkRestoreRun struct {
	RunID     string `json:"run_id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Version   int    `json:"version" doc:"The version approved with proof." example:"2"`
	Outcome   string `json:"outcome,omitempty" enum:"posted,not_posted," doc:"Empty until decided against Fyniq's own record." example:"posted"`
	Reference string `json:"reference,omitempty" doc:"Fyniq's posting reference, for a run decided as posted." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90"`
}

// LinkRestoreView is a restore of Nemura's database that is not reconciled yet.
type LinkRestoreView struct {
	ID                 string           `json:"id" format:"uuid" doc:"The restore." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e11"`
	RecordedAt         string           `json:"recorded_at" format:"date-time" doc:"When the deployment's recovery recorded it." example:"2026-11-25T09:00:00Z"`
	RecordedBy         string           `json:"recorded_by" doc:"The two operators who ran the recovery." example:"Ana Tan, Ben Lim"`
	LastSequenceQueued int64            `json:"last_sequence_queued" doc:"The highest sequence number the restored database knows it queued." example:"41"`
	LastSequenceSent   int64            `json:"last_sequence_sent" doc:"The highest it knows it sent." example:"40"`
	LastFyniqSequence  int64            `json:"last_fyniq_sequence" doc:"The highest Fyniq sequence number it received." example:"12"`
	State              string           `json:"state" enum:"open,reconciled" doc:"open: the link is held until every run below is decided." example:"open"`
	Runs               []LinkRestoreRun `json:"runs" doc:"The runs approved with proof whose answer the restored database does not hold."`
}

// LinkHealthView is the link's health (NM-INT-04).
type LinkHealthView struct {
	State              string           `json:"state" enum:"none,proposed,active,suspended,unlinking" doc:"The link's state." example:"active"`
	Epoch              int              `json:"epoch,omitempty" doc:"The link's epoch." example:"1"`
	Queued             int              `json:"queued" doc:"Events waiting to be sent." example:"0"`
	Failed             int              `json:"failed" doc:"Events not delivered yet, tried again." example:"0"`
	Sent               int              `json:"sent" doc:"Events Fyniq took." example:"120"`
	Refused            int              `json:"refused" doc:"Events Fyniq refused for good." example:"0"`
	OldestWaiting      *string          `json:"oldest_waiting,omitempty" format:"date-time" doc:"When the oldest event not yet sent was queued." example:"2026-11-25T09:00:00Z"`
	LastSent           *string          `json:"last_sent,omitempty" format:"date-time" doc:"When an event was last sent." example:"2026-11-25T09:00:01Z"`
	FailingSince       *string          `json:"failing_since,omitempty" format:"date-time" doc:"When the sync began failing: the oldest waiting event, when nothing was sent since." example:"2026-11-24T09:00:00Z"`
	FailingOver24Hours bool             `json:"failing_over_24_hours" doc:"True when the sync has failed for 24 hours (a warning on Checks, N.1 #7)." example:"false"`
	LastSequenceQueued int64            `json:"last_sequence_queued" doc:"The checkpoint: the highest sequence number queued." example:"41"`
	LastSequenceSent   int64            `json:"last_sequence_sent" doc:"The highest sent." example:"41"`
	LastFyniqSequence  int64            `json:"last_fyniq_sequence" doc:"The highest Fyniq sequence number received." example:"12"`
	Undelivered        []LinkEventView  `json:"undelivered" doc:"The newest events that failed or were refused, at most 50."`
	Restore            *LinkRestoreView `json:"restore,omitempty" doc:"A restore not reconciled yet: the link is held."`
}

// ReceiptApproval is the proof kept for a version of a run.
type ReceiptApproval struct {
	Version        int    `json:"version" doc:"The version." example:"2"`
	EnvelopeSHA256 string `json:"envelope_sha256" doc:"The digest of the figures the approver signed." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	MappingVersion int    `json:"mapping_version" doc:"Fyniq's mapping version it was calculated against." example:"3"`
	ApprovedAt     string `json:"approved_at" format:"date-time" doc:"When it was approved." example:"2026-11-25T09:00:00Z"`
}

// ReceiptSubmission is one sending of a version.
type ReceiptSubmission struct {
	EventID   string  `json:"event_id" format:"uuid" doc:"The event." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Sequence  int64   `json:"sequence" doc:"The link's running number." example:"41"`
	Version   int     `json:"version" doc:"The version sent." example:"2"`
	State     string  `json:"state" enum:"queued,sent,failed,refused" doc:"Where it stands." example:"sent"`
	Code      string  `json:"code,omitempty" doc:"The stable code of a failure or refusal." example:"PPL-19"`
	SettledAt *string `json:"settled_at,omitempty" format:"date-time" doc:"When it was sent or refused." example:"2026-11-25T09:00:01Z"`
}

// ReceiptPayment is a payment instruction Fyniq reported, or a recorded outcome.
type ReceiptPayment struct {
	PersonID      string `json:"person_id" format:"uuid" doc:"The person paid." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	InstructionID string `json:"instruction_id,omitempty" doc:"The bank instruction's ID." example:"INSTR-1"`
	State         string `json:"state" doc:"The state (NM-PAY-10)." example:"paid"`
	Source        string `json:"source" enum:"fyniq,manual" doc:"Where the state came from." example:"fyniq"`
	RecordedAt    string `json:"recorded_at" format:"date-time" doc:"When it was recorded." example:"2026-11-25T09:00:00Z"`
}

// RunReceiptView is what Nemura kept of a run's life with Fyniq.
type RunReceiptView struct {
	RunID       string              `json:"run_id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Approvals   []ReceiptApproval   `json:"approvals" doc:"The proof kept for each version approved while linked."`
	Submissions []ReceiptSubmission `json:"submissions" doc:"Each sending of an approved version."`
	Postings    []RunPosting        `json:"postings" doc:"Fyniq's answers."`
	Payments    []ReceiptPayment    `json:"payments" doc:"The payment instructions and outcomes, without an amount or a name."`
}

// LinkOps is the link's health, repair and restore, and a run's receipt.
type LinkOps interface {
	Health(ctx context.Context, s Session) (LinkHealthView, error)
	Events(ctx context.Context, s Session, after int64, limit int) ([]LinkEventView, error)
	Repair(ctx context.Context, s Session, person uuid.UUID) (int, error)
	DecideRestore(ctx context.Context, s Session, run uuid.UUID, version int, posted bool, reference string) (resumed bool, err error)
	Receipt(ctx context.Context, s Session, run uuid.UUID) (RunReceiptView, error)
}

type linkHealthInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
}
type linkHealthOutput struct{ Body LinkHealthView }

type linkEventsInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Cursor int64  `query:"cursor" minimum:"0" doc:"Return the events queued after this sequence number: pass the next_cursor of the last page." example:"41"`
	Limit  int    `query:"limit" minimum:"1" maximum:"200" doc:"How many to return, at most 200; 50 when absent." example:"50"`
}
type linkEventsOutput struct {
	Body struct {
		Items      []LinkEventView `json:"items" doc:"The events, oldest first, by identity."`
		NextCursor *int64          `json:"next_cursor,omitempty" doc:"The sequence number to pass as cursor for the next page; absent on the last." example:"91"`
	}
}

type linkRepairInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		PersonID string `json:"person_id,omitempty" format:"uuid" doc:"Repair one person; empty repairs everyone and sends again what failed." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	}
}
type linkRepairOutput struct {
	Body struct {
		Queued int `json:"queued" doc:"How many events were queued." example:"12"`
	}
}

type linkRestoreDecisionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		RunID     string `json:"run_id" format:"uuid" doc:"The run the restore holds." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
		Version   int    `json:"version" minimum:"1" doc:"The version." example:"2"`
		Posted    bool   `json:"posted" doc:"True if Fyniq's own record shows the run posted; false if it does not." example:"true"`
		Reference string `json:"reference,omitempty" maxLength:"200" doc:"Fyniq's posting reference, required when posted." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90"`
	}
}
type linkRestoreDecisionOutput struct {
	Body struct {
		Resumed bool `json:"resumed" doc:"True when this decision was the last: the restore is reconciled and the link resumed." example:"false"`
	}
}

type runReceiptInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
}
type runReceiptOutput struct{ Body RunReceiptView }

func init() {
	IdempotencyExempt["POST /v1/link/repairs"] = "a repair queues the current state again: repeating it only queues it again, and Fyniq's version rule makes each a no-op (NM-INT-10)"
	IdempotencyExempt["POST /v1/link/restore/decisions"] = "a run held by a restore is decided once: a second decision is refused (NM_PAYROLL_CONFLICT)"
}

func (a *API) registerLinkOps() {
	unavailable := func() error { return Err(CodeUnavailable, "The link to Fyniq is not set up in this deployment.") }
	huma.Register(a.API, huma.Operation{
		OperationID: "getLinkHealth", Method: http.MethodGet, Path: "/v1/link/health", Tags: []string{"Link"}, Summary: "The link's health",
		Description: "Events by state, the oldest waiting, the last sent, whether the sync has failed for 24 hours (a warning on Checks), the checkpoint (the highest sequence number queued, sent and received), the newest " +
			"events that failed or were refused, with Fyniq's codes, and a restore that is not reconciled yet. By identity only: never a payload. Only an Owner or an Administrator. It changes nothing.", Errors: []int{401, 403, 503},
	}, func(ctx context.Context, in *linkHealthInput) (*linkHealthOutput, error) {
		s, err := a.linkRole(ctx, in.Cookie, authz.Owner, authz.Administrator)
		if err != nil {
			return nil, err
		}
		if a.d.LinkOps == nil {
			return nil, unavailable()
		}
		h, err := a.d.LinkOps.Health(ctx, s)
		if err != nil {
			return nil, payrollError(err)
		}
		return &linkHealthOutput{Body: h}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listLinkEvents", Method: http.MethodGet, Path: "/v1/link/events", Tags: []string{"Link"}, Summary: "The events queued for Fyniq after a sequence number",
		Description: "The events Nemura queued for Fyniq after a sequence number, oldest first, with their state: for the people team to see what was sent and what was not. By identity only: the event's ID, number, " +
			"type, object, version, state and Fyniq's code; never a payload. Cursor-paged. Only an Owner or an Administrator. It changes nothing.", Errors: []int{401, 403, 503},
	}, func(ctx context.Context, in *linkEventsInput) (*linkEventsOutput, error) {
		s, err := a.linkRole(ctx, in.Cookie, authz.Owner, authz.Administrator)
		if err != nil {
			return nil, err
		}
		if a.d.LinkOps == nil {
			return nil, unavailable()
		}
		limit := in.Limit
		if limit == 0 {
			limit = 50
		}
		es, err := a.d.LinkOps.Events(ctx, s, in.Cursor, limit+1)
		if err != nil {
			return nil, payrollError(err)
		}
		out := &linkEventsOutput{}
		if len(es) > limit {
			es = es[:limit]
			next := es[len(es)-1].Sequence
			out.Body.NextCursor = &next
		}
		out.Body.Items = es
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "repairLink", Method: http.MethodPost, Path: "/v1/link/repairs", Tags: []string{"Link"}, Summary: "Send the current state to Fyniq again",
		Description: "Repairs a gap (PRD-002 NM-INT-10): queues the current state of one person, or of everyone, for Fyniq again at the version it has now (Fyniq ignores a version it holds and applies one it missed), and " +
			"queues again each object whose newest event failed or was refused. It is repair and never a change, and it needs an active link. Audited by its scope and number. Only an Owner or an Administrator. " +
			"It is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 409, 422, 503},
	}, func(ctx context.Context, in *linkRepairInput) (*linkRepairOutput, error) {
		s, err := a.linkRole(ctx, in.Cookie, authz.Owner, authz.Administrator)
		if err != nil {
			return nil, err
		}
		if a.d.LinkOps == nil {
			return nil, unavailable()
		}
		person := uuid.Nil
		if in.Body.PersonID != "" {
			if person, err = uuid.Parse(in.Body.PersonID); err != nil {
				return nil, Err(CodePersonNotFound, "There is no such person in this organisation.")
			}
		}
		n, err := a.d.LinkOps.Repair(ctx, s, person)
		if err != nil {
			return nil, payrollError(err)
		}
		out := &linkRepairOutput{}
		out.Body.Queued = n
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decideLinkRestoreRun", Method: http.MethodPost, Path: "/v1/link/restore/decisions", Tags: []string{"Link"}, DefaultStatus: http.StatusOK, Summary: "Decide a run a restore holds",
		Description: "After Nemura's database was restored the link is held and each run approved with proof whose answer the restored database lacks is unknown (PRD-002 NM-INT-13). An Owner or an Administrator, " +
			"with a passkey confirmed in the last five minutes, decides each against Fyniq's own record: posted (with Fyniq's reference) or not posted (the same approval is then sent again). When the last is decided the " +
			"restore is reconciled, the link resumes and the current state is sent again. Audited. It is on the never-list for the AI and MCP surfaces.", Errors: []int{401, 403, 409, 422, 503},
	}, func(ctx context.Context, in *linkRestoreDecisionInput) (*linkRestoreDecisionOutput, error) {
		s, err := a.linkRole(ctx, in.Cookie, authz.Owner, authz.Administrator)
		if err != nil {
			return nil, err
		}
		if a.d.LinkOps == nil {
			return nil, unavailable()
		}
		if !s.Fresh(a.d.Clock.Now()) {
			return nil, Err(CodeStepUp, "Confirm it is you with your passkey, then try again: reconciling a restore decides what Fyniq posted.")
		}
		run, perr := uuid.Parse(in.Body.RunID)
		if perr != nil {
			return nil, Err(CodeNotFound, "There is no such pay run.")
		}
		resumed, err := a.d.LinkOps.DecideRestore(ctx, s, run, in.Body.Version, in.Body.Posted, in.Body.Reference)
		if err != nil {
			return nil, linkAdminError(err)
		}
		out := &linkRestoreDecisionOutput{}
		out.Body.Resumed = resumed
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getPayRunReceipt", Method: http.MethodGet, Path: "/v1/payroll/runs/{id}/receipt", Tags: []string{"Payroll"}, Summary: "What Nemura kept of a run's life with Fyniq",
		Description: "The run's receipt (PRD-002 NM-INT-13): the proof kept for each version approved while linked (its digest, the mapping version, when), each sending and where it stands, Fyniq's answers and the " +
			"payment instructions and outcomes. No figure, no name and no account: it is what two databases are reconciled by. The Payroll administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 404, 503},
	}, func(ctx context.Context, in *runReceiptInput) (*runReceiptOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return nil, err
		}
		if !p.Pay {
			return nil, Err(CodeRoleRequired, "Only the Payroll administrator and the Owner see and change pay.")
		}
		run, perr := uuid.Parse(in.ID)
		if perr != nil {
			return nil, Err(CodeNotFound, "There is no such pay run.")
		}
		if a.d.LinkOps == nil {
			return nil, unavailable()
		}
		r, err := a.d.LinkOps.Receipt(ctx, s, run)
		if err != nil {
			return nil, payrollError(err)
		}
		return &runReceiptOutput{Body: r}, nil
	})
}
