# Tasks

Each slice carries its migration (001350 to 001449), port and adapter, attack tests, API with matrix rows, screen with help keys and guide pages
compiled into the knowledge base, and the oracle's half where it calculates. Load `fyniq-feature-change` and `fyniq-design-system` before each slice.

## 1. Decisions and specs
- [ ] 1.1 PRD-001 records the decisions (FR-ENT-04 to 07, RND-TRANSLATE, two Appendix D rows); the specs are updated and stamped. `// req: FR-ENT-01`

## 2. The consolidation calculation
- [x] 2.1 `RND-TRANSLATE` and `internal/domain/consolidation`: translation, the reserve, eliminations, differences, with hand-worked cases. `// req: FR-ENT-02`, `// req: RND-TRANSLATE`
  Built (rev 0.627): `rounding.Translate`, `internal/domain/consolidation`, `TestATranslatedLineRoundsOnceHalfAwayFromZero`, `TestAConsolidationThatCannotBeWorkedIsRefusedInPlainWords`. **Left:** the package is not in the mutation list (its tests are the differential's).
- [x] 2.2 The oracle's half (`oracle/src/oracle/consolidation.py`), its schema and hand cases, differential scenarios and `make differential`. `// req: QA-ENG-02`
  Built: `oracle/src/oracle/consolidation.py`, `consolidation.schema.json`, `consolidations/hand.json` (11 cases worked by hand) and `generated.json` (240), `TestTheConsolidationCasesConformAndTheEngineReproducesEach`, `oracle/tests/test_consolidation.py`.

## 3. Groups and the link
- [x] 3.1 Migration: invitation registry, `group_entity`, `group_membership`, acceptance and the two read functions, with grants and attack tests. `// req: FR-ENT-04`, `// req: SEC-02`
  Built (rev 0.628): migration 001350, the grants listed for review. Tests: `TestAttackAGroupLinkNeedsBothOwnersAndReadsOnlyWhileItStands`, `TestAttackAGroupHasOneLevelAndAnOrganisationOneGroup`, `TestAttackAGroupLinkIsReachedOnlyThroughItsFunctions`.
- [x] 3.2 Store, API (invite, accept, list, end), matrix rows, audit in both trails. `// req: FR-ENT-04`
  Built: `EntityGroups` (port and adapter), `internal/api/entity_groups.go` (getEntityGroup, inviteGroupEntity, withdrawGroupInvitation, acceptGroupInvitation, endGroupLink), matrix rows `matrixEntityGroups`, `TestAnOwnerInvitesAnEntityItsOwnerAcceptsAndEitherEndsIt`.
- [ ] 3.3 Screen `entity-setup`, help, guide and kb. `// req: QA-UI-05`

## 4. Consolidation
- [x] 4.1 Mapping and rates (store, API). `// req: FR-ENT-05`
  Built (rev 0.629): migration 001351, `Consolidations`, mapGroupAccount, listGroupMappings, addGroupRate, listGroupRates, getGroupPairs, setGroupPairs, listGroupEntityAccounts, matrix rows `matrixConsolidation`. Test: `TestAParentConsolidatesItsEntityAndTheRunStaysFrozen`.
- [x] 4.2 The run: read, translate, eliminate, freeze; API and matrix rows. `// req: FR-ENT-06`, `// req: FR-RPT-12`
  Built (rev 0.629): `groupconsol.Prepare` and `Freeze`, runConsolidation, listConsolidations, getConsolidation. **Left:** a domain unit test of `groupconsol` of its own (the API test covers one currency, no translation, no elimination; the engine's own cases cover those), and an API test of a foreign-currency run with rates.
- [ ] 4.3 Screen `consolidation`, help, guide and kb. `// req: QA-UI-05`

## 5. Intercompany
- [ ] 5.1 Records, offers and the mirror draft. `// req: FR-GL-13`
- [ ] 5.2 Screen additions, help, guide and kb. `// req: QA-UI-05`

## 6. Accountant-firm mode
- [ ] 6.1 Firm mode, client invitation, engagement, per-client roles. `// req: FR-ENT-03`
- [ ] 6.2 The practice dashboard (API and screen), help, guide and kb. `// req: FR-ENT-07`

## 7. The switcher
- [ ] 7.1 Group label and "All entities". `// req: FR-UX-18`

## Where the build stopped (21 Nov 2026, paused for budget)

Built and integrated: the plan and decisions (1.1), the calculation with its oracle half and a full `make differential` (2.1, 2.2), the link with its attack tests (3.1, 3.2), the set-up and the run (4.1, 4.2).

**Left, in this order:**
- 3.3 and 4.3: the screens `entity-setup` (the group page: invite, accept, end, mapping) and `consolidation` (the frozen run with the entity columns, the eliminations, the rates and the differences banner), their help keys, guide pages and knowledge-base pages (kb-schema@2); adapt `docs/design/screens/src/EntitySetup.html` and `Consolidation.html` (they still show the superseded "create with dual approval" and an entity inside an organisation); Basic or Expert tags (Expert); the web client is generated, rebuild the web app after `make openapi`.
- 5.1, 5.2 intercompany (FR-GL-13): `intercompany_transaction` rows in both tenants, the offers function `group_intercompany_offers()` (design D3 point 2: no free text), the mirror as a draft journal, the reconciliation list.
- 6.1, 6.2 accountant-firm mode (FR-ENT-03, FR-ENT-07): firm mode, the client invitation through the same registry (`kind = 'firm'` is already allowed), the engagement row in the client, the practice dashboard through the caller's own rights in each client, the screen `practice-dashboard`.
- 7.1 the switcher group label and *All entities* (FR-UX-18).
- A parent's own-account mapping needs a screen too; until then the API does it.
- Not verified: the full authorisation matrix, `make scan`, `make images`, the browser journeys and the visual snapshots; `make test` of the whole tree; the pre-existing failures of `TestEveryNumericColumnHasPrecisionAndRefusesNaN` (automation_rule) and `TestTheTenantExportCataloguesEveryTable` (sandbox_organisation) on dev/integration come from other changes.
