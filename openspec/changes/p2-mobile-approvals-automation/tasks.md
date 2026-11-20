# Tasks

## 1. Decisions and specs
- [ ] 1.1 PRD-001 records the decisions; the specs are updated and stamped. `// req: FR-APR-06`
  (Done per slice: each built task adds its PRD revision, spec scenario and stamp; rev 0.618 for section 2.)

## 2. Approvals
- [x] 2.1 Delegation of approval authority (D1). `// req: FR-APR-06`, `// req: QA-SEC-03`
  (Built, rev 0.618, `[owner to confirm]`: migration 001200/001201, `approval_delegation` and `approval_signature.on_behalf_of`, the trigger refusing a signature without a live delegation on its signing day (FYD02), `approval.StandIn`, the three operations with matrix rows, the screen Delegations at `/delegations`, the guide and knowledge pages, the design fragment, and the grants listed for review. Tests: `TestAttackDelegationsLendNoMoreThanTheirDaysAndAreKept`, `TestADelegateSignsTheStepOfTheDelegatorForItsDaysAndNoMore`, `TestADelegatorAndTheirDelegateCountOnceInAQuorum`. **Left:** a delegation by object type or role.)
- [x] 2.2 Reminders and escalation, notify only (D2). `// req: FR-APR-08`
  (Built, rev 0.618: the hourly job `approvals.remind`, the settings `approvals.reminder_hours` and `approvals.escalate_hours`, the insert-only `approval_reminder` log, the types `approval.reminder`, `approval.escalated`, `approval.delegated`; `TestAPendingApprovalIsRemindedAndEscalatedButNeverDecided`.)
- [x] 2.3 Approve by email link and in the PWA (D3). `// req: FR-APR-07`
  (Built, rev 0.618 and 0.622: the email half, a link with no token and no action (`TestAnApprovalEmailLinksToTheRequestAndOpeningItDecidesNothing`), and the installed app, which is the same web app with the same confirmation.)

