# Tasks

## 1. Money

- [x] 1.1 Implement `Money` on apd v3 with a currency and no float constructor, and a lint rule forbidding floats in money packages; tests `// req: ENG-10` and `// req: ENG-11`
- [x] 1.2 Refuse arithmetic across currencies without an explicit conversion, and compute base amounts once and store them; tests `// req: ENG-13` and `// req: ENG-14`
- [x] 1.3 Serialise money as decimal strings with a currency and bound input precision at the edge; tests `// req: ENG-12` (JSON round trip) and `// req: ENG-44` (over-precise input rejected with its code)
- [x] 1.4 Add the named rounding policies the manual journal needs (§10.4) as functions called by name; property test (rapid) `// req: QA-ENG-03` checks the totals invariants of each policy

## 2. Engine

- [x] 2.1 Write `ledger.Post` as a pure function with no clock, database or network; test `// req: ENG-01` runs it twice on the same input and compares the output byte for byte
- [x] 2.2 Persist the lines in the document's transaction through the unit of work; test `// req: ENG-02` forces a failure after posting and asserts that neither the document nor the lines are saved
- [x] 2.3 Record provenance (document, version, rule, ledger revision) on every line; test `// req: ENG-03`
- [x] 2.4 Make the ledger the single source of every balance; test `// req: ENG-04` asserts balances come only from lines and the materialised cache
- [x] 2.5 Add domain validation with stable codes, field names and fixes; test `// req: ENG-22` covers each code

## 3. First posting source and mutation tests

- [ ] 3.1 Add the manual journal as the first posting source (draft, post, reverse); golden tests from the sample company pass, and the oracle agrees (p0-python-oracle-differential)
- [ ] 3.2 Run gremlins on money, rounding and the engine in `make ci` and record reviewed survivors; verify `// req: QA-ENG-04` in the CI log, then `make check` and `make test`
