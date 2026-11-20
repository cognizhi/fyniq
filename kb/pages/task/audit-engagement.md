---
key: audit.engagement
type: task
title: Work with the auditor's engagement
summary: Opening and confirming an auditor's time-boxed engagement, the auditor's requests and reproducible samples, and who answers
sources:
  - docs/user-guide/auditor-engagement.md sha256:86b743d314adb8ec0b814e3ce55a71000b213d76887bf7ed7cd6cfa48bbcb699
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-23
aliases:
  - auditor engagement
  - auditor requests
  - audit sample
  - audit request list
---

# Work with the auditor's engagement

## Find it

- In Expert mode, **Reports** has **Auditor engagement**. An Owner opens an engagement; the auditor asks and samples; an Owner, Controller or Accountant answers.

(src: docs/user-guide/auditor-engagement.md#find-it)

## Open an engagement

- An Owner opens an engagement for a member holding the Auditor role: a name, the firm, the period under audit and the dates between which requests and samples may be added, up to 366 days. It stays pending until a second Owner confirms it; in Founder mode the same Owner may. An Owner can close it earlier, and a closed engagement is never reopened.

(src: docs/user-guide/auditor-engagement.md#open-an-engagement)

## Requests

- While the engagement is active and between its dates, the auditor asks for an item and names who answers it: an Owner, Controller or Accountant. That person, or an Owner, answers with a note and evidence files already in the books, and the auditor accepts and closes the request. Nobody else writes to the engagement.

(src: docs/user-guide/auditor-engagement.md#requests)

## Samples

- The auditor draws a sample of posted ledger lines: a period inside the audit period, optionally an account, the smallest line and how many lines, up to 200. A seed kept with the sample means the same seed over the same ledger draws the same lines, so nothing is chosen by hand. Each line opens its source read-only. A sample never changes.

(src: docs/user-guide/auditor-engagement.md#samples)

## What the auditor cannot do

- The auditor writes only these requests and samples; everything else is read-only and payroll personal data stays hidden unless granted. Every request, answer and sample is in the audit trail.

(src: docs/user-guide/auditor-engagement.md#what-the-auditor-cannot-do)
