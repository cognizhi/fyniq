---
key: auth.sign-in
title: Sign in to Fyniq
mode: basic
roles: [all]
requirements: [FR-ADM-08, FR-ADM-02, FR-ADM-03, SEC-01, SEC-05, SEC-08, SEC-35]
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
- Joining a company, or joining as the second Super admin? Have the invitation code you were given.

## Steps

1. Open Fyniq. If you are not signed in, it opens **Sign in to Fyniq**.
2. Select **Continue with** your provider, and sign in there.
3. Fyniq takes you back to the page you were on.

To leave, select **Sign out**.

## Confirm it's you with a passkey

A passkey lets you confirm it's you with your fingerprint, face or device PIN, instead of a code.
Asking for or approving a change needs this confirmation in the last 5 minutes.

1. Sign in with your authenticator app first. Within 5 minutes, open **Settings › Profile and
   security**, add a passkey and give it a name, such as "Work laptop". Your device asks for your
   fingerprint, face or PIN.
2. Later, when Fyniq asks you to confirm it's you, choose your passkey and confirm on your device.
   The confirmation counts for 5 minutes.
3. To stop using a passkey, for example on a lost device, revoke it. It works no more.

Each request to confirm works once, for 5 minutes, and only on this site.

## Your devices

Each time you sign in, Fyniq emails you, at the address your provider confirmed: when, with which
provider and from which browser. If it was not you, end that session.

1. Open **Settings › Profile and security**: **Your devices** lists every browser you are signed
   in on, with when it signed in and was last used. The one you are using is marked.
2. To end a session, such as on a lost phone, select **Sign out** next to it. It is signed out at
   its next request.
3. To end every session but this one, select **Sign out everywhere else**.

## Recover your account

If your sign-in provider is down, or you lost that account, sign in with another provider you
linked on your profile. If you lost every authenticator, your phone with its authenticator app and
your passkeys, recover your account:

1. Sign in with your provider. Fyniq shows **Confirm it's you**; select **Lost every authenticator?**
2. Ask an Owner or Administrator of your organisation, in person or by phone, to confirm your
   recovery. They give you a confirmation.
3. Type one of your recovery codes and the confirmation, then select **Recover my account**.
4. Keep the ten new recovery codes Fyniq shows once, then add a new passkey on your profile.

- Your other sessions end, and you and every Owner are told.
- For 24 hours approving, releasing payments, changing bank details, roles or sign-ins, break-glass
  and confirming others' recoveries wait.
- Email alone or a support request never recovers an account.

## Ask to join by your email domain

If your company lets people join by their work email, and you sign in with an account whose
verified email is on its domain, the sign-in page offers **Ask to join** with the company's name.

1. Type your name as others will see it, then select **Ask to join**.
2. An Owner of the company approves or rejects it. Until they approve it you see nothing of the
   company.
3. Once approved, sign in again: you start as an Employee, and an Owner can give you other roles.

## Join a company with an invitation

1. Sign in as above. Fyniq shows **Joining by invitation**, because you have no account yet.
2. Enter the **Invitation code** and **Your name**, then select **Accept the invitation**.
3. You join the company with the roles its invitation gives you, and Fyniq opens.

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
| Sign-in failed (AUTH-SIGNIN-08): that provider accepts only accounts on some email domains | Sign in with your work account at that provider, or choose another way to sign in |
| The recovery is not valid (STORAGE-RECOVERY-01) | Check both codes. A recovery code works once, and a confirmation for 24 hours: ask for a new one |
| Held while your account cools off after its recovery (API-AUTHZ-08) | Do it after the time shown, or ask another member to do it |
| That invitation code is not valid, was used already or has expired | Ask whoever invited you for a new invitation |
| A second factor is required (API-AUTHZ-07) | Sign in again with your authenticator app, or confirm it's you with a passkey |
| You have no passkey (API-PASSKEY-02) | Sign in again with your authenticator app, then add a passkey |
| The passkey's answer was refused (API-PASSKEY-01) | Try again on this site, with a passkey you added here, and confirm with your fingerprint, face or PIN |
| The challenge cannot be used (API-PASSKEY-03) | It was used already or more than 5 minutes passed: start again |

## Related

- [Set up a new installation](setup-first-run.md)
- [Your profile and security](profile-and-security.md)
