---
key: claims.approve-team-claims
type: task
title: Approve or return members' expense claims, and set the expense policy
summary: Who approves a member's claim, how to approve or return it with a comment under Purchases › Expense claims, and how the expense policy is set
sources:
  - docs/user-guide/claims-approve-team.md sha256:8a3cbda567ce3ed1d33bba558e9373484493f46dfb3f44703b3f59539d754b4f
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-26
aliases:
  - approve a claim
  - return a claim
  - to approve
  - expense policy
  - claim categories
---

# Approve or return members' expense claims, and set the expense policy

## Who approves a claim

- Another person approves a member's claim before the company pays it back: until Fyniq knows each
  person's line manager from the people records, someone who approves expense claims, such as an
  Approver; the Controller too when a line is above its category's limit or past the claim
  deadline. Nobody approves their own claim. (src: docs/user-guide/claims-approve-team.md#what-it-is)
- The Approver and the Controller hold the right; approving needs the person to confirm it's them,
  with a passkey or by signing in again. (src: docs/user-guide/claims-approve-team.md#before-you-start)

## Approve or return a claim

- **Purchases** › **Expense claims** › **To approve** lists the claims waiting for the person; open
  one to check its receipts, its lines and the expense policy's outcome (**Review** lines were sent
  with a reason).
- **Approve** shows exactly what is signed. **Return with a comment** says what to fix; the claimant
  sees it in My claims, fixes and submits again, and each version is kept. The last approval posts
  the claim. **All claims** lists every member's claims by stage, for those who see them all.

(src: docs/user-guide/claims-approve-team.md#approve-or-return-a-claim)

## Set the expense policy

- **Settings** › **Expense policy**: **Load the starter categories** when there are none, then check
  each. A category has the account its claims cost, the most a line can be, the amount above which a
  line needs its receipt, and the amount above which a meal names who attended; one is the mileage
  category.
- The claim deadline, the rates per km for a car and a motorcycle, and the account cash advances
  post to are organisation settings under **Settings** › **Organisation**, changed with a second
  person's approval; see [[settings.organisation]].

(src: docs/user-guide/claims-approve-team.md#set-the-expense-policy)

## Common problems

- Your own claim: another person approves it. A line above its limit: the Controller approves it
  too. A receipt on another record: check the claimant's reason first. A finance link showing "not
  part of your access": it is outside your rights. The claimant's side: [[claims.make-a-claim]].

(src: docs/user-guide/claims-approve-team.md#common-problems)
