# Design

## Context

- **Documents:**
  - A document has a type, a status (draft → posted → reversed) and immutable versions, each naming who
    saved it.
  - Posting records who posted it and when (FY053).
  - The posting service in `internal/domain/journal/` is the one path that appends to the ledger
    (archtest).
  - Journals are the pattern to follow: drafts, statuses, FM-2 approval of a post above the threshold,
    FM-1 for a void, edits that reverse and repost (FR-GL-12), and replay (ENG-06).
- **Journal lines:** they carry account, currency, amount, rate, rate source and base amount. They
  have no supplier column and no dimensions; FR-DIM is not built.
- **Contacts:** vendors have payment terms, a default account, a default GST code, a country and a
  currency (FR-CT-01).
- **Already built:**
  - number sequences per document type (FR-ORG-04);
  - evidence attaches to records (FR-DOC-05);
  - GST codes and rates come from the `sg-gst` pack (TX, IM, ZP, BL, NR, EP, OP for purchases);
  - organisation GST registrations are kept with their dates.
- **From `p1-multi-currency-fx`:**
  - the rate cascade (ENG-40);
  - `ledger.RealisedFX` (ENG-41) and `ledger.ForeignInputTax` (ENG-42);
  - the revaluation of the AP control account by currency, until bills are revalued one by one.
- **Scheduler:** platform jobs run from the tick, under their own principals with the worker's role. The
  worker may append entries and move a document's status. It makes documents only through narrow
  functions (`create_scheduled_draft`, `create_scheduled_revaluation`).
- **Appendix D, "Bills, vendor credits":**
  - Accountant: CP (create and post);
  - Bookkeeper and AP specialist: C (create);
  - Controller: A (approve);
  - Approver: A up to a limit;
  - Owner, Auditor and Tax agent: V (view).
