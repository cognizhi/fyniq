package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) AIProposalTrail() storage.AIProposalTrail { return aiTrail{u} }

type aiTrail struct{ u *unitOfWork }

func toUndo(r sqlcdb.AIProposalUndoOfRow) storage.AIProposalUndo {
	return storage.AIProposalUndo{ID: r.ID, ProposalID: r.ProposalID, UndoneBy: r.UndoneBy, UndoneAt: r.UndoneAt, Reason: r.Reason, Reversed: r.Reversed}
}

func (r aiTrail) Undo(ctx context.Context, proposal, by uuid.UUID, at time.Time, reason string, reversed json.RawMessage) (storage.AIProposalUndo, error) {
	if err := r.u.check(); err != nil {
		return storage.AIProposalUndo{}, err
	}
	if _, ok, err := r.UndoOf(ctx, proposal); err != nil {
		return storage.AIProposalUndo{}, err
	} else if ok {
		return storage.AIProposalUndo{}, &storage.Error{Code: storage.CodeVersion, Message: "the proposal was undone already"}
	}
	row, err := r.u.q.InsertAIProposalUndo(ctx, sqlcdb.InsertAIProposalUndoParams{ProposalID: proposal, UndoneBy: by, UndoneAt: at, Reason: reason, Reversed: reversed})
	if err != nil {
		return storage.AIProposalUndo{}, mapError(err)
	}
	out := toUndo(sqlcdb.AIProposalUndoOfRow(row))
	return out, r.u.event(ctx, "ai_proposal.undone", "ai_proposal", proposal, nil, map[string][2]any{"undone_by": {nil, by.String()}, "reason": {nil, reason}, "reversed": {nil, string(reversed)}})
}

func (r aiTrail) UndoOf(ctx context.Context, proposal uuid.UUID) (storage.AIProposalUndo, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.AIProposalUndo{}, false, err
	}
	rows, err := r.u.q.AIProposalUndoOf(ctx, proposal)
	if err != nil || len(rows) == 0 {
		return storage.AIProposalUndo{}, false, mapError(err)
	}
	return toUndo(rows[0]), true, nil
}

func (r aiTrail) Viewed(ctx context.Context, proposal, user uuid.UUID, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	return mapError(r.u.q.InsertAIProposalView(ctx, sqlcdb.InsertAIProposalViewParams{ProposalID: proposal, UserID: user, FirstViewedAt: at}))
}

func (r aiTrail) QualityRows(ctx context.Context, from, to time.Time) ([]storage.AIQualityRow, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AIQualityRows(ctx, sqlcdb.AIQualityRowsParams{FromAt: from, ToAt: to})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIQualityRow, 0, len(rows))
	for _, x := range rows {
		out = append(out, storage.AIQualityRow{ID: x.ID, TaskType: x.TaskType, Status: x.Status, ProposedAt: x.ProposedAt, DecidedBy: x.DecidedBy, DecidedAt: x.DecidedAt,
			ApprovedWhole: x.ApprovedSha256 != nil && *x.ApprovedSha256 == x.PayloadSha256, Reason: x.Reason, FirstViewedAt: x.FirstViewedAt})
	}
	return out, nil
}

func toReview(r sqlcdb.AIQualityReviewOfRow) storage.AIQualityReview {
	return storage.AIQualityReview{ID: r.ID, Month: r.Month, SignedBy: r.SignedBy, SignedAt: r.SignedAt, Report: r.Report, Note: str(r.Note)}
}

func (r aiTrail) Review(ctx context.Context, month time.Time, by uuid.UUID, at time.Time, report json.RawMessage, note string) (storage.AIQualityReview, error) {
	if err := r.u.check(); err != nil {
		return storage.AIQualityReview{}, err
	}
	if _, ok, err := r.ReviewOf(ctx, month); err != nil {
		return storage.AIQualityReview{}, err
	} else if ok {
		return storage.AIQualityReview{}, &storage.Error{Code: storage.CodeVersion, Message: "the month's review was signed already"}
	}
	row, err := r.u.q.InsertAIQualityReview(ctx, sqlcdb.InsertAIQualityReviewParams{Month: month, SignedBy: by, SignedAt: at, Report: report, Note: optional(note)})
	if err != nil {
		return storage.AIQualityReview{}, mapError(err)
	}
	out := toReview(sqlcdb.AIQualityReviewOfRow(row))
	return out, r.u.event(ctx, "ai_quality_review.signed", "ai_quality_review", out.ID, nil, map[string][2]any{"month": {nil, month.Format("2006-01")}, "signed_by": {nil, by.String()}})
}

func (r aiTrail) ReviewOf(ctx context.Context, month time.Time) (storage.AIQualityReview, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.AIQualityReview{}, false, err
	}
	rows, err := r.u.q.AIQualityReviewOf(ctx, month)
	if err != nil || len(rows) == 0 {
		return storage.AIQualityReview{}, false, mapError(err)
	}
	return toReview(rows[0]), true, nil
}
