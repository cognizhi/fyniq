# Tasks

## 1. OAuth server

- [x] 1.1 Pin Hydra (Apache-2.0) by version and digest as the `oauth` image, sharing the database, with opaque tokens; verify the pin in the image check and record version and edition in ADR-T3's file under `docs/adr/`
- [x] 1.2 Implement the login and consent endpoints in the API, granting the MCP audience only when the client is allowed it; test (exit item 6) issues a token to a test client, and the API rejects a wrong-audience token (`// req: AI-36`) (local build; the release build waits for the SignIn port and memberships)
- [x] 1.3 Add refresh rotation with family revocation, revocation, and a role change denying the next call within 60 seconds; tests `// req: AI-38` cover reused refresh tokens, expired and revoked tokens, and the role change (the role-change command arrives with memberships, task 4.x, and calls `tokens.RevokeAll`)

## 2. The open question 40 spike

- [x] 2.1 Have the MCP server obtain an API-audience token with the RFC 7523 JWT-bearer grant from its KMS key, only for the subject of a just-validated MCP token, at most 5 minutes, with scopes intersected by the API; test asserts no passthrough and the intersection (`// req: AI-36`) (as built: `internal/mcpdelegation`, used only by `cmd/mcp`; `authn.Delegation` at the API; measured in `internal/api/delegation_test.go` against the pinned Hydra. The key is a local ES256 key in the spike; Cloud KMS and `private_key_jwt` with p0-infra-opentofu and P1, and no MCP tool calls it until P1)
- [x] 2.2 Write up the spike's limits and measurements for the security reviewer in `docs/adr/` beside the Enterprise Licence quote; verify it is linked from pull request #4 (written: `docs/adr/ADR-T3-mcp-delegation-spike.md`, linked from #4 on 15 Oct 2026. 16 Oct 2026: the founders decided not to buy the Enterprise Licence, so no quote will sit beside it; the record states the decision, option (b) on the free build)

## 3. Sign-in

- [x] 3.1 Add the `SignIn` port with Identity Platform and the Firebase emulator, and Google and Zoho providers with the configured issuer per provider; test `// req: FR-ADM-08` rejects an ID token from another issuer (slice 4a: the port, the ID-token check every provider passes with its test, the local fallback the PRD names instead of the emulator (§12.8 rev 0.19: a dev OpenID Connect issuer in the local build), first-party sessions and the web app's sign-in; slice 4b: the Identity Platform adapter with Google and Zoho, which opens the release build once configured; built and tested against a fake of Identity Platform's REST API, the live run held for staging with task 3.2)
- [ ] 3.2 [live-verify] Complete a live Zoho sign-in through the US data centre in staging and store the evidence (§17.3 exit item 7)

## 4. First-run setup and Super admins

- [x] 4.1 Generate the one-time setup code at deployment and build the setup wizard so the first person to finish becomes Super admin and Owner; tests `// req: FR-ADM-06` and `// req: CTL-IAM-07` (no code, reused code, second run) (the API: `GET`/`POST /v1/setup`; the `/install` screen follows once the SignIn port can sign a person in; locally `make setup-code`, in the cloud the deployment, p0-infra-opentofu)
- [x] 4.2 Add the second Super admin, offline recovery codes and the two-person recovery runbook (`docs/runbooks/super-admin-recovery.md`), with every change audited and step-up required; test the nightly count of Super admins ≥ 1 (`// req: CTL-IAM-07`), then `make check` and `make test` (the API: `/v1/super-admins/…`; recovery takes two proofs, SEC-35; step-up comes from the dev token locally and from the SignIn port in the release build, task 3.1; the notification of a recovery waits for notifications, P1)
