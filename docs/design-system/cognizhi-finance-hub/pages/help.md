# Page override · Help and the user guide

Overrides MASTER.md for `/help` (PRD §7.9: FR-UX-32, FR-UX-33, FR-UX-35). Info popovers and the
walkthrough follow MASTER §12.

## Purpose
One place to learn the whole product: tasks, concepts, Singapore rules, shortcuts, the glossary
and what's new. Pages are Markdown in `docs/user-guide/`, built into the app and versioned with
each release.

## Layout (≥ 1024 px)
1. Eyebrow "Help · user guide", the page title (h1, `--fs-xl`), and one line saying which mode
   the page is written for.
2. **Left column** (240 px, sticky): search box, table of contents (Getting started · tasks ·
   concepts · Singapore guides · keyboard shortcuts · glossary · What's new), and a "Take the
   tour again" button.
3. **Page** (max 880 px; reading measure ≤ 75 characters): h2 sections, tables inside
   `.table-wrap`, callouts.

Below 1024 px the table of contents stacks above the page, search first.

## Opening Help
- The header **Help** button (Lucide `circle-help` plus the label "Help"; icon only below 768 px,
  with its accessible name kept), ⌘K "Help: …", and the "Learn more" links in info popovers and
  empty states.
- **Context-aware** (FR-UX-33): the Help button opens the page mapped to the current route in the
  feature registry (Home → Getting started, Inbox → Approvals, Banking → Recording money).
- Deep links `/help/<page-id>` are stable; a renamed page redirects.

## Page template
Title · one-sentence purpose · "Before you start" (optional) · numbered steps for a task ·
"Good to know" · a **Recommended default** callout wherever the product ships a default
(`--primary-subtle`, Lucide `circle-check`) · a **Check first** callout for caveats still being
confirmed (`--status-warning-subtle`, `triangle-alert`) · related pages · version and last update.

Screenshots come from the end-to-end tests, in light and dark themes, so they match the release.
Their alt text states the point of the picture, and a screenshot is never the only place a step
is described.

## Search
Client-side over titles, headings and keywords, with synonyms (for example "salary" finds
Founders' money). Results update as the user types without moving focus. With no match: "No
pages match", plus a link to ask the assistant.

## What's new
Built from the release notes (the user-facing entries of the release-please changelog), newest
first, with the version and date; each entry links to its guide page. FR-UX-35 shows the latest
entry once as a dismissible card.

## Basic vs Expert
- The same pages in both modes. Basic hides the accounting-detail blocks (postings, account
  codes, rule IDs); Expert shows them inline in `--font-code` and `--text-muted`.
- The table of contents never hides a page because of the mode; Expert-only features say
  "Available in Expert mode" with a switch action (FR-UX-26).

## Accessibility
- The table of contents is a `nav` with `aria-current="page"` on the open page; the search box
  has a label.
- Headings in order (h1 page title, h2 sections); tables have header cells; callouts use
  `role="note"` with an icon and a text label ("Recommended default:", "Check first:"), never
  colour alone.
- Printable: the header, sidebar and table of contents are hidden in print.

## Density
Comfortable regardless of the user's density setting (Help is a reading page).
