---
key: settings.accounting
type: task
title: Record and approve the accounting framework and policies
summary: How the framework, its edition and each accounting policy are recorded by year, and how the accountant confirms and approves them
sources:
  - docs/user-guide/accounting-framework.md sha256:cfbf52b529296e0f1b63313ddf35f65908bb5a252f60d37afd56e1e949951206
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-21
aliases:
  - accounting framework
  - accounting policies
  - SFRS for Small Entities
  - management statements
  - approve a policy
---

# Record and approve the accounting framework and policies

## What it is

- The statements are prepared under an accounting framework (such as SFRS for Small Entities) in an edition; the accounting policies say how Fyniq treats revenue, government grants, leases, untaken leave and the rest.
- Both live under **Settings → Accounting framework and policies** (Expert mode), one financial year at a time.
- Each framework or policy recorded is a version: it applies from the first day of a financial year until a later version replaces it, so it carries into later years. A version never changes; a correction is a new version.
- Fyniq never writes a policy: each one says what it decides, and the words are the accountant's.

(src: docs/user-guide/accounting-framework.md#what-it-is)

## Record the framework

- Choose **This financial year** or **Next financial year**, select **Record the framework**, choose the framework and check the edition Fyniq fills in, then **Check** and **Record the framework**.
- Fyniq recommends SFRS for Small Entities if the accountant confirms the company qualifies.
- SFRS for Small Entities has a Third Edition for financial years beginning on or after 1 January 2027; a year from then needs the framework recorded with that edition, even when an earlier year's framework carries into it.

(src: docs/user-guide/accounting-framework.md#record-the-framework)

## Write a policy

- Under **Accounting policies**, select **Write** (or **Change**), write the policy as the accountant decided it, then **Check** and **Record the policy**.
- Where a policy does not apply, say so, for example "The company has no leases."
- The capitalisation threshold policy approves the amount set under **Settings → Organisation → Defaults** ([[settings.change]]); the amount is changed there.

(src: docs/user-guide/accounting-framework.md#write-a-policy)

## Confirm and approve

- A member who holds the Accountant role confirms the framework (that the company may use it) and approves each policy, with **Confirm** or **Approve**.
- Nobody signs a version they recorded themselves; another member with the Accountant role signs it. Each version is signed once, with who signed it and when.
- The Owner and the Controller can record the framework and policies but cannot confirm or approve them ([[members.roles]]).

(src: docs/user-guide/accounting-framework.md#confirm-and-approve)

## Management statements

- Until the framework in force for a year is confirmed and every policy in force is approved, that year's statements are titled **Management statements** and name no framework; the page lists what is still to do.
- Once everything is signed, they are titled **Financial statements** and say what they are prepared under, for example "Prepared under SFRS for Small Entities (Third Edition)".
- A new version of a policy waits for approval again, so the year goes back to management statements until it is approved.

(src: docs/user-guide/accounting-framework.md#management-statements)

## Common problems

- **You wrote it, so another accountant signs it**: ask another member who holds the Accountant role.
- **Signed already, or replaced**: the version is signed already, or a later version from the same year replaces it; sign the latest version.
- **Not the first day of a financial year**: versions apply from a year's start; if the years are not set, set the books start and the first year end under **Settings → Organisation** ([[settings.organisation]]).

(src: docs/user-guide/accounting-framework.md#common-problems)
