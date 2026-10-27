# Tasks

## 1. Envelope and records

- [x] 1.1 Write the envelope builder per object type, with its declared dependencies, in RFC 8785 canonical JSON with SHA-256. Test `// req: FR-APR-10`: a rebuilt envelope that differs blocks execution
  (20 Oct 2026: `internal/domain/approval`: an RFC 8785 canonicaliser (UTF-16 key order, ES6 string escapes, integers
  only: decimals travel as strings) pinned by the RFC's own example; the envelope (organisation, object type, ID and
  version, operation, change, amounts, dependencies, policy version, team mode, requester) refuses a dependency its
  type does not declare or leaves out one it does (`approval.Dependencies`, one table). The API builds it for
  organisation settings (version, value in effect on the date, scheduled changes), memberships (status, version, live
  roles) and invitations, and rebuilds it with the request's row locked first, then the members' lock (ENG-47 order):
  a difference blocks with APR-04 and voids the request. Tests: `TestTheEnvelopeIsCanonicalAndCoversItsDependencies`,
  `TestApprovalsBlockOnChangeAndExpire` (a role that expires changes the rebuilt envelope). **Left:** the ledger
  impact and its hash (FR-APR-02's view, with wave 2's documents); envelopes for the other object types as they land.)
- [x] 1.2 Signed approval records; the database refuses one without a hash or an MFA claim, and a version trigger voids approvals on edit. Tests `// req: FR-APR-03` (G4-056) and `// req: compliance/singapore-statutory Electronic signatures on approvals`
  (20 Oct 2026: migration 000030: `approval_request` (the envelope as signed and its SHA-256, which a CHECK recomputes;
  the change; the policy version; the requester and their second factor; one pending per record; decided once, FY102)
  and `approval_signature` (signer, time, method, hash; refused without a 32-byte hash or a second factor at most 5
  minutes old, with another hash (FY100), by the requester (FY101), or on a request not pending or expired (FY103);
  never changed but to be voided, FY102). A request is approved only with a signature of its envelope (FY100). A new
  version of the record (a setting's next change, a role assignment) voids its pending request by trigger, and voiding
  a request voids its signatures; the unit of work audits the voids. Tests: `TestAttackApprovalsAreSignedEnvelopes`,
  `TestTwoPersonRuleForASetting`. **Left:** keeping a passkey or TOTP assertion for re-verification (G4-056) arrives
  with `p1-users-roles` 2.2: until then the method is the session's step-up (`session_step_up`). Rev 0.43: passkeys exist
  and step sessions up. Rev 0.44: `POST /v1/approvals/{id}/challenge` (If-Match) issues a challenge of 16 random bytes and
  the envelope's SHA-256 bound to the session, organisation and request; approve and reject take `passkey` (the answer),
  checked and recorded in the deciding transaction; the signature keeps the passkey and the assertion (migration 000047,
  FY142), which `passkey.Reverify` checks again. Tests: `TestPasskeySignedApprovals`,
  `TestAttackPasskeySignaturesAnswerTheirChallenge`, `TestAKeptAssertionVerifiesAgain`.)
- [x] 1.3 Step-up for high-risk approvals, and expiry from the app clock with invalidation on any dependency change. Tests `// req: FR-APR-04` and `// req: FR-APR-05` (G4-055)
  (20 Oct 2026: the requester and every signer pass step-up within `authn.StepUpWindow` (API-AUTHZ-05); a request
  expires 7 days after it is made by `ports.Clock` (APR-03), and a void request answers APR-04. **Left:** G4-055's
  payment batch and bank-change case, with wave 2's payments. Rev 0.43: a passkey step-up satisfies FR-APR-04 for the
  requester and every approver, like the provider's TOTP at sign-in.)

## 2. Policies

- [ ] 2.1 Policies per object type, with conditions and ordered steps with a quorum. Test `// req: FR-APR-01`
  (20 Oct 2026, provisional (PRD rev 0.36): policies are code, `approval.Policies`, one per object type with a version
  string each envelope records, ordered steps naming an Appendix D row and a quorum; the three built are one step,
  quorum 1, A on the row (the Owner). Rev 0.40: policies are each organisation's versioned data (migration 000039,
  `approval_policy`), one to five ordered steps, each a quorum of 1 to 5 people who hold A on the object's row
  (`approval.Rows`), narrowed to roles or named members, never both, so a policy never grants a right Appendix D does
  not; `approval.CheckSteps` refuses steps the active members cannot sign (APR-08); the built-in policy applies until
  changed. A request keeps its steps (`policy_steps`), and the database lets a signature sign only the current step
  and approves only when every step has its quorum (FY100, FY106); a person signs a request once (APR-11). Tests:
  `TestStepsAreSignedInOrderToTheirQuorum`, `TestAPolicysStepsCanBeSigned`, `TestAttackApprovalStepsAreSignedInOrder`,
  `TestStepsAreSignedInOrderAndRechecked`. **Left:** conditions (amount, entity, department, account, vendor, risk
  tier), with the first object types that carry them (wave 2's documents).)
- [x] 2.2 Policy changes are approval-gated (SOD-8) and versioned; a pending approval keeps its version, and a change restarts it. Test `// req: FR-APR-09` (G4-057)
  (20 Oct 2026, provisional (PRD rev 0.40): `PATCH /v1/approval-policies/{object_type}` always answers 202 with an
  `approval_policy.change` request; the Administrator (C on "Approval policies, AI catalogue") asks, the Owner or the
  Controller (A), never the requester, approves; the policy for approval policies is fixed (APR-09). The steps are
  checked when asked for and again when the change executes. The database adds a version only as the next, with the
  approved request holding exactly its steps, never changes one (FY104, FY105), and voids every pending request of its
  object type when one is added; the executing transaction re-checks every earlier approver (APR-10). `GET
  /v1/approval-policies` and `GET /v1/approval-policies/{object_type}` (with its history). Tests:
  `TestApprovalPolicyChangesAreApprovedAndKept`, `TestAttackApprovalPoliciesAreApprovedVersions`. The pack-version
  half of G4-057 arrives with pack activation (task 4.1).)
- [ ] 2.3 The approval view: document, ledger impact, affected balances and checks, and the diff against the previous version. Test `// req: FR-APR-02`
  (20 Oct 2026: `GET /v1/approvals` and `GET /v1/approvals/{id}` show the envelope, its hash, what the request changes
  before and after, the signatures and whether the caller may sign; a request is shown to its requester and to whoever
  holds a right on its object's Appendix D row, never to the AI. **Left:** the ledger impact, affected balances and
  checks, with wave 2's documents.)

## 3. Team modes and Founder mode

- [ ] 3.1 Founder, Standard and Enterprise modes, with dual-controlled switching. Tests `// req: access/team-modes <mode> mode` and `… Mode switching`
  (20 Oct 2026, provisional (PRD rev 0.42): migration 000043: `team_mode`, versioned and added only as the next version
  with the approved `team_mode.change` request that holds it, never changed (FY108); a new version voids every pending
  request of the organisation (`approval_void_pending_all`); `sod_override`, one member's conflict kept for one version,
  only with the approved switch or role change that holds it (FY109). Founder mode until the first switch; every
  envelope records the mode in force. `GET /v1/team-mode` (mode, conflicts, overrides, history) and `PATCH
  /v1/team-mode` (always 202: the Administrator asks, an Owner approves); a switch to Standard mode lists the members
  whose roles conflict (SOD-6, `authz.RoleConflicts`) and pending invitations whose roles would, and waits until each
  member's is resolved or overridden with a reason (TEAM-01, TEAM-06); `GET /v1/organisation` shows the mode. Tests:
  `TestTeamModeSwitchesAreDualControlled`, `TestASwitchBlocksWhenTheConflictsChange`, `TestAttackTeamModesAreApprovedSwitches`,
  `TestModes`, `TestASwitchWaitsForEveryConflict`. **Left:** Enterprise mode (TEAM-03 until its per-entity approval
  matrices, SSO, SCIM and quarterly access reviews exist); the team-mode screen (5.1).)
- [ ] 3.2 FM-1 (two-person rule with step-up for both), FM-2 (self-approval at or below the threshold, flagged) and FM-3 (the weekly review task, escalating after 14 days). Tests `// req: FM-1`, `// req: FM-2` and `// req: FM-3`
  (20 Oct 2026: FM-1 for the objects built: `POST /v1/approvals/{id}/approve` and `/reject` sign the envelope whose
  hash the approver sends; the requester never signs (APR-01, and FY101 in the database); the signer holds A on the
  object's row (the Owner), passes step-up and is a person, never the AI. The final approval executes the change in
  the same transaction, audited with the approval's ID. Tests: `TestTwoPersonRuleForASetting`,
  `TestRoleChangesAfterTheBootstrapWaitForApproval`. Rev 0.40: the requester withdraws their pending request
  (`POST /v1/approvals/{id}/withdraw`), which voids it and its signatures; anyone else is refused (APR-12). Rev 0.46, FM-3:
  migration 000051's `review_log_entry` and `review_log_review` (never changed, FY150; never reviewed by the one who
  acted, FY151); each approved change and each founder-bootstrap role change or invitation is logged as it executes;
  `GET /v1/review-log` (with the unreviewed and overdue counts: 14 days in Founder mode) and `POST /v1/review-log/reviews`
  (APR-13, APR-14). Tests: `TestTheReviewLog`, `TestAttackTheReviewLogNeverChanges`. **Left:** FM-2, with the first
  documents that carry an amount; the weekly Inbox task, with the Inbox; the Checks warning, with Checks.)
- [ ] 3.3 FM-8 break-glass and FM-9 beneficiary exclusion. Tests `// req: FM-8` (G4-062, G4-063) and `// req: FM-9`
  **Left.**
- [ ] 3.4 The rules and records for FM-4, FM-5 and FM-7, called by the close (wave 2) and reconciliation (wave 3). FM-6's advisory review arrives with wave 6. Tests `// req: FM-4`, `// req: FM-5` and `// req: FM-7` on the records (G4-109)
  **Left.**
- [ ] 3.5 The control check: period lock, approvals, segregation of duties and team mode together. Test `// req: FR-CTL-04`
  **Left.**

## 4. Wire the waiting refusals

- [ ] 4.1 Switch vendor bank details (FR-CT-03), account merges (FR-COA-04), pack activation (ENG-32), administrative changes (FR-ADM-04) and Owner changes (FR-ADM-09) from "refused until the engine lands" to approval through it, with each change's test extended
  (20 Oct 2026: the two refusals that existed now ask for approval and answer 202 with the request: a change to a
  setting under FM-1 (SET-05 retired) and, after the founder bootstrap, a role assigned or revoked and an invitation
  (MEM-05 retired; the invitation's code is shown to the requester at once and works once approved). PRD rev 0.36
  supersedes "refused until the approval engine" in FR-ORG-08 and FR-ADM-01. Rev 0.41: pack activation (ENG-32) goes
  through the engine as the object type `statutory_pack` (dependencies: the loaded digest and dates, and whether it is
  activated), approved by A on "Statutory parameters", with its pinned tests run again before the activation is
  recorded. 27 Oct 2026 (rev 0.153): vendor bank details go through the engine as `payee_bank_change` with
  `payee_bank_change.approve`, approved by A on "Vendor bank details" by someone who neither prepared nor asked for the
  change, in every team mode (FY237). **Left:** account merges, the rest of FR-ADM-04 and Owner changes, as those
  features land.)

## 5. Screens

- [ ] 5.1 Build the screens as designed: accountant-review, approval-confirmation, approval-policies, bank-details-change, payment-batch (approval part), personal-approvals and team-mode. Add help keys and `docs/user-guide/` pages compiled into `kb/`; the Inbox approve shortcut opens the confirmation and never approves directly (FR-UX-03)
  (20 Oct 2026: the guide page `approvals.md`, compiled into `approvals.decide`, and the settings and members pages
  updated. Later the same day: the Inbox (`screens/approvals/Inbox.tsx`: To approve, your requests, others', the weekly
  review), the request page with approval-confirmation (`ApprovalPage.tsx`, `Decide.tsx`: the diff, the steps and
  signatures, the envelope's hash with copy; Approve and Reject open the confirmation, which signs with a passkey over
  the challenge for the version shown, or with the session's step-up when there is no passkey; a rejection needs a
  reason; the requester withdraws) and the review log (`ReviewLog.tsx`, FM-3). Tests `approvals.test.tsx`.
  Then team-mode (`screens/settings/TeamMode.tsx`: the mode as cards, the conflicts now, the switch with a reason
  per member's conflict over the version shown, the reasons kept and the history) and approval-policies
  (`ApprovalPolicies.tsx`: each kind of change with its steps; a change sets 1 to 5 steps, each a quorum narrowed to
  roles or named members, over the policy's ETag; the fixed policy stays fixed). Tests `TeamMode.test.tsx`.
  **Left:** accountant-review, bank-details-change, payment-batch and personal-approvals with their modules; Founder
  mode's other controls (FM-4…9) and break-glass on team-mode as they are built; the Inbox's keyboard triage (J/K, A
  opens the confirmation) with p1-app-shell 2.2.)
- [ ] 5.2 Finish with `make check` and `make test`
