package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) BankMatching() storage.BankMatching { return bankMatching{u} }

type bankMatching struct{ u *unitOfWork }

func toRule(r sqlcdb.FyniqBankRule) storage.BankRule {
	return storage.BankRule{ID: r.ID, Name: r.Name, Position: int(r.Position), Conditions: r.Conditions, Action: r.Action, Active: r.Active,
		FromAIProposal: r.FromAiProposal, AutoApply: r.AutoApply, AutoApplyApprovalID: r.AutoApplyApprovalID, CreatedBy: r.CreatedBy,
		CreatedAt: r.CreatedAt, ChangedAt: r.ChangedAt, Version: int(r.Version)}
}

func toMatch(r sqlcdb.FyniqBankMatch) storage.BankMatch {
	return storage.BankMatch{ID: r.ID, LineID: r.LineID, Kind: r.Kind, DocumentID: r.DocumentID, EntryID: r.EntryID, TransferID: r.TransferID,
		Basis: r.Basis, RuleID: r.RuleID, MadeBy: r.MadeBy, MadeAt: r.MadeAt, UndoneBy: str(r.UndoneBy), UndoneAt: r.UndoneAt,
		UndoReason: str(r.UndoReason)}
}

func toTransfer(r sqlcdb.FyniqBankTransfer) storage.BankTransfer {
	return storage.BankTransfer{ID: r.ID, FromAccountID: r.FromAccountID, ToAccountID: r.ToAccountID, FromLineID: r.FromLineID,
		ToLineID: r.ToLineID, OnDate: r.OnDate, FromCurrency: r.FromCurrency, FromAmount: storage.Decimal(r.FromAmount),
		ToCurrency: r.ToCurrency, ToAmount: storage.Decimal(r.ToAmount), Fee: toDecPtr(r.Fee), EntryID: r.EntryID, MadeBy: r.MadeBy,
		MadeAt: r.MadeAt, UndoneBy: str(r.UndoneBy), UndoneAt: r.UndoneAt, UndoReason: str(r.UndoReason), Version: int(r.Version)}
}

func toSignal(r sqlcdb.FyniqBankSignal) storage.BankSignal {
	return storage.BankSignal{ID: r.ID, MoneyAccountID: r.MoneyAccountID, Amount: storage.Decimal(r.Amount), Currency: r.Currency,
		Payer: str(r.Payer), Reference: str(r.Reference), ReceivedAt: r.ReceivedAt, InboundMessageID: r.InboundMessageID,
		InvoiceID: r.InvoiceID, Status: r.Status, ClosedLineID: r.ClosedLineID, ClosedAt: r.ClosedAt}
}

func toReconciliation(r sqlcdb.FyniqBankReconciliation) storage.BankReconciliation {
	return storage.BankReconciliation{ID: r.ID, MoneyAccountID: r.MoneyAccountID, PeriodID: r.PeriodID, Currency: r.Currency,
		StatementBalance: storage.Decimal(r.StatementBalance), BookBalance: storage.Decimal(r.BookBalance),
		UnreconciledTotal: storage.Decimal(r.UnreconciledTotal), UnreconciledCount: int(r.UnreconciledCount),
		Difference: storage.Decimal(r.Difference), LedgerRevision: r.LedgerRevision, PreparedBy: r.PreparedBy, PreparedAt: r.PreparedAt,
		SignedBy: str(r.SignedBy), SignedAt: r.SignedAt, Version: int(r.Version)}
}

