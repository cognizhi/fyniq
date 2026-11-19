## Context

What the repository holds today (read on 19 Nov 2026, main at `5823cbf4`):

- **The shell.** `cmd/mcp` serves `internal/mcpshell`: every request under `/mcp`, whatever its method, gets 403 and the
  RFC 9457 problem `MCP-OFF-01`, logged as `mcp.call_refused` without the credentials or the body. Its test is QA-MCP-01 at
  launch (`// req: QA-MCP-01`, `// asvs: V8.2.1, V16.3.2`). The plan gates this change on security review #4 and puts it after
  go-live (build plan wave 6).
- **OAuth.** Hydra Apache-2.0, opaque access tokens (`STRATEGIES_ACCESS_TOKEN: opaque`), `TTL_ACCESS_TOKEN: 1h`,
  `TTL_REFRESH_TOKEN: 336h` (14 days, rotation on use) in `deploy/local/compose.yaml`. `internal/oauthflow` is the login and
  consent app: S256 PKCE only, audiences only the API's or the MCP's and only those the client's record allows, scopes
  `fyniq.read.<area>` (reports, sales, purchases, banking, accounting, planning, compliance, proposals),
  `fyniq.propose.<type>`, class from the client's record, `granted_at` in the token's claims (the API refuses a grant after
  90 days), consent never remembered. It refuses a request that names **no audience**: an MCP client sends `resource`, not
  `audience` (G4-082), so today a real MCP client's flow is refused.
- **The API side of a token.** `authn.Introspector` refuses an MCP-audience token (`ErrWrongAudience`, no passthrough).
  `authn.Delegation` accepts a token from the MCP server's delegation client, always `agent` class, at most 5 minutes
  (`MaxDelegated`), the person's organisation and roles read at use. `authn.Principal.Scopes` carries the token's scopes
  and **nothing reads them for an agent**: `authn.go` says "mapping scopes to operations arrives with the scope catalogue in
  P1". `authz.Decide(policy, roles, p.AI())` applies the AI's column of Appendix D to every agent-class principal.
- **Delegation, option (b).** `internal/mcpdelegation.Delegator`: introspect the MCP token, sign an ES256 assertion with the
  MCP server's key (issuer `urn:fyniq:mcp`, one minute, a `jti`), exchange it at Hydra's token endpoint as the delegation
  client for the API audience, refuse a token that lives over 5 minutes. Measured at 18 to 21 ms per call. Known limits
  (ADR-T3 spike): whoever holds the key and the client's credentials can mint for any person within the agent class; the
  delegated token cannot be revoked early (it has no consent session), so 5 minutes is the window; a role change applies at
  once because the API reads roles on every use.
- **Tools.** `contracts/ai/tools/*.json` (calc, checks.explain, checks.list, lineage.explain, metrics.query, packs.get,
  records.get, records.search; `p1-knowledge-base` adds kb.*), `internal/aitool` (strict schemas, read-only annotations,
  limits capped at `MaxRows = 50`, `MaxTimeoutMS = 5000`, `MaxResultTokens = 4000`, the single scope `ai.read`, a version
  per tool in `contracts/ai/tools.lock.json`). `callAITool` (`internal/api/ai_tool_calls.go`) runs a tool through its public
  read operation in process with the caller's bearer token, so roles, masking and the AI exclusions (`p.AI()`) apply, then
  cuts to the limits, quotes free text as `untrusted_text`, screens it (fail closed) and returns typed facts. It is an
  **internal** operation: the AI service's service token plus a turn's or a run's `fyai_` grant, and it records the turn's
  data classes (`widenTurn`) for model routing.
- **Proposals.** `internal/proposal` (catalogue, which ships empty; a test-only command exercises the path),
  `POST /internal/v1/proposals` for a turn's token, the `ai_proposal` store, the approval object `ai_proposal` and the
  executor with its own role. `ai.task_types` (dual controlled) says which types are on.
- **Settings.** `internal/domain/settings` holds `DualControl` and `OffAtOnce` flags; `features.ai_assistant` is the pattern
  (on needs a second approver, off is immediate). `settings.Narrows` knows switches and task-type lists only.
