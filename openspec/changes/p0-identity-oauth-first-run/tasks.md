# Tasks

## 1. OAuth server

- [ ] 1.1 Pin Hydra (Apache-2.0) by version and digest as the `oauth` image, sharing the database, with opaque tokens; verify the pin in the image check and record version and edition in ADR-T3's file under `docs/adr/`
- [ ] 1.2 Implement the login and consent endpoints in the API, granting the MCP audience only when the client is allowed it; test (exit item 6) issues a token to a test client, and the API rejects a wrong-audience token (`// req: AI-36`)
- [ ] 1.3 Add refresh rotation with family revocation, revocation, and a role change denying the next call within 60 seconds; tests `// req: AI-38` cover reused refresh tokens, expired and revoked tokens, and the role change

## 2. The open question 40 spike

- [ ] 2.1 Have the MCP server obtain an API-audience token with the RFC 7523 JWT-bearer grant from its KMS key, only for the subject of a just-validated MCP token, at most 5 minutes, with scopes intersected by the API; test asserts no passthrough and the intersection (`// req: AI-36`)
- [ ] 2.2 Write up the spike's limits and measurements for the security reviewer in `docs/adr/` beside the Enterprise Licence quote; verify it is linked from pull request #4

## 3. Sign-in

- [ ] 3.1 Add the `SignIn` port with Identity Platform and the Firebase emulator, and Google and Zoho providers with the configured issuer per provider; test `// req: FR-ADM-08` rejects an ID token from another issuer
- [ ] 3.2 Complete a live Zoho sign-in through the US data centre in staging and store the evidence (§17.3 exit item 7)

## 4. First-run setup and Super admins

- [ ] 4.1 Generate the one-time setup code at deployment and build the setup wizard so the first person to finish becomes Super admin and Owner; tests `// req: FR-ADM-06` and `// req: CTL-IAM-07` (no code, reused code, second run)
- [ ] 4.2 Add the second Super admin, offline recovery codes and the two-person recovery runbook (`docs/runbooks/super-admin-recovery.md`), with every change audited and step-up required; test the nightly count of Super admins ≥ 1 (`// req: CTL-IAM-07`), then `make check` and `make test`
