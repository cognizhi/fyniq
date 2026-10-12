---
name: fh-mcp-server
description: Expose Finance Hub tools over the Model Context Protocol safely — the remote MCP server (cmd/mcp) as an OAuth 2.1 resource server behind Ory Hydra, RFC 9728 metadata, PKCE, RFC 8707 audience-bound tokens, RFC 8693 token exchange (never token passthrough), scopes mapped to tools and catalogue task types, the never-list, tool annotations and hash-pinned tool definitions, AI-exclusion filtering of results, streamable-HTTP transport rules (Origin check, session binding), limits, kill switches, audit, client allowlists and the MCP red-team suite (QA-MCP-01). Use whenever you add or change an MCP tool, scope, client or transport setting, connect Claude Code or Claude Desktop to the local stack, touch cmd/mcp, the Connected apps page, Hydra client registration, or are asked "can an agent do X", "make it MCP-ready" or "is the MCP server secure".
---

# The MCP server

The MCP server lets AI clients (Claude Desktop, Claude Code, other agents) use the **same read
tools and the same `propose` tool** as the in-app assistant (PRD-001 rev 0.2 §9.7). It is a
**thin transport over the shared tool registry** (§9.3) and adds no new data path. It is open
by design (any MCP client can be connected) and closed by default (nothing connects until the
Owner allows it).

> **Status:** no code exists yet. This skill states the target design (§9.7, ADR-T3 §12.2.1,
> Appendix F.7a). Commands marked *(P0)* or *(P1)* arrive in that phase. MCP specification
> details are flagged **[verify against the MCP spec revision in force]**: the 2025-06-18
> revision made servers OAuth resource servers (RFC 9728 metadata, RFC 8707 audience binding);
> the 2025-11-25 revision added Client ID Metadata Documents.

## Non-negotiables

1. **Agents are proposal-only.** Every MCP client is client class `agent` (AI-34). No tool can
   write except `propose`, and the API rejects any other write from an agent-class token
   (CTL-MCP-01).
2. **Approvals never happen over MCP** (AI-42). Approve, reject and execute exist only in the
   web app or PWA, with step-up. MCP elicitation is never used for approvals, credentials or
   personal data.
3. **No token passthrough** (AI-36). The MCP server never forwards the client's token. It
   exchanges it for an API-audience token with the intersection of the granted scopes and the
   user's current rights.
4. **Off by default** (AI-35). Off per organisation (Owner, dual-controlled) and per user
   (opt-in). Only allowlisted clients can connect. Dynamic client registration is disabled.
5. **Excluded data never leaves** (AI-41, AI-02). NRIC/FIN, full bank numbers, secrets and
   authentication data are filtered out of results; payroll only with `fh.read.payroll`, the
   organisation's AI payroll setting and the user's payroll role.

## Architecture and paths

| Piece | Path / service | Identity and rights |
|---|---|---|
| MCP server | `cmd/mcp` → Cloud Run `mcp`, routed at `/mcp` | Own service account; **no database access**; reaches data only through the API |
| Tool registry | `contracts/ai/tools/*.yaml` (shared with the in-app assistant; see **fh-ai-proposals**) | Generated into Go types and the MCP `tools/list` output |
| OAuth server | Ory Hydra (`oauth` service, ADR-T3), at `/oauth2` | Issues tokens; the FH API is its login and consent app |
| Consent and Connected apps | `apps/web` Settings → Connected apps (AI-44) | Users revoke their own; the Owner revokes any |
| Audit | AI activity log (AI-12) through the API | — |

## Authorisation flow (AI-36…38)

```
client ──GET /.well-known/oauth-protected-resource──────────────▶ mcp      (RFC 9728 metadata:
                                                                            authorization server, scopes)
client ──authorize?code_challenge=S256…&resource=https://…/mcp──▶ hydra    (PKCE S256, RFC 8707)
hydra  ──login + consent──────────────────────────────────────────▶ web app  (MFA; scopes, data
                                                                            categories, expiry)
client ◀──access token (aud = MCP resource, ≤ 60 min) + refresh (rotating)── hydra
client ──tools/call  Authorization: Bearer …──────────────────────▶ mcp      (validate aud, scope,
                                                                            client class, expiry, jti)
mcp    ──token exchange (RFC 8693, aud = API, scopes ∩ user rights)─▶ hydra
mcp    ──read tool / propose with the exchanged token────────────────▶ api
```