func idText(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

func (r bankMatching) CreateRule(ctx context.Context, n storage.NewBankRule) (storage.BankRule, error) {
	if err := r.u.check(); err != nil {
		return storage.BankRule{}, err
	}
	row, err := r.u.q.InsertBankRule(ctx, sqlcdb.InsertBankRuleParams{Name: n.Name, Position: small(n.Position), Conditions: n.Conditions,
		Action: n.Action, Active: n.Active, FromAiProposal: n.FromAIProposal, By: r.u.actor(ctx), At: n.At})
	if err != nil {
		return storage.BankRule{}, mapError(err)
	}
	out := toRule(row)
	return out, r.u.event(ctx, "bank_rule.created", "bank_rule", out.ID, &out.Version, map[string][2]any{"name": {nil, out.Name},
		"position": {nil, out.Position}, "conditions": {nil, string(out.Conditions)}, "action": {nil, string(out.Action)},
		"active": {nil, out.Active}, "from_ai_proposal": {nil, out.FromAIProposal}})
}

func (r bankMatching) ChangeRule(ctx context.Context, id uuid.UUID, version int, n storage.NewBankRule) (storage.BankRule, error) {
	before, err := r.Rule(ctx, id)
	if err != nil {
		return before, err
	}
	row, err := r.u.q.UpdateBankRule(ctx, sqlcdb.UpdateBankRuleParams{Name: n.Name, Position: small(n.Position), Conditions: n.Conditions,
		Action: n.Action, Active: n.Active, By: r.u.actor(ctx), At: n.At, ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankRule{}, &storage.Error{Code: storage.CodeVersion, Message: "the bank rule changed since it was read"}
	}
	if err != nil {
		return storage.BankRule{}, mapError(err)
	}
	after := toRule(row)
	changes := map[string][2]any{}
	if before.Name != after.Name {
		changes["name"] = [2]any{before.Name, after.Name}
	}
	if before.Position != after.Position {
		changes["position"] = [2]any{before.Position, after.Position}
	}
	if string(before.Conditions) != string(after.Conditions) {
		changes["conditions"] = [2]any{string(before.Conditions), string(after.Conditions)}
	}
	if string(before.Action) != string(after.Action) {
		changes["action"] = [2]any{string(before.Action), string(after.Action)}
	}
	if before.Active != after.Active {
		changes["active"] = [2]any{before.Active, after.Active}
	}
	return after, r.u.event(ctx, "bank_rule.changed", "bank_rule", id, &after.Version, changes)
}

func (r bankMatching) SetRuleAutoApply(ctx context.Context, id uuid.UUID, version int, approval uuid.UUID, at time.Time) (storage.BankRule, error) {
	if err := r.u.check(); err != nil {
		return storage.BankRule{}, err
	}
	row, err := r.u.q.SetBankRuleAutoApply(ctx, sqlcdb.SetBankRuleAutoApplyParams{ApprovalID: &approval, By: r.u.actor(ctx), At: at, ID: id,
		Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankRule{}, &storage.Error{Code: storage.CodeVersion, Message: "the bank rule changed since it was read"}
	}
	if err != nil {
		return storage.BankRule{}, mapError(err)
	}
	out := toRule(row)
	return out, r.u.event(ctx, "bank_rule.auto_apply", "bank_rule", id, &out.Version, map[string][2]any{"auto_apply": {false, true},
		"approval_id": {nil, approval.String()}})
}

func (r bankMatching) Rules(ctx context.Context) ([]storage.BankRule, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.BankRules(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BankRule, 0, len(rows))
	for _, x := range rows {
		out = append(out, toRule(x))
	}
	return out, nil
}

func (r bankMatching) Rule(ctx context.Context, id uuid.UUID) (storage.BankRule, error) {
	if err := r.u.check(); err != nil {
		return storage.BankRule{}, err
	}
	row, err := r.u.q.BankRule(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankRule{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.BankRule{}, mapError(err)
	}
	return toRule(row), nil
}

func (r bankMatching) MakeMatch(ctx context.Context, n storage.NewBankMatch) (storage.BankMatch, error) {
	if err := r.u.check(); err != nil {
		return storage.BankMatch{}, err
	}
	row, err := r.u.q.InsertBankMatch(ctx, sqlcdb.InsertBankMatchParams{LineID: n.LineID, Kind: n.Kind, DocumentID: n.DocumentID,
		EntryID: n.EntryID, TransferID: n.TransferID, Basis: n.Basis, RuleID: n.RuleID, By: r.u.actor(ctx), At: n.At})
	if err != nil {
		return storage.BankMatch{}, mapError(err)
	}
	out := toMatch(row)
	return out, r.u.event(ctx, "bank_line.match_made", "bank_line", n.LineID, nil, map[string][2]any{"match.kind": {nil, out.Kind},
		"match.basis": {nil, out.Basis}, "match.document_id": {nil, idText(out.DocumentID)}, "match.entry_id": {nil, idText(out.EntryID)},
		"match.transfer_id": {nil, idText(out.TransferID)}, "match.rule_id": {nil, idText(out.RuleID)}})
}

func (r bankMatching) UndoMatch(ctx context.Context, id uuid.UUID, reason string, at time.Time) (storage.BankMatch, error) {
	if err := r.u.check(); err != nil {
		return storage.BankMatch{}, err
	}
	by := r.u.actor(ctx)
	row, err := r.u.q.UndoBankMatch(ctx, sqlcdb.UndoBankMatchParams{By: &by, At: &at, Reason: &reason, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankMatch{}, &storage.Error{Code: storage.CodeBanking, Message: "the match is undone already, or there is no such match"}
	}
	if err != nil {
		return storage.BankMatch{}, mapError(err)
	}
	out := toMatch(row)
	return out, r.u.event(ctx, "bank_line.match_undone", "bank_line", out.LineID, nil, map[string][2]any{"match.id": {out.ID.String(), nil},
		"undo_reason": {nil, reason}})
}

func (r bankMatching) Matches(ctx context.Context, f storage.BankMatchFilter) ([]storage.BankMatch, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.BankMatches(ctx, sqlcdb.BankMatchesParams{LineID: f.LineID, DocumentID: f.DocumentID, SystemOnly: f.SystemOnly, Since: f.Since,
		AllMatches: f.All})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BankMatch, 0, len(rows))
	for _, x := range rows {
		out = append(out, toMatch(x))
	}
	return out, nil
}

func (r bankMatching) RecordTransfer(ctx context.Context, n storage.NewBankTransfer) (storage.BankTransfer, error) {
	if err := r.u.check(); err != nil {
		return storage.BankTransfer{}, err
	}
	row, err := r.u.q.InsertBankTransfer(ctx, sqlcdb.InsertBankTransferParams{FromAccountID: n.FromAccountID, ToAccountID: n.ToAccountID,
		FromLineID: n.FromLineID, ToLineID: n.ToLineID, OnDate: n.OnDate, FromAmount: string(n.FromAmount), ToAmount: string(n.ToAmount),
		Fee: decPtr(n.Fee), EntryID: n.EntryID, By: r.u.actor(ctx), At: n.At, FromCurrency: n.FromCurrency, ToCurrency: n.ToCurrency})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankTransfer{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("the currencies %s and %s are not both known", n.FromCurrency, n.ToCurrency)}
	}
	if err != nil {
		return storage.BankTransfer{}, mapError(err)
	}
	out := toTransfer(row)
	changes := map[string][2]any{"from_account_id": {nil, out.FromAccountID.String()}, "to_account_id": {nil, out.ToAccountID.String()},
		"on": {nil, day(out.OnDate)}, "from": {nil, string(out.FromAmount) + " " + out.FromCurrency}, "to": {nil, string(out.ToAmount) + " " + out.ToCurrency},
		"from_line_id": {nil, idText(out.FromLineID)}, "to_line_id": {nil, idText(out.ToLineID)}, "entry_id": {nil, idText(out.EntryID)}}
	if out.Fee != nil {
		changes["fee"] = [2]any{nil, string(*out.Fee)}
	}
	return out, r.u.event(ctx, "bank_transfer.recorded", "bank_transfer", out.ID, &out.Version, changes)
}

func (r bankMatching) CompleteTransfer(ctx context.Context, id uuid.UUID, version int, fromLine, toLine, entry *uuid.UUID) (storage.BankTransfer, error) {
	before, err := r.Transfer(ctx, id)
	if err != nil {
		return before, err
	}
	row, err := r.u.q.CompleteBankTransfer(ctx, sqlcdb.CompleteBankTransferParams{FromLineID: fromLine, ToLineID: toLine, EntryID: entry, ID: id,
		Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankTransfer{}, &storage.Error{Code: storage.CodeVersion, Message: "the transfer changed since it was read"}
	}
	if err != nil {
		return storage.BankTransfer{}, mapError(err)
	}
	after := toTransfer(row)
	changes := map[string][2]any{}
	if idText(before.FromLineID) != idText(after.FromLineID) {
		changes["from_line_id"] = [2]any{nil, idText(after.FromLineID)}
	}
	if idText(before.ToLineID) != idText(after.ToLineID) {
		changes["to_line_id"] = [2]any{nil, idText(after.ToLineID)}
	}
	if idText(before.EntryID) != idText(after.EntryID) {
		changes["entry_id"] = [2]any{nil, idText(after.EntryID)}
	}
	return after, r.u.event(ctx, "bank_transfer.completed", "bank_transfer", id, &after.Version, changes)
}

func (r bankMatching) UndoTransfer(ctx context.Context, id uuid.UUID, version int, reason string, at time.Time) (storage.BankTransfer, error) {
	if err := r.u.check(); err != nil {
		return storage.BankTransfer{}, err
	}
	by := r.u.actor(ctx)
	row, err := r.u.q.UndoBankTransfer(ctx, sqlcdb.UndoBankTransferParams{UndoneBy: &by, UndoneAt: &at, UndoReason: &reason, ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankTransfer{}, &storage.Error{Code: storage.CodeVersion, Message: "the transfer changed since it was read"}
	}
	if err != nil {
		return storage.BankTransfer{}, mapError(err)
	}
	after := toTransfer(row)
	return after, r.u.event(ctx, "bank_transfer.undone", "bank_transfer", id, &after.Version,
		map[string][2]any{"undone_at": {nil, at.UTC().Format(time.RFC3339Nano)}, "undo_reason": {nil, reason}})
}

func (r bankMatching) Transfer(ctx context.Context, id uuid.UUID) (storage.BankTransfer, error) {
	if err := r.u.check(); err != nil {
		return storage.BankTransfer{}, err
	}
	row, err := r.u.q.BankTransfer(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankTransfer{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.BankTransfer{}, mapError(err)
	}
	return toTransfer(row), nil
}

func (r bankMatching) InTransit(ctx context.Context) ([]storage.BankTransfer, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.BankTransfersInTransit(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BankTransfer, 0, len(rows))
	for _, x := range rows {
		out = append(out, toTransfer(x))
	}
	return out, nil
}

func (r bankMatching) RecordSignal(ctx context.Context, n storage.NewBankSignal) (storage.BankSignal, error) {
	if err := r.u.check(); err != nil {
		return storage.BankSignal{}, err
	}
	row, err := r.u.q.InsertBankSignal(ctx, sqlcdb.InsertBankSignalParams{MoneyAccountID: n.MoneyAccountID, Amount: string(n.Amount),
		Payer: opt(n.Payer), Reference: opt(n.Reference), ReceivedAt: n.ReceivedAt, InboundMessageID: n.InboundMessageID,
		InvoiceID: n.InvoiceID, Currency: n.Currency})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankSignal{}, &storage.Error{Code: storage.CodeInvalid, Message: "the signal's currency " + n.Currency + " is not known"}
	}
	if err != nil {
		return storage.BankSignal{}, mapError(err)
	}
	out := toSignal(row)
	return out, r.u.event(ctx, "bank_signal.received", "bank_signal", out.ID, nil, map[string][2]any{"amount": {nil, string(out.Amount) + " " + out.Currency},
		"invoice_id": {nil, idText(out.InvoiceID)}})
}

func (r bankMatching) CloseSignal(ctx context.Context, id uuid.UUID, status string, line *uuid.UUID, at time.Time) (storage.BankSignal, error) {
	if err := r.u.check(); err != nil {
		return storage.BankSignal{}, err
	}
	row, err := r.u.q.CloseBankSignal(ctx, sqlcdb.CloseBankSignalParams{Status: status, ClosedLineID: line, At: &at, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankSignal{}, &storage.Error{Code: storage.CodeBanking, Message: "the signal is closed already, or there is no such signal"}
	}
	if err != nil {
		return storage.BankSignal{}, mapError(err)
	}
	out := toSignal(row)
	return out, r.u.event(ctx, "bank_signal."+status, "bank_signal", id, nil, map[string][2]any{"status": {storage.SignalPending, status},
		"closed_line_id": {nil, idText(line)}})
}

func (r bankMatching) PendingSignals(ctx context.Context) ([]storage.BankSignal, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.PendingBankSignals(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BankSignal, 0, len(rows))
	for _, x := range rows {
		out = append(out, toSignal(x))
	}
	return out, nil
}

func (r bankMatching) Signals(ctx context.Context) ([]storage.BankSignal, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.BankSignals(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BankSignal, 0, len(rows))
	for _, x := range rows {
		out = append(out, toSignal(x))
	}
	return out, nil
}

func (r bankMatching) Signal(ctx context.Context, id uuid.UUID) (storage.BankSignal, error) {
	if err := r.u.check(); err != nil {
		return storage.BankSignal{}, err
	}
	row, err := r.u.q.BankSignal(ctx, id)
	if err != nil {
		return storage.BankSignal{}, mapError(err)
	}
	return toSignal(row), nil
}

func reconciliationChanges(rec storage.BankReconciliation) map[string][2]any {
	return map[string][2]any{"statement_balance": {nil, string(rec.StatementBalance)}, "book_balance": {nil, string(rec.BookBalance)},
		"unreconciled_total": {nil, string(rec.UnreconciledTotal)}, "unreconciled_count": {nil, rec.UnreconciledCount},
		"difference": {nil, string(rec.Difference)}, "ledger_revision": {nil, rec.LedgerRevision}}
}

func (r bankMatching) PrepareReconciliation(ctx context.Context, n storage.NewBankReconciliation) (storage.BankReconciliation, error) {
	existing, err := r.Reconciliation(ctx, n.MoneyAccountID, n.PeriodID)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return storage.BankReconciliation{}, err
	}
	by := r.u.actor(ctx)
	var row sqlcdb.FyniqBankReconciliation
	if errors.Is(err, ports.ErrNotFound) {
		row, err = r.u.q.InsertBankReconciliation(ctx, sqlcdb.InsertBankReconciliationParams{MoneyAccountID: n.MoneyAccountID, PeriodID: n.PeriodID,
			StatementBalance: string(n.StatementBalance), BookBalance: string(n.BookBalance), UnreconciledTotal: string(n.UnreconciledTotal),
			UnreconciledCount: small(n.UnreconciledCount), Difference: string(n.Difference), LedgerRevision: n.LedgerRevision, By: by, At: n.At,
			Currency: n.Currency})
		if errors.Is(err, pgx.ErrNoRows) {
			return storage.BankReconciliation{}, &storage.Error{Code: storage.CodeInvalid, Message: "the reconciliation's currency " + n.Currency + " is not known"}
		}
	} else {
		row, err = r.u.q.ReprepareBankReconciliation(ctx, sqlcdb.ReprepareBankReconciliationParams{StatementBalance: string(n.StatementBalance),
			BookBalance: string(n.BookBalance), UnreconciledTotal: string(n.UnreconciledTotal), UnreconciledCount: small(n.UnreconciledCount),
			Difference: string(n.Difference), LedgerRevision: n.LedgerRevision, By: by, At: n.At, ID: existing.ID})
	}
	if err != nil {
		return storage.BankReconciliation{}, mapError(err)
	}
	out := toReconciliation(row)
	return out, r.u.event(ctx, "bank_reconciliation.prepared", "bank_reconciliation", out.ID, &out.Version, reconciliationChanges(out))
}

func (r bankMatching) SignReconciliation(ctx context.Context, id uuid.UUID, version int, mfaAt, at time.Time) (storage.BankReconciliation, error) {
	if err := r.u.check(); err != nil {
		return storage.BankReconciliation{}, err
	}
	by := r.u.actor(ctx)
	row, err := r.u.q.SignBankReconciliation(ctx, sqlcdb.SignBankReconciliationParams{By: &by, At: &at, MfaAt: &mfaAt, ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankReconciliation{}, &storage.Error{Code: storage.CodeVersion, Message: "the reconciliation changed since it was read"}
	}
	if err != nil {
		return storage.BankReconciliation{}, mapError(err)
	}
	out := toReconciliation(row)
	return out, r.u.event(ctx, "bank_reconciliation.signed", "bank_reconciliation", id, &out.Version, map[string][2]any{"signed_by": {nil, by}})
}

func (r bankMatching) Reconciliations(ctx context.Context, periodID uuid.UUID) ([]storage.BankReconciliation, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.BankReconciliations(ctx, periodID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BankReconciliation, 0, len(rows))
	for _, x := range rows {
		out = append(out, toReconciliation(x))
	}
	return out, nil
}

func (r bankMatching) Reconciliation(ctx context.Context, moneyAccountID, periodID uuid.UUID) (storage.BankReconciliation, error) {
	if err := r.u.check(); err != nil {
		return storage.BankReconciliation{}, err
	}
	row, err := r.u.q.BankReconciliation(ctx, sqlcdb.BankReconciliationParams{MoneyAccountID: moneyAccountID, PeriodID: periodID})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankReconciliation{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.BankReconciliation{}, mapError(err)
	}
	return toReconciliation(row), nil
}