- **Integration clients.** `registerIntegrationClients` (`internal/api/integration_clients.go`), operations
  `ListIntegrationClients` and the rest under the `ConnectedApps` capability; no Connected apps page exists in the web app
  yet, and no `ports.OAuthServer` method lists or revokes one client's consent.
- **Controls.** `compliance/controls/CTL-MCP-01..04.yaml` and `CTL-API-02.yaml` exist as registry rows whose checks `arrives:
  "p1-mcp-first-party"`.
- **The MCP specification has moved.** The current revision is **2026-07-28** (modelcontextprotocol.io/specification/
  versioning, read 19 Nov 2026), not the 2025-11-25 the `fyniq-mcp-server` skill and the PRD's earlier text cite. It
  **removes protocol-level sessions and the GET stream**, requires an `MCP-Protocol-Version` header and mirrored
  `Mcp-Method` and `Mcp-Name` headers (a mismatch is `400` with `-32020 HeaderMismatch`), makes `server/discover` a
  mandatory RPC, requires `Origin` validation (a present, invalid `Origin` is 403), says a server that supports only this
  revision should answer GET and DELETE with 405 and ignore `Mcp-Session-Id`, deprecates dynamic client registration, prefers
  Client ID Metadata Documents, has the authorisation server include `iss` (RFC 9207) and the client check it, and defines
  403 `insufficient_scope` challenges with `WWW-Authenticate`. Earlier revisions used `initialize` and `Mcp-Session-Id`.
  AI-39 ("`Mcp-Session-Id` is random and bound to the token's subject and client") was written for the earlier shape.

## Goals / Non-Goals

**Goals:**
- An MCP client acts as the signed-in person, through the same tool registry and the same API read operations as the
  in-app assistant, as an `agent`-class caller that can only read and propose.
- Closed by default at four levels: the deployment, the organisation, the client and the person.
- Every gate in the API, so `cmd/mcp` is stateless, has no database, no write credential and no provider key.
- A suite (QA-MCP-01) that proves each control, grown slice by slice and a merge gate throughout.
- Claude Code connects to the local sample company with the same OAuth flow (DEV-06).

**Non-Goals:**
- Third-party clients, Client ID Metadata Documents, administrator registration of other publishers, DPoP, third-party MCP
  clients in the quarterly access review (AI-46: P2, `p2-api-third-parties`).
- Outbound MCP (AI-47: P3).
- Any approve, reject, execute, export, administration or settings tool (the never-list, §9.6): never, in any phase.
- MCP elicitation and sampling (AI-42: never for approvals, credentials or personal data; the server offers neither
  capability).
- Cloud configuration that cannot be proved offline: the KMS signing key, `private_key_jwt` for the delegation client, Cloud
  Run ingress and the Cloud Armor rule for `/mcp` (`[live-verify]`).
- The security review itself (#4). This change prepares its pack and does not claim its sign-off.

## Decisions

**D1. The launch shell stays until a deployment switch turns the server on (AI-35, golden rule 6).**
- `cmd/mcp` keeps serving `mcpshell`. A new setting of the deployment, `FYNIQ_MCP_SERVER` (`off` by default, in every flavour),
  makes it serve `internal/mcpserver` instead. The two never run together: `mcpshell`'s test keeps passing unchanged, and a
  second test proves that with the switch off the new package is not even reached.
- The switch is the operator's, set after security review #4 is signed. It is distinct from `features.mcp` (an Owner's
  decision for an organisation) so that nobody enabling a setting in an app can open an endpoint the operator has not.
- Alternative: a second image for the real server, so the shell image has no server code at all. Stronger, one more image to
  budget, sign and scan (§12.9); see "Decisions needed" 1.

**D2. All gates live in the API; `cmd/mcp` is a stateless transport (AI-16, AI-36, AI-43).**
- `internal/mcpserver` does what only a resource server can: terminate HTTP, speak JSON-RPC, validate the bearer token's
  audience by introspection, check `Origin` and the protocol headers, and build `tools/list` from the pinned manifest. For
  each `tools/call` it asks the delegator for an API token and calls one internal operation with its own service token
  (`ServiceMCP`, a new class beside `ServiceAI`) and that token. It holds no state that matters across requests, so Cloud Run
  may run any number of instances.
