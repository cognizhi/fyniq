# Threat model: integration clients

| | |
|---|---|
| Feature and requirement IDs | Integration clients of the public API: FR-INT-02 (client classes, P1 part), SEC-07 (client credentials, networks, expiry), AI-34 (client classes), FR-INT-16 (per-client limits), AUD-01; `p1-public-api` task 3.1: integration clients (17 Nov 2026) and personal access tokens (18 Nov 2026, section 5) |
| Author, reviewer, date | Claude Code for the founders, 17 Nov 2026; reviewer: a founder at sign-off (second human review waived before production, AGENTS.md) |
| Data and its class | Each client's record: its name, class, the member it acts for, its scopes, networks, limits, expiry and the RFC 7638 thumbprint of its public key (internal); its public key and its OAuth client at the OAuth server (internal); what it reads and writes through the API is the organisation's data, at the class of each row (up to confidential) |
| Who acts | An Administrator asks to register a client (C on Connected apps); the Owner approves it with a second approver (FR-INT-02's dual control); an Administrator revokes one. The client itself calls the API with tokens from the OAuth server (Ory Hydra, ADR-T3). The worker deletes a revoked client from the OAuth server. No AI or MCP tool registers, changes or revokes a client |

## 1. What we are building

An integration client is a system that calls the API on its own: OAuth 2.1 client credentials, authenticated with
`private_key_jwt` by the integration's own key, with tokens from the OAuth server. Fyniq holds what the client is to the
organisation (its class, member, scopes, networks, limits and expiry); the OAuth server holds the OAuth client and its
tokens. Trust boundaries:
- the requester → API: a registration is hostile input, its key included;
- the approval → the OAuth server: only an approved request registers a client there;
- the client → the OAuth server → the API: a token is accepted only while Fyniq's record says the client is live;
- the API → the client: what the token may do is its member's roles now, narrowed by its scopes;
- the worker → the OAuth server: a revoked client is deleted there.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| I1 | requester → API | Elevation: one person registers a client with wide scopes and uses it, alone | A standing back door into the books | Medium |
| I2 | requester → API | Tampering: a wildcard or an approve or release scope; a private key sent as the client's key; a client that never expires | A client that approves its own postings, or a leaked secret | Medium |
| I3 | client → API | Spoofing: a client the OAuth server knows but Fyniq never registered, or a token issued to someone else, accepted | An unregistered system acts in an organisation | Low |
| I4 | client → API | Elevation: a client does more than its member may, or keeps acting after its member left or lost a role | Rights beyond any person's | Medium |
| I5 | client → API | Elevation: a token's scopes wider than the record's, or a call from outside the client's networks | Scope creep, or a stolen key used from anywhere | Medium |
| I6 | revocation | Elevation: a revoked or expired client's tokens still accepted | A retired integration keeps reading and writing | Low |
| I7 | client → API | Denial of service: one client floods the API | Slow or refused service for the organisation | Medium |
| I8 | records | Repudiation: a client's changes not attributable to it; a client removed or rewritten after the fact | Who did what is lost | Low |
| I9 | API → database | Tampering: a client's record written directly, in another organisation, or by the worker or the AI | Rules bypassed at the backstop | Low |

## 3. What we do about it

| # | Mitigation | Where | Verified by |
|---|---|---|---|
| I1 | Registration is an approval request of the object `api_client`: an Administrator asks, the Owner approves with a second approver; only then is it registered with the OAuth server, and a rollback deletes it there again | `internal/api/integration_clients.go`, `internal/domain/approval` | `TestIntegrationClients`, the authorisation matrix |
| I2 | The definition is checked whole: narrow `fyniq.read.<area>` and `fyniq.write.<area>` scopes only, never a wildcard, an approve or a release; networks as CIDR prefixes; limits within the platform's; expiry within a year; the public half of an EC P-256 or RSA (2048+) key, never a private key. The database's checks repeat the scope, expiry and key rules | `internal/domain/apiclient`, `internal/authz/scopes.go`, migration 000432 | `TestAnIntegrationClientIsCheckedWhole`, `TestScopesNarrowAClientToWholeAreas`, `TestAttackAnAPIClientKeepsItsRules` |
| I3 | A client's token is accepted only when Fyniq has a live record of its client ID, and only when the token's subject is the client itself; an unknown client falls back to the consent rules, which refuse a client-credentials token | `internal/authn/introspect.go` | `TestTheIntrospectorTakesAnAPIClientFromItsRecord`, `TestIntegrationClients` (the orphan client) |
| I4 | A client's rights are its member's roles read at introspection, never the token's claims; a member who left or holds no role ends it | `internal/authn/introspect.go` | `TestTheIntrospectorTakesAnAPIClientFromItsRecord` |
| I5 | The token's scopes are cut to the record's; the gate refuses an operation the scopes do not cover (`authz.ScopeAllows`) and a call from outside the client's networks, judged on the audited client address | `internal/api/api.go`, `internal/authz/scopes.go` | `TestScopesNarrowAClientToWholeAreas`, `TestIntegrationClients` |
| I6 | Revocation is never held back; the API refuses the client's tokens at once (within the introspection cache's 30 seconds, AI-38) and the worker deletes it and its tokens from the OAuth server; an expired client is refused the same | `internal/authn/introspect.go`, `cmd/worker` | `TestIntegrationClients` |
| I7 | Each client has its own read and write limits within the platform's (FR-INT-16), with `RateLimit` headers and `Retry-After` on 429 | `internal/api/conventions.go` | `TestIntegrationClients` |
| I8 | Every change a client makes is audited as the integration, with its client ID (AUD-01); a client is never removed (FY420), only revoked, once, and kept for the access review | `internal/api`, migration 000432 | `TestIntegrationClients`, `TestAttackAnAPIClientKeepsItsRules` |
| I9 | `FORCE ROW LEVEL SECURITY` on the client table; the global route is read only by `api_client_access` before any tenant is set; the worker and the AI roles write none of it | migration 000432 | `TestAttackAnAPIClientKeepsItsRules` |

