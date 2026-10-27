# Tasks

## 1. Pack format and loader

- [x] 1.1 Define the pack layout (`pack.toml`, CSV tables, `pinned/`) and write the loader and validator. A version without a source URL, verifier or review date, or with a value still marked `[verify]`, is refused. Tests `// req: ENG-30`, `// req: FR-ORG-05` and `// req: platform/release-and-versioning Statutory parameter pack versions` (20 Oct 2026: `packs/<pack>/<year>.<n>/`, embedded by the `packs` package; `internal/domain/pack` Load and LoadAll refuse a version with every problem, codes PACK-01…12, 24 refusal cases; the layout is provisional, PRD rev 0.37)
- [x] 1.2 Add global read-only pack tables, written by the migrator role, and the per-organisation activation row. Attack test: the API role cannot write pack contents (20 Oct 2026: migration 000033, `statutory_pack_version` and `statutory_pack_row`, read through views, unchangeable once loaded (FY110); migration 000034, `statutory_pack_activation`, a tenant row, audited, two people; `migrate up` loads the shipped packs; attack tests for every service role)

## 2. Selection, tax point and rounding

- [x] 2.1 Write `pack.At(name, date)` and the tax-point rule as pure functions. Test `// req: ENG-31`: a future rate never changes a past document (20 Oct 2026: `pack.At`, `RateOn`, `GSTRate`, `TaxPoint` and `PaymentTaxPoint` under the rule `sg-gst/earlier-of@1`)
- [ ] 2.2 Implement the tax point and GST dates under the named `sg-gst` rule. Test `// req: ENG-45` with the pinned cases G4-018…021 (20 Oct 2026: the tax points of G4-018 and G4-019 are pinned. **Left:** the amount taxed at a deposit (G4-018 keeps its [verify]), G4-020 (registration mid-service: `gst.StatusOn` at the tax point; not done here), and, with `p1-gst`, G4-021 (the claim-date policy and the FR-TAX-12 queue) and the GST dates stored on documents)
- [ ] 2.3 Apply RND-GST-EXCL per subtotal or per line, as the organisation's setting says, and RND-GST-INCL extraction. Tests `// req: RND-GST-EXCL` and `// req: RND-GST-INCL`, with cases worked by hand (**Left:** the rounding package sits under the oracle's differential and mutation gates, so this goes with the oracle's half in `p1-gst`)
  (27 Oct 2026, the oracle session: the oracle's half is written from §10.4, `money.rnd_gst_excl` per GST-code subtotal or per line and `money.rnd_gst_incl` at the base currency's minor units, with hand-worked cases and properties `# req: RND-GST-EXCL`, `# req: RND-GST-INCL` in `oracle/tests/test_gst_rounding.py`. **Left:** the differential reaches them through the bank-line and invoice shapes of `fyniq-scenario@1` once `cmd/scenario-run` reads those (an engine-session task in `p1-bank-import-reconcile` and `p1-sales`))

## 3. Activation

- [x] 3.1 Activation needs the pinned tests to pass for the effective date, and a second approver through `p1-approval-engine` (SOD-10). Test `// req: ENG-32`; until the engine lands, activation is refused outside the sample company (20 Oct 2026: `pack.CheckActivation` refuses an activation without the engine's approval record, approved by its proposer, or with a failing pinned case; no role may insert an activation yet, and the database refuses one approved by its proposer. Provisional, rev 0.37: the sample company is refused too. Rev 0.41: `POST /v1/statutory-packs/{version_id}/activate` asks for activation through the approval engine (202): C on "Statutory parameters" asks, A approves, never the proposer; the pinned tests run when asked and again when it executes (`pack.CheckActivation` with the engine's record), and only a version this release carries with its loaded digest is activated (PACK-07); migration 000041 gives the API the INSERT grant and a guard (FY107) that records an activation only as its approved request, signed by its approver, and never changes one. Tests: `TestPackActivationWaitsForASecondApprover`, `TestAttackPackActivationIsTheApprovedRequest`, `TestAttackActivationNeedsTwoPeopleAndAnAuditEvent`)

## 4. The first packs

- [ ] 4.1 Write `sg-gst`, `sg-deadlines`, `sg-cpf`, `sg-sdl` and `sg-shg` from the official pages, each value with its source URL and a pinned test (`fyniq-sg-statutory` skill). Any value without a primary source keeps `[verify]`, and its pack stays inactive (20 Oct 2026: `sg-gst@2026.1` holds the rate history and the twelve B.6 codes with their F5 boxes, each from IRAS, and 22 pinned cases; BL corrected to "not reported", rev 0.37. **Left:** the registration threshold and the reverse-charge codes in `sg-gst`; `sg-deadlines`, `sg-cpf`, `sg-sdl` and `sg-shg`)

## 5. Home rows

- [ ] 5.1 (**Left**) Map each §14.2 row of `compliance/singapore-statutory` to the change that closes it, recorded in this change's `design.md`. Close the rows no module owns with tests (`// req: compliance/singapore-statutory <title>`): retention defaults and MAS guidelines as reference only. The electronic-signature conditions close with `p1-approval-engine`, which tags their test

## 6. Screen

- [x] 6.1 Build the statutory-packs screen as designed: list, version detail with its source and pinned results, and activation. Add help keys, a page in `docs/user-guide/` and a compiled `kb/` page; the page shows the FR-UX-41 banner while the GST review is pending
  (20 Oct 2026, rev 0.41: the read endpoints, `GET /v1/statutory-packs` and `GET /v1/statutory-packs/{version_id}` with
  its sources and pinned results, and the guide page `statutory-packs.md` compiled into `settings.statutory-packs`.
  **Left:** the screen and its help keys, with the FR-UX-41 banner.)
  Later the same day: the screen, Settings › Statutory parameter packs (`apps/web/src/screens/settings/StatutoryPacks.tsx`,
  Expert mode): each version with its pinned results; its detail with the official sources, each pinned case and the
  digest; Activate (202, over the version's ETag) while its tests pass and it is not active or waiting; and the FR-UX-41
  banner from the review register (`components/ReviewBanner.tsx`). Tests `Organisation.test.tsx`,
  `ReviewBanner.test.tsx`.)
- [ ] 6.2 Finish with `make check` and `make test`
