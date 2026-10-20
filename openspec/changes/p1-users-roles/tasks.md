# Tasks

## 1. Roles and the matrix

- [x] 1.1 Transcribe Appendix D into `internal/authz/matrix.csv` and generate the policy table from it. A test compares the table with the PRD's rows and fails on drift; an unknown area denies. Tag each area's test `// req: access/roles-and-segregation Permission matrix: <area>`
  (20 Oct 2026: the transcription is the Go table itself, not a CSV, so one file holds the fact: `authz.Rows` and
  `authz.Grants` carry all 39 rows of Appendix D in its order, and `TestTheGrantsAreAppendixD` fails when a cell, a row
  or the order differs from the PRD; `TestAnUnknownAreaDenies` refuses every principal on an area Appendix D does not
  name. The per-area matrix tests run each operation's area in `TestAuthorisationMatrix`.)
- [ ] 1.2 The roles of §6 and their combinations, attribute conditions and field masking, checked in the API and by row-level security scope predicates on sensitive tables. Tests on every role's rights and limits, `// req: access/roles-and-segregation <role> role`, and the permission model test
- [ ] 1.3 SOD-1…10, evaluated at action time from the record's history. Tests `// req: SOD-1` through `// req: SOD-10`, one conflict each, blocked in Standard mode
  (20 Oct 2026, provisional (PRD rev 0.42): SOD-6 is the conflict a combination of roles makes, derived from Appendix D
  (`authz.RoleConflicts`: the Administrator with any role holding P, A or R on a transaction row; the Owner role alone
  exempt). In a mode that blocks conflicts a role change that makes it needs `sod_override_reason`, approved with the
  change (TEAM-02); an invitation with conflicting roles is refused; posting or reversing a journal is refused to a
  person who holds it without an override (TEAM-04). SOD-8 and SOD-10 are the approval engine's requester-never-signs
  rule. Tests: `TestRoleCombinationsThatConflict`, `TestTeamModeSwitchesAreDualControlled`. **Left:** SOD-1 to SOD-5,
  SOD-7 and SOD-9 per item, with their objects (payments, journals above the threshold, pay runs, reconciliations,
  close, write-offs); and SOD-6 when approving: once a transaction row's changes go through the approval engine (bills,
  payment batches, period close), `checkConflicts` belongs in `sign()` for those rows too, as it is in `posting` now.)
- [ ] 1.4 The AI and integration principals: reads within the delegating user's rights minus the AI exclusions, and narrow scopes. Tests `// req: access/roles-and-segregation AI assistant principal` and `… Integration principal`

## 2. People and sign-in

