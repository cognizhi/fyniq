# Threat model: outbound email providers, senders and webhooks

| | |
|---|---|
| Feature and requirement IDs | Outbound email through ZeptoMail with a Resend failover, senders per purpose, provider webhooks and the suppression list: FR-INT-21 (PRD-001 rev 0.102, rev 0.103); notification and digest email: FR-NTF-01, FR-NTF-02 (rev 0.108); quotes and invoices emailed with their PDFs: FR-AR-06, FR-AR-16 (rev 0.133); payment reminders and reminders of quotes about to expire, sent from the Inbox, and statements of account: FR-AR-08 (rev 0.137, rev 0.138) |
| Author, reviewer, date | Claude Code for the owner, 23 Oct 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Recipients' email addresses, subjects and bodies (personal data, PDPA); the PDFs of invoices and quotes sent (amounts, customers' names and addresses, the company's bank details: confidential business data); a provider's bounce and complaint reasons; the providers' send keys and webhook secrets (secrets) |
| Who acts | The Administrator (senders, test sends, releases); the worker (sends); ZeptoMail and Resend (webhooks); everyone with View on Organisation integrations (reads) |

## 1. What we are building

The worker sends each queued message through the deployment's providers in order (ZeptoMail, then Resend
only after a refusal), from the organisation's sender of the message's purpose on one of the deployment's
sending domains. The providers' keys are Secret Manager secrets in the worker's environment. The providers
call `POST /api/webhooks/email/{resend,zeptomail}` on the API with signed events; the API records each
event on its message and suppresses an address after a hard bounce, a complaint or the provider's own
suppression; the worker sends nothing to a suppressed address until an Administrator releases it.

Trust boundaries: browser ↔ API (senders, suppressions); worker ↔ provider (send, over HTTPS); provider
↔ API (webhooks, from the internet); API and worker ↔ database (RLS).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Provider → webhook | Spoofing: anyone posts a forged bounce or complaint | Addresses suppressed, mail to customers stops | Medium: the URL is public |
| T2 | Provider → webhook | Tampering / replay: a captured webhook is posted again later | Duplicate events, a released address suppressed again | Low |
| T3 | Webhook → database | Elevation: a signed event names another organisation's message | Events or suppressions in the wrong tenant | Low |
| T4 | Browser → API | Spoofing: an organisation sends from a domain it does not own (another tenant's, or a bank's) | Phishing under the deployment's providers | Medium |
| T5 | Worker → provider | Information disclosure: a provider key or a body in logs or the API | Mail sent as the platform; personal data leaked | Low |
| T6 | Worker → provider | Repudiation / duplicates: a lost answer leads to a second send, or to the failover sending it too | A customer receives an invoice twice | Medium |
| T7 | Provider → webhook | Denial of service: large or many requests | API load | Low |
| T8 | Database | Tampering: an event or suppression rewritten to hide a bounce | Lost evidence of delivery | Low |
| T9 | Worker → provider | Information disclosure: notification email carries amounts, counterparties and people's reasons to a provider outside Singapore and into mailboxes, which may be personal | Financial and personal data transferred without assessment | High without a control: every approval says what it is |
| T10 | Worker → database | Elevation: the worker, which now queues notification email, queues a test message or a sign-in alert, or emails a notification to someone it does not concern | Mail to anyone from the platform's sender | Low |
| T11 | Browser → API → provider | Information disclosure: an invoice or quote, with its amounts and the customer's details, goes to the wrong address, or reaches a provider outside Singapore | A customer's invoice read by someone else; a transfer without assessment | Medium: an address is typed by a person |
| T12 | Worker ↔ file store | Tampering: the PDF attached is swapped in the store between the send and the worker reading it, or names another organisation's file | The customer receives a document Fyniq never issued | Low |
| T13 | API → provider | Repudiation: the customer says the invoice never arrived, or said something else | Disputes about what was sent and when | Medium |
| T14 | API → provider | Tampering / injection: a typed recipient or a file name carries a line break into the message's headers | Extra recipients, a forged header | Low |
| T15 | Browser → API → provider | Repudiation / harm: a customer who paid is chased, because a receipt recorded by hand is late, or the same reminder goes twice | A customer annoyed or embarrassed; trust lost | Medium while receipts are typed |
| T16 | Browser → API | Elevation: someone who may only read invoices sends or skips reminders, or a skip hides a reminder nobody decided on | Customers chased, or not chased, against the company's wishes | Low |

