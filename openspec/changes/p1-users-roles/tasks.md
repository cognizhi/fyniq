# Tasks

## 1. Roles and the matrix

- [x] 1.1 Transcribe Appendix D into `internal/authz/matrix.csv` and generate the policy table from it. A test compares the table with the PRD's rows and fails on drift; an unknown area denies. Tag each area's test `// req: access/roles-and-segregation Permission matrix: <area>`
  (20 Oct 2026: the transcription is the Go table itself, not a CSV, so one file holds the fact: `authz.Rows` and
  `authz.Grants` carry all 39 rows of Appendix D in its order, and `TestTheGrantsAreAppendixD` fails when a cell, a row
  or the order differs from the PRD; `TestAnUnknownAreaDenies` refuses every principal on an area Appendix D does not
  name. The per-area matrix tests run each operation's area in `TestAuthorisationMatrix`.)
- [x] 1.2 The roles of §6 and their combinations, attribute conditions and field masking, checked in the API and by row-level security scope predicates on sensitive tables. Tests on every role's rights and limits, `// req: access/roles-and-segregation <role> role`, and the permission model test
  (20 Nov 2026, with 2.1: attribute conditions for the Approver's department cells (PRD-001 rev 0.371) are enforced twice: the API's policy
  table still refuses a department-qualified read it has no endpoint for, and the database holds a restrictive policy on journal
  lines and balances for the API's role that reaches only the rows of the department values the person's Approver role is
  narrowed to (`TestAttackADepartmentScopeBindsWhatTheAPIReads`: no scope reaches none; any other role beyond Employee leaves
  the person unrestricted here; the worker, the executor and the read-only role are unfiltered; the tenant policy is never
  widened; a scope is fixed, audited, and names department values of a live Approver assignment only). The AI and MCP calls
  for a person inherit it (`TestAPrincipalNamesItsPersonForTheDatabase`). Roles' own rights and limits were tested above.
  Field masking is the API's. **Left:** the department-qualified endpoints (reports, budgets, approvals by department) and
  documents' own lines; this task stays open until they are built.)
  (20 Nov 2026, PRD-001 rev 0.374: the department-qualified P&L: `getFinancialStatement` admits a person whose view of the reports is their
  department's (`authz.DeptOnly`) for the P&L only, the database adding up the lines of their scope's values; everything the
  filter does not reach is refused. `TestADepartmentsApproverReadsTheirDepartmentsProfitAndLoss`. **Left:** the Approver's budgets
  (C dept) and approvals by department (a bill's lines against the scope), documents' own lines; this task stays open.)
  (20 Nov 2026, PRD-001 rev 0.381, the rest of the department cells: the Approver's "C dept" on the budgets reads and plans their
  department's lines (`budget.Scope`; `TestADepartmentsBudgetHolderPlansTheirDepartmentsLines`: the draft they read, a save that replaces
  their lines only and refuses another department's, a company line and a changed name; Budget vs Actual added up for their
  department; drafting, copying, importing, asking for approval, the forecasts and the planning summary refused; no scope reaches none).
  An Approver whose every assignment is narrowed reaches the bills, invoices and expense claims, their files and their requests of their
  departments only (`TestADepartmentsApproverReachesTheirDepartmentsRecords`: lists and reads, a refused sign leaves the claim unposted,
  the company's payables and a bill's files refused, a role that reaches the row unqualified or an unscoped Approver unchanged); a
  request notifies only those it is theirs (`TestAnApproversNotificationFollowsTheirDepartments`). Decided [owner to confirm]: an Approver role
  with no scope is not narrowed on records and approvals (so nobody loses what they had), purchase orders carry no department so are in none.
  Built: the department-qualified endpoints, approvals by department and documents' own lines. **Left:** nothing in this task.)
- [x] 1.3 SOD-1…10, evaluated at action time from the record's history. Tests `// req: SOD-1` through `// req: SOD-10`, one conflict each, blocked in Standard mode
  (20 Oct 2026, provisional (PRD rev 0.42): SOD-6 is the conflict a combination of roles makes, derived from Appendix D
  (`authz.RoleConflicts`: the Administrator with any role holding P, A or R on a transaction row; the Owner role alone
  exempt). In a mode that blocks conflicts a role change that makes it needs `sod_override_reason`, approved with the
  change (TEAM-02); an invitation with conflicting roles is refused; posting or reversing a journal is refused to a
  person who holds it without an override (TEAM-04). SOD-8 and SOD-10 are the approval engine's requester-never-signs
  rule. Tests: `TestRoleCombinationsThatConflict`, `TestTeamModeSwitchesAreDualControlled`. **Left:** SOD-1 to SOD-5,
  SOD-7 and SOD-9 per item, with their objects (payments, journals above the threshold, pay runs, reconciliations,
  close, write-offs); and SOD-6 when approving: once a transaction row's changes go through the approval engine (bills,
  payment batches, period close), `checkConflicts` belongs in `sign()` for those rows too, as it is in `posting` now.)
  (6 Nov 2026, PRD-001 rev 0.179, on the owner's decisions of the same day: SOD-5 at the reconciliation's sign-off:
  whoever decided a line of the account booked in the month (as it stands) does not sign it off in Standard and
  Enterprise modes (BANK-15), unless they ask for an override with a reason (`requestReconciliationOverride`,
  `bank_reconciliation.sign`) that an Owner approves; it is then signed off as them with the override kept, reported on
  Checks (C.2 #62), and the database refuses any other (migrations 000231 and 000232, FY267). SOD-6 when acting:
  `checkConflicts` in `sign()` for requests on a transaction's row, and on releasing a batch, signing off a
  reconciliation and attesting a close. SOD-7 holds through FR-GL-08's reopening on another person's approval. SOD-1,
  SOD-2, SOD-3 and SOD-9 were built with their objects (`payee_test.go`, `payment_batch_release_test.go`, the approval
  tests, `writeoffs_test.go`). Tests `TestWhoDecidedTheLinesSignsOffOnlyUnderAnOverride`,
  `TestAConflictOfRolesNeverApprovesATransaction`, `TestAttackAReconciliationKeepsSOD5`, `TestReopeningAClosedPeriod`
  (SOD-7), `reconciliation.test.tsx`. **Left:** SOD-4, with pay runs (Nemura, FR-PPL-10).)
  (20 Nov 2026, the rest, on the 2.4 precedent: SOD-4, preparing a pay run and approving or paying it, comes with pay runs
  from Nemura after go-live (`docs/prd/plans/launch-exceptions.json`, rev 0.25: no one prepares or approves a pay run
  at launch), so its object does not exist yet; SOD-1 to SOD-3 and SOD-5 to SOD-10 are built and tested as above.)
- [x] 1.4 The AI and integration principals: reads within the delegating user's rights minus the AI exclusions, and narrow scopes. Tests `// req: access/roles-and-segregation AI assistant principal` and `… Integration principal`

  (20 Nov 2026: built across the AI and API changes and now proved against the spec's scenarios. The AI reads for a
  member within that member's rights, lesser of theirs and the AI column's, and never writes, approves, posts or
  releases for any role on any row (`TestAIReadsWithinTheDelegatingUsersRightsAndWritesNothing`); it never signs an
  approval (`TestDecide`, the never-list's tests in `internal/proposal`); its results leave out the AI exclusions
  (`TestPlantedExclusionsReachNoModelRequest`, `TestTheAITracesHoldNoExcludedValue`). An integration's scopes name whole
  areas, read or write, and never reach users, payments, payroll, equity, the audit trail or approvals
  (`TestScopesNarrowAClientToWholeAreas`, tagged `Integration principal`).)
## 2. People and sign-in

- [x] 2.1 Invitations with roles, scopes and expiry. Tests `// req: FR-ADM-01` and `// req: SEC-06`, including the quarterly review list of external users
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
  (12 Nov 2026: the quarterly review of external users (the access review, rev 0.165), FM-3's listing of the
  bootstrap's changes (`founder_bootstrap`) and the join and members screens are built. PRD-001 rev 0.253, the owner's
  decision: department scopes are built in P1, a role narrowed to department values reaching only those records, lines
  and budgets under Appendix D's "dept" cells; entity scopes with multi-entity (P3). **Left:** department scopes and
  invitation emails.)
  (20 Nov 2026, department scopes built in part, PRD-001 rev 0.371: `POST /v1/members/{member_id}/roles` takes `department_value_ids`
  for the Approver role (each a value of the department dimension, checked when asked, carried by the approval request and
  fixed for the assignment: migration 000702), the members list shows them and Settings › Members and roles offers the
  department values when the Approver role is chosen. **Left:** an invitation's scope, invitation emails
  (`p1-templates-email`).)
  (20 Nov 2026, PRD-001 rev 0.381: an invitation that gives the Approver role carries `department_value_ids` (migrations 000706 and 000707;
  the second Owner signs them with the invitation; the redeemed role holds them from the first minute;
  `TestAnInvitationNarrowsTheApproverRoleItGives`, `TestAttackAnInvitationsDepartmentScopeNeedsTheApproverRole`), and the invite dialog offers
  the departments. **Left:** invitation emails (`p1-templates-email`); this task stays open for them alone.)
  (20 Nov 2026, PRD-001 rev 0.385: an invitation can carry an email address, to which one message goes, at once in the bootstrap and otherwise once the
  invitation is approved; it names the organisation and says to ask the inviter for the code, which is never in an email, and the address is not kept
  (migrations 000709, 000710; `TestAnInvitationEmailNeverCarriesTheCode`). Decided [owner to confirm]: the code stays given in person or on a call.
  Built: invitation emails. **Left:** nothing in this task.)
- [x] 2.2 Mandatory MFA: TOTP through the `SignIn` port's fake, WebAuthn passkeys in the API, no SMS. Tests `// req: FR-ADM-02` (P1 part) and `// req: SEC-01`, with a replayed assertion refused; sign-in and step-up allow password managers, paste and passkeys (the sign-in half of NFR-A11Y-05, which `p1-app-shell` closes)
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
  founders cannot add a passkey from the web app yet; the sign-in half of NFR-A11Y-05. Rev 0.44: approvals signed with a
  passkey assertion over the envelope hash (`p1-approval-engine` 1.2, G4-056) are built.)
  (20 Nov 2026, the rest: the browser ceremonies are Settings › Profile and security (add, name and revoke passkeys,
  `Profile.test.tsx`, `stepup.test.tsx`, `passkey.test.ts`), the step-up dialog offers a passkey or a new sign-in, and
  `accessibleAuthentication.test.tsx` closes the sign-in half of NFR-A11Y-05 (paste and password managers allowed, no
  cognitive test). TOTP stays with the sign-in provider and the local page's fake; a replayed assertion is refused
  (`TestAttackPasskeysAreAreAnsweredChallenges` as named above). No SMS factor exists anywhere.)
- [x] 2.3 Sessions: idle and absolute timeouts, the device list, revocation and sign-in alerts through the outbox. Tests `// req: FR-ADM-03`, `// req: SEC-05` and `// req: SEC-08` extend P0's
  (20 Oct 2026, provisional (PRD rev 0.45): migration 000049: `user_session.user_agent`, `start_session_from`,
  `user_sessions` and `end_user_session`; `GET /v1/session/devices`, `DELETE /v1/session/devices/{device_id}` and `POST
  /v1/session/devices/sign-out-others`, a device's ID derived from its session's hash; `signin.EmailAlert` emails each
  sign-in to the provider-verified address through the outbox in the installation's organisation (email purpose
  `sign_in_alert`). Test: `TestYourDevices`. **Left:** a configurable idle timeout (SEC-05), with the security
  settings; the devices screen (4.1).)
  (6 Nov 2026, PRD-001 rev 0.180, on the owner's decision of the same day: the idle timeout is the organisation setting
  `security.idle_timeout_minutes` ("Sign out after inactivity", 5 to 60 minutes, 30 by default, dual control; the new
  setting kind `minutes`). Migration 000233: `fyniq.idle_timeout` reads the value in force on the organisation's date,
  and `session_for` finds the session's organisation before applying it; the devices list uses the same
  (`Sessions.IdleTimeout`). Test: `TestBrowserSessions` (10 and 60 minutes). **Left:** the devices screen (4.1).)
  (7 Nov 2026: the devices screen is Settings › Profile and security › Your devices (`Profile.test.tsx`), so the task
  is done.)
- [x] 2.4 Sign-in providers by configuration, allowed domains and identity linking; the Super admin's scope; Owner succession with dual control. Tests `// req: FR-ADM-07`, `// req: FR-ADM-08` and `// req: FR-ADM-09` extend P0's
  (6 Nov 2026, PRD-001 rev 0.181, on the owner's decisions of the same day: Owner succession. An Owner starts one for
  another Owner who is permanently unavailable (`POST /v1/members/{member_id}/succession`), with a reason and the kind
  of evidence, naming a member to become an Owner if wanted; the outgoing Owner and every Owner are told
  (`security.owners`, emailed at once); for 7 days the outgoing Owner may object and its starter withdraw it; its files
  go on its own `owner_succession` document. From the 7th day a Super admin, any one, completes it with a file
  attached: the named member becomes an Owner and the outgoing Owner's membership ends. Every Owner is told of every
  Owner change. Migrations 000234 and 000235 (FY268). Screen: Owner successions on Settings › Members and roles. Tests
  `TestAnOwnerIsSucceededAfterSevenDaysNotice`, `TestAttackAnOwnerSuccessionKeepsItsRules`, `Successions.test.tsx`.
  **Left:** sign-in providers by configuration, allowed domains and identity linking (FR-ADM-08); the Super admin
  console for organisations a Super admin is not a member of (P3).)
  (6 Nov 2026, PRD-001 rev 0.196, on the owner's decisions of the same day: identity linking. From Settings › Profile
  and security, in a session whose second factor is less than 5 minutes old, a person signs in with a provider
  (`/api/auth/signin?provider=…&link=1`, sent from Fyniq only) and that account is added to theirs, unless it signs
  someone in already; a matching email links nothing. `GET /v1/session/identities` lists their sign-ins and
  `DELETE /v1/session/identities/{identity_id}` removes one, never the one in use nor the last, ending its sessions.
  The database checks the second factor again (migration 000239, FY270, FY271); each change is audited in the
  installation's trail and sent as `security.sign_in_methods`. Tests `TestYourSignIns`,
  `TestLinkingASignInAddsItToTheSessionsUser`, `Profile.test.tsx`. **Left:** Super admins switching providers, their
  allowed domains and ending their sessions, with the last-method guard and dual control; joining by domain as a
  pending Employee membership an Owner approves; adding providers in the app (P2 or P3).)
  (7 Nov 2026, PRD-001 rev 0.197: the provider switches. Settings › Sign-in providers: a Super admin turns each
  configured provider on or off, optionally ending its sessions, and sets the email domains it accepts
  (`GET /v1/installation/sign-in-methods`, `PATCH /v1/installation/sign-in-methods/{provider}`); with two Super admins
  a change waits as an installation change of step `sign_in`. The flow refuses a provider turned off and an account
  on no accepted domain (AUTH-SIGNIN-08). The database refuses a change that leaves a Super admin no way in
  (migrations 000240 and 000241, FY272, FY273). Tests `TestSignInProviders`, `TestTheFlowFollowsTheProvidersSwitches`,
  `SignInProviders.test.tsx`. **Left:** joining by domain as a pending Employee membership an Owner approves; adding
  providers in the app (P2 or P3).)
  (17 Nov 2026, for the launch gate, G4-061: a sign-in by an identity with no Fyniq user whose verified email is a
  member's links and reaches nothing, as before, and is now recorded against the member in each of their
  organisations' trails (`session.email_matched`, linked false), its actor the provider and a hash of the issuer and
  subject. `fyniq.email_matches` (migration 000440) gives the API the members' organisations and IDs only; the hook is
  `signin.AuditEmailMatch`. Test `TestYourSignIns`.)
  (7 Nov 2026, PRD-001 rev 0.199, on the owner's decision of the same day: joining by domain. An Owner claims the
  organisation's email domains (one organisation per domain); a Super admin lets a provider take requests for the
  domains it accepts. A person without an account, signed in with a verified email on a claimed domain, asks to join;
  an Owner approves (a member with the Employee role) or rejects. Migrations 000244 and 000245. Tests
  `TestJoinByDomain`, `join.test.tsx`. Every P1 part is built; adding providers in the app (P2 or P3) and the Super
  admin console for organisations a Super admin is not a member of (P3) belong to later phases.)
- [x] 2.5 Account recovery as an audited state machine with two independent proofs, cooling-off, notices and revocation. Test `// req: SEC-35` with G4-060
  (7 Nov 2026, PRD-001 rev 0.198, on the owner's decisions of 6 Nov 2026: ten recovery codes per person on the profile,
  keyed hashes; a confirmation from an Owner or Administrator after speaking to the person (one-time, 24 hours); the
  recovery from a session without a second factor gives it one by recovery, ends other sessions and tokens, replaces
  the codes, notifies the person and every Owner, logs the weekly review and holds `authz.HeldInCoolingOff` for 24
  hours. Migrations 000242 and 000243. Tests `TestAccountRecovery` (G4-060), `TestTheCoolingOffHoldsRealOperations`,
  `recovery.test.tsx`. **Left:** the cooling-off's length as a setting.)
  (12 Nov 2026, PRD-001 rev 0.253, the owner's decision: the organisation setting `security.recovery_cooling_off_hours`,
  24 to 72 hours, 24 by default, under dual control; a person in several organisations is held for the longest.)
  (12 Nov 2026: built. The setting kind `hours`; migration 000324: `recovery_cooling_off` reads each of the person's
  organisations' value in force and keeps the longest, and `recover_account` holds for it. Guide
  `organisation-settings.md` (Hold after an account recovery). Test `TestTheHoldAfterARecoveryIsTheOrganisationsSetting`.)

## 3. Clients, outputs and administration

- [x] 3.1 OAuth clients with class, owner, expiry and scopes; `private_key_jwt` for confidential clients, PKCE for public ones, exact redirect URIs. Test `// req: SEC-07`
  (20 Nov 2026: built with the public API and the first-party MCP changes, found by audit. Each API client has a
  class (AI-34), an owner, an expiry within a year and narrow scopes, its networks and limits, registered by two people
  (`internal/api/integration_clients.go`, `internal/domain/apiclient`, migration of `api_client`, `TestIntegrationClients`,
  `TestAttackAnAPIClientKeepsItsRules`, tagged `// req: SEC-07`); confidential clients authenticate with
  `private_key_jwt` and never a shared secret, public clients use the authorisation code with PKCE, each redirect URI
  is registered with the client and matched exactly by Hydra (`internal/adapters/hydra`; the flow is in `oauth_test.go`,
  `TestTokenLifecycle`, with no test yet for a near-miss URI); the integration clients are
  listed in the quarterly access review (`AccessReview.tsx`, "Integration clients").)
- [x] 3.2 Authorisation on every derived output and channel: exports, events, cached reports and notifications. The role change takes effect within 60 seconds. Test `// req: SEC-34` with G4-064
  (6 Nov 2026, PRD-001 rev 0.186, for the outputs that exist: a notification by row (`notify.Sees`) shows, counts and
  is emailed only while its person's roles hold the right; the list pages past what they no longer see, a read by ID
  answers not found, and the digest withdraws held email instead of sending it (migration 000238:
  `notification.withdrawn_at`, once, never of sent email, the worker's alone; FY188). Exports, files, evidence and
  reports are checked at each request; every answer is `Cache-Control: no-store` and no report result is kept; sessions
  read the roles at each request and consented tokens are revoked within 60 seconds (`TestTokenLifecycle`). Tests
  `TestEveryOutputFollowsTheRolesHeldNow` (G4-064), `TestHeldEmailFollowsTheRole`, `TestAPersonSeesWhatTheirRolesHoldNow`.
  Server-sent events (FR-UX-19), scheduled deliveries, the reader-filtered knowledge-base index (AI-54) and AI traces
  apply it as they arrive.)
- [x] 3.3 Administrative changes go through the approval policy once `p1-approval-engine` lands, and are refused until then. Test `// req: FR-ADM-04`
  (20 Oct 2026: role changes are refused once a second member exists (MEM-05), as FM-1 needs; the other
  administrative changes of FR-ADM-04 arrive with their features. Later on 20 Oct 2026, `p1-approval-engine`
  (rev 0.36): those role changes and invitations now wait for another Owner's approval instead; MEM-05 is retired.)

  (20 Nov 2026: `p1-approval-engine` landed (rev 0.36): role changes, invitations, policy and team-mode changes wait
  for another Owner's approval, and the refusal-until-then of MEM-05 is retired. Tests `team_mode_test.go`,
  `approval_attack_test.go`, `TeamMode.test.tsx`, tagged `// req: FR-ADM-04`.)
## 4. Screens

- [ ] 4.1 Build the screens as designed: account-recovery, auditor-workspace, mfa-enrolment, super-admin-console and users-roles. Add help keys, tooltips and `docs/user-guide/` pages compiled into `kb/`; mfa-enrolment is a first-run tour step
  (20 Oct 2026, in part: Settings › Profile and security (`screens/settings/Profile.tsx`): whether you confirmed it's
  you and until when, your passkeys (add with the browser's ceremony, revoke) and your devices (sign one out, or every
  other one), with the guide page `profile-and-security.md` compiled into `preferences.security`. Tests
  `Profile.test.tsx`, `stepup.test.tsx`, `passkey.test.ts`. Then users-roles as Settings › Members and roles
  (`screens/settings/Members.tsx`, Expert mode): the members with their roles and SOD-6 conflicts, the invitations not
  yet used (withdraw), the role changes waiting for another Owner, inviting (the code shown once, at once in the
  founder bootstrap or with the approval request after it) and adding or ending a role, with the conflict's reason in a
  mode that blocks conflicts. Tests `Members.test.tsx`. **Left:** mfa-enrolment's guided steps and recovery codes (TOTP
  is the provider's); on users-roles, the columns the API does not return yet (email, last sign-in, MFA status,
  scopes), default experience mode per invitation, Owner transfer and succession, and the access review;
  account-recovery, auditor-workspace and super-admin-console.)
  (20 Nov 2026, audit of what stands: account-recovery is built (`screens/Recovery.tsx`, `RecoveryCodes.tsx`: a recovery code plus a
  still-bound authenticator or an Owner's or Administrator's confirmation, the cooling-off banner, `recovery.test.tsx`, guide
  `profile-and-security.md`, SEC-35); the auditor's workspace is the Auditor's landing, Reports, where **Your engagement**
  says until when the read-only access lasts and opens the trial balance as at any date, the general ledger, the journals,
  who has access, the evidence and the audit trail (`AuditorAccess`, `HomeWork.test.tsx`, QA-UI-08's walkthrough). **Left:**
  (1) mfa-enrolment's guided first-run steps at `/sign-in/enrol`: TOTP is the provider's and passkeys and recovery codes have
  their cards on Profile and security, but no single guided page walks a new person through passkey, codes and a second
  authenticator; (2) the super-admin-console at `/admin`: it needs two reads that do not exist yet, the platform's health
  (services, scheduler heartbeat, outbox backlog, last audit anchor, across organisations through a definer function) and
  the list of Super admins, besides the installation's connectors, announcements and sign-in providers that have their pages;
  (3) on users-roles, the columns the API does not return (email, last sign-in, MFA status, scopes), the default
  experience mode per invitation, and department scopes (see 2.1).)
  (20 Nov 2026, mfa-enrolment, the first item of the Left above, is built: `screens/Enrol.tsx` at `/sign-in/enrol`, linked from Profile and
  security: three steps, a passkey (or, where the browser cannot make one, the authenticator app the provider asks for), ten
  recovery codes, and a second authenticator (recommended), each Done or To do from what the API holds, and "You are set up"
  when the first two are done; guide `sign-in.md` (Set up how you confirm it's you), compiled into `auth.sign-in`; test
  `enrol.test.tsx`. Not a first-run tour step: the founder's tour is at its seven.)
  (20 Nov 2026, super-admin-console built, PRD-001 rev 0.372: `GET /v1/installation/health` and `GET /v1/installation/super-admins` for a
  Super admin (counts, times and names only, through definer functions: migration 000703), and Settings › Installation console
  (`AdminConsole.tsx`): healthy or degraded and why, the platform jobs' beats, the outbox's backlog, the Super admins with the
  warning while there is one; guide `installation-console.md`, compiled into `settings.console`. Tests
  `TestTheSuperAdminConsoleReadsHealthAndSuperAdmins`, `adminConsole.test.tsx`. Not shown: installation email senders, whose
  SPF, DKIM and DMARC status has its own page.)
  (20 Nov 2026, members columns, PRD-001 rev 0.373: last sign-in and live passkeys beside each member (`member_activity()`, migration 000704).
  **Left, an owner question:** the member's email (personal data the Auditor and the DPO would see on the list); the default experience
  mode per invitation, an invitation's department scope, MFA status beyond passkeys (TOTP is the provider's).)
- [x] 4.2 Run the walkthrough test for each role (QA-UI-08) on the workspaces these rights produce
  (20 Nov 2026, found built: `apps/web/journeys/walkthroughs.spec.ts` runs QA-UI-08 for each of the sixteen roles of §7.1
  in an organisation of its own: landing page, sidebar, Home blocks, Inbox, the "not part of your access" page and
  the API's 403 for the same read; two roles see the union (`navigation.test.ts`).)
- [ ] 4.3 Finish with `make check` and `make test`
