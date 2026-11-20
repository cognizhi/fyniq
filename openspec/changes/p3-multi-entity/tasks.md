# Tasks

Each slice carries its migration (001350 to 001449), port and adapter, attack tests, API with matrix rows, screen with help keys and guide pages
compiled into the knowledge base, and the oracle's half where it calculates. Load `fyniq-feature-change` and `fyniq-design-system` before each slice.

## 1. Decisions and specs
- [ ] 1.1 PRD-001 records the decisions (FR-ENT-04 to 07, RND-TRANSLATE, two Appendix D rows); the specs are updated and stamped. `// req: FR-ENT-01`

## 2. The consolidation calculation
- [x] 2.1 `RND-TRANSLATE` and `internal/domain/consolidation`: translation, the reserve, eliminations, differences, with hand-worked cases. `// req: FR-ENT-02`, `// req: RND-TRANSLATE`
  Built (rev 0.626): `rounding.Translate`, `internal/domain/consolidation`, `TestATranslatedLineRoundsOnceHalfAwayFromZero`, `TestAConsolidationThatCannotBeWorkedIsRefusedInPlainWords`. **Left:** the package is not in the mutation list (its tests are the differential's).
- [x] 2.2 The oracle's half (`oracle/src/oracle/consolidation.py`), its schema and hand cases, differential scenarios and `make differential`. `// req: QA-ENG-02`
  Built: `oracle/src/oracle/consolidation.py`, `consolidation.schema.json`, `consolidations/hand.json` (11 cases worked by hand) and `generated.json` (240), `TestTheConsolidationCasesConformAndTheEngineReproducesEach`, `oracle/tests/test_consolidation.py`.

## 3. Groups and the link
- [ ] 3.1 Migration: invitation registry, `group_entity`, `group_membership`, acceptance and the two read functions, with grants and attack tests. `// req: FR-ENT-04`, `// req: SEC-02`
- [ ] 3.2 Store, API (invite, accept, list, end), matrix rows, audit in both trails. `// req: FR-ENT-04`
- [ ] 3.3 Screen `entity-setup`, help, guide and kb. `// req: QA-UI-05`

## 4. Consolidation
- [ ] 4.1 Mapping and rates (store, API). `// req: FR-ENT-05`
- [ ] 4.2 The run: read, translate, eliminate, freeze; API and matrix rows. `// req: FR-ENT-06`, `// req: FR-RPT-12`
- [ ] 4.3 Screen `consolidation`, help, guide and kb. `// req: QA-UI-05`

## 5. Intercompany
- [ ] 5.1 Records, offers and the mirror draft. `// req: FR-GL-13`
- [ ] 5.2 Screen additions, help, guide and kb. `// req: QA-UI-05`

## 6. Accountant-firm mode
- [ ] 6.1 Firm mode, client invitation, engagement, per-client roles. `// req: FR-ENT-03`
- [ ] 6.2 The practice dashboard (API and screen), help, guide and kb. `// req: FR-ENT-07`

## 7. The switcher
- [ ] 7.1 Group label and "All entities". `// req: FR-UX-18`