- The API does the rest, per call, in one transaction: the organisation's `features.mcp`, the person's opt-in, the client on
  `mcp.clients` and not killed, the person still an active member, the grant not past 90 days, the scope for this tool and
  this input, the limits, the audit row. A refusal is a problem with a stable code (ENG-23) that the server turns into the
  MCP result or HTTP status the revision defines. Because every call asks, a switch, a removal from the allowlist, a role
  change or an opt-out take effect on the next call (well inside 60 seconds, AI-43), even though a delegated token cannot be
  revoked. Hydra's `RevokeSubject` and a consent revocation still run, for the MCP token itself.
- Alternative: keep the policy in `cmd/mcp` with its own cache. Rejected: a second copy of the rules that can be stale, in a
  process that must not have a database, and the exact thing the red-team suite would then have to test twice.

**D3. Reaching the data: new internal operations on `callAITool`'s code (AI-16, AI-41).**
- `/internal/v1/mcp/tool-calls`, `/internal/v1/mcp/proposals` and `/internal/v1/mcp/proposal-reads` (names settled when the
  contract is written), authenticated by `ServiceMCP` plus the option-(b) token, which `authn.Delegation` already turns into
  an agent-class principal with the person's roles. They call the same `checkToolInput`, `runTool` and `screenResult` as
  `callAITool`, so there is one data path. They do **not** call `widenTurn`: there is no turn, and MCP data never goes through
  Fyniq's model router; it goes to the client's own model, which is the point of the data-class decision (D10).
- The internal listener's authentication gains a second accepted caller; `runAgentModel`'s exception (the worker with its own
  token) is the precedent for a per-operation rule, and a test proves each service token opens only its own operations.
- Alternative A: let the MCP server call the public `/v1` read operations itself with the delegated token and shape results
  in `cmd/mcp`. This duplicates the tool mapping, the field allow-lists (`recordFields`), the limits and the screening in a
  process outside the reviewed API contract. Rejected.
- Alternative B: the MCP server sends the MCP token to the API for exchange (our own RFC 8693). Removes the any-subject
  weakness of option (b) but sends the MCP-audience token to the API, which AI-36 says never happens, and reverses the
  founders' decision of 16 Oct 2026. Not taken; named in "Decisions needed" 3 so the security review can ask for it.

**D4. Scope enforcement reads the tool and its input, from a map in a manifest (AI-37, AI-40).**
- The registry's single scope `ai.read` cannot express `fyniq.read.reports` versus `fyniq.read.sales`, and `records.get`,
  `records.search` and `metrics.query` cross areas. `contracts/ai/mcp-scopes.json` maps each tool, and for the generic ones
  each value of the input that selects the area (`records.get.kind`, `records.search.kind`, `metrics.query.measure`), to one
  scope. A tool or a value with no entry is **not exposed and not callable** (default deny); a new tool is invisible to MCP
  until a reviewer maps it. The map is data under `contracts/`, so a tool's own file and `tools.lock.json` do not change,
  and the knowledge-base tools arriving in #828 need no edit to land.
- The API checks it per call against the token's scopes (`Principal.Scopes`, the intersection Hydra granted), returns 403
  `insufficient_scope` with the scope needed, and the server sends the revision's `WWW-Authenticate` challenge. `tools/list`
  shows a person only the tools their scopes allow and the organisation's enabled task types allow (AI-32).
- `fyniq.read.payroll` is refused at consent unless `ai.payroll_access` is on and the person has a payroll role (AI-02), and
  is re-checked per call. P1 has no payroll or equity read tool, so neither scope maps to a tool yet; `fyniq.read.equity`
  needs an Owner's grant and is not offered at consent in P1.

**D5. The manifest and the never-list (AI-17, AI-40, CTL-MCP-02).**
- `contracts/ai/mcp-manifest.json` is generated (a Go test with `-update`, reviewed in the pull request like the OpenAPI
  contract): for each exposed tool its name, `title`, description, input schema, annotations (`readOnlyHint: true`,
  `openWorldHint: false` for reads; for `propose`, `readOnlyHint: false`, `destructiveHint: false`, a description saying it
  creates a pending proposal and changes nothing), its scope and the SHA-256 of its canonical JSON. The server loads the
  manifest embedded in the binary and refuses to start, and refuses to serve a tool, whose definition's hash differs.
  The manifest changes only through the same pull request that changes a tool, and the release notes name it (CMP-AI-03).
