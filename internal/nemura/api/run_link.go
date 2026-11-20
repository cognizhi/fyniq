package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

// A pay run's proof to Fyniq while the apps are linked (PRD-002 NM-INT-03, 06, 07; p1-nemura-fyniq-link 3.1, 3.2): the approver reads the approval envelope, which is the figures Fyniq will post by
// department, signs its digest with the passkey they enrolled in Fyniq, and approves with that answer; Nemura keeps what was signed and sends it, and the run shows what Fyniq answered.

// RunEnvelopeDepartment is a department's gross pay in the wage month, by Fyniq's own department.
type RunEnvelopeDepartment struct {
	DepartmentID string `json:"department_id" format:"uuid" doc:"Fyniq's department (a dimension value): departments come from Fyniq." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e70"`
	Gross        string `json:"gross" doc:"Gross pay of the department's people in the wage month, as a decimal string in dollars." example:"12450.00"`
}

// RunEnvelopeDeduction is a deduction by kind.
type RunEnvelopeDeduction struct {
	Kind   string `json:"kind" doc:"The kind of deduction, which Fyniq's mapping gives its account." example:"recovery"`
	Amount string `json:"amount" doc:"The month's deductions of that kind, as a decimal string in dollars." example:"150.00"`
}

// RunEnvelopeView is the approval envelope the approver signs, in words the approver can read: it is the whole of what Fyniq posts.
type RunEnvelopeView struct {
	Organisation   string                  `json:"organisation" format:"uuid" doc:"This Nemura organisation." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44"`
	RunID          string                  `json:"run_id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Version        int                     `json:"version" doc:"The version approved." example:"2"`
	Supersedes     int                     `json:"supersedes" doc:"The version this one corrects: Fyniq posts the difference from it; 0 for the first." example:"1"`
	WageMonth      string                  `json:"wage_month" doc:"The wage month, YYYY-MM." example:"2026-11"`
	Departments    []RunEnvelopeDepartment `json:"departments" doc:"Gross pay by Fyniq's department."`
	EmployeeCPF    string                  `json:"employee_cpf" doc:"The employees' CPF for the month." example:"2480.00"`
	EmployerCPF    string                  `json:"employer_cpf" doc:"The employer's CPF for the month." example:"2108.00"`
	SDL            string                  `json:"sdl" doc:"The Skills Development Levy for the month." example:"34.75"`
	SHG            string                  `json:"shg" doc:"Self-help group fund contributions for the month." example:"12.00"`
	Deductions     []RunEnvelopeDeduction  `json:"deductions" doc:"Deductions by kind."`
	Reimbursements string                  `json:"reimbursements" doc:"Claims paid with the salary." example:"0.00"`
	PackVersion    string                  `json:"pack_version" doc:"The statutory packs the run was calculated under." example:"sg-cpf@2026.1,sg-sdl@2026.1"`
	PolicyVersion  string                  `json:"policy_version" doc:"The people pack." example:"sg-people@2026.1"`
	MappingVersion int                     `json:"mapping_version" doc:"The version of Fyniq's account mapping the run was calculated against." example:"3"`
	ApproverRight  string                  `json:"approver_right" doc:"The right the approver holds: payroll.approve." example:"payroll.approve"`
}

// RunApprovalEnvelope is what the approver reads and signs.
type RunApprovalEnvelope struct {
	Linked         bool            `json:"linked" doc:"True while the apps are linked: the run is then approved with this envelope's proof; false: approve the usual way." example:"true"`
	Ready          bool            `json:"ready" doc:"True when the envelope can be signed now; false with the reasons in issues." example:"true"`
	Issues         []string        `json:"issues" doc:"Why the envelope cannot be signed yet, in plain words: a department Fyniq does not have, a person with no sign-in identity, no mapping yet."`
	Envelope       RunEnvelopeView `json:"envelope" doc:"The figures that will be posted."`
	EnvelopeSHA256 string          `json:"envelope_sha256,omitempty" doc:"The SHA-256 of the envelope's canonical form, lower-case hexadecimal: what the passkey signs." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Challenge      string          `json:"challenge,omitempty" doc:"The WebAuthn challenge: the digest's 32 raw bytes, base64url without padding. Sign exactly this, with a passkey enrolled in Fyniq, and no other." example:"n4bQgYhMfWWaL-qgxVrQFaO_TxsrC4Is0V1sFbDwCgg"`
	RPID           string          `json:"rp_id" doc:"The relying party the two apps share: pass it as rpId, so one authenticator works in both." example:"example.test"`
	Origin         string          `json:"origin" doc:"Nemura's own origin, where the passkey answers; Fyniq checks it against the link." example:"https://people.example.test"`
}