## 4. What is left

- The Connected apps screen that lists and revokes clients follows the API; until then an Administrator uses the API.
- Hydra's own client registration and token endpoint in the dev cloud, behind its load balancer [live-verify].

## 5. Personal access tokens (18 Nov 2026)

A personal access token (FR-INT-02) is a developer's own credential: a client of the OAuth server whose secret the server
generates, returns once and keeps only as a hash (`client_secret_basic`, client-credentials grant, audience the API's), so
Fyniq never holds it. The developer exchanges client ID and secret for access tokens of an hour (AI-38). **Decision
recorded (18 Nov 2026, the recommended default):** the shown-once credential is that secret, not a long-lived bearer token,
so access tokens stay within AI-38's 60 minutes and every token comes from the OAuth server (ADR-T3); the alternative, a
90-day bearer token, would break AI-38. Creation is self-service for the member's own token (Appendix D's "Own MCP
connection"), because its class is `agent`: it reads (and proposes, once the proposal operations arrive) and cannot write, whatever its scopes, until the Owner
reclasses it with a second approver (the approval object `api_client`, operation `api_client.reclass`).

| # | Boundary or step | Threat (STRIDE) | Mitigation | Verified by |
|---|---|---|---|---|
| P1 | creation | Elevation: a token, an integration or the AI makes a credential, or a person makes one without step-up | Only a session of a person with a Fyniq user and step-up in the last 5 minutes; the operation is not in the AI or MCP surface and its Appendix D cell is empty for the AI | `TestPersonalAccessTokens`, the matrix rows |
| P2 | creation | Tampering: wide scopes, an expiry beyond 90 days, a wildcard | The same checks as an integration client's, at once; the database repeats the scope and the 90-day rule | `TestAPersonalAccessTokenIsCheckedWhole`, `TestAttackAnAPIClientKeepsItsRules` |
| P3 | secret | Disclosure: the secret in a list, a log, the audit trail or a stored idempotent answer | Shown once in a `Cache-Control: no-store` answer; the command takes no `Idempotency-Key` and no preview (exempt, `TestConventionExemptions`); the record, the lists and the audit event never hold it | `TestPersonalAccessTokens` (lists and the whole trail searched) |
| P4 | use | Elevation: an agent token writes, because its scopes say so, or reads what its member may not | Class `agent` takes the AI column of Appendix D intersected with the member's roles (`authz.Decide`), so a write scope changes nothing until the reclass, and a member who may not view a row never lends it | `TestPersonalAccessTokens` (a draft journal refused before the reclass, created after, audited as the integration), `TestAPersonalAccessTokenNeverExceedsItsMember` |
| P5 | use | Spoofing: a token for another audience, scope or secret | Hydra issues only for the API's audience and the registered scopes, to the secret alone; the API refuses a client it does not know or whose subject is not the client | `TestPersonalAccessTokens` |
| P6 | reclass | Elevation: a person reclasses their own token | An approval request of `api_client` asked by a person with C on Connected apps and approved by the Owner with a second approver, never the requester; it lapses if the token moves on | `TestPersonalAccessTokens` |
| P7 | revocation | A lost laptop: the token keeps working | A member revokes their own at once, an Administrator any; the API refuses it within the introspection cache's 30 seconds and the worker deletes the client from Hydra | `TestPersonalAccessTokens` |
| P9 | creation | Elevation: members mint credentials with no Owner's say | The organisation's switch is off until an Owner turns it on with a second approver; creation is refused without it, in plain words with its code | `TestPersonalAccessTokens` (refused while off and while asked but not approved, made once approved, refused again after the switch is turned off), the matrix row (`pastPolicy`) |
| P8 | records | A member sees or revokes another's token | The own-token operations find only the caller's tokens (not found for another's); the organisation-wide list and revoke need C or V on Connected apps | `TestPersonalAccessTokens`, the matrix rows |

**Owner's decisions (18 Nov 2026, PRD-001 rev 0.299).** (1) Who may make a token: the organisation's setting `security.personal_access_tokens`, off by default, which an Owner turns on with a second approver (golden rule 6, as for MCP and integration clients); while it is off creation is refused with `API-CLIENT-09` (P9); turning it off takes effect at once and stops anyone making another, and tokens already made keep working until they expire or are revoked. (2) A reclassed token keeps its shared secret: **the recorded exception to SEC-07's `private_key_jwt` for integration clients**, behind the `api_client.reclass` approval (the Owner and a second approver); its access tokens still live an hour, its scopes, networks and limits still narrow it, and an Administrator or its member can revoke it at once. The risk accepted: a leaked secret acts as an integration client, within its scopes and networks, until revoked or expired.

Left: the access review (FR-CTL-06, CTL-API-02) lists no clients yet, integration clients and personal tokens alike, until the Connected apps screen (AI-46); a Hydra `client_secret` has no expiry the OAuth server enforces, so Fyniq's record is the authority (the API refuses
an expired token at once); the worker does not yet sweep expired personal clients from Hydra, which hold no access once the
record has expired [live-verify against Hydra in the dev cloud].
