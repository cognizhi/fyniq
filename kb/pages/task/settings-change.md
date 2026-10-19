---
key: settings.change
type: task
title: Change or reset an organisation setting
summary: Change a company-wide setting from today or a later date, reset it to its recommended value, and why some settings need a second approver
sources:
  - docs/user-guide/organisation-settings.md sha256:0463646dacc3573b726c20843b25f57e723488d635fbba060d35e2ed12cbc70b
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
- They show **Needs a second approver**. Saving a change to one asks for approval: the setting keeps its value until another member who approves settings, such as an Owner, approves it ([[approvals.decide]]).

(src: docs/user-guide/organisation-settings.md#settings-that-need-a-second-approver)
