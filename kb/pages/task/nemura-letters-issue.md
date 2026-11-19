---
key: nemura.letters.issue
type: task
title: Check, sign and issue letters in Nemura
summary: How the people team checks a letter request and the signatory signs it with a passkey, and what the issued letter proves
sources:
  - docs/user-guide/nemura/letters-issue.md sha256:e2f6afbdf1f92580296c5c2bd3e70a7b54e094e1d61edc039ffc93c86099104d
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - sign a letter
  - issue a letter
  - letter evidence
  - passkey signature
---

# Check, sign and issue letters in Nemura

## How it works

- **Letters** lists every request, oldest due first. A letter is issued in two steps: an HR officer checks it, then the signatory (the Owner) signs it with a passkey. Once issued it is kept as evidence that nothing can change. (src: docs/user-guide/nemura/letters-issue.md#what-it-is)

## Steps

1. The HR officer selects **Check**: Nemura fills the template, draws the PDF, keeps it and gives it a reference; nobody checks their own letter. To refuse, write a reason and select **Refuse**.
2. The signatory selects **Read the letter**, compares the hash shown, then **Sign with my passkey**; the letter is issued once.
3. The person finds it under their requests ([[nemura.letters.request]]).

(src: docs/user-guide/nemura/letters-issue.md#steps)

## What an issued letter proves

- The exact PDF is hashed and kept with the template version, the data, the signatory, their authority and what the passkey confirmed.
- Whenever the letter is read its bytes are checked against that hash, and a changed byte is never served. A passkey approval is the platform's proof of who approved, not a signature inside the PDF.

(src: docs/user-guide/nemura/letters-issue.md#what-an-issued-letter-proves)

## When it looks wrong

- The hash confirmed is not the checked letter's: open the letter again and confirm the hash shown.
- The stored letter no longer matches its hash: it is not signed or shown, and the security team looks into it.
- The passkey was not confirmed: nothing is signed; try again within five minutes.

(src: docs/user-guide/nemura/letters-issue.md#common-problems)
