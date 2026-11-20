---
key: nemura.verify-letter
type: task
title: Verify a letter
summary: How anyone checks an issued letter by its code or its file, what the page shows, how a person withdraws it and shares its facts by an expiring link
sources:
  - docs/user-guide/nemura/verify-letter.md sha256:1a0db2b238c6c3571f9d8fcc550a1b366179593d3a46812c5e4bdeb4a477dae5
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

## Comparing a document

- With the letter as a file, choose Compare a document: the browser sends only the file's SHA-256, never the file, and the answer is the same as with the reference and code. A document that is not an issued letter, or changed by one byte, is not found. (src: docs/user-guide/nemura/verify-letter.md#compare-a-document-instead)

## Withdrawing it

- The person a letter is about can withdraw its verification, with Withdraw verification under My letters in My requests; a look-up then says withdrawn and any link they shared stops showing facts. The letter itself is not withdrawn. (src: docs/user-guide/nemura/verify-letter.md#withdraw-the-verification-of-your-own-letter)

## Sharing what the letter says about you

- The verification shows nothing of the person. The person can share the letter's facts through a link they make for an issued letter: it works for 1 to 30 days (7 by default), is shown once, and shows facts such as the name, job title and start date with the kind, day and company, never what the letter is for. (src: docs/user-guide/nemura/verify-letter.md#share-what-your-letter-says-about-you)
- They revoke a link with Revoke beside it; revoking never withdraws the letter or its verification. (src: docs/user-guide/nemura/verify-letter.md#share-what-your-letter-says-about-you)

## When it looks wrong

- Nothing matches: check the reference and code against the letter. Too many tries for one reference in an hour are refused: wait. A shared link that shows nothing may have expired or been revoked, or the person withdrew the verification: ask them for a new one. (src: docs/user-guide/nemura/verify-letter.md#common-problems)
