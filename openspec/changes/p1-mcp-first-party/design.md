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
- Older clients (2025-06-18, 2025-11-25) use `initialize` and `Mcp-Session-Id`. If the installed Claude Desktop or Claude Code
  still do, a compatibility layer is needed: `initialize` answered, a random session ID bound to the token's subject and
  client ID (a session presented with another token is refused), GET and legacy DELETE handled as the AI-39 text says. This is
  the larger surface; "Decisions needed" 2 asks which eras to serve and task 1.2 first captures what the installed clients
  send, so the answer rests on evidence.
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
- **Verified before built, not assumed (task 1.1):** what Hydra v26.2.0 does with `resource` at authorize and token; whether
  `iss` is emitted; loopback redirect on any port; whether a pre-registered `client_id` can be given to the installed Claude
  Code and Claude Desktop with dynamic registration off (DEV-06 depends on it; if the clients insist on registering, the
  allowlisted-client rule is satisfied by a Client ID Metadata Document, which is P2's, and the decision returns to the owner).

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
  with a platform maximum; lowering is immediate, raising within the maximum is an Owner's change and audited.
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
(`claude mcp add --transport http fyniq-local http://localhost:8080/mcp`). Nothing local reaches staging or production.

## Migration plan

Migrations 000580 to 000599 only (other changes use other ranges; they merge in number order). Each is its own slice's:

| No. | Slice | What |
|---|---|---|
| 000580 | 2.2 | `mcp_user_optin` (organisation, user, opted in at, opted out at), FORCE RLS, the API's grants only |
| 000581 | 8.1 | `mcp_call` (append-only, FORCE RLS), its indexes for the window counts, the export catalogue, the data inventory class |
| 000582 | 8.3 | `mcp_seen` (user, client, network prefix, first seen), for AI-45's new-client and new-origin alerts |
| 000583 to 000599 | | Held back for follow-ups; unused numbers are not reused by others |

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
- **Self-registration is an attack surface** the owner chose to open in P1 (D14): a public endpoint that creates OAuth clients.
  It is closed by default, policy-checked, limited, narrow, pending until approved and expiring; the red-team rows for it are
  part of QA-MCP-01 before the deployment switch is set.
- **Scope map completeness.** Default deny means a forgotten tool is invisible, not exposed; the cost is a missing feature, so
  a test lists every registry tool and says whether it is mapped, and a reviewer reads the diff.
- **Data leaves to the client's model.** Controlled by consent, scopes and the enabling decision, not by Fyniq's router
  (D10); the owner decides how far (Decisions needed 4).
- **Per-call API cost.** Each tool call is an introspection, a delegation (about 20 ms), one internal operation and one write
  of `mcp_call`. Well inside the assistant's budget; measured in 10.1.
- **Time to proof.** The suite needs Hydra in Testcontainers or the in-memory server the P0 tests use. The matrix of
  authorisation rows for the new operations runs only the new rows locally (`FYNIQ_MATRIX_ONLY`).

## Decided (the owner, 19 Nov 2026)

The owner decided the five questions this design asked. They are in PRD-001 rev 0.294 (AI-39, AI-35, new AI-90) and the specs.

1. **One image plus the deployment switch `FYNIQ_MCP_SERVER`** (D1, as built in #840). The shell stays the default.
2. **MCP revision 2026-07-28 only** (D6). A compatibility layer for the earlier handshake (task 4.3) is built only if a real
   client needs it. AI-39's session binding is superseded: an `Mcp-Session-Id` is ignored, never minted; the headers are
   checked against the body.
3. **Delegation stays open question 40's option (b)** (D8), as built; review #4 may still ask for option (c).
4. **An external client's model reads what scope consent and the Owner's dual-controlled enabling allow; the AI exclusions
   always apply** (D10). No separate personal-data setting.
5. **Client registration is built now, in P1 (D14, AI-90)**, instead of waiting for P2 and Client ID Metadata Documents, so a
   client that insists on registering itself (Claude Code's default; Claude Desktop-class connectors) can connect.

Small choices decided here, so none stops the build: limit values as AI-43 proposes (60 a minute, 600 an hour, 5,000 a day, 30
proposals an hour, 500 rows or 256 KB as ceilings); a second factor within the existing step-up window before consent;
`fyniq.read.equity` not offered in P1 (no equity tool exists); the first-party clients are Claude Desktop and Claude Code;
`iss` is not emitted by Hydra v26.2.0 (a SHOULD in the revision in force) and is left for the review pack.

**D14. Client registration (AI-90): Fyniq's own endpoint in front of Hydra's admin API.**
- *The choice.* Three ways to let a client register: (a) enable Hydra's own dynamic registration; (b) fetch a Client ID Metadata
  Document from a URL the client names; (c) a Fyniq-owned RFC 7591 endpoint that validates and policy-checks the request, writes
  Fyniq's own record, then creates the client through Hydra's admin API. **Chosen: (c).** (a) leaves the policy to Hydra, which
  registers whatever its schema allows (our finding: it is off, and has no hook for an Owner's switch, a per-IP limit, a denylist
  or approval). (b) makes the server fetch an attacker-named URL (SSRF, slow-read and size attacks) and the revision's own
  preference for it matters only to clients that implement it; Claude Code 2.1.327 registers with RFC 7591 (design D7's capture).
  (b) stays P2 (AI-46), behind `internal/egress` and its SSRF guards, when third parties arrive.
- *Where.* In the API service (it has the database, the settings and the audit). A public operation `POST /v1/oauth/register` in
  the contract (RFC 7591 request and response; errors are RFC 7591's `invalid_client_metadata` and `invalid_redirect_uri` inside
  a problem with a stable code). The RFC 8414 authorisation-server metadata the client discovers is Fyniq's too: the API serves
  `/.well-known/oauth-authorization-server/oauth` (the issuer's path), built from Hydra's own discovery document plus
  `registration_endpoint`, so a client finds the endpoint; Caddy and the load balancer route both paths to the API.
- *Closed by default, dual control.* A new setting `mcp.registration` (switch, off; turning it on is dual controlled, turning it
  off is immediate). The endpoint answers only while at least one organisation has it on (a SECURITY DEFINER function reads the
  effective settings across tenants, the one cross-tenant read, returning a boolean); otherwise 403 with a stable code. It is also
  refused when the deployment's switch (`FYNIQ_MCP_SERVER`) is off, so the launch shell's closed door stays closed.
- *Policy, in code and in `contracts/ai/mcp-registration-policy.json`.* Public clients only (`token_endpoint_auth_method: none`),
  grants `authorization_code` and `refresh_token`, response type `code`. Redirect URIs, one to five, each **exact**: `https` with a
  host name (no IP literal, no userinfo, no fragment, no wildcard, at most 255 characters), or a loopback address
  (`http://127.0.0.1`, `http://[::1]` or `http://localhost`, where the port alone may vary: RFC 8252, which Hydra matches
  natively, as task 1.1 proved); no other scheme in P1. `client_name`: 1 to 100 printable characters, and never a name that
  impersonates a first-party client or Fyniq (compared after case, spacing and look-alike folding). Anything the server would
  fetch or trust is refused: `jwks_uri`, `jwks`, `request_uris`, `sector_identifier_uri`, `software_statement`, `logo_uri`,
  `client_uri`, `tos_uri`, `policy_uri`; so are `scope` values beyond the default, any other grant, and secrets. A deny list (names,
  redirect hosts) is a contract file reviewed like code. Unknown fields are refused, not ignored.
- *Narrow and pending.* The client is created `agent` class (proposal-only), MCP audience only, scopes `openid`, `offline_access`
  and `fyniq.read.reports`. It is **unverified**: the consent screen says so and shows its name and redirect host, never a verified
  publisher. It cannot connect until an Owner approves it onto the allowlist: `mcp.clients` becomes a list of first-party names
  and registered client IDs (a new kind; adding is dual controlled, removing is at once), each ID checked against Fyniq's record.
  Widening a registered client's scopes beyond reports is an Owner's later, dual-controlled change.
- *Limits.* Per IP: 5 registrations an hour and 20 a day (the edge's per-IP limit for routes that need no organisation, SEC-23,
  plus a counter in the record); at most 50 registrations pending across the installation; a request body of at most 8 KiB. Over a
  limit: 429 with `Retry-After`.
- *Expiry.* A registration no organisation approves within 7 days, or an approved one never used within 30, is removed from Hydra
  and marked expired by a worker job; a removed client's registration is not reusable.
- *Record and audit.* A global table `mcp_client` (client ID, name, redirect URIs, status pending, approved, denied or expired,
  created, last used, the registering address's network prefix); every registration, refusal, approval, denial and expiry is a
  security event with the requesting address, never a secret, and each organisation's approval or denial is also in its own audit.
- *Not decided here, `[live-verify]`:* that Claude Desktop-class connectors register from Anthropic's callback
  (`https://claude.ai/api/mcp/auth_callback`, exact https, which the policy admits) rather than a private-use scheme, which the
  policy refuses in P1.
- *Red-team rows (QA-MCP-01):* registration while closed; a wildcard, a fragment, a userinfo, an IP-literal, a private-use or a
  `javascript:` redirect; two URIs where one is a prefix of the other's host; a secret, a key set, a URL to fetch, a software
  statement; an impersonating name and a look-alike; scopes beyond the default; the sixth registration from one address; the
  fifty-first pending; a registered client using MCP before approval; a registered client's token reaching a tool outside its scopes;
  an expired registration's client; Hydra's own `/oauth2/register` still 404; a registration that tries to make the server call a URL
  (nothing is fetched).

## Open questions

- Claude Desktop: whether it registers with RFC 7591 from Anthropic's callback and which protocol revision it sends; cannot be
  checked on a laptop's local stack (`[live-verify]` with the dev cloud). If it needs a private-use redirect scheme, the policy
  (exact https and loopback only) refuses it until the owner decides otherwise.
- Whether the missing `iss` (RFC 9207) must be fixed before the switch is set: today a SHOULD in the revision in force.
- Every statutory or vendor claim left as `[verify]` in the PRD stays so; nothing here restates one.
