---
key: settings.ai
type: task
title: Turn the AI off at once, ask to turn it on, and set the AI's monthly budgets
summary: Settings › AI, the AI switch off at once and on with a second approver, budgets in money and tokens with their 80% alert and 100% stop, and the monthly usage
sources:
  - docs/user-guide/ai-settings.md sha256:1bd4d71019f28aac2fd78ee95fa72c8eb081387a34c4dd0ecac3cc3ddc111b1a
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-14
aliases:
  - turn off the AI
  - kill switch
  - AI switch
  - AI budget
  - token budget
  - AI spending limit
  - AI usage
  - AI cost
---

# Turn the AI off at once, ask to turn it on, and set the AI's monthly budgets

## What it is

- **Settings → AI** (Expert mode) holds the AI switch and the monthly budgets, and shows the providers and model routes at a glance; **AI usage and budgets** shows what the assistant and the agents used and cost each month.
- The Owner, the Administrator, the Controller, the Auditor and the Compliance officer can see them. Where the AI's settings sit among the company's: [[settings.organisation]]; the connections the AI uses: [[settings.ai-providers]].

(src: docs/user-guide/ai-settings.md#what-it-is)

## The AI switch

- While it is off, nothing is sent to any AI provider; it is off until someone turns it on.
- **Ask to turn it on** asks another member who approves settings, such as an Owner, to approve it ([[approvals.decide]]); connections can still be set up and tested while it is off.
- **Turn all AI off now** is the Owner's and takes effect at once, with no second approver: new requests stop, and a reply being written stops within seconds. Accounting, checks and deadlines carry on. Anyone who may change settings can also turn it off under **Settings → Organisation → Features**.

(src: docs/user-guide/ai-settings.md#the-ai-switch)

## Budgets

- A monthly limit for the organisation, an agent, a connection or a person, in the base currency, or in tokens for the organisation or a person; a token budget also limits a subscription.
- Before each call Fyniq sets aside the most it could cost and the most tokens it could use against every budget that applies; a call that does not fit is not started.
- At 80% the Owners are alerted, once a month; at 100% no new call starts until the month ends or the Owner raises the budget. Probes, evals and retries count too.
- Only the Owner adds, changes or removes a budget (**Add a budget**, **Change**); an empty amount removes it.

(src: docs/user-guide/ai-settings.md#budgets)

## Usage and budgets

- **AI usage and budgets** shows a month at a time: spent, tokens, requests and fallbacks, then the use by agent, by connection and model, and by person, each beside its budget.
- Costs are exact, in the base currency at each day's exchange rate; a subscription shows its quota used and when it resets.
- Enter a provider's invoice as a bill, and compare it with the usage here.

(src: docs/user-guide/ai-settings.md#usage-and-budgets)
