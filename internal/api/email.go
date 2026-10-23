package api

import (
	"context"
	"fmt"
	"net/http"
	netmail "net/mail"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ outbound email (FR-INT-21, FR-NTF-05)

// The email codes.
const (
	CodeNoEmailAddress = "EML-01" // the organisation has no email address to send a test message to
	CodeSenderAddress  = "EML-02" // not an address, or a name that cannot go in a header, or a name without an address
	CodeSenderDomain   = "EML-03" // the address is not on a domain the deployment sends from
)

// MailInfo is the deployment's outbound email as the API shows it (FR-INT-21; the owner's decision of 23 Oct 2026,
// PRD-001 rev 0.102): the providers' names in order, the platform's sender, and the domains an organisation may
// choose its senders on. The providers' keys never reach the API.
type MailInfo struct {
	Providers []string
	From      string
	Domains   []string
}

// EmailProvider is one of the deployment's email providers.
type EmailProvider struct {
	Name string `json:"name" enum:"smtp,zeptomail,resend" doc:"The provider." example:"zeptomail"`
	Role string `json:"role" enum:"primary,failover" doc:"primary sends; failover sends only what the providers before it refused." example:"primary"`
}

// SendingDomain is a domain the deployment sends email from.
type SendingDomain string

// Schema implements huma.SchemaProvider: a domain, with an example.
func (SendingDomain) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A domain the deployment sends email from.", Examples: []any{"mail.example.com"}}
}

// EmailSender is the address a purpose's email is sent from.
type EmailSender struct {
	Purpose     string     `json:"purpose" enum:"notifications,invoices,reminders,security" doc:"What the email is for: notifications, invoices and quotes, reminders, or security notices such as sign-in alerts." example:"invoices"`
	Address     string     `json:"address,omitempty" doc:"The organisation's sender of this purpose; absent while it uses the platform's." example:"accounts@mail.example.com"`
	DisplayName string     `json:"display_name,omitempty" doc:"The name shown with the address." example:"Example Pte Ltd"`
	From        string     `json:"from" doc:"What the email's From says: the organisation's sender, or the platform's." example:"Example Pte Ltd <accounts@mail.example.com>"`
	Platform    bool       `json:"platform" doc:"true while this purpose uses the platform's sender." example:"false"`
	SetAt       *time.Time `json:"set_at,omitempty" doc:"When the organisation last chose it." example:"2026-10-23T09:00:00Z"`
	ETag        string     `json:"etag" doc:"The choice in force: send it in If-Match to change it." example:"\"none\""`
}

// EmailSettings is the organisation's outbound email: the deployment's providers and domains, and its senders.
type EmailSettings struct {
	Providers      []EmailProvider `json:"providers" doc:"The providers in order: the first sends, the next only what the ones before it refused. None: no email is sent."`
	PlatformSender string          `json:"platform_sender,omitempty" doc:"The platform's sender, for a purpose the organisation has not chosen one for." example:"Fyniq <no-reply@mail.example.com>"`
	Domains        []SendingDomain `json:"domains" doc:"The domains a sender may be chosen on; the deployment's operators add one."`
	Senders        []EmailSender   `json:"senders" doc:"Every purpose's sender, in the order notifications, invoices, reminders, security."`
}

// EmailSenderChange chooses a purpose's sender: an address on a sending domain, or neither field, to use the platform's.
type EmailSenderChange struct {
	Address     string `json:"address,omitempty" maxLength:"254" doc:"An address on one of the sending domains; leave it out to use the platform's sender." example:"accounts@mail.example.com"`
	DisplayName string `json:"display_name,omitempty" maxLength:"80" doc:"The name shown with it; only with an address." example:"Example Pte Ltd"`
}

// EmailSenderPath names a purpose.
type EmailSenderPath struct {
	Purpose string `path:"purpose" enum:"notifications,invoices,reminders,security" doc:"The purpose." example:"invoices"`
}

type emailSenderInput struct {
	EmailSenderPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body EmailSenderChange
}

type emailSettingsOutput struct{ Body EmailSettings }

