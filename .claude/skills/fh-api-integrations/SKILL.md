---
name: fh-api-integrations
description: Design, change and review the public API, events and webhooks of Cognizhi Finance Hub Cloud — API-first (the web app uses only the public, versioned OpenAPI 3.1 API), RFC 9457 errors with stable codes, Idempotency-Key, ETag/If-Match, cursor pagination, 202 operations, bulk results, dry-run previews (Prefer: return=preview), the /v1/metrics:query semantic endpoint, RateLimit headers, Standard Webhooks with CloudEvents and AsyncAPI, OAuth client classes (first-party-app, integration, agent), generated SDKs and the sandbox. Use whenever you add or change an endpoint, a request or response field, an error code, an event type, a webhook, an OAuth scope or client, anything under contracts/, or when someone asks to "expose X in the API", "integrate with Y", "send a webhook", "add an API key", "make it AI-friendly" or "is this a breaking change".
---

# Public API, events and webhooks

FH Cloud is **API-first** (PRD-001 rev 0.2, principle 11, FR-INT-01). Every capability is an API
operation before it is a screen. The web app is one client of the same public, versioned API that
integrators and AI agents use. There are **no private endpoints** (QA-API-03). The contracts in
`contracts/` are the source of truth. Server stubs, the TypeScript client used by `apps/web`, the
Go and Python SDKs and the AI tool schemas are all generated from them.

> **Status:** no code exists yet. This skill states the conventions and target paths that P0
> implements (§12.4 layout, §12.8 command contract). Commands marked *(P0)* arrive in P0. The
> repository has these targets today: `make help | check | check-tokens | check-config |
> deps-up | deps-down | hooks`.

## Where things live

| Path | Content |
|---|---|
| `contracts/openapi/` | OpenAPI 3.1 document(s) for `/v1`; one file per area, bundled in CI |
| `contracts/events/` | AsyncAPI 3 catalogue of event types (CloudEvents envelope) |
| `contracts/schemas/` | JSON Schema for every resource (referenced by OpenAPI, AsyncAPI and AI tools) |
| `contracts/ai/` | AI tool and task-type definitions (see **fh-ai-proposals**) |
| `contracts/partners/` | Partners' OpenAPI documents (Peppol access point, banks), mocked locally by Prism |
| `cmd/api`, `internal/` | The Go server (huma generates OpenAPI from typed handlers; the diff against `contracts/` must be empty) |
| `apps/web/src/api/` | The generated TypeScript client: the **only** way the web app calls the server |

## The conventions (FR-INT-06…19)