- **"Payment batches"** is its own row (the Releaser's R).

## Goals / Non-Goals

**Goals:**
- A bill is entered, approved, posted, paid, credited, voided and edited with the ledger right at every
  step, and AP's open documents add up to 2010.
- Input tax is what the supplier stated, claimable only on the facts that make it claimable.
- Annual subscriptions spread over their months; recurring bills appear as drafts on time.
- A bill from a non-resident supplier is held until the tax agent decides its withholding tax.

**Non-Goals:**
- Capture and automated bill entry (FR-AP-04, FR-AP-12): `p1-document-capture`.
- Payment batches, bank files and the "in a batch" state (FR-BNK-12): `p1-bank-import-reconcile`.
- The three-way match (FR-AP-01, P2), vendor statement reconciliation (FR-AP-10, P2), supplier contracts
  (FR-AP-21, P2), S45 filing (FR-AP-11, P3), receiving Peppol invoices (FR-AP-05, P2).
- Dimensions on bill lines: with FR-DIM.

## Decisions

The owner decided three of these on 22 Oct 2026; the rest are provisional, on the recommended option,
until the accountant's sign-off. Each one that changes a requirement's text goes into a PRD revision with
the slice that builds it.

- **Order: purchases before sales (the owner, 22 Oct 2026).** See `proposal.md`.
- **Document types:**
  - `bill`, `vendor_credit` and `bill_payment`, with versions;
  - `purchase_order`;
  - recurring bills, a template with versions as recurring journals have.
- **A bill's content**, one payload per version:
  - supplier;
  - the supplier's number;
  - bill date (the tax point);
  - due date: from the terms, the supplier's or FR-ORG-04's 30 days;
  - currency, and the rate (typed or from the cascade);
  - lines: account, description, net, GST code and the stated GST, and an optional prepayment period
    (first month and number of months);
  - the SGD GST as the tax invoice states it, for a foreign bill (ENG-42);
  - the input-tax facts (FR-AP-20) and, for a non-resident supplier, the nature of the payment
    (FR-AP-19);
  - notes.
- **Our number:** from the bill sequence (FR-ORG-04), given when the bill posts. The supplier's number
  is kept as entered and is part of the duplicate check.
- **States:**
  - **approval** (draft, awaiting approval, approved, rejected): from the approval request of its post;
  - **ledger** (not posted, posted, voided by reversal): the document's status;
  - **payment** (unpaid, scheduled for a date, part-paid, paid, written off): from the payments and
    credits applied to it;
  - **due** (not due, due soon, overdue): from the due date and the open amount.
  - Only posting and voiding change the ledger (FR-AR-03's rule, mirrored).
- **Approval as for journals:**
  - a bill whose total in SGD is above the self-approval threshold (FM-2, S$2,000 by default) posts
    only on another person's approval, in any team mode;
  - at or below it, a Founder-mode post is logged as self-approved for the weekly review (FM-3);
  - whoever prepared the version never approves its post (SOD-3);
  - a void needs a second person in Founder mode (FM-1).
- **The posting rule `bill@1`**, per line:
  - the net, at the bill's rate under `RND-FX`, to the line's account, or to 1210 when the line is
    spread;
  - a claimable line's stated GST to 2200;
  - a line whose input tax is not claimable (code BL or NR, a private share, no valid tax invoice, or an
    organisation not GST-registered at the bill date): its GST stays in the line's cost;
  - Cr 2010 with the total.
  - A foreign bill takes `ForeignInputTax`: the claimable GST as stated in SGD, and the difference to
    7030.
  - The stated GST differing from `RND-GST-EXCL` over the claimable net by more than S$0.01 per
    subtotal is ⚠ (G4-024), which the approver acknowledges.
  - `RND-GST-EXCL` (per GST-code subtotal) is built here as a named policy with its property tests.
- **AP sub-ledger:** the open amounts of posted bills, credits and payments by supplier. It adds up to
  2010's balance (INV-07). The check compares the two, and the close shows a difference. Journal lines
  gain no supplier column now; INV-13's person key decides that later for 2500 and 2120.
- **Payments by hand (the owner, 22 Oct 2026):**
  - a `bill_payment` names a money account, a date, an amount in the bills' currency and how it is
    applied to one or more of the supplier's bills;
  - it posts Dr 2010 (each bill's applied amount at that bill's stored rate) and Cr the money account
    (at the payment's rate);
  - realised FX goes to 7030 (`ledger.RealisedFX`, no tolerance);
  - an unapplied amount stays a supplier credit;
  - voiding a payment reverses it and reopens the bills;
  - a residual after FX stays open until question 32 sets tolerances, or is written off with a reason
    (FR-BNK-08).
- **Vendor credits:**
  - a credit's lines post the reverse of a bill's;
  - applying it to a bill of the same currency is an allocation and posts nothing. Across rates, the
    allocation posts the realised FX between the two stored rates;
  - a refund into a money account posts Dr the money account, Cr 2010 against the credit.
- **Prepayment spreads (the owner, 22 Oct 2026):**
  - a spread line posts its net to 1210 on the bill's date;
  - each month from the first month, `ALLOC-EVEN` releases a share: Dr the line's account, Cr 1210,
    dated the month's last day;
  - the platform job `bills.releases` posts each due release up to the organisation's current month
    (ENG-05), from the bill's posted version, with its own posting identity. It needs no new database
    right;
  - a release whose month is closed waits, as automatic reversals do (FR-AUT-02);
  - voiding or editing the bill reverses its posted releases in the same transaction, and the edited
    version's releases start again from its own schedule;
  - this closes `p1-general-ledger-close` task 6.4 for bills; invoices follow with `p1-sales`.
- **Recurring bills (the owner, 22 Oct 2026):**
  - a template: the supplier, its lines and a schedule, as recurring journals have;
  - the platform job `bills.recurring` makes each occurrence's version-1 draft through
    `create_scheduled_bill_draft`. That is a new function with EXECUTE for `fyniq_worker` only, which
    makes a draft bill of an active template's next occurrence, once;
  - the worker cannot post it (FY053), and whoever wrote the template's version never approves the
    draft's post (SOD-3).
- **Purchase orders:**
  - lines, approval under FR-APR-01 (object type `purchase_order`, the built-in policy until the
    organisation sets one), and statuses draft, awaiting approval, approved, billed, closed;
  - "Convert to bill" makes a draft bill of the remaining lines;
  - a PO posts nothing.
- **Non-resident payments:**
  - a vendor gains its tax residence (a country);
  - a bill from a vendor resident outside Singapore records the nature of the payment;
  - a decision (not applicable; withhold at the stated rate; a reduced treaty rate with a certificate of
    residence) is kept per supplier and nature, with the tax agent's advice attached as evidence. It is
    recorded by a member holding C on "Bills, vendor credits", and names the tax agent who gave it;
  - until one exists the bill shows ⚠, and recording a payment of it is refused: FR-AP-19's hold, which
    the payment batch takes over when it exists;
  - withholding posts the gross expense, the net to 2010 and the tax to a withholding-tax payable
    account (a new system role, 2330 in the standard chart, between 2300 and 2400 [accountant to
    confirm the code]).
- **Input-tax eligibility:** each line records its facts:
  - the supplier's GST registration;
  - the document: a tax invoice, a simplified tax invoice, an import permit, or none;
  - addressed to the company, or to an agent the company reimbursed;
  - the business share;
  - whether the expense is disallowed.
  - The condition (claimable, not claimable with its reason, or ⚠ for the tax agent) follows from the
    facts and the organisation's registration at the bill date.
- **Duplicates:** a bill with the same supplier, supplier's number and total as another, dated within
  60 days of it, is ⚠ with a link to the other. FR-AP-12's block is for captured bills, with
  `p1-document-capture`.

## Risks / Trade-offs

- **More automatic postings** (spread releases): mitigated by deriving them from the posted bill alone,
  posting only months that have come, and reversing them with the bill.
- **A wider worker role** (one more function): the same shape as the two the founders accepted. It moves
  to the scheduler's own role at the cloud gate (§17.5).
- **Payments by hand before bank import:** bank import must match, never double-post. FR-BNK-07's
  matching takes a recorded payment as a match candidate. That is noted in its proposal when it is
  written.
- **Not GST-registered:** most of FR-AP-20's conditions decide nothing for Cognizhi today, but the facts
  are recorded now, so registering later needs no re-entry.
