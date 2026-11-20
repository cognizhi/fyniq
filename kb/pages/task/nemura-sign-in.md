---
key: nemura.sign-in
type: task
title: Sign in to Nemura and sign out
summary: How a person signs in to Nemura, joins with an invitation or the one-time setup code, what the page says when it is not ready, and how to sign out
sources:
  - docs/user-guide/nemura/sign-in.md sha256:ad074fab24c1a9ab4da9195372ee2657645169ce5c767bb79123fe4b8a894d09
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-19
aliases:
  - log in to Nemura
  - Nemura login
  - sign out of Nemura
  - Nemura is not set up yet
  - invitation code
  - setup code
  - recovery code
  - Owner cannot sign in
  - Finance People switch
---

# Sign in to Nemura and sign out

## What it is

- Nemura has its own sessions, so people sign in even when the company does not use Fyniq.
- It never asks for an NRIC to sign in. (src: docs/user-guide/nemura/sign-in.md#what-it-is)

## Before you start

- The company sends an invitation; it is opened after signing in.
- **Nemura is not set up yet** means nobody has created the first organisation. (src: docs/user-guide/nemura/sign-in.md#before-you-start)

## Steps

1. Open Nemura's address.
2. Select **Continue with** the provider the company uses.
3. Sign in at the provider; Nemura then opens **My home**, or **People home** for the people team. See [[nemura.home]].

(src: docs/user-guide/nemura/sign-in.md#steps)

## Join with a code

- **I have an invitation**: choose it on the sign-in page, pick the provider and type the invitation code. The code links the account to the place the company made; it works once and expires.
- **Set up Nemura** is for the person who installs it, once, with the one-time setup code the installation prints; that person becomes the first organisation's Owner.
- **I have a recovery code** is for an Owner who can no longer sign in: two operators of the installation issue the code (valid 24 hours, works once); signing in with the account to keep ends every other sign-in, session and passkey, so a passkey is registered again afterwards.
- The code is never in the address bar. A wrong, used or expired code returns to sign-in with a message; ask for a new one.

(src: docs/user-guide/nemura/sign-in.md#joining-with-a-code)

## Move between Nemura and Fyniq

- When the company links Nemura to Fyniq and the person holds a role in Fyniq's finance workspace, a **Finance | People** switch shows in the header. Fyniq tells Nemura, signed, that the sign-in account holds such a role and nothing more: no role name and no email.
- The switch is a door and never a grant: Fyniq's own sign-in and roles decide what the person sees there, and a role in one app gives nothing in the other. It opens Fyniq with the sign-in provider used here, so the sign-in page is not shown again; if the provider asks for a confirmation, give it.
- Nemura and Fyniq use one sign-in provider for the company, so use the same account in both. If the switch does not show, Fyniq has not said the account holds a finance role: ask the Owner.

(src: docs/user-guide/nemura/sign-in.md#moving-between-nemura-and-fyniq)

## Sign out

- **Sign out** is at the top of every page. It ends the session on this device only; other devices stay signed in.
- When the company links Nemura to Fyniq and the person is mapped to a person there, signing out here also signs them out of Fyniq on every device, and being removed from Nemura does the same, within a minute or so. Fyniq ends nothing for a person nobody mapped, and the person stays a member there with their roles and passkeys. Being removed from Fyniq ends their Nemura sessions in the same way; signing out of Fyniq on its own does not yet. (src: docs/user-guide/nemura/sign-in.md#signing-out)

## When it fails

- **No way to sign in is set up yet**: no provider is connected; ask whoever runs Nemura.
- **We could not reach Nemura**: check the connection and select **Try again**.
- **Not a member of any organisation here**: ask the company for an invitation, then choose **I have an invitation**.
- Sent back to sign-in after a while: the session ended; sign in again. (src: docs/user-guide/nemura/sign-in.md#common-problems)
