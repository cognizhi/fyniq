# Design

## Context

There are no packs yet. P0's money and rounding package implements the named rounding policies
(`RND-*`), and its tests pin them. The PRD keeps `[verify]` tags on values that still need a primary
source (verification sweep of 14 Oct 2026). The oracle session owns the Python oracle, and the GST
computations get the oracle's half in `p1-gst`, not here.

## Goals / Non-Goals

**Goals:**
- A statutory value used anywhere has exactly one source: an active pack version, chosen by date.

**Non-Goals:**
- GST returns, F5 and InvoiceNow (`p1-gst`, P2).
- CPF computation (Nemura's payroll).
- CIT computation (P2).

## Decisions

- **Packs are files in git, activated in the database.**
  - Each version is a directory, `packs/sg-gst@2026.1/`, holding:
    - `pack.toml` with the metadata;
    - data tables as CSV;
    - `pinned/`, the statutory test cases.
  - Loading a version copies it into global read-only tables; the migrator role writes them.
  - Activation is an organisation's tenant row, so an organisation chooses when to adopt a version,
    under approval.
  - Alternative considered: packs edited in the UI. Rejected because the source URL and the pinned
    tests belong in review (CTL-CHG-01), and the UI only views and activates.
- **Selection is a pure function.** `pack.At(name, date)` returns the version in force. The tax point
  is computed first, by the rule named in the `sg-gst` pack version; the version is then chosen at the
  tax point. Both are pure functions with table tests, and they never call the clock.
- **Activation goes through the approval engine.** It is a high-risk action (SOD-10, FM-1). Until
  `p1-approval-engine` lands, activation is refused ~~unless the organisation is the local sample
  company, which is seeded active~~. (20 Oct 2026, PRD rev 0.37, provisional: the sample company is
  refused too. Nothing marks an organisation as the sample company, and seeding a two-person approval
  would fake SOD-10; no role may insert an activation until the engine's executor gets the grant.)
- **Layout (20 Oct 2026, rev 0.37):** `packs/<pack>/<year>.<n>/` (the skill's nesting) with
  `id = "<pack>@<year>.<n>"` inside, rather than a directory named `sg-gst@2026.1`. Rows of every table
  name their source ids; pinned cases are worked from the sources, not from the tables.
- **`[verify]` blocks activation.** The loader fails a pack version whose metadata marks any value
  unverified, so an unconfirmed rate cannot reach a posting.
- **Rounding stays in the money package.** RND-GST-EXCL and RND-GST-INCL are named policies there;
  the pack chooses only per line or per subtotal (an organisation setting, FR-ORG-04).
- **Ports and adapters (ADR-T8):** storage only.
- **Audit events (AUD-01):** pack loaded, activation proposed, activation approved or refused, and
  version superseded.

## Risks / Trade-offs

- [The official pages change their wording or URLs] → Each pack's source URL is checked by a
  scheduled link check later (wave 5 compliance). Pinned tests keep the values honest meanwhile.
- [The tax agent rules differently at GST registration (question 29)] → The tax-point and
  reverse-charge rules are named and versioned, so a ruling is a new pack version, not a code change.
