package banking

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The line commands (FR-BNK-04, FR-BNK-06, FR-BNK-14; p1-bank-import-reconcile task 9.1): a possible duplicate kept or
// excluded, a wrong statement's line excluded, and a reconciled or excluded line put back in the queue, whatever it
// was reconciled to.

// CodeLineState is a line that is not where the command needs it: a keep of a line that is no possible duplicate, an
// exclusion of a reconciled line, or an undo of a line that waits already.
const CodeLineState = "BANK-11"

// status is a line's status as a message says it.
func status(s string) string { return strings.ReplaceAll(s, "_", " ") }

// Keep keeps a possible duplicate as a line of its own, at version: it joins the queue (C.2 #51).
func Keep(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, version int, at time.Time) (storage.BankLine, error) {
	l, err := u.Banking().Line(ctx, id)
	if err != nil {
		return l, err
	}
	if l.Status != storage.LinePossibleDuplicate {
		return l, refuse(CodeLineState, "Only a possible duplicate is kept this way; the line is in the queue already or reconciled",
			"the line is %s, not a possible duplicate", status(l.Status))
	}
	return u.Banking().SetLineStatus(ctx, id, version, storage.LineUnreconciled, at)
}

// Exclude takes a waiting line out of the queue at version, posting nothing, with its reason: a duplicate confirmed, or
// a line of a statement imported in error. It is put back with Unreconcile while its period is open.
func Exclude(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, version int, reason string, at time.Time) (storage.BankLine, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return storage.BankLine{}, refuse(CodeRequest, "Say why the line is excluded, such as the line it duplicates", "the exclusion has no reason")
	}
	l, err := u.Banking().Line(ctx, id)
	if err != nil {
		return l, err
	}
	if l.Status != storage.LinePossibleDuplicate && l.Status != storage.LineUnreconciled {
		return l, refuse(CodeLineState, "Undo what the line was reconciled to first, then exclude it",
			"the line is %s; only a line waiting in the queue is excluded", status(l.Status))
	}
	return u.Banking().SetLineStatus(storage.WithReason(ctx, reason), id, version, storage.LineExcluded, at)
}

// Unreconcile puts a line back in the queue with a reason, while its period is open: a categorisation is reversed, a
// receipt or bill payment made from the line voided, a match to money recorded by hand undone, a transfer undone with
// its other side, and an excluded line included again. A line in suspense is classified instead (journal.CodeNotCategory).
// With systemOnly, only what the system reconciled is undone (CodeNotAuto), as the Auto-reconciled list does.
func Unreconcile(ctx context.Context, txns journal.BankTransactions, org, lineID uuid.UUID, reason string, at time.Time, systemOnly bool) (storage.BankLine, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return storage.BankLine{}, refuse(CodeRequest, "Say why the line goes back to the queue", "the undo has no reason")
	}
	var (
		line storage.BankLine
		m    *storage.BankMatch
	)
	if err := txns.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if line, err = u.Banking().Line(ctx, lineID); err != nil {
			return err
		}
		matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{LineID: &lineID})
		if len(matches) > 0 {
			m = &matches[0]
		}
		return err
	}); err != nil {
		return line, err
	}
	switch {
	case m == nil && line.Status == storage.LineExcluded && !systemOnly:
		err := txns.Store.Do(storage.WithReason(ctx, reason), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			line, err = u.Banking().SetLineStatus(ctx, line.ID, line.Version, storage.LineUnreconciled, at)
			return err
		})
		return line, err
	case m == nil:
		return line, refuse(CodeLineState, "Nothing is undone: the line waits in the queue already", "the line is %s", status(line.Status))
	case systemOnly && m.Basis == storage.BasisPerson:
		return line, refuse(CodeNotAuto, "Undo it where it was made: the categorisation, the match or the transfer.", "the line was not reconciled by the system")
	}
	var err error
	switch m.Kind {
	case storage.MatchReceipt, storage.MatchBillPayment, storage.MatchRecorded:
		_, err = txns.Unmatch(ctx, org, lineID, reason, at)
	case storage.MatchTransfer:
		_, err = txns.UndoTransfer(ctx, org, *m.TransferID, reason, at)
	default: // a categorisation, or a posting to suspense, which Undo refuses
		_, err = txns.Undo(ctx, org, lineID, reason, at)
	}
	if err != nil {
		return line, err
	}
	err = txns.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		line, err = u.Banking().Line(ctx, lineID)
		return err
	})
	return line, err
}
