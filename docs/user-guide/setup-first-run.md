---
key: installation.setup
title: Set up a new installation
mode: basic
roles: [super_admin]
requirements: [FR-ADM-06, FR-ADM-07]
---

# Set up a new installation

## What it is

A new installation of Fyniq opens **Set up Fyniq**. The first person to finish becomes the Super
admin of the installation and the Owner of its first organisation. Afterwards the page closes for
good.

## Before you start

- Get the one-time setup code. Whoever runs Fyniq prints it with `make setup-code`; in the cloud it
  is in Secret Manager. Keep it to yourself.
- Know the company's name and the currency its books are kept in. The base currency cannot change
  later.
- Have a printer or paper ready: you get ten recovery codes, shown once.

## Steps

1. Open Fyniq and select **Continue with** your provider to sign in. The sign-in you use becomes
   the Super admin's.
2. Enter **Your name** and select **Continue**.
3. Enter the **One-time setup code** and select **Continue**.
4. Enter the **Company name**, choose the **Base currency** and select **Create organisation**.
5. Print or write down your ten recovery codes and keep them offline. Tick **I have kept these
   codes somewhere safe, offline.** and select **Continue**.
6. Recommended: select **Create an invitation** and give the code to the other founder in person or
   on a call, never in a shared chat or a ticket. It works once and expires after 7 days. They sign
   in and enter it (see [Sign in to Fyniq](sign-in.md)).
7. Select **Continue**, or **Later** to skip the invitation, then **Go to Fyniq**.

Inviting a Super admin needs a sign-in with your second step in the last 5 minutes. If Fyniq asks
you to confirm it's you, select **Sign in again**; you come back to the invitation.

## Common problems

| Message | What to do |
|---|---|
| That setup code isn't right, or this installation is set up already | Copy the code again from where it was printed. Case, spaces and dashes do not matter |
| Your sign-in has ended | Sign in again, then enter the setup code again |
| This installation is set up | The setup page is closed. Select **Sign in** |

## Related

- [Sign in to Fyniq](sign-in.md)
