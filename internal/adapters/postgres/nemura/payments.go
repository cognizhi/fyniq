package nemura

import (
	"context"
	"errors"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/checks"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// PaymentInput is one change of a person's payment in a pay run (NM-PAY-10). It is the one function both writers use: Fyniq's status events through the link's inbox (Source "fyniq", with the
// instruction ID and the link as the audit actor) and a person recording an outcome standalone (Source "manual", with a document of the person as evidence).
type PaymentInput struct {
	Run, Person uuid.UUID
	State       string
	Instruction string
	Amount      string // dollars, a decimal string
	PaidOn      time.Time
	Source      string // fyniq or manual
	Evidence    *uuid.UUID
	Note        string
	By          *uuid.UUID // the person recording a manual outcome
}

func paymentError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM175":
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: pg.Message + "."}
		case "NM176":
			return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The evidence of a payment is a document of the person who was paid: choose one of theirs."}
		case "NM127":
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "A payment state is evidence: a change is a new event."}
		}
	}
	return err
}

// RecordPaymentState adds an event to a person's payment in a run: it refuses a state that is not legal after the last one (an unknown outcome blocks a new payment until it is resolved), a run
// whose latest version is not approved, a person owed nothing in it, a manual record with no evidence, and a Fyniq event with no instruction ID. The audit event names the payment and its state
// and never the amount.
func RecordPaymentState(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, actor audit.Actor, in PaymentInput) error {
	if !payroll.ValidState(in.State) {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "state: that is not a payment state; use scheduled, released, sent, accepted, paid, rejected, returned or unknown."}
	}
	if in.Source != "fyniq" && in.Source != "manual" {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "source: a payment state comes from Fyniq's status events or from a person with evidence."}
	}
	if in.Source == "manual" && (in.By == nil || in.Evidence == nil) {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "evidence: an outcome recorded by a person needs a document of the person paid as evidence (a bank statement line or a transfer receipt). A bare word is never enough."}
	}
	if in.Source == "fyniq" && in.Instruction == "" {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "instruction_id: a state from Fyniq names the payment instruction."}
	}
	amt, _, err := apd.NewFromString(in.Amount)
	if err != nil || amt.Sign() < 0 || amt.Exponent < -2 {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "amount: write the amount paid in dollars and cents, such as 4000.00."}
	}
	future := day(in.PaidOn) > day(at)
	if in.PaidOn.IsZero() || future && (in.State == payroll.PayPaid || in.State == payroll.PayReturned || in.State == payroll.PayRejected) {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "paid_on: give the date of the payment, which cannot be in the future."}
	}
	if len(in.Note) > 500 {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "note: keep it to 500 characters."}
	}
	pv, err := payableVersion(ctx, tx, in.Run)
	if err != nil {
		return err
	}
	var owed bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.pay_run_line WHERE version_id = $1 AND person_id = $2 AND disburse > 0)", pv, in.Person).Scan(&owed); err != nil {
		return err
	}
	if !owed {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "person_id: nothing is paid to that person in this run."}
	}
	var seq int
	if err := tx.QueryRow(ctx, "SELECT coalesce(max(seq), 0) + 1 FROM nemura.pay_payment_event WHERE run_id = $1 AND person_id = $2", in.Run, in.Person).Scan(&seq); err != nil {
		return err
	}
	id := uuid.Must(uuid.NewV7())
	var instruction, note *string
	if in.Instruction != "" {
		instruction = &in.Instruction
	}
	if in.Note != "" {
		note = &in.Note
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.pay_payment_event (id, run_id, person_id, seq, state, instruction_id, amount, paid_on, source, evidence_id, note, recorded_by, recorded_at) VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8, $9, $10, $11, $12, $13)`,
		id, in.Run, in.Person, seq, in.State, instruction, amt.Text('f'), day(in.PaidOn), in.Source, in.Evidence, note, in.By, at); err != nil {
		return paymentError(err)
	}
	changes := map[string]audit.Change{"run": {nil, in.Run.String()}, "person": {nil, in.Person.String()}, "state": {nil, in.State}, "source": {nil, in.Source}}
	if in.Instruction != "" {
		changes["instruction_id"] = audit.Change{nil, in.Instruction}
	}
	return Audit(ctx, tx, org, at, actor, "nemura.payment.state_recorded", audit.Object{Type: "pay_payment_event", ID: id}, changes, "")
}

// payableVersion is the run's latest version that someone approved and nothing superseded: the one that is paid.
func payableVersion(ctx context.Context, tx pgx.Tx, run uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, "SELECT id FROM nemura.pay_run_version WHERE run_id = $1 AND approved_by IS NOT NULL AND state <> 'superseded' ORDER BY version DESC LIMIT 1", run).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return id, &payroll.Refusal{Code: payroll.CodeConflict, Message: "Payments are recorded against an approved version of the run: approve one first."}
	}
	return id, err
}

// PaymentEventRow is one event of a person's payment.
type PaymentEventRow struct {
	Seq                                      int
	State, Instruction, Amount, Source, Note string
	PaidOn                                   time.Time
	Evidence                                 *uuid.UUID
	RecordedAt                               time.Time
}

// PaymentLineRow is a person's payment in a run: what is owed, the events, and what is wrong with it.
type PaymentLineRow struct {
	Person         uuid.UUID
	PersonName     string
	Disburse       string
	State          string // the latest, empty when nothing is recorded
	Events         []PaymentEventRow
	Attention      bool // rejected, returned or unknown
	AmountMismatch bool // paid an amount other than the one owed
}

// PaymentsRow is a run's payments.
type PaymentsRow struct {
	Version int
	State   string // approved, paid in part or paid
	Lines   []PaymentLineRow
}

// PaymentsOf lists each person's payment of the run's payable version with its events, and the run's own payment state.
func PaymentsOf(ctx context.Context, tx pgx.Tx, run uuid.UUID) (PaymentsRow, error) {
	var out PaymentsRow
	pv, err := payableVersion(ctx, tx, run)
	if err != nil {
		return out, err
	}
	if err := tx.QueryRow(ctx, "SELECT version FROM nemura.pay_run_version WHERE id = $1", pv).Scan(&out.Version); err != nil {
		return out, err
	}
	lines, err := RunLines(ctx, tx, pv)
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT person_id, seq, state, coalesce(instruction_id, ''), amount::numeric(20,2)::text, paid_on, source, evidence_id, coalesce(note, ''), recorded_at FROM nemura.pay_payment_event WHERE run_id = $1 ORDER BY person_id, seq`, run)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	events := map[uuid.UUID][]PaymentEventRow{}
	for rows.Next() {
		var p uuid.UUID
		var e PaymentEventRow
		if err := rows.Scan(&p, &e.Seq, &e.State, &e.Instruction, &e.Amount, &e.PaidOn, &e.Source, &e.Evidence, &e.Note, &e.RecordedAt); err != nil {
			return out, err
		}
		events[p] = append(events[p], e)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	owed, paid := 0, 0
	for _, l := range lines {
		if dec(l.Disburse).Sign() <= 0 {
			continue
		}
		owed++
		pl := PaymentLineRow{Person: l.Person, PersonName: l.PersonName, Disburse: l.Disburse, Events: events[l.Person]}
		if n := len(pl.Events); n > 0 {
			last := pl.Events[n-1]
			pl.State = last.State
			pl.Attention = payroll.NeedsAttention(last.State)
			pl.AmountMismatch = last.State == payroll.PayPaid && dec(last.Amount).Cmp(dec(l.Disburse)) != 0
		}
		if pl.State == payroll.PayPaid {
			paid++
		}
		out.Lines = append(out.Lines, pl)
	}
	out.State = payroll.RunPayState(owed, paid)
	return out, nil
}

