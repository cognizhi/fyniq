package postgres

import (
	"context"
	"errors"
	"fmt"
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

func (r emails) Start(ctx context.Context, id uuid.UUID, now time.Time) (storage.EmailMessage, error) {
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
	if _, err := r.u.q.StartEmailAttempt(ctx, sqlcdb.StartEmailAttemptParams{ID: id, Now: &now}); err != nil {
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
