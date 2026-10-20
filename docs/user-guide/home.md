---
key: home.overview
title: The Home page and finding your way around
mode: basic
roles: [all]
requirements: [FR-UX-01, FR-UX-29, FR-UX-31, FR-UX-40]
status: draft            # written with the app shell's first slice (P1); reviewed when the shell's screens are built
---

# The Home page and finding your way around

## What it is

**Home** is where Fyniq opens once you are signed in. It says which organisation you are signed in
to and which currency its books are kept in. The pages of your workspace appear as each module
arrives; you only ever see the pages your roles allow.

## Before you start

- Sign in first ([Sign in to Fyniq](sign-in.md)). If Fyniq has not been set up yet, it opens the
  setup page instead ([Set up a new installation](setup-first-run.md)).

## Steps

### Find anything with search

1. Select **Search and commands** in the header, or press **Ctrl+K** (**⌘K** on a Mac).
2. Type part of a page's name or a command, such as "dark".
3. Use the arrow keys to pick a result, then press **Enter**. **Esc** closes the search.

Results marked **Expert** belong to Expert mode.

### Change how Fyniq looks

Select **Appearance** in the header to change the theme, high contrast, density and motion. See
[Change how Fyniq looks](appearance.md).

### Settings

Select **Settings** at the foot of the sidebar (on a phone, under **More**). It lists your own
settings first, such as [Your profile and security](profile-and-security.md), then the
organisation's settings your roles let you see. Most of the organisation's settings are in Expert
mode: in Basic mode, Settings says how many more there are and offers to switch.

### Sign out

Select **Sign out** in the header. Fyniq ends your session in this browser and returns you to the
sign-in page.

## Common problems

- **"The API is not running."** Fyniq cannot reach its server. Try again in a minute; if you run
  Fyniq on your own computer, start it with `make dev`.
- **"You are not a member of an organisation yet."** You are signed in, but no organisation has
  invited you. Ask an Owner or Administrator to invite you
  ([Invite people and give them roles](members-and-roles.md)).

## Related

- [Change how Fyniq looks](appearance.md)
- [Sign in to Fyniq](sign-in.md)
- [Your profile and security](profile-and-security.md)
