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
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) EmailMessages() storage.EmailMessages { return emails{u} }

type emails struct{ u *unitOfWork }

// emailRow is every email query's row: they all select the same columns.
type emailRow = sqlcdb.GetEmailMessageRow

func toEmail(r emailRow) storage.EmailMessage {
	return storage.EmailMessage{ID: r.ID, Purpose: r.Purpose, Recipient: r.Recipient, Subject: r.Subject, Status: r.Status, Provider: str(r.Provider),
		ProviderID: str(r.ProviderID), Attempts: int(r.Attempts), AttemptStartedAt: r.AttemptStartedAt, AcceptedAt: r.AcceptedAt,
		LastError: str(r.LastError), CreatedAt: r.CreatedAt}
}

// clip keeps an error at the column's 500 characters.
func clip(s string) string {
	if r := []rune(s); len(r) > 500 {
		return string(r[:500])
	}
	return s
}

func (r emails) Queue(ctx context.Context, purpose, recipient, subject string) (storage.EmailMessage, error) {
	if err := r.u.check(); err != nil {
		return storage.EmailMessage{}, err
	}
	row, err := r.u.q.InsertEmailMessage(ctx, sqlcdb.InsertEmailMessageParams{Purpose: purpose, Recipient: recipient, Subject: subject})
	if err != nil {
		return storage.EmailMessage{}, mapError(err)
	}
	m := toEmail(emailRow(row))
	if err := r.u.event(ctx, "email_message.queued", "email_message", m.ID, nil, map[string][2]any{
		"purpose": {nil, m.Purpose}, "recipient": {nil, m.Recipient}, "subject": {nil, m.Subject}}); err != nil {
		return storage.EmailMessage{}, err
	}
	return m, nil
}

func (r emails) Get(ctx context.Context, id uuid.UUID) (storage.EmailMessage, error) {
	if err := r.u.check(); err != nil {
		return storage.EmailMessage{}, err
	}
	row, err := r.u.q.GetEmailMessage(ctx, id)
	if err != nil {
		return storage.EmailMessage{}, mapError(err)
	}
	return toEmail(row), nil
}

func (r emails) Start(ctx context.Context, id uuid.UUID, now time.Time, provider string) (storage.EmailMessage, error) {
	if err := r.u.check(); err != nil {
		return storage.EmailMessage{}, err
	}
	locked, err := r.u.q.LockEmailMessage(ctx, id)
	if err != nil {
		return storage.EmailMessage{}, mapError(err)
	}
	before := toEmail(emailRow(locked))
	if before.Status != storage.EmailQueued || before.AttemptStartedAt != nil {
		return before, nil
	}
	if _, err := r.u.q.StartEmailAttempt(ctx, sqlcdb.StartEmailAttemptParams{ID: id, Now: &now, Provider: &provider}); err != nil {
		return storage.EmailMessage{}, mapError(err)
	}
	return before, nil
}

func (r emails) Release(ctx context.Context, id uuid.UUID, lastError string) (storage.EmailMessage, error) {
	if err := r.u.check(); err != nil {
		return storage.EmailMessage{}, err
	}
	row, err := r.u.q.ReleaseEmailAttempt(ctx, sqlcdb.ReleaseEmailAttemptParams{ID: id, LastError: opt(clip(lastError))})
	if err != nil {
		return storage.EmailMessage{}, mapError(err)
	}
	return toEmail(emailRow(row)), nil
}

