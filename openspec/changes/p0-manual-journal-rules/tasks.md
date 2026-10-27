# Tasks

## 1. Scenario format (the oracle session)

- [x] 1.1 In `test/differential/scenario.schema.json`, add `role` to `accounts[]` (optional, enum `share-capital`) and `dependentRequired: {"fx_rate": ["fx_rate_source"]}` to `journalLine`, and update the README's Scenario section. The oracle reads `role` (and ignores it until 3.2) and exits 2 on a rate without a source. Verify with a new invalid-scenario test `# req: QA-ENG-02` and `make oracle-test`; the Go round trip still passes in `make test`

## 2. Engine (the engine session; revise with `/opsx:update` before applying)

- [x] 2.1 ENG-24: a manual journal that breaks any of rules 1–8 is rejected whole, posts no line and reports every failure at once with its field and code (FR-INT-07). Tests `// req: ENG-24`: one per rule; a journal breaking three rules reports three failures; and the USD 0.01 + 0.01 − 0.02 journal at 1.5 is rejected as unbalanced in base, with no line added (as built: `ledger.PostJournal` checks rules 1–8 and INV-13 and reports every failure, with the balance rules whenever every line has a base amount; new codes LEDGER-13 rate on a base-currency line, LEDGER-14 base amount out of range, LEDGER-15 share capital, LEDGER-16 reversal date. Rule 1's "a currency the organisation uses" is every currency Fyniq knows until the multi-currency switch, FR-ORG-06, P1)
- [x] 2.2 ENG-03: a base-currency line stores rate 1 with source "base"; a rate entered on it is refused; a rate typed on the document gets the source "document". Tests `// req: ENG-03` (as built: the API sets the source of a rate the client sends to "document" and refuses any other source; the engine refuses a rate on a base-currency line at posting)
- [x] 2.3 ENG-44 and INV-05:
  - decimal places are counted as written: `"12345.0"` is refused for JPY and `"100"` is accepted for USD, and the per-currency money-string pattern (FR-INT-13) says so;
  - a computed base amount outside its column is refused with a field error on the line, never truncated;
  - a non-zero line whose base amount rounds to 0.00 posts (IDR 50 at SGD 0.000085).

  Tests `// req: ENG-44` and `// req: INV-05` (as built: the contract's Money schema carries one `if`/`then` per group of minor units; `TestMoneyStringsFollowTheirCurrency` validates it with a JSON Schema validator)
- [x] 2.4 Reversals from P0:
  - a reversal negates the stored lines and never re-converts them (tests `// req: ENG-14`);
  - it is dated on or after its entry in an open period, and each entry is reversed at most once; an earlier date or a second reversal is refused (tests `// req: FR-GL-12`).

  This builds the rev 0.24 note without claiming the P1 row (as built: `ledger.Reverse` refuses a date before the entry, LEDGER-16; at most once is the document status, JOURNAL-03, and the reversal's posting identity in the database)
- [x] 2.5 INV-13 from P0:
  - mark share-capital accounts in the chart of accounts with a migration, marking 3010 and 3020 in the default chart and in `cmd/seed`'s sample company;
  - carry the mark through the storage port;
  - refuse a manual journal line to such an account in the domain and in the posting-source trigger.

  A database attack test `// req: INV-13` inserts such a line directly and expects the trigger to refuse it. The engine session also checks what the nightly verification (#32) already covers of INV-13 (as built: migration 000014 adds `account.role` (`share-capital`), fixed at creation (`account_guard`), and the check in `journal_line_check`, FY007, which refuses only the `manual_journal` source; 000015 validates the constraint. The default chart's 3010 and 3020 are marked in `internal/seed`, whose journals no longer post to them, so the seed's golden scenarios and trial balance were regenerated. The nightly verification (#32) checks INV-08 only: INV-13's sub-ledger reconciliation arrives with the register, P1, and the trigger enforces the P0 rule)
- [x] 2.6 `cmd/scenario-run` reads `accounts[].role` and maps it to the chart-of-accounts mark. Verify with scenario-run's own golden test (not a case under `test/differential/golden/`) and the Go round trip (as built: `cmd/scenario-run/testdata/decided-rules.json` and its result)
- [x] 2.7 Add no golden case and no generator change for a decided shape in this group: until group 3, the oracle still posts earlier-dated reversals and share-capital lines. Verify that `make test` (the gate, unchanged) and `make check` pass (as built: the gate runs 63 scenarios with 0 disagreements; no decided shape is in `test/differential/golden/` or the generator)

## 3. Oracle (the oracle session, after group 2 merges)

- [x] 3.1 Refuse a reversal dated before its document. Hand-worked tests `# req: FR-GL-12`: the day before (refused), the same day and a later month (both post) (as built: `reversal-before-entry`; a refused reversal leaves the document unreversed, so a later one still posts)
- [x] 3.2 Refuse a manual journal line to an account whose role is `share-capital`, with tests `# req: INV-13`. The rule list in `oracle/src/oracle/ledger.py` cites ENG-24 and INV-13 (as built: rule 4's `share-capital-account`, either side of the line; an equity account without the role still posts)
- [x] 3.3 In `test/differential/README.md`, rename "Choices where PRD-001 is silent" to "Decided in PRD-001 rev 0.24", with each row citing its requirement; update the strategies' docstring to match. Verify with `make oracle-test` (as built: the table gains the share-capital row; `make oracle-test` 120 passed, `make differential` and the Go gate green)
- [ ] 3.4 Add generator kinds for every shape that was open, each with its intent:
  - a non-zero line whose base amount rounds to 0.00 (posts);
  - a rate on a base-currency line (rejected);
  - a computed base amount beyond 16 integer digits (rejected);
  - a reversal dated before its document (rejected);
  - a line to a share-capital account (rejected).

  The properties check that built-to-post documents post and built-to-fail ones are rejected. Verify with `ORACLE_HYPOTHESIS_PROFILE=thorough` on the property tests
- [ ] 3.5 Add `test/differential/golden/decided-rules.json` with one document or reversal per decided shape, worked by hand, and list it in `golden/README.md`. Hand-mutate each new rule and confirm the suite kills every mutant; record them in the pull request. The mutants:
  - allow an earlier-dated reversal;
  - allow a share-capital line;
  - re-convert a reversal;
  - accept `"12345.0"` for JPY;
  - truncate an out-of-range base amount
- [ ] 3.6 The pull request says who wrote it and that its author did not read the engine (QA-ENG-01). `make test` (whose gate now runs the new shapes against the engine), `make differential` and `make check` pass

## Workflow follow-up

- `/opsx:verify`, then `/opsx:archive --skip-specs`.
- Update the progress note in `docs/prd/plans/p0-foundation-2026-10-14.md`.
