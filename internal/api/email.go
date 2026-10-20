package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ outbound email (FR-INT-21, FR-NTF-05)

// CodeNoEmailAddress: the organisation has no email address to send a test message to.
const CodeNoEmailAddress = "EML-01"

// EmailMessage is the record of an outbound email: never its body.
type EmailMessage struct {
	ID         string     `json:"id" format:"uuid" doc:"The message's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40"`
	Purpose    string     `json:"purpose" enum:"test" doc:"Why it was sent." example:"test"`
	Recipient  string     `json:"recipient" doc:"Who it goes to." example:"accounts@example.com"`
	Subject    string     `json:"subject" doc:"Its subject." example:"Fyniq test message"`
	Status     string     `json:"status" enum:"queued,accepted,failed,unknown" doc:"queued until the worker sends it; accepted when the provider took it; failed when every attempt was refused; unknown when an attempt may have handed it over and the provider cannot say, so it is never sent again on a guess." example:"queued"`
	Provider   string     `json:"provider,omitempty" doc:"The provider that took or refused it." example:"smtp"`
	Attempts   int        `json:"attempts" doc:"How many times the worker tried." example:"1"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty" doc:"When the provider accepted it." example:"2026-10-20T09:00:05Z"`
	LastError  string     `json:"last_error,omitempty" doc:"Why the last attempt did not end it." example:"the email provider refused the message: 554 rejected"`
	CreatedAt  time.Time  `json:"created_at" doc:"When it was queued." example:"2026-10-20T09:00:00Z"`
}

// EmailMessagePath names a message.
type EmailMessagePath struct {
	MessageID string `path:"message_id" format:"uuid" doc:"The message's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40"`
}

type testMessageInput struct {
	KeyHeader
	PreferHeader
}

type emailMessageOutput struct {
	Status   int
	Location string `header:"Location" doc:"The message's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     EmailMessage
}

type emailMessageReadOutput struct{ Body EmailMessage }

func emailMessageOf(m storage.EmailMessage) EmailMessage {
	return EmailMessage{ID: m.ID.String(), Purpose: m.Purpose, Recipient: m.Recipient, Subject: m.Subject, Status: m.Status, Provider: m.Provider,
		Attempts: m.Attempts, AcceptedAt: m.AcceptedAt, LastError: m.LastError, CreatedAt: m.CreatedAt.UTC()}
}

// testMessageText is the body of a test message: no organisation data beyond its name.
const testMessageText = "This is a test message from Fyniq.\n\nIt was sent to the organisation's own email address to check that " +
	"outbound email works. No action is needed.\n"

func registerEmail(a *API) {
	register(a, authz.SendTestEmail, huma.Operation{
		OperationID: "sendTestEmail", Method: http.MethodPost, Path: "/v1/organisation/email/test-messages", Tags: []string{"Organisation"},
		DefaultStatus: http.StatusCreated, Summary: "Send a test email",
		Description: "Queues a test message to the organisation's own email address, from its profile, to check outbound email (FR-INT-21, " +
			"FR-NTF-05). It goes only to that address, so this cannot send mail anywhere else. Side effects: the message's record, its " +
			"audit event and its send in the outbox; the worker sends it once, and the record shows the outcome. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and queues nothing.",
	}, []string{CodeNoEmailAddress, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, storage.CodeRetry, storage.CodeLockTimeout},
		func(ctx context.Context, in *testMessageInput) (*emailMessageOutput, error) {
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, struct{}{}, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (EmailMessage, error) {
				o, err := u.Organisation(ctx)
				if err != nil {
					return EmailMessage{}, err
				}
				if o.Profile.Email == "" {
					return EmailMessage{}, problem(CodeNoEmailAddress, "The organisation has no email address to send the test message to.",
						"Set the organisation's email in its profile, then send the test again")
				}
				if a.deps.Outbox == nil {
					return EmailMessage{}, problem(CodeUnavailable, "Background work is not configured.", "Configure the outbox, then try again.")
				}
				m, err := email.Queue(ctx, *a.deps.Outbox, u, p.Organisation, "test", o.Profile.Email, "Fyniq test message", testMessageText)
				return emailMessageOf(m), err
			})
			if err != nil {
				return nil, err
			}
			out := &emailMessageOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.Location = "/v1/organisation/email/messages/" + w.Body.ID
			}
			return out, nil
		})

	register(a, authz.GetEmailMessage, huma.Operation{
		OperationID: "getEmailMessage", Method: http.MethodGet, Path: "/v1/organisation/email/messages/{message_id}", Tags: []string{"Organisation"},
		Summary:     "Get an email message's outcome",
		Description: "The record of an outbound message: its recipient, subject, attempts and outcome, never its body (FR-INT-21). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *EmailMessagePath) (*emailMessageReadOutput, error) {
		id, err := parseID("message_id", in.MessageID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out emailMessageReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			m, err := u.EmailMessages().Get(ctx, id)
			out.Body = emailMessageOf(m)
			return err
		})
		return &out, err
	})
}
