// Package email sends outbound email through the outbox (FR-INT-21, FR-NTF-05): Queue records the message and
// writes its job in the same unit of work as whatever asked for it, and the worker's Work sends it, so a
// message is sent once, or Fyniq knows it may not have been (G4-073). The message's record never holds its
// body; the body travels in the outbox job only.
package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is the outbox kind of a send, run by the worker class.
const Kind = "email.send"

type payload struct {
	MessageID uuid.UUID `json:"message_id"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Text      string    `json:"text"`
}

// Queue records a message to one recipient and writes its send to the outbox, in the unit of work u.
func Queue(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, purpose, to, subject, text string) (storage.EmailMessage, error) {
	m, err := u.EmailMessages().Queue(ctx, purpose, to, subject)
	if err != nil {
		return storage.EmailMessage{}, err
	}
	b, err := json.Marshal(payload{MessageID: m.ID, To: to, Subject: subject, Text: text})
	if err != nil {
		return storage.EmailMessage{}, err
	}
	if _, err := d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: b,
		BusinessKey: "email:" + m.ID.String()}); err != nil {
		return storage.EmailMessage{}, err
	}
	return m, nil
}

// Work is the worker's handler of Kind: it sends the message from the address from through provider.
//   - A message with an outcome is left as it is: the job ran before.
//   - A message an earlier attempt may have handed over is reconciled first: the provider says whether it took
//     it; when it cannot say (SMTP), the message is recorded as unknown and never sent again on a guess.
//   - A refusal clears the attempt, so the outbox retries it; on the last attempt the message is failed.
//   - Any other error leaves the attempt marked: the next run reconciles it.
func Work(provider ports.Email, store storage.Store, from string, clock ports.Clock) outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p payload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.MessageID == uuid.Nil {
			return fmt.Errorf("email: outbox row %s is not a send", row.ID)
		}
		finish := func(o storage.EmailOutcome) error {
			return store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				_, err := u.EmailMessages().Finish(ctx, p.MessageID, o)
				return err
			})
		}
		var before storage.EmailMessage
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			before, err = u.EmailMessages().Start(ctx, p.MessageID, clock.Now())
			return err
		}); err != nil {
			return err
		}
		if before.Status != storage.EmailQueued {
			return nil
		}
		key := p.MessageID.String()
		if before.AttemptStartedAt != nil {
			r, err := provider.Status(ctx, key)
			switch {
			case err == nil:
				now := clock.Now()
				return finish(storage.EmailOutcome{Status: storage.EmailAccepted, Provider: provider.Name(), ProviderID: r.ProviderID, AcceptedAt: &now})
			case errors.Is(err, ports.ErrEmailUnknown):
				return finish(storage.EmailOutcome{Status: storage.EmailUnknown, Provider: provider.Name(),
					LastError: "an earlier attempt may have handed the message over, and the provider cannot say: it is not sent again"})
			case !errors.Is(err, ports.ErrEmailRefused):
				return err // ask again on the next run
			}
			// The provider never took it: sending is safe.
			if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				if _, err := u.EmailMessages().Release(ctx, p.MessageID, ""); err != nil {
					return err
				}
				_, err := u.EmailMessages().Start(ctx, p.MessageID, clock.Now())
				return err
			}); err != nil {
				return err
			}
		}
		r, err := provider.Send(ctx, ports.OutboundEmail{Key: key, From: from, To: []string{p.To}, Subject: p.Subject, Text: p.Text, Date: clock.Now()})
		switch {
		case err == nil:
			now := clock.Now()
			return finish(storage.EmailOutcome{Status: storage.EmailAccepted, Provider: provider.Name(), ProviderID: r.ProviderID, AcceptedAt: &now})
		case errors.Is(err, ports.ErrEmailRefused) && row.MaxAttempts > 0 && row.Attempts >= row.MaxAttempts:
			if ferr := finish(storage.EmailOutcome{Status: storage.EmailFailed, Provider: provider.Name(), LastError: err.Error()}); ferr != nil {
				return ferr
			}
			return nil
		case errors.Is(err, ports.ErrEmailRefused):
			if rerr := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				_, err := u.EmailMessages().Release(ctx, p.MessageID, err.Error())
				return err
			}); rerr != nil {
				return rerr
			}
			return err
		}
		return err
	}
}
