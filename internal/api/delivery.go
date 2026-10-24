package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the delivery log and a test message per channel (FR-NTF-05, rev 0.113)

// EmailMessagePage is a page of the delivery log.
type EmailMessagePage struct {
	Items      []EmailMessage `json:"items" doc:"The messages, newest first, each with its attempts and outcome; never a body."`
	NextCursor string         `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// TestNotification is the notification the caller sent themselves.
type TestNotification struct {
	EventID string `json:"event_id" format:"uuid" doc:"The event: the worker fans it out to the caller in a moment." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f70"`
	Title   string `json:"title" doc:"What the notification says." example:"A test notification you sent at 10:05 Singapore time"`
	Email   string `json:"email" enum:"immediate,daily,weekly,off" doc:"How it goes by email, as the caller's preference for test notifications says: at once unless they chose otherwise, and not at all without a verified sign-in email." example:"immediate"`
}

type emailMessagesInput struct {
	PageQuery
	Status  string `query:"status" enum:"queued,accepted,failed,unknown" doc:"Only messages with this outcome." example:"failed"`
	Purpose string `query:"purpose" enum:"test,sign_in_alert,notification,digest,invoice,quote,reminder,statement" doc:"Only messages of this purpose." example:"notification"`
}

type emailMessagesOutput struct{ Body EmailMessagePage }

type testNotificationInput struct {
	KeyHeader
	PreferHeader
}

type testNotificationOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     TestNotification
}

func registerDelivery(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListEmailMessages, huma.Operation{
		OperationID: "listEmailMessages", Method: http.MethodGet, Path: "/v1/organisation/email/messages", Tags: []string{"Organisation"},
		Summary: "List the email delivery log",
		Description: "Every email the organisation queued, newest first: test messages, notifications and digests, each with its recipient, " +
			"subject, attempts, the provider and the outcome, never its body (FR-NTF-05, FR-INT-21). A refused attempt is retried by the " +
			"outbox until the last, and a message whose outcome is unknown is never sent again on a guess. Read one for what the provider " +
			"reported after. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *emailMessagesInput) (*emailMessagesOutput, error) {
		f := storage.EmailMessageFilter{Status: in.Status, Purpose: in.Purpose, Limit: in.PageSize + 1}
		if in.Cursor != "" {
			after, err := decodeCursor(in.Cursor)
			if err != nil {
				return nil, err
			}
			if f.Before, err = uuid.Parse(after); err != nil {
				return nil, badCursor()
			}
		}
		p, _ := authn.From(ctx)
		out := EmailMessagePage{Items: []EmailMessage{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			ms, err := u.EmailMessages().List(ctx, f)
			if err != nil {
				return err
			}
			if len(ms) > in.PageSize {
				ms = ms[:in.PageSize]
				out.NextCursor = encodeCursor(ms[len(ms)-1].ID.String())
			}
			for _, m := range ms {
				out.Items = append(out.Items, emailMessageOf(m))
			}
			return nil
		})
		return &emailMessagesOutput{Body: out}, err
	})

	register(a, authz.SendTestNotification, huma.Operation{
		OperationID: "sendTestNotification", Method: http.MethodPost, Path: "/v1/notifications/test-messages", Tags: []string{"Notifications"},
		DefaultStatus: http.StatusAccepted, Summary: "Send yourself a test notification",
		Description: "Sends the caller a notification of the type notification.test through every channel they receive it on (FR-NTF-05): " +
			"in the app always, and by email at once unless their preference for it says otherwise, to their verified sign-in email. " +
			"It goes through the same event stream, fan-out and outbox as any notification, so its email shows in the delivery log. " +
			"Side effects: the event, and the caller's notification and email once the worker fans it out (202). Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and sends nothing.",
	}, append([]string{CodeCaptureNoUser, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, locks...),
		func(ctx context.Context, in *testNotificationInput) (*testNotificationOutput, error) {
			user, err := person(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (TestNotification, error) {
				now := a.deps.Clock.Now()
				title := fmt.Sprintf("A test notification you sent at %s Singapore time", now.In(appclock.Singapore).Format("15:04"))
				out := TestNotification{Title: title, Email: storage.EmailOff}
				prefs, err := u.Notifications().Preferences(ctx, user)
				if err != nil {
					return out, err
				}
				emails, err := u.InboundMessages().MemberEmails(ctx)
				if err != nil {
					return out, err
				}
				if t, ok := notify.Lookup(notify.TestType); ok && emails[user] != "" {
					out.Email = notify.Cadence(t, prefs)
				}
				if a.deps.Outbox == nil {
					return out, nil
				}
				p, _ := authn.From(ctx)
				ev, err := notify.Emit(ctx, *a.deps.Outbox, u, p.Organisation, notify.Event{Type: notify.TestType, RecordType: "user", RecordID: &user,
					Title: title, Link: "/settings/profile", Audience: notify.Audience{Users: []uuid.UUID{user}}, At: now})
				out.EventID = ev.ID.String()
				return out, err
			})
			if err != nil {
				return nil, err
			}
			return &testNotificationOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