SOD: senders and releases are the Administrator's (Create on Organisation integrations); no money moves. RLS:
every table has `organisation_id` and `FORCE ROW LEVEL SECURITY`. The AI and MCP have no route to any of it.

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | Each provider's signature is checked before the body is read: Svix's HMAC-SHA256 over id, time and raw body (Resend); HMAC-SHA256 of the form value under the Agent's key (ZeptoMail); constant-time comparison; no receiver without its secret (ASVS V11.2, V13.3) | `TestAResendWebhookIsReadOnlyWhenSignedLately`, `TestAZeptoMailWebhookIsReadOnlyWhenSignedLately`, the forged request in `TestTheProvidersWebhooksRecordEventsAndSuppressHardBounces` (`// req: FR-INT-21`) |
| T2 | A signature older than five minutes is refused; an event is recorded once per provider delivery ID (`email_event_once`); a suppression in force is not duplicated | The six-minute cases above; the retried delivery in the webhook test; `TestEmailEventsAndSuppressionsKeepTheirHistory` |
| T3 | The organisation and message come from the reference Fyniq sent, inside the signed payload; the event is recorded in that organisation's transaction (RLS) only when the message exists there and the recipient is the message's | The event about another recipient in the webhook test |
| T4 | A sender must be on a domain the deployment's operators listed (EML-03); the providers also refuse unverified domains | `TestAnOrganisationChoosesItsSendersOnTheDeploymentsDomains` |
| T5 | Keys live in Secret Manager and the worker's environment, never in the API, its answers or the database; the message record and events never hold a body; errors keep the provider's code and a short reason only (SEC-15) | The settings test checks no key is shown; `TestAnEmailsOutcomeIsFinalAndOnlyTheWorkerRecordsIt` |
| T6 | Outbox sends with reconciliation before any retry: Resend's under its Idempotency-Key within 23 hours, ZeptoMail's never resent on a guess; the failover only after a refusal | `TestTheFailoverSendsOnlyWhatThePrimaryRefused`, `TestStatusSendsAgainUnderTheKeyWithinTheWindow`, `TestAnEmailThatMayHaveBeenTakenIsNeverSentAgainOnAGuess` |
| T7 | Bodies over 256 KB are refused before parsing; the edge's rate limits apply (Cloud Armor in the cloud) | The size check in `mailhooks.handle` |
| T8 | Events never change or go (FY180); a suppression is only released, once (FY181); each is audited as `integration:<provider>` or its person | `TestEmailEventsAndSuppressionsKeepTheirHistory`, the audit checks in the webhook test |
| T9 | Notification email names only its type and links to Fyniq unless the organisation turns on `notify.email_details`, which needs a second approver (FM-1; the owner's decision of 24 Oct 2026); the link is the deployment's own origin | `TestNotificationsAreFannedOutEmailedAndDigested` (the subject before and after the setting) |
| T10 | A trigger lets the worker's role insert only notification and digest email; recipients are members the event concerns with a sign-in email the provider verified, never the person who caused it; a person reads and changes only their own notifications | `TestNotificationsKeepTheirHistory`, `TestTheNotificationCentreAndPreferences`, `TestAnEventConcernsItsAudience` |
| T11 | Only an issued invoice or a sent quote is emailed, by someone with P on "Invoices, credit notes", as issuing needs; it goes to the customer's billing email unless one plain address is given; each send records the recipient and is audited, and the delivery log shows it; a tax invoice missing a particular is not sent. The providers' PDPA transfer assessment (FR-INT-21) covers invoice email before go-live (below) | `TestInvoicesAndQuotesAreEmailed`, the matrix rows `emailInvoice` and `emailQuote` (`// req: FR-AR-06`) |
| T12 | The job names the evidence file, never its bytes; the worker reads it in the job's organisation under RLS, from the retention-locked store once committed, and checks its SHA-256 against the evidence row before any attempt is marked: a changed file is never sent | `TestAMessageCarriesItsEvidenceFiles` (`// req: FR-DOC-07`) |
| T13 | The PDF sent is kept as the document's evidence (append-only, retention-locked); the send records the version, the message, the PDF's SHA-256 and the template version, and never changes (FY195, FY196); delivery comes from the provider's signed events | `TestASendIsOfAnIssuedInvoicesCurrentVersion`, `TestInvoicesAndQuotesAreEmailed` |
| T14 | A typed recipient must parse as one bare address; attachment names match a header-safe pattern and only PDFs go, at most 5 and 10 MB; each adapter refuses anything else before sending | `TestAnInvoicesPDFTravelsAsAnAttachment`, `TestAnAttachmentIsTheDocumentedField`, the address case in `TestInvoicesAndQuotesAreEmailed` (`// asvs: V1.2.4`) |
| T15 | No reminder goes by itself while receipts are recorded by hand (the owner's decision of 24 Oct 2026): each due one waits in the Inbox for a person to send or skip; the list counts only what is still open on the invoice, so a receipt recorded takes it off; a reminder step is recorded once per document (`sales_reminder`, unique, append-only, FY203), so a second send of the same step is refused | `TestRemindersWaitForAPerson`, `TestARemindersStepIsDecidedOnce` |
| T16 | Sending or skipping a reminder, or emailing a statement, needs P on "Invoices, credit notes", as emailing an invoice does; a statement goes as it was made (FY204), with no online link; each decision names who made it, when, and a skip's reason, and is audited | The matrix rows `sendSalesReminder` and `skipSalesReminder` |

## 4. What remains

- ZeptoMail's signing is implemented from Zoho's sample: the key's bytes (its text, UTF-8) and the event
  names are `[verify]` against the staging sandbox before go-live (PRD-001 rev 0.103).
- No per-source rate limit on the webhook routes in the application; the edge's applies. Revisit if the
  providers' IP ranges are published and stable enough to allow-list.
- With `notify.email_details` on, notification titles reach the provider; turn it on only once the provider's
  PDPA transfer assessment covers financial data (FR-INT-21).
- Invoice and quote email carries business and personal data to the providers: their PDPA transfer
  assessments must cover it before the first invoice is emailed in production (FR-INT-21).
- Automatic reminders (no person in the loop) wait for receipts from bank import (`p1-bank-import-reconcile`),
  which revisits T15 before an Owner can switch a customer group to them.
- Resend takes an attachment's type from its file name's extension; the field naming the type is not
  documented clearly, so none is sent `[verify]` in the staging sandbox that PDFs arrive as `application/pdf`.
- Opens and clicks are not recorded, so no tracking data is kept; turning tracking on in a provider account
  is an operator decision outside the application.
