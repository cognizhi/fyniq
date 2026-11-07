package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the register of share issues (FR-EQ-08)

func (u *unitOfWork) ShareIssues() storage.ShareIssues { return shareIssues{u} }

type shareIssues struct{ u *unitOfWork }

func toShareIssue(r sqlcdb.FyniqShareIssue) storage.ShareIssue {
	out := storage.ShareIssue{ID: r.ID, HolderID: r.HolderID, ClassAccountID: r.ClassAccountID, Shares: storage.Decimal(r.Shares), Price: storage.Decimal(r.Price),
		Currency: r.Currency, Amount: storage.Decimal(r.Amount), Consideration: r.Consideration, ResolutionRefs: r.ResolutionRefs, EffectiveOn: r.EffectiveOn,
		SettlementID: r.SettlementID, EntryID: r.EntryID, RecordedBy: r.RecordedBy, RecordedAt: r.RecordedAt, ClassID: r.ClassID}
	if r.LodgementRef != nil {
		out.LodgementRef = *r.LodgementRef
	}
	return out
}

func (r shareIssues) Record(ctx context.Context, x storage.ShareIssue) (storage.ShareIssue, error) {
	if err := r.u.check(); err != nil {
		return storage.ShareIssue{}, err
	}
	var lodgement *string
	if x.LodgementRef != "" {
		lodgement = &x.LodgementRef
	}
	row, err := r.u.q.InsertShareIssue(ctx, sqlcdb.InsertShareIssueParams{ID: x.ID, HolderID: x.HolderID, ClassAccountID: x.ClassAccountID, Shares: string(x.Shares),
		Price: string(x.Price), Amount: string(x.Amount), Consideration: x.Consideration, ResolutionRefs: x.ResolutionRefs, LodgementRef: lodgement,
		EffectiveOn: x.EffectiveOn, SettlementID: x.SettlementID, EntryID: x.EntryID, RecordedBy: x.RecordedBy, RecordedAt: x.RecordedAt, Currency: x.Currency,
		ClassID: x.ClassID})
	if err != nil {
		return storage.ShareIssue{}, mapError(err)
	}
	out := toShareIssue(row)
	issue := map[string]any{"holder_id": out.HolderID.String(), "class_account_id": out.ClassAccountID.String(), "shares": string(out.Shares),
		"price": string(out.Price), "amount": map[string]any{"amount": string(out.Amount), "currency": out.Currency}, "consideration": out.Consideration,
		"resolution_refs": out.ResolutionRefs, "lodgement_ref": out.LodgementRef, "effective_on": out.EffectiveOn.Format("2006-01-02")}
	if out.SettlementID != nil {
		issue["settlement_id"] = out.SettlementID.String()
	}
	if out.ClassID != nil {
		issue["class_id"] = out.ClassID.String()
	}
	if out.EntryID != nil {
		issue["entry_id"] = out.EntryID.String()
	}
	if err := r.u.event(ctx, "share_issue.recorded", "share_issue", out.ID, nil, map[string][2]any{"issue": {nil, issue}}); err != nil {
		return storage.ShareIssue{}, err
	}
	return out, nil
}

func (r shareIssues) List(ctx context.Context) ([]storage.ShareIssue, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ShareIssues(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ShareIssue, 0, len(rows))
	for _, x := range rows {
		out = append(out, toShareIssue(x))
	}
	return out, nil
}

func (r shareIssues) OfSettlement(ctx context.Context, settlementID uuid.UUID) (*storage.ShareIssue, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ShareIssueOfSettlement(ctx, &settlementID)
	if err != nil || len(rows) == 0 {
		return nil, mapError(err)
	}
	out := toShareIssue(rows[0])
	return &out, nil
}

// ------------------------------------------------------------------ holding movements (FR-EQ-17)

func (u *unitOfWork) Holdings() storage.Holdings { return holdings{u} }

type holdings struct{ u *unitOfWork }

func toMovement(r sqlcdb.FyniqHoldingMovement) storage.HoldingMovement {
	return storage.HoldingMovement{ID: r.ID, Kind: r.Kind, TransactionID: r.TransactionID, ShareIssueID: r.ShareIssueID, ReversesID: r.ReversesID,
		HolderID: r.HolderID, ClassAccountID: r.ClassAccountID, ClassID: r.ClassID, Shares: storage.Decimal(r.Shares), EffectiveOn: r.EffectiveOn,
		RecordedBy: r.RecordedBy, RecordedAt: r.RecordedAt}
}

func (h holdings) Move(ctx context.Context, m storage.HoldingMovement) (storage.HoldingMovement, error) {
	if err := h.u.check(); err != nil {
		return storage.HoldingMovement{}, err
	}
	row, err := h.u.q.InsertHoldingMovement(ctx, sqlcdb.InsertHoldingMovementParams{ID: m.ID, Kind: m.Kind, TransactionID: m.TransactionID,
		ShareIssueID: m.ShareIssueID, ReversesID: m.ReversesID, HolderID: m.HolderID, ClassAccountID: m.ClassAccountID, ClassID: m.ClassID,
		Shares: string(m.Shares), EffectiveOn: m.EffectiveOn, RecordedBy: m.RecordedBy, RecordedAt: m.RecordedAt})
	if err != nil {
		return storage.HoldingMovement{}, mapError(err)
	}
	out := toMovement(row)
	moved := map[string]any{"kind": out.Kind, "holder_id": out.HolderID.String(), "class_account_id": out.ClassAccountID.String(),
		"shares": string(out.Shares), "effective_on": out.EffectiveOn.Format("2006-01-02")}
	for k, v := range map[string]*uuid.UUID{"transaction_id": out.TransactionID, "share_issue_id": out.ShareIssueID, "reverses_id": out.ReversesID,
		"class_id": out.ClassID} {
		if v != nil {
			moved[k] = v.String()
		}
	}
	if err := h.u.event(ctx, "holding.moved", "holding_movement", out.ID, nil, map[string][2]any{"movement": {nil, moved}}); err != nil {
		return storage.HoldingMovement{}, err
	}
	return out, nil
}

func (h holdings) List(ctx context.Context) ([]storage.HoldingMovement, error) {
	if err := h.u.check(); err != nil {
		return nil, err
	}
	rows, err := h.u.q.HoldingMovements(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.HoldingMovement, 0, len(rows))
	for _, x := range rows {
		out = append(out, toMovement(x))
	}
	return out, nil
}

func (h holdings) OfTransaction(ctx context.Context, transactionID uuid.UUID) ([]storage.HoldingMovement, error) {
	if err := h.u.check(); err != nil {
		return nil, err
	}
	rows, err := h.u.q.HoldingMovementsOfTransaction(ctx, &transactionID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.HoldingMovement, 0, len(rows))
	for _, x := range rows {
		out = append(out, toMovement(x))
	}
	return out, nil
}
