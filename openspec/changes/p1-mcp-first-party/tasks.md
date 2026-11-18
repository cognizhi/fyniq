## 1. Verify before building

Each slice is its own pull request; each passes `make check` and its own tests, adds its rows to the one QA-MCP-01 suite and
leaves the launch shell's test (`TestTheShellExposesNoToolAndRefusesEveryCall`) unchanged. Migration numbers are in design.md
(000580 to 000599).

- [x] 1.1 Hydra's facts, against the pinned v26.2.0 on the local stack (the in-memory Hydra of `internal/api/oauth_test.go`):
  what it does with `resource` at the authorisation and the token request, whether the response carries `iss`, whether a
  loopback redirect on any port matches a registered one, that dynamic client registration is off, and that a public client
  registered with `token_endpoint_auth_method: none` completes the flow. Each answer is a test (`// req: AI-36`,
  `// req: AI-35`, `// asvs: V10.4.1`) and a line in design.md "Open questions". If Hydra cannot emit `iss` or accept the
  redirect, the gap and the options go to the owner.
  Built: `TestHydraAndTheParametersAnMCPClientSends` (`internal/api/oauth_mcp_facts_test.go`). `resource` is not an audience;
  loopback redirects match on any port for `127.0.0.1` and `localhost`; dynamic registration answers 404; Hydra emits no `iss`
  (a SHOULD in the revision in force; recorded in design D7 and left for the review pack).
- [x] 1.2 What the installed clients send: Claude Code and Claude Desktop pointed at a recording stub on the local stack,
  capturing `MCP-Protocol-Version`, the methods, whether `initialize` and `Mcp-Session-Id` appear, and how each obtains a
  `client_id` (pre-registered, dynamic, Client ID Metadata Document). The versions and the capture go in the pull request
  and in design.md; they decide Decisions needed 2 and whether task 4.3 is built (DEV-06). A client that cannot use a
  pre-registered ID is a finding for the owner, not a workaround.
  Built (the capture is in design D7): Claude Code 2.1.327 sends `server/discover` (2026-07-28) first and falls back to
  `initialize` (2025-11-25) only after a failure, looks for the path-suffixed protected-resource metadata, and accepts a
  pre-registered client ID with a fixed callback port. **Left:** Claude Desktop, which cannot be run against a local stack
  (design "Open questions"); task 4.3 stays conditional.

## 2. Closed by default (AI-35, FR-INT-03)

- [x] 2.1 The settings (design D9): `features.mcp` (off, dual controlled on, off at once), `mcp.clients` (a new kind;
  choices from `contracts/ai/mcp-clients.json`; adding dual controlled, removing at once, `settings.Narrows` taught the kind),
  and the limit settings with their platform maximums; guide links and reasons. Tests `// req: AI-35`, `// req: AI-43`,
  `// req: CTL-MCP-03` (a second approver is needed to enable and to add a client; removal is immediate).
  Built: `features.mcp` (off, dual controlled, off at once), `mcp.clients` (a `Choices` setting over `claude-code` and
  `claude-desktop`, empty, adding dual controlled, removing at once: `Narrows` and the catalogue test learned the kind), six
  limit settings at AI-43's defaults with platform maxima (dual controlled, like the assistant's limits; a new `Unit` field
  makes the messages say rows or kilobytes), the guide sections and the compiled knowledge-base pages. `mcp.clients` lists
  `claude-desktop` only because the release will pre-register it; whether Claude Desktop can connect at all is design
  "Open questions". Nothing reads the settings yet: the gates that do arrive with tasks 3.2 and 6.2.
