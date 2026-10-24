# communication/email-and-notifications Specification

## Purpose
How Fyniq reaches people and receives mail: the outside-service capabilities with their launch adapters and local substitutes, outbound and inbound email, the notification centre and preferences, announcement banners, Slack and Zoho Cliq, asking from chat, and the one event stream behind every channel. Derived from PRD-001 §8.24.

## Requirements

### Requirement: Outside services behind interchangeable adapters
Every outside service SHALL sit behind a capability interface with interchangeable adapters, so that a provider is a configuration choice, not a code change.

Source: PRD-001 §8.24

#### Scenario: The founders' providers are adapters
- **WHEN** the organisation uses Google and Zoho accounts, Zoho ZeptoMail and Resend for email, and Slack or Zoho Cliq
- **THEN** each of them is used through an adapter
- **AND** none is hard-wired

#### Scenario: Changing provider is configuration
- **WHEN** an organisation switches from one provider of a capability to another
- **THEN** it changes configuration, not code

### Requirement: Launch adapters and local substitutes
The system SHALL ship, for each outside-service capability, the adapters at launch and the local substitute (ADR-T4) listed in the §8.24 capabilities table, each in its phase.

Source: PRD-001 §8.24

#### Scenario: Sign-in
- **WHEN** sign-in adapters are offered (P1)
- **THEN** they are Google, Zoho, any OpenID Connect provider, email and password, and SAML in Enterprise mode
- **AND** the local substitute is the Firebase Authentication emulator with mock providers only, real Zoho and Google sign-in being tested in staging

#### Scenario: Outbound email
- **WHEN** outbound email adapters are offered (P1)
- **THEN** they are Zoho CPaaS (formerly ZeptoMail), Resend and generic SMTP
- **AND** the local substitute is Mailpit

#### Scenario: Inbound email for capture addresses
- **WHEN** inbound email adapters are offered (P1)
- **THEN** they are Resend receiving (webhook, then fetch the message) and mailbox polling over IMAP (e.g. a Zoho Mail mailbox)
- **AND** the local substitute is Mailpit plus a local webhook

#### Scenario: The local substitute for receiving as built (rev 0.107)
- **WHEN** mail is sent to Mailpit for the capture domain in local mode
- **THEN** Mailpit's webhook announces it to the API under a local-only secret, and the worker reads it through Mailpit's API
- **AND** it is received, decided and kept as Resend's would be, while IMAP polling is not built for launch (open question 45)

#### Scenario: Messaging
- **WHEN** messaging adapters are offered (P2)
- **THEN** they are Slack, Zoho Cliq and a generic incoming webhook
- **AND** the local substitute is a local message sink at `/dev/messages`

#### Scenario: Ticketing
- **WHEN** ticketing adapters are offered (P2)
- **THEN** they are Jira Cloud, Linear, GitHub Issues, Zoho Desk or Zoho Projects, ServiceNow (P3) and a generic webhook
- **AND** the local substitute is a local ticket sink at `/dev/tickets`

### Requirement: FR-INT-21 Outbound email
The system SHALL send email through adapters for Zoho ZeptoMail, Resend and generic SMTP (Mailpit locally), with a sender address per purpose, a primary provider and a failover; the platform renders its own templates and providers only deliver.

Source: PRD-001 §8.24 · Priority M · Phase P1

#### Scenario: Sender address per purpose
- **WHEN** email is sent
- **THEN** it uses the sender address set for its purpose: notifications, invoices and quotes, reminders, or security

#### Scenario: Sending domain is checked daily
- **WHEN** the email settings are viewed
- **THEN** the sending domain's SPF, DKIM and DMARC status is shown
- **AND** the status is checked daily

#### Scenario: Provider webhooks update status and suppression
- **WHEN** a provider webhook arrives
- **THEN** its signature is verified and it is deduplicated
- **AND** it updates the message's status and the suppression list

#### Scenario: Providers that never report delivered
- **GIVEN** ZeptoMail reports opens, clicks, bounces and complaints only, and Resend also reports delivered and delayed
- **WHEN** a message's status is tracked
- **THEN** the status model allows for a provider that never reports "delivered"

#### Scenario: Idempotent sends through the outbox
- **WHEN** a message is sent
- **THEN** the send follows the outbox contract (ADR-T7)
- **AND** Resend receives the `Idempotency-Key` where available
- **AND** ZeptoMail, which has none, receives the outbox key in `client_reference`, used for matching only and preventing nothing

#### Scenario: Unknown result is reconciled before a retry
- **WHEN** the result of a send is unknown
- **THEN** it is reconciled before any retry, within Resend's 24-hour idempotency window

#### Scenario: Failover only for sends not accepted
- **WHEN** the primary provider fails
- **THEN** a send fails over to the second provider only when it is known not to have been accepted

