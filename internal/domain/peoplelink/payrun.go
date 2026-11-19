package peoplelink

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The event type of an approved pay run (PRD-002 Appendix E) and the refusals of its verification (PRD-001 FR-PPL-09, FR-PPL-11). Each failure has
// its own stable code, so that a refusal says which check failed; none of them carries a figure.
const (
	TypePayrollRunApproved = "payroll.run.approved"

	CodeRunEnvelope  = "PPL-11" // the approval envelope's digest is not the one named, or it is not well formed
	CodeRunOrg       = "PPL-12" // the approval was signed for another organisation than the linked one
	CodeRunSegregate = "PPL-13" // the preparer approved the run (SOD-4)
	CodeRunRight     = "PPL-14" // the approver did not hold the payroll-approval right when signing
	CodeRunPasskey   = "PPL-15" // the approver's signed record does not verify against their passkey
	CodeRunMapping   = "PPL-16" // the run was calculated against another account-mapping version than Fyniq's current one
	CodeRunUsed      = "PPL-17" // the approval envelope was used before
	CodeRunStale     = "PPL-18" // a version no newer than one already posted
	CodeRunApprover  = "PPL-19" // the approver has no sign-in identity here, or no passkey to sign with
	CodeHold         = "PPL-21" // a payment to a person held for tax clearance is not released (FR-PPL-13)
	CodeRunPosting   = "PPL-20" // the posting is refused; Fyniq's ledger code and reason go back to Nemura
)

// PayrollRightApprove is the right to approve a pay run in Nemura, which the approver must have held when they signed (NM-ROLE-04).
const PayrollRightApprove = "payroll.approve"

// Party is a person as both apps know them: by sign-in issuer and subject, never by email (FR-PPL-18, NM-INT-12).
type Party struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// RunDepartment is a department's gross pay in the run's wage month. The department is a dimension value of Fyniq's: departments come from Fyniq.
type RunDepartment struct {
	DepartmentID string `json:"department_id"`
	Gross        string `json:"gross"`
}

// RunDeduction is a deduction authorised under FR-PAY-16, by kind (such as advance_recovery), which the mapping gives its account.
type RunDeduction struct {
	Kind   string `json:"kind"`
	Amount string `json:"amount"`
}

// RunEnvelope is the approval envelope over a run's canonical figures (PRD-002 NM-INT-06): the organisation, run, version and wage month, the
// figures by department and pay item, the pack, policy and mapping versions, the preparer and the approver. The approver's passkey signs its
// SHA-256, so the figures Fyniq posts are the figures that were approved: they are read from this envelope and from nothing else.
type RunEnvelope struct {
	Organisation   string          `json:"organisation"`
	SourceSystem   string          `json:"source_system"`
	RunID          string          `json:"run_id"`
	Version        int             `json:"version"`
	Supersedes     int             `json:"supersedes"`
	WageMonth      string          `json:"wage_month"` // YYYY-MM
	Departments    []RunDepartment `json:"departments"`
	EmployeeCPF    string          `json:"employee_cpf"`
	EmployerCPF    string          `json:"employer_cpf"`
	SDL            string          `json:"sdl"`
	SHG            string          `json:"shg"`
	Deductions     []RunDeduction  `json:"deductions"`
	Reimbursements string          `json:"reimbursements"`
	PackVersion    string          `json:"pack_version"`
	PolicyVersion  string          `json:"policy_version"`
	MappingVersion int             `json:"mapping_version"`
	Preparer       Party           `json:"preparer"`
	Approver       Party           `json:"approver"`
	ApproverRight  string          `json:"approver_right"`
}

// RunSubmission is the payload of payroll.run.approved: the envelope as Nemura signed it, its SHA-256, and the approver's signed approval
// record, a passkey assertion over that SHA-256 as the challenge (NM-INT-06, FR-PPL-09).
type RunSubmission struct {
	Envelope       json.RawMessage `json:"envelope"`
	EnvelopeSHA256 string          `json:"envelope_sha256"`
	Approval       struct {
		AuthenticatorData string `json:"authenticator_data"` // base64url
		ClientData        string `json:"client_data"`
		Signature         string `json:"signature"`
	} `json:"approval"`
}

// ParseRun reads a submission strictly and checks what needs no storage: the envelope's SHA-256 is the digest of its canonical bytes, it was signed
// for the linked organisation, its figures are well formed, the approver is not the preparer and held the right to approve (FR-PPL-09, SOD-4).
// It returns the envelope and the digest as the passkey's challenge.
func ParseRun(payload []byte, linkedOrganisation string) (RunEnvelope, RunSubmission, []byte, error) {
	var sub RunSubmission
	d := json.NewDecoder(bytes.NewReader(payload))
	d.DisallowUnknownFields()
	if err := d.Decode(&sub); err != nil {
		return RunEnvelope{}, sub, nil, &Refusal{CodeMalformed, "the payload is not a pay run submission of PRD-002 Appendix E", err}
	}
	canonical, err := Canonical(sub.Envelope)
	if err != nil {
		return RunEnvelope{}, sub, nil, &Refusal{CodeRunEnvelope, "the approval envelope cannot be read as canonical JSON", err}
	}
	sum := sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:]) != sub.EnvelopeSHA256 {
		return RunEnvelope{}, sub, nil, &Refusal{CodeRunEnvelope, "the approval envelope is not the one whose SHA-256 the approval names", nil}
	}
	var env RunEnvelope
	dd := json.NewDecoder(bytes.NewReader(sub.Envelope))
	dd.DisallowUnknownFields()
	if err := dd.Decode(&env); err != nil {
		return RunEnvelope{}, sub, nil, &Refusal{CodeRunEnvelope, "the approval envelope has a field, or a value, that PRD-002 NM-INT-06 does not list", err}
	}
	switch {
	case env.Organisation != linkedOrganisation || env.SourceSystem != "nemura":
		return RunEnvelope{}, sub, nil, &Refusal{CodeRunOrg, "the approval was signed for another organisation than the linked one", nil}
	case env.Approver == env.Preparer || env.Approver.Issuer == "" || env.Approver.Subject == "":
		return RunEnvelope{}, sub, nil, &Refusal{CodeRunSegregate, "the run's preparer cannot approve it, and the approver must be named by issuer and subject (SOD-4)", nil}
	case env.ApproverRight != PayrollRightApprove:
		return RunEnvelope{}, sub, nil, &Refusal{CodeRunRight, "the approver did not hold the right to approve a pay run when they signed", nil}
	}
	if _, err := uuid.Parse(env.RunID); err != nil || env.Version < 1 || env.Supersedes < 0 || env.Supersedes >= env.Version || env.MappingVersion < 1 {
		return RunEnvelope{}, sub, nil, &Refusal{CodeRunEnvelope, "the run's ID, version, the version it supersedes or its mapping version is not valid", nil}
	}
	if _, err := time.Parse("2006-01", env.WageMonth); err != nil || len(env.Departments) == 0 {
		return RunEnvelope{}, sub, nil, &Refusal{CodeRunEnvelope, fmt.Sprintf("the wage month %q is not YYYY-MM, or the run has no department", env.WageMonth), nil}
	}
	return env, sub, sum[:], nil
}
