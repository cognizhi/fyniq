package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ Owner succession (FR-ADM-09)

func (u *unitOfWork) OwnerSuccessions() storage.OwnerSuccessions { return ownerSuccessions{u} }

type ownerSuccessions struct{ u *unitOfWork }

func toSuccession(r sqlcdb.FyniqOwnerSuccession) storage.OwnerSuccession {
	return storage.OwnerSuccession{ID: r.ID, DocumentID: r.DocumentID, OutgoingMembershipID: r.OutgoingMembershipID, OutgoingUserID: r.OutgoingUserID,
		SuccessorMembershipID: r.SuccessorMembershipID, Reason: r.Reason, EvidenceKind: r.EvidenceKind, StartedBy: r.StartedBy,
		StartedAt: r.StartedAt, DueAt: r.DueAt, Status: r.Status, EndedBy: r.EndedBy, EndedAt: r.EndedAt, Version: int(r.Version)}
}

func (r ownerSuccessions) List(ctx context.Context) ([]storage.OwnerSuccession, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListOwnerSuccessions(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.OwnerSuccession, len(rows))
	for i, x := range rows {
		out[i] = toSuccession(x)
	}
	return out, nil
}

func (r ownerSuccessions) Get(ctx context.Context, id uuid.UUID) (storage.OwnerSuccession, error) {
	if err := r.u.check(); err != nil {
		return storage.OwnerSuccession{}, err
	}
	row, err := r.u.q.GetOwnerSuccession(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.OwnerSuccession{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.OwnerSuccession{}, mapError(err)
	}
	return toSuccession(row), nil
}

func (r ownerSuccessions) Start(ctx context.Context, n storage.NewOwnerSuccession) (storage.OwnerSuccession, error) {
	if err := r.u.check(); err != nil {
		return storage.OwnerSuccession{}, err
	}
	row, err := r.u.q.InsertOwnerSuccession(ctx, sqlcdb.InsertOwnerSuccessionParams{DocumentID: n.DocumentID, OutgoingMembershipID: n.OutgoingMembershipID,
		OutgoingUserID: n.OutgoingUserID, SuccessorMembershipID: n.SuccessorMembershipID, Reason: n.Reason, EvidenceKind: n.EvidenceKind,
		StartedBy: n.StartedBy, StartedAt: n.StartedAt})
	if err != nil {
		return storage.OwnerSuccession{}, mapError(err)
	}
	out := toSuccession(row)
	v := out.Version
	changes := map[string][2]any{"outgoing_membership_id": {nil, out.OutgoingMembershipID}, "reason": {nil, out.Reason},
		"evidence_kind": {nil, out.EvidenceKind}, "started_by": {nil, out.StartedBy}, "due_at": {nil, out.DueAt.UTC().Format(time.RFC3339Nano)}}
	if out.SuccessorMembershipID != nil {
		changes["successor_membership_id"] = [2]any{nil, *out.SuccessorMembershipID}
	}
	return out, r.u.event(ctx, "owner_succession.started", "owner_succession", out.ID, &v, changes)
}

func (r ownerSuccessions) End(ctx context.Context, id uuid.UUID, version int, status string, by uuid.UUID, at time.Time) (storage.OwnerSuccession, error) {
	if err := r.u.check(); err != nil {
		return storage.OwnerSuccession{}, err
	}
	row, err := r.u.q.EndOwnerSuccession(ctx, sqlcdb.EndOwnerSuccessionParams{Status: status, EndedBy: by, EndedAt: at, ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.OwnerSuccession{}, &storage.Error{Code: storage.CodeVersion, Message: "the succession changed or ended since it was read"}
	}
	if err != nil {
		return storage.OwnerSuccession{}, mapError(err)
	}
	out := toSuccession(row)
	v := out.Version
	return out, r.u.event(ctx, "owner_succession."+status, "owner_succession", id, &v, map[string][2]any{
		"status": {storage.SuccessionNotice, status}, "ended_by": {nil, by}, "ended_at": {nil, at.UTC().Format(time.RFC3339Nano)}})
}
