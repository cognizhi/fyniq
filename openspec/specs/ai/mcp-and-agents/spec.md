# ai/mcp-and-agents Specification

## Purpose
How MCP clients and other external agents reach Fyniq's read tools and the propose tool, closed by default: client classes, enablement and allowlists, OAuth authorisation, consent and scopes, token lifetimes, transport, the tool surface and its results, approvals that stay in Fyniq, limits, audit, detection, third-party clients and outbound agents. Derived from PRD-001 §9.7.

## Requirements

### Requirement: AI-15 MCP server
An MCP server SHALL expose the same read tools and the `propose` tool to MCP clients, with OAuth sign-in as the user.

Source: PRD-001 §9.7 · Priority S · Phase P1 / P2

#### Scenario: Same tools as the in-app assistant
- **WHEN** an MCP client lists the tools
- **THEN** it gets the same read tools and the `propose` tool as the in-app assistant

#### Scenario: The client acts as the signed-in user
- **WHEN** a user connects an MCP client
- **THEN** the user signs in with OAuth
- **AND** the client acts as that user

#### Scenario: P1 serves members using allowlisted first-party clients
- **GIVEN** phase P1
- **WHEN** an organisation member connects with an allowlisted first-party client (Claude Desktop, Claude Code)
- **THEN** the connection is allowed

#### Scenario: Third-party clients from P2
- **GIVEN** phase P2
- **WHEN** a third-party client registered under AI-46 connects
- **THEN** the connection is allowed

### Requirement: AI-16 Same rules as the in-app assistant
MCP clients SHALL get exactly the same permissions, exclusions, proposal-only rule and audit as the in-app assistant, and no tool exposed over MCP SHALL write except `propose`.

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: Same permissions and exclusions
- **WHEN** an MCP client reads data for a user
- **THEN** it sees exactly what the in-app assistant would see for that user, with the same AI exclusions

#### Scenario: Only the propose tool writes
- **WHEN** the tools exposed over MCP are listed
- **THEN** none of them can write except `propose`
- **AND** what `propose` writes is a proposal, as for the in-app assistant

#### Scenario: Same audit
- **WHEN** an MCP client calls a tool
- **THEN** the call is audited as the in-app assistant's calls are

### Requirement: AI-17 Versioned, contract-tested tool definitions
Tool descriptions and schemas SHALL be versioned and contract-tested, so external agents cannot rely on undocumented behaviour.

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: A changed tool definition gets a new version
- **WHEN** a tool's description or schema changes
- **THEN** it is published as a new version

#### Scenario: Contract tests cover every tool
- **WHEN** the contract tests run
- **THEN** each tool's behaviour is tested against its versioned description and schema

### Requirement: AI-34 Client classes
Every OAuth client SHALL be registered with a class, `first-party-app` (our web app and PWA), `integration` (a system with narrow write scopes, FR-INT-02) or `agent` (any AI client), and agents SHALL be proposal-only whatever scopes they request.

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: MCP and unclassed clients are agents
- **WHEN** an MCP client, or any client not explicitly classed, is registered
- **THEN** its class is `agent`

#### Scenario: The API rejects non-proposal writes from agents
- **WHEN** an `agent`-class token attempts any write other than a proposal
- **THEN** the API rejects it, whatever scopes the client requested
- **AND** control test CTL-MCP-01 verifies this

#### Scenario: A class is the Owner's declaration
- **WHEN** a client is classed
- **THEN** the class is the Owner's declaration about the client
- **AND** the API enforces the class's rights but cannot detect a model behind an `integration` credential

#### Scenario: Integration scopes assume an automated client
- **WHEN** scopes are granted to an `integration` client
- **THEN** they are only scopes the Owner would accept from an unattended program, AI included
- **AND** its writes stay under the approval policies (FR-INT-01), with the same anomaly alerts (AI-45, SEC-31) and a place in the quarterly access review (FR-CTL-06)

