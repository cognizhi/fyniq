# ADR-T3: identity and OAuth for integrations and MCP

**Status:** accepted (PRD-001 rev 0.20, open question 11). The decision itself, its context and the
alternatives are in PRD-001 §12.2.1; this file records what P0 pinned and proved against it, as ADR-T3
asks ("the Hydra version and edition are pinned").

## Pinned

| | |
|---|---|
| Server | Ory Hydra, **Apache-2.0 build v26.2.0** (git `0b84568f`, built 20 Mar 2026) |
| Image | `oryd/hydra:v26.2.0@sha256:ff67c7fb5f95074fa53374d41151713554960504b340cd3f95b09e65deaea2a9` (`internal/adapters/hydra.Image`; the local stack uses the same, a test keeps them equal) |
| Edition | Apache-2.0: no RFC 8693 token exchange, no RFC 8707 resource indicators, no Client ID Metadata Documents (open question 40). The Enterprise Licence quote is the founders' to obtain |
| Storage | Its own database, `hydra`, on the same PostgreSQL server, owned by its own role (Cloud SQL in the cloud, p0-infra-opentofu) |
| Settings | `OAUTH2_PKCE_ENFORCED=true`; opaque access tokens (`STRATEGIES_ACCESS_TOKEN=opaque`) with introspection; access tokens 1 hour; `--sqa-opt-out` (no telemetry); `--dev` only in the local stack and the tests (it allows an `http` issuer) |
| Endpoints | Public under `/oauth/` (issuer `http://localhost:8080/oauth/` locally); admin API internal only. Login and consent are the API's `/auth/login` and `/auth/consent` |

## How tokens are checked (P0)

- **Audience binding without RFC 8707.** Consent grants an audience only when the client asked for it,
  it is the API's or the MCP server's, and the client's record allows it (`internal/oauthflow`). The API
  accepts only tokens whose audience is the API (`authn.Introspector`); the MCP server will accept only
  the MCP audience.
- **PKCE (S256) only.** Hydra enforces it at the end of the flow; the login page also refuses a request
  without an S256 challenge, before anyone signs in.
- **Introspection, cached for at most 30 seconds**, keyed by the token's SHA-256, so a revoked token stops
  working within 30 seconds (AI-38 allows 60).
- **Lifetimes (AI-38).** Access tokens 1 hour. Refresh tokens rotate on every use, with Hydra's reuse
  detection: a reused refresh token is refused and its whole family revoked. `TTL_REFRESH_TOKEN` is 14
  days, which rotation makes the idle expiry. Hydra's Apache-2.0 build has no absolute lifetime for a
  rotating chain, so consent stamps `granted_at` on the token and the API refuses any grant older than 90
  days, refreshed or not (and any undated one).
- **Revocation after a change (AI-38).** A change to what a user was granted (roles, deactivation, a removed
  client, a kill switch) writes an `oauth.subject.revoke` outbox row in its own unit of work, with an
  `oauth.tokens.revoke_requested` audit event; the worker revokes every token of the user (Hydra deletes
  their consent sessions); with the 30-second cache the next call is denied within 60 seconds. Hydra does
  not report the family revocations it makes on refresh reuse, so those leave no Fyniq audit event; the
  client sees `invalid_grant`.
- **Who the token is for.** The organisation, roles and client class are claims consent stamps on the
  token. That is a snapshot at consent; the token lifecycle slice revokes a user's tokens when their role
  changes (AI-38), and memberships replace the snapshot with a lookup when they arrive. The class always
  comes from the client's record (AI-34), never from the request.
- **Who signs in.** Until the SignIn port (Identity Platform), the login and consent pages identify the
  user by the local build's dev tokens, which only tests and local tools send. **The release build accepts
  no credential at all** until memberships and the SignIn port exist.

## Proven (P0 exit item 6, first part)

`internal/api/oauth_test.go` runs the pinned image: a test client completes login and consent with PKCE
and gets an API-audience token the API accepts; an allowlisted client gets an MCP-audience token the API
refuses; the API also refuses expired and revoked tokens; a client cannot get an audience its record does
not allow; a flow without PKCE and a refused consent end without a code; `fyniq.read.payroll` is never
granted.

`TestTokenLifecycle` proves the second part: refresh tokens rotate; a reused refresh token is refused and
its family's access and refresh tokens stop working; after a change asks for a user's tokens to be revoked,
the API still answers within its 30-second cache and refuses after it.

Still to prove (the MCP spike): the MCP server obtains a separate API-audience token without passthrough
(option (b), RFC 7523). The role change itself arrives with memberships, which call the revocation.
