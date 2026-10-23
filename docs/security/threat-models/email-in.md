# Threat model: email-in addresses and received mail

| | |
|---|---|
| Feature and requirement IDs | Email-in addresses, received mail, the quarantine and sender decisions: FR-INT-22, FR-DOC-12 (PRD-001 rev 0.105, rev 0.106) |
| Author, reviewer, date | Claude Code for the owner, 23 Oct 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Senders' addresses, subjects, message text and attachments from anyone on the internet (untrusted, AI-18; personal data, PDPA); members' verified sign-in emails (personal data) |
| Who acts | Anyone who can send email; Resend (webhooks, the receiving API); the worker (fetch, expiry); the Administrator (the organisation's address and its senders); each member (their own) |

## 1. What we are building

An Administrator names the organisation's email-in address and each member their own, readable names on the
deployment's capture domain. Resend receives mail for that domain and posts a signed `email.received` webhook to the
API, which finds the address's organisation through `capture_route` (the global name registry, before any tenant is
set), records the message and queues its fetch. The worker reads the message, its attachments and the receiving
server's SPF, DKIM and DMARC results from Resend's API, decides its standing (accepted from members and contacts,
quarantined from anyone else, rejected from rejected senders), checks and scans each file like an upload and keeps it
in the staging store under `inbound/`. A quarantined message ends after the organisation's days unless someone decides.

Trust boundaries: the internet ↔ Resend (anyone can send); Resend ↔ API (webhook); worker ↔ Resend (fetch, download
URLs); API and worker ↔ database (RLS; the global registry); later, the capture pipeline and the AI ↔ the kept files.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Internet → address | Spoofing: a stranger forges a member's or a contact's From to have mail accepted | A forged bill enters capture as a known sender's | High |
| T2 | Resend → webhook | Spoofing: a forged email.received names an address | A fetch of another message, or noise | Medium |
| T3 | Registry | Information disclosure / tampering: mail for one organisation reaches another, or a name is taken over | Another tenant's bills | Low |
| T4 | Worker → files | Tampering: malware, oversized or disguised files; a download URL elsewhere | Malware stored; the worker fetches an internal URL | Medium |
| T5 | Kept files → AI | Elevation: instructions in a message or attachment steer the AI | Wrong drafts | Medium (later pipeline) |
| T6 | Quarantine | Denial of service / information disclosure: spam piles up; strangers' personal data kept for ever | Storage, PDPA | Medium |
| T7 | Decisions | Repudiation / tampering: who accepted a sender, or an accepted sender's mail edited | Unaccountable acceptance | Low |
| T8 | Identities | Information disclosure: members' sign-in emails exposed to other organisations | Personal data | Low |

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | A sender is known only when authenticated: DMARC passes, or SPF or DKIM where the domain has no DMARC; a member's address accepts only that member's verified sign-in email; everything else is quarantined | `TestTheStandingOfASender`, `TestASenderIsAuthenticatedByTheirDomain`, the DMARC-failing contact in `TestTheWorkerFetchesDecidesAndKeepsReceivedMail` (`// req: FR-INT-22`) |
| T2 | The webhook's Svix signature is checked before the body is read; the worker reads the message from Resend's API with Fyniq's key, never from the webhook | `TestAResendWebhookIsReadOnlyWhenSignedLately`; `TestReceivedMailIsQuarantinedUntilSomeoneDecides` |
| T3 | Names live in the global registry, written only by `capture_address`'s trigger and read only through `capture_route`; a name stays with its organisation for good (FY183); the message is recorded under that organisation's RLS | `TestAnEmailInAddressNameBelongsToOneOrganisation`, `TestEmailInAddressesAndSenderDecisionsKeepTheirHistory` |
| T4 | Files over 25 MB are refused before download; each is checked by content type, scanned (EICAR locally, ClamAV in the stack) and stripped like an upload; a download URL must be https; files live under `inbound/`, apart from evidence | The virus left out in `TestTheWorkerFetchesDecidesAndKeepsReceivedMail`; `TestReceivedMailIsReadFromResendsReceivingAPI` |
| T5 | Kept files are untrusted data (AI-18) and never evidence until a person confirms what they show; the capture pipeline (`p1-document-capture`) treats them under AI-18's rules | The pipeline's own threat model, when it is built |
| T6 | Quarantined mail ends after the organisation's days (30 by default; 0 keeps it until someone decides, the owner's choice), its files deleted; a rejected sender's mail is deleted at once | `TestQuarantinedMailExpiresAndKeepsItsHistory`, the rejection in the API test |
| T7 | Sender decisions are append-only (FY184), a message's status only moves forward (FY185), a file is only removed (FY186); each change is audited with its person or `integration:resend` | The history tests above |
| T8 | Emails are kept on the global identity, read only through `member_emails`, for the current organisation's active members | `TestMembersAndContactsAreKnownSenders` |

## 4. What remains

- Resend's retention of received mail is not documented, so Fyniq keeps each message on arrival; nothing relies on
  fetching it again later.
- No rate limit per sender in the application; Resend's and the edge's apply. A flood fills the quarantine until it
  expires.
- The capture pipeline that turns accepted mail into draft bills, and its prompt-injection controls, come with
  `p1-document-capture`.
