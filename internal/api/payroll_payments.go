package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/bankcode"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/domain/salarybatch"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the payment lines of a posted pay run (FR-PAY-11, FR-PPL-12, FR-PPL-14; p1-payroll-link task 6.1)
//
// Pay reaches Fyniq twice: as the run's department totals (payroll.run.approved, which posts) and as one payment line per person (payroll.payments.ready, which makes a salary batch). The lines
// come through the link's client with the scope payroll.payments.submit and the link key's signature, like every event. They are accepted only for the run version Fyniq posted last, for the
// preparer and approver it was posted under, for people of the projection, and for no more than the run's net pay on 2100. Each line's payee, bank, account name and number are sealed on arrival
// and appear in no audit event, log, outbox row or error (FR-PPL-17). A later submission for the same run version with a higher object version revises lines: each revised line is a new version,
// which makes the batch's approval stale (FR-PPL-14).

// PayrollPaymentsReceipt is what became of a submission of payment lines.
type PayrollPaymentsReceipt struct {
	EventID string `json:"event_id" format:"uuid" doc:"The event's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Outcome string `json:"outcome" enum:"received,revised,unchanged,duplicate" doc:"received: a salary batch was made for the run; revised: lines were replaced by newer versions and the batch must be approved again; unchanged: the submission states what Fyniq holds, or is older; duplicate: the event was received before." example:"received"`
	BatchID string `json:"batch_id,omitempty" format:"uuid" doc:"The salary batch." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Number  string `json:"number,omitempty" doc:"Its number." example:"SB-0003"`
	Lines   int    `json:"lines,omitempty" doc:"How many lines are new or revised by this submission." example:"12"`
}

type (
	payrollPaymentsOutput struct{ Body PayrollPaymentsReceipt }
	payrollPaymentsInput  struct {
		RawBody []byte
		Body    PeopleLinkEvent
	}
)

var payFixes = map[string]string{
	peoplelink.CodePayLines:   "Send the lines of the run exactly as PRD-002 Appendix E lists them, for the preparer and approver the run was posted under",
	peoplelink.CodePayRun:     "Send the lines for the version of the run Fyniq posted last, after posting it",
	peoplelink.CodePayTotal:   "Send lines that pay no more than the run's net pay",
	peoplelink.CodePayPerson:  "Send the people's events first, so that each person of a line is in the projection",
	peoplelink.CodePayRelease: "A released line is not changed: record what became of its payment, or send a new run version",
	peoplelink.CodePayBlocked: "Resolve the person's unknown outcome in Fyniq first: record whether the money was paid, rejected or returned",
}

func payRefusal(code, detail string) *Problem { return problem(code, detail, payFixes[code]) }

func registerPayrollPayments(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.ReceivePayrollPayments, exemptByEventID(huma.Operation{
		OperationID: "receivePayrollPayments", Method: http.MethodPost, Path: "/v1/people-link/payroll-payments", Tags: []string{"People"},
		Summary: "Receive the payment lines of a posted pay run from Nemura",
		Description: "Nemura's `payroll.payments.ready` (PRD-002 Appendix E): the link's envelope carrying one payment line per person for a run Fyniq posted: the person, the amount, the payee's bank and " +
			"account (sealed here, never in an audit, a log or an outbox row) and the bank-detail record, version and digest each came from (FR-PPL-14). It needs the link's client and the scope " +
			"payroll.payments.submit. Fyniq checks the link's signature and epoch, that the version is the one it posted last under the same preparer and approver, that each person is in the " +
			"projection, that no unknown outcome blocks the person in this run and that the lines pay no more than the run's net pay on 2100; it then makes the run's salary batch, which a person " +
			"prepares, another approves and the Payment releaser releases (FR-PAY-11). A later submission for the same run version with a higher object version revises lines (each a new " +
			"version, and the batch is approved again); a line that was released is not changed. Side effects: the batch with its sealed lines and the inbox row, each audited with counts " +
			"and IDs only. Idempotent on the event ID (a repeat answers duplicate). Only the link's client may call it; on the never-list for the AI and MCP (§9.6).",
	}), append([]string{CodeNoLink, CodeLinkClient, CodeLinkMinimum, peoplelink.CodeMalformed, peoplelink.CodeSignature, peoplelink.CodeDigest, peoplelink.CodeReplay,
		peoplelink.CodeEpoch, peoplelink.CodeOrg, peoplelink.CodePayLines, peoplelink.CodePayRun, peoplelink.CodePayTotal, peoplelink.CodePayPerson, peoplelink.CodePayRelease,
		peoplelink.CodePayBlocked, storage.CodeSalaryBatch, payee.CodeNoKeys}, locks...), func(ctx context.Context, in *payrollPaymentsInput) (*payrollPaymentsOutput, error) {
		return a.receivePayrollPayments(ctx, in)
	})
}

