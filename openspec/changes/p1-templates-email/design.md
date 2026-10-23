# Design

## Context

- `cmd/render` serves POST /render (rev 0.96); its image is built on `distroless/static` (was `distroless/cc`) within its budget.
- The outbox (p0-outbox-background-work) delivers jobs through the push dispatcher locally and Cloud
  Tasks in the cloud (§12.11).
- Mailpit runs in `make deps-up`.
- Evidence upload comes from `p1-documents-evidence`.

## Goals / Non-Goals

**Goals:**
- A document renders to the same bytes every time from the same template version and data.
- An email is sent once, or the system knows it was not.
- Every channel is fed from one event.

**Non-Goals:**
- Drag-and-drop layout, conditional blocks, the column chooser and the visual diff (P2,
  question 44).
- Slack, Zoho Cliq and other messaging channels (P2).
- Payslip templates (Nemura, P2).

## Decisions

- **The template is data, the renderer is fixed.**
  - A template version is JSON: the blocks, theme and texts, with fields from the typed catalogue.
  - The render service maps it to Typst sources it owns, so users never write Typst. The "no free
    code" rule holds by construction.
  - Locked IRAS blocks are inserted by the renderer for tax-invoice types and cannot be removed by the
    JSON, so publishing checks them (FR-TPL-03).
  - Alternative considered: user-editable Typst. Rejected because it is free code.
- **Determinism.**
  - Fonts are embedded from the self-hosted set, time and locale come from the document, and Typst's
    version is pinned.
  - Snapshot tests compare PDF bytes for each starter template, and FR-DOC-04's hash proves reprints
    identical.
- **Sending through the outbox, with reconciliation.**
  - Each send is an outbox job with an idempotency key passed to the provider where supported.
  - A crash after the provider accepted (G4-073) leaves the job "unknown"; the job asks the provider
    for status before any retry.
  - Failover to the second provider happens only when the first did not accept. Webhooks update
    status and the suppression list, and a provider that never reports "delivered" is handled by a
    timeout state.
- **Inbound is untrusted input.**
  - The inbound adapter stores the raw message and its authentication results (SPF, DKIM, DMARC).
  - Attachments go through the two-phase upload and the scanner. Unknown senders and failed
    authentication are quarantined, and nothing in a message is an instruction to the AI (AI-18).
- **One event, many channels.** The outbox event (versioned in `contracts/events/`, `p1-public-api`)
  feeds notifications, digests, banners and later webhooks and chat. Each channel's delivery attempts
  are logged and retried (FR-NTF-05).
- **Ports and adapters (ADR-T8):**
  - `Email` (new: ZeptoMail, Resend, SMTP; Mailpit locally through SMTP);
  - `InboundMail` (new: Resend inbound, a Mailpit-fed local adapter);
  - `BlobStore`, `Scanner`, storage and queue.
- **Audit events (AUD-01):**
  - template draft saved, version published, rollback;
  - banner published or withdrawn;
  - sender and provider configuration changed;
  - message sent, failed, or suppressed (metadata only, no bodies);
  - inbound message quarantined or released.

## Risks / Trade-offs

- [Provider-specific idempotency differs (ZeptoMail and Resend)] → The reconciliation step does not
  rely on provider idempotency. Each adapter declares what it supports, and the contract tests run
  against fakes now and the real providers at the cloud gate.
- [Deliverability without a configured domain] → Mailpit locally; the daily domain check warns the
  Owner before any real send.
