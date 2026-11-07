package equity

import (
	"fmt"
	"slices"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The ACRA steps of an equity transaction (FR-EQ-09, FR-EQ-17; p1-equity-register design D4), recorded between its
// approval and its effective date, and what they raise.
const (
	CodeStep          = "EQUITY-10" // a step that does not fit: its kind, a day after today, or a reference too long
	CodeLodgementDue  = "EQUITY-11" // ⏰ a lodgement is due: an issue's shareholders' approval, a transfer's notice, a conversion's
	CodeNotRegistered = "EQUITY-12" // ⚠ approved and not effective after the set number of days
	CodeLapsing       = "EQUITY-13" // ⚠ a cash issue's shareholders' approval lapses within 30 days
)

// TransferLodgeWithin is how many days after the instrument of transfer is signed ACRA's guidance expects the notice of
// transfer (s126) [verify]; ConversionLodgeWithin is how many after the resolution a conversion between classes is lodged
// [verify].
const (
	TransferLodgeWithin   = 14
	ConversionLodgeWithin = 14
)

// StepsFor are the steps a transaction of a kind records, in their order: a cash issue takes a founder's conversion's,
// and so does a SAFE's or note's conversion, an allotment too.
func StepsFor(kind string) []string {
	switch kind {
	case KindIssueCash, KindInstrumentConversion:
		return director.StepKinds
	case KindTransfer:
		return []string{storage.StepInstrumentSigned, storage.StepTransferLodged}
	case KindClassConversion:
		return []string{storage.StepResolved, storage.StepConversionLodged}
	}
	return nil
}

// Steps are a transaction's latest step of each kind.
type Steps map[string]storage.EquityStep

// StepsOf are the latest step of each kind among a transaction's steps, recorded earliest first.
func StepsOf(all []storage.EquityStep) Steps {
	out := Steps{}
	for _, s := range all {
		out[s.Kind] = s
	}
	return out
}

// CheckStep is what is wrong with a step of a transaction of kind as entered (EQUITY-10).
func CheckStep(kind, step string, on time.Time, reference string, today time.Time) error {
	kinds := StepsFor(kind)
	switch {
	case len(kinds) == 0:
		return &Error{CodeStep, fmt.Sprintf("a %s records no ACRA step", kindWords(kind)), "Record the steps of the transaction it reverses, if any"}
	case !slices.Contains(kinds, step):
		return &Error{CodeStep, fmt.Sprintf("%q is not a step of a %s", step, kindWords(kind)), "Choose one of " + fmt.Sprint(kinds)}
	case step != storage.StepAGMDue && on.After(today):
		return &Error{CodeStep, fmt.Sprintf("the step is dated %s, after today", on.Format(time.DateOnly)), "Record a step once it has happened"}
	case len(reference) > director.MaxStepReference:
		return &Error{CodeStep, fmt.Sprintf("the reference has %d characters; the most is %d", len(reference), director.MaxStepReference),
			"Enter ACRA's transaction number only"}
	}
	return nil
}

func kindWords(kind string) string {
	return map[string]string{KindIssueCash: "cash issue", KindTransfer: "transfer", KindClassConversion: "conversion between classes",
		KindReversal: "reversal", KindInstrumentConversion: "conversion of a SAFE or note"}[kind]
}

// Flag is what a transaction's steps raise: ⏰ due, or ⚠ warning.
type Flag = director.Flag

// Flags are what an approved transaction that is not effective raises on today: a cash issue as a founder's conversion
// does (⏰ its shareholders' approval to lodge 14 days after the resolution, ⚠ not registered after the set days, ⚠
// its approval lapsing within 30 days); a transfer ⏰ its notice of transfer 14 days after the instrument is signed; a
// conversion between classes ⏰ its lodgement 14 days after the resolution; each ⚠ when not effective after the set
// days (FR-EQ-09) [verify the lodgement periods].
func Flags(kind string, steps Steps, approvedOn, lapsesOn, today time.Time, registrationDays int) []Flag {
	day := func(t time.Time) string { return t.Format("2 Jan 2006") }
	if kind == KindIssueCash || kind == KindInstrumentConversion {
		conv := director.Steps{}
		for k, s := range steps {
			conv[k] = storage.ConversionStep{ID: s.ID, Kind: s.Kind, On: s.On, Reference: s.Reference, RecordedBy: s.RecordedBy, RecordedAt: s.RecordedAt}
		}
		out := director.Flags(conv, approvedOn, lapsesOn, today, registrationDays)
		codes := map[string]string{director.CodeApprovalDue: CodeLodgementDue, director.CodeNotRegistered: CodeNotRegistered,
			director.CodeApprovalLapsing: CodeLapsing}
		for i := range out {
			out[i].Code = codes[out[i].Code]
		}
		return out
	}
	var out []Flag
	due := func(after string, within int, what, fix string) {
		from := approvedOn
		if s, ok := steps[after]; ok {
			from = s.On
		}
		on := from.AddDate(0, 0, within)
		out = append(out, Flag{Code: CodeLodgementDue, Kind: "due", DueOn: &on, Message: what + " is due to be lodged with ACRA by " + day(on), Fix: fix})
	}
	switch kind {
	case KindTransfer:
		if _, ok := steps[storage.StepTransferLodged]; !ok {
			due(storage.StepInstrumentSigned, TransferLodgeWithin, "the notice of transfer (s126)",
				"Lodge it, attach ACRA's acknowledgement and record the day it was lodged; the instrument of transfer bears share duty, the parties' matter")
		}
	case KindClassConversion:
		if _, ok := steps[storage.StepConversionLodged]; !ok {
			due(storage.StepResolved, ConversionLodgeWithin, "the conversion of the shares", "Lodge it, attach ACRA's acknowledgement and record the day it was lodged")
		}
	default:
		return nil
	}
	if registrationDays > 0 {
		if since := int(today.Sub(approvedOn) / (24 * time.Hour)); since > registrationDays {
			out = append(out, Flag{Code: CodeNotRegistered, Kind: "warning",
				Message: fmt.Sprintf("approved %s and not effective after %d days", day(approvedOn), registrationDays),
				Fix:     "Record the day ACRA's register of members shows it, once it does"})
		}
	}
	return out
}
