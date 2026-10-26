---
key: claims.make-a-claim
type: task
title: Claim back money spent for the company
summary: How a member claims a receipt in My claims, attaches it and submits it, completes company-card lines, and uses a cash advance
sources:
  - docs/user-guide/claims-my-claims.md sha256:b0a341fc9fb85d5414b6d25fd613fcdedd9771cce148fa839d69ddeff5aa368b
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-26
aliases:
  - expense claim
  - reimbursement
  - claim a receipt
  - company card
  - personal spend
  - cash advance
---

# Claim back money spent for the company

## Where claims live

- **My claims** is where a person asks the company to pay back what they spent for it: a receipt,
  or a trip in their own car. It also shows what the company still owes them, the lines of a
  company card they hold, and any cash advance. (src: docs/user-guide/claims-my-claims.md#what-it-is)
- Someone who only claims lands on My claims and sees only their own items, never the company's
  figures. (src: docs/user-guide/claims-my-claims.md#what-it-is)
- From Nemura, the **Claims** tile opens My claims with the same sign-in; **Back to People**
  returns. (src: docs/user-guide/claims-my-claims.md#before-you-start)
- The expense policy sets, per category, a limit per line, the amount above which a receipt is
  needed, and when a meal names who attended; claims are due within the claim deadline.
  (src: docs/user-guide/claims-my-claims.md#before-you-start)

## Claim a receipt

1. **My claims** › **New claim**; say what the claim is for and its date.
2. Each receipt: who was paid, its date, the category, the amount and currency, the GST code (TX
   when it shows GST, NR when the merchant is not GST-registered) and any GST shown.
3. A receipt in another currency converts at the day's rate; what left the person's account in
   Singapore dollars, when entered, is what the company pays back.
4. A meal or entertainment names who attended. **Save draft** keeps it to change later.

(src: docs/user-guide/claims-my-claims.md#claim-a-receipt)

## Attach the receipt

- Under **Evidence** on the saved claim: choose the file, say what it is, **Attach**; Fyniq scans
  it and strips image details such as location. Then pick it on its line under **Its receipt**.
- Above the category's receipt amount a line cannot be submitted without its receipt; a file
  already on another record blocks the line until the person says why it is not a duplicate.

(src: docs/user-guide/claims-my-claims.md#attach-the-receipt)

## Submit and follow it

- Fix what is marked **Fix**; **Review** lines can be sent and the approver sees why. **Submit**
  asks the person to confirm it's them. **Withdraw** makes it a draft again.
- Another person approves it, and finance too when a line is above its limit or past the
  deadline; nobody approves their own claim. The manager's side: [[claims.approve-team-claims]].
- Stages: draft, submitted, returned, approved or paid. **What the company owes you** totals the
  approved claims not yet paid back; a returned claim shows the approver's comment to fix.

(src: docs/user-guide/claims-my-claims.md#submit-a-claim) (src: docs/user-guide/claims-my-claims.md#follow-your-claims)

## Company cards and cash advances

- A card holder's payments show under **Card lines** once finance imports the statement:
  **Complete** each with the category and GST code of its receipt, or as personal spend, then
  attach the receipt. Personal spend is owed to the company until refunded; no claim settles it.
  (src: docs/user-guide/claims-my-claims.md#complete-your-company-card-lines)
- A cash advance shows under **Cash advances**; claims that choose it under **Settles a cash
  advance** settle what is open, anything above is paid back, and what is left is refunded by bank
  transfer. One still open after the claim deadline is flagged.
  (src: docs/user-guide/claims-my-claims.md#cash-advances)

## After approval

- Paid back by bank transfer, later in the next payment batch; a reimbursement of what was spent
  for work is not pay. A claim in another currency is paid back in Singapore dollars.
  (src: docs/user-guide/claims-my-claims.md#what-happens-next)
- Payslips are not in Fyniq: with Nemura they are under **My pay**, otherwise in the payroll
  provider's system. (src: docs/user-guide/claims-my-claims.md#related)
