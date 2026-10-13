# integrations/public-api Specification

## Purpose
The public, versioned API that every client uses, the web app included: its conventions for versions, errors, idempotency, concurrency, lists, long and bulk operations, webhooks and events, contracts, previews, metrics, rate limits, SDKs, the sandbox and agent access, together with the MCP server and the third-party integrations and imports. Derived from PRD-001 §8.18 and §8.18.1.

## Requirements

### Requirement: FR-INT-01 API-first public REST API
The system SHALL provide an API-first public REST API (OpenAPI 3.1) covering every user-facing operation, using the same domain commands, validations and approval policies as the UI.

Source: PRD-001 §8.18 · Priority M · Phase P1 (our UI) / P2 (third parties)

#### Scenario: The web app uses only the public API
- **WHEN** the web app performs any operation
- **THEN** it calls the public API
- **AND** there are no private endpoints (QA-API-03)

#### Scenario: The API applies the same rules as the UI
- **WHEN** an operation is requested through the API
- **THEN** it runs through the same domain commands, validations and approval policies as the same operation in the UI

#### Scenario: Third-party access opens in P2
- **GIVEN** the web app uses the API from P1
- **WHEN** a third party asks for access to the API
- **THEN** third-party access is available from P2

### Requirement: FR-INT-02 Client credentials by class
The system SHALL issue API credentials by client class (AI-34), as `integration` clients and developers' personal access tokens, with tokens coming from the OAuth server (ADR-T3).

Source: PRD-001 §8.18 · Priority M · Phase P2 / P1 (client classes)

#### Scenario: Integration clients
- **WHEN** an `integration` client is set up
- **THEN** it uses OAuth 2.1 client credentials with `private_key_jwt`
- **AND** it holds narrow scopes granted by the Owner with dual control, IP allow-lists where possible, and an expiry

#### Scenario: Personal access tokens
- **WHEN** a developer creates a personal access token
- **THEN** it is hashed at rest, carries the same scopes as OAuth clients, expires in at most 90 days and is shown once

#### Scenario: Personal access tokens are agent-class by default
- **WHEN** a personal access token is created
- **THEN** its class is `agent` by default, because a token may be pasted into an AI tool
- **AND** the Owner may reclass it as `integration` with dual control

#### Scenario: Rate limits and audit per client
- **WHEN** a client calls the API
- **THEN** rate limits apply per client and per organisation
- **AND** every call is audited with the client ID

#### Scenario: The client-class model exists from P1 for MCP
- **GIVEN** phase P1, before third-party credentials open in P2
- **WHEN** an MCP client connects
- **THEN** the client-class model (AI-34) already applies to it

### Requirement: FR-INT-03 MCP server
The system SHALL provide an MCP server exposing the AI read tools and the propose tool to MCP clients under the user's own identity and the same proposal-only rule, secured as §9.7 specifies.

Source: PRD-001 §8.18 · Priority S · Phase P1 (first-party clients) / P2 (third-party)

#### Scenario: MCP clients act under the user's own identity
- **WHEN** an MCP client calls a read tool
- **THEN** the call runs under the identity of the user the client acts for

#### Scenario: Proposal-only through MCP
- **WHEN** an MCP client wants to change data
- **THEN** it can only use the propose tool, under the same proposal-only rule as the AI assistant

#### Scenario: First-party clients before third-party clients
- **WHEN** MCP clients are admitted
- **THEN** first-party clients are admitted from P1 and third-party clients from P2, each secured as §9.7 specifies

### Requirement: FR-INT-04 Third-party integrations
The system SHALL integrate with a payment provider (Stripe), a Peppol access point, bank feeds, Google Workspace (sign-in, Drive export), Slack (specified in FR-NTF-04) and a CRM or billing system (customers, subscriptions).

Source: PRD-001 §8.18 · Priority S · Phase P2

#### Scenario: Integrations available
- **WHEN** an organisation connects its outside services
- **THEN** it can connect a payment provider (Stripe), a Peppol access point, bank feeds, Google Workspace for sign-in and Drive export, Slack for notifications and deep links, and a CRM or billing system for customers and subscriptions

#### Scenario: Approvals stay in the web app
- **GIVEN** Slack is connected
- **WHEN** a notification about an item waiting for approval is posted to Slack
- **THEN** it carries a deep link
- **AND** the approval itself stays in the web app (FR-NTF-04)

### Requirement: FR-INT-05 Import from other accounting systems
The system SHALL import from Xero, QuickBooks Online and Zoho Books for SaaS onboarding.

Source: PRD-001 §8.18 · Priority C · Phase P3

#### Scenario: What is imported
- **WHEN** an organisation onboarding from Xero, QuickBooks Online or Zoho Books runs the import
- **THEN** its chart of accounts, contacts, open items, balances and history are imported

