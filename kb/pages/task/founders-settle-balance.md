---
key: founders.settle-balance
type: task
title: Settle what the company owes a founder
summary: The five ways to settle a founder's balance, the recommended default (keep as a loan, convert at the first priced round) and the steps
sources:
  - docs/user-guide/founders-money.md sha256:b2ff34f027f280effc67d5d0c2c074e13386e8313c5a1cb9365a4c074b7f2629
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-13
---

# Settle what the company owes a founder

## Recommended default

- Keep the balance as an **interest-free loan, repayable on demand**, recorded by a board
  minute; at the **first priced funding round**, convert it into ordinary shares **at the round
  price**. (src: docs/user-guide/founders-money.md#the-recommended-defaults)
- Why: the founder keeps the claim, nobody's ownership moves at an arbitrary price, and
  investors see a clean balance sheet at the round. (src: docs/user-guide/founders-money.md#the-recommended-defaults)
- It is a setting: Settings → Founders' money → Default settlement route.
  (src: docs/user-guide/founders-money.md#the-recommended-defaults)
- Background: [[founders.balance]].

## The five options

| Option | What happens | Watch out for |
|---|---|---|
| Repay | The company pays the founder back in cash | Not income for the founder; founders are rarely repaid from a new round's money, and grant money can't be used for it |
| Keep as a loan | The amount stays owed | Investors will ask; usually converted or waived at the round |
| Convert to shares | New shares are issued and the debt pays for them | Needs the shareholders' approval and ACRA filings; the shares exist from the date ACRA registers them, never backdated |
| Waive | The founder forgives the amount | Every shareholder gains in proportion; the company may owe tax if it had deducted the amount, for example unpaid pay |
| Convert to a SAFE or note | The amount becomes shares at the next priced round | Common at seed; included in the round's dilution |

(src: docs/user-guide/founders-money.md#the-five-ways-to-settle-what-the-company-owes-you)

## Steps

1. Equity → *Money I put in* → **Settle**, then choose an option. (src: docs/user-guide/founders-money.md#steps)
2. The other founder approves; if every founder benefits, the accountant also reviews.
   (src: docs/user-guide/founders-money.md#steps)
3. Attach the resolutions and the agreement. (src: docs/user-guide/founders-money.md#steps)
4. For a conversion: after approval the company secretary files with ACRA; attach the ACRA
   acknowledgement; Fyniq posts the conversion on the date ACRA registered it.
   (src: docs/user-guide/founders-money.md#steps)

## Messages and fixes

- **⏰ Share-issue approval not filed with ACRA**: file the shareholders' approval within 14 days
  of the resolution and attach the acknowledgement. (src: docs/user-guide/founders-money.md#common-problems)
- **✖ Required evidence missing**: attach the resolution, agreement or ACRA acknowledgement the
  step lists. (src: docs/user-guide/founders-money.md#common-problems)
- Pay-related messages: [[founders.pay-choice]].