| ID | Rule | How it shows on the wire |
|---|---|---|
| FR-INT-06 | **Versioning.** Major version in the path; additive changes only inside `/v1`; a breaking change means `/v2`. Deprecations get 6 months' notice. | `/v1/invoices`; `Deprecation: @1767225600` (RFC 9745); `Sunset: Tue, 30 Jun 2027 00:00:00 GMT` (RFC 8594); a `Link` to the migration note |
| FR-INT-07 | **Errors** as RFC 9457 problem details with a stable `code` (the same codes as ENG-23), all invalid fields at once, and a plain-English fix. | `Content-Type: application/problem+json` (example below) |
| FR-INT-08 | **Idempotency** on every state-changing `POST`; the key is stored for 24 h per client; a replay returns the original response; the same key with a different body returns 422. [verify the IETF draft status at P0] | `Idempotency-Key: 01J9Z…` |
| FR-INT-09 | **Optimistic concurrency** on versioned resources; the version is the same row version used in proposal `base_versions` (§9.5). | `ETag: "v7"`; update with `If-Match: "v7"`; stale → `412` |
| FR-INT-10 | **Lists**: cursor pagination, documented filters, sorting, sparse fields. Money as a decimal string with its currency (ENG-12), dates ISO 8601, IDs as UUIDv7 strings. | `?page_size=100&cursor=…&filter[status]=posted&sort=-date&fields=id,total` → `{"data":[…],"next_cursor":"…"}` |
| FR-INT-11 | **Long and bulk operations**: > 10 s → `202 Accepted` + an operation resource; bulk ≤ 1,000 items with a result per item. | `Location: /v1/operations/{id}`; `{"results":[{"index":0,"status":201,"id":"…"},{"index":1,"status":422,"problem":{…}}]}` |
| FR-INT-12 | **Webhooks**: Standard Webhooks (HMAC-SHA256, timestamp, `webhook-id`, 5-minute replay window), retries for 3 days with back-off, then a dead-letter view; CloudEvents 1.0 envelope; catalogued in AsyncAPI 3; emitted from the transactional outbox. | `webhook-id`, `webhook-timestamp`, `webhook-signature: v1,…`; body `{"specversion":"1.0","type":"fh.invoice.posted.v1",…}` |
| FR-INT-13 | **Machine-readable**: descriptions and examples written for developers and models; `llms.txt` and the agent guide are published with the reference. | Every operation has a `description`, `x-fh-permission`, `x-fh-approval-policy`, examples |
| FR-INT-14 | **Preview before commit**: every write command supports a dry run that returns validation and ledger impact without committing. | `Prefer: return=preview` → `200` with `{"valid":true,"impact":{…}}` and `Preference-Applied: return=preview` |
| FR-INT-15 | **Semantic metrics** for integrations and agents (P2); never SQL. | `POST /v1/metrics:query {"measures":["revenue"],"dimensions":["customer","month"],"period":"2026-Q3"}` |
| FR-INT-16 | **Rate limits** signalled in headers; defaults 600 reads and 120 writes a minute per client. [verify the IETF draft status at P0] | `RateLimit-Policy`, `RateLimit`, `Retry-After` on `429` |
| FR-INT-17 | **Generated SDKs**: TypeScript (P1, used by the web app), Go and Python (P2). | Never hand-write a client |
| FR-INT-18 | **Sandbox** organisation with the sample company for integration testing (P2). | Separate credentials; no real data |
| FR-INT-19 | **A2A agent card** for read-only questions (C, P3). | Through the same tool layer and limits as MCP |

### Example operation (contracts/openapi/sales.yaml)

```yaml
/v1/invoices/{invoice_id}:post:
  post:
    operationId: postInvoice
    summary: Post a draft invoice to the ledger
    description: >
      Posts an approved draft invoice. Creates AR, revenue (or deferred revenue) and GST
      lines in the same transaction (ENG-02). Side effects: the invoice becomes immutable
      (FR-GL-12); later changes create a new version. Requires `sales.invoice.post`.
      An approval policy may apply (FR-APR-01): if it does, the response is 202 with an
      approval request instead of 200. Agents (client class `agent`) cannot call this;
      they use the `ar.post_invoice` proposal task instead (AI-34).
    x-fh-permission: sales.invoice.post
    x-fh-approval-policy: invoice
    x-fh-req: [FR-AR-03, FR-INT-08, FR-INT-09, FR-INT-14]
    parameters:
      - $ref: "#/components/parameters/InvoiceId"
      - $ref: "#/components/parameters/IdempotencyKey"
      - $ref: "#/components/parameters/IfMatch"
      - $ref: "#/components/parameters/PreferPreview"
    responses:
      "200": { $ref: "#/components/responses/InvoicePosted" }
      "202": { $ref: "#/components/responses/ApprovalRequired" }
      "409": { $ref: "#/components/responses/Problem" }   # period locked
      "412": { $ref: "#/components/responses/Problem" }   # stale If-Match
      "422": { $ref: "#/components/responses/Problem" }   # validation
```

Amounts in every schema: `{"amount": "1090.00", "currency": "SGD"}`. The JSON type is
`string` with a decimal pattern; `number` is never used for money, rates or quantities (ENG-10/12;
a Spectral rule enforces it).

### Example problem details

```json
{
  "type": "https://docs.finance-hub.example/errors/FX_RATE_MISSING",
  "title": "No exchange rate for this currency",
  "status": 422,
  "code": "FX_RATE_MISSING",
  "detail": "There is no USD rate for 3 Oct 2026, so INV-0142 cannot post.",
  "fix": "Add the USD rate for 3 Oct 2026 in Settings → FX rates, or enter the rate on the invoice.",
  "instance": "/v1/invoices/0192f3a4-…/post",
  "request_id": "req_01J9Z…",
  "errors": [
    { "path": "/lines/0/currency", "code": "FX_RATE_MISSING", "detail": "USD on 2026-10-03" }
  ]
}
```

