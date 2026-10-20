---
key: settings.statutory-packs
type: task
title: See and activate a statutory pack version
summary: What a statutory pack is, how to see a version's sources and pinned tests, and how activating one waits for a second person
sources:
  - docs/user-guide/statutory-packs.md sha256:58e4a42b33c06c972076ac920d1321b902a71b94233a7540ddfa291a4d32079d
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-20
aliases:
  - statutory parameters
  - GST rate pack
  - pack update
  - activate a pack
---

# See and activate a statutory pack version

## What a pack is

- Statutory values, such as GST rates and tax codes, are kept in packs; each version takes its values from official pages, names its sources and who verified it, and has pinned tests worked by hand from those pages.
- A new version is used in the books only after the company activates it.

(src: docs/user-guide/statutory-packs.md#what-it-is)

## See the packs

- **Settings → Statutory parameter packs** (Expert mode); choose a version to see when it is in force, its sources and each pinned test's result.
- A version whose pinned tests do not all pass cannot be activated.

(src: docs/user-guide/statutory-packs.md#see-the-packs)

## Activate a version

- Open the version, select **Activate** and confirm it's you when asked; Fyniq shows **Waiting for approval** and the version is not active yet.
- A Controller, an Accountant or a Tax agent asks; an Owner or a Controller who did not ask approves it, like any other change ([[approvals.decide]]).
- Fyniq runs the pinned tests again before it records the activation, with who asked, who approved and the request.

(src: docs/user-guide/statutory-packs.md#activate-a-version)

## Common problems

- **A pinned statutory test fails**: the version cannot be activated; the platform team publishes a corrected version.
- **That pack version is activated already**: nothing to do.
- **This release cannot read that pack version**: activate it after updating Fyniq to a release that carries it.

(src: docs/user-guide/statutory-packs.md#common-problems)
