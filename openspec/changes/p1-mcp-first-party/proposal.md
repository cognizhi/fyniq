## Why

Fyniq's assistant answers inside the web app. People also work in Claude Desktop and Claude Code, and PRD-001 §9.7 says
those clients may use the same read tools and the same `propose` tool, as the signed-in person, under the same rules
(AI-15, AI-16). The ground is laid and the door is shut:

- `cmd/mcp` serves `internal/mcpshell`, built for launch (p1-launch-readiness 6.3): it exposes no tool and refuses every
  call with `MCP-OFF-01`, each refusal logged. Its test (`TestTheShellExposesNoToolAndRefusesEveryCall`) is QA-MCP-01 at
  launch.
- The OAuth server is Ory Hydra (Apache-2.0, open question 40 decided: no Enterprise Licence). The login and consent app
  (`internal/oauthflow`) already refuses any flow without PKCE S256, grants only `fyniq.read.<area>`,
  `fyniq.propose.<task_type>` and the `openid`/`offline_access` scopes, derives the client's class from its record, and
  stamps `granted_at` so the API refuses a grant after 90 days. `authn.Introspector` refuses an MCP-audience token at the
  API (`ErrWrongAudience`). `internal/mcpdelegation` is option (b): an RFC 7523 JWT-bearer grant that gives the MCP server a
  5-minute API-audience token for the person behind an MCP token, always agent class (`authn.Delegation`). The P0 spike's
  measurements are in `docs/adr/ADR-T3-mcp-delegation-spike.md`.
- The assistant gave the tools a home: `contracts/ai/tools/` (eight read tools today, more arriving with the knowledge
  base), `internal/aitool`, and the API's `callAITool`, which runs a tool through its public read operation and cuts the
  result to the tool's limits, quotes free text as `untrusted_text`, screens it and returns typed facts. Proposals
  (`p1-ai-proposals-core`) have a catalogue, a store, the approval object and an executor with its own identity.

What does not exist is everything between an MCP client and those pieces: no MCP protocol handling, no resource-server
checks, no scope enforcement (the registry's only scope is `ai.read`; a token's `fyniq.read.*` scopes are copied onto the
principal and nothing reads them), no organisation switch, user opt-in or client allowlist, no pre-registered first-party
clients, no limits or kill switches, no MCP audit, no Connected apps or consent screen, and no red-team suite. This change
builds them, and keeps the launch shell serving until a deployment switch, set after security review #4, turns the real
server on.

The MCP server is a **security boundary** (golden rules 5 and 6): an agent that reaches it is the AI acting for a person,
through a client Fyniq does not control. Nothing here widens a scope, a grant or the never-list; every gate sits in the
API, so that `cmd/mcp` can be stateless and database-free.

## What Changes

- **Closed by default, opened deliberately (AI-35, FR-INT-03).** The setting `features.mcp` (off; turning it on is dual
  controlled, turning it off is immediate), the allowlist `mcp.clients` (only the release's first-party clients can be
  listed; adding is dual controlled, removing is immediate and is the per-client kill switch), a per-user opt-in, and the
  deployment switch that keeps the launch shell in place. Hydra's dynamic client registration is off; Claude Desktop and
  Claude Code are pre-registered public clients with PKCE and fixed loopback redirects.
- **OAuth for MCP (AI-36, AI-37, AI-38).** Consent after a fresh second factor; the `resource` parameter becomes the
  token's audience; scopes narrowed to the default `fyniq.read.reports`; payroll and equity never by default; consent
  renewed at 90 days; access tokens at most 60 minutes, refresh tokens rotating with reuse detection, idle 14 days,
  absolute 90 days; every token of a person revoked on a role change, deactivation, client removal or kill switch, and a
  per-call check so the 5-minute delegated token cannot outlive that by more than 60 seconds.
- **The MCP server (AI-15, AI-39).** `internal/mcpserver` behind `cmd/mcp`: Streamable HTTP, RFC 9728
  protected-resource metadata, 401 and 403 `WWW-Authenticate` challenges, MCP-audience token validation by introspection,
  the Origin allowlist, the protocol revision's header checks, and no stdio and no legacy HTTP+SSE. It holds no database
  access and no write credential.
