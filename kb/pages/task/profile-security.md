---
key: preferences.security
type: task
title: Your profile and security: Basic or Expert mode, your bank account, passkeys and devices
summary: Basic or Expert mode, your bank account for reimbursements and its approval, confirming it's you, passkeys, and signing out other devices
sources:
  - docs/user-guide/profile-and-security.md sha256:d9e33a44fe9b69a5e68f8a7872ecdc2044145cb2291e08b4ac622a2da4a1dacd
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-28
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
  - bank account for reimbursements
  - my bank account
  - change my bank account
  - where claims are paid
---

# Your profile and security: Basic or Expert mode, your bank account, passkeys and devices

## What the page holds

- **Settings › Profile and security** holds what is the person's own: Basic or Expert mode, the bank account their expense claims are paid into, their passkeys and the devices they are signed in on; nobody else changes them.
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

## Your bank account for reimbursements

- Someone who claims expenses sees the **Bank account for reimbursements** card: the bank's code and the last four digits of where claims are paid ([[claims.make-a-claim]]).
- **Show number** shows the whole number after confirming it's you; Fyniq records each look.
- Only the person enters their own account; someone in finance, never the person, approves each change before it is used.
- While a change waits, payments to the person wait too, so a claim is never paid into an account nobody checked.

(src: docs/user-guide/profile-and-security.md#your-bank-account-for-reimbursements)

## Change your bank account

- **Change my bank account**, then the bank's SWIFT/BIC code (such as DBSSSGSG), the name on the account and the number: a Singapore bank's account in SGD; **Save as a draft**.
- Attach a screenshot of the banking app, or the statement's header, showing the name and the account number.
- **Ask for approval** and confirm it's you; the card shows **Change waits for approval** until someone in finance approves it ([[approvals.decide]]).
- **Withdraw** takes the change back: the account stays as it was, and payments are no longer held.
- Once approved, the new account replaces the old one for every payment from then on.

(src: docs/user-guide/profile-and-security.md#change-your-bank-account)

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
- "No screenshot of your banking app or statement header is attached.": attach one showing the name and the account number before asking for approval.
- "An account for reimbursements is a Singapore bank's account.": use a bank in Singapore's code, such as DBSSSGSG, OCBCSGSG or UOVBSGSG.
- Related: how Fyniq looks ([[preferences.appearance]]); claiming expenses ([[claims.make-a-claim]]).

(src: docs/user-guide/profile-and-security.md#common-problems) (src: docs/user-guide/profile-and-security.md#related)