#### Scenario: Test send and sandboxes
- **WHEN** an email provider is set up
- **THEN** a test send is available
- **AND** in staging each provider's sandbox is used

#### Scenario: The test send goes only to the organisation's own address
- **WHEN** an Administrator sends a test message (rev 0.39)
- **THEN** it goes only to the organisation's email address from its profile, so it cannot mail anyone else
- **AND** while the profile has no email address the test is refused and nothing is queued

#### Scenario: An SMTP send that may have been handed over is not sent again
- **GIVEN** SMTP cannot be asked what became of a message (rev 0.39)
- **WHEN** a send's connection breaks after the message's data
- **THEN** the message is recorded as unknown and never sent again on a guess

#### Scenario: A refused send is retried, then failed
- **WHEN** the provider refuses a send (rev 0.39)
- **THEN** the outbox retries it
- **AND** on its last attempt the message is recorded as failed with the provider's answer

#### Scenario: SMTP outside local mode speaks only STARTTLS
- **WHEN** an SMTP relay is used outside local mode (rev 0.39)
- **THEN** a server that does not offer STARTTLS is refused and nothing is sent

#### Scenario: Message log without bodies
- **WHEN** a message is sent
- **THEN** it is logged with recipient, template, status and provider
- **AND** no body is stored beyond what the record needs

#### Scenario: Providers outside Singapore need a transfer assessment
- **GIVEN** neither provider documents a Singapore region (ZeptoMail: US, EU, AU, IN, CN, JP; Resend sends from US, EU, São Paulo or Tokyo and keeps account data and logs in the US)
- **WHEN** a provider is to be used
- **THEN** it is first in the data inventory with a PDPA transfer assessment

#### Scenario: G4-073 Crash after the provider accepted a message
- **WHEN** the worker crashes after the email provider accepted a message; separately, a scheduled-posting task runs twice, and a task exhausts its retries
- **THEN** the message row becomes unknown-outcome and is reconciled, not re-sent blind or failed over
- **AND** the posting exists once
- **AND** the exhausted row shows as dead on Checks with replay

#### Scenario: The deployment sets providers, the organisation its senders (rev 0.102)
- **WHEN** outbound email is configured
- **THEN** the deployment sets the providers in order, their keys as secrets never shown through the API, the platform's sender and the sending domains
- **AND** an Administrator chooses each purpose's sender on one of those domains (EML-03), or keeps the platform's, each choice audited and never changed (FY179)

#### Scenario: Failover and reconciliation as built (rev 0.102)
- **WHEN** the primary refuses a send
- **THEN** the failover is tried in the same run, and a send the primary may have taken is never handed to it
- **AND** each attempt records its provider: Resend's is reconciled by sending again under its key within 23 hours, ZeptoMail's stays unknown

#### Scenario: Webhooks and suppression as built (rev 0.103)
- **WHEN** a provider's webhook arrives
- **THEN** it is read only with a valid signature under five minutes old, and each event is recorded once on the message its reference names, in that organisation, for that recipient
- **AND** a hard bounce, complaint or the provider's suppression suppresses the address: nothing is sent to it until an Administrator releases it

#### Scenario: The daily domain check as built (rev 0.104)
- **WHEN** a sending domain is checked, within the hour in the settings and nightly
- **THEN** each record its providers asked for, as the deployment lists them, is found as listed, missing, wrong or failed, beside its DMARC policy
- **AND** a domain is ok only when every record is there and its policy quarantines or rejects; one that is not is recorded as a failed check

### Requirement: FR-INT-22 Inbound email
The system SHALL receive email for the capture addresses (FR-DOC-12) through an inbound adapter and store everything received as untrusted documents (AI-18).

Source: PRD-001 §8.24 · Priority M · Phase P1

#### Scenario: Message received with its authentication results
- **WHEN** a message arrives through a provider's webhook or by IMAP polling
- **THEN** its SPF, DKIM and DMARC results are kept with it

#### Scenario: Attachments are scanned
- **WHEN** a received message has attachments
- **THEN** they are scanned (ClamAV)

#### Scenario: Unknown senders are quarantined
- **WHEN** a message comes from an unknown sender
- **THEN** it goes to a quarantine list for a person to accept

#### Scenario: Bank report from an unexpected sender
- **WHEN** a bank report arrives from a sender other than the bank's registered address
- **THEN** it is quarantined for a person to accept (UC-BNK-01)

#### Scenario: Inbound adapters
- **WHEN** an inbound adapter is chosen
- **THEN** it is Resend receiving (documented without a beta label; the webhook carries metadata and the message is fetched through the API) or IMAP polling of a mailbox
- **AND** ZeptoMail is not offered, because it is send-only

