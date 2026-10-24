package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Notifications() storage.Notifications { return notifications{u} }

type notifications struct{ u *unitOfWork }

type notificationEventRow = sqlcdb.GetNotificationEventRow

func toNotificationEvent(r notificationEventRow) storage.NotificationEvent {
	return storage.NotificationEvent{ID: r.ID, Type: r.Type, RecordType: str(r.RecordType), RecordID: r.RecordID, Title: r.Title, Link: str(r.Link),
		Actor: r.Actor, Audience: r.Audience, OccurredAt: r.OccurredAt, FannedOutAt: r.FannedOutAt}
}

type notificationRow = sqlcdb.GetNotificationRow

func toNotification(r notificationRow) storage.Notification {
	return storage.Notification{ID: r.ID, EventID: r.EventID, UserID: r.UserID, Type: r.Type, Severity: r.Severity, Actionable: r.Actionable,
		Email: r.Email, EmailMessageID: r.EmailMessageID, DigestID: r.DigestID, CreatedAt: r.CreatedAt, ReadAt: r.ReadAt, ArchivedAt: r.ArchivedAt,
		Title: r.Title, Link: str(r.Link)}
}

func (r notifications) Emit(ctx context.Context, e storage.NewNotificationEvent) (storage.NotificationEvent, error) {
	if err := r.u.check(); err != nil {
		return storage.NotificationEvent{}, err
	}
	var rt *string
	if e.RecordType != "" {
		rt = &e.RecordType
	}
	row, err := r.u.q.InsertNotificationEvent(ctx, sqlcdb.InsertNotificationEventParams{Type: e.Type, RecordType: rt, RecordID: e.RecordID,
		Title: e.Title, Link: opt(e.Link), Actor: r.u.actor(ctx), Audience: e.Audience, OccurredAt: e.OccurredAt})
	if err != nil {
		return storage.NotificationEvent{}, mapError(err)
	}
	out := toNotificationEvent(notificationEventRow(row))
	if err := r.u.event(ctx, "notification_event.emitted", "notification_event", out.ID, nil, map[string][2]any{
		"type": {nil, out.Type}, "record_type": {nil, nilIfEmpty(out.RecordType)}, "title": {nil, out.Title}}); err != nil {
		return storage.NotificationEvent{}, err
	}
	return out, nil
}

func (r notifications) Emitted(ctx context.Context, typ string, record uuid.UUID) (bool, error) {
	if err := r.u.check(); err != nil {
		return false, err
	}
	ok, err := r.u.q.NotificationEventEmitted(ctx, sqlcdb.NotificationEventEmittedParams{Type: typ, RecordID: &record})
	return ok, mapError(err)
}

func (r notifications) Event(ctx context.Context, id uuid.UUID) (storage.NotificationEvent, error) {
	if err := r.u.check(); err != nil {
		return storage.NotificationEvent{}, err
	}
	row, err := r.u.q.GetNotificationEvent(ctx, id)
	if err != nil {
		return storage.NotificationEvent{}, mapError(err)
	}
	return toNotificationEvent(row), nil
}

func (r notifications) FannedOut(ctx context.Context, id uuid.UUID, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	_, err := r.u.q.FannedOutNotificationEvent(ctx, sqlcdb.FannedOutNotificationEventParams{ID: id, At: &at})
	return mapError(err)
}

func (r notifications) Add(ctx context.Context, n storage.NewNotification) (storage.Notification, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.Notification{}, false, err
	}
	id, err := r.u.q.InsertNotification(ctx, sqlcdb.InsertNotificationParams{EventID: n.EventID, UserID: n.UserID, Type: n.Type, Severity: n.Severity,
		Actionable: n.Actionable, Email: n.Email, CreatedAt: r.u.store.clock.Now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Notification{}, false, nil // fanned out to them before
	}
	if err != nil {
		return storage.Notification{}, false, mapError(err)
	}
	out, err := r.Get(ctx, id)
	return out, err == nil, err
}

func (r notifications) Emailed(ctx context.Context, id, message uuid.UUID) error {
	if err := r.u.check(); err != nil {
		return err
	}
	_, err := r.u.q.EmailedNotification(ctx, sqlcdb.EmailedNotificationParams{ID: id, Message: &message})
	return mapError(err)
}

func (r notifications) Get(ctx context.Context, id uuid.UUID) (storage.Notification, error) {
	if err := r.u.check(); err != nil {
		return storage.Notification{}, err
	}
	row, err := r.u.q.GetNotification(ctx, id)
	if err != nil {
		return storage.Notification{}, mapError(err)
	}
	return toNotification(row), nil
}

func (r notifications) List(ctx context.Context, f storage.NotificationFilter) ([]storage.Notification, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	var before *uuid.UUID
	if f.Before != uuid.Nil {
		before = &f.Before
	}
	limit := f.Limit
	if limit <= 0 || limit > math.MaxInt32 {
		limit = 100
	}
	rows, err := r.u.q.Notifications(ctx, sqlcdb.NotificationsParams{UserID: f.UserID, Archived: f.Archived, Unread: f.Unread, Before: before,
		PageSize: int32(limit)}) // 1 to MaxInt32, checked above
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Notification, 0, len(rows))
	for _, x := range rows {
		out = append(out, toNotification(notificationRow(x)))
	}
	return out, nil
}

