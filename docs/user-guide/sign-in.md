---
key: auth.sign-in
title: Sign in to Fyniq
mode: basic
roles: [all]
requirements: [FR-ADM-08, SEC-05, SEC-08]
---

# Sign in to Fyniq

## What it is

Fyniq signs you in with an account you already have at a sign-in provider, such as Google or Zoho.
The Super admin chooses which providers this installation offers.

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

## Related

- [Set up a new installation](setup-first-run.md)
