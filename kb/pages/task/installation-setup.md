---
key: installation.setup
type: task
title: Set up a new installation of Fyniq
summary: First-run setup step by step: sign in, the one-time setup code, the first organisation, ten recovery codes shown once, and inviting a second Super admin
sources:
  - docs/user-guide/setup-first-run.md sha256:d21fb5c6c02817e9424e4a4064f3f9899a16f1f9f01d6c7a580555e84c42eb5e
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-15
aliases:
  - install
  - first-run setup
  - setup code
  - Super admin
  - recovery codes
---

# Set up a new installation of Fyniq

## What setup does

- A new installation opens **Set up Fyniq**.
- The first person to finish becomes the installation's Super admin and the Owner of its first organisation.
- Afterwards the setup page closes for good.

(src: docs/user-guide/setup-first-run.md#what-it-is)

## Before starting

- The one-time setup code, printed by `make setup-code` (in the cloud: Secret Manager); keep it private.
- The company's name and its base currency, which cannot change later.
- A printer or paper for the ten recovery codes, shown once.

(src: docs/user-guide/setup-first-run.md#before-you-start)

## Steps

- Sign in with **Continue with** a provider: that sign-in becomes the Super admin's ([[auth.sign-in]]).
- Enter **Your name**, then the **One-time setup code**, then the **Company name** and **Base currency**, and select **Create organisation**.
- Keep the ten recovery codes offline, tick the confirmation and continue.
- Recommended: **Create an invitation** for the other founder and give them the code in person or on a call; it works once and expires after 7 days.
- Inviting needs a sign-in with the second step in the last 5 minutes; **Sign in again** returns to the invitation.
- **Later** skips the invitation; **Go to Fyniq** finishes.

(src: docs/user-guide/setup-first-run.md#steps)

## Problems during setup

- "That setup code isn't right, or this installation is set up already": copy the code again; case, spaces and dashes do not matter.
- "Your sign-in has ended": sign in again and enter the code again.
- "This installation is set up": the page is closed; sign in instead.

(src: docs/user-guide/setup-first-run.md#common-problems)