| Rule | Value |
|---|---|
| Grants | Authorisation code + PKCE (S256) only; no implicit, no password grant |
| Access token | JWT, audience = the MCP resource URL, ≤ 60 minutes |
| Refresh token | Rotates on every use with reuse detection; idle 14 days; absolute 90 days |
| Revocation | All of a user's tokens within 60 s on role change, deactivation, client removal or kill switch |
| Sender constraint | DPoP (RFC 9449) when the client supports it [verify client support] |
| Consent | Shown in our web app after MFA; renewed every 90 days; default grant `fh.read.reports` only |
| Client registration | P1: pre-registered first-party public clients, fixed loopback redirect URIs. P2: administrator-registered third parties via a Client ID Metadata Document or manual registration, verified redirect URIs and publisher (AI-46) [verify against the MCP spec revision in force] |

## Scopes ↔ tools ↔ task types

| Scope | Grants | Examples |
|---|---|---|
| `fh.read.reports` | Report and KPI read tools (default) | `report.pl`, `report.balance_sheet`, `kpi.get`, `metrics.query` |
| `fh.read.sales` / `.purchases` / `.banking` / `.accounting` / `.planning` / `.compliance` | Read tools for that area | `invoice.list`, `bill.get`, `bank.unreconciled_lines`, `gl.lines`, `forecast.scenario` |
| `fh.read.payroll` | Payroll reads, **never by default**; also needs the AI payroll setting and the payroll role (AI-02) | `payrun.summary` |
| `fh.propose.<task_type>` | Calling `propose` with that catalogue task type only | `fh.propose.bank.categorise_lines`, `fh.propose.gl.post_journal` |
| `fh.read.proposals` | Status of the user's own proposals | `proposal.get`, `proposal.list` |

The effective permission for each call is the intersection of the scope, the user's current role
permissions minus the AI exclusions, and the organisation's enabled task types (AI-32). A task
type that is off for the organisation is not listed in `tools/list`.

**Never-list (§9.6; enforced by CTL-MCP-02).** No tool may approve, reject or execute anything;
release or initiate payments; create or change payee bank details; change users, roles, team
mode, approval policies, the AI catalogue or AI settings; change statutory parameters; reopen
periods; void or delete posted records; file with IRAS, CPF or ACRA; export the tenant; or touch
the audit trail. The registry build fails if a tool maps to any of these domain commands.

## Tool definitions (AI-17, AI-40)

- Generated from `contracts/ai/tools/` and **static per release**. The build writes
  `contracts/ai/mcp-manifest.json` with a SHA-256 hash per tool definition. The server refuses to
  serve a definition whose hash differs from the manifest. A change needs the eval and red-team
  suites (CMP-AI-03) and shows up in the release notes.
- Annotations: read tools `readOnlyHint: true`, `openWorldHint: false`. `propose` declares that
  it creates a pending proposal, not a change, with `destructiveHint: false` [verify the
  annotation names against the MCP spec revision in force].
- Descriptions never contain organisation data (names, descriptions, notes), so injected text
  cannot travel through `tools/list`.
- Input schemas are strict (`additionalProperties: false`), use decimal strings for money, and
  set limits (row caps, date ranges) that match AI-04.

## Results (AI-41)

1. Fetch through the API with the exchanged token (RLS, masking and the AI exclusions apply).
2. Return structured content. Put untrusted free text (bank descriptions, email bodies,
   document text) in clearly labelled fields such as `untrusted_text`, never in instructions.
3. Apply the output filter for NRIC/FIN and bank-number patterns as a second line of defence.
4. Cap rows and bytes; summarise large results in the tool rather than truncating silently.

## Transport (AI-39)

- Streamable HTTP only, through the load balancer and Cloud Armor. No stdio and no legacy
  HTTP+SSE in production.
- Validate `Origin` against the allowlist (DNS-rebinding defence); reject a missing `Origin`
  from browsers.
