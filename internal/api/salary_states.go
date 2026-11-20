package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/salarybatch"
	"github.com/cognizhi/fyniq/internal/linksend"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ paid means paid (FR-PPL-12; p1-payroll-link task 6.2)
//
// A release makes a line "released" and nothing more: a file written and a journal posted are never "paid". A line is paid only when the bank feed's debit is matched to it (matchSalaryBankLine) or
// when a person records the outcome with evidence (recordSalaryLineState), and neither is done by whoever prepared or approved the run in Nemura (SOD-4). Every state of a line goes back to Nemura as
// payroll.payments.status in the same unit of work (the outbox row names the state, and the instruction ID, the amount, the date and the evidence's ID are read from their sources when it is sent:
// design D5). An unknown outcome blocks any new instruction for the person in the run, by file or by API, until a person resolves it.

// SalaryBankMatch matches the one bank line that debited a release of a salary batch to the lines it paid.
type SalaryBankMatch struct {
	BankLineID string   `json:"bank_line_id" format:"uuid" doc:"The statement line: a debit of the batch's own bank account, waiting in the Categorise queue." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c40"`
	LineIDs    []string `json:"line_ids" minItems:"1" maxItems:"500" doc:"The payment lines of this batch it paid: released and not yet paid, rejected or returned. Their amounts make the debit exactly." example:"[\"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c11\"]"`
}

type (
	salaryMatchInput struct {
		SalaryBatchPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body SalaryBankMatch
	}
)

// salaryEvidence is nil when the evidence file exists and is not removed.
func salaryEvidence(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) error {
	e, err := u.Evidence().Get(ctx, id)
	if err != nil || e.RemovedAt != nil {
		return problem(salarybatch.CodeState, "The evidence is not a file of this organisation, or it was removed.", salaryFixes[salarybatch.CodeState])
	}
	return nil
}

// recordSalaryState records a state of a released line, queues it for Nemura and returns the state's ID.
func (a *API) recordSalaryState(ctx context.Context, u storage.UnitOfWork, an salaryAnalysis, line storage.SalaryLine, instruction string, e storage.SalaryLineEvent) error {
	e.BatchID, e.LineRef, e.PersonID, e.RunID, e.InstructionID, e.At = an.batch.ID, line.LineRef, line.PersonID, an.batch.RunID, instruction, a.deps.Clock.Now()
	amount := line.Amount
	e.Amount = &amount
	id, err := u.SalaryBatches().AddEvent(ctx, e)
	if err != nil {
		return err
	}
	n := 0
	events, err := u.SalaryBatches().Events(ctx, an.batch.ID)
	if err != nil {
		return err
	}
	for _, x := range events {
		if x.LineRef == line.LineRef {
			n++
		}
	}
	_, err = linksend.QueueStatus(ctx, u, line.LineRef, n, id)
	return err
}

// releasedLine is a line of the batch, by Nemura's ID, that a release took, with its instruction ID; a problem when there is none.
func (a *API) releasedLine(ctx context.Context, u storage.UnitOfWork, an salaryAnalysis, ref uuid.UUID) (storage.SalaryLine, string, error) {
	rls, err := u.SalaryBatches().ReleaseLines(ctx, an.batch.ID)
	if err != nil {
		return storage.SalaryLine{}, "", err
	}
	for _, l := range an.lines {
		if l.LineRef != ref {
			continue
		}
		for _, r := range rls {
			if r.LineRef == ref {
				return l, r.InstructionID, nil
			}
		}
		return storage.SalaryLine{}, "", salaryProblem(salarybatch.CodeState, "The line was not released, so nothing became of its payment yet.")
	}
	return storage.SalaryLine{}, "", salaryProblem(salarybatch.CodeLine, "The batch has no such line.")
}

// notRunParty refuses whoever prepared or approved the run in Nemura (SOD-4) from saying what became of its payments.
func (a *API) notRunParty(ctx context.Context, u storage.UnitOfWork, batch, user uuid.UUID) error {
	party, err := u.SalaryBatches().IsRunParty(ctx, batch, user)
	if err != nil {
		return err
	}
	if party {
		return salaryProblem(salarybatch.CodeSeparation, "You prepared or approved this pay run in Nemura, so another person says what became of its payments (SOD-4).")
	}
	return nil
}

