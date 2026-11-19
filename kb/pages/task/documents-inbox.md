---
key: documents.inbox
type: task
title: Document inbox
summary: Capturing a bill or receipt by upload, paste or accepted email; having the model read it; checking the reading; making a draft bill; duplicates and dismissals
sources:
  - docs/user-guide/document-inbox.md sha256:7b6880429587dd6ac1c9139508d9e1a10948507104c511a6c539afda19a31b41
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - document inbox
  - capture a bill
  - upload a bill
  - paste a receipt
  - email a bill
  - scan a receipt
  - read a document
  - bill from a document
  - automated bill entry
  - duplicate bill
  - model's reading
---

# Document inbox

## What it is

- **Purchases → Document inbox** keeps every bill and receipt that came in as a file: uploaded, pasted from the clipboard, or attached to mail from a sender you accepted that reached the company's email-in address. Each file is one row with a state: Waiting to be read, Read: check it, Could not be read, Duplicate, Drafted or Dismissed.
- A Bookkeeper, an AP specialist or an Accountant captures, reads and drafts.

(src: docs/user-guide/document-inbox.md#what-it-is)

## Capture a document

- **Upload a document** takes a PDF, a photo (PNG, JPEG, WebP; an iPhone's HEIC is converted), or a text or email file. It is checked as any file is: its type from its content, a malware scan, and its location data removed.
- Paste a screenshot anywhere on the page to capture it. The same file twice is shown once, as a Duplicate of the first.

(src: docs/user-guide/document-inbox.md#capture-a-document)

## Have it read

- **Have it read** asks the model to read the file into fields: the supplier, the number, the date, the currency, each line and the totals, each with how sure the model was. A field below 80% is flagged: check it first.
- The reading is shown beside the file, in the AI's violet, as what the model read. It is data to check against the document: it never decides a figure, whether GST is claimable, or what is posted.
- Fyniq adds its own checks: the lines and GST add up to the total, the dates are real, the GST registration number looks like one, and what a tax invoice must show but this document does not.
- The model is given the document's text with every bank account number, IBAN and NRIC or FIN number hidden first; you key a supplier's bank details yourself. Only a PDF whose text Fyniq can extract, or a text or email file, is read: a photo or a scanned PDF is kept, and you type the bill in.
- A document that tells the model to do something is stopped, and fails with the reason. Only a PDF or a photo is read; type anything else in by hand. **Read it again** reads it afresh.

(src: docs/user-guide/document-inbox.md#have-it-read)

## Make a draft bill

- **Make a draft bill** opens the bill with the reading filled in: choose the supplier (it names the one the reading matched), choose each line's account, correct what the reading got wrong, then **Save draft bill**.
- It is a draft, made by you: the document is attached to it as evidence, and nothing posts until it is checked and posted under your approval policy. Its input tax is yours to decide from the facts, never from the amount the model read ([[purchases.bills]]).
- A bill that repeats one already entered (the same supplier, number and total within 60 days) is refused; if it really is another, say why in the box and the reason is kept.

(src: docs/user-guide/document-inbox.md#make-a-draft-bill)

## Dismiss a document

- **Dismiss** ends a document nobody will draft, with a reason. A dismissed or drafted document is final.

(src: docs/user-guide/document-inbox.md#dismiss-a-document)
