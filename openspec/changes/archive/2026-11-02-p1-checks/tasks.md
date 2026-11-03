# Tasks

Each slice below is one pull request that passes `make test` on its own. Slice 1 is two: the engine with its API (1.1 to
1.6), then the page (1.7), as the KPIs were.

## 1. The engine, its storage and its runs (slice 1)

- [x] 1.1 PRD rev 0.163 with the code: Appendix D's row "Checks and books health" (design D7). Add `authz.Checks`
  and its policies (`listChecks`, `getCheck`, `runChecks`, `setCheckOwner`) and the matrix rows. Test:
  `TestPolicyMatchesAppendixD`, the matrix. `// req: QA-SEC-03`
- [x] 1.2 `internal/domain/checks`:
  - `Definition`, `Result`, `Env` (D1);
  - running a set of definitions;
  - books health (D9);
  - a run's error recorded as failed, never in the result.
  Tests: property tests (no change, no new state; health is the worst status). `// req: FR-CTL-01`,
  `// req: FR-CTL-02`
- [x] 1.3 Migration: `check_state`, `check_run`, `check_owner` (RLS forced, worker writes, purge function), the
  storage port and adapter, attack tests, the grants and schema goldens. `// req: FR-CTL-01`
- [x] 1.4 The runs (D5):
  - the `checks.refresh` outbox work from the audit watermark, enqueued at commit by the adapter and coalesced;
  - the platform job `checks.run` at 04:15 Singapore time;
  - `runChecks`, at most once a minute;
  - status changes audited.
  Tests: a posting reruns its checks within one refresh; the nightly runs all; a burst makes one run.
  `// req: FR-CTL-01`
- [x] 1.5 The API: `listChecks` (the summary and books health, each check with its owner, last run and items
  filtered by the viewer, D7), `getCheck`, `runChecks`, `setCheckOwner` (If-Match, CHECK-02). Then `make openapi`.
  Tests: through the API as each matrix role. `// req: FR-CTL-01`, `// req: FR-CTL-02`
  Built (rev 0.163, migration 000193): the Appendix D row "Checks and books health" also took C.1 #5 to ⚠, as the close
  built it in `p1-reports` (D3), so the two say one thing. The close adds the catalogue's ✖ through
  `closing.Service.More`. The design's "As built" notes record the rest: no delay before a refresh (a commit asks only
  while none waits), the purge refused by a trigger within retention, C.2 #1 recomputed nightly and on request, and C.2 #3
  read from `audit.verified`. C.1 #23's failure is triggered on a stubbed statement: the equity movement and its
  positions are both worked out from the lines, so no change to the lines or the balances makes them disagree.
- [x] 1.6 The first checks, close-backed and ledger (D2, D6):
  - C.1 #1 debits equal credits; #2 the balance sheet balances every month; #3 cash flow agrees to the bank;
    #4 and #5 every account in the P&L and the balance sheet; #6 opening balances balance; #7 system accounts
    exist; #23 movement in equity reconciles;
  - C.2 #1 INV-08; #3 the audit chain.
  - A test runs the close and the catalogue on one fixture and compares them.
  Each with a fixture that fires and one that passes. `// req: C.1 #1` … `// req: C.2 #3`
- [x] 1.7 The Checks page (`/checks`, `Checks.html`):
  - the summary, the nightly verification group, the areas, the items with their links, Run now, the owner
    picker;
  - books health in Home's pulse card, with net burn moving to Basic's tiles;
  - registry entries, help keys `checks.overview` and `checks.owner`, the guide page `checks.md` compiled into
    `kb/`.
  Tests: Vitest with axe; mode parity. `// req: FR-CTL-02`, `// req: FR-UX-09`
  Built: `screens/checks/Checks.tsx` with the nightly verification group of the checks built (C.2 #1, #3), Run now
  (`checks.run`) and the owner's dialog (`checks.owner`); books health takes the pulse card's third place for a reader
  of the checks, with net burn moving to the tiles; `home.md` and its compiled page say so. Item links open the
  period's close, the chart or the opening balances; the documents' links join with their checks.

## 2. Setup, transactions and documents (slice 2)

- [x] 2.1 C.1 #8 chart of accounts problems (✖ and ⚠ halves); #9 contacts and FX rates problems (the employees'
  half joins with the payroll link). `// req: C.1 #8`, `// req: C.1 #9`
- [x] 2.2 C.1 #10, #11, #12: bank lines that cannot post, to review, dated in the future.
  `// req: C.1 #10` … `// req: C.1 #12`
- [x] 2.3 C.1 #13, #14, #15: invoices that cannot post, to review, overdue more than 90 days.
  `// req: C.1 #13` … `// req: C.1 #15`
- [x] 2.4 C.1 #16, #17 bills; #18 journals (✖ and ⚠). `// req: C.1 #16` … `// req: C.1 #18`
- [x] 2.5 C.1 #24 sales document line problems; #25 quotes and orders to follow up. `// req: C.1 #24`,
  `// req: C.1 #25`
