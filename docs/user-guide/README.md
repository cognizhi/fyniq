# User guide: Fyniq

This folder holds the source of the in-app user guide (PRD-001 §7.9, FR-UX-32). Each Markdown page
is built into the web app at `/help/<page>` and released with the platform. There is no external
help site and no separate CMS. Status: **pre-P0**. Most pages below are planned; the ones that
exist are marked ✔.

## How the guide works

- **One page per module and per common task**, written for the person doing the work. Basic-mode
  pages use plain words. Expert pages may use accounting terms, linked to the glossary.
- **Every page has a help key** in the feature registry (FR-UX-31). The same key drives the
  page's tooltips, its "Learn more" links and its tour step, so the four never drift apart. A test
  fails when a route has no guide page (QA-UI-05).
- **Screenshots are generated** by the end-to-end tests in light and dark themes (FR-UX-32).
  Never paste a hand-made screenshot; add or extend a test instead.
- **What's new** is built from the release notes (`CHANGELOG.md`, written by release-please).
- **The AI assistant answers "how do I…" from these pages** and links the pages it used
  (FR-UX-33, AI-25). Write so that a single section answers a single question.
- **After editing a page, recompile the knowledge base** (the `fyniq-knowledge-base` skill, then
  `make kb-index` and `make kb-lint`). The assistant reads the compiled pages in `kb/`, not these
  files, and `make check` fails while a compiled page is stale (PRD §9.11).
- An accountant reviews any page that states an accounting or statutory rule. Keep `[verify]`
  out of user-facing text: confirm the rule first, or leave the page out of the release.
  Pages marked with `status: draft` in their front matter are not released until reviewed.

## Page front matter

```yaml
---
key: banking.categorise          # the help key in the feature registry
title: Categorise bank lines
mode: basic                      # basic | expert (expert pages are hidden in Basic mode)
roles: [owner, bookkeeper, accountant]
requirements: [FR-BNK-04, FR-UX-11]
---
```

Sections, in this order: **What it is** (two sentences) · **Before you start** · **Steps**
(numbered; one action per step; name buttons exactly as the UI does) · **What happens in the
books** (Expert only) · **Common problems** (each ✖ or ⚠ message and its fix) · **Related**.

## Contents (planned)

| Section | Pages |
|---|---|
| Getting started | Your first sign-in and the tour · Basic and Expert modes · The Home page · Set up the company (FR-UX-10) · Invite your accountant · Practise in the sample company |
| Everyday work | Record money in and out · Categorise and match bank lines · Create and send an invoice · Record a bill · Expense claims and costs you paid personally · Approve or reject · The Inbox |
| Founders and equity | ✔ [Founders' money: what the company owes you, and the options](founders-money.md) · Who owns the company (cap table) · Record a share issue · SAFEs and notes · Model a funding round · Investor records and updates |
| Payroll | Employees · Run payroll · CPF, SDL and SHG · Payslips and IR8A |
| Accounting (Expert) | Journals · Chart of accounts · Period close · Fixed assets · Foreign currency · Year end |
| Reports | Financial statements · Explain a number · Exports · Board pack |
| Compliance | Statutory calendar · GST returns · Evidence: attach screenshots and files · Audit trail |
| AI and integrations | Ask the AI · AI proposals and approvals · Connected apps and MCP · The API |
| Reference | Keyboard shortcuts · Status chips (✔ ⚠ ✖ ⏰ ✎ 🔒) · Glossary · What's new |
