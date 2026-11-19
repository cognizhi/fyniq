// Package selfservice holds the rules of a person's own details and of the requests they make to the people team (PRD-002 NM-ESS-03, NM-ESS-04): what a person may change
// at once, what needs evidence and another person's approval, how a request is followed, and when it is due. It is pure.
package selfservice

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Codes of a refusal; each is in the API's catalogue.
const (
	CodeInvalid  = "NM_REQUEST_INVALID"
	CodeConflict = "NM_REQUEST_CONFLICT"
	CodeNotFound = "NM_NOT_FOUND"
	CodeRole     = "NM_ROLE_REQUIRED"
)

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

func invalid(format string, a ...any) error { return &Refusal{CodeInvalid, fmt.Sprintf(format, a...)} }

// Due days (working days) for each kind of request that has no rule of its own: a decision on a change of details, an answer to a question to HR. Letters have their type's
// days and flexible work has the pack's reply deadline. Decided 20 Nov 2026 (`p1-nemura-self-service-letters` O4).
const (
	ChangeDueDays   = 5
	QuestionDueDays = 3
)

// Kinds of a change of details that need evidence and approval (NM-ESS-03).
const (
	LegalName = "legal_name"
	Residency = "residency"
	NRIC      = "nric_fin"
)

// ChangeKinds are the changes that wait for evidence and approval, in the order the page lists them.
var ChangeKinds = []string{LegalName, Residency, NRIC}

// Residencies are the residency values the record holds.
var Residencies = []string{"citizen", "permanent_resident", "foreigner"}

// Contact are the details a person changes at once.
type Contact struct {
	Phone, PersonalEmail, Address, PostalCode string
}

var phone = regexp.MustCompile(`^[+0-9][0-9 ()-]{2,28}$`)

func text(name, v string, max int) error {
	if utf8.RuneCountInString(strings.TrimSpace(v)) > max {
		return invalid("%s is longer than %d characters: shorten it.", name, max)
	}
	return nil
}

// CheckContact refuses what is not a phone number, an email or a short address, naming the field.
func CheckContact(c Contact) error {
	if c.Phone != "" && !phone.MatchString(strings.TrimSpace(c.Phone)) {
		return invalid("The phone number is not valid: write digits, spaces, brackets or dashes, with a + for a country code, such as +65 9123 4567.")
	}
	if e := strings.TrimSpace(c.PersonalEmail); e != "" {
		a, err := mail.ParseAddress(e)
		if err != nil || a.Address != e || len(e) > 200 {
			return invalid("The personal email is not valid: write it as name@example.com.")
		}
	}
	if err := text("The address", c.Address, 300); err != nil {
		return err
	}
	return text("The postal code", c.PostalCode, 12)
}

// EmergencyContact is someone to call about a person.
type EmergencyContact struct {
	Name, Relationship, Phone string
}

// MaxEmergency is how many emergency contacts a person keeps.
const MaxEmergency = 3

// CheckEmergency refuses more than three, or one with no name, relationship or phone.
func CheckEmergency(list []EmergencyContact) error {
	if len(list) > MaxEmergency {
		return invalid("An emergency contact list holds up to %d people: remove one.", MaxEmergency)
	}
	for i, c := range list {
		n := strings.TrimSpace(c.Name)
		switch {
		case n == "" || utf8.RuneCountInString(n) > 120:
			return invalid("Emergency contact %d needs a name of up to 120 characters.", i+1)
		case strings.TrimSpace(c.Relationship) == "" || utf8.RuneCountInString(strings.TrimSpace(c.Relationship)) > 60:
			return invalid("Emergency contact %d needs a relationship of up to 60 characters, such as spouse or parent.", i+1)
		case !phone.MatchString(strings.TrimSpace(c.Phone)):
			return invalid("Emergency contact %d needs a phone number: write digits, spaces, brackets or dashes, such as +65 9123 4567.", i+1)
		}
	}
	return nil
}

// Change is a request to change what needs evidence and approval. It holds no NRIC or FIN: the request says what changed in words and carries the evidence, and the
// people team records the number through the sealed flow.
type Change struct {
	Kind, LegalName, Residency, Note string
	PRStart                          *time.Time
}

// CheckChange refuses a change that is not whole: a name for a legal-name change, a residency (and the day residence began for a permanent resident) for a residency
// change, and always a note saying what changed.
func CheckChange(c Change) error {
	known := false
	for _, k := range ChangeKinds {
		known = known || k == c.Kind
	}
	if !known {
		return invalid("The kind of change is not legal_name, residency or nric_fin.")
	}
	if n := strings.TrimSpace(c.Note); n == "" || utf8.RuneCountInString(n) > 500 {
		return invalid("Say what changed and why, in up to 500 characters: the people team reads it with your evidence.")
	}
	switch c.Kind {
	case LegalName:
		n := strings.TrimSpace(c.LegalName)
		if n == "" || utf8.RuneCountInString(n) > 200 {
			return invalid("The new legal name is required, up to 200 characters, as it appears on your identity document.")
		}
	case Residency:
		ok := false
		for _, r := range Residencies {
			ok = ok || r == c.Residency
		}
		if !ok {
			return invalid("The residency is not citizen, permanent_resident or foreigner.")
		}
		if (c.Residency == "permanent_resident") != (c.PRStart != nil) {
			return invalid("A permanent resident gives the day permanent residence began, and nobody else does.")
		}
	case NRIC:
		if c.LegalName != "" || c.Residency != "" || c.PRStart != nil {
			return invalid("A change of NRIC or FIN holds no number: say what changed in the note, attach the evidence, and the people team records the number.")
		}
	}
	return nil
}

// CheckQuestion refuses an empty or very long question.
func CheckQuestion(q string) error {
	if n := strings.TrimSpace(q); n == "" || utf8.RuneCountInString(n) > 1000 {
		return invalid("Write your question in up to 1,000 characters.")
	}
	return nil
}

// Steps of a request a person follows (NM-ESS-04): received, with HR, ready, or closed (refused or withdrawn).
const (
	Received = "received"
	WithHR   = "with_hr"
	Ready    = "ready"
	Closed   = "closed"
)

// Step is where a request is, from its kind and state. A letter that is checked waits for the signatory, so it is with HR; a request still waiting for its first look
// is received; one decided, issued, answered or replied is ready; one refused or withdrawn is closed.
func Step(kind, state string) string {
	switch state {
	case "issued", "approved", "answered", "replied":
		return Ready
	case "refused", "withdrawn":
		return Closed
	case "checked":
		return WithHR
	}
	return Received
}

// Kinds of request the list shows.
const (
	KindLetter   = "letter"
	KindFlex     = "flexible_work"
	KindChange   = "details_change"
	KindQuestion = "question"
)

// Request is one row of a person's list.
type Request struct {
	ID, Kind, Title, State, Step string
	Asked                        time.Time
	Due                          *time.Time
	Reply                        string
}