- Descriptions are static text from `contracts/`, generated with no organisation data; a test seeds injection strings into
  every organisation text field of the sample company and proves none reaches `tools/list` (AI-40).
- The never-list is a build gate: the generator fails if a tool maps to a command or operation that approves, rejects,
  executes, releases or initiates a payment, changes payee bank details, users, roles, team mode, approval policies, the AI
  catalogue or settings, statutory parameters, periods, voids or deletes posted records, files with a regulator, exports the
  tenant or touches the audit trail. It reuses the catalogue loader's never-list (`internal/proposal`), which already matches
  the words of a command and refuses by operation, so there is one list.

**D6. Transport: Streamable HTTP, by the revision in force, with the older shape as an option (AI-39).**
- Serve the 2026-07-28 shape: POST only, one JSON-RPC message per request, `application/json` answers (an SSE answer only if
  a tool ever streams, none does), `MCP-Protocol-Version` and the mirrored headers validated against the body with `-32020`,
  `server/discover`, `tools/list`, `tools/call`, `ping`; GET and DELETE answer 405; `Mcp-Session-Id` is ignored and never
  minted; `Origin` present and not on the allowlist is 403 (a missing `Origin` from a non-browser client is allowed, as the
  revision requires validation of a present header; a browser fetch always sends one). The allowlist is the deployment's own
  origins plus nothing else; native clients send none.
- Authentication is per request: the bearer token on every POST, never a session ID. That makes AI-39's "session ID is not
  authentication" true by construction for this revision, and the session-fixation red-team case becomes "a stale
  `Mcp-Session-Id` is ignored and cannot widen anything".
- Older clients (2025-06-18, 2025-11-25) use `initialize` and `Mcp-Session-Id`. Claude Code 2.1.327 tries the modern shape
  first (task 1.2), so a compatibility layer is not needed for it. If Claude Desktop, or a later finding, needs one: `initialize` answered, a random session ID bound to the token's subject and
  client ID (a session presented with another token is refused), GET and legacy DELETE handled as the AI-39 text says. This is
  the larger surface; "Decisions needed" 2 asks which eras to serve and task 1.2 captured what the installed Claude Code
  sends, so the answer rests on evidence for it; Claude Desktop is still open.
- stdio and the legacy HTTP+SSE endpoints are not offered (nothing is mounted); Cloud Armor and the load balancer sit in front
  in the cloud `[live-verify]`.

**D7. Authorisation: the metadata, the challenges and the consent app (AI-36, AI-37, AI-38).**
- `GET /.well-known/oauth-protected-resource` (and the path-suffixed form for `/mcp`) returns RFC 9728 metadata: `resource` =
  the MCP URL, `authorization_servers` = Hydra's issuer, `scopes_supported` = the minimal set (`fyniq.read.reports`),
  `bearer_methods_supported: ["header"]`. A request without a valid token gets 401 and `WWW-Authenticate: Bearer
  resource_metadata="…"`, with `scope` naming what the call needs; a token with too few scopes gets 403 `insufficient_scope`
  with the scope. Caddy and the cloud load balancer route the well-known path to the `mcp` service (today Caddy sends only
  `/mcp*` there).
- Consent (`oauthflow.Decide`), extended: for the MCP audience it refuses unless `features.mcp` is on for the person's
  organisation, the person has opted in, the client is on `mcp.clients`, the client's class is `agent`, the requested scopes are
  within the default plus what the person asks for (never payroll without the setting and the role, never equity in P1), and
  the person passed a second factor within the step-up window (`Principal.StepUpAt`; the consent page shows nothing before
  it). It maps `resource` to the audience when the request carries no `audience` (G4-082), and refuses a `resource` that is not
  the MCP URL exactly. The consent screen JSON gains the publisher (from `contracts/ai/mcp-clients.json`), the data categories
  each scope reaches and the expiry; consent stays un-remembered, so every authorisation shows it, and renewal at 90 days
  follows (the existing `granted_at` rule).
- Hydra: access tokens at most 60 minutes, refresh tokens rotating with reuse detection (a reused token revokes its family),
  idle expiry 14 days, absolute 90 days; the authorisation response carries `iss` (RFC 9207, if Hydra v26.2.0 supports it:
  verified in task 1.1); dynamic client registration off. The first-party clients are registered by a new
  `ports.OAuthClients.RegisterPublicClient`: `token_endpoint_auth_method: none`, `authorization_code` and `refresh_token`
  grants only, the MCP audience only, class `agent`, loopback redirect URIs (any port, per RFC 8252, if Hydra allows it:
  task 1.1) from `contracts/ai/mcp-clients.json`.
