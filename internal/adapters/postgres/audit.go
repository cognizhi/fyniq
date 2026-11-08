package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

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
	// AUD-02: a sensitive value is proved by the organisation's fingerprint before masking, never kept.
	scope := []byte("audit:" + u.org.String())
	c, err := audit.Prove(ctx, c, func(ctx context.Context, v []byte) ([]byte, error) { return u.store.fingerprint(ctx, scope, v) })
	if err != nil {
		return err
	}
	now := u.store.clock.Now()
	e, err := audit.New(u.org, now, audit.Actor(actor), action,
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
	e.Request = audit.Request{ID: a.Request.ID, IP: a.Request.IP, UserAgent: a.Request.UserAgent, Session: a.Request.Session}
	// AUD-06: a security event keeps its IP address and user agent beside the chain, fingerprinted in its body, so they
	// can be erased after the security-log retention while the event stays and verifies.
	if audit.IsSecurity(action) && (e.Request.IP != "" || e.Request.UserAgent != "") {
		d := storage.AuditDetail{EventID: e.EventID, OccurredAt: now, IP: audit.Clip(e.Request.IP, 64), UserAgent: audit.Clip(e.Request.UserAgent, 1000)}
		mac, err := u.store.fingerprint(ctx, scope, audit.DetailValue(d.IP, d.UserAgent))
		if err != nil {
			return fmt.Errorf("audit: fingerprint the request details of %s: %w", action, err)
		}
		e.Request.IP, e.Request.UserAgent = "", ""
		e.Request.Detail = audit.FingerprintPrefix + base64.RawURLEncoding.EncodeToString(mac)
		if u.details == nil {
			u.details = map[uuid.UUID]storage.AuditDetail{}
		}
		u.details[e.EventID] = d
	}
	u.pending = append(u.pending, e)
	return nil
}

// actor is who acts in this unit of work, as "type:id": the request's actor, or the store's service identity. A
// document version and a posting record it (FY053), as the audit event does.
func (u *unitOfWork) actor(ctx context.Context) string {
	a := u.store.actor
	if from := storage.AuditFrom(ctx); from.Actor != nil {
		a = *from.Actor
	}
	return a.String()
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
		if d, ok := u.details[e.EventID]; ok {
			if err := u.q.InsertAuditRequestDetail(ctx, sqlcdb.InsertAuditRequestDetailParams{ID: d.EventID, OccurredAt: d.OccurredAt,
				Ip: d.IP, UserAgent: d.UserAgent}); err != nil {
				return mapError(err)
			}
		}
	}
	u.pending, u.details = nil, nil
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
	out := storage.AuditHead{Hash: row.HeadHash}
	if row.Seq != nil {
		out.Seq = *row.Seq
	}
	if row.Action != nil {
		out.Action = *row.Action
	}
	return out, nil
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

func (r auditTrail) Details(ctx context.Context, events []uuid.UUID) (map[uuid.UUID]storage.AuditDetail, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListAuditRequestDetails(ctx, events)
	if err != nil {
		return nil, mapError(err)
	}
	out := make(map[uuid.UUID]storage.AuditDetail, len(rows))
	for _, d := range rows {
		out[d.ID] = storage.AuditDetail{EventID: d.ID, OccurredAt: d.OccurredAt, IP: d.Ip, UserAgent: d.UserAgent}
	}
	return out, nil
}

func (r auditTrail) DetailsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.CountAuditRequestDetailsBefore(ctx, cutoff)
	return n, mapError(err)
}

func (r auditTrail) EraseDetails(ctx context.Context, cutoff time.Time) (storage.AuditErasure, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditErasure{}, err
	}
	row, err := r.u.q.EraseAuditRequestDetails(ctx, cutoff)
	if err != nil {
		return storage.AuditErasure{}, mapError(err)
	}
	out := storage.AuditErasure{}
	if row.Erased != nil {
		out.Erased = *row.Erased
	}
	if row.FirstSeq != nil {
		out.FirstSeq = *row.FirstSeq
	}
	if row.LastSeq != nil {
		out.LastSeq = *row.LastSeq
	}
	return out, nil
}

