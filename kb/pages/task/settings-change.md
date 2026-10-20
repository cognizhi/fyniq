---
key: settings.change
type: task
title: Change or reset an organisation setting, or edit the profile
summary: Change a company-wide setting from a date, reset it to recommended, why some need a second approver, and edit the profile
sources:
  - docs/user-guide/organisation-settings.md sha256:f374e8f2e9205e304168f6468e9098e08e4802c0b2b07d7f9c9fad2f38e41320
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-21
aliases:
  - reset to recommended
  - change a setting
  - effective date
---

# Change or reset an organisation setting, or edit the profile

## Steps

- Open **Settings → Organisation** (Expert mode) and select **Change** next to the setting ([[settings.organisation]] lists them).
- Enter the new value and the date it takes effect: today or later, never earlier.
- **Preview** checks it without saving; **Save** records it, after confirming it's you if asked.
- Every earlier value is kept with its dates, so the value on any day can be shown again.
- **Reset to recommended** records the recommended value as a new change; nothing is deleted.
- If someone changed the setting meanwhile, reload it first.

(src: docs/user-guide/organisation-settings.md#change-a-setting)

## Settings that need a second approver

- Thresholds, retention and the AI assistant need a second approver.
- They show **Needs a second approver**, and the button says **Ask for approval** instead of **Save**. Saving a change to one asks for approval: the setting keeps its value until another member who approves settings, such as an Owner, approves it ([[approvals.decide]]).

(src: docs/user-guide/organisation-settings.md#settings-that-need-a-second-approver)

## Edit the profile

- The **Profile** at the top of the page is what documents print: legal name, UEN, addresses, contact details, and the bank and PayNow details customers pay to; also when each financial year starts, when the books start and the time zone.
- **Edit the profile**, change the fields (a field left empty is cleared), then **Save the profile**; if someone else saved it meanwhile, reload first.
- Once the first financial year is open in **Accounting → Periods and close** ([[accounting.periods]]), the books start, the month the year starts in and the end of the first year no longer change.

(src: docs/user-guide/organisation-settings.md#the-profile)
