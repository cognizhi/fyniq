---
key: ai.assistant
type: task
title: Ask the assistant about your books
summary: The assistant panel (Ctrl+J or ⌘J): ask about this, explain, simulate, figures with sources, rating answers, what it never sees, conversations
sources:
  - docs/user-guide/ai-assistant.md sha256:00cbde2d6712c9e358d086770d8f8a15720bc3db763ab4e1925caa025f9d2797
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-15
aliases:
  - AI assistant
  - ask the AI
  - chat with my books
  - where does this figure come from
  - delete my AI conversation
  - assistant switched off
---

# Ask the assistant about your books

## What it is

- **Assistant** in the header, or **Ctrl+J** (**⌘J** on a Mac), opens it beside the page: beside it on a wide screen, over it on a tablet, full screen on a phone.
- Ask in your own words; it reads the books as you, sees only what you may see, and changes nothing. Everything it writes is marked **AI** in violet. Its activity is in [[settings.ai-activity]].

(src: docs/user-guide/ai-assistant.md#what-it-is)

## Ask about a record, a report or a figure

- **Ask about this** on an invoice, bill, journal, contact or report, or on a traced figure, opens the assistant with it as the context chip; **Remove** takes it off. Hidden while the AI is switched off.

(src: docs/user-guide/ai-assistant.md#ask-about-a-record-a-report-or-a-figure)

## Explain and simulate

- **Ask**: your own words. **Explain**: where the thing asked about comes from and what it is made of.
- **Simulate**, on an invoice or bill asked about: the lowest cash balance in the next 90 days and the first day below the buffer, now and if paid on another day or not in those 90 days. Fyniq's figures, nothing saved; more in **Cash ahead**.

(src: docs/user-guide/ai-assistant.md#explain-and-simulate)

## Where each figure comes from

- Fyniq writes every figure from the books, never the assistant; a chip beneath the answer names its source, such as a report and its months, and opens that report, ledger or record.
- A figure that cannot be established is said to be so, never guessed; one Fyniq could not check is left out, with how many were.
- Expert mode lists what each figure was read from and its ledger revision.

(src: docs/user-guide/ai-assistant.md#where-each-figure-comes-from)

## Ask how something works

- Ask how Fyniq works, what a status means or what your organisation decided; the assistant answers from the guide in its own words, never from memory, and only from guide pages the person may open. Related: [[help.guide]].
- A chip beneath the answer names the guide page each explanation comes from and opens it; it never links the assistant's own working notes. A chip marked **awaiting review** points to a guide page not yet reviewed, and the assistant says so.
- The guide holds no figures of the books, which always come from the books. For a tax rate, threshold or deadline the assistant checks Fyniq's statutory values and says when the guide may be out of date.

(src: docs/user-guide/ai-assistant.md#ask-how-something-works)

## What it never sees

- NRIC and FIN numbers, bank account numbers, secrets and home addresses never reach it, even typed into a question.
- Payroll reaches it only with AI payroll access on (an Owner's switch, [[settings.organisation]]) and your own right to see payroll.

(src: docs/user-guide/ai-assistant.md#what-it-never-sees)

## Rate an answer

- Thumbs up or down under each answer, with a reason, once. An Owner or Administrator reviews it with the question and the answer; only what they add to the golden set tests the assistant ([[settings.ai-activity]]).

(src: docs/user-guide/ai-assistant.md#rate-an-answer)

## Your conversations

- Yours alone. **Your conversations** lists them newest first; **New conversation** starts afresh.
- **Delete** removes one from your list at once, and Fyniq erases it overnight; kept ones are erased when the organisation's retention for them ends. A full conversation needs a new one.

(src: docs/user-guide/ai-assistant.md#your-conversations)

## When it does not answer

- **The assistant is switched off**: the Owner switched the AI off or has not switched it on ([[settings.ai]]); nothing is sent.
- **The AI budget is spent for this month**: not sent; an Owner can raise the budget. Too many questions in a minute or a day: it says so, and when to ask again.

(src: docs/user-guide/ai-assistant.md#when-it-does-not-answer)
