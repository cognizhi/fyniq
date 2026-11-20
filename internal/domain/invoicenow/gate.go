package invoicenow

import (
	"errors"
	"slices"
	"strings"
	"time"
)

// Status is where a submission stands. A submission moves queued, then sent, then acknowledged (IRAS's acknowledgement
// ID received), or failed with its reasons; a failed one is queued again by a person, which is a new attempt, not a
// new version (a correction is a new version, with the same document UUID: FR-TAX-10).
type Status string

// The statuses of a submission.
const (
	Queued       Status = "queued"
	Sent         Status = "sent"
	Acknowledged Status = "acknowledged"
	Failed       Status = "failed"
)

// Moves are the legal changes of status, which the database allows alone.
var Moves = map[Status][]Status{
	Queued: {Sent, Failed},
	Sent:   {Acknowledged, Failed},
	Failed: {Queued},
}

// Legal says whether a submission may move from one status to another.
func Legal(from, to Status) bool { return slices.Contains(Moves[from], to) }

// Reason is why a document of the return is left out of the submissions (FR-TAX-10, G4-034).
type Reason string

// The exclusions IRAS's InvoiceNow requirement names (PRD-001 §14.2) and one for anything else, which says what.
const (
	ReverseCharge        Reason = "reverse_charge"
	ImportPermit         Reason = "import_permit"
	UnregisteredSupplier Reason = "unregistered_supplier"
	BlockedInputTax      Reason = "blocked_input_tax"
	OutOfScope           Reason = "out_of_scope"
	Other                Reason = "other"
)

// Reasons are every reason, in the order the screen offers them.
var Reasons = []Reason{ReverseCharge, ImportPermit, UnregisteredSupplier, BlockedInputTax, OutOfScope, Other}

// Item is a document of a return period that feeds the GST return: a posted sale or purchase with a GST code the return
// reports.
type Item struct {
	DocumentID string
	Number     string
	Kind       string // sale, purchase, credit_note, debit_note, claim
	Date       time.Time
}

// Submission is the latest version's submission of a document.
type Submission struct {
	DocumentID string
	Version    int
	Status     Status
	Ack        string // IRAS's acknowledgement ID, once received
}

// Missing is a document the gate waits for, with where it stands.
type Missing struct {
	Item
	State string // none, queued, sent, failed
}

// Contingency is a recorded decision to file with a gap (FR-TAX-11): requested by one person, approved by another, with
// the reason and the evidence of the outage.
type Contingency struct {
	RequestedBy string
	ApprovedBy  string
	Reason      string
	Evidence    int // the number of evidence files
}

// ErrContingency is a contingency that is not valid.
var ErrContingency = errors.New("the contingency needs a reason, evidence of the outage, and two different people")

// Valid says whether the contingency may let a return be filed.
func (c Contingency) Valid() error {
	if strings.TrimSpace(c.Reason) == "" || c.Evidence < 1 || c.RequestedBy == "" || c.ApprovedBy == "" || c.RequestedBy == c.ApprovedBy {
		return ErrContingency
	}
	return nil
}

// Result is the gate for a return period.
type Result struct {
	// Applies is false while the organisation has not said the requirement applies: the gate is then not assessed.
	Applies      bool
	Ready        bool
	Missing      []Missing
	Acknowledged int
	Excluded     int
	// Contingency is set when the return may be filed with the gap listed in Missing.
	Contingency *Contingency
}

// Evaluate is the gate (FR-TAX-11): every item has an acknowledged latest submission or an exclusion, or a valid
// contingency covers the gap. With no requirement the gate is not assessed and never blocks.
func Evaluate(applies bool, items []Item, subs []Submission, excluded map[string]Reason, c *Contingency) Result {
	if !applies {
		return Result{Applies: false, Ready: true}
	}
	latest := map[string]Submission{}
	for _, s := range subs {
		if cur, ok := latest[s.DocumentID]; !ok || s.Version > cur.Version {
			latest[s.DocumentID] = s
		}
	}
	r := Result{Applies: true}
	for _, it := range items {
		if _, ok := excluded[it.DocumentID]; ok {
			r.Excluded++
			continue
		}
		s, ok := latest[it.DocumentID]
		switch {
		case ok && s.Status == Acknowledged && s.Ack != "":
			r.Acknowledged++
		case !ok:
			r.Missing = append(r.Missing, Missing{Item: it, State: "none"})
		default:
			r.Missing = append(r.Missing, Missing{Item: it, State: string(s.Status)})
		}
	}
	slices.SortFunc(r.Missing, func(a, b Missing) int {
		if c := a.Date.Compare(b.Date); c != 0 {
			return c
		}
		return strings.Compare(a.Number, b.Number)
	})
	r.Ready = len(r.Missing) == 0
	if !r.Ready && c != nil && c.Valid() == nil {
		r.Ready = true
		r.Contingency = c
	}
	return r
}
