---
key: settings.console
title: The installation console
mode: expert
roles: [super_admin]
requirements: [FR-ADM-07]
status: draft
---

# The installation console

## What it is

**Settings › Installation console** is a Super admin's view of the installation as a whole: whether the platform is healthy, and who
the Super admins are. It reads across every organisation but shows counts, times and names only: never an organisation's books or
records, which a Super admin reaches only as a member of that organisation.

## Platform health

- The page says **Healthy** or **Degraded**. It is degraded when the Scheduler's tick, which runs every 5 minutes, last ran more than
  15 minutes ago or has never run, or when background work has failed its attempts and needs someone. A list says why, in words.
- **Background work waiting** and **Background work that failed** count the work queued for every organisation, and **Oldest work
  waiting since** says how long the oldest has waited.
- The table lists each platform job, such as the tick and the audit trail's anchoring and verification, with when it last ran and
  what it reported. The page reads again every minute.

## Super admins

- The list shows each active Super admin and since when. While there is only one, a warning says to invite a second, since a second
  keeps the installation recoverable. The last Super admin is only replaced, never removed.
- Adding a Super admin, recovering one and the other installation pages are on their own pages: [Sign-in providers](sign-in-providers.md).

## Common problems

- "Reading the platform's health needs step-up": confirm it's you with your passkey or authenticator app, then open the page again.
- "Degraded: the Scheduler's tick has never run": the platform's Scheduler is not calling Fyniq; ask whoever runs the deployment.