- [ ] 2.2 Migration 000580 `mcp_user_optin` (FORCE RLS, the API's grants only, the export catalogue, goldens, `make db-lint`),
  the storage port and adapter, and `PUT /v1/me/mcp` and `GET /v1/me/mcp` for a person's own opt-in, audited. Tests
  `// req: AI-35`, an attack test that another person's opt-in cannot be set (`// asvs:`).
- [x] 2.3 The deployment switch (design D1): `cmd/mcp` serves `mcpshell` unless `FYNIQ_MCP_SERVER=on`, in every flavour and in
  the Compose profile; with it off the new package is not reached, and the shell's test and image budget are unchanged.
  Tests `// req: QA-MCP-01`, `// req: AI-35`.
  Built: `FYNIQ_MCP_SERVER` (`mcpserver.Enabled`: unset or `off` is the shell, `on` is the server, any other value stops the
  service from starting); `cmd/mcp`'s `routes` chooses, and its test shows the shell answers (and the server's metadata is
  not even routed) for unset, off and an empty value. The Compose profile passes it through, off.

## 3. OAuth for MCP (AI-36, AI-37, AI-38)

- [ ] 3.1 The first-party clients: `contracts/ai/mcp-clients.json` (Claude Desktop and Claude Code: publisher, redirect
  patterns, class `agent`), `ports.OAuthClients.RegisterPublicClient` and the Hydra adapter, registration at local start-up,
  Hydra's configuration (no dynamic registration, token lifetimes, rotation with reuse detection, absolute 90 days, `iss` as
  task 1.1 found). Tests `// req: AI-35` (no DCR, a fixed redirect), `// req: AI-36` (no implicit, no password grant, no
  PKCE-less code), `// req: AI-38` (lifetimes, rotation, reuse revokes the family, idle and absolute expiry).
- [ ] 3.2 The consent gates (design D7): `oauthflow.Decide` for the MCP audience refuses unless the organisation's switch is
  on, the person opted in, the client is allowlisted and `agent`-class, a second factor is fresh, the scopes are within the
  default plus the person's ask, payroll needs `ai.payroll_access` and a payroll role, equity is not offered; `resource`
  becomes the audience and must equal the MCP URL; the consent JSON gains the publisher, the data categories per scope and the
  expiry. Tests `// req: AI-37`, `// req: AI-36` (G4-081, G4-082), `// req: AI-02`, `// req: AI-35`; red-team rows for an unknown
  client ID, an unregistered redirect URI and consent phishing.
- [ ] 3.3 Revocation (AI-38): `ports.OAuthServer` lists a subject's consent sessions and revokes one client's; a role change,
  deactivation, client removal from `mcp.clients`, the organisation switch off and the person's opt-out each revoke the
  person's MCP tokens (`RevokeSubject` or the one client). Tests `// req: AI-38` (every token within 60 seconds),
  `// req: CTL-MCP-04` (a role change denies the next call within 60 seconds), G4-081 end to end.

## 4. The server core (AI-15, AI-39), behind the deployment switch

- [x] 4.1 `internal/mcpserver`: Streamable HTTP by the revision in force (design D6): one POST per message, the protocol and
  mirrored headers validated with `-32020`, `server/discover`, `tools/list`, `tools/call`, `ping`, GET and DELETE 405,
  `Mcp-Session-Id` ignored, `Origin` validated, request size and time limits, no stdio and no HTTP+SSE mounted, no elicitation
  or sampling capability. Tests `// req: AI-39`, `// req: AI-42`, `// asvs: V13.4.1`; red-team rows for a bad `Origin`, a
  header-body mismatch, an oversized body and a stale session ID.
  Built: `internal/mcpserver` speaks revision 2026-07-28 only: POST with one message each, `server/discover`, `tools/list`
  (the manifest's tools that the token's scopes allow), `tools/call` (to a `Backend`), `ping`; `initialize` is answered with
  the revision served (`-32022`); the headers are checked against the body (`-32020`, with the Base64 sentinel decoded); a
  present, unlisted `Origin` is 403 before the token is looked at; GET, DELETE and the rest are 405; a session ID is ignored
  and never minted; a batch, an oversized body, a non-JSON type and a token in the URL are refused; and no log line holds a
  token or arguments. `internal/neverlist` now holds the never-list that `internal/proposal` and the tool surface share, so
  `cmd/mcp` links no domain command (`TestTheMCPServiceCarriesNoDatabaseCode`, in both flavours). **Left:** the tool call's
  data path is `Unavailable` (503) until task 6.2.