#### Scenario: G4-094 Agent on an integration credential; image exfiltration attempt
- **GIVEN** an agent uses an `integration`-class credential
- **AND** separately, a bank description carries a Markdown image whose URL encodes ledger data
- **WHEN** both are processed
- **THEN** the agent can do only what the integration scopes and approval policies allow
- **AND** the in-app assistant renders no remote image and no link outside the allowed set

### Requirement: AI-35 Off by default, enabled deliberately
MCP access SHALL be off per organisation until the Owner enables it (dual-controlled, FM-1) and off per user until the user opts in, and only clients on the organisation's allowlist SHALL be able to connect.

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: Off for the organisation until enabled under dual control
- **GIVEN** an organisation whose Owner has not enabled MCP access
- **WHEN** an MCP client tries to connect
- **THEN** it cannot connect
- **AND** enabling MCP access is dual-controlled (FM-1)

#### Scenario: Off for each user until they opt in
- **GIVEN** MCP access is enabled for the organisation and a user has not opted in
- **WHEN** an MCP client tries to connect as that user
- **THEN** it cannot connect

#### Scenario: Only allowlisted clients connect
- **WHEN** a client that is not on the organisation's allowlist tries to connect
- **THEN** it cannot connect

#### Scenario: No dynamic client registration
- **WHEN** a client attempts dynamic client registration
- **THEN** it is refused, because dynamic client registration is disabled

#### Scenario: P1 first-party clients are pre-registered
- **GIVEN** phase P1
- **WHEN** the first-party public clients are set up
- **THEN** they are pre-registered with PKCE and fixed loopback redirect URIs

### Requirement: AI-36 Authorisation
The MCP server SHALL accept only OAuth 2.1 authorisation code with PKCE (S256), with no implicit and no password grant, and, as a resource server, only tokens whose audience is the MCP resource (RFC 8707 resource indicators).

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: Only authorisation code with PKCE S256
- **WHEN** a client uses the implicit grant, the password grant, or the authorisation code without PKCE (S256)
- **THEN** it is refused

#### Scenario: Protected-resource metadata
- **WHEN** a client discovers the MCP server
- **THEN** the server publishes RFC 9728 protected-resource metadata

#### Scenario: Only MCP-audience tokens are accepted
- **WHEN** a token whose audience is not the MCP resource is presented to the MCP server
- **THEN** the server rejects it

#### Scenario: No token passthrough
- **WHEN** the MCP server calls the API for a user
- **THEN** it exchanges the user's token (RFC 8693) for an API-audience token
- **AND** it never sends the MCP token itself to the API

#### Scenario: OAuth server edition without token exchange
- **GIVEN** the chosen OAuth server edition has no token exchange
- **WHEN** the MCP server calls the API for a user
- **THEN** it obtains a separate short-lived API-audience token for the same user and client by the mechanism chosen under open question 40, never the MCP token itself

#### Scenario: API token scopes are the intersection
- **WHEN** an API-audience token is issued to the MCP server
- **THEN** its scopes are the intersection of the granted scopes and the user's current rights

#### Scenario: G4-081 Consent, refresh-token reuse and a role change against the pinned Hydra edition
- **GIVEN** an allowlisted MCP client against the pinned Hydra edition
- **WHEN** the user consents, the client makes a call, uses the same refresh token twice, and then the user's role changes
- **THEN** the MCP-audience token is accepted by the MCP server and rejected by the API
- **AND** the API is reached only with a separate token
- **AND** the reused refresh token revokes the family
- **AND** the next call after the role change is denied within 60 seconds

#### Scenario: G4-082 Resource indicator without audience
- **WHEN** an MCP client sends `resource=https://…/mcp` and no `audience`
- **THEN** the token's audience is the MCP resource (set by the consent app)
- **AND** a token for another resource is rejected with 401

