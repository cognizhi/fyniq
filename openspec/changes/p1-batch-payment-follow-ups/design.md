# Design

## Context

- **Auto-reconcile** (`internal/domain/banking/auto.go`) runs after an upload or a person's **Reconcile now**, as the
  system principal, within the limits `bank.auto_cap_line` and `bank.auto_cap_rule_day`. It reconciles a transfer in
  transit, one open invoice or bill named by number with its contact, and an approved rule's hit; anything else stays
  in the queue, with why.
- **A released batch** posted each payment on release (Dr 2010 or 2150 · Cr the money account). Its payment carries
  the reference the payment list wrote: the batch's number and the documents it pays (`PB-0001 BILL-0920 …`). A person
  matches the line to the payment with `match-recorded`, which queues the supplier's remittance advice once.
- **The advice job** (`payments.remittance`) is an outbox row with the business key `remittance:<payment>`. After its
  attempts it is dead, and the batch shows the payment's advice as failed.

## Decisions

### D1 A batch's payment, matched by itself

- **Candidates:** for a debit line on a money account, the released batches paid from that account whose number the
  line's text names as a word (its description, reference or counterparty); among their payments, those not yet
  matched whose amount and currency are the line's.
- **Exactly one, and nothing else:** the line is reconciled only when there is exactly one such payment, and no open
  document or transfer the existing rules would also match. Two payments of the same amount in a batch, or a payment
  and an open bill, leave the line in the queue as ambiguous.
- **Limits:** the line's size counts against `bank.auto_cap_line` like any other; a batch's payment is no rule, so
  `bank.auto_cap_rule_day` does not apply.
- **The match:** the same as a person's `match-recorded`, made by the system principal, with the basis
  `batch_payment`. It queues the remittance advice as a person's match does, appears in the Auto-reconciled list, is
  undone while the period is open, and can be drawn into the weekly sample (FM-3).
- **A batch's total** on one line is never matched by itself; a person matches it.

### D2 Send again

- **Who:** a person who can prepare the batch (`PaymentBatches`, Create), as releasing needs no less; the AI and agents
  never.
- **When:** only for a payment whose advice is failed (its job dead), never one sent, waiting or not wanted.
- **What:** one new job for the payment, audited (`remittance.resent`, who and why). The sender's checks hold: a
  payment voided since, or one with an advice sent, sends nothing.
- **Once:** the new job has a business key of its own, and the advice is still kept and sent at most once a payment.

## Risks

- **A wrongly keyed reference** naming another batch's number: the amount must also be that payment's alone, and a
  wrong match is undone, logged and sampled.
- **Resending to a supplier who already heard** another way: Send again is a person's choice on a failed advice
  only.
