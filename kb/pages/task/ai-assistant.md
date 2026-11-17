---
key: ai.assistant
type: task
title: Ask the assistant about your books
summary: The assistant panel (Ctrl+J or ⌘J): ask in your own words, figures with source chips, what it never sees, your conversations, when it does not answer
sources:
  - docs/user-guide/ai-assistant.md sha256:756092b6dc29fb1f7b408195c7c0664c0a03be1375bb555c4fbd6a3f811985dc
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

## Where each figure comes from

- Fyniq writes every figure from the books, never the assistant; a chip beneath the answer names its source, such as a report and its months, and opens that report, ledger or record.
- A figure that cannot be established is said to be so, never guessed; one Fyniq could not check is left out, with how many were.
- Expert mode lists what each figure was read from and its ledger revision.

(src: docs/user-guide/ai-assistant.md#where-each-figure-comes-from)

## What it never sees

- NRIC and FIN numbers, bank account numbers, secrets and home addresses never reach it, even typed into a question.
- Payroll reaches it only with AI payroll access on (an Owner's switch, [[settings.organisation]]) and your own right to see payroll.

(src: docs/user-guide/ai-assistant.md#what-it-never-sees)

## Your conversations

- Yours alone. **Your conversations** lists them newest first; **New conversation** starts afresh.
- **Delete** removes one from your list at once, and Fyniq erases it overnight; kept ones are erased when the organisation's retention for them ends. A full conversation needs a new one.

(src: docs/user-guide/ai-assistant.md#your-conversations)

## When it does not answer

- **The assistant is switched off**: the Owner switched the AI off or has not switched it on ([[settings.ai]]); nothing is sent.
- **The AI budget is spent for this month**: not sent; an Owner can raise the budget. Too many questions in a minute or a day: it says so, and when to ask again.

(src: docs/user-guide/ai-assistant.md#when-it-does-not-answer)