- [x] 4.2 The resource server (design D7): RFC 9728 metadata at the well-known path, 401 and 403 `WWW-Authenticate`
  challenges with the needed scope, token validation by introspection for the MCP audience only (an API-audience token, an
  expired, revoked or unknown one, and the delegated token itself are refused), the Caddy route for the well-known path, and a
  `cmd/mcp` that has no DSN. Tests `// req: AI-36`, `// req: AI-39`; red-team rows for the wrong audience, an expired token, a
  revoked token and the MCP token sent to the API directly (401).
  Built: RFC 9728 metadata at both well-known paths (resource, issuer, `scopes_supported` the default grant only), the 401
  and `insufficient_scope` 403 challenges, `OAuthVerifier` (introspection on every call; audience exactly the MCP resource,
  so a token that is also good for the API is refused; class `agent`; consent within 90 days), the Caddy route for the
  well-known path, and `TestTheMCPServerChecksATokenHydraIssued` against the pinned Hydra (a real token lists its tools; an
  API-audience, integration-class, both-audience and revoked token do not). **Left:** the MCP-to-API token (task 6.1) and
  the cloud route that lets this service introspect and do nothing else with Hydra's admin API (`[live-verify]`; design
  Risks).
- [ ] 4.3 (Only if task 1.2 shows an installed client still needs it.) The earlier revisions' compatibility layer: `initialize`,
  a random `Mcp-Session-Id` bound to the token's subject and client, a session refused with another token. Tests `// req:
  AI-39`; the red-team session-fixation row.

## 5. The tool surface (AI-16, AI-17, AI-40)

- [x] 5.1 `contracts/ai/mcp-scopes.json` (design D4) and the generator for `contracts/ai/mcp-manifest.json` (a Go test with
  `-update`): exposed tools with annotations, scope and SHA-256; default deny for an unmapped tool or input value; the
  never-list as a build gate on the generator, from the catalogue loader's list. Tests `// req: AI-40`, `// req: AI-17`,
  `// req: CTL-MCP-02` (planted never-list commands fail the build; every registry tool is listed as mapped or not).
  Built: `internal/mcpsurface` (`Build`, `Verify`, `Scopes.Needs`), `contracts/ai/mcp-scopes.json` and the generated
  `contracts/ai/mcp-manifest.json` (`go test ./internal/mcpsurface -update`). Seven of the registry's eight tools are exposed,
  with scopes read from the input: `metrics.query` and `lineage.explain` by measure and by dimension (customer needs
  `fyniq.read.sales`, vendor `fyniq.read.purchases`, account `fyniq.read.accounting`), `records.get` and `records.search` by
  kind (searching needs a kind: the manifest makes it required), the checks and `packs.get` by `fyniq.read.compliance`.
  Not exposed in P1: `calc` (it runs in the AI service), contacts and expense claims (personal data), and every kind or
  measure not named: a test requires each enum value to be mapped or listed unexposed with its reason. The never-list is
  applied to a tool's verb, not its name, because the catalogue loader's words include `packs`, which would refuse the read
  tool `packs.get`; the gate also refuses a tool that is not read-only and closed-world, that runs outside the API, a write
  verb, and any scope outside the read areas (payroll, equity and the propose scopes cannot be mapped). **Left for 7.x:** the
  `propose_<type>` and proposal-read tools join the manifest with their own rule (a task type is refused by
  `proposal.Refuse` on its ID and command).
