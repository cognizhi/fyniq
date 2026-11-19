---
key: settings.ai-activity
type: task
title: See what the AI did, and use your own AI plan
summary: Settings › AI activity: model calls, outcomes, tokens and cost, the assistant's questions, reviewing feedback on answers, masked payroll; your own AI plan
sources:
  - docs/user-guide/ai-activity.md sha256:773acd27a8dec35053e2c84012e4d58c5f0557a10c17260628d0a7cc6f07834c
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-15
aliases:
  - AI activity log
  - what did the AI do
  - AI calls
  - AI cost per call
  - my own ChatGPT plan
  - use my own Claude subscription
  - thumbs down on an answer
  - review AI feedback
  - golden set
  - what the AI did with my data
  - AI data use
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

## Questions to the assistant

- Each question asked of the assistant ([[ai.assistant]]), above the model calls: when, who asked (for the Owner and the data protection officer), the question and the classes of data it read, how it ended (**Answered**, **Refused** with its code, or **Failed**), whether the answer was asked for again after a figure failed the check, each read of the books with how many figures it read, and its tokens.
- Beside each, its reads refused, what screening stopped or withheld, and its cost in the base currency. What a read returned is never shown, only how it went; **Its model calls** shows the calls the question made. Someone else's question that read payroll is masked for a reader who may not see payroll unmasked.
- A read of the knowledge base names the know-how page it read and that page's version, so the exact text behind an answer can be found again; the text itself is not shown here.

(src: docs/user-guide/ai-activity.md#questions-to-the-assistant)

## Feedback on answers

- Members rate the assistant's answers with a thumbs up or down and a reason, once per answer; their conversation shows the rating and where its review is.
- The Owner and an Administrator see **Feedback on answers** here: each rating, its reason, who gave it, the question and the answer; **Show** picks waiting, golden set, dismissed or all.
- **Add to the golden set** puts it in the set the assistant's evals are built from; **Dismiss** leaves it out. Reviewed once; unreviewed feedback never feeds the evals.
- Feedback on someone else's question that read payroll is masked for a reviewer who may not see payroll unmasked and can only be dismissed; someone who sees payroll can add it. Showing it unmasked is recorded in the audit trail.

(src: docs/user-guide/ai-activity.md#feedback-on-answers)

## Masked entries

- Someone else's entry that carried payroll is masked, its detail withheld, for a reader who may not see payroll unmasked; showing one unmasked is recorded in the audit trail.

(src: docs/user-guide/ai-activity.md#masked-entries)

## Filter

- By agent, by day, and for the Owner and the data protection officer by member. **Its fallbacks** shows one call with every step it tried; **Show every call** goes back; **Show more** loads the next page.

(src: docs/user-guide/ai-activity.md#filter)

## Your own AI plan

- **Profile → Use your own AI plan** is not offered yet; the assistant uses only the organisation's own connections ([[settings.ai-providers]]). A consumer chat plan meant for the provider's apps, or a reused chat session or browser cookie, is never supported.

(src: docs/user-guide/ai-activity.md#your-own-ai-plan)
