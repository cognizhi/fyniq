---
key: nemura.features
type: task
title: Switch Nemura's features on or off
summary: How the HR administrator or Owner sets each optional Nemura feature on, off or held by another system, and why a switch is refused
sources:
  - docs/user-guide/nemura/features.md sha256:ada9b5d124001f1d21fee0fdfa8410f14da206deb5173b7581903ed42d2dae3d
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-19
aliases:
  - turn off leave
  - enable payroll in Nemura
  - external system
  - feature switch
---

# Switch Nemura's features on or off

## What it is

- Each optional feature is **On**, **Off**, or held by **Another system**; People records is always on.
- A feature that is off has no pages and no jobs, and switching it off keeps its records. (src: docs/user-guide/nemura/features.md#what-it-is)

## Who and what first

- Only the HR administrator or the Owner.
- Payroll needs Leave, and Onboarding needs Letters. (src: docs/user-guide/nemura/features.md#before-you-start)

## Steps

1. Open **Features**.
2. Choose the state; for **Another system**, type the system that holds the data.
3. Select **Apply**.

(src: docs/user-guide/nemura/features.md#steps)

## When a switch is refused

- Payroll needs Leave on or held by another system: switch Leave on first.
- Another feature needs this one: switch that one off first.
- Work is still open: record how each open thing will be handled, then switch off.
- Hiring, Performance, Onboarding and Time come later; Payroll needs the Owner and a second approver, which arrives with Payroll. See [[nemura.home]].

(src: docs/user-guide/nemura/features.md#common-problems)
