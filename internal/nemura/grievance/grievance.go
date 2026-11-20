// Package grievance is Nemura's confidential grievance channel (PRD-002 NM-SEC-06): who may handle one, how it is routed away from the people it is about, what a written outcome is.
//
// It is pure: no storage, no clock, no network. The adapter runs it, and the database's row-level policies and triggers hold the same rules as a backstop.
package grievance

import (
	"encoding/binary"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// Refusal is a plain refusal with a stable code.
type Refusal struct{ Code, Message string }

func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

// The codes.
const (
	CodeNotFound  = "NM_NOT_FOUND"
	CodeInvalid   = "NM_INVALID"
	CodeState     = "NM_GRIEVANCE_STATE"
	CodeNoHandler = "NM_GRIEVANCE_NO_HANDLER"
)

// Bounds of the words a person writes.
const (
	MinWords  = 20 // characters: a summary or an outcome says something
	MaxWords  = 4000
	MaxPeople = 5
)

// Findings are what a written outcome concludes.
var Findings = []string{"substantiated", "partly_substantiated", "not_substantiated", "no_finding"}

// EntryKinds are what the handler records in the inquiry.
var EntryKinds = []string{"note", "interview", "finding"}

// Words checks a summary or an outcome and says what is wrong in plain words.
func Words(what, s string) *Refusal {
	n := len([]rune(strings.TrimSpace(s)))
	if n < MinWords || n > MaxWords {
		return &Refusal{Code: CodeInvalid, Message: "The " + what + " is 20 to 4000 characters: say what happened, when and who was there, so it can be looked into."}
	}
	return nil
}

// Candidate is a member who holds a role that may handle a grievance today.
type Candidate struct {
	Member uuid.UUID
	Role   string // "hr_admin" or "owner"
}

// Route picks the handler of a grievance: an HR administrator before the Owner, never the complainant, never anyone it is about and never one of those in `not`
// (for a handover, the handler who is passing it on). Among those eligible the choice is spread by the grievance's own identifier, so that no one is always first
// and no count of other people's grievances is needed (a handler cannot read them). ok is false when nobody is eligible: the grievance then waits, visible to the
// complainant and to authorised people who are not involved, until one takes it.
func Route(grievance uuid.UUID, cands []Candidate, complainant uuid.UUID, about map[uuid.UUID]bool, not ...uuid.UUID) (handler uuid.UUID, ok bool) {
	skip := map[uuid.UUID]bool{complainant: true}
	for _, n := range not {
		skip[n] = true
	}
	var tier [2][]uuid.UUID
	for _, c := range cands {
		if skip[c.Member] || about[c.Member] {
			continue
		}
		switch c.Role {
		case "hr_admin":
			tier[0] = append(tier[0], c.Member)
		case "owner":
			tier[1] = append(tier[1], c.Member)
		}
	}
	for _, t := range tier {
		if len(t) == 0 {
			continue
		}
		sort.Slice(t, func(i, j int) bool { return t[i].String() < t[j].String() })
		return t[binary.BigEndian.Uint64(grievance[8:])%uint64(len(t))], true
	}
	return uuid.Nil, false
}
