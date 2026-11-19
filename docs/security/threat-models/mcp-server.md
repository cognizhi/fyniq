# Threat model: the MCP server and client registration

| | |
|---|---|
| Feature and requirement IDs | The remote MCP server for first-party clients and the registration of AI apps: AI-15 to AI-17, AI-34 to AI-45, AI-90, FR-INT-03, DEV-06, QA-MCP-01, CTL-MCP-01 to 04 (PRD-001 rev 0.294; `p1-mcp-first-party`) |
| Author, reviewer, date | Claude Code for the owner, 19 Nov 2026. Security review #4 is the reviewer; the deployment switch `FYNIQ_MCP_SERVER` is **not** set until it signs |
| Data and its class | What the assistant's read tools return as the person: business figures and records (confidential business data), with the AI exclusions applied (no NRIC or FIN, no full bank number, no secret, no payroll unless enabled and the person's right) |
| Who acts | An AI app (Claude Code, Claude Desktop, or one that registered itself) for a person; the person; an Owner; the MCP service; the API; the OAuth server (Hydra) |

## 1. What we are building

A person connects an AI app to Fyniq. The app discovers the MCP server's protected-resource metadata (RFC 9728), signs the
person in at Hydra with the authorisation code and PKCE S256, and Fyniq's own login and consent app (after a fresh second factor)
grants a token for the MCP server alone (RFC 8707) with only the scopes the person allows. The app then calls `server/discover`,
`tools/list` and `tools/call` over Streamable HTTP, revision 2026-07-28, one POST each. The MCP service checks the token by
introspection on every call, shows the tools the token's scopes allow from a hash-pinned manifest, and for a call obtains a
5-minute API-audience token for the person (open question 40, option (b), RFC 7523, signed by its own key), and calls the API's
one MCP operation with its own service token. The API checks the delegated token, the scope each tool and input needs, the
organisation's switch, the allowlist and the person's opt-in, applies limits, runs the tool as the person (AI exclusions and masking),
filters and caps the result, and records the call. The only write over MCP is a proposal (task 7.x, not built: see section 6).

Apps can also register themselves (AI-90): a closed, policy-checked endpoint of Fyniq's own in front of Hydra's admin API.

Trust boundaries: the internet or the person's machine ↔ the MCP service; MCP service ↔ Hydra (introspection, token endpoint) ↔ API;
API ↔ database. The MCP service has no database, no write credential and no provider key.

## 2. Threats, controls and tests (STRIDE)

The test column names the row of `compliance/qa-mcp-01.json`, which `internal/archtest` keeps true.

| # | Threat | Control | QA-MCP-01 row |
|---|---|---|---|
| M1 | Elevation: an app writes directly | Agent class is proposal-only; the MCP operation runs read tools; the MCP binary has no write path | agent-direct-write, database-access |
| M2 | Elevation: a dangerous tool is exposed | The manifest is the registry minus the never-list, default-deny, generated and hash-pinned | dangerous-tool-exposed |
| M3 | Tampering: a tool definition is changed after review | SHA-256 per definition, verified before serving | tool-definition-altered |
| M4 | Tampering: injected text reaches the model through `tools/list` | Definitions are static contract text; no organisation data | tool-description-injection |
| M5 | Spoofing: the MCP token is replayed at the API (confused deputy) | Audience-bound; the API refuses it; a separate delegated token | token-passthrough |
| M6 | Spoofing: wrong audience, expired, revoked, or good for both audiences | Introspection on every call; audience exactly the MCP resource; agent class; consent within 90 days | audience-mix-up |
| M7 | Spoofing: the delegation assertion is replayed or widened | One use; scopes intersected by the trust relationship and the API | assertion-replay |
| M8 | Elevation: a role change, opt-out or removal leaves access | Every gate asked on every call; an opt-out revokes the app's tokens | stale-rights |
| M9 | Elevation: a scope reaches more than it names | The scope is read from the tool and its input; payroll and equity never granted | scope-widening |
| M10 | Information disclosure: an NRIC, bank number or payroll leaves | AI exclusions in the API, the output filter, per-call scope and setting | sensitive-exfiltration |
| M11 | Denial: an oversized result | A ceiling per organisation; refused, not cut | oversized-result |
| M12 | Spoofing: DNS rebinding | Origin validated before the token; POST only | dns-rebinding |
| M13 | Tampering: a header disagrees with the body | The revision's validation (-32020) | header-mismatch |
| M14 | Spoofing: session fixation | No sessions in the revision served; an ID is ignored | session-fixation |
| M15 | Spoofing: consent phishing, an unknown client or redirect | Registered clients, exact redirects, a fresh second factor, every gate | consent-phishing |
| M16 | Elevation and denial: self-registration abuse | Closed by default; policy; limits by network; pending until an Owner approves; expiry | registration-abuse, no-dynamic-registration-at-hydra |
| M17 | Denial of wallet | Limits per minute, hour and day from the trail | denial-of-wallet |
| M18 | Repudiation and tampering: the trail is changed | Append-only table; the API's role cannot change it | trail-tampering |
| M19 | Elevation: the server answers before it is meant to | The deployment switch; the launch shell is the default | opened-by-accident |

