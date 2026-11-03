---
key: founders.pre-incorporation-costs
type: task
title: Costs a founder paid before the books start, and the accountant's decision on each
summary: Record a founder's cost from before the books start with its receipt; the accountant decides treatment, account and GST; it posts to their 2500
sources:
  - docs/user-guide/pre-incorporation-costs.md sha256:72f758772d66f67f8562cf74cc5943797c4a8953eafa3b0ebcdb9d63292ec020
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-03
aliases:
  - costs before incorporation
  - pre-incorporation costs
  - costs before the books start
  - incorporation fees
  - paid before the company existed
  - accountant's decision
---

# Costs a founder paid before the books start, and the accountant's decision on each

## What it is

- Accounting › **Costs before incorporation** keeps the costs a director or shareholder paid
  themselves before the books start, each with the date on its receipt.
  (src: docs/user-guide/pre-incorporation-costs.md#what-it-is)
- Nothing posts until the accountant decides it; once decided, the cost is owed back to the person
  who paid it on their director's account, 2500 Loan from director / shareholder, as a cost paid
  personally. (src: docs/user-guide/pre-incorporation-costs.md#what-it-is)
- General information, not tax advice: the accountant decides each cost.
  (src: docs/user-guide/pre-incorporation-costs.md#what-it-is)
- The person's account and statement: [[founders.director-accounts]]; their own view:
  [[founders.balance]].

## Record a cost

- An Owner or the Controller selects **Add a cost** and enters the date on the receipt (before the
  books start), who it was paid to, what it was for, who paid it, the amount and any GST shown.
  (src: docs/user-guide/pre-incorporation-costs.md#record-a-cost)
- Attach the receipt or invoice: the accountant cannot decide a cost without one.
  (src: docs/user-guide/pre-incorporation-costs.md#record-a-cost)
- It shows **Needs decision**, and can still be changed, or its file taken off, until decided.
  (src: docs/user-guide/pre-incorporation-costs.md#record-a-cost)

## The accountant's decision

- **Treatment:** deductible (a revenue cost from up to 12 months before the business started),
  capital (such as incorporation costs or equipment), or not deductible; founders' own costs from
  before incorporation are for the tax agent to confirm.
  (src: docs/user-guide/pre-incorporation-costs.md#what-the-accountant-decides)
- **The account** it posts to, and **whether its GST is claimed**: only while GST-registered; GST
  paid before registration is claimed in the first GST return only where IRAS's rules allow it,
  such as services within 6 months before registration or goods still held, otherwise it stays in
  the cost. (src: docs/user-guide/pre-incorporation-costs.md#what-the-accountant-decides)
- The person who paid a cost never decides it, even as the accountant.
  (src: docs/user-guide/pre-incorporation-costs.md#what-the-accountant-decides)

## When it posts

- At once on the decision: the chosen account is debited and the person's account on 2500 credited
  with what they paid, on the books start, or on the day of the decision when the books start's
  month is closed. (src: docs/user-guide/pre-incorporation-costs.md#when-it-posts)
- A decided cost changes no more; correct one with a journal to the person's director's account.
  (src: docs/user-guide/pre-incorporation-costs.md#when-it-posts)
- The page lists every cost by status, and what each person's costs come to, decided and waiting.
  (src: docs/user-guide/pre-incorporation-costs.md#what-the-page-shows)

## Messages and fixes

| Message | Fix |
|---|---|
| The cost is not before the books start | Record a later cost as an expense claim or a bill |
| The organisation has no books start | Set it in Settings › Organisation first |
| The person is not a director or shareholder | Mark the contact as one, or choose another |
| The cost has no receipt attached | Attach the receipt or invoice, then decide it |
| You paid this cost, so another person decides it | Ask the accountant to decide it |
| The organisation was not GST-registered on the posting date | Leave the GST in the cost |
| The cost is decided and posted, so it changes no more | Correct it with a journal to the person's director's account |

(src: docs/user-guide/pre-incorporation-costs.md#common-problems)
