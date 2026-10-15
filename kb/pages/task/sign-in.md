---
key: auth.sign-in
type: task
title: Sign in to Fyniq, or join as the second Super admin
summary: How to sign in with a provider, when a session ends, how the second Super admin joins with an invitation code, and what each sign-in error means
sources:
  - docs/user-guide/sign-in.md sha256:91075e4ebc067b9014ef4e5af6de22091fbd99ee7612737746ae66c13e37b102
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-15
aliases:
  - log in
  - login
  - signed out
  - session expired
  - invitation code
---

# Sign in to Fyniq, or join as the second Super admin

## How sign-in works

- Fyniq signs a person in with an account they already have at a sign-in provider, such as Google or Zoho.
- The Super admin chooses which providers the installation offers.
- A session ends after 30 minutes without activity, and after 12 hours in all; signing in again returns to the page the person was on.

(src: docs/user-guide/sign-in.md#what-it-is)

## Steps

- Open Fyniq: without a session it opens **Sign in to Fyniq**.
- Select **Continue with** the provider and sign in there; Fyniq returns to the page.
- **Sign out** leaves.

(src: docs/user-guide/sign-in.md#steps)

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
- An invitation code that is not valid, used or expired: ask the Super admin for a new invitation.

(src: docs/user-guide/sign-in.md#common-problems)
