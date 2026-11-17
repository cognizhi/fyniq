# Threat model: integration clients

| | |
|---|---|
| Feature and requirement IDs | Integration clients of the public API: FR-INT-02 (client classes, P1 part), SEC-07 (client credentials, networks, expiry), AI-34 (client classes), FR-INT-16 (per-client limits), AUD-01; `p1-public-api` task 3.1, in part (personal access tokens follow) |
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
| I2 | The definition is checked whole: narrow `fyniq.read.<area>` and `fyniq.write.<area>` scopes only, never a wildcard, an approve or a release; networks as CIDR prefixes; limits within the platform's; expiry within a year; the public half of an EC P-256 or RSA (2048+) key, never a private key. The database's checks repeat the scope, expiry and key rules | `internal/domain/apiclient`, `internal/authz/scopes.go`, migration 000420 | `TestAnIntegrationClientIsCheckedWhole`, `TestScopesNarrowAClientToWholeAreas`, `TestAttackAnAPIClientKeepsItsRules` |
| I3 | A client's token is accepted only when Fyniq has a live record of its client ID, and only when the token's subject is the client itself; an unknown client falls back to the consent rules, which refuse a client-credentials token | `internal/authn/introspect.go` | `TestTheIntrospectorTakesAnAPIClientFromItsRecord`, `TestIntegrationClients` (the orphan client) |
| I4 | A client's rights are its member's roles read at introspection, never the token's claims; a member who left or holds no role ends it | `internal/authn/introspect.go` | `TestTheIntrospectorTakesAnAPIClientFromItsRecord` |
| I5 | The token's scopes are cut to the record's; the gate refuses an operation the scopes do not cover (`authz.ScopeAllows`) and a call from outside the client's networks, judged on the audited client address | `internal/api/api.go`, `internal/authz/scopes.go` | `TestScopesNarrowAClientToWholeAreas`, `TestIntegrationClients` |
| I6 | Revocation is never held back; the API refuses the client's tokens at once (within the introspection cache's 30 seconds, AI-38) and the worker deletes it and its tokens from the OAuth server; an expired client is refused the same | `internal/authn/introspect.go`, `cmd/worker` | `TestIntegrationClients` |
| I7 | Each client has its own read and write limits within the platform's (FR-INT-16), with `RateLimit` headers and `Retry-After` on 429 | `internal/api/conventions.go` | `TestIntegrationClients` |
| I8 | Every change a client makes is audited as the integration, with its client ID (AUD-01); a client is never removed (FY420), only revoked, once, and kept for the access review | `internal/api`, migration 000420 | `TestIntegrationClients`, `TestAttackAnAPIClientKeepsItsRules` |
| I9 | `FORCE ROW LEVEL SECURITY` on the client table; the global route is read only by `api_client_access` before any tenant is set; the worker and the AI roles write none of it | migration 000420 | `TestAttackAnAPIClientKeepsItsRules` |

## 4. What is left

- Personal access tokens (FR-INT-02: hashed at rest, ≤ 90 days, shown once, class `agent` by default, reclassed by the
  Owner with dual control) are the next slice of task 3.1; the table already holds them.
- The Connected apps screen that lists and revokes clients follows the API; until then an Administrator uses the API.
- Hydra's own client registration and token endpoint in the dev cloud, behind its load balancer [live-verify].
