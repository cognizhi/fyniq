# Tasks

## 1. Pack format and loader

- [ ] 1.1 Define the pack layout (`pack.toml`, CSV tables, `pinned/`) and write the loader and validator. A version without a source URL, verifier or review date, or with a value still marked `[verify]`, is refused. Tests `// req: ENG-30`, `// req: FR-ORG-05` and `// req: platform/release-and-versioning Statutory parameter pack versions`
- [ ] 1.2 Add global read-only pack tables, written by the migrator role, and the per-organisation activation row. Attack test: the API role cannot write pack contents

## 2. Selection, tax point and rounding

- [ ] 2.1 Write `pack.At(name, date)` and the tax-point rule as pure functions. Test `// req: ENG-31`: a future rate never changes a past document
- [ ] 2.2 Implement the tax point and GST dates under the named `sg-gst` rule. Test `// req: ENG-45` with the pinned cases G4-018…021
- [ ] 2.3 Apply RND-GST-EXCL per subtotal or per line, as the organisation's setting says, and RND-GST-INCL extraction. Tests `// req: RND-GST-EXCL` and `// req: RND-GST-INCL`, with cases worked by hand

## 3. Activation

- [ ] 3.1 Activation needs the pinned tests to pass for the effective date, and a second approver through `p1-approval-engine` (SOD-10). Test `// req: ENG-32`; until the engine lands, activation is refused outside the sample company

## 4. The first packs

- [ ] 4.1 Write `sg-gst`, `sg-deadlines`, `sg-cpf`, `sg-sdl` and `sg-shg` from the official pages, each value with its source URL and a pinned test (`fyniq-sg-statutory` skill). Any value without a primary source keeps `[verify]`, and its pack stays inactive

## 5. Home rows

- [ ] 5.1 Map each §14.2 row of `compliance/singapore-statutory` to the change that closes it, recorded in this change's `design.md`. Close the rows no module owns with tests (`// req: compliance/singapore-statutory <title>`): retention defaults and MAS guidelines as reference only. The electronic-signature conditions close with `p1-approval-engine`, which tags their test

## 6. Screen

- [ ] 6.1 Build the statutory-packs screen as designed: list, version detail with its source and pinned results, and activation. Add help keys, a page in `docs/user-guide/` and a compiled `kb/` page; the page shows the FR-UX-41 banner while the GST review is pending
- [ ] 6.2 Finish with `make check` and `make test`
