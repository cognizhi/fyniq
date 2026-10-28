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
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is the outbox kind of a send, run by the worker class.
const Kind = "email.send"

type payload struct {
	MessageID   uuid.UUID   `json:"message_id"`
	To          string      `json:"to"`
	Subject     string      `json:"subject"`
	Text        string      `json:"text"`
	Attachments []uuid.UUID `json:"attachments,omitempty"` // the evidence files it carries, such as an invoice's PDF
}

// Files reads the organisation's evidence files a message carries (evidence.Files).
type Files func(ctx context.Context, org uuid.UUID, ids []uuid.UUID) ([]ports.EmailAttachment, error)

// Queue records a message to one recipient and writes its send to the outbox, in the unit of work u.
func Queue(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, purpose, to, subject, text string) (storage.EmailMessage, error) {
	return QueueWith(ctx, d, u, org, purpose, to, subject, text, nil)
}

// QueueWith is Queue for a message that carries evidence files, such as the PDF of an invoice sent (FR-AR-06): the
// outbox job names them, and the worker reads them as it sends, so a file never travels in the job.
func QueueWith(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, purpose, to, subject, text string, files []uuid.UUID) (storage.EmailMessage, error) {
	m, err := u.EmailMessages().Queue(ctx, purpose, to, subject)
	if err != nil {
		return storage.EmailMessage{}, err
	}
	b, err := json.Marshal(payload{MessageID: m.ID, To: to, Subject: subject, Text: text, Attachments: files})
	if err != nil {
		return storage.EmailMessage{}, err
	}
	if _, err := d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: b,
		BusinessKey: "email:" + m.ID.String()}); err != nil {
		return storage.EmailMessage{}, err
	}
	return m, nil
}

// Reference is what a provider echoes in its webhooks for a message: its organisation and its ID.
func Reference(org, message uuid.UUID) string { return org.String() + "_" + message.String() }

// ParseReference reads a Reference.
func ParseReference(s string) (org, message uuid.UUID, ok bool) {
	a, b, found := strings.Cut(s, "_")
	o, err1 := uuid.Parse(a)
	m, err2 := uuid.Parse(b)
	if !found || err1 != nil || err2 != nil || len(a) != 36 || len(b) != 36 {
		return uuid.Nil, uuid.Nil, false
	}
	return o, m, true
}

// SenderOf is the sender purpose of a message's purpose (FR-INT-21): a sign-in alert is security's; an invoice or a
// quote sent to a customer (FR-AR-06) the invoices sender's; a reminder of one (FR-AR-08) the reminders sender's; the
// rest so far are notifications.
func SenderOf(messagePurpose string) string {
	switch messagePurpose {
	case "sign_in_alert":
		return storage.SenderSecurity
	case PurposeInvoice, PurposeQuote, PurposeStatement, PurposeRemittance:
		return storage.SenderInvoices
	case PurposeReminder:
		return storage.SenderReminders
	}
	return storage.SenderNotifications
}

// The purposes of the email that carries a document to a customer (FR-AR-06), and of a reminder of one (FR-AR-08).
const (
	PurposeInvoice  = "invoice"
	PurposeQuote    = "quote"
	PurposeReminder = "reminder"
	// PurposeStatement is a statement of account (FR-AR-08), from the invoices sender.
	PurposeStatement = "statement"
	// PurposeRemittance is a supplier's remittance advice (FR-BNK-12), from the invoices sender: the organisation's accounts
	// address until a purchases sender is configured.
	PurposeRemittance = "remittance"
)

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
//   - A message to an address on the suppression list is failed without being sent.
//   - A message an earlier attempt may have handed over is reconciled first, with the provider that attempt went
//     to: the provider says whether it took it; when it cannot say (SMTP, ZeptoMail), the message is recorded as
//     unknown and never sent again on a guess.
//   - A refusal moves on to the next provider: failover only for a send known not to have been accepted. When
//     every provider refused it, the attempt is cleared, so the outbox retries it; on the last attempt the
//     message is failed.
//   - Any other error leaves the attempt marked with its provider: the next run reconciles it.
func Work(providers []ports.Email, store storage.Store, from string, clock ports.Clock) outbox.Work {
	return WorkWith(providers, store, from, clock, nil)
}

// ProviderSet is the outbound email of one message's work: the providers it may send through now, in order, and every
// provider it may reconcile an earlier attempt with, by name.
type ProviderSet struct {
	Enabled []ports.Email
	ByName  map[string]ports.Email
}

// Providers gives the providers for one piece of work, read when it starts (FR-INT-20: never from a cached flag): the
// connectors' (connector.Runtime), or a fixed list.
type Providers func(ctx context.Context, org uuid.UUID) (ProviderSet, error)

// Static is a fixed list of providers, every one enabled.
func Static(providers []ports.Email) Providers {
	set := ProviderSet{Enabled: providers, ByName: map[string]ports.Email{}}
	for _, p := range providers {
		set.ByName[p.Name()] = p
	}
	return func(context.Context, uuid.UUID) (ProviderSet, error) { return set, nil }
}