### Requirement: AI-37 Consent and scopes
A consent screen in the web app (after MFA) SHALL show the client name and verified publisher, the requested scopes, the data categories and the expiry, and scopes SHALL map to tool groups and catalogue task types.

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: Consent after MFA shows what is granted
- **WHEN** a user is asked to consent to a client
- **THEN** the consent screen appears in the web app after MFA
- **AND** it shows the client name and verified publisher, the requested scopes, the data categories and the expiry

#### Scenario: Scope names
- **WHEN** scopes are requested
- **THEN** they are `fyniq.read.<area>` (reports, sales, purchases, banking, accounting, planning, compliance), `fyniq.read.proposals` (the status of proposals the client created) and `fyniq.propose.<task_type>`

#### Scenario: Default grant is reports only
- **WHEN** a client is granted its default scopes
- **THEN** the grant is `fyniq.read.reports` only

#### Scenario: Payroll scope is never granted by default
- **WHEN** `fyniq.read.payroll` is requested
- **THEN** it is never granted by default
- **AND** it needs the AI payroll setting and the user's payroll role (AI-02)

#### Scenario: Equity scope is never granted by default
- **WHEN** `fyniq.read.equity` (holders, holdings, instruments, investor records) is requested
- **THEN** it is never granted by default
- **AND** it needs an Owner's grant

#### Scenario: Consent is renewed every 90 days
- **GIVEN** a consent given 90 days ago
- **WHEN** the client is used
- **THEN** the consent must be renewed

### Requirement: AI-38 Token lifetimes and revocation
Access tokens SHALL last at most 60 minutes, and refresh tokens SHALL rotate on every use with reuse detection, an idle expiry of 14 days and an absolute expiry of 90 days.

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: Access token lifetime
- **WHEN** an access token is issued
- **THEN** it expires within 60 minutes

#### Scenario: Refresh token rotation and reuse detection
- **WHEN** a refresh token is used
- **THEN** it is rotated
- **AND** reuse of an already-used refresh token is detected

#### Scenario: Refresh token expiry
- **WHEN** a refresh token has not been used for 14 days, or 90 days have passed since it was first issued
- **THEN** it expires

#### Scenario: Revocation within 60 seconds
- **WHEN** a user's role changes, the user is deactivated, the client is removed or a kill switch is used
- **THEN** all of that user's tokens are revoked within 60 seconds

#### Scenario: Sender-constrained tokens where supported
- **GIVEN** a client that supports DPoP (RFC 9449) [verify client support]
- **WHEN** it is issued tokens
- **THEN** sender-constrained tokens are required

### Requirement: AI-39 Transport
The MCP server SHALL offer the Streamable HTTP transport only, through the load balancer and Cloud Armor; stdio and the legacy HTTP+SSE transport SHALL NOT be offered in production.

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: Streamable HTTP through the load balancer
- **WHEN** a client connects in production
- **THEN** it uses Streamable HTTP through the load balancer and Cloud Armor
- **AND** stdio and the legacy HTTP+SSE transport are not offered

#### Scenario: Origin header checked against an allowlist
- **WHEN** a request arrives with an `Origin` header not on the allowlist
- **THEN** the server rejects it (DNS-rebinding defence)

#### Scenario: Session ID is not authentication
- **WHEN** a session is created
- **THEN** its `Mcp-Session-Id` is random and bound to the token's subject and client
- **AND** it is never accepted as authentication

### Requirement: AI-40 Tool surface
MCP tools SHALL be generated from the shared registry and annotated, and the never-list of §9.6 SHALL be enforced: there are no approve, reject, execute, export, administration or settings tools (control test CTL-MCP-02).

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: Tool annotations
- **WHEN** the tools are listed
- **THEN** read tools are annotated `readOnlyHint: true`
- **AND** `propose` declares that it creates a pending proposal, not a change

#### Scenario: No never-list tools
- **WHEN** the tool surface is checked by control test CTL-MCP-02
- **THEN** there are no approve, reject, execute, export, administration or settings tools

