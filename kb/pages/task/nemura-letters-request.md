---
key: nemura.letters.request
type: task
title: Ask for a letter in Nemura
summary: How an employee asks for a letter such as an employment confirmation, what it shows, who checks and signs it, and how to download or share it
sources:
  - docs/user-guide/nemura/letters-request.md sha256:0cc3eb1e77585d7c847fd66860c39860f0fbf54da511abf4504f8590708f7383
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - employment confirmation
  - request a letter
  - certificate of employment
  - letter for a bank
  - letter for a visa
---

# Ask for a letter in Nemura

## What it is

- A person can ask the company for a letter about them, such as an employment confirmation for a bank, a landlord or a visa. It is filled from their record and shows only facts of it: name, job title and start date, never salary unless the letter type is for that and they ask for it.
- The people team checks it and the signatory signs it. (src: docs/user-guide/nemura/letters-request.md#what-it-is)

## Steps

1. Open **Ask for a letter** and choose **Which letter?**
2. Write **What is it for?** and select **Send the request**; Nemura shows the day it is due.
3. Follow it under **My requests**; when issued, **Download**, or **Make a link that works for seven days**, which delivers that one letter only.

(src: docs/user-guide/nemura/letters-request.md#steps)

## What happens next

- An HR officer checks the letter, which draws the exact PDF and gives it a reference; nobody checks their own letter.
- The signatory reads that PDF and signs it with a passkey; the exact bytes are kept as evidence, and changing one byte breaks the match. See [[nemura.letters.issue]].
- A request not yet issued can be withdrawn; a refusal always comes with a reason.

(src: docs/user-guide/nemura/letters-request.md#what-happens-next)

## When it looks wrong

- The company does not issue that letter, or its template has no published version: ask the people team ([[nemura.letters.templates]]).
- A fact the letter needs is missing from the record: the people team records it first.
- The local build draws Latin letters only.

(src: docs/user-guide/nemura/letters-request.md#common-problems)
