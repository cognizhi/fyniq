---
key: preferences.security
type: task
title: Your profile and security: Basic or Expert mode, passkeys and devices
summary: Basic or Expert mode and what each changes, confirming it's you, adding and revoking passkeys, and signing out other devices
sources:
  - docs/user-guide/profile-and-security.md sha256:9ddb8f136bbec03c1735bde93b81d5b6ebbd93090bebdcbc0ac1b4c3e8f1a4a1
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-20
aliases:
  - profile
  - security settings
  - expert mode
  - basic mode
  - experience mode
  - add passkey
  - remove passkey
  - revoke passkey
  - sign out other devices
  - sessions
---

# Your profile and security: Basic or Expert mode, passkeys and devices

## What the page holds

- **Settings › Profile and security** holds what is the person's own: Basic or Expert mode, their passkeys and the devices they are signed in on; nobody else sees or changes them.
- **Settings** is at the foot of the sidebar, under **More** on a phone ([[home.overview]]).
- Adding or revoking a passkey needs a confirmation that it's you in the last 5 minutes; Fyniq asks first if there was none.

(src: docs/user-guide/profile-and-security.md#what-it-is) (src: docs/user-guide/profile-and-security.md#before-you-start)

## Basic and Expert mode

- **Basic** shows the essentials in plain words; **Expert** adds accounts, rule IDs, the organisation's settings and every detail.
- The mode changes only what is shown, never what the person may do, what Fyniq checks or what an action does.
- Safety information shows in both: what cannot post, what waits for approval, locked periods and deadlines.
- Choose it on this page or with **Basic | Expert** in the header. Until chosen, roles decide: an Accountant, Controller, Tax agent, Auditor or Payroll administrator starts in Expert; everyone else, and every Owner, in Basic.
- For now the choice is kept in the browser.

(src: docs/user-guide/profile-and-security.md#basic-and-expert-mode)

## Confirm it's you

- The page says whether the person confirmed it's you recently and until when it counts.
- **Confirm it's you now** does it before a series of changes, so they do not each ask ([[auth.sign-in]]).

(src: docs/user-guide/profile-and-security.md#confirm-its-you)

## Add or revoke a passkey

- **Add a passkey**, name it (such as "Work laptop"), select **Add the passkey**, and confirm with fingerprint, face or PIN on the device.
- The device keeps the private key; Fyniq keeps only the public half. A passkey synced to the person's other devices says so.
- **Revoke**, then **Revoke** again, stops a passkey at once; it cannot be undone, so add a new one instead.

(src: docs/user-guide/profile-and-security.md#add-a-passkey) (src: docs/user-guide/profile-and-security.md#revoke-a-passkey)

## Your devices

- Lists every browser signed in, with when it signed in, the provider and when it was last used; **This device** marks the current one.
- **Sign out** next to another device ends that session at its next request; **Sign out everywhere else** ends all but this one.
- Each sign-in also sends an email.

(src: docs/user-guide/profile-and-security.md#your-devices)

## Common problems

- "You have no passkey yet.": select **Sign in again**, sign in with the authenticator app, then add a passkey.
- "The passkey was cancelled, or took too long.": the device's prompt closed or timed out; try again.
- "This browser cannot make passkeys.": use a current browser on a device with a fingerprint reader, face recognition or a PIN.
- Related: how Fyniq looks ([[preferences.appearance]]).

(src: docs/user-guide/profile-and-security.md#common-problems) (src: docs/user-guide/profile-and-security.md#related)
