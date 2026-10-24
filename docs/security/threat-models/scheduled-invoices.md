# Threat model: recurring invoices the scheduler issues and sends

| | |
|---|---|
| Feature and requirement IDs | A recurring invoice set to issue and send, approved by a second person, whose invoices the scheduler issues and emails: FR-AR-07, FR-AUT-01, FR-AUT-02, FM-3, SOD-3, FR-AR-06 (PRD-001 rev 0.136; `p1-sales` task 6.1; the owner's decisions of 24 Oct 2026: issue and send only after a second person's approval, and the worker does it, fenced by the database) |
| Author, reviewer, date | Claude Code for the owner, 24 Oct 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Invoices: revenue posted to the ledger, amounts, the customer's name, address and billing email (confidential business data, some personal); the PDF and the email that carry them |
| Who acts | A member with C on "Invoices, credit notes" (sets up the recurring invoice and its mode), another member with A on it (approves its content), the worker as `scheduler:invoices.recurring` (makes, issues and emails each occurrence's invoice) |

## 1. What we are building

A recurring invoice set to **issue and send** has its current content version approved through the approval
engine (object `recurring_invoice`, operation `recurring_invoice.approve`) by someone other than its author. Each
hour the worker's job `invoices.recurring` makes the draft of each occurrence (as it does for any recurring
invoice), then, when the recurring invoice issues and sends and its approved version is the version the draft came
from, issues the draft (posting `invoice@1` as the scheduler) and emails it as `emailInvoice` does: the PDF kept as
evidence, an online link, the email from the invoices sender, and the send. Each invoice it issues is listed for the
weekly review (FM-3). A change of content withdraws the approval: until a new version is approved, the scheduler
makes drafts only.

Until now the worker posted only releases and reversals and queued only notification email; it held no right to
number or post an invoice, to attach evidence, or to queue invoice email. This feature gives it those rights, and
the database confines each of them to the one case: a scheduled draft of an approved recurring invoice.

Trust boundaries: browser ↔ API (setup and approval); worker ↔ database (the new grants and their fences); worker ↔
render service, file store and email providers (as for `emailInvoice`).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Worker → database | Elevation: a compromised or buggy worker issues an invoice a person drafted, or one never approved, using its new grants | Revenue and customer debt posted with no person's approval | Medium without a fence |
| T2 | Worker → database | Elevation: the worker queues email of purpose invoice to anyone, with any text, from the invoices sender | Phishing from the company's own sender | Medium without a fence |
| T3 | API | Spoofing / SOD: the author of a recurring invoice approves their own content, or approves a version and then changes it | One person sets up and releases recurring revenue alone | Medium |
| T4 | Worker | Repudiation: nobody notices the invoices the scheduler issued | Wrong invoices go out unnoticed | Medium |
| T5 | Worker → providers | Information disclosure: an invoice goes to a wrong or stale billing email | A customer's invoice read by someone else | Low |
| T6 | Worker | Tampering: the scheduler issues an occurrence twice, or issues a draft a person changed after it was made | Double billing; an unapproved change goes out | Low |
| T7 | Worker → render, store, providers | Denial of service: the render service or the provider is down | Invoices issued but not sent | Medium |

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | A trigger lets `fyniq_worker` post (status to posted), number, and set the poster of an invoice only when its current version names a recurring invoice set to issue and send whose approved version is the version it came from, and the poster is `scheduler:invoices.recurring` (FY201); its other new grants are fenced the same way | `TestTheSchedulerIssuesOnlyApprovedRecurringInvoices` (attacks as `fyniq_worker` on a person's draft and on an unapproved one) |
| T2 | The worker may queue invoice email only with a send of such an invoice in the same transaction (a deferred check, FY202); evidence, sends and links it writes are fenced to such an invoice | The same test |
| T3 | Approval through the approval engine (signed payload hash, FR-APR-03); never by the author of the version approved (SOD-3); the approval names the version and lapses when the content changes, so a changed recurring invoice makes drafts until approved again | `TestARecurringInvoiceIsApprovedBeforeTheSchedulerIssuesIt` |
| T4 | Each invoice the scheduler issues is a review-log entry (`scheduled_issue`) for the weekly review (FM-3); the audit trail names `scheduler:invoices.recurring`. A draft the invoice rules refuse to issue stays a draft, and those who issue invoices are notified | The same test |
| T5 | The recipient is the customer's billing email at the time of sending; a customer without one gets the invoice issued and not sent, for a person to send | The same test |
| T6 | The draft is made once per occurrence (FY199); the scheduler issues only a draft still at version 1, made by itself, in the run that made it, or sends an issued one that has no send yet | The same test |
| T7 | An issued invoice with no send is tried again by each run for 7 days (`storage.ScheduledSendFor`), then waits for a person, so one that cannot be sent is not retried for ever; a send that fails leaves the invoice issued and listed | The same test |

## 4. What remains

- The worker writes the email's subject and body from the template; the fence checks the recipient and the
  document, not the text. A compromised worker could still word an approved invoice's email differently.
- Opening a link and answering online are as in `document-links.md`.
- Nobody is notified when the scheduler issues an invoice it cannot send (no billing email, or no renderer): it shows
  unsent on the invoice and in the weekly review's list. Surfacing it on the screens comes with `p1-sales` task 8.1.
