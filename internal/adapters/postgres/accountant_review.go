package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the accountant's review (FM-7)

func (u *unitOfWork) AccountantReviews() storage.AccountantReviews { return accountantReviews{u} }

type accountantReviews struct{ u *unitOfWork }

func toAccountantReview(r sqlcdb.FyniqAccountantReview, closedAt time.Time, hash []byte) storage.AccountantReview {
	return storage.AccountantReview{ID: r.ID, PeriodID: r.PeriodID, CloseID: r.CloseID, ClosedAt: closedAt, CloseHash: hash, OpenedBy: r.OpenedBy,
		OpenedAt: r.OpenedAt, Status: r.Status, SignedBy: r.SignedBy, SignedAt: r.SignedAt, Statement: str(r.Statement), SupersededAt: r.SupersededAt,
		ReopenID: r.ReopenID, Version: int(r.Version)}
}

// withParts adds each review's queries and journals.
func (r accountantReviews) withParts(ctx context.Context, out []storage.AccountantReview) ([]storage.AccountantReview, error) {
	qs, err := r.u.q.ListAccountantReviewQueries(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	js, err := r.u.q.ListAccountantReviewJournals(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	at := map[uuid.UUID]int{}
	for i, x := range out {
		at[x.ID] = i
	}
	for _, q := range qs {
		if i, ok := at[q.ReviewID]; ok {
			out[i].Queries = append(out[i].Queries, storage.ReviewQuery{ID: q.ID, AskedBy: q.AskedBy, AskedAt: q.AskedAt, Question: q.Question,
				AnsweredBy: q.AnsweredBy, AnsweredAt: q.AnsweredAt, Answer: str(q.Answer)})
		}
	}
	for _, j := range js {
		if i, ok := at[j.ReviewID]; ok {
			out[i].Journals = append(out[i].Journals, storage.ReviewJournal{JournalID: j.JournalID, LinkedBy: j.LinkedBy, LinkedAt: j.LinkedAt})
		}
	}
	return out, nil
}

func (r accountantReviews) List(ctx context.Context) ([]storage.AccountantReview, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListAccountantReviews(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AccountantReview, len(rows))
	for i, x := range rows {
		out[i] = toAccountantReview(x.FyniqAccountantReview, x.ClosedAt, x.SnapshotHash)
	}
	return r.withParts(ctx, out)
}

func (r accountantReviews) Get(ctx context.Context, id uuid.UUID) (storage.AccountantReview, error) {
	if err := r.u.check(); err != nil {
		return storage.AccountantReview{}, err
	}
	row, err := r.u.q.GetAccountantReview(ctx, id)
	if err != nil {
		return storage.AccountantReview{}, mapError(err)
	}
	out, err := r.withParts(ctx, []storage.AccountantReview{toAccountantReview(row.FyniqAccountantReview, row.ClosedAt, row.SnapshotHash)})
	if err != nil {
		return storage.AccountantReview{}, err
	}
	return out[0], nil
}

func (r accountantReviews) Open(ctx context.Context, periodID, closeID, by uuid.UUID, at time.Time) (storage.AccountantReview, error) {
	if err := r.u.check(); err != nil {
		return storage.AccountantReview{}, err
	}
	row, err := r.u.q.InsertAccountantReview(ctx, sqlcdb.InsertAccountantReviewParams{PeriodID: periodID, CloseID: closeID, OpenedBy: by, OpenedAt: at})
	if err != nil {
		return storage.AccountantReview{}, mapError(err)
	}
	v := int(row.Version)
	if err := r.u.event(ctx, "accountant_review.opened", "accountant_review", row.ID, &v, map[string][2]any{
		"period_id": {nil, periodID}, "close_id": {nil, closeID}, "opened_by": {nil, by}, "opened_at": {nil, at.UTC().Format(time.RFC3339Nano)}}); err != nil {
		return storage.AccountantReview{}, err
	}
	return r.Get(ctx, row.ID)
}

// bump moves a review in review to its next version: CodeVersion when it changed since or is no longer in review.
func (r accountantReviews) bump(ctx context.Context, id uuid.UUID, version int) (int, error) {
	row, err := r.u.q.BumpAccountantReview(ctx, sqlcdb.BumpAccountantReviewParams{ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, &storage.Error{Code: storage.CodeVersion, Message: "the review changed, or is no longer in review, since it was read"}
	}
	if err != nil {
		return 0, mapError(err)
	}
	return int(row.Version), nil
}

func (r accountantReviews) Ask(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, at time.Time, question string) (storage.AccountantReview, error) {
	if err := r.u.check(); err != nil {
		return storage.AccountantReview{}, err
	}
	v, err := r.bump(ctx, id, version)
	if err != nil {
		return storage.AccountantReview{}, err
	}
	q, err := r.u.q.InsertAccountantReviewQuery(ctx, sqlcdb.InsertAccountantReviewQueryParams{ReviewID: id, AskedBy: by, AskedAt: at, Question: question})
	if err != nil {
		return storage.AccountantReview{}, mapError(err)
	}
	if err := r.u.event(ctx, "accountant_review.asked", "accountant_review", id, &v, map[string][2]any{
		"query": {nil, map[string]any{"id": q.String(), "asked_by": by.String(), "question": question}}}); err != nil {
		return storage.AccountantReview{}, err
	}
	return r.Get(ctx, id)
}

func (r accountantReviews) Answer(ctx context.Context, id uuid.UUID, version int, query, by uuid.UUID, at time.Time, answer string) (storage.AccountantReview, error) {
	if err := r.u.check(); err != nil {
		return storage.AccountantReview{}, err
	}
	v, err := r.bump(ctx, id, version)
	if err != nil {
		return storage.AccountantReview{}, err
	}
	_, err = r.u.q.AnswerAccountantReviewQuery(ctx, sqlcdb.AnswerAccountantReviewQueryParams{AnsweredBy: by, AnsweredAt: at, Answer: answer, ID: query, ReviewID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.AccountantReview{}, &storage.Error{Code: storage.CodeAccountantReview, Message: "the query is answered already, or is not the review's"}
	}
	if err != nil {
		return storage.AccountantReview{}, mapError(err)
	}
	if err := r.u.event(ctx, "accountant_review.answered", "accountant_review", id, &v, map[string][2]any{
		"answer": {nil, map[string]any{"query_id": query.String(), "answered_by": by.String(), "answer": answer}}}); err != nil {
		return storage.AccountantReview{}, err
	}
	return r.Get(ctx, id)
}

func (r accountantReviews) Link(ctx context.Context, id uuid.UUID, version int, journal, by uuid.UUID, at time.Time) (storage.AccountantReview, error) {
	if err := r.u.check(); err != nil {
		return storage.AccountantReview{}, err
	}
	v, err := r.bump(ctx, id, version)
	if err != nil {
		return storage.AccountantReview{}, err
	}
	if err := r.u.q.InsertAccountantReviewJournal(ctx, sqlcdb.InsertAccountantReviewJournalParams{ReviewID: id, JournalID: journal, LinkedBy: by, LinkedAt: at}); err != nil {
		return storage.AccountantReview{}, mapError(err)
	}
	if err := r.u.event(ctx, "accountant_review.journal_linked", "accountant_review", id, &v, map[string][2]any{
		"journal_id": {nil, journal.String()}, "linked_by": {nil, by.String()}}); err != nil {
		return storage.AccountantReview{}, err
	}
	return r.Get(ctx, id)
}

func (r accountantReviews) SignOff(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, at time.Time, statement string) (storage.AccountantReview, error) {
	if err := r.u.check(); err != nil {
		return storage.AccountantReview{}, err
	}
	row, err := r.u.q.SignOffAccountantReview(ctx, sqlcdb.SignOffAccountantReviewParams{SignedBy: by, SignedAt: at, Statement: statement, ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.AccountantReview{}, &storage.Error{Code: storage.CodeVersion, Message: "the review changed, or is no longer in review, since it was read"}
	}
	if err != nil {
		return storage.AccountantReview{}, mapError(err)
	}
	v := int(row.Version)
	if err := r.u.event(ctx, "accountant_review.signed_off", "accountant_review", id, &v, map[string][2]any{
		"status": {storage.AccountantReviewInReview, storage.AccountantReviewSignedOff}, "signed_by": {nil, by.String()},
		"signed_at": {nil, at.UTC().Format(time.RFC3339Nano)}, "statement": {nil, statement}}); err != nil {
		return storage.AccountantReview{}, err
	}
	return r.Get(ctx, id)
}

func (r accountantReviews) Supersede(ctx context.Context, closeID, reopenID uuid.UUID, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	rows, err := r.u.q.SupersedeAccountantReviews(ctx, sqlcdb.SupersedeAccountantReviewsParams{SupersededAt: at, ReopenID: reopenID, CloseID: closeID})
	if err != nil {
		return mapError(err)
	}
	for _, x := range rows {
		v := int(x.Version)
		if err := r.u.event(ctx, "accountant_review.superseded", "accountant_review", x.ID, &v, map[string][2]any{
			"status": {nil, storage.AccountantReviewSuperseded}, "reopen_id": {nil, reopenID.String()}, "superseded_at": {nil, at.UTC().Format(time.RFC3339Nano)}}); err != nil {
			return err
		}
	}
	return nil
}
