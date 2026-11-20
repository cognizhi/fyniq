---
key: settings.connected-apps
title: Connect an AI app, and see what it did
mode: basic
roles: [owner, administrator, controller, accountant, bookkeeper, ar_specialist, ap_specialist, payroll_administrator, payment_releaser, approver, tax_agent, dpo]
requirements: [AI-16, AI-35, AI-37, AI-42, AI-44, AI-46, AI-89, AI-90]
status: draft
---

# Connect an AI app, and see what it did

## What it is

**Settings → Connected apps** shows the AI apps that work for you, such as Claude Code or Claude Desktop. An app reads your company's
books as you, and only what you may see; it can propose a change, which a person approves in Fyniq. It cannot approve, pay, export or
change settings, and it never sees an NRIC, a full bank account number or a secret.

## Who allows what

- An **Owner** turns AI apps on for the organisation and adds each app that may connect, in the organisation's settings: each needs a
  second approver. Taking an app off, or turning AI apps off, takes effect at once, and the tokens the people's apps hold for it end too:
  the whole switch ends every app's tokens, and taking one app off ends only that app's.
- **You** allow AI apps to work for you, with **Allow AI apps to work for me**. Nothing connects as you until you do. **Stop AI apps working
  for me** ends your apps' access at once, and their tokens.
- Each time an app asks to connect, you sign in with your second factor and see what it asks for. Reports are the default; the other
  areas are your choice, one at a time. Payroll and equity are never given to an app by default.

## When an app asks to connect

- The app sends you to Fyniq, to **Connect an app**. Sign in if you are asked, and confirm it's you with your second factor: nothing is
  allowed without it.
- The page says which app asks and whether Fyniq verified who publishes it. An app that registered itself says so, and shows where it sends
  you after you answer: check that address.
- Each area it asks for has a tick box, and you keep only the ones you want. An area that cannot be allowed (payroll and equity, or one an
  Owner has not granted) is shown as **Not granted**.
- **Allow** sends you back to the app, which can then read as you; **Deny** tells the app you refused. Either way, nothing else changes.

## What you see

- **Apps**: the apps the organisation can allow, with **Verified** for those Fyniq registered (with their publisher) and **Not verified** for
  an app that registered itself, and whether each is **Allowed**.
- **What you have allowed**: each app you connected, what it can read, when you allowed it and when to renew (after 90 days). **Disconnect**
  ends it at once.
- **Waiting for an Owner**: apps that registered themselves, where each sends you to sign in. Check that address before you allow one. An Owner
  approves an app by adding its ID to the apps that may connect, or **Deny**s it.
- **What the apps did**: which app read with which tool, and whether it was refused or limited.
- **Last used** (Expert mode): beside each app, when it last made a call, or that it has not been used.
- **What everyone has allowed** (an Owner): each person's connections, with **Disconnect** for any of them. It ends that person's
  tokens for that app at once; their other apps, their sign-in and their integrations are not touched.
- **Integration clients and personal access tokens**: the systems and the developer tokens that reach your books through the API, with
  their class, access and end date. They are made and revoked under Settings, Integrations, not here.

## Apps from other publishers

- An Administrator registers an AI app of another publisher by hand under **Apps from other publishers** (Expert mode), with a passkey or authenticator code: its name, who publishes it, one to five
  exact sign-in addresses, what the organisation uses it for, and where the publisher processes what it reads. Fyniq checks the name and the addresses; it does not contact the publisher or
  fetch anything, and it has not verified who publishes the app.
- Registering is not allowing. The app cannot connect until an Owner adds its ID to the apps that may connect, with a second approver, and registering is closed until an Owner has turned on **AI apps
  through MCP** and **An Administrator may register an AI app of another publisher**. An app that processes outside Singapore needs the PDPA transfer assessed first.
- Every registration makes a data inventory entry and appears in the quarterly access review. **Remove the app** takes it away at once; its record stays.

## Changes an app proposes

- An app can only read, and **propose** the kinds of change the Owner has turned on under Settings, AI. Each kind it asks for is a row on
  the connect page, **Suggest: ...**, which you tick or not. A proposal waits in Approvals, marked as made by an AI app, with what it
  would do in Fyniq's own words. Nothing changes until a person approves it, and an app can never approve, reject, cancel or run one.
- Another member who may approve that kind of change approves it as for any proposal. You can approve one an app made for you only
  after you confirm it's you again **after** the app proposed it, and only the simplest kind; an earlier confirmation does not count.
- An app can list and check the status of the changes it proposed for you, and no one else's.
- The Owner sets how many changes an app may propose an hour (Settings, AI apps); over it, the app is told to wait.

## If an app is refused

- An app is refused when the organisation has not turned AI apps on, has not allowed that app, or you have not allowed AI apps to work for
  you: the app says which. A limit on how often an app may ask is set by the Owner, and the app is told to wait.

## Related

- [Organisation settings](organisation-settings.md)
