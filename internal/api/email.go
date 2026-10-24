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
	"github.com/cognizhi/fyniq/internal/maildns"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ outbound email (FR-INT-21, FR-NTF-05)

// The email codes.
const (
	CodeNoEmailAddress = "EML-01" // the organisation has no email address to send a test message to
	CodeSenderAddress  = "EML-02" // not an address, or a name that cannot go in a header, or a name without an address
	CodeSenderDomain   = "EML-03" // the address is not on a domain the deployment sends from
	CodeReleased       = "EML-04" // the suppression was released already
)

// MailInfo is the deployment's outbound email as the API shows it (FR-INT-21; the owner's decision of 23 Oct 2026,
// PRD-001 rev 0.102): the providers' names in order, the platform's sender, and the domains an organisation may
// choose its senders on. The providers' keys never reach the API.
type MailInfo struct {
	Providers []string
	From      string
	Domains   []string
	// Check is the sending domains' DNS, as last checked and when (rev 0.104); nil: none is checked.
	Check func(ctx context.Context) ([]maildns.Result, time.Time)
	// CaptureDomain is the domain of the email-in addresses (rev 0.105); empty: email-in is not set up.
	CaptureDomain string
}

// DomainRecord is a record a sending domain's providers asked for.
type DomainRecord struct {
	Type   string `json:"type" enum:"TXT,CNAME" doc:"The record's type." example:"TXT"`
	Name   string `json:"name" doc:"Its name." example:"1234._domainkey.mail.example.com"`
	Value  string `json:"value" doc:"What it should hold: text a TXT record contains, or a CNAME's target." example:"v=DKIM1; k=rsa; p=MIGfMA0"`
	Status string `json:"status" enum:"ok,missing,wrong,failed" doc:"ok: there as asked; missing: no such record; wrong: another value; failed: the lookup failed, so try later." example:"ok"`
	Found  string `json:"found,omitempty" doc:"What a wrong record holds, kept short." example:"v=DKIM1; p=OTHERKEY"`
}

// DomainCheck is a sending domain's DNS, as last checked.
type DomainCheck struct {
	Domain    string         `json:"domain" doc:"The sending domain." example:"mail.example.com"`
	OK        bool           `json:"ok" doc:"true when every record its providers asked for is there and its DMARC policy quarantines or rejects." example:"true"`
	DMARC     string         `json:"dmarc" enum:"reject,quarantine,none,invalid,missing,failed" doc:"Its DMARC policy, its own or its parent domain's: none only reports; missing has no record; failed could not be looked up." example:"quarantine"`
	Records   []DomainRecord `json:"records" doc:"The records its providers asked for, as the deployment lists them."`
	CheckedAt time.Time      `json:"checked_at" doc:"When it was checked: at most an hour ago, and every night." example:"2026-10-23T09:00:00Z"`
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
	DomainChecks   []DomainCheck   `json:"domain_checks" doc:"Each sending domain's DNS records and DMARC policy, checked within the hour and every night (FR-INT-21)."`
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
	ID         string       `json:"id" format:"uuid" doc:"The message's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4e40"`
	Purpose    string       `json:"purpose" enum:"test,sign_in_alert,notification,digest,invoice,quote,reminder" doc:"Why it was sent: a test message, a sign-in alert, a notification, a digest of them, an invoice or a quote emailed to a customer (FR-AR-06), or a reminder of one (FR-AR-08)." example:"test"`
	Recipient  string       `json:"recipient" doc:"Who it goes to." example:"accounts@example.com"`
	Subject    string       `json:"subject" doc:"Its subject." example:"Fyniq test message"`
	Status     string       `json:"status" enum:"queued,accepted,failed,unknown" doc:"queued until the worker sends it; accepted when the provider took it; failed when every attempt was refused; unknown when an attempt may have handed it over and the provider cannot say, so it is never sent again on a guess." example:"queued"`
	Provider   string       `json:"provider,omitempty" doc:"The provider that took or refused it." example:"smtp"`
	Attempts   int          `json:"attempts" doc:"How many times the worker tried." example:"1"`
	AcceptedAt *time.Time   `json:"accepted_at,omitempty" doc:"When the provider accepted it." example:"2026-10-20T09:00:05Z"`
	LastError  string       `json:"last_error,omitempty" doc:"Why the last attempt did not end it." example:"the email provider refused the message: 554 rejected"`
	CreatedAt  time.Time    `json:"created_at" doc:"When it was queued." example:"2026-10-20T09:00:00Z"`
	Events     []EmailEvent `json:"events,omitempty" doc:"What the provider reported after it took the message, oldest first: some providers never report delivered. Only when one message is read."`
}

// EmailEvent is a provider's report about a message it took.
type EmailEvent struct {
	Kind       string    `json:"kind" enum:"delivered,delayed,soft_bounce,hard_bounce,complaint,suppressed,failed" doc:"What happened: a hard bounce, a complaint or the provider's suppression puts the address on the suppression list." example:"hard_bounce"`
	Provider   string    `json:"provider" enum:"zeptomail,resend" doc:"Who reported it." example:"zeptomail"`
	Detail     string    `json:"detail,omitempty" doc:"The provider's reason." example:"550 5.1.1 unknown user"`
	OccurredAt time.Time `json:"occurred_at" doc:"When it happened, as the provider says." example:"2026-10-20T09:00:40Z"`
}

