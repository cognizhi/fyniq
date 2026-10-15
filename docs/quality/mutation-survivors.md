# Mutation testing: reviewed survivors (QA-ENG-04)

`make mutation` runs gremlins on money, the rounding policies and the posting engine, and fails
below 90% efficacy in any of them. Go mutation tooling is pre-1.0, so every mutant that survives is
reviewed by hand and listed here: either a test is added that kills it, or it is shown to be
equivalent (it cannot change behaviour). Remove an entry when the code it names changes.

| Date | Package | Mutant | Verdict |
|---|---|---|---|
| 15 Oct 2026 | `internal/domain/rounding` | `CONDITIONALS_BOUNDARY` at `AllocProRata`, `minor < 0` → `minor <= 0` | **Equivalent.** It changes only a zero amount, which then takes the sign −1; every part of zero is zero, and money has no negative zero, so the parts are identical |

Results on 15 Oct 2026 (p0-money-posting-engine): money 100% (35 killed), rounding 97.6% (41 killed,
1 equivalent), ledger 100% (21 killed). Survivors killed by new tests that day: the inclusive
length limits of memos and descriptions, the reversal's memo without a reason, and the
weight named in an `ALLOC-PRORATA` error.