- **Verified against the pinned Hydra v26.2.0 (task 1.1, `TestHydraAndTheParametersAnMCPClientSends`, 19 Nov 2026):**
  - `resource` is **not** turned into an audience: the login and consent requests have empty requested audiences and the
    request URL carries `resource`. The consent app reads it and sets the audience itself (G4-082); the token request may
    repeat `resource` and is accepted; the issued token's audience is exactly what consent granted.
  - A loopback redirect registered without a port matches on any port for the IP addresses `127.0.0.1` and `[::1]` only
    (RFC 8252), **not for `localhost`**, which Claude Code asks for (`http://localhost:<port>/callback`): a `localhost`
    redirect is registered with its port (corrected 19 Nov 2026; the first assertion proved nothing). A different path never
    matches.
  - Dynamic client registration is off (`POST /oauth2/register` answers 404).
  - Hydra emits **no `iss`** in the authorisation response and its metadata does not set
    `authorization_response_iss_parameter_supported`. The revision in force makes `iss` a SHOULD for the authorisation server
    and has a client proceed when both are absent; it says a later revision is expected to make it a MUST. Not an owner decision
    now; a gap for the security-review pack, and the test fails when an upgrade starts emitting it so the consent app can rely
    on it. A fix, if the MUST arrives, is Hydra's, or a thin proxy in front of it.
- **Verified against the installed Claude Code 2.1.327 (task 1.2, a recording stub on 127.0.0.1, 19 Nov 2026):**
  - It sends `POST /mcp` `server/discover` first, with `MCP-Protocol-Version: 2026-07-28` and `Mcp-Method: server/discover`,
    and the `_meta` of the revision (client info and capabilities, including elicitation, which this server never offers).
    Only after that failed did it fall back to `initialize` with `protocolVersion: 2025-11-25`; so a server that answers
    `server/discover` correctly is spoken to in the modern shape.
  - It looks for `/.well-known/oauth-protected-resource/mcp` (the path-suffixed form, with `MCP-Protocol-Version: 2025-11-25`),
    then `/.well-known/oauth-authorization-server` and `/.well-known/openid-configuration`, and, with no client ID given, tries
    dynamic registration (`POST /register`, `application_type: native`, redirect `http://localhost:3118/callback`).
  - A pre-registered client works: `claude mcp add --client-id <id> --callback-port <port> …`, or `oauth: {clientId,
    callbackPort}` in a `--mcp-config` file; with it the client did not try to register. DEV-06 therefore needs no dynamic
    registration: the local instructions give the client ID and a fixed port the registered redirect allows.
  - It notes "SEP-2352 isolation" (issuer-stamped credentials), another reason Hydra's missing `iss` is on the review list.
  - **Not verified: Claude Desktop.** Its remote servers are added as connectors that sign in from Anthropic's side with
    Anthropic's callback URL and need a publicly reachable `/mcp`, which a local stack is not; whether a pre-registered client ID
    is accepted there, and with which redirect, is `[live-verify]` and goes to the owner as an open question below.

**D8. Delegation is Q40 option (b), carried with its conditions (AI-36).**
- Wire `mcpdelegation.Delegator` behind a signer port: a local ES256 key in the secret store locally; Cloud KMS
  `EC_SIGN_P256_SHA256` in the cloud `[live-verify]`. The delegation client authenticates with a secret locally and with
  `private_key_jwt` in the cloud (SEC-07) `[live-verify]`. The trust relationship is registered at start-up locally and by
  OpenTofu in the cloud `[live-verify]`.
- Conditions the founders accepted on 16 Oct 2026 become tasks: an alert on **every** delegated token (AI-45, via the audit
  row each call writes); the delegator introspects the MCP token before each assertion (already so); the per-call gates of D2
  close the 5-minute window for everything except a token already past its checks. The QA-MCP-01 suite and review #4 can still
  reject option (b), and option (c) changes only `internal/mcpdelegation`.
