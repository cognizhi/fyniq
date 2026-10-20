---
key: preferences.security
title: Your profile and security
mode: basic
roles: [all]
requirements: [FR-UX-23, FR-UX-24, FR-UX-27, FR-ADM-02, FR-ADM-03, SEC-01]
status: draft            # written with the app shell's settings pages (P1); reviewed with the shell's screens
---

# Your profile and security

## What it is

**Settings › Profile and security** holds what is yours alone: whether Fyniq shows you Basic or
Expert mode, the passkeys you confirm it's you with, and the devices you are signed in on. Nobody
else sees or changes them.

## Before you start

- Open **Settings** at the foot of the sidebar (on a phone, under **More**), then **Profile and
  security**.
- Adding or revoking a passkey needs you to have confirmed it's you in the last 5 minutes. If you
  have not, Fyniq asks first.

## Basic and Expert mode

**Basic** shows the essentials in plain words. **Expert** adds accounts, rule IDs, the
organisation's settings and every detail. The mode changes only what you see: never what you may
do, what Fyniq checks, or what an action does. Safety information always shows in both: what
cannot post, what waits for approval, locked periods and deadlines.

Choose the mode here, or with **Basic | Expert** in the header. Until you choose, your roles
decide: an Accountant, Controller, Tax agent, Auditor or Payroll administrator starts in Expert;
everyone else, and every Owner, starts in Basic. For now your choice is kept in this browser.

## Confirm it's you

The page says whether you confirmed it's you recently, and until when that counts. Select
**Confirm it's you now** to do it before a series of changes, so they do not each ask.

## Add a passkey

1. Select **Add a passkey**.
2. Give it a name you will recognise, such as "Work laptop", and select **Add the passkey**.
3. Your device asks for your fingerprint, face or PIN. When it has, the passkey shows in the list.

Your device keeps the passkey's private key; Fyniq keeps only the public half. A passkey synced to
your other devices says so.

## Revoke a passkey

Select **Revoke** next to the passkey, then **Revoke** again to confirm. It stops working at once.
You cannot undo it: add a new passkey instead.

## Your devices

The list shows every browser you are signed in on: when it signed in, with which provider, and
when it was last used. **This device** marks the one you are using.

- Select **Sign out** next to another device to end that session. It is signed out at its next
  request.
- Select **Sign out everywhere else** to end every session but this one.

Each sign-in also sends you an email, so you know when a new device signs in.

## Common problems

- **"You have no passkey yet."** Select **Sign in again** and sign in with your authenticator app,
  then add a passkey.
- **"The passkey was cancelled, or took too long."** Your device's prompt was closed or timed out.
  Try again when you are ready.
- **"This browser cannot make passkeys."** Use a current browser on a device with a fingerprint
  reader, face recognition or a PIN.

## Related

- [Sign in to Fyniq](sign-in.md)
- [Change how Fyniq looks](appearance.md)
