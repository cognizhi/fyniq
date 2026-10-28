---
key: auth.sign-in
type: task
title: Sign in to Fyniq, confirm it's you with a passkey, or join with an invitation
summary: Signing in with a second factor, passkeys, your devices, when a session ends, joining a company or as the second Super admin by invitation, and errors
sources:
  - docs/user-guide/sign-in.md sha256:246fe91eb85a49b70bbc93f6111c3ce2ea9d7f0a6850ecd308566afc464d7f67
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-28
aliases:
  - log in
  - login
  - signed out
  - session expired
  - invitation code
  - join a company
  - accept an invitation
  - passkey
  - two-factor
  - 2FA
  - authenticator app
  - confirm it's you
  - devices
  - sign out everywhere
  - lost phone
  - sign-in email
---

# Sign in to Fyniq, confirm it's you with a passkey, or join with an invitation

## How sign-in works

- Fyniq signs a person in with an account they already have at a sign-in provider, such as Google or Zoho.
- The Super admin chooses which providers the installation offers.
- Every user signs in with a second factor: an authenticator-app code the provider asks for, or a passkey; a sign-in without one can only confirm it's you with a passkey until it does.
- A session ends after 30 minutes without activity, and after 12 hours in all; signing in again returns to the page the person was on.

(src: docs/user-guide/sign-in.md#what-it-is)

## Steps

- Open Fyniq: without a session it opens **Sign in to Fyniq**.
- Select **Continue with** the provider and sign in there; Fyniq returns to the page.
- **Sign out** leaves.

(src: docs/user-guide/sign-in.md#steps)

## Passkeys

- A passkey confirms it's you with a fingerprint, face or device PIN instead of a code; asking for or approving a change needs that confirmation in the last 5 minutes ([[approvals.decide]]).
- Add one in **Settings › Profile and security** within 5 minutes of signing in with the authenticator app, and name it, such as "Work laptop" ([[preferences.security]]).
- When Fyniq asks, choose the passkey and confirm on the device; the confirmation counts for 5 minutes.
- Revoke a passkey on a lost device: it works no more.
- Each request to confirm works once, for 5 minutes, and only on this site.

(src: docs/user-guide/sign-in.md#confirm-its-you-with-a-passkey)

## Your devices

- Each sign-in sends an email, to the address the provider confirmed, with when, which provider and which browser; if it was not you, end that session.
- **Your devices**, in **Settings › Profile and security**, lists every browser signed in, with when it signed in and was last used; the current one is marked.
- **Sign out** next to a device ends that session, such as on a lost phone, at its next request.
- **Sign out everywhere else** ends every session but the current one.

(src: docs/user-guide/sign-in.md#your-devices)

## Joining a company with an invitation

- A person invited to a company signs in, sees **Joining by invitation**, enters the **Invitation code** and **Your name**, and selects **Accept the invitation**.
- They join the company with the roles its invitation gives them, and Fyniq opens; making the invitation: [[members.invite]].

(src: docs/user-guide/sign-in.md#join-a-company-with-an-invitation) (src: docs/user-guide/sign-in.md#before-you-start)

## Joining as the second Super admin

- The person needs the invitation code the first Super admin gave them, and an account at a listed provider.
- After signing in they see **Joining by invitation**: they enter the **Invitation code** and **Your name** and select **Accept the invitation**.
- They keep their ten recovery codes offline, tick the confirmation and select **Go to Fyniq**.
- An invitation works once, within 7 days. Setting up the installation and creating the invitation: [[installation.setup]].

(src: docs/user-guide/sign-in.md#join-as-the-second-super-admin) (src: docs/user-guide/sign-in.md#before-you-start)

## Sign-in errors

- AUTH-SIGNIN-01: the provider is not available; choose another way to sign in.
- AUTH-SIGNIN-02: the sign-in took longer than 10 minutes or began in another browser; sign in again in one browser.
- AUTH-SIGNIN-03: the provider's answer did not pass Fyniq's checks; sign in again, and tell the Super admin if it repeats.
- AUTH-SIGNIN-04: the sign-in service did not answer; try again in a moment.
- An invitation code that is not valid, used or expired: ask whoever invited you for a new invitation.
- API-AUTHZ-07, a second factor is required: sign in again with the authenticator app, or confirm with a passkey.
- API-PASSKEY-02, no passkey: sign in again with the authenticator app, then add a passkey.
- API-PASSKEY-01, the passkey's answer was refused: try again on this site with a passkey added here, confirming with fingerprint, face or PIN.
- API-PASSKEY-03, the challenge cannot be used: it was used already or 5 minutes passed; start again.

(src: docs/user-guide/sign-in.md#common-problems)