// receivePayrollPayments verifies and stores the lines of one run version.
func (a *API) receivePayrollPayments(ctx context.Context, in *payrollPaymentsInput) (*payrollPaymentsOutput, error) {
	p, _ := authn.From(ctx)
	var env peoplelink.Envelope
	if err := json.NewDecoder(bytes.NewReader(in.RawBody)).Decode(&env); err != nil {
		return nil, problem(peoplelink.CodeMalformed, "The envelope is not well formed.", linkFixes[peoplelink.CodeMalformed])
	}
	if env.Header.Type != peoplelink.TypePayrollPaymentsReady {
		return nil, problem(peoplelink.CodeMalformed, fmt.Sprintf("This operation takes %q, not %q.", peoplelink.TypePayrollPaymentsReady, env.Header.Type), "Send the event to the operation that takes its type.")
	}
	if a.deps.Fields == nil {
		return nil, payee.NoKeys()
	}
	now := a.deps.Clock.Now()
	var out PayrollPaymentsReceipt
	var runID uuid.UUID
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		l, refusal, err := a.linkedEnvelope(ctx, u, p, env, now)
		if err != nil {
			return err
		}
		if refusal != nil {
			return refusal
		}
		eventID, err := uuid.Parse(env.Header.EventID)
		if err != nil {
			return linkRefusal(peoplelink.CodeMalformed, "The event ID is not an ID.")
		}
		entry := storage.InboxEntry{EventID: eventID, LinkID: l.ID, Epoch: env.Header.Epoch, Sequence: env.Header.Sequence, Type: env.Header.Type, ObjectType: env.Header.ObjectType,
			ObjectID: env.Header.ObjectID, ObjectVersion: env.Header.ObjectVersion, PayloadSHA256: env.Header.PayloadSHA256, Outcome: peoplelink.OutcomeApplied, ReceivedAt: now}
		if fresh, err := u.PeopleLinks().Receive(ctx, entry); err != nil {
			return err
		} else if !fresh {
			out = PayrollPaymentsReceipt{EventID: env.Header.EventID, Outcome: "duplicate"}
			return nil
		}
		ready, err := peoplelink.ParsePayments(env.Payload)
		if err != nil {
			var r *peoplelink.Refusal
			if errors.As(err, &r) {
				if r.Code == peoplelink.CodeMalformed {
					return linkRefusal(r.Code, r.Message+".")
				}
				return payRefusal(r.Code, r.Message+".")
			}
			return err
		}
		runID = ready.RunID
		if env.Header.ObjectVersion < 1 {
			return payRefusal(peoplelink.CodePayLines, "The lines' version is not valid.")
		}
		receipt, err := u.PayrollRuns().LatestReceipt(ctx, "nemura", ready.RunID)
		if errors.Is(err, ports.ErrNotFound) {
			return payRefusal(peoplelink.CodePayRun, "Fyniq has not posted this run.")
		}
		if err != nil {
			return err
		}
		if receipt.Version != ready.RunVersion {
			return payRefusal(peoplelink.CodePayRun, fmt.Sprintf("Version %d of the run is not the one Fyniq posted last (version %d).", ready.RunVersion, receipt.Version))
		}
		if ready.Approver != (peoplelink.Party{Issuer: receipt.ApproverIssuer, Subject: receipt.ApproverSubject}) || ready.Preparer != (peoplelink.Party{Issuer: receipt.PreparerIssuer, Subject: receipt.PreparerSubject}) {
			return payRefusal(peoplelink.CodePayLines, "The lines name another preparer or approver than the run was posted under.")
		}
		org, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		base, err := money.Lookup(org.BaseCurrency)
		if err != nil {
			return err
		}
		if base.Code != salarybatch.SGD.Code {
			return payRefusal(peoplelink.CodePayLines, "Salary batches pay in SGD.")
		}
		posted, err := ledger.DecodePayrollPosted(receipt.Version, receipt.Totals, base)
		if err != nil {
			return err
		}
		net, err := journal.PayrollNetPay(ctx, u, posted, base)
		if err != nil {
			return err
		}

		batch, err := u.SalaryBatches().OfRun(ctx, receipt.SourceSystem, ready.RunID, ready.RunVersion)
		exists := err == nil
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		var current []storage.SalaryLine
		released := map[uuid.UUID]bool{}
		settled := map[uuid.UUID]bool{} // a payment that was rejected or returned is no longer owed in this batch: a new line pays the person again
		if exists {
			if batch.Status == storage.SalaryCancelled {
				return payRefusal(peoplelink.CodePayRelease, "The salary batch of this run was cancelled.")
			}
			if env.Header.ObjectVersion <= batch.LinesVersion {
				out = PayrollPaymentsReceipt{EventID: env.Header.EventID, Outcome: "unchanged", BatchID: batch.ID.String(), Number: fmt.Sprintf("SB-%04d", batch.Seq)}
				return nil
			}
			if current, err = u.SalaryBatches().Lines(ctx, batch.ID); err != nil {
				return err
			}
			events, err := u.SalaryBatches().Events(ctx, batch.ID)
			if err != nil {
				return err
			}
			for ref, e := range salarybatch.Latest(events) {
				settled[ref] = e.State == storage.LineRejected || e.State == storage.LineReturned
			}
			rl, err := u.SalaryBatches().ReleaseLines(ctx, batch.ID)
			if err != nil {
				return err
			}
			for _, x := range rl {
				released[x.LineRef] = true
			}
		}
		byRef := map[uuid.UUID]storage.SalaryLine{}
		for _, c := range current {
			byRef[c.LineRef] = c
		}
		var fresh []storage.SalaryLine
		batchID := batch.ID
		for _, line := range ready.Lines {
			amount, err := money.Parse(line.Amount, salarybatch.SGD)
			if err != nil {
				return payRefusal(peoplelink.CodePayLines, "A line's amount is not in dollars and cents.")
			}
			person, err := u.PeopleLinks().Person(ctx, line.PersonID)
			if err != nil {
				return err
			}
			if !person.Known() || person.Removed {
				return payRefusal(peoplelink.CodePayPerson, "A line is for a person Fyniq does not know.")
			}
			states, err := u.SalaryBatches().StatesOfRunPerson(ctx, ready.RunID, line.PersonID)
			if err != nil {
				return err
			}
			for _, s := range states {
				if salarybatch.BlocksInstruction(s) {
					return payRefusal(peoplelink.CodePayBlocked, "A person of the lines has an unknown outcome in this run.")
				}
			}
			next := storage.SalaryLine{LineRef: line.LineID, LineVersion: 1, PersonID: line.PersonID, Amount: storage.Decimal(amount.String()), BankDetailID: line.BankDetailID,
				BankDetailVersion: line.BankDetailVersion, BankDetailDigest: line.BankDetailDigest, EventID: eventID, BatchID: batchID}
			if prior, ok := byRef[line.LineID]; ok {
				if prior.PersonID != line.PersonID {
					return payRefusal(peoplelink.CodePayLines, "A line names another person than it did before.")
				}
				same, err := amount.Cmp(mustAmount(prior))
				if err != nil {
					return err
				}
				if same == 0 && prior.BankDetailID == next.BankDetailID && prior.BankDetailVersion == next.BankDetailVersion && prior.BankDetailDigest == next.BankDetailDigest {
					continue // states what Fyniq holds: nothing to revise
				}
				if released[line.LineID] {
					return payRefusal(peoplelink.CodePayRelease, "A line that was released is not changed.")
				}
				next.LineVersion = prior.LineVersion + 1
			}
			// The BIC: Nemura's own when it sent one, else looked up from its bank code in Fyniq's table (design D26); a code the table does not have leaves it empty, and a person fills it in when preparing
			// the batch. A line that is revised keeps the BIC a person filled in for the same bank.
			bic := line.BankBIC
			if bic == "" {
				bic, _ = bankcode.BIC(line.BankCode, now)
			}
			if bic == "" {
				if prior, ok := byRef[line.LineID]; ok && prior.Sealed != nil {
					if clear, err := a.deps.Fields.Open(ctx, prior.Sealed, salarybatch.LineAAD(p.Organisation, prior.BatchID, prior.LineRef, prior.LineVersion)); err == nil {
						if was, err := salarybatch.ReadDetail(clear); err == nil && was.BankCode == line.BankCode && was.AccountNumber == line.AccountNumber {
							bic = was.BankBIC
						}
					}
				}
			}
			detail, err := salarybatch.Detail{PayeeName: line.PayeeName, BankBIC: bic, BankCode: line.BankCode, BankName: line.BankName, AccountName: line.AccountName, AccountNumber: line.AccountNumber}.Marshal()
			if err != nil {
				return err
			}
			// The line's own ID names it in the AAD, and the batch's ID is not known before the batch is made: a new batch seals under the ID the database gives it next, so it is sealed after Create.
			next.Sealed = detail
			fresh = append(fresh, next)
			byRef[line.LineID] = next
		}
		// The lines, as they will stand, pay no more than the run's net pay on 2100.
		total := money.Zero(salarybatch.SGD)
		for ref, x := range byRef {
			if settled[ref] {
				continue
			}
			v, err := salarybatch.Amount(x)
			if err != nil {
				return err
			}
			if total, err = total.Add(v); err != nil {
				return err
			}
		}
		if over, err := total.Cmp(net); err != nil {
			return err
		} else if over > 0 {
			return payRefusal(peoplelink.CodePayTotal, "The lines pay more than the run's net pay posted to 2100.")
		}
		if exists && len(fresh) == 0 {
			out = PayrollPaymentsReceipt{EventID: env.Header.EventID, Outcome: "unchanged", BatchID: batch.ID.String(), Number: fmt.Sprintf("SB-%04d", batch.Seq)}
			return nil
		}
		seal := func(b uuid.UUID) error {
			for i := range fresh {
				sealed, err := a.deps.Fields.Seal(ctx, fresh[i].Sealed, salarybatch.LineAAD(p.Organisation, b, fresh[i].LineRef, fresh[i].LineVersion))
				if err != nil {
					return err
				}
				fresh[i].Sealed, fresh[i].BatchID = sealed, b
			}
			return nil
		}
		if exists {
			if err := seal(batch.ID); err != nil {
				return err
			}
			b, err := u.SalaryBatches().Revise(ctx, batch.ID, env.Header.ObjectVersion, fresh, now)
			if err != nil {
				return err
			}
			out = PayrollPaymentsReceipt{EventID: env.Header.EventID, Outcome: "revised", BatchID: b.ID.String(), Number: fmt.Sprintf("SB-%04d", b.Seq), Lines: len(fresh)}
			return nil
		}
		seq, err := u.SalaryBatches().NextSeq(ctx)
		if err != nil {
			return err
		}
		// The batch needs its ID to bind its lines' seals to it, and the lines need their seals to be stored with it: the ID is chosen here and the database keeps it.
		b, err := a.createSalaryBatch(ctx, u, p.Organisation, storage.SalaryBatch{Seq: seq, LinkID: l.ID, SourceSystem: receipt.SourceSystem, RunID: ready.RunID, RunVersion: ready.RunVersion,
			ReceiptID: receipt.ID, WageMonth: receipt.WageMonth, LinesVersion: env.Header.ObjectVersion, CreatedAt: now}, fresh)
		if err != nil {
			return err
		}
		out = PayrollPaymentsReceipt{EventID: env.Header.EventID, Outcome: "received", BatchID: b.ID.String(), Number: fmt.Sprintf("SB-%04d", b.Seq), Lines: len(fresh)}
		return nil
	})
	if err != nil {
		// A refusal is audited in a transaction of its own, since the work was rolled back: the code and the event, never a name, an account or an amount.
		pr := problemFrom(ctx, err)
		code := pr.Code
		_ = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "salary_batch.refused", ObjectType: "payroll_run", ObjectID: cmpID(runID, env.Header.ObjectID),
				Changes: map[string][2]any{"refusal": {nil, map[string]any{"code": code, "event_id": env.Header.EventID, "lines_version": env.Header.ObjectVersion, "epoch": env.Header.Epoch}}}})
		})
		return nil, err
	}
	return &payrollPaymentsOutput{Body: out}, nil
}

