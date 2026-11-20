package nemura

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/ports"
)

// TypePaymentsReady is the payment lines of a pay run Fyniq has posted (PRD-002 Appendix E, payroll.payments.ready; design D28): one line for each person owed money whose bank is on record.
const TypePaymentsReady = "payroll.payments.ready"

// PaymentLineEvent is one line of payroll.payments.ready, and the whole of it (contracts/peoplelink/README.md): who is paid, how much, and the bank detail record the line was made against. The account number
// is in it, in the signed envelope, to Fyniq alone: it is opened for the signing and held nowhere (design D28).
type PaymentLineEvent struct {
	LineID            uuid.UUID `json:"line_id"`
	PersonID          uuid.UUID `json:"person_id"`
	PayeeName         string    `json:"payee_name"`
	BankCode          string    `json:"bank_code"`
	BankName          string    `json:"bank_name"`
	AccountName       string    `json:"account_name"`
	AccountNumber     string    `json:"account_number"`
	Amount            string    `json:"amount"`
	BankDetailID      uuid.UUID `json:"bank_detail_id"`
	BankDetailVersion int       `json:"bank_detail_version"`
	BankDetailDigest  string    `json:"bank_detail_digest"`
}

// PaymentsReadyEvent is the payload of payroll.payments.ready.
type PaymentsReadyEvent struct {
	RunID      uuid.UUID          `json:"run_id"`
	RunVersion int                `json:"run_version"`
	PayDate    string             `json:"pay_date"`
	Preparer   link.Party         `json:"preparer"`
	Approver   link.Party         `json:"approver"`
	Lines      []PaymentLineEvent `json:"lines"`
}

// lineNamespace names a payment line: the same person in the same run is the same line for ever, so a revision is a new version of it at Fyniq.
var lineNamespace = uuid.MustParse("6f7d1f6e-3a1c-4d57-9e2b-5a0c8b7e1d42")

// LineIDOf is the ID of a person's payment line in a run.
func LineIDOf(run, person uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(lineNamespace, []byte(run.String()+":"+person.String()))
}

// QueuePaymentsReady queues the payment lines of a run Fyniq answered "posted", when the link is active: nothing is queued standalone, for a refused answer, or while nothing is owed. The lines version is
// the run's own counter, which goes up with each sending, so Fyniq revises what changed. Called in the transaction that keeps Fyniq's answer.
func QueuePaymentsReady(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, run uuid.UUID) (bool, error) {
	if !linkActive(ctx, tx) {
		return false, nil
	}
	var v int
	if err := tx.QueryRow(ctx, `INSERT INTO nemura.link_object_version (object_type, object_id, version) VALUES ('payments_ready', $1, 1)
ON CONFLICT (organisation_id, object_type, object_id) DO UPDATE SET version = nemura.link_object_version.version + 1 RETURNING version`, run.String()).Scan(&v); err != nil {
		return false, err
	}
	return QueueLinkEvent(ctx, tx, org, at, TypePaymentsReady, "payroll_run", run.String(), v)
}

