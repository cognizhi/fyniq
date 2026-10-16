# Proposal

## Why

P0's exit items 6 and 7 require a working OAuth server with audience-bound tokens and a live Zoho sign-in
with an issuer check (§17.3), and the founders decided the identity path in rev 0.20 (open questions 11,
36 and 40). The security review (pull request #4) runs in parallel. Building does not wait for it
(rev 0.22), but its findings can still change this change.

## What Changes

- Sign-in through Identity Platform behind a `SignIn` port (local: the Firebase Authentication emulator),
  with Google and Zoho as OpenID Connect providers. The Zoho issuer is the configured data centre's
  (US, `https://accounts.zoho.com`), and a token from any other issuer is rejected (FR-ADM-08 P0 part,
  exit item 7).
- Ory Hydra, Apache-2.0 build, pinned by version and digest as the `oauth` image, sharing the database
  (ADR-T3), with opaque tokens and introspection (open question 40), and its login and consent endpoints
  in the API.
- Tokens: audience-bound and never passed through (AI-36); refresh-token rotation, where a reused refresh
  token revokes its family; revocation within 60 seconds, and a role change denies the next call within
  60 seconds (AI-38).
- The open question 40 spike, option (b): the MCP server obtains a separate API-audience token with the
  RFC 7523 JWT-bearer grant from its own KMS key, only for the subject of an MCP token it has just
  validated, with a lifetime of 5 minutes at most and scopes intersected by the API. The spike records
  its limits for the security reviewer and the founders' final choice before P1's MCP work.
- First-run setup: a one-time setup code created at deployment; the first person to finish becomes the
  Super admin and the Owner of the first organisation (FR-ADM-06); a second Super admin, offline printed
  recovery codes and a two-person recovery runbook (open question 36); changes audited with step-up
  (CTL-IAM-07).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds FR-ADM-06 and FR-ADM-08's P0 part (`access/users-and-sign-in`), AI-36
and AI-38 for the test client (`ai/mcp-and-agents`), CTL-IAM-07, and §17.3
exit items 6 and 7.

## Impact

- `cmd/api` (login, consent, setup wizard), `cmd/mcp` (token exchange spike only), the `oauth` image,
  `internal/ports/signin`, `internal/adapters/{gcp,local}/signin`, `docs/runbooks/super-admin-recovery.md`.
- **Needs the founder first:** a Zoho developer app in the US data centre, the Google OAuth client, and
  the Ory Enterprise Licence quote (open question 40). (16 Oct 2026: the founders decided not to buy the
  licence, so the quote is no longer needed.)
- **Second human reviewer required:** the security reviewer (AGENTS.md: the AI/MCP path and identity).
- Screens: the first-run setup and sign-in screens are already designed (`first-run-setup`, `sign-in` in
  `docs/design/screens/inventory.json`); the first-run setup carries the security banner (FR-UX-41).
