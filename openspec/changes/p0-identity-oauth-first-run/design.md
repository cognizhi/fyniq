# Design

## Context

ADR-T3 (accepted, rev 0.20): Identity Platform for user sign-in, Ory Hydra as the OAuth 2.1 server for
integrations and MCP. Hydra's Apache-2.0 build has no RFC 8707 support, no Client ID Metadata Documents
and no token exchange (checked 14 Oct 2026). §9.7 and open question 40 describe the workarounds.

## Goals / Non-Goals

**Goals:**
- Prove exit items 6 and 7 against the pinned Hydra version and edition.
- Give the security reviewer a working option (b) to judge, with its limits measured.

**Non-Goals:**
- The MCP server's tools (P1), passkeys (built in the API in P1, since Identity Platform has none), and
  third-party clients (P2).

## Decisions

- **Audience binding without RFC 8707.** The consent app grants the MCP resource as the access token's
  audience (`grant_access_token_audience`) only when it is among the client's allowed audiences; the API
  accepts only API-audience tokens, and the MCP server only MCP-audience tokens.
- **Option (b) for the spike.** The MCP server signs a JWT assertion with its KMS key for the subject of
  the MCP token it just introspected; Hydra issues a token with at most 5 minutes' lifetime; the API
  intersects its scopes with the user's current rights. The weakness, that a stolen MCP key could mint
  tokens for any user within the agent class, is written up for the security reviewer with the
  Enterprise Licence quote beside it.
- **Revocation within 60 seconds.** Opaque tokens with introspection, cached for at most 30 seconds; a
  role change revokes the user's tokens.
- **Issuer check.** The configured issuer per provider is stored, and the ID token's `iss` must equal it
  exactly.
- **First-run setup.** The setup code is generated at deployment into Secret Manager and shown once; it
  is consumed atomically and cannot be reused.
- **Roles stamped at consent (P0).** Until memberships exist, consent stamps the organisation, roles and
  client class on the access token, and the API reads them back through introspection. It is a snapshot,
  valid until the token expires or is revoked: the token lifecycle slice revokes a user's tokens on a role
  change (AI-38), and the memberships slice replaces the snapshot with a lookup.
- **The release build stays closed.** It accepts no credential, for the API or for sign-in, until
  memberships and the SignIn port exist. Only the local build chains its dev tokens and Hydra's tokens,
  and the two never reach each other's checker (a `fyniq-dev.` token is never sent to Hydra).
- **Who signs in, before the SignIn port.** The login and consent pages read the local build's dev token
  from the Authorization header. That is for tests and local tools only; in production they read the
  first-party session, and POST /auth/consent gains a CSRF token with it.
- **Consent rules.** Only known scopes (`openid`, `offline_access`, `fyniq.read.<area>`,
  `fyniq.propose.<task>`); `fyniq.read.payroll` and `fyniq.read.equity` never in P0. Audiences: the
  requested ones, each the API's or the MCP server's and on the client's record, or consent is refused.
  Consent is never remembered. The pinned version and settings are recorded in
  `docs/adr/ADR-T3-identity-and-oauth.md`.
- **Audit events emitted.** `setup.completed`, `super_admin.added`, `super_admin.recovery_used`,
  `oauth.client.created`, `oauth.token.revoked_family` (AUD-01).
- **Ports and adapters.** `SignIn` (Identity Platform, Firebase emulator), `Signer` (KMS, local key) and
  `OAuthServer` (Hydra admin API; local Hydra in Compose).

## Risks / Trade-offs

- [The security review rejects option (b)] → the spike's code is isolated in `cmd/mcp`, and switching to
  the Enterprise Licence (option a) or another server (option c) changes only that path.
- [Zoho's token `iss` differs from the discovery document] → exit item 7 tests the live token in staging,
  not only discovery.