#### Scenario: Definitions are static and hash-pinned per release
- **WHEN** a release is built
- **THEN** the tool definitions are static, versioned and hash-pinned for that release

#### Scenario: A tool change needs the eval and red-team suites
- **WHEN** a tool definition changes
- **THEN** the change needs the eval and red-team suites to pass (CMP-AI-03)

#### Scenario: Tool descriptions hold no organisation data
- **WHEN** a tool description is generated
- **THEN** it contains no organisation data, so it cannot carry injected text

### Requirement: AI-41 Results
MCP results SHALL return structured content, and AI exclusions (AI-02), masking and row caps (AI-04) SHALL be applied before any result leaves the server.

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: Untrusted free text is labelled
- **WHEN** a result contains free text from bank descriptions, emails or documents
- **THEN** that text is labelled as untrusted data

#### Scenario: Exclusions, masking and caps before the result leaves
- **WHEN** a result is returned
- **THEN** AI exclusions, masking and row caps have been applied before it leaves the server

#### Scenario: Figures as typed facts
- **WHEN** a result contains figures
- **THEN** they are returned as typed facts with their provenance (AI-68)

#### Scenario: Responsibility for wording stays with the client
- **WHEN** an external client's model words an answer from Fyniq results
- **THEN** Fyniq is responsible for its tools, scopes and results, not for that wording
- **AND** the agent guide (FR-INT-13) says so and tells clients to cite fact IDs

### Requirement: AI-42 Approvals stay in Fyniq
MCP clients SHALL be able to create proposals and read their status, and approving, rejecting or executing SHALL be possible only in the Fyniq web app or PWA, with step-up where required.

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: Clients create and read proposals
- **WHEN** an MCP client creates a proposal
- **THEN** it can read that proposal's status

#### Scenario: No decision over MCP
- **WHEN** an MCP client tries to approve, reject or execute a proposal
- **THEN** it cannot

#### Scenario: Decisions in the web app or PWA
- **WHEN** a proposal is approved, rejected or executed
- **THEN** it happens in the Fyniq web app or PWA, with step-up where required

#### Scenario: Elicitation never collects approvals, credentials or personal data
- **WHEN** the MCP server uses MCP elicitation
- **THEN** it never uses it to collect approvals, credentials or personal data

### Requirement: AI-43 Limits and kill switches
The MCP server SHALL limit, per client and per user, requests a minute, tool calls an hour, result size, proposals an hour and token budgets, and the organisation kill switch (AI-13) and a per-client kill switch SHALL take effect within 60 seconds.

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: Proposed P1 defaults
- **WHEN** an organisation has not adjusted the limits
- **THEN** each client and each user is limited to 60 requests a minute, 600 tool calls an hour and 30 proposals an hour, with results of at most 500 rows or 256 KB (proposed P1 defaults)

#### Scenario: Limits are adjustable per organisation
- **WHEN** an organisation adjusts its limits
- **THEN** the adjusted limits apply to its clients and users

#### Scenario: Kill switches within 60 seconds
- **WHEN** the organisation kill switch (AI-13) or a per-client kill switch is used
- **THEN** it takes effect within 60 seconds

### Requirement: AI-44 Audit and visibility
Every MCP session, tool call and proposal SHALL be in the AI activity log (AI-12) with the client ID and version, token ID (`jti`), user and IP.

Source: PRD-001 §9.7 · Priority M · Phase P1

#### Scenario: MCP activity is logged
- **WHEN** an MCP session starts, or a tool is called or a proposal created over MCP
- **THEN** it is recorded in the AI activity log with the client ID and version, the token ID (`jti`), the user and the IP

#### Scenario: Connected apps lists each client
- **WHEN** a user opens Settings → Connected apps
- **THEN** each client is listed with its scopes and last use

#### Scenario: Users revoke their own, the Owner revokes any
- **WHEN** a user revokes one of their own connections, or the Owner revokes any connection
- **THEN** the connection is revoked

