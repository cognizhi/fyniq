---
key: review.awaiting-verification
title: Why some pages say "Needs additional verification"
mode: basic
roles: [owner, administrator, controller, accountant, bookkeeper, payroll-administrator, approver]
requirements: [FR-UX-41, NM-UX-06, QA-UI-09]
status: draft            # written ahead of the feature (P1); reviewed when the screens are built
---

# Why some pages say "Needs additional verification"

## What it is

Some rules in Fyniq and Nemura were built before a specialist finished reviewing them. A page
that uses those rules shows a **Needs additional verification** banner at the top. It tells you
which rules are waiting, who reviews them, and what to check before you rely on the page.

## Who reviews what

| The banner says | What is waiting | Who reviews it |
|---|---|---|
| Statutory and payroll rules await review | CPF, SDL, SHG, GST, withholding tax, founders' money, share-based payment, the accounting framework, leave liability and record retention | An accountant or tax agent |
| Approval and payment rules await review | What each role sees, approvals, salary and payment batches, segregation of duties | A second director |
| Sign-in, AI and integration settings await review | Sign-in providers, the first-run setup, connected apps and MCP, AI providers and model routes | A security reviewer |

A page that uses rules from two areas lists both.

## What to do

1. Read the banner. It names the rules and the reviewer.
2. Check what the banner asks before you act: the figures before you rely on them or file them,
   each approval and payment before you release it, or each setting before you turn it on.
3. If something looks wrong, ask your accountant, or the person who looks after Fyniq, before you
   go on.

You can collapse the banner to one line with **Collapse**. You cannot switch it off, and it shows
in Basic and Expert mode alike.

## When it goes away

The banner disappears by itself once the review is finished: the reviewer approves the review,
and the next Fyniq release no longer shows it on that area's pages. Nobody can hide it with a
setting.

## Where you never see it

Sign-in, account recovery and app-consent pages never show the banner, and nor do your own pages
(My claims in Fyniq, and My home, My pay, My leave and Ask in Nemura).

## Common problems

| What you see | What to do |
|---|---|
| A banner on a page you use every day | Carry on, but check what the banner asks before you file, release or switch something on |
| A figure you think is wrong on a page with the banner | Do not file or release it. Ask your accountant, and tell the person who looks after Fyniq |
| The banner is still there after you heard the review is done | It goes with the next release. Ask the person who looks after Fyniq when that is |

## Related

- [Claim back money you spent for the company](claims-my-claims.md)
- [Founders' money: what the company owes you, and the options](founders-money.md)
