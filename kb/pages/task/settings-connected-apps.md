---
key: settings.connected-apps
type: task
title: Connect an AI app, and see what it did
summary: Settings › Connected apps: allow AI apps for yourself, what an Owner allows, verified apps, disconnecting, waiting registrations and what the apps did
sources:
  - docs/user-guide/connected-apps.md sha256:cf32c111be8991580a4f32d0893feb0109f2b1406d666cf9c7b2acda78fce22d
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-19
aliases:
  - connect Claude Code
  - connect Claude Desktop
  - AI apps
  - MCP
  - connected apps
  - disconnect an app
  - allow an AI app
  - app registered itself
---

# Connect an AI app, and see what it did

## What it is

- **Settings → Connected apps** shows the AI apps that work for you, such as Claude Code or Claude Desktop. An app reads the company's books as you, and only what you may see; it can propose a change, which a person approves in Fyniq. It cannot approve, pay, export or change settings, and never sees an NRIC, a full bank account number or a secret.

(src: docs/user-guide/connected-apps.md#what-it-is)

## Who allows what

- An **Owner** turns AI apps on for the organisation and adds each app that may connect, in the organisation's settings ([[settings.organisation]]): each needs a second approver; taking one off, or turning AI apps off, takes effect at once.
- **You** allow AI apps to work for you with **Allow AI apps to work for me**; nothing connects as you until you do, and **Stop AI apps working for me** ends your apps' access and tokens at once.
- Each time an app asks to connect, you sign in with your second factor and see what it asks for: reports are the default, the other areas are your choice, and payroll and equity are never given by default.

(src: docs/user-guide/connected-apps.md#who-allows-what)

## When an app asks to connect

- The app sends you to **Connect an app** in Fyniq; sign in if asked, and confirm it's you with your second factor, without which nothing is allowed.
- The page says which app asks and whether Fyniq verified who publishes it; an app that registered itself says so and shows where it sends you afterwards, which you should check.
- Each area it asks for has a tick box and you keep only the ones you want; an area that cannot be allowed (payroll and equity, or one an Owner has not granted) shows **Not granted**.
- **Allow** sends you back to the app, which can then read as you; **Deny** tells the app you refused.

(src: docs/user-guide/connected-apps.md#when-an-app-asks-to-connect)

## What you see

- **Apps**: the apps the organisation can allow, **Verified** for those Fyniq registered and **Not verified** for an app that registered itself, and whether each is **Allowed**.
- **What you have allowed**: each connected app, what it can read, when you allowed it and when to renew (after 90 days); **Disconnect** ends it at once.
- **Waiting for an Owner**: apps that registered themselves and where each sends you to sign in; check that address first. An Owner approves one by adding its ID to the apps that may connect, or **Deny**s it.
- **What the apps did**: which app read with which tool, and whether it was refused or limited.

(src: docs/user-guide/connected-apps.md#what-you-see)

## If an app is refused

- An app is refused when the organisation has not turned AI apps on, has not allowed that app, or you have not allowed AI apps to work for you; the app says which. The Owner sets how often an app may ask, and the app is told to wait.

(src: docs/user-guide/connected-apps.md#if-an-app-is-refused)
