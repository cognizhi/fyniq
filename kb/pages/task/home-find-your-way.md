---
key: home.overview
type: task
title: The Home page, search and commands, and signing out
summary: What Home shows once signed in, how to find a page or run a command with Search and commands (Ctrl+K), and how to sign out
sources:
  - docs/user-guide/home.md sha256:40cc51aabb24494f13e9865944a28d9d3b6359c15fd9597cbb3eda02a4ccd898
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-20
aliases:
  - command palette
  - quick search
  - home page
  - log out
  - where is everything
---

# The Home page, search and commands, and signing out

## What Home shows

- Home is where Fyniq opens after sign-in.
- It names the organisation the person is signed in to and the currency its books are kept in.
- Workspace pages appear as each module arrives; a person only sees the pages their roles allow.
- Without a session Fyniq opens sign-in ([[auth.sign-in]]); a new installation opens setup ([[installation.setup]]).

(src: docs/user-guide/home.md#what-it-is) (src: docs/user-guide/home.md#before-you-start)

## Find a page or run a command

- Select **Search and commands** in the header, or press **Ctrl+K** (**⌘K** on a Mac).
- Type part of a page's name or a command, pick a result with the arrow keys and press **Enter**; **Esc** closes it.
- Results marked **Expert** belong to Expert mode.
- **Appearance** in the header changes theme, high contrast, density and motion: [[preferences.appearance]].

(src: docs/user-guide/home.md#find-anything-with-search) (src: docs/user-guide/home.md#change-how-fyniq-looks)

## Sign out

- Select **Sign out** in the header: the session in this browser ends and the sign-in page opens.

(src: docs/user-guide/home.md#sign-out)

## Common problems

- "The API is not running.": Fyniq cannot reach its server; try again in a minute, or start it with `make dev` on a local install.
- "You are not a member of an organisation yet.": ask an Owner or Administrator for an invitation ([[members.invite]]).

(src: docs/user-guide/home.md#common-problems)