func (r auditTrail) Find(ctx context.Context, q storage.AuditQuery) ([]storage.AuditEvent, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if q.Limit < 1 || q.Limit > 1000 {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("a page of %d audit events", q.Limit), Hint: "Ask for 1 to 1,000"}
	}
	text := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	rows, err := r.u.q.FindAuditEvents(ctx, sqlcdb.FindAuditEventsParams{Actor: text(q.Actor), Originator: text(q.Originator),
		ObjectType: text(q.ObjectType), ObjectID: q.ObjectID, Field: text(q.Field), Action: text(q.Action), Approval: text(q.ApprovalID),
		FromAt: q.From, ToAt: q.To, Before: q.Before, Max: int32(q.Limit)}) // #nosec G115 -- 1 to 1,000, checked above
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AuditEvent, len(rows))
	for i, e := range rows {
		out[i] = storage.AuditEvent{ID: e.ID, Seq: e.Seq, Body: e.Body, PrevHash: e.PrevHash, Hash: e.Hash}
	}
	return out, nil
}

func (r auditTrail) Event(ctx context.Context, id uuid.UUID) (storage.AuditEvent, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditEvent{}, err
	}
	e, err := r.u.q.GetAuditEventByID(ctx, id)
	if err != nil {
		return storage.AuditEvent{}, mapError(err)
	}
	return storage.AuditEvent{ID: e.ID, Seq: e.Seq, Body: e.Body, PrevHash: e.PrevHash, Hash: e.Hash}, nil
}

func (r auditTrail) Boundaries(ctx context.Context) ([]storage.AuditBoundary, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListAuditBoundaries(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AuditBoundary, 0, len(rows))
	for _, b := range rows {
		x, err := boundaryOf(b.ID, b.LastSeq, b.LastHash, b.LastOccurredAt, b.Approval, b.Counts, b.AnchorKey, b.WrittenAt)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

func (r auditTrail) RecordBoundary(ctx context.Context, b storage.NewAuditBoundary) (storage.AuditBoundary, error) {
	if err := r.u.check(); err != nil {
		return storage.AuditBoundary{}, err
	}
	counts, err := json.Marshal(b.Counts)
	if err != nil {
		return storage.AuditBoundary{}, err
	}
	row, err := r.u.q.InsertAuditBoundary(ctx, sqlcdb.InsertAuditBoundaryParams{LastSeq: b.LastSeq, LastHash: b.LastHash, LastOccurredAt: b.LastOccurredAt,
		Approval: b.Approval, Counts: counts, AnchorKey: b.AnchorKey, WrittenAt: b.WrittenAt})
	if err != nil {
		return storage.AuditBoundary{}, mapError(err)
	}
	out, err := boundaryOf(row.ID, row.LastSeq, row.LastHash, row.LastOccurredAt, row.Approval, row.Counts, row.AnchorKey, row.WrittenAt)
	if err != nil {
		return out, err
	}
	return out, r.u.event(ctx, "audit.boundary_written", "audit_chain", r.u.org, nil, map[string][2]any{"last_seq": {nil, b.LastSeq},
		"approval": {nil, b.Approval}, "counts": {nil, b.Counts}, "anchor_key": {nil, b.AnchorKey}})
}

func (r auditTrail) Purge(ctx context.Context, lastSeq int64) (int64, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.PurgeAudit(ctx, lastSeq)
	return n, mapError(err)
}

func boundaryOf(id uuid.UUID, seq int64, hash []byte, at time.Time, approval string, counts []byte, key string, written time.Time) (storage.AuditBoundary, error) {
	out := storage.AuditBoundary{ID: id, NewAuditBoundary: storage.NewAuditBoundary{LastSeq: seq, LastHash: hash, LastOccurredAt: at, Approval: approval,
		AnchorKey: key, WrittenAt: written}}
	return out, json.Unmarshal(counts, &out.Counts)
}