## 3. Automation
- [x] 3.1 Workflow rules without code (D5). `// req: FR-AUT-03`
  (Built, rev 0.620, `[owner to confirm]`: migration 001202, `domain/automation`, `notify.fireRules` in the posting's unit of work, the setting `features.automation_rules` (off, dual control), three operations with matrix rows, the screen Workflow rules at `/settings/workflow-rules`, the guide and knowledge page, the design fragment and the grants for review; `TestAWorkflowRuleTellsMembersWhenABillOverItsAmountIsPosted`, `TestAttackAWorkflowRuleHasOnlyTheTriggersAndActionsTheDatabaseAllows`. **Left:** the action raise a task, triggers other than a posting.)
- [x] 3.2 Outbound webhooks: satisfied by FR-INT-12. `// req: FR-AUT-04`
  (Built by `p2-api-third-parties` (rev 0.602): signed, retried, logged webhooks with a dead-letter list; nothing more here. The spec row is covered by that change's tests.)

## 4. The PWA
- [x] 4.1 Manifest, service worker, installability (D4). `// req: FR-UX-16`
  (Built, rev 0.622: `public/manifest.webmanifest`, the icons, `public/sw.js` for the app's own files only (`pwa/sw.test.ts` holds that it never touches `/api` or a write), registration in production only, the sign-out clean-up. **[live-verify]** installing on iOS and Android. D4 changed, `[owner to decide]`: no offline reads of approvals, as the API says `no-store`; options in the PRD row.)
- [x] 4.2 Camera capture and the offline receipt queue (D4). **[live-verify]** on a real phone. `// req: FR-UX-16`
  (Built, rev 0.622: `Take a photo`, `pwa/queue.ts` (sealed AES-GCM, non-extractable key, time limit from `pwa.offline_queue_hours`, per person, off on a shared device, cleared at sign-out) with 15 tests, uploaded through `captureDocument` with the receipt's ID as the Idempotency-Key. **Left:** trying the camera and IndexedDB on real phones.)

## 5. Soft locks
- [ ] 5.1 Soft-locked periods and per-module soft locks (D6). `// req: FR-GL-09`

## 6. Purchases
- [ ] 6.1 Supplier contracts and renewals, reminders and C.2 #52 (D7). `// req: FR-AP-21`
  **Left, not started (paused for budget):** migration (a `supplier_contract` register with the supplier, owner, dates, renewal type, notice deadline, billing, amount and currency, minimum commitment and a capture as its evidence, versioned, never deleted, RLS, audit), a port and adapter, five operations on the Bills row, a daily job `contracts.remind` that tells the owner 30 and 7 days before the notice deadline once each (a log table like `approval_reminder`, new notification types), the screen, and check C.2 #52 in `internal/domain/closing` and `internal/domain/checks` (a supplier billed in each of the three months before the month, or a contract covering the month, with no bill dated in it; a warning) with a fixture that triggers it. The links from a contract to its POs, recurring bills and bills are left for a later slice. Model on `automation_rule` (001202) and `approvalremind`.
- [ ] 6.2 Vendor statement reconciliation (D7). `// req: FR-AP-10`
  **Left, not started (paused for budget):** statement lines entered or imported per supplier and period, matched to bills and payments by reference and amount (exact decimals), differences listed, nothing posted without a person; a read model and a screen, no new write path to the ledger.

## 7. Administration and experience
- [x] 7.1 The usage view (D7). `// req: FR-ADM-05`
  (Built, rev REV: `GET /v1/usage` on the *Tenant export* row (the Owner, the Administrator and the DPO; View; never the AI), read-only from what the platform already keeps (no migration, no new grant): the AI calls by the month they started in with their tokens and exact cost per currency, the report exports made ready and the evidence files uploaded, and what is held in storage now (evidence, captured documents, exports); the screen *Usage and cost* at `/settings/usage`, the guide and knowledge page and the design fragment. Tests: `TestTheUsageViewShowsEveryMonthAndOnlyToTheOnesWhoMaySeeIt`, the matrix row, `Usage.test.tsx`. **Left:** the base-currency total of the AI's cost (per-currency sums are exact; converting needs the day's rates), a per-person breakdown, and the compute and bandwidth figures, which the platform does not meter yet.)
- [x] 7.2 Pinned Basic mode (D7). `// req: FR-UX-28`
  (Built, rev REV, `[owner to confirm]`: one switch setting per pinnable role, `experience.pinned_basic.<role>` (every role but the Owner and the Administrator, so the people who set it never lose the switch); a person is pinned when every role they hold is pinned (the line manager is a relationship, not a role); the preferences and the session say `pinned_basic`, show Basic, and a save of Expert is refused (PRF-05); the web app draws no Basic | Expert switch, palette command or shortcut for them. Tests: `TestAnAdministratorPinsARoleToBasicAndOnlyThatRole`, `mode.test.ts`. **Left:** nothing.)
- [ ] 7.3 Feature tours and What's new (D7). `// req: FR-UX-35`
  **Left, not started (paused for budget):** design: an append-only `user_tip_state` table (user, kind `seen` with key and version, `whats_new` with the release, or `tips_off`), `GET`/`PUT /v1/help-state/tips` beside the tour's, a tips catalogue in `apps/web/src/tour/` of one to three steps per feature with a version bumped when a release changes the feature, a dismissible What's new card from `help/guide.ts`'s release notes shown once per release, and a Profile switch to turn tips off.

## 8. Templates
- [ ] 8.1 Layout, column chooser, conditional blocks (D8). `// req: FR-TPL-01`, `// req: FR-TPL-02`
  **Left, not started (paused for budget):** the typed layout JSON the render service already reads gains per-block order and settings, a column chooser and the five fixed conditional blocks (GST-registered, foreign currency, overdue, discount present, PayNow available); the editor drag-and-drop (with a keyboard alternative, WCAG 2.5.7) and live preview.
- [ ] 8.2 Visual diff of two versions (D8). `// req: FR-TPL-04`
  **Left, not started:** compare two published versions' content side by side with the list of changed settings (versions already carry the content and its SHA-256).
- [ ] 8.3 Code mode in the render sandbox (D8). `// req: FR-TPL-06`
  **Left, deliberately not built partly:** a half-sandboxed code mode must not ship. Design to build whole: Typst source edited by an expert, stored as a template version kind `code` (versioned, approved as `document_template.publish`), compiled only by the private `render` service in a fresh folder as Typst's root holding the source, the data as one JSON file and the logo; no network, no packages (the image ships the allowed ones), an empty environment, a time and memory limit, fonts the image ships, PDF/UA-1 output. Attack tests to write first: a `read`/`image`/`include` of a path outside the folder, of `/etc/passwd` and of a URL; a `@preview` package import; a `sys.inputs` or environment read; an infinite loop and a memory bomb; a source that tries to write; data fields outside the allow-list; a template that renders another organisation's data; the output staying PDF/UA-1. Needs `[live-verify]` on the real render image.

## 9. Governance and compliance
- [ ] 9.1 The annual-return pack (D9). `// req: FR-CS-07`
  **Left, not started:** needs the ACRA filing categories from the official ACRA pages (URL, retrieval date, quote), tagged `[accountant to confirm]`; assemble officers, members, share capital and the financial-statement figures from the corporate registers and the balance service; files nothing.
- [ ] 9.2 Evidence export per framework and period (D9). `// req: CMP-04`
  **Left, not started:** a job that packages a control framework's evidence for a period as a signed archive from the control registry (`compliance/controls/`) and the evidence store.
