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
  first-party session, and POST /auth/consent gains a CSRF token with it. (As built, slice 4a: they read
  the session cookie, and a POST the cookie authenticates must carry `Sec-Fetch-Site: same-origin`
  instead of a CSRF token; see "Sign-in, as built".)
- **Consent rules.** Only known scopes (`openid`, `offline_access`, `fyniq.read.<area>`,
  `fyniq.propose.<task>`); `fyniq.read.payroll` and `fyniq.read.equity` never in P0. Audiences: the
  requested ones, each the API's or the MCP server's and on the client's record, or consent is refused.
  Consent is never remembered. The pinned version and settings are recorded in
  `docs/adr/ADR-T3-identity-and-oauth.md`.
- **Token lifetimes and revocation (AI-38), as built.** A refresh rotates the tokens; a reused refresh token
  revokes its family (Hydra's reuse detection). Refresh tokens last 14 days from their last use. Hydra's
  Apache-2.0 build has no absolute lifetime for a chain, so consent stamps `granted_at` and the API refuses
  a grant older than 90 days. A change to a user's grant revokes their tokens through the outbox
  (`oauth.subject.revoke`, run by the worker), never in the request.
- **First-run setup, as built (slice 3a).** Users, sign-in identities ((issuer, subject) pairs), the
  installation row and Super admins are global tables reached only through definer functions; memberships
  and role assignments are tenant tables. `POST /v1/setup` is one transaction: it consumes the code
  (`claim_setup`, row-locked, so of two concurrent setups one is refused), creates the Super admin and
  their identity, the installation organisation (its own audit trail: setup and Super-admin events,
  founder decision 15 Oct 2026) and the first organisation with the new user as Owner; the new user is
  the actor of every event. It accepts only a person's first-party session (never an OAuth token or the
  AI); the issuer comes from the authenticator, never the token. The code is 130 random bits, stored as
  its SHA-256, created by `migrate setup-code` and refused after setup. A role change (`internal/members`)
  revokes the member's tokens in the same unit of work. The `/install` screen follows the SignIn port.
- **Super admins, as built (slice 3b).** The second Super admin joins with a one-time invitation (7 days;
  a new one replaces an unused one); P0 allows at most two and never none (`super_admin_guard`, FY083/FY084),
  and the singleton row lock serialises Super admin changes. Each Super admin gets ten printed recovery
  codes at setup and on joining, and can replace them. Recovery needs two proofs (SEC-35): a recovery code
  with the other Super admin's confirmation (24 hours), or the deployment's two-person recovery
  (`migrate super-admin-recovery`, the migrator only, naming two operators, 24 hours;
  `docs/runbooks/super-admin-recovery.md`). The recovering sign-in replaces the Super admin's others,
  their codes are replaced, and the replaced sign-ins' tokens (and the user's) are revoked through the
  outbox. Codes are 130 random bits, stored as SHA-256 in `super_admin_grant`, used once, refused with one
  answer whatever the reason, and never logged or put in the trail; every grant created or used needs its
  event in the installation's trail (`require_grant_audit`). Inviting, confirming and replacing codes need
  an active Super admin in a first-party session who passed step-up in the last 5 minutes
  (`Principal.StepUpAt`, our choice of window): the local build reads it from the dev token's `step_up_at`;
  the release build has no step-up until the SignIn port (passkey or TOTP, FR-ADM-02, SEC-01), so these
  operations stay closed there. Dual control of Super admin changes (FR-ADM-07) is not needed in P0: the
  only change a second Super admin could dual-control would be a third, which P0 refuses. The nightly
  `superadmins.verify` (03:00 Singapore time) fails and records `control.check.failed` when no Super admin
  can sign in (CTL-IAM-07). Not built: FR-ADM-09's notification of every Super admin and Owner on a
  recovery (notifications, P1; the runbook says to tell them), SEC-35's cooling-off period (approvals, P1),
  and the setup wizard's prompt for the second Super admin (the `/install` screen).
