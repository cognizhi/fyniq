# Threat model: the scheduler's postings and drafts

| | |
|---|---|
| Feature and requirement IDs | Automatic reversals and recurring journals: FR-GL-02, FR-AUT-01, FR-AUT-02, SOD-3, FM-1, FM-2 (PRD-001 rev 0.55, the founders' decisions of rev 0.58, rev 0.59 and rev 0.60); the month-end revaluation: FR-GL-04 (rev 0.75, the founder's decision of 22 Oct 2026) |
| Author, reviewer, date | Claude Code for the founders, 21 Oct 2026, revaluation 22 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | Journal amounts, accounts and memos (confidential); no personal data, bank numbers or secrets |
| Who acts | The platform jobs `journals.auto-reverse`, `journals.recurring` and `fx.revalue`, as the principals `scheduler:journals.auto-reverse`, `scheduler:journals.recurring` and `scheduler:fx.revalue`, with the worker's database role `fyniq_worker`; the people who keep recurring journals and post their drafts |

## 1. What we are building

The tick (Cloud Scheduler; locally the API's loop) starts the platform jobs through the outbox. Hourly,
`journals.auto-reverse` posts the reversal of each posted journal whose reversal date has come, and
`journals.recurring` makes the draft journal of each recurring journal's occurrence that has come. Daily,
`fx.revalue` revalues each ended, open month that is due, once its last day's rates are in, and posts it: unlike
a recurring journal nobody writes its content, which the engine works out from the ledger and the rates (the
founder's decision of 22 Oct 2026). All three run in the worker with `fyniq_worker`, which also sends email and
finalises evidence.

Trust boundaries: **tick ↔ worker** (an authenticated task, SEC-12); **worker ↔ database** (the
`fyniq_worker` role, RLS per organisation); **draft ↔ person** (a person posts a scheduler's draft
under the usual approvals: nothing recurring posts by itself, the founders' decision of 21 Oct 2026).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | worker ↔ database | Spoofing: a fault in the worker saves a document as a person, so SOD-3 is fooled | A journal prepared by "a person" who never saw it | Low |
| T2 | worker ↔ database | Tampering: the worker writes arbitrary journal content, or posts it | Wrong figures posted without a person | Low |
| T3 | worker ↔ database | Tampering: the worker marks a posted document reversed without reversing its entry | The document and the ledger disagree | Low |
| T4 | worker ↔ database | Tampering: the worker rewrites a recurring journal's content or schedule | Every later draft is wrong | Low |
| T5 | draft ↔ person | Elevation of privilege: the author of a recurring journal approves the posting of its drafts | SOD-3 bypassed through the template | Medium |
| T6 | the job | Repudiation: nobody can tell which job did what | Unexplained postings | Low |
| T7 | the job | Denial of service: a lagging or failed scheduler leaves drafts or reversals unmade | Months close without their accruals | Medium |
| T8 | the job | Tampering: a run makes the same draft twice | Double accruals | Low |
| T9 | worker ↔ database | Tampering: the worker saves a revaluation it made up, or one of another period, and posts it | Wrong unrealised FX in a month | Low |
| T10 | worker ↔ database | Elevation of privilege: the revaluation function becomes a general way for the worker to make or post documents | The T1 and T2 controls bypassed | Low |
| T11 | the job | Tampering: the scheduler revalues a month a person already revalued, or one whose rates are not in | Double or wrong revaluation | Medium |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | The worker holds no INSERT on `document` or `document_version`. Its one way to make a document is `create_scheduled_draft`, which saves it as `scheduler:journals.recurring`, never as a person | `TestRecurringJournalsMakeDraftsTheSchedulerCannotPost` (SEC-03; V13.2.2) |
| T2 | `create_scheduled_draft` copies the content from the recurring journal's current version, written by a person, and adds only the date. The worker cannot set `posted_by`, so the database refuses to post the draft (FY053) | The same test: a direct insert answers 42501, a post FY053 |
| T3 | `fyniq_worker` holds `UPDATE (status)` only. `document_reversed_posted` (FY056) refuses "reversed" without the reversing entry, for every role | `TestAttackAnEditReversesAndPostsTogether` (FR-GL-12) |
| T4 | The worker may update `next_on` only. Content versions never change or go (FY058), even for the owner | `TestRecurringJournalsMakeDraftsTheSchedulerCannotPost` |
| T5 | The approval of a draft's post refuses whoever wrote the version it came from, read from `recurring_journal_version`, never from the draft (JOURNAL-09) | `TestTheAuthorOfARecurringJournalDoesNotApproveItsDrafts` (SOD-3) |
| T6 | Each job has its own principal, recorded on every audit event it writes (AUD-01); a waiting reversal is audited too | `TestAutomaticReversals`, `TestRecurringJournalsMakeDraftsTheSchedulerCannotPost` |
| T7 | A waiting reversal warns on the journal, on Home and in the close (C.2 #56). A recurring occurrence more than a day late refuses the close (C.1 #28); an active recurring journal whose content would not post is ✖ on itself and ⚠ at the close (C.1 #29, the founders' decision of rev 0.61) | `TestAWaitingAutomaticReversalShows`, `TestRecurringJournals` |
| T8 | `create_scheduled_draft` makes an occurrence only when it is the recurring journal's next one and after its last (FY057), under the row's lock | `TestRecurringJournalsMakeDraftsTheSchedulerCannotPost` |
| T9 | `create_scheduled_revaluation` saves only an `fx_revaluation` of an open period whose `period_id`, last day and day after are the payload's (FY169). The worker could already append any balanced entry (since 000004, for the reversals); the database's checks still apply to it: balanced, base-only lines only on a revaluation and only on accounts marked revalue against the unrealised-FX account (FY008), nothing in a closed period. The run records what it read, and the replay re-runs `fx_revaluation@1` over it (ENG-06) | `TestTheSchedulerRevaluesAnEndedMonth` (SEC-03; V13.2.2), `TestAttackBaseOnlyLinesComeOnlyFromTheRevaluation` |
| T10 | The function fixes the type, the version (1) and the actor (`scheduler:fx.revalue`); EXECUTE is granted to `fyniq_worker` only; the role still holds no INSERT on `document` and no UPDATE of `posted_by` | `TestTheSchedulerRevaluesAnEndedMonth`, `TestEachRoleHoldsExactlyItsGrants` |
| T11 | The job takes the ledger lock and revalues only a period whose plan is due (balances to revalue and no current run); a currency without a rate for the last day, or no unrealised-FX account, makes it wait with an audit event (`fx_revaluation.waiting`). A rate entered after the run is taken by a person's run, which posts the difference | `TestTheSchedulerRevaluesAnEndedMonth` |

## 4. What remains

- The email and evidence jobs share `fyniq_worker`, so they hold the document permissions above. On
  21 Oct 2026 the founders accepted this until the cloud gate. The scheduler's jobs then move to a
  role of their own (PRD-001 §17.5 item 5), `create_scheduled_revaluation` with them.
- The scheduler posts a revaluation with no person's click, as it posts automatic reversals. The close
  check (C.2 #59) and the Revaluation tab show each run and who made it.
- A recurring journal's content is checked by the posting rules when it is read, not when it is saved;
  an incomplete one is kept and shown, and the close warns of it while it is active.
