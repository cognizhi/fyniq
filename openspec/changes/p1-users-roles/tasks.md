# Tasks

## 1. Roles and the matrix

- [ ] 1.1 Transcribe Appendix D into `internal/authz/matrix.csv` and generate the policy table from it. A test compares the table with the PRD's rows and fails on drift; an unknown area denies. Tag each area's test `// req: access/roles-and-segregation Permission matrix: <area>`
- [ ] 1.2 The roles of §6 and their combinations, attribute conditions and field masking, checked in the API and by row-level security scope predicates on sensitive tables. Tests on every role's rights and limits, `// req: access/roles-and-segregation <role> role`, and the permission model test
- [ ] 1.3 SOD-1…10, evaluated at action time from the record's history. Tests `// req: SOD-1` through `// req: SOD-10`, one conflict each, blocked in Standard mode
- [ ] 1.4 The AI and integration principals: reads within the delegating user's rights minus the AI exclusions, and narrow scopes. Tests `// req: access/roles-and-segregation AI assistant principal` and `… Integration principal`

## 2. People and sign-in

- [ ] 2.1 Invitations with roles, scopes and expiry. Tests `// req: FR-ADM-01` and `// req: SEC-06`, including the quarterly review list of external users
- [ ] 2.2 Mandatory MFA: TOTP through the `SignIn` port's fake, WebAuthn passkeys in the API, no SMS. Tests `// req: FR-ADM-02` (P1 part) and `// req: SEC-01`, with a replayed assertion refused; sign-in and step-up allow password managers, paste and passkeys (the sign-in half of NFR-A11Y-05, which `p1-app-shell` closes)
- [ ] 2.3 Sessions: idle and absolute timeouts, the device list, revocation and sign-in alerts through the outbox. Tests `// req: FR-ADM-03`, `// req: SEC-05` and `// req: SEC-08` extend P0's
- [ ] 2.4 Sign-in providers by configuration, allowed domains and identity linking; the Super admin's scope; Owner succession with dual control. Tests `// req: FR-ADM-07`, `// req: FR-ADM-08` and `// req: FR-ADM-09` extend P0's
- [ ] 2.5 Account recovery as an audited state machine with two independent proofs, cooling-off, notices and revocation. Test `// req: SEC-35` with G4-060

## 3. Clients, outputs and administration

- [ ] 3.1 OAuth clients with class, owner, expiry and scopes; `private_key_jwt` for confidential clients, PKCE for public ones, exact redirect URIs. Test `// req: SEC-07`
- [ ] 3.2 Authorisation on every derived output and channel: exports, events, cached reports and notifications. The role change takes effect within 60 seconds. Test `// req: SEC-34` with G4-064
- [ ] 3.3 Administrative changes go through the approval policy once `p1-approval-engine` lands, and are refused until then. Test `// req: FR-ADM-04`

## 4. Screens

- [ ] 4.1 Build the screens as designed: account-recovery, auditor-workspace, mfa-enrolment, super-admin-console and users-roles. Add help keys, tooltips and `docs/user-guide/` pages compiled into `kb/`; mfa-enrolment is a first-run tour step
- [ ] 4.2 Run the walkthrough test for each role (QA-UI-08) on the workspaces these rights produce
- [ ] 4.3 Finish with `make check` and `make test`