func registerSalaryStates(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.RecordSalaryLineState, huma.Operation{
		OperationID: "recordSalaryLineState", Method: http.MethodPost, Path: "/v1/salary-batches/{batch_id}/lines/{line_id}/state", Tags: []string{"People"},
		Summary: "Record what became of a salary payment, with evidence",
		Description: "Records a state of a released line (FR-PPL-12): sent, accepted, paid, rejected, returned or unknown, from a person, with a file of evidence (a bank advice or a statement) and the day it happened. " +
			"The move must be one a payment makes (PRD-002 NM-PAY-10): paid goes back only to returned, an unknown outcome is resolved only to paid, rejected or returned, and a line not released has nothing " +
			"to record. An exported file or a release never makes a line paid, and a bank feed's own match is matchSalaryBankLine. An unknown outcome blocks any new instruction for the person in the run, " +
			"by file or by API, until it is resolved. Not for whoever prepared or approved the run in Nemura (SOD-4). Needs a person who passed step-up. Side effects: the state, its audit event (the state, " +
			"its source and IDs, never an amount) and payroll.payments.status queued for Nemura in the same transaction. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid, CodeSessionRequired, CodeStepUpRequired, salarybatch.CodeState, salarybatch.CodeLine,
		salarybatch.CodeSeparation, storage.CodeSalaryBatch}, locks...), func(ctx context.Context, in *salaryStateInput) (*salaryWriteOutput, error) {
		id, err := parseID("batch_id", in.BatchID)
		if err != nil {
			return nil, err
		}
		ref, err := parseID("line_id", in.LineID)
		if err != nil {
			return nil, err
		}
		evidence, err := parseID("body.evidence_id", in.Body.EvidenceID)
		if err != nil {
			return nil, err
		}
		day, err := time.Parse(time.DateOnly, in.Body.On)
		if err != nil {
			return nil, salaryProblem(salarybatch.CodeState, "The day is not a date.")
		}
		p, err := a.steppedUpPerson(ctx, "Recording what became of a salary payment")
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (SalaryBatch, error) {
			b, err := a.salaryBatch(ctx, u, id, true)
			if err != nil {
				return SalaryBatch{}, err
			}
			if err := a.notRunParty(ctx, u, id, p.UserID); err != nil {
				return SalaryBatch{}, err
			}
			if today, err := orgToday(ctx, a, u); err != nil {
				return SalaryBatch{}, err
			} else if day.After(today) && in.Body.State != storage.LineAccepted && in.Body.State != storage.LineSent {
				return SalaryBatch{}, salaryProblem(salarybatch.CodeState, "The day is in the future: say when it happened.")
			}
			if err := salaryEvidence(ctx, u, evidence); err != nil {
				return SalaryBatch{}, err
			}
			an, err := a.analyseSalary(ctx, u, b)
			if err != nil {
				return SalaryBatch{}, err
			}
			line, instruction, err := a.releasedLine(ctx, u, an, ref)
			if err != nil {
				return SalaryBatch{}, err
			}
			from := an.latest[ref].State
			if !salarybatch.CanMove(from, in.Body.State) || in.Body.State == storage.LineScheduled || in.Body.State == storage.LineReleased {
				return SalaryBatch{}, salaryProblem(salarybatch.CodeState, fmt.Sprintf("A payment that is %s does not become %s.", from, in.Body.State))
			}
			if err := a.recordSalaryState(ctx, u, an, line, instruction, storage.SalaryLineEvent{State: in.Body.State, Source: storage.LineByManual, EvidenceID: evidence, OnDate: &day,
				RecordedBy: "user:" + p.UserID.String()}); err != nil {
				return SalaryBatch{}, err
			}
			return a.salaryOf(ctx, u, p, b, true)
		})
		if err != nil {
			return nil, err
		}
		return &salaryWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, w.Body.Status), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.MatchSalaryBankLine, huma.Operation{
		OperationID: "matchSalaryBankLine", Method: http.MethodPost, Path: "/v1/salary-batches/{batch_id}/bank-match", Tags: []string{"People"},
		Summary: "Match the bank debit of a salary release and mark its lines paid",
		Description: "Matches one statement line of the batch's own bank account, a debit waiting in the Categorise queue, to the released lines it paid, and marks them paid from the bank feed (FR-PPL-12). Its " +
			"amount must be exactly the sum of the lines named (a bulk debit of a release, or one line's own debit), in SGD, and each line must be released and not yet paid, rejected or returned. The line is posted " +
			"to 2100 Salaries payable (Dr 2100, Cr the bank account) as categorising it does, with no person on a line (FR-PAY-13), and each paid line goes back to Nemura as payroll.payments.status with the " +
			"bank line's date. Not for whoever prepared or approved the run in Nemura (SOD-4). Needs a person who passed step-up, and If-Match with the batch's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid, CodeSessionRequired, CodeStepUpRequired, salarybatch.CodeState, salarybatch.CodeLine,
		salarybatch.CodeSeparation, salarybatch.CodeAccount, storage.CodeSalaryBatch}, append(locks, postingCodes()...)...), func(ctx context.Context, in *salaryMatchInput) (*salaryWriteOutput, error) {
		id, err := parseID("batch_id", in.BatchID)
		if err != nil {
			return nil, err
		}
		lineID, err := parseID("body.bank_line_id", in.Body.BankLineID)
		if err != nil {
			return nil, err
		}
		var refs []uuid.UUID
		seen := map[uuid.UUID]bool{}
		for _, s := range in.Body.LineIDs {
			r, err := parseID("body.line_ids", s)
			if err != nil {
				return nil, err
			}
			if seen[r] {
				return nil, salaryProblem(salarybatch.CodeLine, "A line is named twice.")
			}
			seen[r] = true
			refs = append(refs, r)
		}
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		p, err := a.steppedUpPerson(ctx, "Matching a salary payment to its bank line")
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (SalaryBatch, error) {
			b, err := a.salaryBatch(ctx, u, id, true)
			if err != nil {
				return SalaryBatch{}, err
			}
			if err := checkIfMatch(in.IfMatch, salaryETag(b)); err != nil {
				return SalaryBatch{}, err
			}
			if err := a.notRunParty(ctx, u, id, p.UserID); err != nil {
				return SalaryBatch{}, err
			}
			bl, err := u.Banking().Line(ctx, lineID)
			if err != nil {
				if err == ports.ErrNotFound {
					return SalaryBatch{}, problem(CodeNotFound, "There is no such bank line in this organisation.", "Choose a line from the Categorise queue")
				}
				return SalaryBatch{}, err
			}
			if b.PayDate == nil {
				return SalaryBatch{}, salaryProblem(salarybatch.CodeNotPrepared, "The batch has no bank account yet.")
			}
			macc, err := salaryMoneyAccount(ctx, u, b.MoneyAccountID)
			if err != nil {
				return SalaryBatch{}, err
			}
			if bl.MoneyAccountID != macc.ID || bl.Currency != salarybatch.SGD.Code || bl.Status != storage.LineUnreconciled {
				return SalaryBatch{}, salaryProblem(salarybatch.CodeAccount, "The statement line is not a waiting SGD line of the batch's own bank account.")
			}
			debit, err := money.ParseStored(string(bl.Amount), salarybatch.SGD)
			if err != nil || debit.Sign() >= 0 {
				return SalaryBatch{}, salaryProblem(salarybatch.CodeAccount, "The statement line is not a debit.")
			}
			debit = debit.Neg()
			an, err := a.analyseSalary(ctx, u, b)
			if err != nil {
				return SalaryBatch{}, err
			}
			total := money.Zero(salarybatch.SGD)
			type paid struct {
				line        storage.SalaryLine
				instruction string
			}
			var lines []paid
			for _, ref := range refs {
				line, instruction, err := a.releasedLine(ctx, u, an, ref)
				if err != nil {
					return SalaryBatch{}, err
				}
				if from := an.latest[ref].State; !salarybatch.CanMove(from, storage.LinePaid) {
					return SalaryBatch{}, salaryProblem(salarybatch.CodeState, fmt.Sprintf("A payment that is %s does not become paid.", from))
				}
				amount, err := salarybatch.Amount(line)
				if err != nil {
					return SalaryBatch{}, err
				}
				if total, err = total.Add(amount); err != nil {
					return SalaryBatch{}, err
				}
				lines = append(lines, paid{line, instruction})
			}
			if same, err := total.Cmp(debit); err != nil {
				return SalaryBatch{}, err
			} else if same != 0 {
				return SalaryBatch{}, salaryProblem(salarybatch.CodeAccount, "The bank line's amount is not the sum of the lines named.")
			}
			settings, err := journal.LoadSettings(ctx, u)
			if err != nil {
				return SalaryBatch{}, err
			}
			var payable uuid.UUID
			for _, acc := range settings.Accounts {
				if acc.Role == ledger.RoleSalariesPayable {
					payable = acc.ID
				}
			}
			if payable == uuid.Nil {
				return SalaryBatch{}, problem(ledger.CodeSystemRole, "The chart has no salaries payable account (2100).", "Load the standard chart, or give an account the role "+ledger.RoleSalariesPayable)
			}
			org, err := u.Organisation(ctx)
			if err != nil {
				return SalaryBatch{}, err
			}
			// The debit is posted to 2100 by the line's own categorisation (Dr 2100, Cr the bank account), with no person on it: the unit of work is this one.
			if _, err := a.bank.Categorise(ctx, org.ID, journal.Categorisation{Draft: banktransaction.Draft{LineID: lineID, Reference: salaryNumber(b),
				Parts: []banktransaction.Part{{AccountID: payable, GSTCode: "NA"}}}, Basis: storage.BasisPerson, At: a.deps.Clock.Now()}); err != nil {
				return SalaryBatch{}, bankProblems("The bank line cannot be posted to 2100", err)
			}
			day := bl.BookingDate
			for _, x := range lines {
				if err := a.recordSalaryState(ctx, u, an, x.line, x.instruction, storage.SalaryLineEvent{State: storage.LinePaid, Source: storage.LineByBankFeed, BankLineID: lineID, OnDate: &day,
					RecordedBy: "user:" + p.UserID.String()}); err != nil {
					return SalaryBatch{}, err
				}
			}
			return a.salaryOf(ctx, u, p, b, true)
		})
		if err != nil {
			return nil, err
		}
		return &salaryWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, w.Body.Status), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}

type salaryStateInput struct {
	SalaryBatchLinePath
	KeyHeader
	PreferHeader
	Body SalaryLineState
}
