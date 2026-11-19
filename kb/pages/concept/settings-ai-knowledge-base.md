---
key: settings.ai-knowledge-base
type: concept
title: The knowledge base the assistant reads: pages, notes, publishing
summary: The pages the assistant reads, the notes they come from, versions, findings and publishing; nothing changes until a person publishes
sources:
  - docs/user-guide/knowledge-base.md sha256:4d4aa1d7644f0cc6d417142e6cc4785d84da76e6b4fdcf1efc785b6a24848263
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-19
aliases:
  - knowledge base
  - knowledge base admin
  - what the assistant reads
  - assistant pages
  - publish a page
  - publish the knowledge base
  - write a note
  - notes and policies
  - lint findings
  - page versions
  - roll back a page
  - why the assistant says something old
  - teach the assistant
  - add a policy for the assistant
---

# The knowledge base the assistant reads: pages, notes, publishing

## What it is

- **Settings → AI → Knowledge base** (Expert mode) shows the pages the assistant reads when it answers a how-to question: Fyniq's own pages, which come with each release, and the organisation's own pages, which say how the company does things.
- The assistant cites the user guide, a setting or a record, never these pages, and it reads a page only once it is published: a draft changes nothing it says ([[ai.assistant]]).

(src: docs/user-guide/knowledge-base.md#what-it-is)

## The index

- Each page shows its key, title, who wrote it, its data class and where it stands: published, a newer draft, not published, or from the release for Fyniq's own pages.
- Filter by all, platform or organisation, and search by key, title or summary. Fyniq's own pages are read-only here.
- Each person sees in answers only the pages their roles and data rights allow; identity numbers are never in a page.

(src: docs/user-guide/knowledge-base.md#the-index)

## Write a note

- Notes, policies and decisions are what the organisation's pages are made from: a key that never changes, what it is, a title, what it says and the strictest data it holds.
- A note that holds what looks like an identity number, a bank account number or a secret is refused.
- Saving a note changes nothing the assistant says: the compiler turns a changed note into a draft page, and a person publishes it.

(src: docs/user-guide/knowledge-base.md#write-a-note)

## Pages rendered from settings

- The team mode, who approves what, and each group of settings have a page each, made from the settings with no AI and rewritten when a setting changes ([[settings.team-mode]], [[approvals.policies]]).
- A rendered page a person has edited is never overwritten: the next render waits beside it, and a finding asks a person to choose.

(src: docs/user-guide/knowledge-base.md#pages-rendered-from-settings)

## Open a page

- Versions lists every version, newest first, with who made it and which is published. Roll back makes a new version with an earlier version's text; nothing is deleted.
- Compare versions shows line by line what changed. Edit saves the text as the next version, a draft; a recompile never overwrites a version a person wrote.
- A page that replaces one of Fyniq's pages is flagged "Fyniq changed this page" when Fyniq's page changes later, and the assistant says it may be out of date until a person reviews it.
- Sources lists the notes and settings a page cites; every number in a section must be in the source it cites.

(src: docs/user-guide/knowledge-base.md#open-a-page)

## Publish

- Choose the drafts to publish and, if wanted, say why; the reason is kept with the snapshot and shown in the weekly review.
- Each draft is checked first: a section with no source, a number in no cited source, a link to no page, a secret or an open finding stops it, and the message says which.
- In Founder mode an Owner publishes alone and it waits in the weekly review; in Team mode another Owner signs a request first. An Administrator edits but cannot publish ([[settings.team-mode]], [[approvals.decide]]).
- A publication is a numbered snapshot that is never changed; to undo one, publish an earlier version again.

(src: docs/user-guide/knowledge-base.md#publish)

## Lint findings

- A finding is a recompile waiting beside a person's edit, a contradiction with a setting, or a source that changed. An open finding on a page stops its publication.
- Resolve closes a finding with a sentence on what was done; it closes once and who closed it is kept.

(src: docs/user-guide/knowledge-base.md#lint-findings)

## Who can do what

- Owners write notes, edit, roll back, resolve findings and publish. Administrators write notes, edit and roll back. Auditors only view ([[members.roles]]).
- Every change is audited. The assistant and connected apps read published pages through the knowledge-base tools only and cannot write, publish or approve.

(src: docs/user-guide/knowledge-base.md#who-can-do-what)
