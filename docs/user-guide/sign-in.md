---
key: auth.sign-in
title: Sign in to Fyniq
mode: basic
roles: [all]
requirements: [FR-ADM-08, FR-ADM-02, SEC-01, SEC-05, SEC-08]
---

# Sign in to Fyniq

## What it is

Fyniq signs you in with an account you already have at a sign-in provider, such as Google or Zoho.
The Super admin chooses which providers this installation offers.

Every user signs in with a second factor: a code from an authenticator app, which the provider asks
for, or a passkey. A sign-in without one can only confirm it's you with a passkey; nothing else works
until you do.

Your session ends after 30 minutes without activity, and after 12 hours in all. Sign in again and
Fyniq takes you back to the page you were on.

## Before you start

- You need an account at one of the providers the **Sign in to Fyniq** page lists.
- Joining as the second Super admin? Have the invitation code the first Super admin gave you.

## Steps

1. Open Fyniq. If you are not signed in, it opens **Sign in to Fyniq**.
2. Select **Continue with** your provider, and sign in there.
3. Fyniq takes you back to the page you were on.

To leave, select **Sign out**.

## Confirm it's you with a passkey

A passkey lets you confirm it's you with your fingerprint, face or device PIN, instead of a code.
Asking for or approving a change needs this confirmation in the last 5 minutes.

1. Sign in with your authenticator app first. Within 5 minutes, add a passkey and give it a name,
   such as "Work laptop". Your device asks for your fingerprint, face or PIN.
2. Later, when Fyniq asks you to confirm it's you, choose your passkey and confirm on your device.
   The confirmation counts for 5 minutes.
3. To stop using a passkey, for example on a lost device, revoke it. It works no more.

Each request to confirm works once, for 5 minutes, and only on this site.

## Join as the second Super admin

1. Sign in as above. Fyniq shows **Joining by invitation**, because you have no account yet.
2. Enter the **Invitation code** and **Your name**, then select **Accept the invitation**.
3. Print or write down your ten recovery codes and keep them offline. Tick **I have kept these
   codes somewhere safe, offline.**, then select **Go to Fyniq**.

An invitation works once, within 7 days.

## Common problems

| Message | What to do |
|---|---|
| Sign-in failed (AUTH-SIGNIN-01): that sign-in provider is not available | Choose another way to sign in on the page |
| Sign-in failed (AUTH-SIGNIN-02): the sign-in took longer than 10 minutes, or began in another browser | Sign in again, from start to finish in the same browser |
| Sign-in failed (AUTH-SIGNIN-03): the provider's answer did not pass Fyniq's checks | Sign in again. If it keeps happening, tell the Super admin |
| Sign-in failed (AUTH-SIGNIN-04): the sign-in service did not answer | Try again in a moment |
| That invitation code is not valid, was used already or has expired | Ask the Super admin for a new invitation |
| A second factor is required (API-AUTHZ-07) | Sign in again with your authenticator app, or confirm it's you with a passkey |
| You have no passkey (API-PASSKEY-02) | Sign in again with your authenticator app, then add a passkey |
| The passkey's answer was refused (API-PASSKEY-01) | Try again on this site, with a passkey you added here, and confirm with your fingerprint, face or PIN |
| The challenge cannot be used (API-PASSKEY-03) | It was used already or more than 5 minutes passed: start again |

## Related

- [Set up a new installation](setup-first-run.md)
