---
key: settings.announcements
type: task
title: Publish and end announcements, the banners under the header
summary: Publish a banner to everyone or chosen roles, with severity, link, start and end; end or cancel one; Super admins to everyone
sources:
  - docs/user-guide/announcements.md sha256:1c061e843c5959556358fd5a9cafdea2c5b60ff8025afe73ebc5d733274378cd
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-24
aliases:
  - announcement
  - banner
  - publish a banner
  - maintenance notice
  - end an announcement
  - schedule an announcement
---

# Publish and end announcements, the banners under the header

## What they are

- A banner under the header on every page, for everyone or chosen roles, from its start until its end.
- The Owner and the Administrator publish and end them; the Auditor can see them. **Settings → Announcements** (Expert mode).

(src: docs/user-guide/announcements.md#what-they-are)

## Publish an announcement

- **Message**: plain text, at most 300 characters; one thing, what to do and by when. **Link**: a page in Fyniq or an https:// address (outside links open in a new tab).
- **Severity**: **Info**, **Warning** or **Critical**; only critical interrupts screen readers, and it can't be dismissed.
- **Audience**: **Everyone** or **Chosen roles**. **Starts** (empty: now) and **Ends**, in Singapore time.
- **Publish**, or **Schedule** when it starts later. It never changes once published: end it and publish another. Publishing and ending are in the audit trail.

(src: docs/user-guide/announcements.md#publish-an-announcement)

## End an announcement

- **All announcements** shows each as **Scheduled**, **Live** or **Ended**; **End now** ends a live one or cancels a scheduled one.

(src: docs/user-guide/announcements.md#end-an-announcement)

## To everyone

- A Super admin can choose **Whole installation**, for a banner to every organisation's members, such as a maintenance window or an incident. How banners show to people: [[settings.notifications]].

(src: docs/user-guide/announcements.md#to-everyone)