- [x] 2.6 C.2 #10 possible duplicate bills; #20 posted bills or claims without a receipt; #48 input tax claimed on a
  bill unpaid 12 months after its due date. `// req: C.2 #10`, `// req: C.2 #20`, `// req: C.2 #48`
- [x] 2.7 (PRD rev 0.164, the owner's decisions of 1 Nov 2026) A ⚠ item leaves its check once marked reviewed, until
  it changes: migration 000194 `check_review` (FY246), `reviewCheckItem` (CHECK-04), the runner and the reads leaving
  reviewed items out, Appendix D's A on "Checks and books health" for the Owner, the Controller, the Accountant and the
  Bookkeeper, and **Mark reviewed** on the page (`checks.review`). The transaction checks flag only what cannot post
  (C.1 #10), lines in suspense (#11) and lines booked after today (#12). `// req: FR-CTL-01`
  Built: drafts count when dated by the end of the month to close next (else today), so a later draft never refuses an
  earlier close; posted invoices and bills are reviewed in the open months (the closed ones at their close); C.1 #24 is a
  line worth nothing or naming an item no longer sold; C.1 #25 a quote sent with no answer within 7 days of its validity
  date, or accepted and not invoiced in full; C.2 #48 counts a bill with a claimable line [accountant to confirm].

## 3. Bank, assets, close and planning (slice 3)

- [x] 3.1 C.1 #22 asset register problems (✖ and ⚠, INV-07); C.2 #2 every sub-ledger the modules built have
  (AR, AP, deposits, fixed assets, prepayments, deferred revenue, expense claims; payroll liabilities and GST join
  with their modules). `// req: C.1 #22`, `// req: C.2 #2`, `// req: INV-07`
- [x] 3.2 C.1 #28, #29 recurring entries (the close's checks, through the catalogue); C.2 #56 to #59, the close's
  reversals, variances, checklist and revaluation. `// req: C.1 #28`, `// req: C.1 #29`, `// req: C.2 #56` …
  `// req: C.2 #59`
- [x] 3.3 C.1 #30 suspense not zero; #31 a bank or cash balance below zero at a month end; #32 reconciliations not
  matching; C.2 #8 a feed stale for 3 days; #9 lines unreconciled after 30 days; #43 bank data sync failed (the
  payment providers' sweep joins with them); #49 settlement residuals over the cap, or a write-off approved by the
  receipt's recorder; #51 possible duplicate bank lines. `// req: C.1 #30` … `// req: C.2 #51`
- [x] 3.4 C.1 #33 a closed period changed after closing (against the close's recorded trial balance); C.2 #17
  foreign-currency items not revalued at the last month end; #18 a month ended over 15 days ago and not closed.
  `// req: C.1 #33`, `// req: C.2 #17`, `// req: C.2 #18`
- [x] 3.5 C.1 #36 cash runway below 6 months, from the KPIs' runway (not assessed without a month; passes when not
  burning). `// req: C.1 #36`
- [x] 3.6 FR-CTL-05 on Checks: the nightly verification group (C.2 #1, #2, #3 with when each ran). The Compliance
  Center reads the nightly `check_run`. `// req: FR-CTL-05`
  Built: the close's own checks are close-backed (`closeBacked`): each takes the close's findings of its keys for the
  month to close next, an item each opening the month's close; C.2 #56 to #59 wait for the month to end. A unit test
  feeds each its keys' findings (the close's own tests trigger them); the checklist, a month left open, a closed month
  changed (C.1 #33, against the close's recorded trial balance), the runway, a feed behind and a balance not revalued
  have database fixtures. The worker reads the KPIs' entered counts (migration 000195) for the runway. C.2 #49's
  write-off half is a backstop for SOD-9, which the approval refuses (WRITEOFF-11): no fixture can reach it through the
  application; its tolerance half is the close's. C.1 #22's ⚠ for an asset without its data, and its ✖ for depreciation
  past a disposal or below the residual value, have nothing to find: the register refuses an asset without its account,
  life or start, or with a residual outside its cost (ASSET-04 to ASSET-07), and straight-line depreciation stops at the
  residual value and at a disposal (ASSET-17); the register against its accounts is the close's `assets`.

## 4. Controls and the platform (slice 4)

- [x] 4.1 The access review (D10):
  - migration `access_review_attestation`;
  - `getAccessReview` and `attestAccessReview` (Owner, step-up, If-Match, CHECK-03), the CSV logged as an export;
  - the page `/settings/access-review` (`AccessReview.html`), its help key `settings.access-review`, and the guide
    page `access-review.md` compiled into `kb/`.
  Tests: the report as each role; attest once per quarter. `// req: FR-CTL-06`, `// req: SEC-06`
  *As built:* migration 000196 (the attestation, FY247; `member_sign_ins()`, each active member's last sign-in and
  second factor, read through the function only) and 000197 (its validation). The ETag covers who holds what, not
  sign-ins, so the Owner's own step-up before attesting keeps it. An attestation answers for the quarters before it.
  The integration clients are the installed connectors until Connected apps arrive (AI-46). `exportAccessReview`
  is audited `access_review.exported`. The rules of the quarters are `internal/domain/accessreview`. Tests:
  `TestTheAccessReview`, `TestAnAccessReviewIsAttestedOncePerQuarter`, the page's `accessReview.test.tsx`.
- [x] 4.2 C.2 #4 self-approved items not reviewed within 14 days (FM-3); #5 SoD conflicts not covered by the team
  mode; #6 external users past their expiry still active; #7 access review overdue (rev 0.162).
  `// req: C.2 #4` … `// req: C.2 #7`
  *As built* (rev 0.165, the owner's decisions of 2 Nov 2026, design D13): C.2 #6 is an active member whose roles
  have all ended, fixed by **removing the member** (`removeMember`, new: FM-1 as a role change, never the last Owner,
  FY248; a removed person may join again) or by giving a role again with a new end date, which now ends the expired
  assignment first (it held the role's one live slot, so this was refused before). C.2 #4 and #7 are reviewed only in
  the review log and the access review (`ReviewedIn`); `reviewCheckItem` refuses them (CHECK-04). C.2 #5 is a
  backstop: the API refuses an uncovered conflict, so its fixture assigns the role through the store. Tests:
  `TestTheControlsChecks`, `TestRemovingAMember`, `TestAMemberIsRemovedAndMayJoinAgain`.
- [x] 4.3 C.2 #11 vendor bank details changed within 30 days with payments pending; #39 a cash advance not settled
  by the claim deadline. `// req: C.2 #11`, `// req: C.2 #39`
  *As built:* C.2 #11 takes a vendor's change approved in the last 30 days (its request's decision, the application's
  clock) while a posted bill of the vendor is still open, "added" or "changed" as it replaced an account or not; a
  person's own account is FR-AP-22's, not this check's. C.2 #39 reads `claim.AdvanceWarnings`, the advance's own ⚠.
  Test: `TestThePaymentChecks`.
- [x] 4.4 C.2 #12 a scheduled job failed or late; #16 a statutory pack past its review date or older than the
  latest; #30 completed activities missing recommended evidence; #31 an evidence hash failed re-verification; #37 the
  sending domain fails SPF, DKIM or DMARC; #38 an integration failing its health check or its credentials expiring
  within 14 days. `// req: C.2 #12` … `// req: C.2 #38`
  *As built* (rev 0.166, the owner's decisions of 2 Nov 2026, design D14):
  - C.2 #12 reads the scheduler's heartbeats (the runner's `Platform`) for the jobs of the recurring documents and their
    releases, depreciation, the revaluation and the bills-due reminders, each against its schedule's `Stale`; a failed run
    beats nothing, so it shows once its time passes.
  - C.2 #16: of each pack activated, the version in force past its review date, and a later version loaded and not
    activated.
  - C.2 #31 and #37 read the night's outcome from the organisation's own trail (design D6): `evidence.Verify` and
    `email.CheckDomains` now record it in each organisation's (`evidence.verification.completed`,
    `email.domains.checked`). C.2 #37 adds the 30 days' rates: 2% hard bounces or 0.1% complaints with 50 or more sent,
    any complaint with fewer.
  - C.2 #38: an enabled connector whose last test failed, and a current secret expiring within 14 days, from the new
    optional `expires_on` of `setOrganisationConnectorSecret` (migration 000198; never changed, FY208).
  - **C.2 #30 moved to `p1-documents-evidence`** (its task 3.1): it needs the evidence rules (FR-DOC-06), not built yet.
  - A fix on the way: C.2 #3 read `audit.verified`, but the nightly verification records
    `audit.verification.completed`, so it would never have left "not assessed"; it now reads the audit package's name
    (regression test `TestTheAuditChainCheckReadsTheNightlyVerification`).
  Tests: `TestThePlatformChecks`, `TestTheEmailRates`, the connector test's expiry, the evidence test's C.2 #31, the
  email adapter's counts, the domain job's records.

## 5. Finish

- [x] 5.1 Republish the canvases `core` (Checks, Home), `settings-admin` (Access review) and `roles` (the auditor
  workspace moved). The Checks and Home fragments are checked against the build.
  *Done (2 Nov 2026):* settings-admin republished (Access review added, Users and roles, and three screens changed
  since its last publish; Auditor workspace removed, as it moved), roles republished (Auditor workspace added); core
  was already current: its Checks and Home artboards match the build.
- [x] 5.2 The review register, for the checks whose rules were decided without an accountant (their own PR).
  *Done:* #358 adds the pending area `checks` (C.1 #5's ⚠ for an account with no cash-flow line, C.2 #48's input
  tax to repay after 12 months), stacked on #347; the Checks page shows its `ReviewBanner`.
- [x] 5.3 `make check`, `make test`, `make test-e2e` (a journey: a check fires, its item opens, it is fixed and turns
  OK); archive.
  *So far:* the journey is `e2e/checks.spec.ts` (C.2 #6's item opens Members and roles, the person is removed, the check
  is OK); `make test-e2e` passes. Done: slices 4a and 4b merged as #359 and #360, each through `make test`; archived on 2 Nov 2026.