func (r emails) Finish(ctx context.Context, id uuid.UUID, o storage.EmailOutcome) (storage.EmailMessage, error) {
	if err := r.u.check(); err != nil {
		return storage.EmailMessage{}, err
	}
	row, err := r.u.q.FinishEmailMessage(ctx, sqlcdb.FinishEmailMessageParams{ID: id, Status: o.Status, Provider: opt(o.Provider),
		ProviderID: opt(o.ProviderID), AcceptedAt: o.AcceptedAt, LastError: opt(clip(o.LastError))})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.EmailMessage{}, &storage.Error{Code: storage.CodeVersion, Message: fmt.Sprintf("email message %s has its outcome already", id)}
	}
	if err != nil {
		return storage.EmailMessage{}, mapError(err)
	}
	m := toEmail(emailRow(row))
	if err := r.u.event(ctx, "email_message."+m.Status, "email_message", m.ID, nil, map[string][2]any{
		"status": {storage.EmailQueued, m.Status}, "provider": {nil, nilIfEmpty(m.Provider)}, "provider_id": {nil, nilIfEmpty(m.ProviderID)},
		"attempts": {nil, m.Attempts}, "last_error": {nil, nilIfEmpty(m.LastError)}}); err != nil {
		return storage.EmailMessage{}, err
	}
	return m, nil
}

func (u *unitOfWork) EmailSenders() storage.EmailSenders { return senders{u} }

type senders struct{ u *unitOfWork }

func toSender(id uuid.UUID, purpose string, address, name *string, setBy string, setAt time.Time) storage.EmailSender {
	return storage.EmailSender{ID: id, Purpose: purpose, Address: str(address), DisplayName: str(name), SetBy: setBy, SetAt: setAt}
}

func (r senders) Set(ctx context.Context, purpose, address, displayName string) (storage.EmailSender, error) {
	if err := r.u.check(); err != nil {
		return storage.EmailSender{}, err
	}
	row, err := r.u.q.InsertEmailSender(ctx, sqlcdb.InsertEmailSenderParams{Purpose: purpose, Address: opt(address), DisplayName: opt(displayName),
		SetBy: r.u.actor(ctx), SetAt: r.u.store.clock.Now()})
	if err != nil {
		return storage.EmailSender{}, mapError(err)
	}
	out := toSender(row.ID, row.Purpose, row.Address, row.DisplayName, row.SetBy, row.SetAt)
	if err := r.u.event(ctx, "email_sender.set", "email_sender", out.ID, nil, map[string][2]any{
		"purpose": {nil, purpose}, "address": {nil, nilIfEmpty(address)}, "display_name": {nil, nilIfEmpty(displayName)}}); err != nil {
		return storage.EmailSender{}, err
	}
	return out, nil
}

func (r senders) InForce(ctx context.Context) ([]storage.EmailSender, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.EmailSendersInForce(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.EmailSender, 0, len(rows))
	for _, x := range rows {
		out = append(out, toSender(x.ID, x.Purpose, x.Address, x.DisplayName, x.SetBy, x.SetAt))
	}
	return out, nil
}

type eventRow = sqlcdb.EmailEventsRow

func toEvent(r eventRow) storage.EmailEvent {
	return storage.EmailEvent{ID: r.ID, MessageID: r.MessageID, Provider: r.Provider, DeliveryID: r.DeliveryID, Kind: r.Kind, Recipient: r.Recipient,
		Detail: str(r.Detail), OccurredAt: r.OccurredAt, ReceivedAt: r.ReceivedAt}
}

func (r emails) RecordEvent(ctx context.Context, e storage.EmailEvent) (storage.EmailEvent, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.EmailEvent{}, false, err
	}
	row, err := r.u.q.InsertEmailEvent(ctx, sqlcdb.InsertEmailEventParams{MessageID: e.MessageID, Provider: e.Provider, DeliveryID: e.DeliveryID,
		Kind: e.Kind, Recipient: e.Recipient, Detail: opt(clip(e.Detail)), OccurredAt: e.OccurredAt, ReceivedAt: r.u.store.clock.Now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.EmailEvent{}, false, nil // the provider's retry of a delivery recorded already
	}
	if err != nil {
		return storage.EmailEvent{}, false, mapError(err)
	}
	out := toEvent(eventRow(row))
	if err := r.u.event(ctx, "email_message.event_recorded", "email_message", out.MessageID, nil, map[string][2]any{
		"event_id": {nil, out.ID.String()}, "provider": {nil, out.Provider}, "kind": {nil, out.Kind}, "recipient": {nil, out.Recipient},
		"detail": {nil, nilIfEmpty(out.Detail)}, "occurred_at": {nil, out.OccurredAt.UTC().Format(time.RFC3339)}}); err != nil {
		return storage.EmailEvent{}, false, err
	}
	return out, true, nil
}

