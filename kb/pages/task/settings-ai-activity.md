---
key: settings.ai-activity
type: task
title: See what the AI did, and use your own AI plan
summary: Settings › AI activity: every model call, whose entries you see, outcomes, tokens and cost, masked payroll entries, filters; your own AI plan, not offered yet
sources:
  - docs/user-guide/ai-activity.md sha256:1d52021deeb5f201e092e7058e6a2514a982023942792dd7ff6a0bccdb8e9ea8
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-14
aliases:
  - AI activity log
  - what did the AI do
  - AI calls
  - AI cost per call
  - my own ChatGPT plan
  - use my own Claude subscription
---

# See what the AI did, and use your own AI plan

## What it is

- **Settings → AI activity** (Expert mode, under your own settings) lists every model call, newest first: when, for whom, the agent and its route step, the connection and model, what happened, the tokens and the cost. Every attempt is kept, served or not. Routes: [[settings.ai-routes]].

(src: docs/user-guide/ai-activity.md#what-it-is)

## Whose entries you see

- Each member sees their own; the Owner and the data protection officer see every member's and can filter by member. The page says **Your own entries** or **Every member's entries**.

(src: docs/user-guide/ai-activity.md#whose-entries-you-see)

## What each entry shows

- **Agent and step** (route version and step, the step it fell back from; a probe or qualification run is outside any route) and the classes of data it carried.
- **Connection and model**: the model asked for, the one the provider says answered if different, and its reported version.
- **Outcome**: **Served**, **Skipped** (with why), **Stopped**, or a failure such as **Timed out** or **Refused by the provider**.
- **Tokens in / out** and **Cost**, exact, in the base currency at the day's rate, the provider's currency beneath when it differs; no price, no cost. Tiles count served, fell back, failed or stopped, and masked in the entries shown.

(src: docs/user-guide/ai-activity.md#what-each-entry-shows)

## Masked entries

- Someone else's entry that carried payroll is masked, its detail withheld, for a reader who may not see payroll unmasked; showing one unmasked is recorded in the audit trail.

(src: docs/user-guide/ai-activity.md#masked-entries)

## Filter

- By agent, by day, and for the Owner and the data protection officer by member. **Its fallbacks** shows one call with every step it tried; **Show every call** goes back; **Show more** loads the next page.

(src: docs/user-guide/ai-activity.md#filter)

## Your own AI plan

- **Profile → Use your own AI plan** is not offered yet; the assistant uses only the organisation's own connections ([[settings.ai-providers]]). A consumer chat plan meant for the provider's apps, or a reused chat session or browser cookie, is never supported.

(src: docs/user-guide/ai-activity.md#your-own-ai-plan)