- [ ] 2.1 Invitations with roles, scopes and expiry. Tests `// req: FR-ADM-01` and `// req: SEC-06`, including the quarterly review list of external users
  (20 Oct 2026, built before `p1-approval-engine` so the founders can work in Fyniq: PRD-001 rev 0.35's founder
  bootstrap. Migration 000029 adds `member_invitation` (a one-time code kept as its SHA-256, valid 7 days, withdrawable,
  accepted once), `accept_member_invitation` (in whichever organisation holds the code, under its row-level security;
  every refusal the same FY092), `member_names()` (only the organisation's own members) and a trigger that keeps the last
  Owner (FY093). `GET /v1/members`, `GET`/`POST /v1/members/invitations`, `DELETE /v1/members/invitations/{id}`,
  `POST`/`DELETE /v1/members/{id}/roles`, and `POST /v1/members` to join. Until the organisation has a second active
  member the Owner acts alone, their own roles included; after that every role change is refused until the approval
  engine (`member.Bootstrap`, under a members' lock that joining takes too). **Left:** scopes (entity, department),
  invitation emails (`p1-templates-email`), the quarterly review of external users (SEC-06), listing the bootstrap's
  changes in FM-3's weekly review (with `p1-approval-engine` 3.2), the join and members screens (4.1).)
- [ ] 2.2 Mandatory MFA: TOTP through the `SignIn` port's fake, WebAuthn passkeys in the API, no SMS. Tests `// req: FR-ADM-02` (P1 part) and `// req: SEC-01`, with a replayed assertion refused; sign-in and step-up allow password managers, paste and passkeys (the sign-in half of NFR-A11Y-05, which `p1-app-shell` closes)
  (20 Oct 2026, provisional (PRD rev 0.43): TOTP stays with the sign-in provider; the local sign-in page's second factor
  is its fake, ticked by default. Passkeys in the API (`internal/passkey`, go-webauthn v0.18.2; attestation "none", user
  verification required, relying party = the web app's origin and host): `POST /v1/session/passkeys/challenge` and `POST
  /v1/session/passkeys` register one from a session stepped up in the last 5 minutes; `POST /v1/session/step-up/challenge`
  and `POST /v1/session/step-up` step any of the person's sessions up; `GET`/`DELETE /v1/session/passkeys`. Migration
  000045: `webauthn_credential` and `webauthn_challenge`, global, reached only through functions; a challenge is 32
  random bytes bound to its session and purpose, taken once within 5 minutes and answered once (FY141); a counter never
  repeats or goes back, a key never changes, a passkey is revoked once (FY140); registering and revoking are audited in
  the installation's trail; `user_session.step_up_method` (sign_in or passkey) replaces the "step-up only at sign-in"
  check. Every session needs a second factor: one that passed none only reads itself, steps up and, as a Super admin,
  recovers (API-AUTHZ-07, `authz.Policy.SingleFactor`). Tests: `TestPasskeyCeremonies`, `TestPasskeys`,
  `TestAttackPasskeysAreAnsweredChallenges`, `TestOnlyStepUpAndRecoveryNeedNoSecondFactor`; threat model
  `docs/security/threat-models/passkeys.md`. **Left:** the browser ceremonies' screens (4.1, with `p1-app-shell`), so
  founders cannot add a passkey from the web app yet; the sign-in half of NFR-A11Y-05; approvals signed with a passkey
  assertion over the envelope hash (`p1-approval-engine` 1.2, G4-056).)
- [ ] 2.3 Sessions: idle and absolute timeouts, the device list, revocation and sign-in alerts through the outbox. Tests `// req: FR-ADM-03`, `// req: SEC-05` and `// req: SEC-08` extend P0's
- [ ] 2.4 Sign-in providers by configuration, allowed domains and identity linking; the Super admin's scope; Owner succession with dual control. Tests `// req: FR-ADM-07`, `// req: FR-ADM-08` and `// req: FR-ADM-09` extend P0's
- [ ] 2.5 Account recovery as an audited state machine with two independent proofs, cooling-off, notices and revocation. Test `// req: SEC-35` with G4-060

## 3. Clients, outputs and administration

- [ ] 3.1 OAuth clients with class, owner, expiry and scopes; `private_key_jwt` for confidential clients, PKCE for public ones, exact redirect URIs. Test `// req: SEC-07`
- [ ] 3.2 Authorisation on every derived output and channel: exports, events, cached reports and notifications. The role change takes effect within 60 seconds. Test `// req: SEC-34` with G4-064
- [ ] 3.3 Administrative changes go through the approval policy once `p1-approval-engine` lands, and are refused until then. Test `// req: FR-ADM-04`
  (20 Oct 2026: role changes are refused once a second member exists (MEM-05), as FM-1 needs; the other
  administrative changes of FR-ADM-04 arrive with their features. Later on 20 Oct 2026, `p1-approval-engine`
  (rev 0.36): those role changes and invitations now wait for another Owner's approval instead; MEM-05 is retired.)

## 4. Screens

- [ ] 4.1 Build the screens as designed: account-recovery, auditor-workspace, mfa-enrolment, super-admin-console and users-roles. Add help keys, tooltips and `docs/user-guide/` pages compiled into `kb/`; mfa-enrolment is a first-run tour step
- [ ] 4.2 Run the walkthrough test for each role (QA-UI-08) on the workspaces these rights produce
- [ ] 4.3 Finish with `make check` and `make test`
