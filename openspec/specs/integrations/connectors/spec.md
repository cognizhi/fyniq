# integrations/connectors Specification

## Purpose
How every external service plugs into Fyniq as an interchangeable adapter packaged as a connector: the integration framework and Settings › Integrations, the connector manifest, catalogue and lifecycle, the rules every connector obeys, ticketing connections and third-party connectors. Derived from PRD-001 §8.24.

## Requirements

### Requirement: FR-INT-20 Integration framework
Every external service (sign-in, outbound and inbound email, messaging, payments, bank feeds, the Peppol Access Point, document AI) SHALL implement a capability interface with interchangeable adapters, managed in Settings → Integrations.

Source: PRD-001 §8.24 · Priority M · Phase P1

#### Scenario: Who manages connections
- **WHEN** Settings → Integrations is opened
- **THEN** the Super admin manages installation-wide connections
- **AND** the Owner or Administrator manages organisation-level connections

#### Scenario: Managing a connection
- **WHEN** a connection is managed in Settings → Integrations
- **THEN** the user can pick an adapter, enter credentials, test the connection, see its health, enable or disable it and set a failover order

#### Scenario: Credentials are write-only
- **WHEN** credentials for a connection are entered
- **THEN** they are kept in Secret Manager
- **AND** they are write-only in the interface

#### Scenario: Failover after an unknown outcome
- **GIVEN** a send whose outcome at the primary adapter is unknown
- **WHEN** the failover adapter would send it
- **THEN** the send is first reconciled, or its idempotency key checked, before the failover sends it

#### Scenario: Changes are audited and dual-controlled
- **WHEN** a connection is changed
- **THEN** the change is audited
- **AND** it is dual-controlled (FM-1)

#### Scenario: Each OAuth or SSO connection is switched on its own
- **WHEN** sign-in providers (FR-ADM-08), integration connections (email, chat, ticketing, payment providers, bank data), OAuth clients and MCP access (FR-INT-02, AI-35) are configured
- **THEN** each one can be enabled or disabled on its own

#### Scenario: Disabling a connection
- **WHEN** a connection is disabled
- **THEN** it blocks new work at once, checked when each piece of work starts and never from a cached flag
- **AND** it revokes the tokens Fyniq issued for it within 60 seconds (AI-38, AI-43)
- **AND** its configuration is kept for re-enabling

#### Scenario: G4-096 Connector disabled with queued and in-flight work
- **GIVEN** a connector with tasks queued and one in flight
- **WHEN** the connector is disabled
- **THEN** no new work commits after the disable (checked at start, not cached)
- **AND** queued work is held or cancelled as the manifest declares
- **AND** the in-flight item finishes or is reconciled, never dropped silently

#### Scenario: Every adapter has a local substitute and contract tests
- **WHEN** an adapter is added
- **THEN** it has a local substitute and contract tests

#### Scenario: A new provider never changes the core
- **WHEN** a provider is added
- **THEN** it is added as an adapter, packaged and installed as a connector (FR-INT-25…28)
- **AND** the core is not changed

#### Scenario: Adapters at go-live
- **WHEN** the platform goes live
- **THEN** the number of adapters shipped per capability is the one decided under open question 45

### Requirement: FR-INT-23 Ticketing connections
The system SHALL create a ticket in a connected ticketing system from any task, check, finding of the AI company secretary, security alert or "Report a problem", by hand or by routing rules.

Source: PRD-001 §8.24 · Priority S · Phase P2

#### Scenario: Routing rules
- **WHEN** an event matches a routing rule
- **THEN** a ticket is created with the project, issue type, priority and labels the rule maps its event type to
- **AND** users are mapped by verified email

#### Scenario: Ticket and item link to each other
- **WHEN** a ticket is created for an item
- **THEN** the ticket links back to the item
- **AND** the item stores the ticket key and URL

#### Scenario: Status and comments sync back
- **WHEN** the provider sends a webhook about the ticket
- **THEN** its signature is verified
- **AND** the ticket's status and comments are synced back to the item

#### Scenario: Closing a ticket never decides anything
- **WHEN** a ticket is closed
- **THEN** it may close a task
- **AND** it never approves anything, marks a statutory obligation met or changes a financial record

#### Scenario: Minimal content, deduplicated and retried
- **WHEN** tickets are created for a connection
- **THEN** the content level is minimal by default, with no amounts or personal data
- **AND** tickets are deduplicated per finding and retried
- **AND** ticket creation can be enabled or disabled per connection

#### Scenario: Jira Cloud tickets
- **WHEN** a ticket is created in Jira Cloud
- **THEN** it uses REST API v3 (`POST /rest/api/3/issue`) with the description in Atlassian Document Format
- **AND** `Retry-After` is honoured

#### Scenario: Jira Cloud credentials
- **WHEN** a Jira Cloud connection is set up
- **THEN** it uses an OAuth 2.0 (3LO) app or a scoped API token
- **AND** because such tokens expire in 1–365 days, the connector warns before expiry

#### Scenario: Jira Cloud webhooks
- **WHEN** a Jira Cloud webhook arrives
- **THEN** its `X-Hub-Signature` is verified and it is deduplicated on `X-Atlassian-Webhook-Identifier`
- **AND** webhooks registered by REST, which expire after 30 days, are renewed by the connector

#### Scenario: Jira Cloud data residency
- **WHEN** an organisation on an Atlassian Standard plan or above chooses where its Jira data is kept
- **THEN** Singapore data residency is available from Atlassian