- A "jti replay" red-team case applies to the **assertion** (one use, refused twice: already proved in P0) and to refresh
  tokens (reuse revokes the family). An access token is a bearer credential presented on every call; rejecting its second use
  would break the protocol. The skill's "replayed token" row is read that way and the skill corrected.

**D9. Limits, kill switches, audit, detection in the API (AI-43, AI-44, AI-45).**
- Settings (`internal/domain/settings`): `features.mcp` (switch, off, DualControl, OffAtOnce), `mcp.clients` (a new kind,
  choices from the release's client list; adding is DualControl, removing is `OffAtOnce`, so removal is the per-client kill
  switch and `Narrows` learns the kind), `mcp.requests_per_minute` (60), `mcp.tool_calls_per_hour` (600),
  `mcp.tool_calls_per_day` (5,000), `mcp.proposals_per_hour` (30), `mcp.result_rows` (500) and `mcp.result_kb` (256), each
  with a platform maximum; as the assistant's limits are, each is dual controlled and audited.
- Counters come from `mcp_call` itself (window counts per user and client, the way the assistant counts turns), checked in the
  same transaction that writes the row, so any number of MCP instances share one truth. Over a limit: 429 with `Retry-After`,
  recorded as an outcome. The tools' own caps (50 rows, 4,000 tokens) are lower than `mcp.result_rows`; the setting is a
  ceiling, and a result is cut at the lower, with what was left out counted (AI-04).
- `mcp_call` (append-only, FORCE RLS): organisation, user, client ID and version, the token's `jti`, the delegated token's `jti`,
  tool or operation, input hash, output hash, rows, latency, outcome, IP and time, one row per request that reaches a gate,
  including every refusal. It joins the AI activity log (AI-12; `listAIActivity` gains a class). Each insert is audited
  (AUD-01) in the same transaction, as the actor `ai` on behalf of the person.
- Detection: a user's first use of a client or of a network prefix (`mcp_seen`), a spike over the person's own trailing
  average, three denied scopes in a window, an excluded-field attempt (the exclusion policy already knows when it filters) and
  a screening or red-team pattern hit are security events (SEC-31) and a notice to the Owner (the notifications service).

**D10. What an external client may read: data classes (AI-02, AI-75, AI-83, PDPA).**
- Inside Fyniq, `aipolicy` routes a turn's model calls by the data classes the turn has read (business, personal, restricted,
  know-how), so personal data never reaches a model the policy does not allow. An MCP client is outside that router: its
  model belongs to the person's own plan. The never-send fields (NRIC/FIN, bank numbers, secrets, authentication data) are
  filtered in the API for every agent-class principal and again by the output filter, so they never leave. What can leave is
  business data and, in `records.*`, the names of customers, vendors and employees-as-contacts that the AI's field allow-list
  already passes.
- Recommended default: the Owner's dual-controlled enabling of `features.mcp`, the person's opt-in and the scope grant at
  consent are the controls; the consent screen names the data categories; the data inventory gains the MCP path as a
  processing path (CMP-PD-02). See "Decisions needed" 4.

**D11. Propose and proposal reads (AI-16, AI-42, AI-34).**
- Each `fyniq.propose.<type>` scope maps to one `propose_<type>` tool, listed only while the organisation has the type on
  (`ai.task_types`), the release has its command, and the person's role may request it. The call goes to the same proposal
  service as the assistant's, with the MCP principal as the originator and the client ID recorded; the dry run, impact,
  base versions and limits are the service's. `proposal.get` and `proposal.list` (scope `fyniq.read.proposals`) show only the
  person's own. The catalogue ships empty, so the suite proves the path with the test-only command the assistant's proposals
  used, and every real type arrives with its own change.
- An agent token reaching `approve`, `reject`, `execute` or any non-proposal write is refused by the API whatever scopes it
  holds (CTL-MCP-01), and the nightly sample of agent-class writes is zero. Elicitation and sampling are not declared in the
  server's capabilities.

