# Tasks

## 1. Pack format and loader

- [x] 1.1 Define the pack layout (`pack.toml`, CSV tables, `pinned/`) and write the loader and validator. A version without a source URL, verifier or review date, or with a value still marked `[verify]`, is refused. Tests `// req: ENG-30`, `// req: FR-ORG-05` and `// req: platform/release-and-versioning Statutory parameter pack versions` (20 Oct 2026: `packs/<pack>/<year>.<n>/`, embedded by the `packs` package; `internal/domain/pack` Load and LoadAll refuse a version with every problem, codes PACK-01…12, 24 refusal cases; the layout is provisional, PRD rev 0.37)
- [x] 1.2 Add global read-only pack tables, written by the migrator role, and the per-organisation activation row. Attack test: the API role cannot write pack contents (20 Oct 2026: migration 000033, `statutory_pack_version` and `statutory_pack_row`, read through views, unchangeable once loaded (FY110); migration 000034, `statutory_pack_activation`, a tenant row, audited, two people; `migrate up` loads the shipped packs; attack tests for every service role)

## 2. Selection, tax point and rounding

- [x] 2.1 Write `pack.At(name, date)` and the tax-point rule as pure functions. Test `// req: ENG-31`: a future rate never changes a past document (20 Oct 2026: `pack.At`, `RateOn`, `GSTRate`, `TaxPoint` and `PaymentTaxPoint` under the rule `sg-gst/earlier-of@1`)
- [ ] 2.2 Implement the tax point and GST dates under the named `sg-gst` rule. Test `// req: ENG-45` with the pinned cases G4-018…021 (20 Oct 2026: the tax points of G4-018 and G4-019 are pinned. **Left:** the amount taxed at a deposit (G4-018 keeps its [verify]), G4-020 (registration mid-service), G4-021 (the claim-date policy) and the GST dates stored on documents, with `p1-gst`)
- [ ] 2.3 Apply RND-GST-EXCL per subtotal or per line, as the organisation's setting says, and RND-GST-INCL extraction. Tests `// req: RND-GST-EXCL` and `// req: RND-GST-INCL`, with cases worked by hand (**Left:** the rounding package sits under the oracle's differential and mutation gates, so this goes with the oracle's half in `p1-gst`)

## 3. Activation

- [x] 3.1 Activation needs the pinned tests to pass for the effective date, and a second approver through `p1-approval-engine` (SOD-10). Test `// req: ENG-32`; until the engine lands, activation is refused outside the sample company (20 Oct 2026: `pack.CheckActivation` refuses an activation without the engine's approval record, approved by its proposer, or with a failing pinned case; no role may insert an activation yet, and the database refuses one approved by its proposer. Provisional, rev 0.37: the sample company is refused too. **Left:** the activation command, its audit events and the executor's grant, with `p1-approval-engine`)

## 4. The first packs

- [ ] 4.1 Write `sg-gst`, `sg-deadlines`, `sg-cpf`, `sg-sdl` and `sg-shg` from the official pages, each value with its source URL and a pinned test (`fyniq-sg-statutory` skill). Any value without a primary source keeps `[verify]`, and its pack stays inactive (20 Oct 2026: `sg-gst@2026.1` holds the rate history and the twelve B.6 codes with their F5 boxes, each from IRAS, and 22 pinned cases; BL corrected to "not reported", rev 0.37. **Left:** the registration threshold and the reverse-charge codes in `sg-gst`; `sg-deadlines`, `sg-cpf`, `sg-sdl` and `sg-shg`)

## 5. Home rows

- [ ] 5.1 (**Left**) Map each §14.2 row of `compliance/singapore-statutory` to the change that closes it, recorded in this change's `design.md`. Close the rows no module owns with tests (`// req: compliance/singapore-statutory <title>`): retention defaults and MAS guidelines as reference only. The electronic-signature conditions close with `p1-approval-engine`, which tags their test

## 6. Screen

- [ ] 6.1 (**Left**, with the read endpoints for packs) Build the statutory-packs screen as designed: list, version detail with its source and pinned results, and activation. Add help keys, a page in `docs/user-guide/` and a compiled `kb/` page; the page shows the FR-UX-41 banner while the GST review is pending
- [ ] 6.2 Finish with `make check` and `make test`
