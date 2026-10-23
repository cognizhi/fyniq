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
	netmail "net/mail"
	"slices"
	"strings"

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

// SenderOf is the sender purpose of a message's purpose (FR-INT-21): a sign-in alert is security's; the rest so far
// are notifications.
func SenderOf(messagePurpose string) string {
	if messagePurpose == "sign_in_alert" {
		return storage.SenderSecurity
	}
	return storage.SenderNotifications
}

// fromOf is the address a message of purpose is sent from: the organisation's sender in force for it, or the
// platform's, from.
func fromOf(senders []storage.EmailSender, purpose, from string) string {
	for _, s := range senders {
		if s.Purpose == SenderOf(purpose) && s.Address != "" {
			return (&netmail.Address{Name: s.DisplayName, Address: s.Address}).String()
		}
	}
	return from
}

// Work is the worker's handler of Kind: it sends the message through providers, in order, the first the primary
// and each next one its failover (PRD-001 rev 0.102), from the organisation's sender of its purpose, or the
// platform's, from.
//   - A message with an outcome is left as it is: the job ran before.
//   - A message an earlier attempt may have handed over is reconciled first, with the provider that attempt went
//     to: the provider says whether it took it; when it cannot say (SMTP, ZeptoMail), the message is recorded as
//     unknown and never sent again on a guess.
//   - A refusal moves on to the next provider: failover only for a send known not to have been accepted. When
//     every provider refused it, the attempt is cleared, so the outbox retries it; on the last attempt the
//     message is failed.
//   - Any other error leaves the attempt marked with its provider: the next run reconciles it.
func Work(providers []ports.Email, store storage.Store, from string, clock ports.Clock) outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p payload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.MessageID == uuid.Nil {
			return fmt.Errorf("email: outbox row %s is not a send", row.ID)
		}
		if len(providers) == 0 {
			return errors.New("email: no provider is configured")
		}
		finish := func(o storage.EmailOutcome) error {
			return store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				_, err := u.EmailMessages().Finish(ctx, p.MessageID, o)
				return err
			})
		}
		accepted := func(by ports.Email, r ports.EmailReceipt) error {
			now := clock.Now()
			return finish(storage.EmailOutcome{Status: storage.EmailAccepted, Provider: by.Name(), ProviderID: r.ProviderID, AcceptedAt: &now})
		}
		// retake clears an attempt the provider never took and marks the next one, through next.
		retake := func(next ports.Email, lastError string) error {
			return store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				if _, err := u.EmailMessages().Release(ctx, p.MessageID, lastError); err != nil {
					return err
				}
				_, err := u.EmailMessages().Start(ctx, p.MessageID, clock.Now(), next.Name())
				return err
			})
		}
		var before storage.EmailMessage
		var senders []storage.EmailSender
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			if before, err = u.EmailMessages().Start(ctx, p.MessageID, clock.Now(), providers[0].Name()); err != nil {
				return err
			}
			senders, err = u.EmailSenders().InForce(ctx)
			return err
		}); err != nil {
			return err
		}
		if before.Status != storage.EmailQueued {
			return nil
		}
		m := ports.OutboundEmail{Key: p.MessageID.String(), From: fromOf(senders, before.Purpose, from), To: []string{p.To}, Subject: p.Subject,
			Text: p.Text, Date: clock.Now()}
		if before.AttemptStartedAt != nil {
			i := slices.IndexFunc(providers, func(e ports.Email) bool { return e.Name() == before.Provider })
			if i < 0 {
				return finish(storage.EmailOutcome{Status: storage.EmailUnknown, Provider: before.Provider,
					LastError: "an earlier attempt through " + before.Provider + ", no longer configured, may have handed the message over: it is not sent again"})
			}
			r, err := providers[i].Status(ctx, m, *before.AttemptStartedAt)
			switch {
			case err == nil:
				return accepted(providers[i], r)
			case errors.Is(err, ports.ErrEmailUnknown):
				return finish(storage.EmailOutcome{Status: storage.EmailUnknown, Provider: providers[i].Name(),
					LastError: "an earlier attempt may have handed the message over, and the provider cannot say: it is not sent again"})
			case !errors.Is(err, ports.ErrEmailRefused):
				return err // ask again on the next run
			}
			// The provider never took it: sending is safe.
			if err := retake(providers[0], ""); err != nil {
				return err
			}
		}
		var refused []string
		for i, e := range providers {
			if i > 0 {
				if err := retake(e, strings.Join(refused, "; ")); err != nil {
					return err
				}
			}
			r, err := e.Send(ctx, m)
			switch {
			case err == nil:
				return accepted(e, r)
			case errors.Is(err, ports.ErrEmailRefused):
				refused = append(refused, e.Name()+": "+err.Error())
			default:
				return err // the attempt stays marked with e: the next run reconciles it there
			}
		}
		why := strings.Join(refused, "; ")
		if row.MaxAttempts > 0 && row.Attempts >= row.MaxAttempts {
			return finish(storage.EmailOutcome{Status: storage.EmailFailed, Provider: providers[len(providers)-1].Name(), LastError: why})
		}
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.EmailMessages().Release(ctx, p.MessageID, why)
			return err
		}); err != nil {
			return err
		}
		return fmt.Errorf("%w: %s", ports.ErrEmailRefused, why)
	}
}
