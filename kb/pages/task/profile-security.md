---
key: preferences.security
type: task
title: Your profile and security: Basic or Expert mode, your bank account, passkeys, sign-ins and devices
summary: Basic or Expert mode, your bank account for reimbursements, confirming it's you, passkeys, linking or removing a sign-in, and signing out devices
sources:
  - docs/user-guide/profile-and-security.md sha256:30749625dcf077b18c4f87c8adaf70750fd89e9ff27b3420ea97c9eb06e94848
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-07
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
  - link google
  - link zoho
  - linked sign-in accounts
  - second sign-in provider
  - remove a sign-in
---

# Your profile and security: Basic or Expert mode, your bank account, passkeys, sign-ins and devices

## What the page holds

- **Settings › Profile and security** holds what is the person's own: Basic or Expert mode, the bank account their expense claims are paid into, their passkeys, the accounts they sign in with and the devices they are signed in on; nobody else changes them.
- **Settings** is at the foot of the sidebar, under **More** on a phone ([[home.overview]]).
- Adding or revoking a passkey, and linking or removing a sign-in, needs a confirmation that it's you in the last 5 minutes; Fyniq asks first if there was none.

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

## Link another sign-in

- **Linked sign-in accounts** lists the accounts that sign the person in, such as Google or Zoho; a second one keeps them able to sign in if one provider is down or that account is lost.
- **Link** with the provider's name (such as **Link Google**); Fyniq asks to confirm it's you first if that is older than 5 minutes; then sign in at the provider with the account to add, and the page says it is linked.
- Only signing in with the account here links it: a matching email never does, and an account that signs someone in to Fyniq already is not linked.
- The second factor is still asked for whichever account signs in; each link sends a security notice in Fyniq and by email.

(src: docs/user-guide/profile-and-security.md#link-another-sign-in)

## Remove a sign-in

- **Remove** next to the account, then **Remove** again: it signs the person in no more, and devices signed in with it are signed out.
- Never the one in use nor the last: sign in with another first, or link another. It can be linked again later.

(src: docs/user-guide/profile-and-security.md#remove-a-sign-in)

## Recovery codes

- **Make recovery codes** under **Recovery codes**, confirm it's you, and keep the ten codes shown once (printed, or in a password manager); each works once.
- With an Owner's or Administrator's confirmation, one gives the account back if every authenticator is lost ([[auth.sign-in]]).
- The card says how many are unused; **Make new codes** replaces them all. A second passkey on another device keeps one lost device from locking the person out.

(src: docs/user-guide/profile-and-security.md#recovery-codes)

## Your devices

- Lists every browser signed in, with when it signed in, the provider and when it was last used; **This device** marks the current one.
- **Sign out** next to another device ends that session at its next request; **Sign out everywhere else** ends all but this one.
- Each sign-in also sends an email.

(src: docs/user-guide/profile-and-security.md#your-devices)


## See or correct your personal data

- Under **My personal data** (Settings › Profile) a member asks to see the personal data the company holds about them, or to correct it, and selects **Send the request**.
- The Data Protection Officer is told and answers within 30 days, or tells them in writing within 30 days when they will; the requests stay listed with their status and due day ([[compliance.data-protection]]).

(src: docs/user-guide/profile-and-security.md#see-or-correct-your-personal-data)

## Common problems

- "You have no passkey yet.": select **Sign in again**, sign in with the authenticator app, then add a passkey.
- "The passkey was cancelled, or took too long.": the device's prompt closed or timed out; try again.
- "This browser cannot make passkeys.": use a current browser on a device with a fingerprint reader, face recognition or a PIN.
- "No screenshot of your banking app or statement header is attached.": attach one showing the name and the account number before asking for approval.
- "An account for reimbursements is a Singapore bank's account.": use a bank in Singapore's code, such as DBSSSGSG, OCBCSGSG or UOVBSGSG.
- "That account signs someone in to Fyniq already.": at the provider, sign in with the account meant to be added, then **Link** again.
- "Linking needs you to have confirmed it's you in the last 5 minutes.": select **Link** again, confirm when asked, and sign in at the provider within a few minutes.
- "That provider accepts only accounts on some email domains.": link the account whose email is on the organisation's domain, such as the work account.
- "The sign-in you are using, or your last one, cannot be removed.": sign in with another sign-in first, or link another.
- Related: how Fyniq looks ([[preferences.appearance]]); claiming expenses ([[claims.make-a-claim]]); paying claims in a batch ([[purchases.payment-batches]]).

(src: docs/user-guide/profile-and-security.md#common-problems) (src: docs/user-guide/profile-and-security.md#related)
