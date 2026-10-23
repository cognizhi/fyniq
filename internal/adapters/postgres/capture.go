package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) CaptureAddresses() storage.CaptureAddresses { return captures{u} }

type captures struct{ u *unitOfWork }

// captureRow is every capture address query's row: they all select the same columns.
type captureRow = sqlcdb.LiveCaptureAddressesRow

func toCapture(r captureRow) storage.CaptureAddress {
	return storage.CaptureAddress{ID: r.ID, LocalPart: r.LocalPart, UserID: r.UserID, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, RetiredAt: r.RetiredAt}
}

func captureOwner(user *uuid.UUID) any {
	if user == nil {
		return nil
	}
	return user.String()
}

func (r captures) Create(ctx context.Context, localPart string, user *uuid.UUID) (storage.CaptureAddress, error) {
	if err := r.u.check(); err != nil {
		return storage.CaptureAddress{}, err
	}
	old, err := r.u.q.LiveCaptureAddressOf(ctx, user)
	switch {
	case err == nil:
		if _, err := r.Retire(ctx, old.ID); err != nil {
			return storage.CaptureAddress{}, err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return storage.CaptureAddress{}, mapError(err)
	}
	row, err := r.u.q.InsertCaptureAddress(ctx, sqlcdb.InsertCaptureAddressParams{LocalPart: strings.ToLower(localPart), UserID: user,
		CreatedBy: r.u.actor(ctx), CreatedAt: r.u.store.clock.Now()})
	if err != nil {
		return storage.CaptureAddress{}, mapError(err)
	}
	out := toCapture(captureRow(row))
	if err := r.u.event(ctx, "capture_address.created", "capture_address", out.ID, nil, map[string][2]any{
		"local_part": {nil, out.LocalPart}, "user_id": {nil, captureOwner(user)}}); err != nil {
		return storage.CaptureAddress{}, err
	}
	return out, nil
}

func (r captures) Live(ctx context.Context) ([]storage.CaptureAddress, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.LiveCaptureAddresses(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.CaptureAddress, 0, len(rows))
	for _, x := range rows {
		out = append(out, toCapture(x))
	}
	return out, nil
}

func (r captures) Retire(ctx context.Context, id uuid.UUID) (storage.CaptureAddress, error) {
	if err := r.u.check(); err != nil {
		return storage.CaptureAddress{}, err
	}
	now, by := r.u.store.clock.Now(), r.u.actor(ctx)
	row, err := r.u.q.RetireCaptureAddress(ctx, sqlcdb.RetireCaptureAddressParams{ID: id, RetiredAt: &now, RetiredBy: &by})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gerr := r.u.q.CaptureAddressByID(ctx, id); gerr != nil {
			return storage.CaptureAddress{}, mapError(gerr)
		}
		return storage.CaptureAddress{}, &storage.Error{Code: storage.CodeVersion, Message: fmt.Sprintf("capture address %s was retired already", id)}
	}
	if err != nil {
		return storage.CaptureAddress{}, mapError(err)
	}
	out := toCapture(captureRow(row))
	if err := r.u.event(ctx, "capture_address.retired", "capture_address", out.ID, nil, map[string][2]any{
		"local_part": {nil, out.LocalPart}, "retired_at": {nil, now.UTC().Format(time.RFC3339)}}); err != nil {
		return storage.CaptureAddress{}, err
	}
	return out, nil
}

type senderRow = sqlcdb.CaptureSendersInForceRow

func toCaptureSender(r senderRow) storage.CaptureSender {
	return storage.CaptureSender{ID: r.ID, Address: r.Address, UserID: r.UserID, Decision: r.Decision, DecidedBy: r.DecidedBy, DecidedAt: r.DecidedAt}
}

func (r captures) Decide(ctx context.Context, sender string, user *uuid.UUID, decision string) (storage.CaptureSender, error) {
	if err := r.u.check(); err != nil {
		return storage.CaptureSender{}, err
	}
	row, err := r.u.q.InsertCaptureSender(ctx, sqlcdb.InsertCaptureSenderParams{Address: strings.ToLower(sender), UserID: user, Decision: decision,
		DecidedBy: r.u.actor(ctx), DecidedAt: r.u.store.clock.Now()})
	if err != nil {
		return storage.CaptureSender{}, mapError(err)
	}
	out := toCaptureSender(senderRow(row))
	if err := r.u.event(ctx, "capture_sender."+decision, "capture_sender", out.ID, nil, map[string][2]any{
		"address": {nil, out.Address}, "user_id": {nil, captureOwner(user)}, "decision": {nil, decision}}); err != nil {
		return storage.CaptureSender{}, err
	}
	return out, nil
}

func (r captures) Decisions(ctx context.Context) ([]storage.CaptureSender, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.CaptureSendersInForce(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.CaptureSender, 0, len(rows))
	for _, x := range rows {
		out = append(out, toCaptureSender(x))
	}
	return out, nil
}

// CaptureRoute implements storage.CaptureRoutes: the live address named localPart, through capture_route, which reads
// the global registry before any tenant is set.
func (s *Store) CaptureRoute(ctx context.Context, localPart string) (storage.CaptureRoute, error) {
	row, err := s.q().CaptureRoute(ctx, strings.ToLower(localPart))
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.CaptureRoute{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.CaptureRoute{}, mapError(err)
	}
	out := storage.CaptureRoute{Organisation: row.OrganisationID, Address: row.AddressID}
	if row.UserID != "" {
		id, err := uuid.Parse(row.UserID)
		if err != nil {
			return storage.CaptureRoute{}, err
		}
		out.UserID = &id
	}
	return out, nil
}
