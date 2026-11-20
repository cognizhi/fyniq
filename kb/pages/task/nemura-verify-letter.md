---
key: nemura.verify-letter
type: task
title: Verify a letter
summary: How anyone checks an issued letter by its reference and printed code, what the page shows and how a person withdraws it
sources:
  - docs/user-guide/nemura/verify-letter.md sha256:9b9f725e3589931577b0f765d5bfde8a9da5d7c48a129594f4f8027a7390a17e
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-21
aliases:
  - letter verification
  - check a letter
---

# Verify a letter

## What it is

- Every issued letter carries a reference and a verification code in its footer. Anyone can enter both on the verification page, with no sign-in, to learn whether the company issued the letter and it stands. (src: docs/user-guide/nemura/verify-letter.md#what-it-is)

## Steps

1. Open the verification page and enter the reference and the code as printed; capitals, dashes and spaces do not matter.
2. Select **Verify**: the answer is Valid or Withdrawn, the kind of letter, the day it was issued and the company, and nothing of the person or the words.

(src: docs/user-guide/nemura/verify-letter.md#verify-a-letter)

## Withdrawing it

- The person a letter is about can withdraw its verification; a look-up then says withdrawn. The letter itself is not withdrawn. (src: docs/user-guide/nemura/verify-letter.md#withdraw-the-verification-of-your-own-letter)

## When it looks wrong

- Nothing matches: check the reference and code against the letter. Too many tries for one reference in an hour are refused: wait. (src: docs/user-guide/nemura/verify-letter.md#common-problems)
