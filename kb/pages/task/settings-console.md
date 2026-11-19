---
key: settings.console
type: task
title: Read the platform's health and list the Super admins
summary: A Super admin's installation console: healthy or degraded and why, the platform jobs' latest beats, the background work's backlog, and the Super admins
sources:
  - docs/user-guide/installation-console.md sha256:c1c99ccbfc8aea480c0e7afd7969d548e4d879a085ddc0033448ed89791f1bc2
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - installation console
  - super admin console
  - platform health
  - is fyniq healthy
  - scheduler tick
  - who are the super admins
---

# Read the platform's health and list the Super admins

## What the page holds

- **Settings › Installation console** is a Super admin's view of the installation as a whole: whether the platform is healthy, and who the Super admins are.
- It reads across every organisation but shows counts, times and names only: never an organisation's books or records, which a Super admin reaches only as a member of that organisation.

(src: docs/user-guide/installation-console.md#what-it-is)

## Platform health

- The page says **Healthy** or **Degraded**: degraded when the Scheduler's tick, which runs every 5 minutes, last ran more than 15 minutes ago or has never run, or when background work has failed its attempts and needs someone; a list says why.
- **Background work waiting** and **Background work that failed** count the work queued for every organisation, and **Oldest work waiting since** says how long the oldest has waited.
- The table lists each platform job, such as the tick and the audit trail's anchoring and verification, with when it last ran and what it reported; the page reads again every minute.

(src: docs/user-guide/installation-console.md#platform-health)

## Super admins

- The list shows each active Super admin and since when; while there is only one, a warning says to invite a second, since a second keeps the installation recoverable.
- The last Super admin is only replaced, never removed; the other installation pages are on their own pages ([[settings.sign-in-providers]]).

(src: docs/user-guide/installation-console.md#super-admins)

## Common problems

- "Reading the platform's health needs step-up": confirm it's you with your passkey or authenticator app, then open the page again.
- "Degraded: the Scheduler's tick has never run": the platform's Scheduler is not calling Fyniq; ask whoever runs the deployment.

(src: docs/user-guide/installation-console.md#common-problems)
