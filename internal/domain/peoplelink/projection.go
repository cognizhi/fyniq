package peoplelink

import (
	"time"

	"github.com/google/uuid"
)

// The event types of PRD-002 Appendix E that Nemura sends and the projection applies (FR-PPL-06, FR-PPL-15).
const (
	TypePersonUpserted    = "people.person.upserted"
	TypeBankDetailsChange = "people.bank_details.changed"
	TypeTaxHoldChanged    = "people.tax_hold.changed"
	// TypeAccessChanged is what Nemura says of a sign-in identity (design D7; one sign-in across the apps): an issuer, a subject (never an email) and two yes or no answers. A door and a hint, never a grant.
	TypeAccessChanged = "identity.access.changed"
)

// AccessChanged is the payload of identity.access.changed from Nemura, and the whole of it. PeopleAccess says the identity is an active member of the organisation in Nemura; PayrollApprover that it holds a
// role that approves a pay run there, so Fyniq can ask the person to enrol the passkey the approval is signed with (PPL-19). The object version is the sending event's sequence.
type AccessChanged struct {
	Issuer          string `json:"issuer"`
	Subject         string `json:"subject"`
	PeopleAccess    bool   `json:"people_access"`
	PayrollApprover bool   `json:"payroll_approver"`
}

// Outcomes of an event in the inbox.
const (
	OutcomeApplied      = "applied"
	OutcomeIgnoredOlder = "ignored_older"
	OutcomeRefused      = "refused"
)

// Person is a person of the employee projection: what accounting needs and nothing else (FR-PPL-06). The three versions move apart,
// so a late older version of one part never undoes a newer one (FR-PPL-15). No NRIC, salary or account number is held here.
type Person struct {
	ID                uuid.UUID
	Version           int
	DisplayName       string
	WorkEmail         string
	DepartmentValue   uuid.UUID // Fyniq's own dimension value (departments come from Fyniq, NM-INT-01)
	CostCentreValue   uuid.UUID
	StartedOn, LeftOn *time.Time
	Status            string
	LineManager       uuid.UUID // a person ID; the manager's record may arrive later
	Removed           bool      // a tombstone
	HoldVersion       int
	HoldID            uuid.UUID
	Held              bool
	HoldFrom          *time.Time
	BankVersion       int
	BankRecordID      uuid.UUID
	UserID            uuid.UUID // the Fyniq user the person was mapped to, explicitly; never by email (FR-PPL-18)
	UpdatedAt         time.Time
	known             bool
}

// Known reports whether the projection already held the person before an event applied.
func (p Person) Known() bool { return p.known }

// Existing marks a person read from the store, so that Known says so.
func Existing(p Person) Person { p.known = true; return p }

// PersonUpserted is the payload of people.person.upserted: the record's current figures at its version, or a tombstone.
type PersonUpserted struct {
	PersonID    uuid.UUID  `json:"person_id"`
	DisplayName string     `json:"display_name"`
	WorkEmail   string     `json:"work_email"`
	Department  *uuid.UUID `json:"department_id"`
	CostCentre  *uuid.UUID `json:"cost_centre_id"`
	StartedOn   string     `json:"started_on"`
	LeftOn      string     `json:"left_on"`
	Status      string     `json:"status"`
	LineManager *uuid.UUID `json:"line_manager_person_id"`
	Deleted     bool       `json:"deleted"`
}

