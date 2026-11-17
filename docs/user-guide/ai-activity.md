---
key: settings.ai-activity
title: See what the AI did, and use your own AI plan
mode: expert
roles: [all]
requirements: [AI-12, AI-73, AI-74, AI-76]
---

# See what the AI did, and use your own AI plan

## What it is

**Settings → AI activity** (Expert mode, under your own settings) lists every model call the AI made, newest first:
when, for whom, which agent and step of its route, which connection and model, what happened, the tokens and the cost.
Every attempt is kept, whether it was served or not.

## Whose entries you see

- Each member sees their own entries. The Owner and the data protection officer see every member's, and can filter by
  member.
- The page says which you are seeing: **Your own entries** or **Every member's entries**.

## What each entry shows

- **Agent and step**: the agent, the route version and step it followed, and the step it fell back from, if it did.
  A probe or a qualification run is shown as outside any route. The classes of data it carried are listed beneath.
- **Connection and model**: the model asked for, the model the provider says answered if it differs, and the version
  it reported.
- **Outcome**: **Served**, **Skipped** (not sent, with why), **Stopped** (refused before sending, or cancelled), or a
  failure such as **Timed out** or **Refused by the provider**.
- **Tokens in / out** and **Cost**, exact, in the base currency at the day's rate, with the provider's own currency
  beneath when it differs. A call with no price shows no cost.
- The tiles above the list count, in the entries shown, how many were served, fell back, failed or stopped, and were
  masked.

## Masked entries

- Someone else's entry that carried payroll is masked for a reader who may not see payroll unmasked: its detail is
  withheld.
- When someone else's payroll entry is shown unmasked, the audit trail records it.

## Filter

- Filter by agent, by day, and, for the Owner and the data protection officer, by member.
- **Its fallbacks** shows one call with every step it tried; **Show every call** goes back.
- **Show more** loads the next page.

## Your own AI plan

- **Profile → Use your own AI plan** is not offered yet. The assistant uses only the organisation's own AI
  connections, which the Owner and the administrators choose.
- A consumer chat plan meant for the provider's own apps, and reusing a chat session or a browser cookie, are never
  supported.
