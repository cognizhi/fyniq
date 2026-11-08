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
