---
key: settings.connected-apps
title: Connect an AI app, and see what it did
mode: basic
roles: [owner, administrator, controller, accountant, bookkeeper, ar_specialist, ap_specialist, payroll_administrator, payment_releaser, approver, tax_agent, dpo]
requirements: [AI-35, AI-37, AI-44, AI-90]
status: draft
---

# Connect an AI app, and see what it did

## What it is

**Settings → Connected apps** shows the AI apps that work for you, such as Claude Code or Claude Desktop. An app reads your company's
books as you, and only what you may see; it can propose a change, which a person approves in Fyniq. It cannot approve, pay, export or
change settings, and it never sees an NRIC, a full bank account number or a secret.

## Who allows what

- An **Owner** turns AI apps on for the organisation and adds each app that may connect, in the organisation's settings: each needs a
  second approver. Taking an app off, or turning AI apps off, takes effect at once.
- **You** allow AI apps to work for you, with **Allow AI apps to work for me**. Nothing connects as you until you do. **Stop AI apps working
  for me** ends your apps' access at once, and their tokens.
- Each time an app asks to connect, you sign in with your second factor and see what it asks for. Reports are the default; the other
  areas are your choice, one at a time. Payroll and equity are never given to an app by default.

## What you see

- **Apps**: the apps the organisation can allow, with **Verified** for those Fyniq registered (with their publisher) and **Not verified** for
  an app that registered itself, and whether each is **Allowed**.
- **What you have allowed**: each app you connected, what it can read, when you allowed it and when to renew (after 90 days). **Disconnect**
  ends it at once.
- **Waiting for an Owner**: apps that registered themselves, where each sends you to sign in. Check that address before you allow one. An Owner
  approves an app by adding its ID to the apps that may connect, or **Deny**s it.
- **What the apps did**: which app read with which tool, and whether it was refused or limited.

## If an app is refused

- An app is refused when the organisation has not turned AI apps on, has not allowed that app, or you have not allowed AI apps to work for
  you: the app says which. A limit on how often an app may ask is set by the Owner, and the app is told to wait.

## Related

- [Organisation settings](organisation-settings.md)