func (r emails) Events(ctx context.Context, message uuid.UUID) ([]storage.EmailEvent, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.EmailEvents(ctx, message)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.EmailEvent, 0, len(rows))
	for _, x := range rows {
		out = append(out, toEvent(x))
	}
	return out, nil
}

func (u *unitOfWork) EmailSuppressions() storage.EmailSuppressions { return suppressions{u} }

type suppressions struct{ u *unitOfWork }

type suppressionRow = sqlcdb.EmailSuppressionsRow

func toSuppression(r suppressionRow) storage.EmailSuppression {
	return storage.EmailSuppression{ID: r.ID, Address: r.Address, Reason: r.Reason, EventID: r.EventID, SuppressedAt: r.SuppressedAt,
		ReleasedAt: r.ReleasedAt, ReleasedBy: str(r.ReleasedBy)}
}

func (r suppressions) Suppress(ctx context.Context, address, reason string, event uuid.UUID) (storage.EmailSuppression, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.EmailSuppression{}, false, err
	}
	row, err := r.u.q.InsertEmailSuppression(ctx, sqlcdb.InsertEmailSuppressionParams{Address: strings.ToLower(address), Reason: reason,
		EventID: event, SuppressedAt: r.u.store.clock.Now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.EmailSuppression{}, false, nil // suppressed already
	}
	if err != nil {
		return storage.EmailSuppression{}, false, mapError(err)
	}
	out := toSuppression(suppressionRow(row))
	if err := r.u.event(ctx, "email_suppression.added", "email_suppression", out.ID, nil, map[string][2]any{
		"address": {nil, out.Address}, "reason": {nil, out.Reason}, "event_id": {nil, out.EventID.String()}}); err != nil {
		return storage.EmailSuppression{}, false, err
	}
	return out, true, nil
}

func (r suppressions) InForce(ctx context.Context, address string) (storage.EmailSuppression, error) {
	if err := r.u.check(); err != nil {
		return storage.EmailSuppression{}, err
	}
	row, err := r.u.q.EmailSuppressionInForce(ctx, strings.ToLower(address))
	if err != nil {
		return storage.EmailSuppression{}, mapError(err)
	}
	return toSuppression(suppressionRow(row)), nil
}

func (r suppressions) List(ctx context.Context, all bool) ([]storage.EmailSuppression, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.EmailSuppressions(ctx, all)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.EmailSuppression, 0, len(rows))
	for _, x := range rows {
		out = append(out, toSuppression(x))
	}
	return out, nil
}

func (r suppressions) Release(ctx context.Context, id uuid.UUID) (storage.EmailSuppression, error) {
	if err := r.u.check(); err != nil {
		return storage.EmailSuppression{}, err
	}
	now, by := r.u.store.clock.Now(), r.u.actor(ctx)
	row, err := r.u.q.ReleaseEmailSuppression(ctx, sqlcdb.ReleaseEmailSuppressionParams{ID: id, ReleasedAt: &now, ReleasedBy: &by})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gerr := r.u.q.EmailSuppressionByID(ctx, id); gerr != nil {
			return storage.EmailSuppression{}, mapError(gerr)
		}
		return storage.EmailSuppression{}, &storage.Error{Code: storage.CodeVersion, Message: fmt.Sprintf("email suppression %s was released already", id)}
	}
	if err != nil {
		return storage.EmailSuppression{}, mapError(err)
	}
	out := toSuppression(suppressionRow(row))
	if err := r.u.event(ctx, "email_suppression.released", "email_suppression", out.ID, nil, map[string][2]any{
		"released_at": {nil, now.UTC().Format(time.RFC3339)}}); err != nil {
		return storage.EmailSuppression{}, err
	}
	return out, nil
}
