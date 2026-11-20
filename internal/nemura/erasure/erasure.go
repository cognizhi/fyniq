// Package erasure is Nemura's erasure of a former employee's personal data (PRD-002 NM-SEC-08): when it may be asked for, what the signed
// register outside the database keeps of it, and which of Nemura's personal data it scrubs, which it shreds and which it cannot reach.
//
// It is pure: no storage, no clock, no network. The adapter runs it.
package erasure

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The periods a former employee's records are kept before an erasure may be asked for. Each is a minimum from an official page; the company may keep longer
// (NM-SEC-05) and a legal hold keeps what it names.
const (
	// EmploymentMonths: MOM, "Employment records": for ex-employees, "Last two years, to be kept for one year after the employee leaves employment".
	// https://www.mom.gov.sg/employment-practices/employment-records (retrieved 21 Nov 2026).
	EmploymentMonths = 12
	// PayrollYears: IRAS keeps Forms IR8A, payslips and CPF statements for five years from the relevant Year of Assessment: the pay of year Y is kept to the end of Y+5
	// (the IRAS Record Keeping Guide's example: records of 1 January to 31 December 2025, YA 2026, until 31 December 2030).
	// https://www.iras.gov.sg/taxes/corporate-income-tax/basics-of-corporate-income-tax/record-keeping-requirements (retrieved 21 Nov 2026).
	// [accountant to confirm] the exact reach for IR8A and CPF records (the CPF Board's own page was not read).
	PayrollYears = 5
)

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

// The codes.
const (
	CodeNotLeft       = "NM_ERASURE_NOT_LEFT"
	CodeNotYet        = "NM_ERASURE_NOT_YET"
	CodeHeld          = "NM_ERASURE_HELD"
	CodeOwnRequest    = "NM_ERASURE_SELF"
	CodeNotApproved   = "NM_ERASURE_NOT_APPROVED"
	CodeAlready       = "NM_ERASURE_ALREADY"
	CodeBadRegister   = "NM_ERASURE_REGISTER"
	CodeOpenAccessReq = "NM_ERASURE_ACCESS_REQUEST"
)

// EligibleOn is the first day an erasure may be asked for: after the employment records' period from the leaving date and, when the person was ever paid
// through Nemura, after the payroll records' period from the end of the year of their last pay. lastPay is nil for a person never paid. keepMonths is the
// company's own period for employment records, which may be longer and never shorter (NM-PACK-04).
func EligibleOn(left time.Time, lastPay *time.Time, keepMonths int) time.Time {
	months := EmploymentMonths
	if keepMonths > months {
		months = keepMonths
	}
	on := left.AddDate(0, months, 0)
	if lastPay != nil {
		end := time.Date(lastPay.Year()+PayrollYears, time.December, 31, 0, 0, 0, 0, time.UTC)
		if after := end.AddDate(0, 0, 1); after.After(on) {
			on = after
		}
	}
	return on
}

// Check refuses a request that is too early or whose person has not left. day is the business date.
func Check(left *time.Time, eligible, day time.Time) error {
	if left == nil || left.After(day) {
		return &Refusal{CodeNotLeft, "Only a former employee's personal data is erased: this person has not left. Record their leaving date first."}
	}
	if day.Before(eligible) {
		return &Refusal{CodeNotYet, fmt.Sprintf("This person's records must be kept until %s (the employment records' and the payroll records' periods). Ask again then.", eligible.Format("2 Jan 2006"))}
	}
	return nil
}

// Entry is what the register keeps of an erasure, outside the database so a restore cannot lose it: identities and dates, never a name or a value.
type Entry struct {
	Schema       string    `json:"schema"`
	Organisation uuid.UUID `json:"organisation"`
	Person       uuid.UUID `json:"person"`
	Order        uuid.UUID `json:"order"`
	ErasedOn     string    `json:"erased_on"`
	EligibleOn   string    `json:"eligible_on"`
	RequestedBy  uuid.UUID `json:"requested_by"`
	ApprovedBy   uuid.UUID `json:"approved_by"`
}

// Schema names the register entry's format.
const Schema = "nemura-erasure-register@1"