// EmailSuppression keeps mail from an address until an Administrator releases it.
type EmailSuppression struct {
	ID           string     `json:"id" format:"uuid" doc:"The suppression's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	Address      string     `json:"address" doc:"The address, in lower case." example:"founder@example.com"`
	Reason       string     `json:"reason" enum:"hard_bounce,complaint,suppressed" doc:"Why: the address bounced hard, its owner marked mail as spam, or the provider suppressed it." example:"hard_bounce"`
	SuppressedAt time.Time  `json:"suppressed_at" doc:"When." example:"2026-10-20T09:00:41Z"`
	ReleasedAt   *time.Time `json:"released_at,omitempty" doc:"When it was released; absent while mail to it is not sent." example:"2026-10-21T08:00:00Z"`
}

// EmailSuppressionPage is a page of suppressions, newest first.
type EmailSuppressionPage struct {
	Items      []EmailSuppression `json:"items" nullable:"false" doc:"The suppressions, newest first."`
	NextCursor string             `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// EmailSuppressionPath names a suppression.
type EmailSuppressionPath struct {
	SuppressionID string `path:"suppression_id" format:"uuid" doc:"The suppression's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
}

type suppressionsInput struct {
	PageQuery
	All bool `query:"all" doc:"true lists released suppressions too." example:"false"`
}

type releaseSuppressionInput struct {
	EmailSuppressionPath
	KeyHeader
	PreferHeader
}

type suppressionsOutput struct{ Body EmailSuppressionPage }

type suppressionOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     EmailSuppression
}

func suppressionOf(s storage.EmailSuppression) EmailSuppression {
	out := EmailSuppression{ID: s.ID.String(), Address: s.Address, Reason: s.Reason, SuppressedAt: s.SuppressedAt.UTC()}
	if s.ReleasedAt != nil {
		at := s.ReleasedAt.UTC()
		out.ReleasedAt = &at
	}
	return out
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
			"deployment's operators set, with each domain's DNS records and DMARC policy as checked within the hour; and the " +
			"organisation's sender of each purpose, or the platform's. No provider key is ever shown. Read-only; no side effects.",
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
		out.DomainChecks = []DomainCheck{}
		if a.deps.Mail.Check != nil {
			results, at := a.deps.Mail.Check(ctx)
			for _, r := range results {
				c := DomainCheck{Domain: r.Domain, OK: r.OK, DMARC: r.DMARC, Records: []DomainRecord{}, CheckedAt: at.UTC()}
				for _, x := range r.Records {
					c.Records = append(c.Records, DomainRecord{Type: x.Type, Name: x.Name, Value: x.Value, Status: x.Status, Found: x.Found})
				}
				out.DomainChecks = append(out.DomainChecks, c)
			}
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
			if err != nil {
				return err
			}
			out.Body = emailMessageOf(m)
			evs, err := u.EmailMessages().Events(ctx, id)
			for _, e := range evs {
				out.Body.Events = append(out.Body.Events, EmailEvent{Kind: e.Kind, Provider: e.Provider, Detail: e.Detail, OccurredAt: e.OccurredAt.UTC()})
			}
			return err
		})
		return &out, err
	})

	register(a, authz.ListEmailSuppressions, huma.Operation{
		OperationID: "listEmailSuppressions", Method: http.MethodGet, Path: "/v1/organisation/email/suppressions", Tags: []string{"Organisation"},
		Summary: "List suppressed addresses",
		Description: "The addresses no mail is sent to, newest first (FR-INT-21): each bounced hard, complained, or was suppressed by the provider, " +
			"as its signed webhook reported; a message to one fails without being sent. Released ones too with all=true. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *suppressionsInput) (*suppressionsOutput, error) {
		p, _ := authn.From(ctx)
		out := EmailSuppressionPage{Items: []EmailSuppression{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.EmailSuppressions().List(ctx, in.All)
			if err != nil {
				return err
			}
			if in.Cursor != "" {
				after, err := decodeCursor(in.Cursor)
				if err != nil {
					return err
				}
				i := slices.IndexFunc(all, func(s storage.EmailSuppression) bool { return s.ID.String() == after })
				if i < 0 {
					return badCursor()
				}
				all = all[i+1:]
			}
			if len(all) > in.PageSize {
				all = all[:in.PageSize]
				out.NextCursor = encodeCursor(all[len(all)-1].ID.String())
			}
			for _, s := range all {
				out.Items = append(out.Items, suppressionOf(s))
			}
			return nil
		})
		return &suppressionsOutput{Body: out}, err
	})

	register(a, authz.ReleaseEmailSuppression, huma.Operation{
		OperationID: "releaseEmailSuppression", Method: http.MethodPost, Path: "/v1/organisation/email/suppressions/{suppression_id}/release",
		Tags: []string{"Organisation"}, Summary: "Release a suppressed address",
		Description: "Lets mail go to a suppressed address again (FR-INT-21), once its owner has fixed the mailbox or asked for mail: the next " +
			"message is sent, and a later hard bounce or complaint suppresses it again. Side effects: the release and its audit event. Refused " +
			"when it was released already. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeReleased}, locks...),
		func(ctx context.Context, in *releaseSuppressionInput) (*suppressionOutput, error) {
			id, err := parseID("suppression_id", in.SuppressionID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (EmailSuppression, error) {
				s, err := u.EmailSuppressions().Release(ctx, id)
				if storage.CodeOf(err) == storage.CodeVersion {
					return EmailSuppression{}, problem(CodeReleased, "The address was released already.",
						"Nothing to do: mail is sent to it, and a later hard bounce or complaint suppresses it again")
				}
				return suppressionOf(s), err
			})
			if err != nil {
				return nil, err
			}
			return &suppressionOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
