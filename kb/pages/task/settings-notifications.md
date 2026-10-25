---
key: settings.notifications
type: task
title: Your notifications, how they reach you by email, quiet hours, a test, and banners
summary: The bell and your notifications, choosing email for each kind, quiet hours, sending yourself a test, and dismissing banners
sources:
  - docs/user-guide/notifications.md sha256:db2c7e751d142e0cdda30a2fd899faa43677dff79833fbea3dce316fb2631ac6
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-24
aliases:
  - bell
  - notification centre
  - unread notifications
  - mark all read
  - archive a notification
  - email digest
  - daily digest
  - weekly digest
  - quiet hours
  - test notification
  - security notice
  - dismiss a banner
  - announcement banner
---

# Your notifications, how they reach you by email, quiet hours, a test, and banners

## Your notifications

- The bell in the header counts unread notifications; select it to open them.
- They cover approvals waiting or decided, security notices, mail in quarantine, someone else's postings, months closed, recurring drafts, bills to pay soon and failed background work.
- One that waits for you links to it, and opening it marks it read. Show **Unread**, **All** or **Archived**; **Mark read**, **Mark unread**, **Mark all read** and **Archive** (for good).
- Nobody hears of what they did themselves.

(src: docs/user-guide/notifications.md#your-notifications)

## Choose how they reach you

- **Settings → Notifications**: every notification shows in the app; for each kind choose **Immediately**, **Daily digest**, **Weekly digest** or **Not by email**.
- Recommended: approvals and security notices at once, the rest in the daily digest at 08:00 Singapore time; the weekly digest goes on Mondays at 08:00. A changed kind offers **Reset to recommended**.
- Security notices are **Always on**: a passkey added or removed and a role change are emailed at once; a sign-in is not emailed again, since the sign-in alert emails every sign-in.
- Email goes to the address the sign-in provider verified; without one, notifications show in the app only. Whether an email carries details is the organisation's setting ([[settings.organisation]]).

(src: docs/user-guide/notifications.md#choose-how-they-reach-you)

## Quiet hours

- **Hold email during quiet hours**, in Singapore time, may run past midnight (such as 22:00 until 07:00).
- Email that would go at once waits until they end; security notices still go at once, and nothing waits in the app.

(src: docs/user-guide/notifications.md#quiet-hours)

## Send yourself a test

- **Send me a test notification** reaches you in the app and by email at once unless you chose otherwise; its email shows in the organisation's email delivery log ([[settings.integrations]] covers the email providers).

(src: docs/user-guide/notifications.md#send-yourself-a-test)

## Banners

- Banners under the header come from the Owner or Administrator ([[settings.announcements]]) or from the people who run Fyniq, such as a maintenance window.
- Critical ones come first and stay until they end; **Dismiss** hides any other for you alone, on every device. An outside link opens in a new tab.

(src: docs/user-guide/notifications.md#banners)
