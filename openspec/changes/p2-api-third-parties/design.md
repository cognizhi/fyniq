# Design

## D1. Webhooks: where the event comes from

The one event stream is `notify.Emit` (FR-NTF-05): it records a notification event and queues the fan-out in the same unit of
work (the transactional outbox). Options for webhooks:

| Option | For | Against |
|---|---|---|
| A. The fan-out job also writes a `webhook_delivery` row for each matching endpoint, and a second job delivers each | No new emission point; no lost or phantom event; the catalogue stays the notification types | A change that emits no notification has no webhook (ledger and document events join the stream with their own emitters) |
| B. A trigger on the audit trail | Every change | The audit trail holds names and values (SEC-15); the event data must be IDs only |
| C. Each module calls a webhook emitter | Flexible | Many call sites; easy to emit for a rolled-back change |

Decision: **A** (`[owner to confirm]`). `webhook_delivery` rows are written by the fan-out job (the worker), so an event that
did not commit has none. An event type that is not yet a notification type joins by getting its own `notify` type and
`contracts/events/` schema; the catalogue test stays the gate.

## D2. Signing and delivery (Standard Webhooks)

Headers `webhook-id` (the delivery's stable ID: the same on a retry), `webhook-timestamp` (seconds), `webhook-signature`
(`v1,<base64 HMAC-SHA256 of "id.timestamp.body">`; two signatures while a secret rolls). Secret `whsec_<base64 of 32 random bytes>`,
shown once at creation and on a roll, sealed at rest with the field cipher (it must be recoverable to sign), never in a log or an
audit event. Body: a CloudEvents 1.0 structured JSON envelope (`specversion`, `id`, `source`, `type`, `time`, `subject`,
`datacontenttype`, `data`); data is the event schema's IDs, never a title, a name or an amount. The receiver's replay window is
five minutes by the timestamp. Delivery goes through `internal/egress` with the endpoint's URL as the configured destination: https
only, no private, loopback, link-local or metadata address, no redirect, a 10-second timeout, a 64 KiB answer cap; the answer is
never stored beyond its status and the first 200 bytes.

## D3. Retries and the dead-letter view

2xx is delivery; 410 disables the endpoint; anything else (and a timeout) is retried: 5 s, 5 min, 30 min, 2 h, 5 h, 10 h, then
every 12 h, up to three days from the first attempt (a schedule of 12 attempts). Then the delivery is `dead`, shown in the endpoint's
dead-letter list, and a person with the right replays one (`POST .../replay`: a new attempt series with the same `webhook-id`).
An endpoint that fails ten deliveries running is paused and the Administrator is told (notification `integration.webhook_failing`).

## D4. Endpoint verification and who may register

Creating an endpoint makes it `pending`; `POST .../verify` sends a `fyniq.webhook.verify.v1` event with a challenge, and the
endpoint is `active` only when its 2xx answer's body echoes the challenge. An Administrator (stepped up) creates, changes,
pauses and rolls; reading deliveries is the same right. **No dual control** (`[owner to confirm]`): an endpoint can receive only
IDs and event types, and creating one needs step-up; the alternative, Owner approval with a second approver, is a setting an
organisation can adopt later. A webhook can be switched off at once.

## D5. Inbound tasks (FR-INT-24)

A task made by an integration is a compliance task whose source is `integration` (a new value; the check is widened `NOT VALID`
then validated), with the external reference (`external_ref`, unique per client) and a link back. `fyniq.write.tasks` allows
`POST /v1/tasks` and `PATCH` of its own tasks only; it never completes a *statutory* task and never touches a financial record
(the scope map has no row for either). It is not an AI or MCP tool.

## D6. Third-party MCP clients (AI-46)

A registration is a row (`mcp_client_registration`): name, publisher, redirect URIs (https, or a loopback address, never a wildcard
and never `localhost` without a port), a CIMD URL fetched through the egress guard when given, and the class `agent`. Registered
by an Administrator, approved by the Owner with a second approver (the approval object `mcp_client`), then registered with Hydra
as a public client with PKCE. It appears in `mcp.clients` for the Owner to allow per organisation, in the quarterly access review
and the data inventory. Deleting it removes it from Hydra at once. Nothing here widens an MCP scope or tool.

## D7. Sandbox (FR-INT-18)

An organisation made for sandbox use is a second organisation of the installation, flagged `sandbox`, with credentials of its own. (Amended in building, the owner's
decision of 21 Nov 2026, recorded `[owner to confirm]` in the details.) **Option A, a flagged organisation of the same installation, enabled per deployment,** is chosen:
it reuses tenancy, row-level security and the seed loader, and the SDKs can run against it. It is not created from the Owner's organisation and holds only the synthetic sample company.

- **Creation:** a Super admin only (an installation operation; step-up), with two Super admins as the other's approved installation change of step `sandbox` (the pattern of a private endpoint,
  FR-ADM-07). The new organisation's creator is its Owner (as a practice organisation's). There is no way to flag an existing organisation or to unflag one: the flag is a row of a registry
  no application role can write, inserted only by the creation function in the statement that makes the organisation (migration 001250, FY960), so "no way to turn a real organisation into a sandbox
  or back" is the database's rule. The general organisation creation of FR-ADM-07 (P3) is not built: only the sandbox's.