`code` values are stable and documented, and each is produced by at least one fixture
(QA-RPT-02). Messages say what is wrong and how to fix it (ENG-23).

## Outside services: capability interfaces and adapters (rev 0.6, FR-INT-20)

Every outside service (sign-in, outbound and inbound email, Slack and Zoho Cliq, payments, bank
feeds, the Peppol Access Point, document AI) is an **adapter behind a capability interface**.
Providers are configuration, never code paths in the core.

| Capability | Adapters | Local substitute |
|---|---|---|
| Sign-in (FR-ADM-08) | Google, Zoho (code flow; one provider per Zoho data centre; identify by `sub`), any OpenID Connect provider, SAML (Enterprise) | Firebase Authentication emulator (mock providers) |
| Outbound email (FR-INT-21) | Zoho ZeptoMail (now Zoho CPaaS; no "delivered" event, no idempotency key), Resend (`Idempotency-Key`, delivered events; account data in the US), SMTP; primary and failover | Mailpit |
| Inbound email (FR-INT-22) | Resend receiving; IMAP polling of a mailbox (ZeptoMail is send-only) | Mailpit plus a local webhook |
| Messaging (FR-NTF-04) | Slack, Zoho Cliq, generic incoming webhook | `/dev/messages` sink |

To add a provider:
1. Implement the interface with **contract tests shared by every adapter** of that capability.
2. Credentials go to Secret Manager; the interface shows them write-only.
3. Add a health check and a test action (test send, test message, test sign-in).
4. Verify inbound webhook signatures, and make sends idempotent.
5. Record the provider's processing region in the data inventory (PDPA).
6. Add a local substitute (`fh-local-dev`).

Chat messages carry deep links only: **approvals never happen in chat** (FR-NTF-04). The
platform's own MFA applies whatever the sign-in provider.

**Connectors (rev 0.7, FR-INT-25…28).** Package every adapter as a connector in `connectors/<id>/`.

The manifest declares:
- id, version, capabilities, config JSON Schema and secrets;
- provider scopes and the FH scopes it receives;
- outbound data classes and processing regions;
- webhooks, health check, test action, local substitute and minimum platform version.

The lifecycle is install → configure → test → enable → upgrade (the manifest diff needs
re-consent) → disable → uninstall (revoke, delete secrets, keep the audit).

A connector uses only the domain commands the UI uses, the never-list binds it, and it runs as
its own principal. Bank and payment-provider connectors are **read-only** (FR-BNK-15,
FR-PSP-01). Parse money from raw JSON as exact decimals, never as floats (HitPay sends JSON
numbers). Every OAuth or SSO connection can be enabled or disabled on its own.

## Clients, scopes and classes

| Class (AI-34) | Who | Auth | Writes |
|---|---|---|---|
| `first-party-app` | Our web app and PWA | First-party session cookie; **no bearer tokens in the browser** (SEC-08) | Yes, under the user's permissions and approval policies |
| `integration` | A system (bank feed, Stripe, CRM) | OAuth 2.1 client credentials with `private_key_jwt`; tokens from Ory Hydra (ADR-T3); IP allow-list where possible; owner and expiry (SEC-07) | Only the narrow scopes granted by the Owner with dual control; approval policies still apply |
| `agent` | **Every MCP client and every client not explicitly classed** | OAuth 2.1 code + PKCE as the user (see **fh-mcp-server**) | **Proposals only.** The API rejects any other write from an agent-class token (CTL-MCP-01) |

Personal access tokens (P2) are hashed at rest, scoped, expire in ≤ 90 days and are shown once
(FR-INT-02). Every call is audited with the client ID (AUD-01).

## Gates (all block merge)