// RunProof is the approver's passkey assertion over the envelope's digest, each part base64url without padding.
type RunProof struct {
	AuthenticatorData string `json:"authenticator_data" minLength:"1" maxLength:"2048" doc:"The authenticator data." example:"SZYN5YgOjGh0NBcPZHZgW4_krrmihjLHmVzzuoMdl2MFAAAAAQ"`
	ClientData        string `json:"client_data" minLength:"1" maxLength:"4096" doc:"The client data JSON, with the challenge and the origin." example:"eyJ0eXBlIjoid2ViYXV0aG4uZ2V0In0"`
	Signature         string `json:"signature" minLength:"1" maxLength:"2048" doc:"The signature." example:"MEUCIQ"`
}

// LinkedApprovalInput approves a version with the envelope's proof.
type LinkedApprovalInput struct {
	Version        int      `json:"version" minimum:"1" doc:"The version approved: the newest, while it is calculated." example:"2"`
	Digest         string   `json:"digest" pattern:"^[0-9a-f]{64}$" doc:"The digest of the version as the approver read it (the run's own)." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	EnvelopeSHA256 string   `json:"envelope_sha256" pattern:"^[0-9a-f]{64}$" doc:"The envelope's digest as the approver read it: it must be the one the passkey signed." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	Approval       RunProof `json:"approval" doc:"The approver's passkey assertion over the envelope's digest."`
}

// RunPosting is Fyniq's answer for a version: a posting reference, or the code and words of its refusal.
type RunPosting struct {
	Status     string `json:"status" enum:"posted,refused" doc:"posted: Fyniq booked it; refused: it did not, and the run waits." example:"posted"`
	Reference  string `json:"reference,omitempty" doc:"The posting's reference in Fyniq." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e90"`
	Code       string `json:"code,omitempty" doc:"Fyniq's stable code of a refusal." example:"PPL-19"`
	Message    string `json:"message,omitempty" doc:"Fyniq's plain words for a refusal." example:"The approver has no passkey enrolled in Fyniq."`
	ReceivedAt string `json:"received_at" format:"date-time" doc:"When Nemura received the answer." example:"2026-11-25T09:00:00Z"`
}

// RunSubmissionView says what became of a run's newest approved version.
type RunSubmissionView struct {
	Linked    bool        `json:"linked" doc:"Whether the apps are linked." example:"true"`
	Version   int         `json:"version" doc:"The newest approved version; 0 when none was approved while linked." example:"2"`
	State     string      `json:"state" enum:"none,queued,sent,failed,refused" doc:"none: nothing queued; queued: waiting to be sent; sent: Fyniq took it; failed: not delivered yet, tried again; refused: Fyniq refused it on the way in." example:"sent"`
	Code      string      `json:"code,omitempty" doc:"The stable code of the last failure or refusal." example:"PPL-19"`
	SentAt    *string     `json:"settled_at,omitempty" format:"date-time" doc:"When the event was sent or refused." example:"2026-11-25T09:00:01Z"`
	Posting   *RunPosting `json:"posting,omitempty" doc:"Fyniq's answer, when it has given one."`
	Retryable bool        `json:"retryable" doc:"True when sending the same approval again could change Fyniq's answer: its refusal is about something on Fyniq's side, such as the approver's passkey or a closed period." example:"true"`
}

// RunLink is the pay run's door to the link.
type RunLink interface {
	Envelope(ctx context.Context, s Session, run uuid.UUID, version int) (RunApprovalEnvelope, error)
	Approve(ctx context.Context, s Session, run uuid.UUID, in LinkedApprovalInput) error
	Submission(ctx context.Context, s Session, run uuid.UUID) (RunSubmissionView, error)
	Retry(ctx context.Context, s Session, run uuid.UUID, version int) error
}

type runEnvelopeInput struct {
	Cookie  string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID      string `path:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Version int    `query:"version" minimum:"1" doc:"The version to approve." example:"2"`
}
type runEnvelopeOutput struct{ Body RunApprovalEnvelope }

type linkedApprovalInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Body   LinkedApprovalInput
}

type runSubmissionInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
}
type runSubmissionOutput struct{ Body RunSubmissionView }

type runRetryInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The run." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1f43"`
	Body   struct {
		Version int `json:"version" minimum:"1" doc:"The newest approved version." example:"2"`
	}
}

