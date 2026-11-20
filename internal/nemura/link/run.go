package link

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// The type of an approved pay run (PRD-002 Appendix E, NM-INT-06) and the right the approver must hold when they sign (NM-ROLE-04).
const (
	TypePayrollRunApproved = "payroll.run.approved"
	RightApprove           = "payroll.approve"
)

// Party is a person as both apps know them: by sign-in issuer and subject, never by email (NM-INT-07, NM-INT-12).
type Party struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// RunDepartment is a department's gross pay in the wage month; the department is a dimension value of Fyniq's (departments come from Fyniq, NM-INT-01).
type RunDepartment struct {
	DepartmentID string `json:"department_id"`
	Gross        string `json:"gross"`
}

// RunDeduction is a deduction authorised under FR-PAY-16, by kind, which Fyniq's mapping gives its account.
type RunDeduction struct {
	Kind   string `json:"kind"`
	Amount string `json:"amount"`
}

// RunEnvelope is the approval envelope over a run's canonical figures (NM-INT-06): its fields are the contract's (`contracts/peoplelink/README.md`) and no others, because Fyniq reads it strictly
// and posts the figures of this envelope and nothing else. The approver's passkey signs its SHA-256 as the WebAuthn challenge.
type RunEnvelope struct {
	Organisation   string          `json:"organisation"`
	SourceSystem   string          `json:"source_system"`
	RunID          string          `json:"run_id"`
	Version        int             `json:"version"`
	Supersedes     int             `json:"supersedes"`
	WageMonth      string          `json:"wage_month"`
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

// Seal puts the envelope in its canonical form and takes its SHA-256: the bytes the link carries, and the digest the approver's passkey signs.
func (e RunEnvelope) Seal() (canonical []byte, sha string, err error) {
	if e.Departments == nil {
		e.Departments = []RunDepartment{}
	}
	if e.Deductions == nil {
		e.Deductions = []RunDeduction{}
	}
	sort.Slice(e.Departments, func(i, j int) bool { return e.Departments[i].DepartmentID < e.Departments[j].DepartmentID })
	sort.Slice(e.Deductions, func(i, j int) bool { return e.Deductions[i].Kind < e.Deductions[j].Kind })
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, "", err
	}
	canonical, err = Canonical(raw)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(sum[:]), nil
}

// RunApproval is the approver's signed record: a passkey assertion over the envelope's SHA-256, each part base64url (the form Fyniq reads).
type RunApproval struct {
	AuthenticatorData string `json:"authenticator_data"`
	ClientData        string `json:"client_data"`
	Signature         string `json:"signature"`
}

// RunSubmission is the payload of payroll.run.approved: the envelope as the passkey signed it, its SHA-256 and the approver's signed record. The submission is only the bytes that were approved,
// so a figure is never rebuilt after the approval.
type RunSubmission struct {
	Envelope       json.RawMessage `json:"envelope"`
	EnvelopeSHA256 string          `json:"envelope_sha256"`
	Approval       RunApproval     `json:"approval"`
}

// Assertion refusals: why a passkey answer is not an assertion over this envelope at this origin.
var (
	ErrAssertionShape     = errors.New("the answer is not a passkey assertion (base64url authenticator data, client data and signature)")
	ErrAssertionChallenge = errors.New("the passkey answered another challenge than the approval envelope's digest")
	ErrAssertionOrigin    = errors.New("the passkey answered at another origin than Nemura's")
	ErrAssertionSite      = errors.New("the passkey answered for another site, or the person was not present and verified")
)

// CheckAssertion checks what Nemura can of an approver's assertion before it keeps it and sends it: it says webauthn.get over exactly the envelope's digest (the 32 raw bytes as the challenge), at
// Nemura's own origin, for the shared relying party, with the person present and verified. Nemura cannot check the signature: the credential is the one the approver enrolled in Fyniq, whose public
// key only Fyniq holds, and Fyniq verifies it (p1-payroll-link design D3). A pass here is a well-formed answer, never a proof.
func CheckAssertion(a RunApproval, digest []byte, origin, rpID string) error {
	dec := func(s string) []byte { b, _ := base64.RawURLEncoding.DecodeString(s); return b }
	ad, cd, sig := dec(a.AuthenticatorData), dec(a.ClientData), dec(a.Signature)
	if len(ad) < 37 || len(cd) == 0 || len(sig) == 0 {
		return ErrAssertionShape
	}
	var c struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
	}
	if err := json.Unmarshal(cd, &c); err != nil || c.Type != "webauthn.get" {
		return ErrAssertionShape
	}
	if c.Challenge != base64.RawURLEncoding.EncodeToString(digest) {
		return ErrAssertionChallenge
	}
	if c.Origin != origin {
		return ErrAssertionOrigin
	}
	rp := sha256.Sum256([]byte(rpID))
	const present, verified = 0x01, 0x04
	if !bytes.Equal(ad[:32], rp[:]) || ad[32]&present == 0 || ad[32]&verified == 0 {
		return ErrAssertionSite
	}
	return nil
}

// DeductionKind is the kind a pay run's deduction item stands for: the run names an item "Deduction: <kind>" (NM-PAY-04), which is the kind Fyniq's mapping keys its account by.
func DeductionKind(itemName string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(itemName, "Deduction:")))
}

// DepartmentKey is how a department's name is matched to Fyniq's, in Nemura: case and surrounding spaces do not matter.
func DepartmentKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// ValidDepartment reports whether a Fyniq department ID is an ID, so an envelope never carries a name in its place.
func ValidDepartment(id string) bool { _, err := uuid.Parse(id); return err == nil }

// Challenge is the WebAuthn challenge for an envelope: the 32 raw bytes of its SHA-256.
func Challenge(sha string) ([]byte, error) {
	b, err := hex.DecodeString(sha)
	if err != nil || len(b) != sha256.Size {
		return nil, fmt.Errorf("%q is not a SHA-256", sha)
	}
	return b, nil
}