- **Per deployment:** `FYNIQ_SANDBOX=on` (off by default), the operator's, never a setting. `FYNIQ_SANDBOX_WEBHOOK_HOSTS` lists the only hosts a sandbox's webhooks may reach (none by default).
- **The sample company** is loaded by the API right after the organisation commits (a job would need the worker to hold write rights on the ledger: a widened grant, so not chosen), through the same
  posting engine as the seed; the vocabulary is embedded in the binary (`internal/seed/vocabulary.json`, a test keeps it equal to the screens' sample data). A failed load leaves an organisation with
  no chart that `loadSandboxSampleCompany` fills.
- **Every outbound path respects the flag**, read in each piece of work's own unit of work: `connector.Runtime` refuses every adapter (bank data, payment provider, payment links, inbound email, AI
  model) and gives outbound email a sink (the deployment's providers are never used); the outbox handler runs only an allow-list of kinds for a sandbox, default-deny, so a new outbound kind is blocked
  until listed; webhooks reach only the operator's hosts (at creation, at verification and at delivery, dead at once otherwise); InvoiceNow sends, refreshes and polls nothing; the API refuses a
  connector, a practice organisation and, as for any organisation, anything its role does not allow. The IRAS filer has no caller yet: the rule is that the one that gets it asks `Sandboxes().Is` first.
- **Not built:** a scripted model for a sandbox's AI chat (the AI is refused); the Super admin console screen for making one (API only, `[left]`); a list of sandboxes.

## D8. SDKs

Go and Python clients are generated from `contracts/openapi/fyniq.v1.yaml` and checked in under `sdk/go` and `sdk/python`, with a
thin hand-written layer for the conventions (idempotency key reused on retry, `Retry-After`, problems, cursor pagination), the
way `sdk/typescript` is built. `make api-check` fails while a generated client is stale.

## D9. Ticketing and chat

Each provider is a connector (capability `ticketing` or `messaging`) with a manifest, a port, an adapter and a recorded partner
contract (`contracts/partners/`). A ticket is created from an item a person chooses or by a routing rule; the content level is
**minimal** (no amounts, no personal data) unless the organisation raised it, deduplicated per item. A provider's webhook (signature
verified) syncs status and comments back; closing a ticket may close a task and nothing else. Chat messages carry a deep link and
never approve anything (FR-NTF-04). All of it is `[live-verify]` against each provider's sandbox.

## Order

Webhooks first (the security-critical piece), then inbound tasks, SDKs, third-party MCP clients, sandbox, then the connectors and
routing rules.
