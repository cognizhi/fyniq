---
key: nemura.letters.templates
type: task
title: Set up letter types and templates in Nemura
summary: How the letterhead, the letter types and each template are set, who approves a template, and why a published version never changes
sources:
  - docs/user-guide/nemura/letters-templates.md sha256:47da9020e50214accf766314d31b2e6f99a4b81a620edf89f7ede47228ae0219
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - letter template
  - letterhead
  - merge fields
  - template version
---

# Set up letter types and templates in Nemura

## What it is

- **Letter templates** sets the letterhead, chooses which letters the company issues and writes each letter's template: blocks of text with merge fields from the record, with no free code.
- A template is a draft until the HR administrator approves it, then a numbered version that never changes. (src: docs/user-guide/nemura/letters-templates.md#what-it-is)

## Steps

1. Under **Letterhead**, enter the company name, UEN and address and save.
2. Under **Letter types**, tick **Issued** for each letter the company issues and set the working days it takes.
3. Choose a letter type, write the blocks (letterhead, heading, paragraphs, signature) with fields written like {{person.legal_name}}, and select **Save the draft**; every problem is reported at once.
4. The HR administrator or the Owner, not the author, selects **Approve and publish the draft**; it becomes the next version and the one before is superseded.

(src: docs/user-guide/nemura/letters-templates.md#steps)

## When it looks wrong

- A field the type may not show: remove it or choose another type.
- A template needs a signature block at the end.
- Nobody approves their own draft; a published version cannot be edited, so a new draft is published as the next version. See [[nemura.letters.issue]].

(src: docs/user-guide/nemura/letters-templates.md#common-problems)