// paymentsReadyAt builds the payload from the run, the people's confirmed banks and the cipher, and nothing else. It runs in the worker's signing transaction: the account of each line is opened for it, each
// read audited as the link's service, and nothing of it is kept, logged or queued. False when no version of the run was posted or no person has a line.
func paymentsReadyAt(ctx context.Context, tx pgx.Tx, c ports.Cipher, org uuid.UUID, at time.Time, run uuid.UUID) (PaymentsReadyEvent, bool, error) {
	ev := PaymentsReadyEvent{RunID: run}
	if c == nil {
		return ev, false, errors.New("the link's worker has no cipher: payment lines cannot be built")
	}
	err := tx.QueryRow(ctx, `SELECT max(version) FROM nemura.link_run_posting WHERE run_id = $1 AND status = 'posted'`, run).Scan(&ev.RunVersion)
	if err != nil || ev.RunVersion < 1 {
		return ev, false, nil
	}
	r, versions, err := RunOf(ctx, tx, run)
	if err != nil {
		return ev, false, err
	}
	var vid uuid.UUID
	for _, v := range versions {
		if v.Number == ev.RunVersion {
			vid = v.ID
		}
	}
	if vid == uuid.Nil {
		return ev, false, nil
	}
	ev.PayDate = r.PayDate.Format(time.DateOnly)
	if err := tx.QueryRow(ctx, `SELECT approver_issuer, approver_subject, preparer_issuer, preparer_subject FROM nemura.link_run_approval WHERE run_id = $1 AND version = $2`, run, ev.RunVersion).
		Scan(&ev.Approver.Issuer, &ev.Approver.Subject, &ev.Preparer.Issuer, &ev.Preparer.Subject); err != nil {
		return ev, false, err
	}
	lines, err := RunLines(ctx, tx, vid)
	if err != nil {
		return ev, false, err
	}
	for _, l := range lines {
		owed := dec(l.Disburse)
		if owed.Sign() <= 0 {
			continue
		}
		if blocked, err := PaymentBlocked(ctx, tx, run, l.Person); err != nil {
			return ev, false, err
		} else if blocked {
			continue
		}
		var sealed []byte
		var code, name *string
		err := tx.QueryRow(ctx, `SELECT bank_sealed, bank_code, bank_name FROM nemura.person_secret WHERE person_id = $1 AND bank_sealed IS NOT NULL AND bank_code IS NOT NULL`, l.Person).Scan(&sealed, &code, &name)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // no bank with its code on record: no line, and the run page says why
		}
		if err != nil {
			return ev, false, err
		}
		version, err := bankVersionOf(ctx, tx, l.Person)
		if err != nil || version < 1 {
			return ev, false, err
		}
		chg, ok, err := bankChangeAt(ctx, tx, l.Person, version)
		if err != nil {
			return ev, false, err
		}
		if !ok {
			continue // the version names no confirmed change here: no line rather than a line for an account Nemura cannot show was approved
		}
		change, fp, changeCode := chg.ID, chg.Fingerprint, chg.Code
		if changeCode == nil || *changeCode != *code { // what is on record is what the confirmed change approved, or no line
			continue
		}
		if err := Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "link"}, "nemura.restricted.read", audit.Object{Type: "person", ID: l.Person},
			map[string]audit.Change{"capability": {nil, "salary bank account for a payment line"}, "level": {nil, "full"}}, ""); err != nil {
			return ev, false, err
		}
		plain, err := c.Open(ctx, sealed, aad(org, l.Person, "bank"))
		if err != nil {
			return ev, false, fmt.Errorf("a bank account could not be opened for a payment line: %w", err)
		}
		ev.Lines = append(ev.Lines, PaymentLineEvent{LineID: LineIDOf(run, l.Person), PersonID: l.Person, PayeeName: l.PersonName, BankCode: *code, BankName: *name, AccountName: l.PersonName,
			AccountNumber: string(plain), Amount: cents2(owed), BankDetailID: change, BankDetailVersion: version, BankDetailDigest: BankDetailDigest(change, fp, *code)})
	}
	return ev, len(ev.Lines) > 0, nil
}

// requeuePaymentsFor sends the payment lines of each run again that was sent with a line for the person, when no payment of theirs is past "scheduled": their bank detail changed, so Fyniq blocks the old line
// and needs the new one (FR-PPL-14). Called in the transaction that confirms the change; the lines are rebuilt at send time from what is on record then.
func requeuePaymentsFor(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, person uuid.UUID) (int, error) {
	if !linkActive(ctx, tx) {
		return 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT v.object_id FROM nemura.link_object_version v WHERE v.object_type = 'payments_ready' AND NOT EXISTS (
SELECT 1 FROM nemura.pay_payment_event e WHERE e.run_id::text = v.object_id AND e.person_id = $1 AND e.state <> 'scheduled')`, person)
	if err != nil {
		return 0, err
	}
	var runs []uuid.UUID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		if run, err := uuid.Parse(id); err == nil {
			runs = append(runs, run)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, run := range runs {
		if ok, err := QueuePaymentsReady(ctx, tx, org, at, run); err != nil {
			return n, err
		} else if ok {
			n++
		}
	}
	return n, nil
}

// requeuePayments asks again, at the version each run has, for the payment lines a replay named: no version moves, so Fyniq applies what it missed and answers unchanged to what it holds.
func requeuePayments(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, run uuid.UUID) (bool, error) {
	var v int
	if err := tx.QueryRow(ctx, `SELECT version FROM nemura.link_object_version WHERE object_type = 'payments_ready' AND object_id = $1`, run.String()).Scan(&v); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return QueueLinkEvent(ctx, tx, org, at, TypePaymentsReady, "payroll_run", run.String(), v)
}
