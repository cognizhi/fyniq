# Runbook: the cost review

**When:** once before P1 go-live, with the first whole month of staging's bill (NFR-COST-04, a gate of
`p1-launch-readiness`); then each quarter, and whenever a budget alert or the idle-compute alert fires.
**Requirements:** PRD-001 NFR-COST-01 to NFR-COST-04, §12.6; `p1-operations-dr` design D9.
**Who:** the Owner, with whoever reads the billing account.

Fyniq's costs are written down in PRD-001 §12.6 as a range per item. The review compares a real month with them, line by
line, and either confirms the model or changes it, so nobody learns the platform's cost from a surprising bill.

## What watches the cost all the time

| Guard | What it does | Where |
|---|---|---|
| Budgets (NFR-COST-01, NFR-COST-03) | Email both founders at 50%, 80% and 100% of the month's budget (production US$400, staging US$120). They alert and never cap: a cap would stop the service mid-month | `infra/gcp/modules/budget` |
| Idle compute (NFR-COST-02) | Alerts the founders when a Cloud Run service is billed for compute through an hour in which it answered no request. Production's scanner is excepted: its one warm instance is a listed fixed fee (rev 0.249) | `infra/gcp/modules/slo` |
| The AI budget (AI-13, AI-43) | Degrades AI features only; deadlines and accounting never depend on it (NFR-COST-03) | the AI settings |

## The reconciliation (NFR-COST-04)

1. **Take the month's bill** for the project: Cloud Billing › Reports, grouped by service and SKU, for the whole
   calendar month (the BigQuery billing export when it is on). Note the billing account's currency `[verify]`.
2. **Take the month's workload**, the drivers §12.6 is estimated from:
   - requests to each service (Cloud Run's request count) and the minutes of open streams;
   - files scanned, and the scanner's warm hours;
   - database size, backup and point-in-time log storage, and the weekly exports kept;
   - log volume into the `_Default` and security buckets, and traces kept;
   - the model's input, output and cached tokens;
   - partners' quotes for anything billed outside Google Cloud.
3. **Fill in the table**, one row per line of §12.6, in the release record (the pull request that releases P1):

   | §12.6 item | Estimate | Billed this month | Driver this month | Difference and why |
   |---|---|---|---|---|
   | Cloud Run (services and jobs) | $0–7 | | requests, warm hours | |
   | Cloud SQL | $36–69 | | instance size, availability | |
   | Database storage, backups, PITR logs | $5–8 | | GB stored, exports kept | |
   | … every other line of §12.6 | | | | |
   | **Total** | ≈ $45–110 | | | |

4. **Explain every line outside its range.** A cost that only grows with use (storage, logs, tokens) needs the
   month's use beside it; a cost billed while nobody used the platform needs the idle alert's history beside it.
5. **Update §12.6** in a PRD revision (`docs(prd): …`, the fyniq-release skill): the new ranges, with the month the
   numbers came from. A new fixed fee billed while idle is added to NFR-COST-02's list by the owner's decision, never
   silently.
6. **Record the review**: the table and the revision's number in the release record. For the go-live gate,
   `p1-launch-readiness` checks it is there.