#### Scenario: Readable addresses as built (rev 0.105)
- **WHEN** an Administrator names the organisation's email-in address, or a member their own
- **THEN** it is a readable name on the deployment's capture domain, such as bills@in.example.com, checked against the name rules and the role mailboxes
- **AND** a name stays with its organisation for good (EML-07), and replacing or retiring an address stops its mail

#### Scenario: Known senders and quarantine (rev 0.105)
- **WHEN** mail arrives at an email-in address
- **THEN** it is accepted from the member's own sign-in email at their address, and from members and contacts' billing and remittance emails at the organisation's
- **AND** any other sender is quarantined until an Administrator, or the member for their own address, accepts or rejects it

#### Scenario: Received mail as built (rev 0.106)
- **WHEN** Resend's signed webhook announces mail for an email-in address
- **THEN** the worker fetches it, keeps its text and attachments, checked and scanned, as untrusted files, and accepts it only from an authenticated known or accepted sender
- **AND** anything else is quarantined for the organisation's days (30 by default, or until someone decides), and a decision about the sender settles it

### Requirement: FR-NTF-01 Notification centre
The system SHALL give every user a notification centre: a bell with the unread count, and notifications that each carry a type, a severity, the record it is about and the time.

Source: PRD-001 §8.24 · Priority M · Phase P1

#### Scenario: Bell with unread count
- **GIVEN** a user has unread notifications
- **WHEN** any page is shown
- **THEN** the bell shows the unread count

#### Scenario: Read, unread and archive
- **WHEN** the user works through notifications
- **THEN** each can be marked read or unread, or archived

#### Scenario: Inbox and notifications differ
- **WHEN** something waits for the user to act (an approval, task, proposal or fix)
- **THEN** it is in the Inbox
- **AND** notifications only say what happened

#### Scenario: Actionable notification links to the Inbox
- **WHEN** a notification is actionable
- **THEN** it links to its Inbox item

#### Scenario: Who an event notifies (rev 0.108)
- **WHEN** something happens that a P1 type covers (approvals, security, email-in quarantine, bounces and failed domain checks, postings, the close, recurring entries made, bills due, failed jobs)
- **THEN** it notifies the members it names and those whose roles hold its right on its Appendix D row, such as those who may approve a request
- **AND** never the person who caused it

#### Scenario: The centre as built (rev 0.108)
- **WHEN** a person lists, reads, unreads, archives or reads all of their notifications
- **THEN** only their own are listed or changed, with the unread count, and an archived one stays archived
- **AND** each event is fanned out once by the worker, however often its job runs

#### Scenario: Security and email notices as built (rev 0.109)
- **WHEN** a person signs in, a passkey of theirs is added or removed, mail waits in quarantine, an address is suppressed or a sending domain fails its nightly check
- **THEN** the person, the member at their own address, or those who act on the organisation's email are notified, and a failed domain's Super admins in each organisation they belong to (rev 0.111)
- **AND** a sign-in is not emailed again, since the sign-in alert has emailed it

#### Scenario: Operations notices as built (rev 0.110)
- **WHEN** a person posts, voids or reverses a document, a month closes or reopens, a recurring draft is made, bills fall due or background work dies
- **THEN** those who approve, close, post, pay or look after the organisation are told, in the daily digest unless they choose otherwise
- **AND** never the person who did it, and a notification's own failed fan-out tells nobody

### Requirement: FR-NTF-02 Notification preferences
Each user SHALL set notification preferences per event type: channels, immediate delivery or a daily or weekly digest, and quiet hours.

Source: PRD-001 §8.24 · Priority M · Phase P1

#### Scenario: Channels
- **WHEN** a user sets the channels for an event type
- **THEN** in-app is always on, email can be chosen, and Slack or Cliq can be chosen when connected

#### Scenario: Digest and quiet hours
- **WHEN** a user chooses a daily or weekly digest, or sets quiet hours
- **THEN** notifications of that event type follow that choice instead of arriving immediately

#### Scenario: Organisation defaults
- **WHEN** a new organisation is created
- **THEN** its notification defaults ship with recommended values (FR-ORG-08)

#### Scenario: Security notifications cannot be turned off
- **WHEN** a user tries to turn off a security notification (new-device sign-in, MFA or role change)
- **THEN** it cannot be turned off

#### Scenario: Recommended timing (rev 0.108)
- **WHEN** a person has not chosen otherwise
- **THEN** security notices and approvals are emailed at once and everything else goes in a daily digest at 08:00 Singapore time, with no quiet hours
- **AND** quiet hours a person sets hold immediate email, never a security notice's, until they end

#### Scenario: Minimal email unless the organisation chooses details (rev 0.108)
- **WHEN** a notification is emailed while the setting `notify.email_details` is off, as recommended
- **THEN** the email names only its type and links to Fyniq
- **AND** with the setting on, which needs a second approver, the email carries the notification's title

