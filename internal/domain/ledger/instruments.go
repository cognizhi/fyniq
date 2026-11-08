package ledger

import (
	"fmt"

	"github.com/google/uuid"
)

// RoleConvertibleInstruments is 2700 Convertible notes / SAFE, keyed by instrument (FR-EQ-18, design D6): each line names
// the recorded SAFE or note it is for, so each one's balance reads from the ledger (ENG-04).
const RoleConvertibleInstruments = "convertible-instruments"

// CodeInstrument is a line on convertible instruments without its instrument, or an instrument on another line.
const CodeInstrument = "LEDGER-49"

// InstrumentAccount reports whether the account holds SAFEs and convertible notes, keyed by instrument.
func (a Account) InstrumentAccount() bool { return a.Role == RoleConvertibleInstruments }

// instrumentProblem is what is wrong with the instrument of a line to acc, if anything; field names the line's instrument:
// a line on convertible instruments names a recorded SAFE or note, and no other line one. A rule whose settings list no
// instruments (Settings.Instruments nil) checks only that it is there exactly when it must be; the database refuses the
// wrong one either way (FY292).
func instrumentProblem(s Settings, acc Account, instrument uuid.UUID, field string) *Problem {
	switch {
	case acc.InstrumentAccount() && instrument == uuid.Nil:
		return &Problem{CodeInstrument, field, fmt.Sprintf("account %s holds SAFEs and convertible notes, so the line names its instrument", acc.Code),
			"Choose the recorded SAFE or note the money is for"}
	case acc.InstrumentAccount() && s.Instruments != nil && !s.Instruments[instrument]:
		return &Problem{CodeInstrument, field, "the instrument is not a recorded SAFE or convertible note of this organisation",
			"Record the instrument first: another person approves it"}
	case !acc.InstrumentAccount() && instrument != uuid.Nil:
		return &Problem{CodeInstrument, field, fmt.Sprintf("account %s does not hold SAFEs or convertible notes, so the line names no instrument", acc.Code),
			"Leave the instrument out, or post to 2700 Convertible notes / SAFE"}
	}
	return nil
}
