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
  (Built, rev 0.618 and 0.621: the email half, a link with no token and no action (`TestAnApprovalEmailLinksToTheRequestAndOpeningItDecidesNothing`), and the installed app, which is the same web app with the same confirmation.)

## 3. Automation
- [x] 3.1 Workflow rules without code (D5). `// req: FR-AUT-03`
  (Built, rev 0.620, `[owner to confirm]`: migration 001202, `domain/automation`, `notify.fireRules` in the posting's unit of work, the setting `features.automation_rules` (off, dual control), three operations with matrix rows, the screen Workflow rules at `/settings/workflow-rules`, the guide and knowledge page, the design fragment and the grants for review; `TestAWorkflowRuleTellsMembersWhenABillOverItsAmountIsPosted`, `TestAttackAWorkflowRuleHasOnlyTheTriggersAndActionsTheDatabaseAllows`. **Left:** the action raise a task, triggers other than a posting.)
- [x] 3.2 Outbound webhooks: satisfied by FR-INT-12. `// req: FR-AUT-04`
  (Built by `p2-api-third-parties` (rev 0.602): signed, retried, logged webhooks with a dead-letter list; nothing more here. The spec row is covered by that change's tests.)

## 4. The PWA
- [x] 4.1 Manifest, service worker, installability (D4). `// req: FR-UX-16`
  (Built, rev 0.621: `public/manifest.webmanifest`, the icons, `public/sw.js` for the app's own files only (`pwa/sw.test.ts` holds that it never touches `/api` or a write), registration in production only, the sign-out clean-up. **[live-verify]** installing on iOS and Android. D4 changed, `[owner to decide]`: no offline reads of approvals, as the API says `no-store`; options in the PRD row.)
- [x] 4.2 Camera capture and the offline receipt queue (D4). **[live-verify]** on a real phone. `// req: FR-UX-16`
  (Built, rev 0.621: `Take a photo`, `pwa/queue.ts` (sealed AES-GCM, non-extractable key, time limit from `pwa.offline_queue_hours`, per person, off on a shared device, cleared at sign-out) with 15 tests, uploaded through `captureDocument` with the receipt's ID as the Idempotency-Key. **Left:** trying the camera and IndexedDB on real phones.)

## 5. Soft locks
- [ ] 5.1 Soft-locked periods and per-module soft locks (D6). `// req: FR-GL-09`

## 6. Purchases
- [ ] 6.1 Supplier contracts and renewals, reminders and C.2 #52 (D7). `// req: FR-AP-21`
- [ ] 6.2 Vendor statement reconciliation (D7). `// req: FR-AP-10`

## 7. Administration and experience
- [ ] 7.1 The usage view (D7). `// req: FR-ADM-05`
- [ ] 7.2 Pinned Basic mode (D7). `// req: FR-UX-28`
- [ ] 7.3 Feature tours and What's new (D7). `// req: FR-UX-35`

## 8. Templates
- [ ] 8.1 Layout, column chooser, conditional blocks (D8). `// req: FR-TPL-01`, `// req: FR-TPL-02`
- [ ] 8.2 Visual diff of two versions (D8). `// req: FR-TPL-04`
- [ ] 8.3 Code mode in the render sandbox (D8). `// req: FR-TPL-06`

## 9. Governance and compliance
- [ ] 9.1 The annual-return pack (D9). `// req: FR-CS-07`
- [ ] 9.2 Evidence export per framework and period (D9). `// req: CMP-04`
