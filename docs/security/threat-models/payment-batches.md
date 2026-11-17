# Threat model: payment batches

| | |
|---|---|
| Feature and requirement IDs | Preparing, approving and releasing a payment batch, its payment list, its payments posted and matched, and remittance advice: FR-BNK-12, FR-AP-13, FR-AP-22, G4-055, SOD-1, SOD-2, FM-1, FM-3, SEC-11, AUD-02 (PRD-001 rev 0.154 and rev 0.156; `p1-payments-out`); the money account's payment file, which chooses the bank's own file (rev 0.157; `p1-payment-files`) |
| Author, reviewer, date | Claude Code for the founders, 28 Oct 2026; the payment files added 17 Nov 2026; second review at sign-off (waived before production, AGENTS.md); money movement, so a co-founder review (#6) before production |
| Data and its class | Payees' full account numbers, opened only while the payment list is written (restricted: sealed, never logged); the payment list (restricted: sealed at rest, downloaded on request); amounts, payees' names, dates and references (general); remittance advice PDFs (general) |
| Who acts | The Accountant and the AP specialist, who prepare a batch and ask for it (C on Appendix D's "Payment batches"); the Controller, who approves it (A); the Payment releaser, who releases it and downloads its list (R); in Founder mode the founders hold these roles; the API's role `fyniq_api`; the worker, which sends remittance advice; the field keys (`fields`) |

## 1. What we are building

A batch pays approved bills and expense claims whole, from one SGD bank account on one date, by FAST or GIRO, to the
payees' Singapore accounts in force: one payment per supplier and one per claimant. The money account it pays from says
which file its release writes (rev 0.157): the payment list by default, for any bank; Wise's batch payment file for a
Wise account; DBS IDEAL's UFF for a DBS account. A bank's file is offered only for an account at that bank (its
statement connector's provider, else its institution matched against Fyniq's list), and only once it is built; until
then the payment list is the only file written. Whoever may change the money account changes the setting. A person prepares it; another
approves it through the approval engine, whose envelope holds the batch's total and each payee's account in force; the
Payment releaser, stepped up, releases it. The release posts each payment as recording one by hand does, writes the
payment list (a CSV with full account numbers, until the bank is chosen and its own file is built, rev 0.156), seals the
list into the release row with the SHA-256 of the file, and links each payment. The releaser downloads the list, keys it
into the bank and deletes it. Bank import matches each payment to its statement line; a supplier's match sends its
remittance advice.

Trust boundaries:
- **person ↔ API**: the Appendix D row, step-up, the second person, the team mode's separation;
- **API ↔ database**: `fyniq_api`, RLS, `payment_batch_release_guard`, `payment_batch_payment_guard`,
  `payment_batch_holds`, `payment_batch_approved`;
- **API ↔ KMS**: the payees' numbers opened and the list sealed by the api's account alone;
- **person ↔ bank**: the downloaded list, keyed into the bank by a person, or the bank's own file uploaded to it;
- **worker ↔ email**: the remittance advice, through the outbox.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| B1 | person ↔ API | Elevation: one person prepares, approves and releases a batch, paying whom they like | Money paid with no second person | Medium |
| B2 | person ↔ API | Tampering: a payee's account is changed after the batch is approved, so the release pays an account the approver never saw | A payment diverted | Medium |
| B3 | person ↔ API | Tampering: a bill in a batch is also paid by hand, or is in two batches | Money paid twice | Medium |
| B4 | API ↔ database | Tampering: a release written for a draft batch, a second release, a release or its file changed, a cancelled release | Payments posted that nobody approved, or records that no longer show what was paid | Low |
| B5 | person ↔ bank | Tampering and disclosure: the downloaded list is edited before it is keyed in, or kept and leaked | A payment diverted; account numbers exposed | Medium |
| B6 | person ↔ bank | Tampering: a payment keyed with a wrong account or amount | Money to the wrong account | Medium |
| B7 | API ↔ database, logs | Information disclosure: account numbers in a response, a log, the audit trail or the release row in clear | Bank data exposed | Low |
| B8 | worker ↔ email | Repudiation and spam: remittance advice sent twice, or for a payment the bank rejected | Confusion with suppliers; a wrong record of payment | Low |
| B9 | person ↔ API | Information disclosure: a spreadsheet runs a payee's name as a formula when the list is opened | Code run on the releaser's machine | Low |
| B10 | person ↔ API | Tampering: the money account's payment file is changed between approval and release, or to a file its bank cannot read | A file the bank refuses, or a file whose content differs from what was approved | Low |
| B11 | person ↔ API | Repudiation: nobody can say which file a released batch gave, or who changed the account's setting | A payment disputed with no record of its instruction | Low |
| B12 | API ↔ bank | Tampering and disclosure: a bank's own file carries account numbers to one more place (Wise, DBS IDEAL), or its writer misreads the bank's layout | Bank data exposed; a payment to the wrong account or of the wrong amount | Medium |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| B1 | Approval through the engine by another person who holds A, never the requester or the preparer (FY101, PB-21); the database posts a batch only under such a request (FY242). In Standard mode the releaser is a third person (SOD-2, PB-19); in Founder mode the preparer or approver may release, after the other's approval, and it is logged for the weekly review (FM-3) | `TestAPaymentBatchIsApprovedThenReleased`, `TestAStandardModeReleaseNeedsAThirdPerson` (`// req: SOD-2`, `// req: FM-1`), `TestAttackPaymentBatchesKeepTheirRelease` (`// asvs: V8.2.2`) |
| B2 | The approval's envelope holds each payee's account in force and the total; the release refuses unless the latest approval's still match (PB-17), and the re-approval shows the new account (G4-055). A payee whose change waits is held (PAYEE-12). In Standard mode the releaser never prepared or approved a payee's account change (SOD-1) | `TestAPaymentBatchIsApprovedThenReleased` (`// req: G4-055`), `TestAStandardModeReleaseNeedsAThirdPerson` (`// req: SOD-1`), `TestAPaymentBatchChecksEveryItem` |
| B3 | A bill or claim in another approved batch is refused (PB-11); a payment by hand of one in an approved, unreleased batch is refused at commit (FY241); the release posts each payment through the rules that refuse paying more than is open | `TestAPaymentBatchChecksEveryItem`, `TestAttackPaymentBatchesKeepTheirRelease`, `TestAPaymentBatchIsApprovedThenReleased` |
| B4 | `payment_batch_release` is written once for an approved batch at its current version and never changed, deleted or truncated, even by the table's owner (FY239); a link names a posted payment of the batch's account and date (FY240); a released batch is never cancelled (FY239) | `TestAttackPaymentBatchesKeepTheirRelease` (`// req: QA-ENG-05`, `// asvs: V2.3.5`) |
| B5 | The list is sealed (SEC-11) bound to the batch, with the SHA-256 of the file shown on the batch; it is downloaded only by the releaser, stepped up, and each download is audited with the hash; the guide asks for the list to be deleted once keyed | `TestAPaymentBatchIsApprovedThenReleased` (`// asvs: V14.2.4`) |
| B6 | The bank's count and total are compared with the list's last row before authorising in the bank (the guide); bank import matching leaves a payment of a wrong amount unmatched. A wrong account with the right amount is not caught until the bank's own file replaces the keying (rev 0.156) | The guide `purchases.payment-batches`; [live-verify] with the first batch |
| B7 | Numbers are opened only inside the release's transaction; the list is sealed; the audit trail and the log carry the file's name and hash only | `TestAPaymentBatchIsApprovedThenReleased` (the trail and the log hold no number) |
| B8 | One remittance job per payment, keyed by it, however often the match is undone and done again (Outbox.ByKey); the worker sends nothing for a payment voided since, sent already, or a supplier with no remittance address. The worker's database role keeps advice, records a send and queues remittance email only for a posted bill payment a released batch recorded and a live match ties to the bank (`remittance_allowed`, FY201, FY202; migration 000189) | `TestRemittanceAdviceIsSentWhenTheBatchPaymentMatches` (`// req: QA-ENG-05`, `// asvs: V8.2.2`), run on the worker's role |
| B9 | A cell that starts like a formula is written with a leading apostrophe (CWE-1236) | `TestThePaymentListIsWrittenToTheByte` |
| B10 | Every file is written from the same approved data: the payees, accounts and amounts the latest approval holds (PB-17), so the setting changes the layout and never what is paid. A bank's file is accepted only for an account at that bank and only once built (PAYFILE-01, PAYFILE-02); a change of bank that would leave such a file is refused; release reads the setting as it writes the file and refuses one the bank no longer takes or Fyniq no longer writes, releasing nothing. The database names the known files (`money_account_payment_file_known`) | `TestAMoneyAccountSaysWhichFileItsBatchesGive` (`// asvs: V2.3.1`), `TestEachBankTakesItsOwnFile` |
| B11 | The release keeps the writer's name and version and the file sealed, never rewritten (FY239), so a batch released before a change keeps its file; the setting's change is audited with the old and new value (AUD-01) | `TestAMoneyAccountSaysWhichFileItsBatchesGive` (`// req: AUD-01`, `// asvs: V16.2.1`) |
| B12 | Each bank's file is sealed and downloaded like the list (B5, B7). Each writer is versioned and written only from the bank's own specification, never from unofficial copies (rev 0.156), pinned field by field by a test, and verified by a real upload before use: Wise's from its blank template (task 2.2), DBS's from "DBS UFF Version 2.2" (task 3.2). The bank refuses a file it cannot read before anything is paid | Waits for the specifications: `p1-payment-files` tasks 2.1, 3.1 [live-verify] 2.2, 3.2 |

## 4. What remains

- The bank's own file, which removes the keying: the setting is built (rev 0.157), but Wise's batch payment file waits
  for the owner's blank template from Wise's template builder, and DBS IDEAL's UFF for "DBS UFF Version 2.2", which sits
  behind the IDEAL login (DBS's public page names it and its ISO 20022 changes, not its FAST or GIRO records). Until they
  are built, every account gives the payment list, and B6 stays as it is.
- The setting is not part of the batch's approval: changing it between approval and release changes the file's layout,
  never the payees, accounts or amounts, and is audited. When a bank's file is built, its writer name (`dbs-uff@2.2`)
  needs the release's writer check widened, which accepts `name@integer` today (migration 000185).
- A payment voided between its match and the worker's run is skipped by the worker; there is no test of that window.
- A remittance job that fails five times is dead: nothing retries it, and its key stops a second job for the payment.
  The batch shows **Remittance not sent** (`TestARemittanceThatGaveUpShowsAsFailed`) and the guide says to tell the
  supplier another way; there is no dead-letter replay yet.
- Auto-reconcile leaves a batch's payments to a person to match, as it does any payment recorded by hand.
- Salaries in batches arrive with `p1-payroll-link`; the IR21 hold with them.
- The first live batch is [live-verify]: the list keyed into the bank, the count and total compared, the statement lines
  matched.
