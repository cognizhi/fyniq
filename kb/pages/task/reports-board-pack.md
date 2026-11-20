---
key: reports.board-pack
type: task
title: Build and publish the monthly board pack
summary: Starting a month's board pack, writing its texts, publishing it so its figures are frozen, keeping its PDF as evidence, correcting it, and who reads it
sources:
  - docs/user-guide/board-pack.md sha256:d6cd62d1dd16f62d74e493a48d0cc4041a390520f764fa41792701b1dde442bb
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-23
aliases:
  - board pack
  - publish the board pack
  - monthly board pack
  - correct a board pack
---

# Build and publish the monthly board pack

## Find it

- In Expert mode, **Reports** has a **Board pack**. An Owner, Controller or Accountant builds it; an Owner or Controller publishes it.

(src: docs/user-guide/board-pack.md#find-it)

## Start a month's pack

- **Start the pack** makes a draft for the month; a month has one draft at a time. Its figures (profit and loss, balance sheet, cash flow and the KPIs of the month) are read as they stand now, so the draft shows what would be published.

(src: docs/user-guide/board-pack.md#start-a-months-pack)

## Write the texts

- The highlights, the commentary and the asks of the board are written by a person. The pack never needs the AI: an AI draft of the commentary is accepted into the text and edited, and the text is the person's.
- **Save the texts** keeps them; each save is the next edit.

(src: docs/user-guide/board-pack.md#write-the-texts)

## Publish it

- **Publish** reads the figures at the ledger's revision at that moment and freezes them with the texts, so a posting made afterwards does not change the pack. A month that is not closed is published all the same, and the pack says so.
- **Download the PDF** saves the published pack as a PDF: the month's statements, each beside the period before, its key figures and the texts, as frozen. The file is the same each time, the export is recorded with its hash, a draft has no PDF, and a month that was not closed says so on the page.
- A published pack never changes. To correct it, start the pack of the same month again: that is the next version, which supersedes the first; the first stays.

(src: docs/user-guide/board-pack.md#publish-it)

## Keep the PDF as evidence

- **Keep the PDF as evidence** asks another person to approve keeping the published pack's PDF with the organisation's evidence. An Owner or Controller asks and another Owner or Controller who did not ask approves it; in Founder mode that is the other founder, since the person who asks never signs their own request.
- The request carries the file's SHA-256, so the approver signs for exactly that file, the same file as **Download the PDF**. Until it is approved nothing is kept; a refused or lapsed request is asked again.
- Approved, the file is committed to the locked evidence store and is append-only: never changed or deleted, and found under **Board packs** in Compliance › Evidence. A pack keeps one PDF; a correction is the next version of the pack, with its own.

(src: docs/user-guide/board-pack.md#keep-the-pdf-as-evidence)

## Who reads it

- A Board viewer reads the published packs on the Board and investor portal after signing in, and never sees drafts. No link opens a pack without signing in.

(src: docs/user-guide/board-pack.md#who-reads-it)