func (r notifications) Unread(ctx context.Context, user uuid.UUID) (int, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.UnreadNotifications(ctx, user)
	return int(n), mapError(err)
}

func (r notifications) Mark(ctx context.Context, id, user uuid.UUID, read bool, at time.Time) (storage.Notification, error) {
	if err := r.u.check(); err != nil {
		return storage.Notification{}, err
	}
	var readAt *time.Time
	if read {
		readAt = &at
	}
	if _, err := r.u.q.MarkNotification(ctx, sqlcdb.MarkNotificationParams{ID: id, UserID: user, ReadAt: readAt}); err != nil {
		return storage.Notification{}, mapError(err)
	}
	return r.Get(ctx, id)
}

func (r notifications) Archive(ctx context.Context, id, user uuid.UUID, at time.Time) (storage.Notification, error) {
	if err := r.u.check(); err != nil {
		return storage.Notification{}, err
	}
	if _, err := r.u.q.ArchiveNotification(ctx, sqlcdb.ArchiveNotificationParams{ID: id, UserID: user, At: at}); err != nil {
		return storage.Notification{}, mapError(err)
	}
	return r.Get(ctx, id)
}

func (r notifications) MarkAllRead(ctx context.Context, user uuid.UUID, at time.Time) (int, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.MarkAllNotificationsRead(ctx, sqlcdb.MarkAllNotificationsReadParams{UserID: user, At: &at})
	return int(n), mapError(err)
}

func (r notifications) Held(ctx context.Context, cadence string) ([]storage.Notification, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.HeldNotifications(ctx, cadence)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Notification, 0, len(rows))
	for _, x := range rows {
		out = append(out, toNotification(notificationRow(x)))
	}
	return out, nil
}

func (r notifications) Digested(ctx context.Context, user uuid.UUID, cadence string, message uuid.UUID, ids []uuid.UUID, at time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	digest, err := r.u.q.InsertNotificationDigest(ctx, sqlcdb.InsertNotificationDigestParams{UserID: user, Cadence: cadence, EmailMessageID: message,
		SentAt: at})
	if err != nil {
		return mapError(err)
	}
	n, err := r.u.q.DigestNotifications(ctx, sqlcdb.DigestNotificationsParams{DigestID: &digest, Ids: ids})
	if err != nil {
		return mapError(err)
	}
	if int(n) != len(ids) {
		return &storage.Error{Code: storage.CodeVersion, Message: fmt.Sprintf("%d of %d notifications were digested already", len(ids)-int(n), len(ids))}
	}
	return nil
}

func minutes(v *int16) *int {
	if v == nil {
		return nil
	}
	m := int(*v)
	return &m
}

func asMinutes(v *int) (*int16, error) {
	if v == nil {
		return nil, nil
	}
	if *v < 0 || *v > 1439 {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("%d minutes is not a time of day", *v)}
	}
	m := int16(*v) // 0 to 1439, checked above
	return &m, nil
}

func (r notifications) Prefer(ctx context.Context, p storage.NotificationPreference) (storage.NotificationPreference, error) {
	if err := r.u.check(); err != nil {
		return storage.NotificationPreference{}, err
	}
	from, err := asMinutes(p.QuietFrom)
	if err != nil {
		return storage.NotificationPreference{}, err
	}
	to, err := asMinutes(p.QuietTo)
	if err != nil {
		return storage.NotificationPreference{}, err
	}
	row, err := r.u.q.InsertNotificationPreference(ctx, sqlcdb.InsertNotificationPreferenceParams{UserID: p.UserID, Type: p.Type, Email: opt(p.Email),
		QuietFrom: from, QuietTo: to, SetAt: r.u.store.clock.Now()})
	if err != nil {
		return storage.NotificationPreference{}, mapError(err)
	}
	out := storage.NotificationPreference{UserID: row.UserID, Type: row.Type, Email: str(row.Email), QuietFrom: minutes(row.QuietFrom),
		QuietTo: minutes(row.QuietTo), SetAt: row.SetAt}
	if err := r.u.event(ctx, "notification_preference.set", "notification_preference", row.ID, nil, map[string][2]any{
		"type": {nil, out.Type}, "email": {nil, nilIfEmpty(out.Email)}, "quiet_from": {nil, out.QuietFrom}, "quiet_to": {nil, out.QuietTo}}); err != nil {
		return storage.NotificationPreference{}, err
	}
	return out, nil
}

func (r notifications) Preferences(ctx context.Context, user uuid.UUID) ([]storage.NotificationPreference, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.NotificationPreferences(ctx, user)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.NotificationPreference, 0, len(rows))
	for _, x := range rows {
		out = append(out, storage.NotificationPreference{UserID: x.UserID, Type: x.Type, Email: str(x.Email), QuietFrom: minutes(x.QuietFrom),
			QuietTo: minutes(x.QuietTo), SetAt: x.SetAt})
	}
	return out, nil
}
