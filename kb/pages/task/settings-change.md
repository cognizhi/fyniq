---
key: settings.change
type: task
title: Change or reset an organisation setting
summary: Change a company-wide setting from today or a later date, reset it to its recommended value, and why some settings need a second approver
sources:
  - docs/user-guide/organisation-settings.md sha256:e344f9716616e43b153e4b29e5a6a1c9f89119211bd0a5799b5c0f7ceb488d53
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-20
aliases:
  - reset to recommended
  - change a setting
  - effective date
---

# Change or reset an organisation setting

## Steps

- Open **Settings → Organisation** and choose the setting ([[settings.organisation]] lists them).
- Enter the new value and the date it takes effect: today or later, never earlier.
- **Preview** checks it; **Save** records it.
- Every earlier value is kept with its dates, so the value on any day can be shown again.
- **Reset to recommended** records the recommended value as a new change; nothing is deleted.
- If someone changed the setting meanwhile, reload it first.

(src: docs/user-guide/organisation-settings.md#change-a-setting)

## Settings that need a second approver

- Thresholds, retention and the AI assistant need a second approver.
- Until approvals arrive in Fyniq they keep their recommended values and show **Needs a second approver**; a change to one is refused and nothing changes.

(src: docs/user-guide/organisation-settings.md#settings-that-need-a-second-approver)
