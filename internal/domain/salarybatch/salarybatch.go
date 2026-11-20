// Package salarybatch is a salary payment batch: the payment lines Nemura sends for a pay run Fyniq has posted (PRD-001 FR-PAY-11, FR-PPL-12, 13, 14, 17; PRD-002 Appendix E; p1-payroll-link
// design D9). It holds what a line is and says, the states a line moves through and where each state may come from, the digest of the lines an approval binds, and what stops a line being
// released. The batch's lines are sealed in storage: a name, a bank, an account and an amount appear only to the roles with the salary-payments right (FR-PPL-17).
package salarybatch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a salary batch's refusals (ENG-23). None carries a name, an account or an amount.
const (
	CodeNotPrepared = "SB-01" // the batch has no money account, payment date and method yet
	CodeAccount     = "SB-02" // the money account is not an open SGD bank account
	CodeNothing     = "SB-03" // no line can be released now
	CodeNotOpen     = "SB-04" // the batch is not in the state the command needs
	CodeReapprove   = "SB-05" // a line or a bank detail changed since the batch was approved: approve it again
	CodeSeparation  = "SB-06" // the releaser prepared, asked for or approved the batch, or prepared or approved the run in Nemura
	CodeBlocked     = "SB-07" // a line's bank details are older than the person's newest approved version (FR-PPL-14)
	CodeUnknown     = "SB-08" // an unknown outcome blocks a new instruction for the person in the run (FR-PPL-12)
	CodeReason      = "SB-09" // cancelling needs a reason
	CodeDate        = "SB-10" // the payment date is not a date, or is before the wage month began
	CodeMethod      = "SB-11" // the method is not FAST or GIRO
	CodeState       = "SB-12" // the state is not one a line may move to from where it is, or its source cannot give it
	CodeLine        = "SB-13" // there is no such line in the batch
)

// SGD is the currency a salary batch pays in, as the payment batch does.
var SGD = money.MustLookup("SGD")

// The methods of payment, as the payment batch has them.
const (
	FAST = "fast"
	GIRO = "giro"
)

// Detail is what a line seals: who is paid and where (SEC-11).
type Detail struct {
	PayeeName     string `json:"payee_name"`
	BankBIC       string `json:"bank_bic"`
	AccountName   string `json:"account_name"`
	AccountNumber string `json:"account_number"`
}

// Marshal is the detail as sealed.
func (d Detail) Marshal() ([]byte, error) { return json.Marshal(d) }

// ReadDetail reads a sealed line's opened content.
func ReadDetail(b []byte) (Detail, error) {
	var d Detail
	return d, json.Unmarshal(b, &d)
}

// LineAAD binds a sealed line to its organisation, batch, line and version: copied to another, it does not open.
func LineAAD(org, batch, line uuid.UUID, version int) []byte {
	return fmt.Appendf(nil, "salary_batch_line:%s:%s:%s:%d", org, batch, line, version)
}

// FileAAD binds a release's sealed file to its organisation and batch and the file's own SHA-256, since a batch has several releases.
func FileAAD(org, batch uuid.UUID, sha []byte) []byte {
	return fmt.Appendf(nil, "salary_release:%s:%s:%s", org, batch, hex.EncodeToString(sha))
}

// Amount reads a line's amount.
func Amount(l storage.SalaryLine) (money.Money, error) {
	return money.ParseStored(string(l.Amount), SGD)
}

// Total is what lines pay in all.
func Total(lines []storage.SalaryLine) (money.Money, error) {
	t := money.Zero(SGD)
	for _, l := range lines {
		a, err := Amount(l)
		if err != nil {
			return t, err
		}
		if t, err = t.Add(a); err != nil {
			return t, err
		}
	}
	return t, nil
}

// LinesDigest is the SHA-256 of what an approval binds about the lines: each line's reference, version, person, amount and the bank-detail record, version and digest it came from, in the order of
// the line references. It holds no name and no account, and an approver who sees only totals can still tell that nothing under the total moved (FR-PPL-14, FR-PPL-17).
func LinesDigest(lines []storage.SalaryLine) string {
	rows := make([]string, 0, len(lines))
	for _, l := range lines {
		rows = append(rows, fmt.Sprintf("%s|%d|%s|%s|%s|%d|%s", l.LineRef, l.LineVersion, l.PersonID, l.Amount, l.BankDetailID, l.BankDetailVersion, l.BankDetailDigest))
	}
	slices.Sort(rows)
	sum := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(sum[:])
}

// Latest is each line's latest state, by line reference, from the batch's events in the order they were recorded.
func Latest(events []storage.SalaryLineEvent) map[uuid.UUID]storage.SalaryLineEvent {
	out := map[uuid.UUID]storage.SalaryLineEvent{}
	for _, e := range events {
		out[e.LineRef] = e
	}
	return out
}

// legal are the moves between states (PRD-002 NM-PAY-10, as Nemura keeps them): the first event of a payment may be any state; paid goes back only to returned; rejected or returned to
// scheduled (a new payment); and an unknown outcome is resolved only to paid, rejected or returned.
var legal = map[string][]string{
	storage.LineScheduled: {storage.LineReleased, storage.LineSent, storage.LineAccepted, storage.LinePaid, storage.LineRejected, storage.LineUnknown},
	storage.LineReleased:  {storage.LineSent, storage.LineAccepted, storage.LinePaid, storage.LineRejected, storage.LineUnknown},
	storage.LineSent:      {storage.LineAccepted, storage.LinePaid, storage.LineRejected, storage.LineReturned, storage.LineUnknown},
	storage.LineAccepted:  {storage.LinePaid, storage.LineRejected, storage.LineReturned, storage.LineUnknown},
	storage.LinePaid:      {storage.LineReturned},
	storage.LineRejected:  {storage.LineScheduled},
	storage.LineReturned:  {storage.LineScheduled},
	storage.LineUnknown:   {storage.LinePaid, storage.LineRejected, storage.LineReturned},
}

// CanMove reports whether a line in state from may take state to.
func CanMove(from, to string) bool { return slices.Contains(legal[from], to) }

// Settled says a state is one a person sees as the end of that payment: the money is paid, or came back or was refused.
func Settled(state string) bool {
	return state == storage.LinePaid || state == storage.LineRejected || state == storage.LineReturned
}

// BlocksInstruction says a state stops any new instruction for the person in the run: the money may have gone (FR-PPL-12).
func BlocksInstruction(state string) bool { return state == storage.LineUnknown }

// Overall is a batch's state as a person reads it, from its status and its lines' latest states: waiting, approved, part released, released, paid in part, paid, or cancelled.
func Overall(b storage.SalaryBatch, latest map[uuid.UUID]storage.SalaryLineEvent, lines []storage.SalaryLine) string {
	if b.Status == storage.SalaryCancelled {
		return "cancelled"
	}
	var released, paid int
	for _, l := range lines {
		switch s := latest[l.LineRef].State; {
		case s == storage.LinePaid:
			paid++
			released++
		case s != storage.LineScheduled && s != "":
			released++
		}
	}
	switch {
	case len(lines) > 0 && paid == len(lines):
		return "paid"
	case paid > 0:
		return "paid_in_part"
	case len(lines) > 0 && released == len(lines):
		return "released"
	case released > 0:
		return "released_in_part"
	case b.Status == storage.SalaryApproved:
		return "approved"
	case b.PayDate == nil:
		return "received"
	}
	return "prepared"
}
