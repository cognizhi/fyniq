package peoplelink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The event type of the payment lines of a posted pay run (PRD-002 Appendix E, payroll.payments.ready) and the refusals of receiving them (PRD-001 FR-PAY-11, FR-PPL-12, FR-PPL-14). No refusal
// carries a name, an account or an amount.
const (
	TypePayrollPaymentsReady = "payroll.payments.ready"

	CodePayLines   = "PPL-23" // the lines are not well formed, or are not those of the run's approval (its preparer and approver differ)
	CodePayRun     = "PPL-24" // Fyniq has not posted this version of the run, or a newer one has posted since
	CodePayTotal   = "PPL-25" // the lines pay more than the run's net pay posted to 2100
	CodePayPerson  = "PPL-26" // a line's person is not in the projection: send the people events first
	CodePayRelease = "PPL-27" // a line that went out is not changed, and a batch that was cancelled takes no line
	CodePayBlocked = "PPL-28" // an unknown outcome of the person's payment in this run blocks a new instruction until it is resolved (FR-PPL-12)
	CodeInNemura   = "PPL-29" // payroll lives in Nemura (the link is live): a provider's journal or a file import is refused (FR-PPL-05, FR-PAY-15)
	CodeGate       = "PPL-30" // the payroll go-live gate: an arrangement is missing, so a pay run cannot be approved for payment (FR-PAY-15, G4-009)
)

// MaxPaymentLines is how many lines one submission carries: the payment batch's own limit.
const MaxPaymentLines = 500

// PaymentLine is one person's payment as Nemura sends it: the person, what to pay, the account (sealed on arrival and never in an audit, a log or an outbox) and the bank-detail record, version
// and digest it came from (NM-INT-09). Money is a decimal string in SGD.
type PaymentLine struct {
	LineID            uuid.UUID `json:"line_id"`
	PersonID          uuid.UUID `json:"person_id"`
	PayeeName         string    `json:"payee_name"`
	BankBIC           string    `json:"bank_bic,omitempty"`  // optional: when it is left out Fyniq derives it from BankCode (design D26)
	BankCode          string    `json:"bank_code,omitempty"` // the bank's Singapore code, four digits or four and a three-digit branch
	BankName          string    `json:"bank_name,omitempty"` // the bank's name as Nemura holds it
	AccountName       string    `json:"account_name"`
	AccountNumber     string    `json:"account_number"`
	Amount            string    `json:"amount"`
	BankDetailID      uuid.UUID `json:"bank_detail_id"`
	BankDetailVersion int       `json:"bank_detail_version"`
	BankDetailDigest  string    `json:"bank_detail_digest"`
}

// PaymentsReady is the payload of payroll.payments.ready: a run's version, the day Nemura intends the money to arrive, the run's preparer and approver (which must be the ones the run was posted
// under), and the lines. The header's object version is the lines' version: a later submission for the same run version with a higher one revises lines (FR-PPL-14).
type PaymentsReady struct {
	RunID      uuid.UUID     `json:"run_id"`
	RunVersion int           `json:"run_version"`
	PayDate    string        `json:"pay_date"`
	Preparer   Party         `json:"preparer"`
	Approver   Party         `json:"approver"`
	Lines      []PaymentLine `json:"lines"`
}

var (
	digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	amountRE = regexp.MustCompile(`^[0-9]{1,16}\.[0-9]{2}$`)
	bicRE    = regexp.MustCompile(`^[A-Z0-9]{8}([A-Z0-9]{3})?$`)
	codeRE   = regexp.MustCompile(`^[0-9]{4}([0-9]{3})?$`)
)

// ParsePayments reads the payload strictly and checks what needs no storage: each line has its fields in shape and appears once, the run and its parties are named. It never repeats a value in
// a refusal.
func ParsePayments(payload []byte) (PaymentsReady, error) {
	var p PaymentsReady
	d := json.NewDecoder(bytes.NewReader(payload))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, &Refusal{CodeMalformed, "the payload is not the payment lines of PRD-002 Appendix E", err}
	}
	bad := func(msg string) (PaymentsReady, error) { return p, &Refusal{CodePayLines, msg, nil} }
	if p.RunID == uuid.Nil || p.RunVersion < 1 {
		return bad("the run's ID or version is not valid")
	}
	if _, err := time.Parse(time.DateOnly, p.PayDate); err != nil {
		return bad("the pay date is not YYYY-MM-DD")
	}
	if p.Preparer.Issuer == "" || p.Preparer.Subject == "" || p.Approver.Issuer == "" || p.Approver.Subject == "" {
		return bad("the run's preparer and approver are named by issuer and subject")
	}
	if len(p.Lines) == 0 || len(p.Lines) > MaxPaymentLines {
		return bad(fmt.Sprintf("a submission carries 1 to %d lines", MaxPaymentLines))
	}
	seen := map[uuid.UUID]bool{}
	for i, l := range p.Lines {
		switch {
		case l.LineID == uuid.Nil || l.PersonID == uuid.Nil || l.BankDetailID == uuid.Nil:
			return bad(fmt.Sprintf("line %d has no line, person or bank-detail ID", i+1))
		case seen[l.LineID]:
			return bad(fmt.Sprintf("line %d is in the submission twice", i+1))
		case !amountRE.MatchString(l.Amount) || strings.Trim(l.Amount, "0.") == "":
			return bad(fmt.Sprintf("line %d's amount is not dollars and cents above zero", i+1))
		case !digestRE.MatchString(l.BankDetailDigest) || l.BankDetailVersion < 1:
			return bad(fmt.Sprintf("line %d's bank-detail version or digest is not valid", i+1))
		case l.BankBIC == "" && !codeRE.MatchString(l.BankCode), l.BankBIC != "" && !bicRE.MatchString(l.BankBIC), l.BankCode != "" && !codeRE.MatchString(l.BankCode), len(l.BankName) > 140:
			return bad(fmt.Sprintf("line %d's bank is not in shape: send its BIC, or its bank code", i+1))
		case strings.TrimSpace(l.PayeeName) == "" || strings.TrimSpace(l.AccountName) == "" || len(l.AccountNumber) < 5 || len(l.AccountNumber) > 34 ||
			len(l.PayeeName) > 140 || len(l.AccountName) > 140:
			return bad(fmt.Sprintf("line %d's payee, bank or account is not in shape", i+1))
		}
		seen[l.LineID] = true
	}
	return p, nil
}
