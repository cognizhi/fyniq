---
key: preferences.security
title: Your profile and security
mode: basic
roles: [all]
requirements: [FR-UX-23, FR-UX-24, FR-UX-27, FR-ADM-02, FR-ADM-03, SEC-01, FR-AP-22, FM-1, SEC-11]
status: draft            # written with the app shell's settings pages (P1); reviewed with the shell's screens
---

# Your profile and security

## What it is

**Settings › Profile and security** holds what is yours alone: whether Fyniq shows you Basic or
Expert mode, the bank account your expense claims are paid into, the passkeys you confirm it's you
with, and the devices you are signed in on. Nobody else changes them.

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

## Your bank account for reimbursements

If you claim expenses, the **Bank account for reimbursements** card shows where your claims are
paid: the bank's code and the last four digits. Select **Show number** to see the whole number;
Fyniq asks you to confirm it's you first, and records that you looked.

Only you enter your own account. Someone in finance, never you, approves each change before it is
used. While a change waits, payments to you wait too, so a claim is never paid into an account
nobody checked.

## Change your bank account

1. Select **Change my bank account**.
2. Enter the bank's SWIFT/BIC code, such as DBSSSGSG, the name on the account and the account
   number. It must be a Singapore bank's account in SGD. Select **Save as a draft**.
3. Attach a screenshot of your banking app, or your statement's header, showing your name and the
   account number.
4. Select **Ask for approval** and confirm it's you.

The card shows **Change waits for approval** until someone in finance approves it. To take the
change back, select **Withdraw**: your account stays as it was, and payments to you are no longer
held. Once approved, the new account replaces the old one for every payment from then on.

## Common problems

- **"You have no passkey yet."** Select **Sign in again** and sign in with your authenticator app,
  then add a passkey.
- **"The passkey was cancelled, or took too long."** Your device's prompt was closed or timed out.
  Try again when you are ready.
- **"This browser cannot make passkeys."** Use a current browser on a device with a fingerprint
  reader, face recognition or a PIN.
- **"No screenshot of your banking app or statement header is attached."** Attach one before you
  ask for approval: it must show your name and the account number.
- **"An account for reimbursements is a Singapore bank's account."** The bank's code must be of a
  bank in Singapore, such as DBSSSGSG, OCBCSGSG or UOVBSGSG.

## Related

- [Sign in to Fyniq](sign-in.md)
- [Change how Fyniq looks](appearance.md)
- [Claim back money you spent for the company](claims-my-claims.md)
- [Pay suppliers and claims in a batch](payment-batches.md)