SOD and money: nothing here moves money or approves anything; approving is never an MCP operation (AI-42).

## 3. Residual risks the reviewer should weigh

1. **Option (b)'s any-subject weakness (ADR-T3).** Whoever holds the MCP service's key and its delegation client's credentials can
   obtain an API token for any person within the agent class and that person's current rights. Accepted by the founders on 16 Oct 2026
   with conditions: the key in Cloud KMS and `private_key_jwt` in the cloud (**not wired; `[live-verify]`**, so a release build refuses
   to start the server), an audit row per call, and the per-call gates. The delegated token is not revocable early (5 minutes).
   Alternatives: option (c), or Fyniq's own exchange endpoint, which sends the MCP token to the API.
2. **The MCP service reaches Hydra's admin API** for introspection, which is also the API that creates clients and trust
   relationships. Locally the Compose network narrows it; in the cloud it needs a route that allows introspection alone
   (**`[live-verify]`**). The code uses the narrow `Introspector` type and an egress guard to that host.
3. **Hydra emits no `iss`** (RFC 9207) in the authorisation response: a SHOULD in the revision in force, a MUST expected later.
4. **Self-registration is an open endpoint by the owner's decision** (AI-90). Mitigations above; a registered client is unverified, sees
   the default read scope, and needs an Owner's dual-controlled approval. The consent screen (task 9.3) must show its name and
   redirect host and never a verified publisher; until that screen exists, the consent data carries both.
5. **Data goes to the app's own model** (decided 19 Nov 2026: scope consent plus the Owner's dual-controlled enabling; the AI
   exclusions always apply). It is a processing path for the data inventory (CMP-PD-02) and a transfer where the app's model is outside
   Singapore (CMP-PD-06): to be recorded when the switch is set.
6. **Claude Desktop is unverified**: whether it registers with RFC 7591, from which callback and with which protocol revision.

## 4. What is `[live-verify]` and not in this model's tests

The load balancer and Cloud Armor in front of `/mcp`, the KMS key and `private_key_jwt`, Cloud Run ingress, the introspection-only route,
the `X-Fyniq-Client-IP` header's trust in the cloud, and a person connecting the installed clients through a browser.

## 5. Decision log

Decisions of 14 and 19 Nov 2026 (PRD-001 rev 0.277 and 0.294): one image plus a deployment switch; revision 2026-07-28 only; option (b);
scope consent plus Owner enabling; self-registration built now. Design: `openspec/changes/p1-mcp-first-party/design.md`.

## 6. Not built yet: proposals over MCP

`propose` and the proposal reads (task 7.x) are not built. The catalogue ships empty, so there is nothing to propose, and a proposal
made over MCP has no live turn or run to authenticate its requester, which AI-89's rules for who may sign a tier-1 proposal rest on.
That needs an owner decision (see design.md "Decisions needed") before it is built; until then an MCP client can read and cannot write.