### Requirement: FR-INT-06 Versioning
The API SHALL carry its major version in the path (`/v1/…`), and within a version its changes SHALL be additive only.

Source: PRD-001 §8.18.1 · Priority M · Phase P1

#### Scenario: A breaking change needs a new major version
- **WHEN** a change to the API would break existing clients
- **THEN** it ships only in a new major version
- **AND** `oasdiff` in CI detects it (QA-API-02)

#### Scenario: Deprecation notice
- **WHEN** an operation or field is deprecated
- **THEN** responses carry `Deprecation` (RFC 9745) and `Sunset` (RFC 8594) headers
- **AND** registered clients get at least 6 months' notice

### Requirement: FR-INT-07 Problem-details errors
API errors SHALL be RFC 9457 problem details (`application/problem+json`) with a stable `code` (the same codes as ENG-23), the field path, a plain-English `detail` and a `fix` hint.

Source: PRD-001 §8.18.1 · Priority M · Phase P1

#### Scenario: Error body
- **WHEN** a request fails
- **THEN** the response is `application/problem+json` with a stable `code` from the ENG-23 codes, the field path, a plain-English `detail` and a `fix` hint

#### Scenario: Every invalid field at once
- **GIVEN** a request with several invalid fields
- **WHEN** it is validated
- **THEN** the error lists every invalid field at once

### Requirement: FR-INT-08 Idempotency
Every `POST` that creates or changes state SHALL accept an `Idempotency-Key` header, stored 24 hours per client [verify the status of the IETF idempotency-key draft at P0].

Source: PRD-001 §8.18.1 · Priority M · Phase P1

#### Scenario: Key is kept per client for 24 hours
- **WHEN** a client sends a state-changing `POST` with an `Idempotency-Key`
- **THEN** the key is stored for that client for 24 hours

#### Scenario: Replay returns the original response
- **GIVEN** a request with an `Idempotency-Key` has been processed
- **WHEN** the same request is sent again with the same key
- **THEN** the original response is returned

#### Scenario: Same key with a different body
- **GIVEN** an `Idempotency-Key` already used by the client
- **WHEN** the key is reused with a different body
- **THEN** the API returns 422

### Requirement: FR-INT-09 Optimistic concurrency
Versioned resources SHALL return an `ETag`, and updates to them SHALL require `If-Match`, using the same row versions as `base_versions` in §9.5.

Source: PRD-001 §8.18.1 · Priority M · Phase P1

#### Scenario: Read returns the version
- **WHEN** a versioned resource is read
- **THEN** the response carries an `ETag` for the resource's row version

#### Scenario: Stale update is refused
- **GIVEN** a resource changed after the client read it
- **WHEN** the client updates it with the old version in `If-Match`
- **THEN** the API returns 412

#### Scenario: Update without If-Match
- **WHEN** an update to a versioned resource has no `If-Match`
- **THEN** it is not applied

### Requirement: FR-INT-10 Lists and value formats
List endpoints SHALL use cursor pagination and support filtering on documented fields, sorting and sparse fieldsets, and the API SHALL format money, dates and IDs in one way throughout.

Source: PRD-001 §8.18.1 · Priority M · Phase P1

#### Scenario: Cursor pagination
- **WHEN** a client lists a collection
- **THEN** it can ask for a `page_size` of at most 500
- **AND** it gets an opaque `next_cursor` for the next page

#### Scenario: Filtering, sorting and sparse fieldsets
- **WHEN** a client lists a collection
- **THEN** it can filter on the documented fields, sort, and ask for only the fields it needs

#### Scenario: Value formats
- **WHEN** a response carries money, dates or IDs
- **THEN** money is a decimal string with its currency (ENG-12), dates are ISO 8601 and IDs are UUIDv7 strings

### Requirement: FR-INT-11 Long and bulk operations
Requests that may take longer than 10 s SHALL return `202 Accepted` and an operation resource, and bulk endpoints SHALL return a result per item.

Source: PRD-001 §8.18.1 · Priority M · Phase P1

#### Scenario: Long request
- **WHEN** a request may take longer than 10 s
- **THEN** the API returns `202 Accepted` and an operation resource the client can poll or subscribe to

#### Scenario: Bulk request
- **WHEN** a bulk endpoint receives up to 1,000 items
- **THEN** it returns a result for each item
- **AND** it never fails silently

### Requirement: FR-INT-12 Webhooks and events
Outbound webhooks SHALL follow the Standard Webhooks specification, and events SHALL use the CloudEvents 1.0 envelope, be catalogued in AsyncAPI 3 (`contracts/events/`) and be emitted from the transactional outbox.

Source: PRD-001 §8.18.1 · Priority S · Phase P2