### Requirement: FR-NTF-03 Announcement banners
The Super admin (for everyone) and the Owner or Administrator (for one organisation) SHALL be able to publish announcement banners, and publishing SHALL be audited.

Source: PRD-001 §8.24 · Priority M · Phase P1

#### Scenario: What a banner holds
- **WHEN** a banner is published
- **THEN** it has a message and a link, a severity (info, warning, critical), an audience (everyone, roles), a start and an end time, and whether it can be dismissed

#### Scenario: Critical banners cannot be dismissed
- **WHEN** a banner is critical
- **THEN** it cannot be dismissed

#### Scenario: Where banners sit
- **WHEN** a banner is active for a user
- **THEN** it sits under the header on every page

#### Scenario: Platform banners
- **WHEN** there is a maintenance window, an incident or a new release
- **THEN** the platform adds its own banner, including the "What's new" release note (FR-UX-35)

#### Scenario: Accessible banners
- **WHEN** a banner is shown
- **THEN** info and warning banners use `role="status"` and only a critical banner uses `role="alert"`
- **AND** the banner never covers the focused element (SC 2.4.11)

#### Scenario: Dismissals are stored per user
- **WHEN** a user dismisses a banner
- **THEN** the dismissal is stored for that user on the server

#### Scenario: Publishing is audited
- **WHEN** a banner is published
- **THEN** the publication is audited

### Requirement: FR-NTF-04 Slack and Zoho Cliq
The system SHALL offer an optional organisation-level Slack or Zoho Cliq connection per workspace that posts messages carrying deep links into the web app, and approvals SHALL never happen in chat.

Source: PRD-001 §8.24 · Priority S · Phase P2

#### Scenario: Slack posts with a bot token
- **WHEN** Slack is connected
- **THEN** a private Slack app posts with a bot token (`chat.postMessage`, scope `chat:write`)
- **AND** incoming webhooks are not used, because they are fixed to one channel

#### Scenario: Zoho Cliq posts through its REST API
- **WHEN** Zoho Cliq is connected
- **THEN** a Cliq bot posts to channels and users through the Cliq REST API (OAuth scope `ZohoCliq.Webhooks.CREATE`)
- **AND** messages are queued and batched to stay within 50 requests a minute per user

#### Scenario: Routing and direct messages
- **WHEN** an event occurs
- **THEN** routing rules send its event type to a channel, for example approvals to #finance and company-secretary runs to #compliance
- **AND** users who linked their chat account also get direct messages

#### Scenario: Approvals open the web app
- **WHEN** a chat message concerns an approval
- **THEN** its button opens the web app, where step-up applies (FR-APR-03/04)

#### Scenario: Content level minimal by default
- **WHEN** a chat connection is created
- **THEN** its content level is minimal ("2 approvals waiting", no amounts or names)
- **AND** standard (amounts and counterparties) can be chosen only after the chat provider is in the data inventory with a PDPA transfer assessment

#### Scenario: Chat providers and Singapore residency
- **WHEN** the transfer assessment of a chat provider is made
- **THEN** it takes into account that Slack offers Singapore data residency on its Business+ and Enterprise plans and Cliq documents no Singapore data centre

#### Scenario: Fallback to email
- **WHEN** a chat delivery fails
- **THEN** the message falls back to email

### Requirement: FR-NTF-05 One event stream
Notifications, digests, banners for incidents, chat messages and webhooks SHALL all be produced from the same events (FR-INT-12), so channels are interchangeable.

Source: PRD-001 §8.24 · Priority M · Phase P1

#### Scenario: One event feeds every channel
- **WHEN** an event is emitted
- **THEN** notifications, digests, incident banners, chat messages and webhooks about it are produced from that same event

#### Scenario: Delivery attempts are logged and retried
- **WHEN** a delivery is attempted on any channel
- **THEN** the attempt is logged
- **AND** a failed attempt is retried

#### Scenario: Test message per channel
- **WHEN** a channel is set up
- **THEN** a test message can be sent on it

#### Scenario: Notifications and digests from one event as built (rev 0.108)
- **WHEN** a notification event is recorded with what happened
- **THEN** each notification and digest about it is produced from that event
- **AND** its email goes through the outbox, so each attempt is recorded and retried

### Requirement: FR-NTF-06 Ask from chat
Linked users SHALL be able to ask the assistant read-only questions in Slack or Cliq through the same tools, permissions and exclusions as the assistant, as an `agent`-class client (§9.7).

Source: PRD-001 §8.24 · Priority C · Phase P3

#### Scenario: Read-only answers in chat
- **GIVEN** a user who linked their chat account
- **WHEN** the user asks the assistant a question in Slack or Cliq
- **THEN** it is answered read-only through the same tools, permissions and exclusions

#### Scenario: Answers follow the content level
- **WHEN** an answer is posted in chat
- **THEN** it follows the connection's content level
