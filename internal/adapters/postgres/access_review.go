package postgres

import (
	"context"
	"encoding/hex"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) AccessReviews() storage.AccessReviews { return accessReviews{u} }

type accessReviews struct{ u *unitOfWork }

func toAttestation(r sqlcdb.ListAccessReviewAttestationsRow) storage.AccessReviewAttestation {
	return storage.AccessReviewAttestation{ID: r.ID, Quarter: r.Quarter, AttestedBy: r.AttestedBy, AttestedAt: r.AttestedAt, Note: str(r.Note),
		Report: r.Report, ReportHash: r.ReportHash}
}

func (r accessReviews) List(ctx context.Context) ([]storage.AccessReviewAttestation, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListAccessReviewAttestations(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AccessReviewAttestation, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAttestation(row))
	}
	return out, nil
}

func (r accessReviews) Attest(ctx context.Context, n storage.NewAccessReviewAttestation) (storage.AccessReviewAttestation, error) {
	if err := r.u.check(); err != nil {
		return storage.AccessReviewAttestation{}, err
	}
	row, err := r.u.q.InsertAccessReviewAttestation(ctx, sqlcdb.InsertAccessReviewAttestationParams{Quarter: n.Quarter, AttestedBy: n.AttestedBy,
		AttestedAt: n.AttestedAt, Note: opt(n.Note), Report: n.Report})
	if err != nil {
		return storage.AccessReviewAttestation{}, mapError(err)
	}
	out := toAttestation(sqlcdb.ListAccessReviewAttestationsRow(row))
	return out, r.u.event(ctx, "access_review.attested", "access_review_attestation", out.ID, nil, map[string][2]any{
		"quarter": {nil, n.Quarter.Format("2006-01-02")}, "note": {nil, n.Note}, "report_sha256": {nil, hex.EncodeToString(out.ReportHash)}})
}

func (r accessReviews) SignIns(ctx context.Context) ([]storage.MemberSignIn, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.MemberSignIns(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MemberSignIn, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.MemberSignIn{UserID: row.UserID, LastSignIn: row.LastSignIn, StepUp: row.StepUpMethod})
	}
	return out, nil
}