// ApplyPerson applies a person event of version v to the person as the projection holds it (cur: the zero Person when unknown). An event no
// newer than the version held is ignored (ignored = true) and changes nothing: a late version 1 never brings back the manager version 2
// replaced. A tombstone keeps the person with the removal marked, so approvals that name them stay readable.
func ApplyPerson(cur Person, ev PersonUpserted, v int, at time.Time) (next Person, ignored bool, err error) {
	if v <= cur.Version && cur.known {
		return cur, true, nil
	}
	next = cur
	next.ID, next.Version, next.UpdatedAt = ev.PersonID, v, at
	if ev.Deleted {
		next.Removed = true
		return next, false, nil
	}
	started, err := parseDate(ev.StartedOn)
	if err != nil {
		return cur, false, &Refusal{CodeMalformed, "started_on is not a date (YYYY-MM-DD)", err}
	}
	left, err := parseDate(ev.LeftOn)
	if err != nil {
		return cur, false, &Refusal{CodeMalformed, "left_on is not a date (YYYY-MM-DD)", err}
	}
	if ev.PersonID == uuid.Nil || ev.DisplayName == "" {
		return cur, false, &Refusal{CodeMalformed, "a person needs an ID and a display name", nil}
	}
	if ev.LineManager != nil && *ev.LineManager == ev.PersonID {
		return cur, false, &Refusal{CodeMalformed, "a person cannot be their own line manager", nil}
	}
	next.DisplayName, next.WorkEmail, next.Status, next.StartedOn, next.LeftOn, next.Removed = ev.DisplayName, ev.WorkEmail, ev.Status, started, left, false
	next.DepartmentValue, next.CostCentreValue, next.LineManager = orNil(ev.Department), orNil(ev.CostCentre), orNil(ev.LineManager)
	return next, false, nil
}

// BankDetailsChanged is the payload of people.bank_details.changed: the person's current bank-detail record and its version, never the number
// (FR-PPL-14, NM-INT-09).
type BankDetailsChanged struct {
	PersonID uuid.UUID `json:"person_id"`
	RecordID uuid.UUID `json:"bank_detail_id"`
}

// ApplyBankDetails records that Nemura approved a newer version of a person's bank details. A newer version blocks the person's unreleased
// salary lines until their batch is approved again with it; the number never comes here.
func ApplyBankDetails(cur Person, ev BankDetailsChanged, v int, at time.Time) (next Person, ignored bool, err error) {
	if ev.PersonID == uuid.Nil || ev.RecordID == uuid.Nil {
		return cur, false, &Refusal{CodeMalformed, "a bank-details change needs a person and a record", nil}
	}
	if v <= cur.BankVersion && cur.known {
		return cur, true, nil
	}
	next = cur
	next.ID, next.BankVersion, next.BankRecordID, next.UpdatedAt = ev.PersonID, v, ev.RecordID, at
	return next, false, nil
}

// TaxHoldChanged is the payload of people.tax_hold.changed (NM-INT-08): the hold's ID and whether the person is held from a date. Only
// Nemura's release, after IRAS's notice or directive recorded with evidence, sends status released.
type TaxHoldChanged struct {
	PersonID      uuid.UUID `json:"person_id"`
	HoldID        uuid.UUID `json:"hold_id"`
	EffectiveFrom string    `json:"effective_from"`
	Status        string    `json:"status"` // held or released
	EvidenceRef   string    `json:"evidence_ref"`
}

// ApplyTaxHold applies a hold event: held or released, at the hold's version. A held person's lines are not released (FR-PPL-13).
func ApplyTaxHold(cur Person, ev TaxHoldChanged, v int, at time.Time) (next Person, ignored bool, err error) {
	from, err := parseDate(ev.EffectiveFrom)
	if err != nil {
		return cur, false, &Refusal{CodeMalformed, "effective_from is not a date (YYYY-MM-DD)", err}
	}
	if ev.PersonID == uuid.Nil || ev.HoldID == uuid.Nil || (ev.Status != "held" && ev.Status != "released") {
		return cur, false, &Refusal{CodeMalformed, "a hold event needs a person, a hold and a status of held or released", nil}
	}
	if ev.Status == "released" && ev.EvidenceRef == "" {
		return cur, false, &Refusal{CodeMalformed, "a release needs the evidence of IRAS's notice or directive", nil}
	}
	if v <= cur.HoldVersion && cur.known {
		return cur, true, nil
	}
	next = cur
	next.ID, next.HoldVersion, next.HoldID, next.Held, next.HoldFrom, next.UpdatedAt = ev.PersonID, v, ev.HoldID, ev.Status == "held", from, at
	return next, false, nil
}

func parseDate(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func orNil(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}