- `Mcp-Session-Id` is random, bound to the token's subject and client ID, and never accepted as
  authentication.

## Limits, kill switches, audit (AI-43…45)

AI-43 names the limits but not their values; the numbers below are proposed defaults to confirm
at P1.

| Control | Proposed default |
|---|---|
| Requests per client per user | 60 a minute |
| Tool calls | 600 an hour, 5,000 a day per user |
| Proposals | 30 an hour per user |
| Result size | 500 rows, 256 KB per call |
| Kill switches | Organisation-wide (AI-13) and per client; effective within 60 s |
| Audit fields | Session ID, client ID and version, token `jti`, user, organisation, IP, tool, input hash, output hash, row count, latency, outcome |
| Alerts | New client or network origin for a user, volume spikes, repeated denied scopes, excluded-field attempts, red-team pattern hits (SEC-31) |

Defaults are organisation settings within platform maximums; changing them is audited.

## Testing locally with Claude Code *(P1, DEV-06)*

1. `make setup && make dev` *(P0)*: starts Hydra, the API, the MCP server and the sample
   company behind Caddy.
2. Enable MCP for the sample organisation and opt in your sample user in Settings → Connected
   apps (the local sample has dual control pre-satisfied by a second sample founder).
3. Add the server to Claude Code: `claude mcp add --transport http fh-local http://localhost:8080/mcp`
   (use the port `make dev` prints). Claude Code runs the OAuth flow in your browser.
4. Ask read questions and create a proposal. Check that it appears in the Inbox, that approval is
   only possible in the web app, and that the AI activity log shows the client ID.
5. Never point a local client at staging or production data.

## Threat → control → test (QA-MCP-01, blocks merge)

| Threat | Control | Test |
|---|---|---|
| Agent writes directly | Client class `agent` is proposal-only (AI-34) | Every non-propose write with an agent token → 403 (CTL-MCP-01) |
| Dangerous tool exposed | Registry minus never-list; pinned hashes (AI-40) | Manifest diff; never-list commands absent; altered definition refused (CTL-MCP-02) |
| Token passthrough / confused deputy | RFC 8693 exchange; API checks audience (AI-36) | MCP token sent to the API directly → 401 |
| Audience mix-up, replay | `aud` = MCP resource; `jti` replay cache; ≤ 60 min | Wrong-audience, expired, replayed and revoked tokens → 401 |
| Stale rights after role change | Revocation within 60 s (AI-38) | Change role → next call denied within 60 s (CTL-MCP-04) |
| Tool-description injection | Static descriptions with no organisation data | Injection strings seeded in bank lines and notes never appear in `tools/list` |
| Prompt injection via results | `untrusted_text` labelling; catalogue and approval bound damage | Red-team corpus (QA-AI-03) through MCP: 0 out-of-catalogue proposals |
| Sensitive-data exfiltration | AI-02 exclusions, masking, output filter | NRIC and bank-number patterns never in results; payroll unreachable without scope + setting + role |
| DNS rebinding / CSRF | `Origin` allowlist | Bad or missing `Origin` → 403 |
| Session hijack / fixation | Session bound to subject and client | Session ID reused with another token → rejected |
| Consent phishing / rogue client | Allowlist, no DCR, verified redirects (AI-35, AI-46) | Unknown client ID and unregistered redirect URI → rejected |
| Denial of wallet | Limits and budgets (AI-43) | Exceeding limits → 429 with `Retry-After`; kill switch → all calls denied within 60 s |

## Commits

`feat(mcp): …`, `fix(mcp): …`, `feat(oauth): …`; tool-definition changes also touch
`contracts` and must say which evals ran.

## Definition of done

- [ ] The tool or scope is defined in `contracts/ai/`, mapped to a scope, and absent from the
      never-list; the manifest hash is updated.
- [ ] Results pass through the API with an exchanged token; exclusions and the output filter apply.
- [ ] QA-MCP-01, QA-AI-01…03 (`make evals` *(P0)*) and the authorisation matrix are green.
- [ ] Limits, audit fields and the Connected apps view cover the change.
- [ ] Spec-dependent details checked against the MCP spec revision in force, and the result is
      noted in the pull request.