#### Scenario: Other ticketing providers
- **WHEN** Linear (GraphQL), Zoho Desk (OAuth with an `orgId` header, per-region domains), GitHub Issues, Zoho Projects or, in P3, ServiceNow (§8.24 table) is connected
- **THEN** it follows the same connector contract as Jira Cloud

### Requirement: FR-INT-24 Inbound from ticketing
A ticketing system, or any integration, SHALL be able to create and update tasks through the public API with a narrow scope (`fyniq.write.tasks`).

Source: PRD-001 §8.24 · Priority S · Phase P2

#### Scenario: Tasks through the narrow scope
- **GIVEN** an integration holding the `fyniq.write.tasks` scope
- **WHEN** it creates or updates a task through the public API
- **THEN** the task is created or updated

#### Scenario: No financial records from ticketing
- **WHEN** a ticketing system or other integration tries to create or change a financial record
- **THEN** it is refused

### Requirement: FR-INT-25 Connectors and their manifest
Every external integration SHALL be a connector: an adapter packaged with a manifest, shipped in a connector catalogue with each release.

Source: PRD-001 §8.24 · Priority M · Phase P1

#### Scenario: What the manifest declares
- **WHEN** a connector is packaged
- **THEN** its manifest states its id, name, publisher and version; the capabilities it implements (sign-in, outbound or inbound email, messaging, ticketing, payment provider, bank data, e-invoicing, document AI); a configuration schema (JSON Schema); the secrets it needs; the scopes it asks of the provider and the Fyniq scopes it receives; the data classes it sends out; its processing regions; the webhooks it registers; a health check; a test action; its local substitute; and the minimum platform version

#### Scenario: Connector catalogue
- **WHEN** a release ships
- **THEN** its connectors ship in the connector catalogue of that release

### Requirement: FR-INT-26 Connector lifecycle
Each connector SHALL follow the lifecycle install → configure → test → enable → upgrade → disable → uninstall, per organisation (or installation-wide for the Super admin), with every step audited and dual-controlled (FR-INT-20).

Source: PRD-001 §8.24 · Priority M · Phase P1

#### Scenario: Pause
- **WHEN** a connector is paused
- **THEN** it stops new work and holds its queue

#### Scenario: Disable
- **WHEN** a connector is disabled
- **THEN** it stops new work, cancels the queued work its manifest marks as cancellable and revokes its tokens

#### Scenario: Uninstall
- **WHEN** a connector is uninstalled
- **THEN** it does what disable does and also removes its provider-side webhooks and its secrets
- **AND** its tokens are revoked at the provider where the provider allows it
- **AND** its audit history is kept

#### Scenario: The manifest declares work, secret and parser handling
- **WHEN** a connector's manifest is read
- **THEN** it declares, per capability, what happens to queued and in-flight work, how secrets are rotated without downtime, and the parser version each imported item was read with

#### Scenario: A parser upgrade never re-reads silently
- **WHEN** a connector's parser is upgraded
- **THEN** items imported earlier are not re-read silently

#### Scenario: Upgrade shows the manifest diff
- **WHEN** a connector is upgraded
- **THEN** the manifest diff is shown
- **AND** new scopes or data classes need the Owner's consent again

#### Scenario: Every step is audited and dual-controlled
- **WHEN** any lifecycle step is taken
- **THEN** it is audited and dual-controlled

### Requirement: FR-INT-27 Connector rules
A connector SHALL use only the domain commands and read APIs the UI uses, with no private write path, and SHALL run as its own principal with its own audit identity, rate limits and circuit breaker.

Source: PRD-001 §8.24 · Priority M · Phase P1 / P2 (re-ingestion preview)

#### Scenario: No private write path
- **WHEN** a connector writes data
- **THEN** it does so through the same domain commands the UI uses

#### Scenario: The never-list binds what an AI may cause
- **WHEN** an AI or agent causes work through a connector
- **THEN** the AI never-list (§9.6, AI-66) binds what it may cause
- **AND** the never-list does not apply to the connector's own transport

#### Scenario: Each capability names who may trigger it
- **WHEN** a connector's manifest lists a capability
- **THEN** it names who may trigger it: a completed human authority (a payment batch approved under FR-BNK-12, a filing approved under FR-TAX-03), a system rule (notifications, InvoiceNow transmission of an issued document), or nobody

#### Scenario: A connector never approves
- **WHEN** work reaches a connector, including work started from an AI proposal
- **THEN** the connector approves nothing
- **AND** the work still needs the human authority of the underlying action

#### Scenario: Core work continues when a connector fails
- **GIVEN** a connector fails
- **WHEN** core work runs
- **THEN** it continues, for example chat notifications fall back to email

#### Scenario: Re-ingestion runs as a preview first
- **WHEN** a past date range is re-fetched or re-read, after an outage or a parser fix
- **THEN** it first runs as a preview listing what it would add, change or leave

#### Scenario: Re-ingestion is idempotent on source IDs
- **WHEN** a re-ingestion run is repeated
- **THEN** it adds nothing, because the run is idempotent on source IDs
- **AND** each imported item records the connector and parser version

### Requirement: FR-INT-28 Third-party connectors
Third-party connectors SHALL run out-of-process, built on the public API, events and an `integration`-class OAuth client, and SHALL be described by the same manifest.

Source: PRD-001 §8.24 · Priority C · Phase P3

#### Scenario: Listed after review
- **WHEN** a third-party connector is submitted
- **THEN** it is listed only after review

#### Scenario: Out of process only
- **WHEN** a third-party connector runs
- **THEN** it runs out of process
- **AND** in-process plug-ins (for example WebAssembly) are an option for later (ADR-T8), not a commitment
