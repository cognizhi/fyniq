package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// event queues the audit event of a change made in this unit of work (AUD-01). Who acted and why
// come from the request's context; the store's service identity stands in when no actor is given.
func (u *unitOfWork) event(ctx context.Context, action, objectType string, objectID uuid.UUID, version *int, changes map[string][2]any) error {
	a := storage.AuditFrom(ctx)
	actor := u.store.actor
	if a.Actor != nil {
		actor = *a.Actor
	}
	c := make(map[string]audit.Change, len(changes))
	for k, v := range changes {
		c[k] = audit.Change(v)
	}
	e, err := audit.New(u.org, u.store.clock.Now(), audit.Actor(actor), action,
		audit.Object{Type: objectType, ID: objectID, Version: version}, c)
	if err != nil {
		return err
	}
	if a.OnBehalfOf != nil {
		on := audit.Actor(*a.OnBehalfOf)
		e.OnBehalfOf = &on
	}
	if a.Originator != "" {
		e.Originator = &a.Originator
	}
	if a.ApprovalIDs != nil {
		e.ApprovalIDs = a.ApprovalIDs
	}
	e.Reason = a.Reason
	e.Request = audit.Request(a.Request)
	u.pending = append(u.pending, e)
	return nil
}

// flush writes the queued events, in order, as the last statements before commit.
func (u *unitOfWork) flush(ctx context.Context) error {
	for _, e := range u.pending {
		body, err := e.Canonical()
		if err != nil {
			return fmt.Errorf("audit event %s: %w", e.Action, err)
		}
		if err := u.q.InsertAuditEvent(ctx, sqlcdb.InsertAuditEventParams{ID: e.EventID, Body: string(body)}); err != nil {
			return mapError(err)
		}
	}
	u.pending = nil
	return nil
}

type auditTrail struct{ u *unitOfWork }

func (u *unitOfWork) Audit() storage.Audit { return auditTrail{u} }

func (r auditTrail) Record(ctx context.Context, rec storage.AuditRecord) error {
	if err := r.u.check(); err != nil {
		return err
	}
	return r.u.event(ctx, rec.Action, rec.ObjectType, rec.ObjectID, nil, rec.Changes)
}

func (r auditTrail) Head(ctx context.Context) (storage.AuditHead, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditHead{}, err
	}
	row, err := r.u.q.GetAuditHead(ctx)
	if err != nil {
		return storage.AuditHead{}, mapError(err)
	}
	return storage.AuditHead{Seq: row.Seq, Hash: row.HeadHash, Action: row.Action}, nil
}

func (r auditTrail) Events(ctx context.Context, after int64, limit int) ([]storage.AuditEvent, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100000 {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("a page of %d audit events", limit), Hint: "Ask for 1 to 100,000"}
	}
	rows, err := r.u.q.ListAuditEvents(ctx, sqlcdb.ListAuditEventsParams{After: after, Max: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AuditEvent, len(rows))
	for i, e := range rows {
		out[i] = storage.AuditEvent{ID: e.ID, Seq: e.Seq, Body: e.Body, PrevHash: e.PrevHash, Hash: e.Hash}
	}
	return out, nil
}
