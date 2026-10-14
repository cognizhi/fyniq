---
key: review.needs-verification
type: concept
title: The "Needs additional verification" banner
summary: Why some pages show a banner saying their rules await a specialist's review, who reviews each area, what to check, and when it goes away
sources:
  - docs/user-guide/needs-verification.md sha256:15ce0d605755a979f2b6747bc0e0108b0633b9390cfb21810839068c4b3d36a6
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-14
aliases:
  - needs additional verification
  - awaiting review
  - review banner
  - not yet reviewed
  - can I trust this page
---

# The "Needs additional verification" banner

## What the banner means

- Some rules in Fyniq and Nemura were built before a specialist finished reviewing them.
  (src: docs/user-guide/needs-verification.md#what-it-is)
- A page that uses those rules shows **Needs additional verification** at the top, naming the
  rules, the reviewer and what to check. (src: docs/user-guide/needs-verification.md#what-it-is)

## Who reviews each area

- **Statutory and payroll rules**: CPF, SDL, SHG, GST, withholding tax, founders' money,
  share-based payment, the accounting framework, leave liability and record retention; reviewed
  by an accountant or tax agent.
- **Approval and payment rules**: what each role sees, approvals, salary and payment batches,
  segregation of duties; reviewed by a second director.
- **Sign-in, AI and integration settings**: sign-in providers, the first-run setup, connected apps and MCP,
  AI providers and model routes; reviewed by a security reviewer.
- A page using rules from two areas lists both.

(src: docs/user-guide/needs-verification.md#who-reviews-what)

## What to do on a page with the banner

1. Read the banner: it names the rules and the reviewer.
2. Check before acting: the figures before relying on or filing them, each approval and payment
   before releasing it, each setting before turning it on.
3. If something looks wrong, ask the accountant or the person who looks after Fyniq first.

- **Collapse** shrinks it to one line; it cannot be switched off and shows in Basic and Expert
  mode alike.

(src: docs/user-guide/needs-verification.md#what-to-do)

## When it goes away

- It disappears by itself: once the reviewer approves the review, the next Fyniq release no
  longer shows it on that area's pages. No setting hides it.
  (src: docs/user-guide/needs-verification.md#when-it-goes-away)
- Never shown on sign-in, account recovery or app-consent pages, or on a person's own pages (My
  claims in Fyniq; My home, My pay, My leave and Ask in Nemura).
  (src: docs/user-guide/needs-verification.md#where-you-never-see-it)

## Common problems

- **A figure looks wrong on a page with the banner**: do not file or release it; ask the
  accountant and tell the person who looks after Fyniq.
- **Still there after the review finished**: it goes with the next release.

(src: docs/user-guide/needs-verification.md#common-problems)

## Related

- Claims: [[claims.make-a-claim]]. Founders' money: [[founders.balance]].
  (src: docs/user-guide/needs-verification.md#related)
