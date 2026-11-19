---
key: settings.ai-knowledge-base
title: Manage the knowledge base the assistant reads
mode: expert
roles: [owner, administrator, auditor]
requirements: [AI-50, AI-52, AI-54, AI-55, AI-56, AI-57]
---

# Manage the knowledge base the assistant reads

## What it is

**Settings → AI → Knowledge base** (Expert mode) shows the pages the assistant reads when it answers a how-to
question: Fyniq's own pages, which come with each release, and your organisation's, which say how your company does
things. People read the user guide; these pages are for the assistant, and the assistant never quotes them back as a
source. It cites the user guide, a setting or a record instead.

The assistant reads a page only once it is **published**. A draft, whoever wrote it, changes nothing it says.

## The index

- Each page shows its key, title, who wrote it (Fyniq's compiler, a person, or settings), its data class and where it
  stands: **Published**, **Newer draft** (a later version waits for a person), **Not published** or **From the release**
  for Fyniq's own pages.
- Filter by **All**, **Platform** or **Organisation**, and search by key, title or summary.
- Select an organisation page's key to open it. Fyniq's own pages are read-only here.
- Each person sees in answers only the pages their roles and data rights allow. A page made from payroll or equity
  notes is shown only to people who may read that data, and identity numbers are never in a page.

## Write a note

- Notes, policies and decisions are what your organisation's pages are made from. **Write a note** asks for a key
  (lower-case words, dots and hyphens, such as `expense-policy`, which never changes), what it is, a title, what it says
  and the strictest data it holds.
- Write in words the assistant may explain to colleagues. A note that holds what looks like an identity number, a bank
  account number or a secret is refused.
- Saving a note changes nothing the assistant says. The knowledge-base compiler turns a changed note into a draft page,
  and a person publishes it.

## Pages rendered from settings

- Your team mode, who approves what, and each group of settings have a page each, made from the settings themselves
  with no AI. They are rewritten, and published, when a setting changes, so they never say more than the settings do.
- A rendered page you have edited is never overwritten: the next render waits beside your version, and a finding asks a
  person to choose.

## Open a page

- **Versions** lists every version, newest first, with who made it and when, and which is published. **Roll back**
  makes a new version with an earlier version's text; nothing is deleted.
- **Compare versions** shows what changed between the newest version and an earlier one, line by line.
- **Edit** saves your text as the next version, a draft. A recompile never overwrites a version a person wrote: the
  difference becomes a finding for a person.
- An organisation page can replace one of Fyniq's pages for your organisation alone. When Fyniq later changes the page it
  replaces, the index says **Fyniq changed this page**, and the assistant says your page may be out of date until a person
  reviews it.
- **Sources** lists the notes and settings the page cites. Every section ends with the source it states, and every
  number in it must be in that source.

## Publish

- **Publish** lists the drafts. Choose the ones to publish and, if you like, say why; the reason is kept with the
  snapshot and shown in the weekly review.
- Each draft is checked first. A section with no source, a number that is in no source it cites, a link to a page that
  is not there, a secret, or an open finding stops it, and the message says which.
- In Founder mode an Owner publishes alone, and the publication waits in the other founder's weekly review. In Team
  mode another Owner signs a request first, so a person never publishes their own change alone. An Administrator can
  write and edit but cannot publish.
- A publication is a numbered snapshot. Snapshots are never changed; to undo one, publish an earlier version again.

## Recompile

- **Recompile** (top right, Owner and Administrator) queues the knowledge-base compiler. It drafts a page from each
  note that changed since its page was made, checks every draft with the same lint a publication is checked by, and
  then checks the whole knowledge base. You see the result as new drafts and findings; nothing is published.
- While the AI is switched off the compiler drafts no pages, but the check still runs, and the message says so.
- The compiler works under its own identity and can only make drafts and findings. It cannot publish, read your books or
  overwrite a page a person wrote: a recompile that disagrees with your edit waits beside it.
- Once a week the same check runs by itself for every organisation, with no AI. If the compiler is not connected in
  your installation, **Recompile** says so and nothing is queued.

## Lint findings

- A finding is something a person should look at: a recompile waiting beside a person's edit, a contradiction with a
  setting, or a source that changed. An open finding of those kinds stops that page's publication.
- The weekly check adds advice: a page compiled from an older version of its note (**stale**), a note with no page, an
  overlay of a page Fyniq has since changed (**superseded**), a link to no page, and a page nothing links to
  (**orphan**). Advice never stops a publication: the new version is the answer, and you close the finding when it is.
- **Resolve** closes a finding with a sentence on what was done. It closes once, and who closed it is kept.

## Who can do what

- Owners write notes, edit, roll back, resolve findings and publish (in Team mode, with another Owner's signature).
- Administrators write notes, edit and roll back, but do not publish.
- Auditors can view everything here and change nothing.
- Every change is recorded in the audit trail. The assistant, and connected apps, can read published pages through the
  knowledge-base tools only; they cannot write, publish or approve anything.
