# Proposal

## Why

P1 gave Fyniq an API-first core (conventions, idempotency, ETags, previews, rate limits), integration clients and personal
access tokens behind dual control, a first-party MCP server and the connector framework. What a third party still cannot do:

- **Hear from Fyniq.** There is one event stream (the notification types, `contracts/events/`) but no delivery: an integrator
  polls (FR-INT-12). The Standard Webhooks signing, retries, dead-letter view and endpoint verification are P2.
- **Build on it quickly.** Only a TypeScript SDK exists; the Go and Python clients, and a **sandbox** organisation with its own
  credentials and none of the real data, are P2 (FR-INT-17, FR-INT-18).
- **Ask in the semantic layer.** `POST /v1/metrics:query` already exists (FR-INT-15, built in P1 for the assistant); its public
  contract and its reach for integration clients are verified here.
- **Connect an AI client that is not first-party.** MCP allows only Claude Code and Claude Desktop. An Administrator needs a
  reviewed way to register another (AI-46).
- **Work with the tools a team already uses.** Tickets in Jira, Linear, Zoho Desk or GitHub Issues from a task, check or
  finding, and status coming back (FR-INT-23, FR-INT-24); Slack and Zoho Cliq messages routed by rules (FR-NTF-04).

## What Changes

- **Webhooks and events (FR-INT-12; also the machinery FR-AUT-04 uses).** Tables `webhook_endpoint` and `webhook_delivery`; the
  existing notification fan-out also fans out to matching endpoints in the same transaction (the outbox, so no event is lost or
  phantom); a worker job signs each delivery as Standard Webhooks (`webhook-id`, `webhook-timestamp`,
  `webhook-signature: v1,<HMAC-SHA256>`), sends it through the egress guard (no private address, no redirect), retries with
  exponential back-off for three days, then parks it in a dead-letter list a person replays. An endpoint is verified (a challenge
  it must echo) before it is active. CloudEvents 1.0 envelope; data is the schema of `contracts/events/` (IDs, never titles).
- **Inbound tasks (FR-INT-24).** A narrow scope `fyniq.write.tasks` that creates and updates *tasks* (never a financial record,
  never an approval or a statutory obligation marked met).
- **Third-party MCP clients (AI-46).** An Administrator registers a client (manual registration or a Client ID Metadata Document
  fetched through the egress guard), with verified redirect URIs; the Owner approves with a second approver; it joins the access
  review and the data inventory. Never automatic.
- **Sandbox (FR-INT-18).** A sandbox organisation made from the sample company, with its own credentials, no path to real data.
- **Go and Python SDKs (FR-INT-17).** Generated from `contracts/openapi/`, with the TypeScript client's behaviours (idempotency
  key kept across retries, `Retry-After`, typed problems, pagination).
- **Ticketing and chat connectors (FR-INT-23, FR-NTF-04).** Jira Cloud, Linear, Zoho Desk, GitHub Issues; Slack and Zoho Cliq; each
  a connector with a manifest, built against a recorded partner contract; routing rules (event type to destination) and their
  screens. Minimal content by default.
- **FR-INT-04** (the list of integrations) is satisfied by the catalogue of connectors above plus those built in P1/P2.

## Impact

Migrations 001151 to 001199. API additions under `/v1`. New scopes: `fyniq.write.tasks`. New principals: none for the AI or the
MCP surface; no write tool is added to either. New worker grants (the delivery rows) and one narrow function; each is listed in
`docs/security/p2-grants-for-human-review.md` as `needs-human-review`. Everything that needs an outside account (Jira, Linear,
Slack, a public URL) is built against a stub and tagged `[live-verify]`.
