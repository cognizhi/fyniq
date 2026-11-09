---
key: compliance.risks
type: task
title: Keep the risk register
summary: The risk register - each risk's owner, likelihood, impact, rating, mitigation and review date; adding, changing, closing and reviewing a risk
sources:
  - docs/user-guide/compliance-risks-policies.md sha256:37c6fa625c7b80a04d5ed76d38a51e647fef0a7aafb88d0fb985aa12d796e4a5
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-09
aliases:
  - risk register
  - add a risk
  - review a risk
  - risk review overdue
  - risk rating
  - close a risk
---

# Keep the risk register

## What it is

- **Risks and policies** (from **Compliance**, Expert mode) holds the risk register: each risk with the role that answers for it, its likelihood and impact, how it is reduced, and its next review.
- Each risk's rating, high, medium or low, comes from its likelihood and impact; the highest come first.
- The Owner, the Administrator, the Controller, the Accountant, the Auditor and the Compliance officer see it; the Compliance officer adds, changes and reviews risks ([[members.roles]]).
- A company that runs Fyniq for itself starts with the risks of building and running Fyniq; every other company starts with an empty register. Its controls are on [[compliance.controls]].

(src: docs/user-guide/compliance-risks-policies.md#what-it-is)

## Add or change a risk

- Select **Add a risk**, or **Change** beside a risk; say the risk, choose its **Owner**, **Likelihood** and **Impact**, and say how it is reduced under **Mitigation**.
- Review every 3 months for each quarter; a new risk's first review is the end of the next quarter unless a day is chosen.
- A change saves the next version and keeps the earlier ones, each in the audit trail ([[compliance.audit-trail]]); a risk that no longer applies is closed under **State**, not removed.

(src: docs/user-guide/compliance-risks-policies.md#add-or-change-a-risk)

## Review a risk

- Select **Review** beside a risk that falls due, add a note if wanted, and select **Record the review**; its next review falls its review interval after today.
- An overdue review is called out at the top of the page.

(src: docs/user-guide/compliance-risks-policies.md#review-a-risk)
