---
name: fh-accounting-domain
description: The accounting core of Cognizhi Finance Hub Cloud. Covers how source documents (bank transactions, invoices, bills, pay runs, assets, journals, expense claims, founders' balances and conversions to share capital) become journal entries; the ledger invariants INV-01…13 the database enforces; exact-decimal money (ENG-10…14); the named rounding and allocation policies (RND-*, ALLOC-*); effective-dated parameters; the balance service; and the independent Python oracle. Use it for any work on posting rules, GST, FX, settlements, spreads, deferred revenue, prepayments, depreciation, payroll postings, opening balances, period locks, trial balance, balance sheet or cash-flow checks, or when someone says "the books don't balance", "this number is off by a cent", "add a posting source", "the engine and the oracle disagree" or "why did this post to 7030".
---

# Accounting domain: engine, ledger, money

The ledger is the system of record. Every report, KPI, tile, check, export and AI answer reads it
through **one balance service** (ENG-04). If the ledger is right and nobody bypasses the balance
service, the reports cannot disagree.

> **Status:** no code exists yet; P0 builds it. The paths below are the PRD §12.4 layout. Treat
> them as the contract. When the code lands, keep this skill in step with it.

## Mental model

```
document version ──► posting engine (pure) ──► journal_entry + journal_line ──► balance service ──► everything
        ▲                 ▲                          (append-only, same txn)          ▲
  domain command   parameter packs in force                                     nightly recompute (INV-08)
```

| Rule | Requirement |
|---|---|
| The engine is a **pure function**: (document version, parameters in force at the document date, organisation settings) → entries. No clock, randomness or I/O inside rules. | ENG-01 |
| Posting happens in the **same database transaction** as the document change. | ENG-02 |
| Every entry records its source (document type, ID, version) and its **rule version**; every line records account, amount, currency, base amount, FX rate and source, dimensions, `posting_date` and `recorded_at`. | ENG-03, §11.4 |
| One posting path: `post_entry()` in the domain layer. Database triggers are the backstop, not the logic. | §11.4 |
| Posted history is never edited. An edit in an open period posts a reversal plus replacement entries; "void" means a reversal with a reason. | FR-GL-12 |
| Scheduled recognition (spreads, depreciation, auto-reversals) posts real entries up to the current period only; the future is a flagged projection, never mixed with actuals. | ENG-05 |
| Debit is **positive** and credit **negative**, in base currency (the workbook convention). | §10.2 |

## Where things live (planned paths)

| Path | Holds |
|---|---|
| `internal/money/` | `Money` (cockroachdb/apd v3 decimal + ISO 4217 code), currency minor units, the named rounding and allocation policies |
| `internal/engine/` | One rule set per source (`banktxn`, `invoice`, `bill`, `payrun`, `asset`, `journal`, `claim`, `fxreval`, …), each versioned |
| `internal/ledger/` | `post_entry()`, entry and line types, idempotency keys, reversal helpers |
| `internal/balance/` | The balance service: account × period × dimension-set × currency, "as at" a ledger version (FR-RPT-08) |
| `internal/params/` | Lookup of the parameter-pack version in force at a tax point or pay month (ENG-31) |
| `db/migrations/` | Tables, `NUMERIC` precision per column family, the constraints and triggers behind INV-01…06 |
| `oracle/` | The **independent** Python reference ledger (QA-ENG-01) |
| `packs/` | Statutory parameter packs (data; see **fh-sg-statutory**) |
| `references/posting-rules.md` (this skill) | Every leg of every source, in detail |

## Posting map (summary)

Full legs, tolerances and edge cases are in `references/posting-rules.md` (PRD §10.2,
Appendix B).

| Source | Debit | Credit |
|---|---|---|
| Bank money in | Bank (gross) | Category (net); 2200 GST control (output GST via `RND-GST-INCL`) |
| Bank money out | Category (net); 2200 (input GST) | Bank (gross) |
| … spread over N months | 1210 Prepayments (out) / 2400 Deferred revenue (in), released monthly with `ALLOC-EVEN` | |
| Invoice | 1100 AR (total) | Revenue or 2400 (net, `RND-FX`); 2200 (`RND-GST-EXCL`) |
| Bill | Expense / asset / 1210 (net); 2200 | 2010 AP (total) |
| Customer / supplier payment | Bank / 2010 | 1100 / Bank |
| Settlement difference within tolerance | 7030 or AR/AP | AR/AP or 7030, in the month of the last payment |
| Pay run | Salary by department (gross); 6020 ER CPF; 6030 SDL | 2100 net pay; 2110 EE + ER CPF + SDL + SHG |
| Depreciation | 6610 | 1590 |
| Expense claim | Expense (net); 2200 | 2150 |
| FX revaluation (new) | Open FC items or unrealised FX loss | Unrealised FX gain or the items; auto-reverses on day 1 of the next period |
| Disposal, tax provision, bad debt, provider payout (new) | See references | |
| Founders' balances (rev 0.3, PRD §8.22): cost paid personally, money lent, deferred pay, conversion to shares, waiver | Expense / Bank / 2100 / 2500 or 2120 | 2500 or 2120 / 3010–3020 / 3300 — see references B.7 |

**System accounts** (protected, FR-COA-03): 1100 AR · 1210 Prepayments · 1590 Accumulated
depreciation · 2010 AP · 2100 Salaries payable · 2110 CPF & SDL payable · 2150 Expense claims
payable · 2200 GST control · 2300 Income tax payable · 2400 Deferred revenue · 2999 Suspense ·
3100 Retained earnings · 6020 Employer CPF · 6030 SDL · 6610 Depreciation · 7030 FX & rounding ·
8010 Income tax expense. From rev 0.3 also 2500 Loan from director / shareholder and proposed 2120
Deferred remuneration payable (both with a sub-ledger per person), 3010/3020 Share capital (posted
only from the register of share issues) and proposed 3300 Capital contribution. Codes for unrealised FX, provider clearing, disposal gain/loss and bad
debts are still to be assigned in the CoA review (Appendix B.5).

## Money and precision (non-negotiable)

| Rule | Requirement |
|---|---|
| No binary floating point for money, rates or quantities anywhere: database, Go, TypeScript, exports. A CI lint fails on `float32`/`float64` in money packages and on `number` arithmetic for amounts in the web app. | ENG-10 |
| Money = exact decimal + currency; stored at the currency's minor units (SGD 2, JPY 0, KWD 3). FX rates ≥ 10 decimal places; quantities and unit prices ≤ 6. | ENG-11 |
| Column families: amounts `NUMERIC(20,4)` + `currency` + `CHECK (amount = trunc(amount, minor_units))`; FX `NUMERIC(20,10)`; quantities `NUMERIC(20,6)`; rates `NUMERIC(9,6)`. **Unconstrained `NUMERIC` is forbidden.** | §11.3 |
| Money crosses the API as decimal strings (`"1234.50"`) with its currency, never as JSON numbers. | ENG-12 |
| Adding SGD to USD is a type error unless an explicit conversion (rate + source) is applied. | ENG-13 |
| Base amounts are computed once at posting and stored; reports never re-convert history. | ENG-14 |

## Named rounding and allocation policies

Every rounding uses a **named policy**, the entries record which, and each policy has property
tests. "Half-up" means half away from zero (PostgreSQL `round()` on `NUMERIC` does this; apd needs
`RoundHalfUp` explicitly).

| Policy | Rule |
|---|---|
| `RND-FX` | base = round(foreign × rate, base minor units), half-up |
| `RND-GST-EXCL` | GST = round(net base per GST code × rate, 2), half-up, **per subtotal** by default; per line is an organisation setting, applied consistently |
| `RND-GST-INCL` | GST = round(gross × rate ÷ (1 + rate), 2), half-up; net = gross − GST |
| `RND-CPF` | total = round((capped OW + AW) × (ER% + EE%), 0), half-up; EE = round **down** to the dollar; ER = total − EE |
| `RND-SDL` | clamp(round(remuneration × rate, 2), min, max); 0 when remuneration ≤ 0 |
| `ALLOC-EVEN` | each period = amount ÷ n rounded **down** to the minor unit; remainder cents to the **last** period; parts sum exactly to the whole |
| `ALLOC-PRORATA` | largest-remainder method by weights; ties broken by stable order |
| `RND-SHARE` | (rev 0.3) shares for an amount at a price: the largest whole n with round(n × price, 2) ≤ amount, half-up; converted = round(n × price, 2); the remainder stays on the source balance; never fractional shares |
| `RND-DISPLAY` | display never re-rounds; totals are sums of stored amounts, so reports foot and cross-foot |

`ALLOC-EVEN` is the one **deliberate** difference from the workbook (which used unrounded monthly
amounts). It is the only tolerated difference in the parity tests (§4.3, QA-MIG-01).

## Invariants: the database enforces them, tests and the nightly job prove them

| # | Invariant | Enforced by | Broken usually means |
|---|---|---|---|
| INV-01 | Each entry balances in base (and per currency for single-currency entries) | Deferred constraint trigger at commit | One leg skipped by a validity condition; rounding applied to one leg only |
| INV-02 | Posted entries and lines are immutable | Revoked privileges + trigger | Someone tried to "fix" history in place; use a reversal |
| INV-03 | No posting date in a locked period | Trigger on the period table | A back-dated edit or scheduler run into a closed month |
| INV-04 | Lines post to active, postable (leaf) accounts of the organisation | FKs + trigger | A parent account or a deactivated account in a rule |
| INV-05 | Amounts at the currency's scale; base consistent with `RND-FX` | CHECK + trigger | Arithmetic before rounding, or a rate applied twice |
| INV-06 | Every row has `organisation_id` and is visible only via RLS | `FORCE ROW LEVEL SECURITY`; transaction-scoped `app.org_id` | Missing `set_config(..., true)`; a role with `BYPASSRLS` |
| INV-07 | Subledgers = control accounts (AR = 1100, AP = 2010, payroll liabilities, FA register, GST detail) | Nightly verification + Checks | A document status change without its posting; a manual journal to a control account |
| INV-08 | Materialised balances = recomputation from lines | Nightly verification | The balance cache updated outside `post_entry()` |
| INV-09 | Trial balance nets to zero every period | Follows from INV-01 | (cannot break alone) |
| INV-10 | Balance sheet balances; P&L and BS include every account | Verified; Checks | An account type or report group mapped to neither statement |
| INV-11 | Cash flow agrees to money accounts; equity movement reconciles | Verified; Checks | A balance-sheet account without a cash-flow line |
| INV-12 | Audit chain unbroken and matches its signed anchors | Nightly verification | Out-of-band writes; see **fh-security-compliance** |
| INV-13 | (rev 0.3) Share capital changes only through the register of share issues (or opening balances); per-person sub-ledgers = 2500 and 2120 | Trigger on the posting source + nightly verification | A manual journal to share capital; a director posting without its person key |

## Two implementations that must agree, plus a backstop

1. **The engine** (`internal/engine`, Go).
2. **The oracle** (`oracle/`, Python, Hypothesis). Write it **from the rule in PRD §10 and
   Appendix B, never by translating the Go code**. Translating copies the bugs. Seed it from the
   workbook's `tests/reference.py`.
3. **The database invariants** (`db/migrations`): they refuse what both implementations might
   get wrong.

Differential tests (QA-ENG-02) compare the trial balance, every subledger and every report figure
to the cent on golden data sets and thousands of generated scenarios. See **fh-quality-gates**.

## Recipe: change a posting rule

1. State the rule in debit/credit terms and check it against PRD §10.2 / Appendix B. If the rule
   changes, the PRD changes in the same pull request (**fh-release**, PRD revision convention).
2. Add a **new rule version**. Never alter the behaviour of an existing version: replay tests
   (ENG-06, QA-ENG-06) must reproduce stored entries byte for byte.
3. Change the oracle **independently**, from the stated rule.
4. Add a minimal regression scenario plus a property if the rule has one (e.g. "reversal +
   original nets to zero"). Tag tests `// req: <ID>`.
5. If validation changes, update the message and error code (ENG-20…23) and add a fixture that
   triggers it (QA-RPT-02).
6. `make test` (arrives in P0). Commit as `feat(engine): …` or `fix(engine): …`; if existing
   numbers change for users, write an **Upgrade notes:** paragraph saying which figures will
   differ and from which period.

## Recipe: add a posting source

1. Model the document and its immutable versions (`document`, `document_version`, §11.4).
2. Define its validation with ✖/⚠ severities and plain-English messages (ENG-21/23).
3. Write the rule set in `internal/engine/<source>/` as a pure function with a rule version.
4. Wire it through `post_entry()` in the same transaction as the document command (ENG-02),
   with an idempotency key (§11.4).
5. Extend the subledger-vs-control reconciliation (INV-07) if it touches AR, AP, payroll, assets
   or GST. Add a Checks entry for items that cannot post (Appendix C).
6. Add the source to the oracle, the golden fixtures (DEV-04 sample generator), the AI semantic
   layer if it adds measures, and the audit events (AUD-01).
7. Database: the new tables get `organisation_id`, RLS, `NUMERIC` precision per family, and the
   migration passes squawk (§12.2).

## Debugging an imbalance or a mismatch

| You see | Do |
|---|---|
| Commit rejected by the INV-01 trigger | Print the entry's lines; sum by currency and base. Usually one leg's validity condition differs, or one leg was rounded and its partner not. |
| Differential test: `account@period: engine X vs oracle Y` | Find the **first** period that differs, then the smallest scenario that reproduces it (Hypothesis shrinks for you). Decide from the PRD rule which side is wrong; never "fix" the oracle to match the engine. |
| Off by 0.01 | Which policy? Per-line vs per-subtotal GST (`RND-GST-EXCL`); `ALLOC-EVEN` remainder in the wrong period; half-even instead of half-up in apd. |
| Off by S$1 on CPF | `RND-CPF`: EE rounded down, ER = total − EE; the AW ceiling tracked year to date; the age band changes the month **after** the birthday. |
| FX difference | The rate cascade (ENG-40): document rate → daily feed → monthly table average → ✖. The rate source is stored on every line. |
| Report ≠ trial balance | Something bypassed the balance service (ENG-04). Search for SQL that sums `journal_line` outside `internal/balance`. |
| Nightly INV-07/08 failure | Compare the cache and the recompute per account and period; look for writes outside `post_entry()`. Treat it as an incident (**fh-bug-fix**). |

Locally: `make dev` with the sample company and the business-date override (DEV-05), then the
failing test in isolation. All of this arrives in P0.

## Never

- Floats, or unconstrained `NUMERIC`, anywhere near money.
- `UPDATE` or `DELETE` on posted rows, or correcting data with SQL. Correct with an approved
  journal or a reversal (**fh-bug-fix**).
- A report, check or AI tool that sums ledger lines itself instead of asking the balance service.
- Changing a statutory rate in code. Rates live in packs (**fh-sg-statutory**).
- Computing expected test values by running the engine.

## Definition of done

- [ ] The rule is stated in the PRD (§10 / Appendix B) and matches the code.
- [ ] A new rule version if behaviour changed; replay tests are still green.
- [ ] The oracle was updated independently, and differential tests are green to the cent.
- [ ] Property tests for the policy or rule; every new message is triggered by a fixture.
- [ ] Invariant attack tests (QA-ENG-05) cover any new table or role.
- [ ] Audit events are emitted for every new state change (§16.3).
- [ ] The Conventional Commit has upgrade notes if users' figures change.