- [ ] 5.2 `tools/list` from the embedded manifest, per person (their scopes, the organisation's enabled task types), refusing
  to start or to serve a tool whose definition's hash differs; descriptions free of organisation data. Tests `// req: AI-40`,
  `// req: AI-17` (a version per change; every tool's behaviour against its description and schema), `// req: CTL-MCP-02`
  (an altered definition is refused), the injection strings seeded in every organisation text field never in `tools/list`.
- [ ] 5.3 Scope enforcement in the API (design D4): `authn.Principal.Scopes` read for agent-class callers by the MCP
  operations, 403 `insufficient_scope` with the scope needed, the person's rights and the AI exclusions still applying on top.
  Tests `// req: AI-37`, `// req: AI-16`; red-team rows: `fyniq.read.reports` cannot reach a bill or a journal through
  `records.get`, a payroll tool is unreachable without scope, setting and role (AI-02).

## 6. Reaching the data (AI-16, AI-36, AI-41)

- [ ] 6.1 The delegation wired (design D8): the signer port with a local ES256 key in the secret store, the delegation client
  and trust relationship registered at local start-up, `mcpdelegation.Delegator` used by `mcpserver`, an audit row for each
  delegated token. KMS and `private_key_jwt` are `[live-verify]`. Tests `// req: AI-36` (the MCP token never reaches the API,
  scopes are the intersection, an assertion used twice is refused), `// req: AI-45` (an alert on every delegated token).
- [ ] 6.2 `ServiceMCP` and the internal operations (design D3): `/internal/v1/mcp/tool-calls` and the proposal operations, their
  contract in `fyniq-internal.v1.yaml`, the per-call gates in one transaction (organisation, opt-in, client, active member, grant
  age, scope, class `agent`), one stable problem code per refusal, and a matrix test that each service token opens only its own
  operations. Tests `// req: AI-35`, `// req: AI-34`, `// req: CTL-MCP-01` (every non-propose write with an agent token is 403),
  the authorisation matrix rows (`FYNIQ_MATRIX_ONLY`).
- [ ] 6.3 Results (design D3, AI-41): the tool path of `callAITool` shared, not copied (`checkToolInput`, `runTool`,
  `screenResult`); `untrusted_text` labelling, the NRIC and bank-number output filter, rows and bytes capped at the lower of the
  tool's and the organisation's setting, what was left out counted, figures as typed facts. `tools/call` end to end against the
  sample company. Tests `// req: AI-41`, `// req: AI-16` (the same figures and exclusions as the assistant for the same
  person), `// req: AI-20`; red-team rows for NRIC and bank-number patterns, oversized results and injection text in results.

## 7. Propose and nothing else (AI-16, AI-42)

- [ ] 7.1 `propose_<type>` tools for the types on (`ai.task_types`) that the person's `fyniq.propose.<type>` scopes and role
  allow, relayed to the proposal service with the client recorded; proven with the test-only command, since the catalogue ships
  empty. Tests `// req: AI-16`, `// req: AI-42`, `// req: AI-34`.
- [ ] 7.2 `proposal.get` and `proposal.list` (scope `fyniq.read.proposals`), the person's own only. Tests `// req: AI-42`.
- [ ] 7.3 Approvals stay in Fyniq: an agent token cannot approve, reject, execute or reach any never-list operation, through
  MCP or the API, whatever scopes it holds. Tests `// req: AI-42`, `// req: CTL-MCP-01`, `// req: ai/guardrails External agents
  and MCP clients cannot abuse identity or tools`; the red-team corpus (QA-AI-03) through MCP: zero out-of-catalogue proposals.

## 8. Limits, audit, detection (AI-43, AI-44, AI-45)

- [ ] 8.1 Migration 000581 `mcp_call` (append-only, FORCE RLS), one row per gated request with its refusals, audited in the
  same transaction (AUD-01); the AI activity log lists them with the client and version, `jti`, user and IP. Tests
  `// req: AI-44`, `// req: AI-12`, `// req: AI-16` (audited as the assistant's calls are), an attack test that the table
  cannot be updated or deleted (`// asvs:`).
- [ ] 8.2 The limits (design D9): requests a minute, tool calls an hour and a day, proposals an hour, result size, from
  `mcp_call` in the gate's transaction; 429 with `Retry-After`. Tests `// req: AI-43`.
- [ ] 8.3 Kill switches and detection: the organisation switch (AI-13) and removing a client take effect within 60 seconds;
  migration 000582 `mcp_seen`; alerts for a new client or network origin, a volume spike, repeated denied scopes, an
  excluded-field attempt and a red-team pattern hit, as security events (SEC-31) and an Owner's notice. Tests `// req: AI-43`
  (a switch denies every call within 60 seconds, measured), `// req: AI-45`, `// req: CTL-MCP-04`.

## 9. Screens and help (AI-44, AI-37)

- [ ] 9.1 The Connected apps operations (design D12): the organisation's MCP state and its change as approval objects, the
  person's connections, revoke, the Owner's revoke of any; the contract, the generated client, the authorisation matrix rows.
  Tests `// req: AI-44`, `// req: AI-35`, `// req: CTL-API-02` (integration clients listed and registered here).
- [ ] 9.2 Settings → Connected apps (`connected-apps`): the switch, the allowlist with per-client state and last use, my
  connections, revoke, the integration clients and personal access tokens `p1-public-api` built; Basic and Expert; states for
  off, not allowlisted and needs a second approver; Vitest with axe; the screen's fragment updated locally.
  Tests `// req: AI-44`.
- [ ] 9.3 The consent screen (`oauth-consent`): client, publisher, class (proposal-only), requested scopes in plain language,
  data categories, expiry, Allow and Deny, shown only after the second factor; the refused states. Tests `// req: AI-37`.
- [ ] 9.4 Help keys, `docs/user-guide/` pages, tour step (the first-run path does not include MCP; the page is linked from AI
  settings), compiled into `kb/` (`fyniq-knowledge-base`), and the agent guide's MCP section (FR-INT-13: Fyniq answers for its
  tools and results, not an external model's wording; cite fact IDs). Tests `// req: AI-41`, `// req: QA-UI-05`.

## 10. The suite, the controls, local mode and the review pack

- [ ] 10.1 QA-MCP-01 assembled from the rows each slice added, run in `make test` and its own CI job, and the per-call cost
  measured against the P0 spike (about 20 ms of delegation). Every red-team case of design D8 and the specification is a named
  test: tool-description injection through organisation data, wrong audience, expired, revoked and replayed (assertion and
  refresh) tokens, client impersonation, consent phishing, oversized results, session fixation, a bad `Origin`, payroll
  unreachable, limits and kill switches. Tests `// req: QA-MCP-01`, `// req: AI-36`.
- [ ] 10.2 `CTL-MCP-01` to `CTL-MCP-04` and `CTL-API-02` running in `compliance/controls/` with their evidence (the nightly
  sample of agent-class writes is zero; the manifest and never-list check; the dual-control check on enabling and allowlisting;
  the revocation timing test), and the control map's counts. Tests `// req: CTL-MCP-01` to `CTL-MCP-04`, `// req: CTL-API-02`.
- [ ] 10.3 Local mode (design D13): `make up` registers the clients and the delegation client, serves `/mcp` and the
  well-known path behind Caddy, the local profile enables MCP for the sample company; `docs/runbooks/` says how to connect
  Claude Code and Claude Desktop; the flow is driven in a test with no browser. Tests `// req: DEV-06`, `// req: AI-15`.
  `[live-verify]`: a human connecting the installed clients through a browser.
- [ ] 10.4 The threat model as its own pull request (as #832 did for the knowledge base), the ADR recording the delegation
  conditions and the protocol revisions served, and the security-review (#4) pack: the suite's report, the manifest, the
  limits of option (b) and what stays `[live-verify]`. The deployment switch is **not** set.
- [ ] 10.5 After the owner's decisions: a PRD revision (AI-39's wording for the revisions served, the limit values AI-43
  leaves proposed, the delegation mechanism's final record), the specs and the plan's tables in step, `prdspec.py stamp`,
  `make roadmap`; the `fyniq-mcp-server` skill's status block, its revision, its token and session rows and its limits table
  brought up to the built server.