// Signed is an entry with the key that signed it and the signature over its canonical bytes.
type Signed struct {
	Entry Entry  `json:"entry"`
	KeyID string `json:"key_id"`
	Sig   []byte `json:"sig"`
}

// Canonical is the bytes an entry is signed over.
func (e Entry) Canonical() ([]byte, error) {
	e.Schema = Schema
	return json.Marshal(e)
}

// Key is where the register keeps a person's entry: one per person, write-once.
func Key(org, person uuid.UUID) string {
	return "nemura-erasure/" + org.String() + "/" + person.String() + ".json"
}

// Prefix is where an organisation's entries are.
func Prefix(org uuid.UUID) string { return "nemura-erasure/" + org.String() + "/" }

// Verifier checks a signature made by a key version.
type Verifier interface {
	Verify(keyID string, msg, sig []byte) error
}

// Open reads a signed entry and checks its signature and that it names the organisation and the key it was filed under.
func Open(raw []byte, v Verifier, org uuid.UUID, key string) (Entry, error) {
	var s Signed
	if err := json.Unmarshal(raw, &s); err != nil {
		return Entry{}, &Refusal{CodeBadRegister, "A register entry is not readable."}
	}
	msg, err := s.Entry.Canonical()
	if err != nil {
		return Entry{}, err
	}
	if err := v.Verify(s.KeyID, msg, s.Sig); err != nil {
		return Entry{}, &Refusal{CodeBadRegister, "A register entry's signature does not verify: it is not applied."}
	}
	if s.Entry.Organisation != org || Key(org, s.Entry.Person) != key {
		return Entry{}, &Refusal{CodeBadRegister, "A register entry names another organisation or person than the place it is filed in: it is not applied."}
	}
	return s.Entry, nil
}

// Digest is a SHA-256 of an entry's canonical bytes, kept in the order as its proof.
func Digest(e Entry) ([32]byte, error) {
	b, err := e.Canonical()
	return sha256.Sum256(b), err
}

// How an erasure treats each class of Nemura's personal data (internal/nemura/datacat keeps the classes; a test keeps every class here).
type Bucket string

const (
	// Scrubbed: in rows that may change, the personal columns are emptied in place and the row stays.
	Scrubbed Bucket = "scrubbed"
	// Shredded: a sealed value or a file is destroyed (the value's columns emptied, the blob deleted).
	Shredded Bucket = "shredded"
	// AppendOnly: the personal data sits in plaintext in append-only rows, which the database refuses to change or delete (INV: an issued letter, a payslip, a pay run
	// line and the like). They stay, tied to the person by an opaque ID, until the owner decides how they are shredded (a guard carve-out or sealing under a per-person key).
	AppendOnly Bucket = "append-only"
)

// Classes maps each class key of the data inventory to what an erasure does with it. Several classes are split across tables; the bucket is the strongest that
// can be done to all of them today, so a class with any plaintext in an append-only row is AppendOnly.
var Classes = map[string]Bucket{
	"members":                Scrubbed,
	"sign-in-identities":     AppendOnly, // identity_directory is global and passkeys are append-only: the member is deactivated, the subject stays
	"people-records":         Scrubbed,   // person; join_task.note is append-only and stays
	"legal-classification":   AppendOnly, // person_classification is guarded
	"restricted-identifiers": Shredded,
	"dependants":             AppendOnly,
	"leave-records":          AppendOnly,
	"sick-leave":             AppendOnly,
	"flexible-work":          AppendOnly,
	"own-details":            Scrubbed, // person_contact and emergency_contact; details_change and hr_question are append-only
	"pay-records":            AppendOnly,
	"key-employment-terms":   AppendOnly,
	"people-assistant":       AppendOnly,
	"letters":                AppendOnly,
	"access-requests":        AppendOnly, // an answered request and its decisions are kept as the record of how the obligation was met
	"legal-holds":            AppendOnly,
	"grievances":             AppendOnly, // a grievance is final once decided and its inquiry record is append-only; its words may name people, so a de-identification of them is left (PRD-002 NM-SEC-06, NM-SEC-08)
}

// ErrNotFound is returned for an order that does not exist.
var ErrNotFound = errors.New("there is no such erasure")