// WorkWith is Work with the reader of the files messages carry. The files are read before any attempt is marked, so a
// file that cannot be read yet leaves the message to the outbox's retry, and fails it on the last attempt; nil files
// fails a message that carries any.
func WorkWith(providers []ports.Email, store storage.Store, from string, clock ports.Clock, files Files) outbox.Work {
	if len(providers) == 0 {
		return func(context.Context, uuid.UUID, storage.OutboxRow) error { return errors.New("email: no provider is configured") }
	}
	return WorkFrom(Static(providers), store, from, clock, files)
}

// WorkFrom is WorkWith with the providers read for each message (the connectors'). When none is enabled the message
// waits, held without spending an attempt (outbox.ErrHeld), unless an earlier attempt must be reconciled first: that
// happens with the provider it went to, enabled, paused or disabled (FR-INT-26: in-flight work is reconciled).
func WorkFrom(source Providers, store storage.Store, from string, clock ports.Clock, files Files) outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p payload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.MessageID == uuid.Nil {
			return fmt.Errorf("email: outbox row %s is not a send", row.ID)
		}
		set, err := source(ctx, org)
		if err != nil {
			return err
		}
		providers := set.Enabled
		mark := ""
		if len(providers) > 0 {
			mark = providers[0].Name()
		} else {
			var m storage.EmailMessage
			if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				m, err = u.EmailMessages().Get(ctx, p.MessageID)
				return err
			}); err != nil {
				return err
			}
			if m.Status != storage.EmailQueued {
				return nil
			}
			if m.AttemptStartedAt == nil {
				return fmt.Errorf("%w: no email connector is enabled", outbox.ErrHeld)
			}
			mark = m.Provider // an earlier attempt to reconcile; nothing new is marked
		}
		finish := func(o storage.EmailOutcome) error {
			return store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				_, err := u.EmailMessages().Finish(ctx, p.MessageID, o)
				return err
			})
		}
		var attached []ports.EmailAttachment
		if len(p.Attachments) > 0 {
			var err error
			if files == nil {
				err = errors.New("no reader of evidence files is configured")
			} else {
				attached, err = files(ctx, org, p.Attachments)
			}
			if err != nil {
				if row.MaxAttempts > 0 && row.Attempts >= row.MaxAttempts {
					return finish(storage.EmailOutcome{Status: storage.EmailFailed, LastError: "not sent: its attachment could not be read: " + err.Error()})
				}
				return fmt.Errorf("email: the attachments of message %s: %w", p.MessageID, err)
			}
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
		var suppressed *storage.EmailSuppression
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			if before, err = u.EmailMessages().Start(ctx, p.MessageID, clock.Now(), mark); err != nil {
				return err
			}
			if senders, err = u.EmailSenders().InForce(ctx); err != nil {
				return err
			}
			switch s, err := u.EmailSuppressions().InForce(ctx, p.To); {
			case err == nil:
				suppressed = &s
			case !errors.Is(err, ports.ErrNotFound):
				return err
			}
			return nil
		}); err != nil {
			return err
		}
		if before.Status != storage.EmailQueued {
			return nil
		}
		if suppressed != nil && before.AttemptStartedAt == nil {
			return finish(storage.EmailOutcome{Status: storage.EmailFailed, LastError: fmt.Sprintf("not sent: %s is suppressed after a %s on %s; "+
				"an Administrator releases it in the email settings", suppressed.Address, strings.ReplaceAll(suppressed.Reason, "_", " "),
				suppressed.SuppressedAt.UTC().Format(time.DateOnly))})
		}
		m := ports.OutboundEmail{Key: p.MessageID.String(), Reference: Reference(org, p.MessageID), From: fromOf(senders, before.Purpose, from),
			To: []string{p.To}, Subject: p.Subject, Text: p.Text, Date: clock.Now(), Attachments: attached}
		if before.AttemptStartedAt != nil {
			went, ok := set.ByName[before.Provider]
			if !ok {
				return finish(storage.EmailOutcome{Status: storage.EmailUnknown, Provider: before.Provider,
					LastError: "an earlier attempt through " + before.Provider + ", no longer configured, may have handed the message over: it is not sent again"})
			}
			r, err := went.Status(ctx, m, *before.AttemptStartedAt)
			switch {
			case err == nil:
				return accepted(went, r)
			case errors.Is(err, ports.ErrEmailUnknown):
				return finish(storage.EmailOutcome{Status: storage.EmailUnknown, Provider: went.Name(),
					LastError: "an earlier attempt may have handed the message over, and the provider cannot say: it is not sent again"})
			case !errors.Is(err, ports.ErrEmailRefused):
				return err // ask again on the next run
			}
			// The provider never took it: sending is safe, through the first enabled provider, or later when none is.
			if len(providers) == 0 {
				if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
					_, err := u.EmailMessages().Release(ctx, p.MessageID, "")
					return err
				}); err != nil {
					return err
				}
				return fmt.Errorf("%w: no email connector is enabled", outbox.ErrHeld)
			}
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