- **The tool surface (AI-16, AI-17, AI-40).** A generated, hash-pinned manifest (`contracts/ai/mcp-manifest.json`) of the
  registry's tools plus `propose` and the proposal reads, with a scope map that reads a tool's input (so
  `fyniq.read.reports` cannot reach a bill through `records.get`), annotations, no organisation data in descriptions, and
  the never-list enforced at build time and by test (CTL-MCP-02).
- **Reaching the data (AI-16, AI-36, AI-41).** The MCP server gets an API-audience token with option (b) and calls new
  internal operations whose gates (organisation, user, client, scope, limits) run in the API and whose tool path is
  `callAITool`'s: the same read operations, the same AI exclusions, masking, row caps, `untrusted_text` quoting, screening
  and typed facts, then an output filter for NRIC and bank-number shapes.
- **Propose, never approve (AI-16, AI-42).** `propose` for the task types an organisation has on and the client's scopes
  allow, `proposal.get` and `proposal.list` for the person's own, and a test that no approve, reject, execute, export or
  administration operation is reachable by an agent token.
- **Limits, kill switches, audit, detection (AI-43, AI-44, AI-45).** Requests a minute, tool calls an hour and a day,
  proposals an hour, result size, as organisation settings within platform maximums; 429 with `Retry-After`; the
  organisation switch (AI-13) and the per-client switch effective within 60 seconds; `mcp_call` in the AI activity log with
  client and version, `jti`, user and IP; alerts for a new client or network, spikes, repeated denied scopes, excluded-field
  attempts and red-team pattern hits (SEC-31).
- **Screens (AI-44, AI-37; `connected-apps`, `oauth-consent`).** Settings → Connected apps (the organisation switch, the
  allowlist, my connections, revoke, the Owner's revoke of any; integration clients and personal access tokens join it as
  `p1-public-api` built them) and the consent screen, with help, the guide and the knowledge base.
- **QA-MCP-01 and the controls.** The red-team suite grows with each slice and is a merge gate; CTL-MCP-01 to 04 and
  CTL-API-02 run; DEV-06 makes the local `/mcp` work for Claude Code against the sample company; a threat model and the
  security-review pack for review #4.

## Capabilities

### New Capabilities

None: the requirements are in the main specs (`ai/mcp-and-agents`, `ai/guardrails`, `compliance/ai-and-api-controls`,
`integrations/public-api`, `platform/local-mode`). This change builds them (`skip_specs: true`).

### Modified Capabilities

None yet. design.md "Decisions needed" lists three owner decisions; the ones that change the PRD (AI-39's session wording
for the protocol revision in force, and the limit values AI-43 leaves "proposed") become a PRD revision, with the specs
following, when taken.

## Impact

- **Code:** new `internal/mcpserver` (and its use of `internal/mcpdelegation`, `internal/aitool`); `cmd/mcp`; `internal/api`
  (the internal MCP operations, the gates, the Connected apps operations, scope enforcement); `internal/oauthflow`
  (consent gates); `internal/adapters/hydra` and `ports.OAuthServer` (public-client registration, consent sessions, revoke
  one client); `internal/authn` (scope enforcement); `internal/domain/settings`; `internal/adapters/postgres` (the new
  tables).
- **Contracts:** `contracts/openapi/fyniq.v1.yaml` and `fyniq-internal.v1.yaml` (additive), `contracts/ai/mcp-manifest.json`,
  `contracts/ai/mcp-clients.json`, `contracts/ai/mcp-scopes.json`; the regenerated web client.
- **Database:** migrations 000580 to 000599 only (design.md "Migration plan").
- **Deploy:** the `mcp` service gains the API's URL, its service token and the delegation client's settings; Caddy gains the
  well-known route; Hydra's configuration (token lifetimes, rotation, `iss`, no DCR). The cloud parts (KMS key, `private_key_jwt`,
  Cloud Run ingress, Cloud Armor rules) are `[live-verify]`.
- **Screens:** `connected-apps`, `oauth-consent` (both in the `access-ai` canvas; updated locally and republished by the
  owner's design account).
- **Controls and docs:** CTL-MCP-01 to 04, CTL-API-02, the threat model, the `fyniq-mcp-server` skill's status block, the
  user guide and the knowledge base.
- **Breaking:** nothing; every API change is additive, and the launch shell keeps its behaviour until the switch is set.