- **Sign-in, as built (slice 4a).** The `SignIn` port (`internal/ports/signin.go`) sends the browser to a
  provider and checks what comes back: `Start` returns the provider's address, `Finish` returns the
  verified identity (issuer, subject, name, email and whether it is verified, the authentication time,
  and whether a second factor was passed). Every provider's ID token passes one check,
  `signin.Verify` (go-oidc v3.21.0 with go-jose v4.1.5): signed with RS256 or ES256 by the provider's
  keys, `iss` exactly the provider's configured issuer with no exception (go-oidc lets a Google token
  say `accounts.google.com` for `https://accounts.google.com`; ours does not), the audience the client
  ID (and the authorised party with several audiences), unexpired, not issued or authenticated in the
  future, and the sign-in's nonce. Locally the port is the **fallback the PRD names** (§12.8, rev 0.19:
  the Firebase emulator fails TOTP enrolment): a dev OpenID Connect issuer in the local build's API, at
  `/auth/dev/authorize`, whose page asks who you are and issues an ES256 ID token that `Finish` checks
  like any provider's, after the state, a one-time code (1 minute) and PKCE (S256). Its one provider,
  `local`, has the dev tokens' issuer (`urn:fyniq:local-dev`), so a page sign-in and a dev token with
  the same subject are the same identity. The release build has no SignIn adapter until Identity
  Platform (slice 4b), so it still accepts no credential.
- **Sessions, as built (slice 4a).** `GET /auth/signin?provider=…&return_to=…` derives the state, nonce
  and PKCE verifier from 260 random bits it keeps in an HttpOnly, SameSite=Lax cookie for 10 minutes
  (nothing stored server-side); `GET /auth/callback` checks the return with `Finish`, starts a session
  and sends the browser back to `return_to`, a path on the origin only. The session token (260 random
  bits) lives only in an HttpOnly, SameSite=Lax cookie (`__Host-fyniq_session` with Secure over https;
  `fyniq_session` on http://localhost); the database keeps its SHA-256 in `user_session` (migration
  000013, a global table reached through `start_session`, `session_for`, `end_session` and
  `end_user_sessions`). `session_for` resolves, on every request, the identity's user, their
  organisation (the first they are an active member of; choosing among several arrives with
  organisations, P3) and their live roles, so first-run setup, a role change or a recovery applies at
  the next request. A session ends after 30 minutes idle, 12 hours in all (SEC-05), at
  `POST /auth/signout`, when a new sign-in replaces it in the browser, and when a recovery replaces its
  user's sign-ins (SEC-35: all but the recovering one). The cookie is ambient, so a request it
  authenticates may change state only with `Sec-Fetch-Site: same-origin` (OWASP Fetch Metadata), in the
  API, at sign-out and at consent; a bearer token in Authorization wins over the cookie. Step-up is a
  sign-in that passed a second factor (`amr` contains `mfa`, RFC 8176): the session records its time,
  and Super admin actions accept it for 5 minutes. The OAuth login gives Hydra the user ID as the
  subject (what a role change and a recovery revoke), refuses an identity without a user, and sends a
  browser without a session to sign in and back. `GET /v1/session` and `GET /v1/sign-in-providers`
  let the web app show who is signed in and the providers. Sign-ins are logged as `auth.signed_in`
  (provider, issuer, subject, second factor), never with a token, code or cookie. Not built: the
  sign-in page and `/install` designs (slice 4c), the Identity Platform adapter with Google and Zoho
  (slice 4b), a job that deletes ended and expired sessions, and choosing an organisation.
- **Audit events emitted.** `setup.completed`, `super_admin.added`, (as built, slice 3b) `super_admin.invitation_created`,
  `super_admin.recovery_codes_replaced`, `super_admin.recovery_confirmed`, `super_admin.deployment_recovery_issued` and
  `super_admin.recovered` (planned as `super_admin.recovery_used`),
  `oauth.client.created`, `oauth.tokens.revoke_requested` (AUD-01). (As built: Hydra does not report the
  family revocations it makes on refresh reuse, so `oauth.token.revoked_family` cannot be emitted; the
  client receives `invalid_grant`.)
- **Ports and adapters.** `SignIn` (Identity Platform, Firebase emulator), `Signer` (KMS, local key) and
  `OAuthServer` (Hydra admin API; local Hydra in Compose). (As built: `SignIn`'s local adapter is the dev
  OpenID Connect issuer, `internal/adapters/local/signin.go`, not the Firebase emulator; sessions are
  `storage.Sessions` on PostgreSQL.)

## Risks / Trade-offs

- [The security review rejects option (b)] → the spike's code is isolated in `cmd/mcp`, and switching to
  the Enterprise Licence (option a) or another server (option c) changes only that path.
- [Zoho's token `iss` differs from the discovery document] → exit item 7 tests the live token in staging,
  not only discovery.
