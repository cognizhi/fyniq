# Threat model: outbound email providers, senders and webhooks

| | |
|---|---|
| Feature and requirement IDs | Outbound email through ZeptoMail with a Resend failover, senders per purpose, provider webhooks and the suppression list: FR-INT-21 (PRD-001 rev 0.102, rev 0.103) |
| Author, reviewer, date | Claude Code for the owner, 23 Oct 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Recipients' email addresses, subjects and bodies (personal data, PDPA); a provider's bounce and complaint reasons; the providers' send keys and webhook secrets (secrets) |
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

## 4. What remains

- ZeptoMail's signing is implemented from Zoho's sample: the key's bytes (its text, UTF-8) and the event
  names are `[verify]` against the staging sandbox before go-live (PRD-001 rev 0.103).
- No per-source rate limit on the webhook routes in the application; the edge's applies. Revisit if the
  providers' IP ranges are published and stable enough to allow-list.
- Opens and clicks are not recorded, so no tracking data is kept; turning tracking on in a provider account
  is an operator decision outside the application.