type emailSenderOutput struct {
	ETag     string `header:"ETag" doc:"The choice in force, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     EmailSender
}

// senderPurposes are the purposes, in the order the settings give them.
var senderPurposes = []string{storage.SenderNotifications, storage.SenderInvoices, storage.SenderReminders, storage.SenderSecurity}

func senderETag(c *storage.EmailSender) string {
	if c == nil {
		return etag("none")
	}
	return etag(c.ID)
}

func senderInForce(cs []storage.EmailSender, purpose string) *storage.EmailSender {
	i := slices.IndexFunc(cs, func(c storage.EmailSender) bool { return c.Purpose == purpose })
	if i < 0 {
		return nil
	}
	return &cs[i]
}

// senderOf is purpose's sender as the API shows it, from its choice in force, c (nil: none ever made).
func (a *API) senderOf(purpose string, c *storage.EmailSender) EmailSender {
	out := EmailSender{Purpose: purpose, From: a.deps.Mail.From, Platform: true, ETag: senderETag(c)}
	if c != nil {
		at := c.SetAt.UTC()
		out.SetAt = &at
		if c.Address != "" {
			out.Address, out.DisplayName, out.Platform = c.Address, c.DisplayName, false
			out.From = (&netmail.Address{Name: c.DisplayName, Address: c.Address}).String()
		}
	}
	return out
}

// checkSender reads a sender change: the address, its domain in lower case, on a sending domain; the name only with
// it. Both empty: the platform's sender.
func (a *API) checkSender(b EmailSenderChange) (address, name string, _ error) {
	address, name = strings.TrimSpace(b.Address), strings.TrimSpace(b.DisplayName)
	if address == "" {
		if name != "" {
			return "", "", problem(CodeSenderAddress, "A display name needs an address.", "Give the address too, or leave both out to use the platform's sender",
				FieldProblem{CodeSenderAddress, "body.display_name", "a name without an address", "Give the address, or leave the name out"})
		}
		return "", "", nil
	}
	parsed, err := netmail.ParseAddress(address)
	at := strings.LastIndex(address, "@")
	if err != nil || parsed.Name != "" || parsed.Address != address || at < 1 || len(address[:at]) > 64 || strings.ContainsAny(address, " <>\"") {
		return "", "", problem(CodeSenderAddress, fmt.Sprintf("%q is not an email address.", address), "Give one address, such as accounts@mail.example.com, without a name",
			FieldProblem{CodeSenderAddress, "body.address", "not an address", "Give one address, such as accounts@mail.example.com"})
	}
	if strings.ContainsAny(name, "\r\n<>\"") {
		return "", "", problem(CodeSenderAddress, "The display name holds a line break, angle brackets or quotes.", "Write the name as plain words",
			FieldProblem{CodeSenderAddress, "body.display_name", "a character a header cannot carry", "Write the name as plain words"})
	}
	domain := strings.ToLower(address[at+1:])
	if !slices.Contains(a.deps.Mail.Domains, domain) {
		fix := "No domain is set up for sending: ask the deployment's operators to add one"
		if len(a.deps.Mail.Domains) > 0 {
			fix = "Choose an address on " + strings.Join(a.deps.Mail.Domains, ", ") + ", or ask the deployment's operators to add your domain"
		}
		return "", "", problem(CodeSenderDomain, fmt.Sprintf("Email is not sent from %s.", domain), fix,
			FieldProblem{CodeSenderDomain, "body.address", "not on a sending domain", fix})
	}
	return address[:at+1] + domain, name, nil
}

// EmailMessage is the record of an outbound email: never its body.
type EmailMessage struct {
	ID         string     `json:"id" format:"uuid" doc:"The message's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40"`
	Purpose    string     `json:"purpose" enum:"test,sign_in_alert" doc:"Why it was sent: a test message, or a sign-in alert." example:"test"`
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
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
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

	register(a, authz.GetEmailSettings, huma.Operation{
		OperationID: "getEmailSettings", Method: http.MethodGet, Path: "/v1/organisation/email", Tags: []string{"Organisation"},
		Summary: "Get the email settings",
		Description: "The organisation's outbound email (FR-INT-21; the owner's decision of 23 Oct 2026): the deployment's providers in order, " +
			"the first primary and the next its failover, the platform's sender and the domains a sender may be chosen on, which the " +
			"deployment's operators set; and the organisation's sender of each purpose, or the platform's. No provider key is ever shown. " +
			"Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*emailSettingsOutput, error) {
		p, _ := authn.From(ctx)
		out := EmailSettings{PlatformSender: a.deps.Mail.From, Providers: []EmailProvider{}, Domains: []SendingDomain{}}
		for i, n := range a.deps.Mail.Providers {
			role := "failover"
			if i == 0 {
				role = "primary"
			}
			out.Providers = append(out.Providers, EmailProvider{Name: n, Role: role})
		}
		for _, d := range a.deps.Mail.Domains {
			out.Domains = append(out.Domains, SendingDomain(d))
		}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			cs, err := u.EmailSenders().InForce(ctx)
			for _, purpose := range senderPurposes {
				out.Senders = append(out.Senders, a.senderOf(purpose, senderInForce(cs, purpose)))
			}
			return err
		})
		return &emailSettingsOutput{Body: out}, err
	})

	register(a, authz.SetEmailSender, huma.Operation{
		OperationID: "setEmailSender", Method: http.MethodPut, Path: "/v1/organisation/email/senders/{purpose}", Tags: []string{"Organisation"},
		Summary: "Choose a sender",
		Description: "Chooses the address a purpose's email is sent from (FR-INT-21; the owner's decision of 23 Oct 2026): an address on one " +
			"of the deployment's sending domains (EML-03), with a display name, or neither, so the platform's sender is used. Side effects: " +
			"the choice and its audit event; mail queued afterwards is sent from it. Needs If-Match with the sender's ETag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, CodeSenderAddress, CodeSenderDomain}, locks...),
		func(ctx context.Context, in *emailSenderInput) (*emailSenderOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (EmailSender, error) {
				cs, err := u.EmailSenders().InForce(ctx)
				if err != nil {
					return EmailSender{}, err
				}
				if err := checkIfMatch(in.IfMatch, senderETag(senderInForce(cs, in.Purpose))); err != nil {
					return EmailSender{}, err
				}
				address, name, err := a.checkSender(in.Body)
				if err != nil {
					return EmailSender{}, err
				}
				c, err := u.EmailSenders().Set(ctx, in.Purpose, address, name)
				return a.senderOf(in.Purpose, &c), err
			})
			if err != nil {
				return nil, err
			}
			out := &emailSenderOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}
			if !w.Previewed {
				out.ETag = w.Body.ETag
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
