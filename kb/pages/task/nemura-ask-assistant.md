---
key: nemura.ask-assistant
type: task
title: Ask the people assistant about your leave, pay day and payslip in Nemura
summary: What the assistant answers about your own leave, pay day, pack rules and payslip lines, where its answers come from, and what it never does
sources:
  - docs/user-guide/nemura/ask-assistant.md sha256:e018e0760e9b2c2731b3dee0a2a0272996d77b4ece272162ca667b3d13b5aa7c
data_class: personal
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - ask the assistant
  - ask hr assistant
  - explain my payslip
  - leave balance question
---

# Ask the people assistant about your leave, pay day and payslip in Nemura

## What it is

- Ask the assistant answers questions about your own leave balance, your pay day, what a rule of the Singapore pack says and what a line of your payslip is. Each answer says where it comes from; a rule read from an official page but not yet verified a second time says to confirm it with HR. (src: docs/user-guide/nemura/ask-assistant.md#what-it-is)

## Steps

1. Open Ask the assistant from your home page, write your question and select Ask.
2. Read the answer and its sources; it is marked as an AI answer.

(src: docs/user-guide/nemura/ask-assistant.md#steps)

## What happens next

- A payslip line is explained as a rule and your own figures are on your payslip page beside it; no pay figure goes to a model and no one else's pay or records are read. It never approves, changes, issues or pays anything; your question is not kept. (src: docs/user-guide/nemura/ask-assistant.md#what-happens-next)

## When it goes wrong

- Off: the Owner turns it on ([[nemura.assistant]]). No match: ask another way or ask the people team ([[nemura.ask-hr]]). (src: docs/user-guide/nemura/ask-assistant.md#common-problems)