#### Scenario: Signed delivery
- **WHEN** a webhook is delivered
- **THEN** it carries an HMAC-SHA256 signature, a timestamp and a `webhook-id` for idempotent receipt
- **AND** it is valid only within a 5-minute replay window

#### Scenario: Retries and dead letters
- **WHEN** a delivery fails
- **THEN** it is retried with exponential back-off for 3 days
- **AND** it then goes to a dead-letter view with manual replay

#### Scenario: No lost or phantom events
- **WHEN** a change commits or rolls back
- **THEN** its events are emitted from the transactional outbox, so no event is lost and none is emitted for a change that did not commit

#### Scenario: Endpoint verified before activation
- **WHEN** a webhook endpoint is registered
- **THEN** it is verified before it is activated

### Requirement: FR-INT-13 Machine-readable contracts
The API SHALL be described for both developers and models, with OpenAPI descriptions and examples, JSON Schema for every resource, a published API reference, `llms.txt` and an "agent guide".

Source: PRD-001 §8.18.1 · Priority M · Phase P1

#### Scenario: Operation descriptions
- **WHEN** an operation is described in the OpenAPI document
- **THEN** its description and examples state its purpose, side effects, required permission and the approval policy that may apply

#### Scenario: Schemas for every resource
- **WHEN** a resource is published in the API
- **THEN** a JSON Schema describes it

#### Scenario: Agent guide
- **WHEN** the API reference is published
- **THEN** `llms.txt` and an agent guide are published with it
- **AND** the agent guide explains how to read, how to propose and what is never allowed

### Requirement: FR-INT-14 Preview before commit
Every write command SHALL support a dry run (`Prefer: return=preview`) that returns the validation result and the ledger impact without committing.

Source: PRD-001 §8.18.1 · Priority S · Phase P1

#### Scenario: Dry run commits nothing
- **WHEN** a client sends a write command with `Prefer: return=preview`
- **THEN** the response holds the validation result and the ledger impact
- **AND** nothing is committed

#### Scenario: Same simulation as proposals
- **WHEN** a preview is computed
- **THEN** it uses the same simulation the proposal layer uses (§9.3)

### Requirement: FR-INT-15 Semantic metrics endpoint
The system SHALL expose the semantic layer (AI-03) through `POST /v1/metrics:query` to any authorised client.

Source: PRD-001 §8.18.1 · Priority S · Phase P2

#### Scenario: Ask for a metric instead of writing SQL
- **GIVEN** an authorised integration or agent
- **WHEN** it posts a query for `revenue by customer by month` to `POST /v1/metrics:query`
- **THEN** the semantic layer answers it
- **AND** the client writes no SQL

### Requirement: FR-INT-16 Rate limits and quotas
The API SHALL signal rate limits and quotas with `RateLimit` / `RateLimit-Policy` headers [verify the IETF draft status at P0] and `Retry-After` on 429.

Source: PRD-001 §8.18.1 · Priority M · Phase P1

#### Scenario: Limits are signalled
- **WHEN** a client calls the API
- **THEN** the response carries `RateLimit` and `RateLimit-Policy` headers

#### Scenario: Over the limit
- **WHEN** a client exceeds its limit
- **THEN** the API returns 429 with `Retry-After`

#### Scenario: Default limits
- **WHEN** no organisation-specific limit is set
- **THEN** each client may make 600 reads and 120 writes a minute
- **AND** the organisation can adjust these limits

### Requirement: FR-INT-17 Generated SDKs
The system SHALL generate its API clients from the OpenAPI document: a TypeScript client used by the web app, and Go and Python clients for integrators.

Source: PRD-001 §8.18.1 · Priority S · Phase P1 (TS) / P2 (Go, Python)

#### Scenario: TypeScript client
- **WHEN** the OpenAPI document changes
- **THEN** the TypeScript client the web app uses is generated from it

#### Scenario: Clients for integrators
- **WHEN** clients are published for integrators
- **THEN** Go and Python clients are generated from the OpenAPI document

### Requirement: FR-INT-18 Sandbox
Each organisation SHALL be able to create a sandbox copy of the sample company (FR-UX-10) for integration testing.

Source: PRD-001 §8.18.1 · Priority S · Phase P2

#### Scenario: Sandbox is isolated from real data
- **WHEN** an organisation creates a sandbox
- **THEN** it is a copy of the sample company with its own credentials
- **AND** those credentials have no access to the organisation's real data

### Requirement: FR-INT-19 Agent-to-agent
The system SHALL publish an A2A agent card so other organisations' agents can ask Fyniq read-only questions under OAuth.

Source: PRD-001 §8.18.1 · Priority C · Phase P3

#### Scenario: Read-only questions from another organisation's agent
- **WHEN** another organisation's agent asks a question through the A2A agent card under OAuth
- **THEN** it is answered read-only, through the same tool layer and limits as MCP
