---
key: settings.announcements
title: Announcements
mode: expert
roles: [owner, admin, auditor]
requirements: [FR-NTF-03]
status: draft
---

# Announcements

## What they are

- An announcement is a banner under the header on every page, for everyone in the organisation or for chosen
  roles, from its start until its end.
- The Owner and the Administrator publish and end them; the Auditor can see them.
- Open **Settings → Announcements** (Expert mode).

## Publish an announcement

- Write the **Message**: plain text, at most 300 characters. Say one thing: what to do and by when.
- Add a **Link** if it helps: a page in Fyniq, such as /help/organisation-settings, or an address that starts
  with https://. Outside links open in a new tab.
- Choose the **Severity**: **Info**, **Warning**, or **Critical** for what everyone must see. Info and warning are
  announced politely to screen readers; critical interrupts.
- Choose the **Audience**: **Everyone**, or **Chosen roles**.
- Set when it **Starts** (leave it empty to start now) and when it **Ends**, in Singapore time.
- **People can dismiss it** is on unless it is critical: a critical banner can't be dismissed.
- **Publish** (or **Schedule**, when it starts later). The preview shows how it will look.
- An announcement never changes once published. To say more, end it and publish another. Publishing and ending are
  recorded in the audit trail.

## End an announcement

- **All announcements** lists each with its status: **Scheduled**, **Live** or **Ended**.
- **End now** ends a live one at once, or cancels a scheduled one.

## To everyone

- A Super admin can also choose **Whole installation**, to show a banner to every organisation's members, for
  example a planned maintenance window or an incident.

## Related

- [Notifications and banners](notifications.md)
