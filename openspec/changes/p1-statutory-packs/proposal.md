# Proposal

## Why

Golden rule 12: statutory values come from official sources, as effective-dated packs with a source
URL and pinned tests. Taxes, rounding and deadlines in every later module read them, so the pack
mechanism and the first packs land in wave 1, before any document computes GST (§10.6). This change is
also the home of the §14.2 Singapore obligations: each row is closed with the module it guards, and
this change closes the rest.

## What Changes

- **Packs as versioned data (ENG-30, FR-ORG-05, the pack-version requirement of §12.10):**
  - a format for `packs/<pack>@<year>.<n>`, with the effective dates, official source URL, verifier,
    approver, conditions of use and review date;
  - packs released independently of the platform (ENG-32/33).
- **Version selection (ENG-31):** the version in force at each document's tax point, or at the pay
  month; a future rate never changes the past.
- **Activation (ENG-32):** dual approval (SOD-10) and the pinned statutory tests for the effective
  date, before a version is active.
- **Tax point and GST dates (ENG-45):** document date, tax point, posting date and payment dates,
  under a named, versioned rule in `sg-gst`. The pinned cases include G4-018…021.
- **GST rounding (RND-GST-EXCL, RND-GST-INCL):** per subtotal by default or per line as an
  organisation setting, half-up to the cent.
- **The first packs, from official sources, with pinned tests:**
  - `sg-gst`: rates, codes, the registration threshold and the tax-point rule;
  - `sg-deadlines`: GST, CIT, ACRA and CPF due dates;
  - `sg-cpf`, `sg-sdl` and `sg-shg` as data only. Nemura's payroll computes from them after go-live.
  - A value without a primary source keeps its `[verify]` tag and blocks activation.
- **The 17 unnumbered §14.2 rows of `compliance/singapore-statutory`:**
  - each is mapped to the change that closes it (`p1-gst`, `p1-sales`, `p1-equity-register`,
    `p1-audit-explorer`, the Nemura track, the P2 tax and filing changes);
  - this change closes the rows no module owns: retention defaults and MAS guidelines as reference
    only. The electronic-signature conditions close with `p1-approval-engine`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). This change builds:
- `tax/statutory-parameters`: ENG-30, 31, 32 and 45;
- `organisation/settings`: FR-ORG-05;
- `ledger/money-and-rounding`: RND-GST-EXCL and RND-GST-INCL;
- the pack-version requirement of `platform/release-and-versioning`;
- the home rows of `compliance/singapore-statutory`.

## Impact

- **Code:** `packs/` (new), a pack loader and validator in Go, migrations for pack activation per
  organisation (a tenant row; pack contents are global and read-only, §11.4), the API, the screen and
  the `fyniq-sg-statutory` skill's commands.
- **Screens:** statutory-packs.
- **Reviewers:** the tax agent and the accountant, on the GST rules. Their review runs in
  the review pull requests, and until it merges the pages show the FR-UX-41 banner (rev 0.22).
- **Upgrade notes:** none. This is new data.