### Requirement: AI-45 Detection
The system SHALL alert on a new client or new network origin for a user, volume spikes, repeated denied scopes, attempts to reach excluded fields and red-team pattern hits (SEC-31).

Source: PRD-001 §9.7 · Priority S · Phase P1

#### Scenario: New client or network origin
- **WHEN** a user connects through a client or from a network origin not seen before for that user
- **THEN** an alert is raised

#### Scenario: Volume spikes and repeated denied scopes
- **WHEN** request volume spikes, or a client repeatedly requests scopes that are denied
- **THEN** an alert is raised

#### Scenario: Excluded fields and red-team patterns
- **WHEN** a client attempts to reach excluded fields, or its requests hit a red-team pattern
- **THEN** an alert is raised (SEC-31)

### Requirement: AI-46 Third-party clients
Third-party clients SHALL be registered by an administrator, using a Client ID Metadata Document or manual registration, with verified redirect URIs and publisher, and never automatically.

Source: PRD-001 §9.7 · Priority S · Phase P2

#### Scenario: Administrator registration
- **WHEN** a third-party client is registered
- **THEN** an administrator registers it with a Client ID Metadata Document or manually
- **AND** its redirect URIs and publisher are verified

#### Scenario: Never registered automatically
- **WHEN** a third-party client tries to register itself
- **THEN** it is not registered

#### Scenario: Quarterly access review
- **WHEN** the quarterly access review runs (FR-CTL-06)
- **THEN** each third-party client is reviewed

#### Scenario: Data inventory
- **WHEN** a third-party client is registered
- **THEN** it is recorded in the data inventory as a processing path
- **AND** as a transfer where it processes data outside Singapore (CMP-PD-02, CMP-PD-06)

### Requirement: AI-47 Outbound agents and tools
If the assistant calls external MCP servers or agents (e.g. a bank's or payment provider's), it SHALL use only allowlisted servers, read-only by default, with AI exclusions applied to everything sent and every call logged.

Source: PRD-001 §9.7 · Priority C · Phase P3

#### Scenario: Only allowlisted servers
- **WHEN** the assistant would call an external MCP server or agent that is not on the allowlist
- **THEN** it does not call it

#### Scenario: Read-only by default
- **WHEN** the assistant calls an allowlisted external server
- **THEN** the call is read-only by default

#### Scenario: Exclusions and logging
- **WHEN** the assistant sends data to an external server or agent
- **THEN** AI exclusions are applied to everything sent
- **AND** the call is logged

### Requirement: MCP is a thin transport, open by design and closed by default
The MCP server SHALL be a thin transport over the same tool registry the in-app assistant uses, adding no new data path; any MCP-capable agent can be connected, but nothing is connected until an Owner allows it. MCP references follow the current specification's authorisation part (revision 2026-07-28 on 14 Oct 2026; re-checked at P0).

Source: PRD-001 §9.7

#### Scenario: Same tools as the assistant
- **WHEN** an MCP client lists or calls tools
- **THEN** it reaches the same tool registry as the in-app assistant, through no other data path

#### Scenario: Closed until an Owner allows it
- **GIVEN** a new organisation
- **WHEN** an MCP client tries to connect
- **THEN** it is refused until an Owner allows MCP access

#### Scenario: Specification revision checked at P0
- **WHEN** P0 starts
- **THEN** the MCP specification revision in force is confirmed (the 2025-06-18 revision made servers OAuth resource servers with RFC 9728 metadata and RFC 8707 audience binding; the 2025-11-25 revision added Client ID Metadata Documents; the 2026-07-28 revision adds `iss` checks and deprecates dynamic client registration)

#### Scenario: MCP red-team cases
- **WHEN** QA-MCP-01 runs
- **THEN** it includes tool-description injection through organisation data, token replay and audience mix-up, client impersonation, consent phishing, oversized results and session fixation
