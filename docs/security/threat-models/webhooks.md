# Threat model: outbound webhooks and events

| | |
|---|---|
| Feature and requirement IDs | FR-INT-12 (webhooks and events), FR-AUT-04 (the machinery it gives them); p2-api-third-parties task 1 |
| Author, reviewer, date | Claude Code for the owner, 21 Nov 2026; second review waived before production (AGENTS.md), held with `needs-human-review` |
| Data and its class | An endpoint's address and label (internal); its signing secret (secret); an event's IDs and times (internal; never a title, name, amount or personal data); the delivery log |
| Who acts | An Administrator, signed in and stepped up (endpoints); the Owner with a second approver (the switch); the fan-out and `webhooks.deliver` as the worker; the receiver, outside |

## 1. What we are building

An organisation's endpoints receive CloudEvents about operational and approval events, signed as Standard Webhooks, from the transactional outbox, retried for three days, with a dead-letter list.
Trust boundaries: browser to API (endpoints, secrets shown once); API and worker to database (RLS, grants); worker to the receiver over the egress guard.

## 2. What can go wrong

| # | Threat | Control | Test |
|---|---|---|---|
| T1 | SSRF: an endpoint points at the metadata service, loopback, a private address or an internal name, directly, by DNS rebinding or by redirect | The third-party guard resolves and dials the checked address, refuses every non-public address and the platform's names, follows no redirect, 10 seconds, 64 KiB; the API refuses a non-https address and one with credentials; a refusal is a logged failure, never a send | `TestADeliveryNeverReachesAPrivateAddressOrFollowsARedirect` (non-local guard) |
| T2 | A credential makes an endpoint to exfiltrate events: a token, an integration client, the AI or an MCP client | The operations need a signed-in person with step-up, no client scope reaches the row, none is on the AI or MCP surface; off until an Owner turns the switch on with a second approver | `TestAWebhookEndpointIsVerifiedBeforeItReceivesASignedEvent`, the matrix rows, the MCP surface test |
| T3 | Data leaves that should not: a title, a name, an amount, a security event, a payee or personal-data event | The data is the event schema's IDs and times only, built from the event's fields and never its title; the type allow-list has 15 operational and approval types, checked against the event catalogue's category | `TestOnlyOperationalEventsMayBeAskedFor`, the same first test (no title in the body) |
| T4 | A secret leaks: in an answer, a log, the audit trail or at rest | Sealed under the field key bound to the endpoint; shown once; audit events carry the last four characters only; reads never return it | the first test; `TestSecretsAreRandomAndShowOnlyTheirLastFour` |
| T5 | Forgery or replay by a third party at the receiver | HMAC-SHA256 over id, timestamp and body, a fresh timestamp each attempt, the same id each try so the receiver deduplicates, two signatures while a secret rolls | `TestTheSignatureIsStandardWebhooksV1` |
| T6 | An endpoint nobody owns receives events: a typo, a stranger's address | Pending until it echoes a challenge only its address would receive | the first test |
| T7 | The worker is used to alter an endpoint or a delivery | Column grants and the guard trigger: the worker only counts failures and pauses or disables an active endpoint, never changes an address, a secret or event types, a delivered or dead delivery, a payload or an attempt | `TestWebhookRecordsKeepTheirRulesInTheDatabase` |
| T8 | A flood: a failing endpoint costs the platform | Ten failures in a row pause it; a batch of 100 a tick per organisation; 410 disables it | `TestAFailedWebhookDeliveryIsRetriedThenDeadThenReplayed` |
| T9 | A lost or phantom event | Deliveries are written by the fan-out in the transaction that records the event, once per event and endpoint | `TestWebhookRecordsKeepTheirRulesInTheDatabase` (once), the first test |

## 3. Left

`[live-verify]` a run against a receiver on the public internet and DNS rebinding with a real resolver; delivery from events that are not notification types; the per-organisation rate of deliveries.