| Gate | What it checks | Command |
|---|---|---|
| QA-API-01 | Contract tests: the server matches `contracts/openapi/` | `make test` *(P0)* |
| QA-API-02 | `oasdiff` against the last release: no breaking change inside `/v1` | `make test` *(P0)* |
| QA-API-03 | Every registered route is documented; the web app calls only generated operations | `make test` *(P0)* |
| NFR-API-01 | Spectral lint: description, example, permission and error codes on every operation; no `number` for money | `make test` *(P0)* |
| QA-SEC-03 | Authorisation matrix: every role × endpoint, masking, SoD per team mode | `make test` *(P0)* |

What `oasdiff` calls breaking: removing or renaming a path, field, enum value or parameter;
making an optional request field required; narrowing a type; changing a response status that
clients rely on. Adding an optional field, a new endpoint or a new enum value in a response
documented as open-ended is not breaking.

## Checklists

**Add or change an endpoint**

1. Write the operation in `contracts/openapi/` first: description for humans and models, the
   permission, the approval policy, examples, every problem `code`, and `x-fh-req` IDs.
2. Money and rates are decimal strings with a currency; IDs are UUIDv7 strings.
3. A state-changing `POST` takes `Idempotency-Key`; versioned resources use `ETag`/`If-Match`;
   writes support `Prefer: return=preview`.
4. Long work returns `202` + an operation; bulk work returns per-item results.
5. Implement through the **domain command layer** (the same code the UI, imports and the
   proposal executor use). Emit the audit event in the same transaction (AUD-01).
6. Regenerate the clients; the web app uses the new operation only through the generated client.
7. Add contract, authorisation-matrix and problem-code fixtures; reference the requirement IDs
   in the tests (`// req: FR-…`, QA-TRACE-01).
8. Breaking? Then it is `/v2`, a deprecation plan and a `feat(api)!:` commit with a
   `BREAKING CHANGE:` footer and upgrade notes (REL-03).

**Add an event type**

1. Define it in `contracts/events/` (AsyncAPI 3): `type` = `fh.<resource>.<verb>.v<N>`,
   data schema from `contracts/schemas/`, and when it fires.
2. Write it to the outbox in the same transaction as the change; never publish directly.
3. Payloads carry IDs and the changed fields, never NRIC/FIN, full bank numbers or salaries
   (AI-02, SEC-15). Subscribers fetch details through the API under their own permissions.
4. A breaking payload change is a new `.vN+1` type, emitted alongside the old one until its
   sunset date.

**Add a webhook subscription feature**

1. Endpoint verification before activation; secrets generated per endpoint and rotatable.
2. Sign with Standard Webhooks; include `webhook-id` so receivers can de-duplicate.
3. Retries with exponential back-off for 3 days, then the dead-letter view with manual replay.
4. Every delivery attempt is logged; the Owner can pause an endpoint.

**Add an OAuth scope or client capability**

1. Name scopes `fh.<read|write|propose>.<area>[.<task>]`; there is no wildcard write scope.
2. Classify the client (AI-34). A new class is an ADR, not a code change.
3. Granting a write scope to an integration is dual-controlled (FM-1) and appears in the
   quarterly access review (CTL-API-02).

## Local testing *(P0)*

`make dev` serves the API behind Caddy at one origin with Hydra, Mailpit and the Prism partner
mocks (§12.8). Create a local integration client with `make setup` (it registers the
first-party clients) and call the API with `curl` or the generated SDK. Webhooks can target a
local receiver; partner callbacks come from Prism.

## Commits

`feat(api): …`, `fix(contracts): …`, `feat(api)!: …` with `BREAKING CHANGE:`. Use scope
`contracts` for schema-only changes and `api` for server behaviour.

## Definition of done

- [ ] The contract changed first and is complete (descriptions, examples, permission, codes).
- [ ] Spectral, oasdiff, contract and authorisation-matrix tests are green.
- [ ] No `number` for money; no private route; no hand-written client.
- [ ] Idempotency, concurrency and preview behave as specified, and are tested.
- [ ] Audit events emitted; webhook and event payloads free of excluded personal data.
- [ ] Docs regenerated (reference, `llms.txt`, agent guide); upgrade notes in the commit body
      if integrators must act.