// createSalaryBatch makes a batch and seals its lines under the batch's own ID. The batch is made first with no line, which the database allows within the transaction, and its lines follow: the batch
// is audited once with the count (the audit trigger is deferred to commit).
func (a *API) createSalaryBatch(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, b storage.SalaryBatch, lines []storage.SalaryLine) (storage.SalaryBatch, error) {
	made, err := u.SalaryBatches().Create(ctx, b)
	if err != nil {
		return storage.SalaryBatch{}, err
	}
	for i := range lines {
		sealed, err := a.deps.Fields.Seal(ctx, lines[i].Sealed, salarybatch.LineAAD(org, made.ID, lines[i].LineRef, lines[i].LineVersion))
		if err != nil {
			return storage.SalaryBatch{}, err
		}
		lines[i].Sealed, lines[i].BatchID = sealed, made.ID
	}
	if err := u.SalaryBatches().AddLines(ctx, made.ID, lines, b.CreatedAt); err != nil {
		return storage.SalaryBatch{}, err
	}
	return made, nil
}

// mustAmount reads a stored line's amount; a stored amount always parses, and zero stands for one that does not, which then differs from any line sent.
func mustAmount(l storage.SalaryLine) money.Money {
	m, err := salarybatch.Amount(l)
	if err != nil {
		return money.Zero(salarybatch.SGD)
	}
	return m
}
