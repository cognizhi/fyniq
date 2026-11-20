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

// ----------------------------------------------- delegation of approval authority and approval reminders (FR-APR-06, FR-APR-08)

func (u *unitOfWork) ApprovalDelegations() storage.ApprovalDelegations { return delegations{u} }

func (u *unitOfWork) ApprovalReminders() storage.ApprovalReminders { return reminders{u} }

type delegations struct{ u *unitOfWork }

type delegationRow = sqlcdb.GetApprovalDelegationRow

func toDelegation(r delegationRow) storage.ApprovalDelegation {
	return storage.ApprovalDelegation{ID: r.ID, Delegator: r.Delegator, Delegate: r.Delegate, StartsOn: r.StartsOn, EndsOn: r.EndsOn, Reason: r.Reason,
		Status: r.Status, EndedAt: r.EndedAt, EndedBy: r.EndedBy, Version: int(r.Version), CreatedAt: r.CreatedAt}
}

func (d delegations) event(ctx context.Context, action string, v storage.ApprovalDelegation, fields map[string][2]any) error {
	version := v.Version
	return d.u.event(ctx, action, "approval_delegation", v.ID, &version, fields)
}

func (d delegations) Create(ctx context.Context, n storage.NewApprovalDelegation) (storage.ApprovalDelegation, error) {
	if err := d.u.check(); err != nil {
		return storage.ApprovalDelegation{}, err
	}
	r, err := d.u.q.InsertApprovalDelegation(ctx, sqlcdb.InsertApprovalDelegationParams{Delegator: n.Delegator, Delegate: n.Delegate, StartsOn: n.StartsOn,
		EndsOn: n.EndsOn, Reason: n.Reason, At: n.At})
	if err != nil {
		return storage.ApprovalDelegation{}, mapError(err)
	}
	v := toDelegation(delegationRow(r))
	return v, d.event(ctx, "approval_delegation.created", v, map[string][2]any{"delegator": {nil, v.Delegator}, "delegate": {nil, v.Delegate},
		"starts_on": {nil, v.StartsOn.Format("2006-01-02")}, "ends_on": {nil, v.EndsOn.Format("2006-01-02")}, "reason": {nil, v.Reason}})
}

func (d delegations) Get(ctx context.Context, id uuid.UUID) (storage.ApprovalDelegation, error) {
	if err := d.u.check(); err != nil {
		return storage.ApprovalDelegation{}, err
	}
	r, err := d.u.q.GetApprovalDelegation(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ApprovalDelegation{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ApprovalDelegation{}, mapError(err)
	}
	return toDelegation(r), nil
}

func (d delegations) Lock(ctx context.Context, id uuid.UUID) (storage.ApprovalDelegation, error) {
	if err := d.u.check(); err != nil {
		return storage.ApprovalDelegation{}, err
	}
	r, err := d.u.q.LockApprovalDelegation(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ApprovalDelegation{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.ApprovalDelegation{}, mapError(err)
	}
	return toDelegation(delegationRow(r)), nil
}

func (d delegations) List(ctx context.Context) ([]storage.ApprovalDelegation, error) {
	if err := d.u.check(); err != nil {
		return nil, err
	}
	rows, err := d.u.q.ListApprovalDelegations(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ApprovalDelegation, len(rows))
	for i, r := range rows {
		out[i] = toDelegation(delegationRow(r))
	}
	return out, nil
}

func (d delegations) End(ctx context.Context, id uuid.UUID, version int, by uuid.UUID, at time.Time) (storage.ApprovalDelegation, error) {
	if err := d.u.check(); err != nil {
		return storage.ApprovalDelegation{}, err
	}
	if version < 1 || version > 1_000_000 {
		return storage.ApprovalDelegation{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("version %d of a delegation", version)}
	}
	r, err := d.u.q.EndApprovalDelegation(ctx, sqlcdb.EndApprovalDelegationParams{ID: id, Version: int32(version), By: &by, At: &at}) // 1 to 1,000,000, checked above
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.ApprovalDelegation{}, &storage.Error{Code: storage.CodeVersion, Message: "the delegation was ended or changed since it was read"}
	}
	if err != nil {
		return storage.ApprovalDelegation{}, mapError(err)
	}
	v := toDelegation(delegationRow(r))
	return v, d.event(ctx, "approval_delegation.ended", v, map[string][2]any{"status": {"active", "ended"}, "ended_by": {nil, by}, "ended_at": {nil, at}})
}

type reminders struct{ u *unitOfWork }

func (r reminders) Sent(ctx context.Context, requestID uuid.UUID) ([]storage.ApprovalReminder, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ApprovalRemindersSent(ctx, requestID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.ApprovalReminder, len(rows))
	for i, row := range rows {
		out[i] = storage.ApprovalReminder{Kind: row.Kind, Number: int(row.Number), SentAt: row.SentAt}
	}
	return out, nil
}

func (r reminders) Record(ctx context.Context, requestID uuid.UUID, kind string, number int, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if number < 1 || number > 3 {
		return &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("reminder %d of an approval", number)}
	}
	if err := r.u.q.InsertApprovalReminder(ctx, sqlcdb.InsertApprovalReminderParams{RequestID: requestID, Kind: kind, Number: int32(number), At: at}); err != nil { // 1 to 3, checked above
		return mapError(err)
	}
	return r.u.event(ctx, "approval."+kind+"_sent", "approval_request", requestID, nil, map[string][2]any{"kind": {nil, kind}, "number": {nil, number}, "sent_at": {nil, at}})
}