**D12. Connected apps and consent are screens on API operations (AI-44, AI-37; golden rule 4).**
- Operations (additive, in the public contract, the web app's generated client only): the organisation's MCP state and its
  change (an approval for enabling and for adding a client), the person's opt-in, `GET /v1/me/connected-apps` (each
  connection: client, scopes, granted at, expires, last use from `mcp_call`) and its revoke, the Owner's list and revoke of
  any. They need `ports.OAuthServer` to list a subject's consent sessions and revoke one client's (new methods; the adapter
  uses Hydra's admin `consent/sessions`).
- The integration clients and personal access tokens that `p1-public-api` built appear on the same page; their rows are not
  rebuilt here.

**D13. Local mode (DEV-06, golden rule 9).** `make up` registers the first-party clients and the delegation client, generates
the delegation key, serves `/mcp` and the well-known path behind Caddy, and the sample company has `features.mcp` on in a
local profile with the second approver pre-satisfied, as the skill describes. A script drives the whole OAuth flow in a test
without a browser (the existing `oauth_test` helpers), and a `docs/runbooks/` page says how to connect Claude Code
(`claude mcp add --transport http --client-id claude-code --callback-port 3118 fyniq-local http://localhost:8080/mcp`; the first-party client's redirect is `http://localhost:3118/callback`, exact). Nothing local reaches staging or production.

## Migration plan

Migrations 000580 to 000599 only (other changes use other ranges; they merge in number order). Each is its own slice's:

| No. | Slice | What |
|---|---|---|
| 000580 | 2.2 | `mcp_user_optin` (an append-only row per choice, the latest in force), FORCE RLS, the API's grants only |
| 000581, 000582 | 2.2 | The settings table's value check lets `mcp.clients` be empty and up to 4,000 characters (`NOT VALID`, then validated), so the per-app kill switch can take off the last app |
| 000583 | 8.1 | `mcp_call` (append-only, FORCE RLS), its indexes for the window counts, the export catalogue, the data inventory class |
| 000584 | 8.3 | `mcp_seen` (user, client, network prefix, first seen), for AI-45's new-client and new-origin alerts |
| 000585 to 000599 | | Held back for follow-ups; unused numbers are not reused by others |

The settings need no migration (the catalogue is code). Goldens are updated with
`go test -tags local ./internal/adapters/postgres -run TestSchemaSnapshot -update`; `make db-lint` runs on each.

## Slices and the gates each carries

1. Facts and the closed doors (1.x, 2.x): the spike's findings written down, the settings, the opt-in. The shell is untouched.
2. OAuth (3.x): first-party clients, consent gates, revocation. The first QA-MCP-01 rows (unknown client, unregistered
   redirect, no PKCE, wrong audience, consent phishing).
3. The server core (4.x) behind the deployment switch, with the Origin, header and token rows.
4. The tool surface and the API path (5.x, 6.x): the manifest, the scope map, the delegation wiring, the internal operations.
5. Propose (7.x), then limits, audit and detection (8.x), then the screens (9.x).
6. The suite assembled, the controls running, local mode, the threat model and the review pack (10.x).

Each slice leaves `make test` green, adds its QA-MCP-01 rows to the one suite file, and changes nothing the shell's test
asserts while the deployment switch is off.

## Risks / Trade-offs

- **Option (b)'s any-subject weakness** stands as the founders accepted it. Mitigations are tasks, not promises: the per-call
  gates, the alert on every delegated token, the key behind a port with KMS `[live-verify]`, and the suite. If review #4
  rejects it, option (c) replaces one package.
- **Spec drift.** The revision in force already differs from the PRD's and the skill's text. The change records the revision
  it was built against, re-checks it before the switch is set, and pins the headers it validates in tests.
- **A client that cannot be pre-registered** (insists on dynamic registration or on a Client ID Metadata Document) would make
  the allowlist depend on P2 work. Task 1.1 finds out first.
- **Scope map completeness.** Default deny means a forgotten tool is invisible, not exposed; the cost is a missing feature, so
  a test lists every registry tool and says whether it is mapped, and a reviewer reads the diff.
- **Data leaves to the client's model.** Controlled by consent, scopes and the enabling decision, not by Fyniq's router
  (D10); the owner decides how far (Decisions needed 4).
- **The MCP service reaches Hydra's admin API.** Introspection is an admin operation in Hydra, so the service that checks
  tokens needs the admin address, and a compromised MCP process could otherwise create clients or trust relationships. The
  code narrows it (the server takes an `Introspector`, not the whole `OAuthServer`; the egress guard allows that host only);
  the network must narrow it too: locally the Compose network, in the cloud a route to `/admin/oauth2/introspect` alone
  `[live-verify]`. Alternative if that route cannot be made: validate the token through an internal API operation that
  introspects, which sends the MCP token to the API for that one check and no other.
- **Per-call API cost.** Each tool call is an introspection, a delegation (about 20 ms), one internal operation and one write
  of `mcp_call`. Well inside the assistant's budget; measured in 10.1.
- **Time to proof.** The suite needs Hydra in Testcontainers or the in-memory server the P0 tests use. The matrix of
  authorisation rows for the new operations runs only the new rows locally (`FYNIQ_MATRIX_ONLY`).

## Decisions needed

Each has a recommended default, which the build uses until the owner decides; a different decision changes only the slice named.

1. **How the real server is kept off after launch** (slice 3).
   - **A. One image, a deployment switch `FYNIQ_MCP_SERVER`, default off** (recommended): the shell's behaviour and test are
     untouched; the operator sets it after review #4. Costs: the server's code ships in the image while off, so the guarantee
     is a flag, covered by a test and the red-team suite.
   - B. Two images (`mcp` shell, `mcp-server`): no server code in the launch image, so "the server is not there" is literal.
     Costs: a second image to budget (`deploy/docker/budgets.json`), sign, scan and deploy, and a routing change at the switch.
   - C. Replace the shell now. Rejected: it contradicts the plan's gate on review #4 and the launch task's promise.
2. **Which MCP protocol revisions the server speaks** (slice 4; changes AI-39, so a PRD revision).
   - **A. 2026-07-28, and the earlier handshake revisions as a compatibility layer only if Claude Desktop (or a later finding)
     needs them** (recommended): Claude Code 2.1.327 was captured speaking the modern shape first (task 1.2), the modern,
     sessionless shape is the smaller surface, and the compatibility layer (a bound `Mcp-Session-Id`, as AI-39 says) is built
     only on evidence.
   - B. 2026-07-28 only: smallest surface and AI-39's session wording replaced by per-request authentication; may exclude
     clients that have not moved.
   - C. Both always: widest reach, the largest surface to attack and to keep in step.
   - Either way the PRD revision rewrites AI-39 to say what is true for each revision served.
3. **Delegation mechanism** (slice 4; no change unless the owner asks). Option (b) is decided (rev 0.25) and is built. The
   security reviewer may prefer option (c) (Keycloak with RFC 8693, a second OAuth server to run) or our own exchange endpoint
   (D3, alternative B), which removes the any-subject weakness at the cost of sending the MCP token to the API. No decision now;
   named so review #4 has the options and the cost in one place.
4. **How far an external client's model may read personal data** (slice 6, the consent text and the scope map).
   - **A. Scope consent plus the Owner's enabling is enough, the consent screen names the data categories, the data inventory
     records the path** (recommended): matches how an Owner already decides AI and keeps one switch.
   - B. A separate Owner setting `mcp.personal_data` (off by default) that removes the `records.*` tools and the
     party-dimension metrics from MCP until on: safer default, a second setting to explain, a thinner first release.
   - C. No personal-class tool over MCP until a DPIA is complete: strictest, delays most of the value.

Small choices decided here, so none stops the build: limit values as AI-43 proposes (60 a minute, 600 an hour, 5,000 a day, 30
proposals an hour, 500 rows or 256 KB as ceilings); a second factor within the existing step-up window before consent;
`fyniq.read.equity` not offered in P1 (no equity tool exists); first-party clients are Claude Desktop and Claude Code only;
`iss` is checked by clients, so the authorisation server emits it when Hydra can.

## Open questions

- Claude Desktop: how it connects to a remote MCP server (an Anthropic-hosted connector with a public URL and Anthropic's
  callback, or a local bridge), whether it accepts a pre-registered client ID, and which protocol revision it sends. Cannot be
  checked on a laptop's local stack; `[live-verify]` with the dev cloud. If it needs dynamic registration or a Client ID Metadata
  Document, the first-party allowlist cannot include it until `p2-api-third-parties` builds those, and the owner decides
  whether P1 ships with Claude Code alone.
- Whether the missing `iss` (RFC 9207) must be fixed before the switch is set: today a SHOULD in the revision in force.
- Every statutory or vendor claim left as `[verify]` in the PRD stays so; nothing here restates one.