func (a *API) registerRunLink() {
	payroller := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return s, err
		}
		p, err := a.letterPowers(ctx, s)
		if err != nil {
			return s, err
		}
		if !p.Pay {
			return s, Err(CodeRoleRequired, "Only the Payroll administrator and the Owner see and change pay.")
		}
		return s, nil
	}
	run := func(id string) (uuid.UUID, error) {
		r, err := uuid.Parse(id)
		if err != nil {
			return uuid.Nil, Err(CodeNotFound, "There is no such pay run.")
		}
		return r, nil
	}
	unavailable := func() error {
		return Err(CodeUnavailable, "Approving a pay run with the link's proof needs the link to Fyniq, which this deployment has not set up.")
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "getPayRunApprovalEnvelope", Method: http.MethodGet, Path: "/v1/payroll/runs/{id}/approval-envelope", Tags: []string{"Payroll"}, Summary: "The approval envelope of a pay run version",
		Description: "While Nemura is linked to Fyniq a pay run is approved with proof: this is the approval envelope the approver reads and signs, the figures Fyniq will post by Fyniq's department with the " +
			"pay run's CPF, SDL, SHG, deductions and reimbursements, the packs and Fyniq's mapping version it was calculated against and the preparer and approver by sign-in identity, with its SHA-256 and the " +
			"challenge to sign it with: a passkey the approver enrolled in Fyniq, at Nemura's origin, for the shared relying party. The reasons it cannot be signed yet are listed in plain words. While " +
			"not linked it says so and the plain approval applies. The Payroll administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 404, 503},
	}, func(ctx context.Context, in *runEnvelopeInput) (*runEnvelopeOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := run(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.RunLink == nil {
			return nil, unavailable()
		}
		out, err := a.d.RunLink.Envelope(ctx, s, id, in.Version)
		if err != nil {
			return nil, payrollError(err)
		}
		return &runEnvelopeOutput{Body: out}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "approvePayRunVersionLinked", Method: http.MethodPost, Path: "/v1/payroll/runs/{id}/linked-approval", Tags: []string{"Payroll"}, DefaultStatus: http.StatusNoContent,
		Summary: "Approve a pay run version with the link's proof",
		Description: "Approves the newest version, while it is calculated, as the plain approval does (a passkey confirmed in the last five minutes, over the run's digest, the approver never the preparer) and " +
			"also with the approver's passkey assertion over the approval envelope's digest. Nemura checks that the answer is for this envelope, at its own origin, for the shared relying party and with the person " +
			"present and verified, keeps it with the envelope, and queues the submission to Fyniq in the same transaction; Fyniq verifies the signature against the passkey the approver enrolled there before it " +
			"posts anything. While the apps are linked the plain approval is closed. It is audited with the run, the version and the digests, never an amount. The Payroll administrator and the Owner.",
		Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *linkedApprovalInput) (*struct{}, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		if !s.Fresh(a.d.Clock.Now()) {
			return nil, Err(CodeStepUp, "Confirm it is you with your passkey, then try again: approving a pay run moves money.")
		}
		id, err := run(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.RunLink == nil {
			return nil, unavailable()
		}
		return nil, payrollErrorOrNil(a.d.RunLink.Approve(ctx, s, id, in.Body))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getPayRunSubmission", Method: http.MethodGet, Path: "/v1/payroll/runs/{id}/submission", Tags: []string{"Payroll"}, Summary: "What became of a pay run's submission to Fyniq",
		Description: "For the newest version approved while linked: whether it was sent, Fyniq's answer (the posting's reference, or the code and words of its refusal, with which the run waits) and whether sending " +
			"the same approval again could change the answer. The Payroll administrator and the Owner. It changes nothing.", Errors: []int{401, 403, 404, 503},
	}, func(ctx context.Context, in *runSubmissionInput) (*runSubmissionOutput, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := run(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.RunLink == nil {
			return nil, unavailable()
		}
		out, err := a.d.RunLink.Submission(ctx, s, id)
		if err != nil {
			return nil, payrollError(err)
		}
		return &runSubmissionOutput{Body: out}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "retryPayRunSubmission", Method: http.MethodPost, Path: "/v1/payroll/runs/{id}/submission", Tags: []string{"Payroll"}, DefaultStatus: http.StatusNoContent,
		Summary: "Send a pay run's approval to Fyniq again",
		Description: "After a refusal that can change (the approver has no passkey enrolled in Fyniq, a period is closed there, an account is missing), sends the same stored approval again as a new event. " +
			"The proof is never remade: it is what was approved. A refusal of the figures or the proof needs a new calculation and a new approval. The Payroll administrator and the Owner. It is audited.",
		Errors: []int{401, 403, 404, 409, 422, 503},
	}, func(ctx context.Context, in *runRetryInput) (*struct{}, error) {
		s, err := payroller(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		id, err := run(in.ID)
		if err != nil {
			return nil, err
		}
		if a.d.RunLink == nil {
			return nil, unavailable()
		}
		return nil, payrollErrorOrNil(a.d.RunLink.Retry(ctx, s, id, in.Body.Version))
	})
}
