---
key: banking.payouts
type: task
title: Check a payment provider's payouts and settle a lost dispute
summary: Read each Stripe or HitPay payout against its clearing account, deal with parked events, and write off or credit a lost dispute
sources:
  - docs/user-guide/banking.md sha256:7236d356783239a6e775152324b8b6ebf951d28970208349fbc0d113aea621e8
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-19
aliases:
  - payouts
  - provider payouts
  - stripe payout
  - hitpay payout
  - nets to zero
  - clearing account
  - lost dispute
  - chargeback
  - parked event
  - dispute credit note
---

# Check a payment provider's payouts and settle a lost dispute

## Steps

- **Payouts** (Expert mode, under Banking) shows what a Stripe or HitPay connector brought into the provider's clearing account and proves it comes to nothing once the provider has paid out ([[settings.integrations]]).
- Each provider's card shows the clearing balance the books hold, the balance the provider says it still holds for you, whether they agree, and how far the hourly sweep has made the account complete; a failed sweep shows its reason.
- Each payout is broken into the charges, refunds, disputes and fees it settles, with the gross, fee and net of each and the amount that reached the bank. **Nets to zero** means nothing is left in the clearing account; an amount **left** is a ✖ at the top of the page, and the next sweep fetches what the provider has since reported. A payout **waiting for its bank line** has not been checked yet.
- The page adds nothing up: every figure is the server's.
- **Events waiting for a person** are events Fyniq could not place. A parked event posts nothing; **Mark as dealt with** records what you did or why nothing is needed, in the audit trail.

(src: docs/user-guide/banking.md#payment-provider-payouts)

## A lost dispute

- The provider takes the disputed money and charges a fee; both sit in disputes clearing until settled, once per dispute, for an open dispute whose lines have all posted.
- **Write it off** prepares a draft journal to the dispute loss account that you post under Journals with its own approval. Up to the limit `psp.dispute_expense_limit` (S$100 by default) that is all; above it you confirm that the invoice's revenue stays as invoiced and give a reason (accountant to confirm).
- **Settle a credit note** is for a dispute above the limit whose invoice is known: issue the credit note under Sales first, then its credit, up to the amount disputed and in the company's currency, clears against the money the dispute holds (Dr accounts receivable, Cr disputes clearing). The dispute fee stays held, to write off with **Write it off**. The credit note's GST is for the accountant to confirm.
- A journal never touches accounts receivable by hand; the credit note is how the customer's balance is corrected.

(src: docs/user-guide/banking.md#a-lost-dispute)