// PaymentBlocked says whether a person's payment in a run has an unknown outcome, which blocks any new payment of it, by file or by Fyniq, until it is resolved (NM-PAY-10).
func PaymentBlocked(ctx context.Context, tx pgx.Tx, run, person uuid.UUID) (bool, error) {
	var state string
	err := tx.QueryRow(ctx, "SELECT state FROM nemura.pay_payment_event WHERE run_id = $1 AND person_id = $2 ORDER BY seq DESC LIMIT 1", run, person).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return state == payroll.PayUnknown, err
}

// PaymentFacts are the latest state of each person's payment in each run, with the payslip of the run's payable version, for N.1 #9 and #14.
func PaymentFacts(ctx context.Context, tx pgx.Tx) ([]checks.PaymentFact, error) {
	rows, err := tx.Query(ctx, `WITH latest AS (
  SELECT DISTINCT ON (run_id, person_id) run_id, person_id, state, paid_on, amount FROM nemura.pay_payment_event ORDER BY run_id, person_id, seq DESC),
 pv AS (
  SELECT DISTINCT ON (run_id) id, run_id FROM nemura.pay_run_version WHERE approved_by IS NOT NULL AND state <> 'superseded' ORDER BY run_id, version DESC)
SELECT l.person_id::text, r.month, l.state, l.paid_on, ln.disburse IS NOT NULL AND l.amount <> ln.disburse, s.pay_date
FROM latest l JOIN nemura.pay_run r ON r.id = l.run_id JOIN pv ON pv.run_id = l.run_id
LEFT JOIN nemura.pay_run_line ln ON ln.version_id = pv.id AND ln.person_id = l.person_id
LEFT JOIN nemura.payslip s ON s.version_id = pv.id AND s.person_id = l.person_id
ORDER BY r.month, l.person_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []checks.PaymentFact
	for rows.Next() {
		var f checks.PaymentFact
		var paidOn, slipDate *time.Time
		if err := rows.Scan(&f.Person, &f.Month, &f.State, &paidOn, &f.AmountMismatch, &slipDate); err != nil {
			return nil, err
		}
		if f.State == payroll.PayPaid {
			f.PaidOn = paidOn
		}
		f.HasPayslip, f.PayslipDate = slipDate != nil, slipDate
		out = append(out, f)
	}
	return out, rows.Err()
}
