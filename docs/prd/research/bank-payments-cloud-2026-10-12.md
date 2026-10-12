# Source notes: bank data, payment providers, claims tax, ticketing, templates, cloud

Supports PRD-001 revision 0.7. AI-assisted research passes collected these notes on 12 Oct 2026,
from official sources where they exist; "(sec)" marks a secondary source. Pages were read
through a summarising fetch tool, so quotes are as relayed. This is general information; the
accountant, the tax agent and the bank confirm before reliance.

## 1. Singapore bank data (DBS first)

| Method | Data and format | Onboarding and fees | Status |
|---|---|---|---|
| DBS IDEAL scheduled reports and alerts | Daily account report scheduled and emailed (PDF, Excel, text); real-time email alerts for balances and incoming funds | Included at no extra charge (dbs.com.sg IDEAL "comprehensive services") | Verified |
| DBS IDEAL statement export | MT940 (SWIFT), MT940 (SAP), CSV; BAI2 and camt.053 not found | Online IDEAL application opens instantly; digital token free, unlimited users (DBS SME Pricing Guide, 22 Sep 2026) | Verified |
| DBS account fees | Starter Bundle (incorporated < 3 years) S$10 a month; Business Multi-Currency Account S$50 a year + S$40 a month, waived at an average daily balance of S$10k | DBS SME Pricing Guide (22 Sep 2026) | Verified |
| DBS RAPID API | Balance enquiry, account-data enquiry, inward and intraday credit notification, statements; REST/JSON with PGP | Through the relationship manager or BusinessCare; about 4–6 weeks; fees not published (quoted per client); test environment and IP allowlisting appear on DBS India's form only | Names and lead time verified; Singapore security, push vs poll and fees unclear |
| DBS IDEAL Connect | SFTP or Connect:Direct with PGP; MT940 daily, MT942 every 1–4 hours | Set-up and annual fees; 4–6 weeks (DBS India) | Singapore details unclear |
| DBS feeds to accounting software | Daily feed to Xero (authorised in IDEAL); QuickBooks Online (first import up to 48 hours) | No public partner programme found for other software | Unclear for third parties |
| UOB API Services | Balance, history and search, credit/debit notifications, JSON; open to any corporate client | Fees not published | Verified (fees unclear) |
| OCBC | Credit-notification APIs and host-to-host files; no Singapore account-information API found | — | Unclear |
| Aggregators | Plaid has no Singapore; Finverse lists Singapore (sec); others not found for business accounts | Credential sharing is a bank-terms risk | Do not use credential-sharing aggregators |
| Wise Business | Self-serve API token; balance statements in JSON, CSV, PDF, XLSX, camt.053, MT940 (up to 469 days per call); signed webhooks; sandbox | No API fee found | Verified |
| Airwallex | Self-serve scoped API keys; balances, financial transactions and reports; webhooks; sandbox | Explore free, Grow S$79 a month | Verified |
| Aspire | Transactions and bank-feed APIs aimed at solution providers | Account-holder access unclear | Unclear |
| Regulation | No mandated open banking for businesses; the ABS/MAS API Playbook is voluntary; SGFinDex is for individuals (MAS, 27 Aug 2026) | — | Verified |

## 2. Payment providers

| Item | Finding | Source |
|---|---|---|
| Stripe Singapore pricing | Domestic cards 3.4% + S$0.50; international +0.5%; conversion +2%; PayNow 1.3%; GrabPay 3.3%; Stripe Billing 0.7% of billing volume; dispute S$15; standard payouts free | stripe.com/en-sg/pricing |
| Stripe GST on fees | GST on "certain" fees (Connect, Terminal hardware, Radar) with a monthly tax invoice; processing fees not stated | Stripe support |
| Stripe data | Balance transactions with `amount` (gross), `fee`, `net`, integer minor units, `reporting_category`; itemised payout reconciliation report (needs automatic payouts); webhooks signed with `Stripe-Signature` (HMAC-SHA256), retried for 3 days, unordered, deduplicate on event ID; `Idempotency-Key` up to 255 characters | docs.stripe.com |
| HitPay | PayNow QR 0.65% + S$0.30 (0.9% under S$100); domestic cards 2.8% + S$0.50; international 3.65% + S$0.50; no monthly fee; non-card payouts T+1; API for payment requests, recurring billing, refunds, charges, charges per payout, balances; **amounts are JSON numbers**; webhooks v2 `Hitpay-Signature` (HMAC over the raw body) or legacy `hmac`; GST on fees not found | hitpayapp.com; docs.hitpayapp.com |
| Practice | Gross sales to a clearing account, fees to expense, payout out of clearing so it nets to zero; without an integration the bank shows only net lump sums (sec) | Synder blog; Stripe report docs |

## 3. Ticketing

| Item | Finding | Source |
|---|---|---|
| Jira Cloud | `POST /rest/api/3/issue`, `PUT /rest/api/3/issue/{key}`; descriptions in Atlassian Document Format; OAuth 2.0 (3LO), Forge, or scoped API tokens (expire in 1–365 days); webhooks signed in `X-Hub-Signature`, deduplicated on `X-Atlassian-Webhook-Identifier`, REST-registered ones expire after 30 days; rate limits with `Retry-After`; Singapore data residency on Standard and above | developer.atlassian.com; support.atlassian.com |
| Others | Linear GraphQL (API key or OAuth); Zoho Desk REST (OAuth with `orgId`, per-region domains); GitHub Issues REST or GraphQL | Vendor docs |

## 4. Claims tax (IRAS, CPF)

| Item | Finding | Source |
|---|---|---|
| Reimbursements | Reimbursing actual business expenses is not taxable; allowances and amounts above the acceptable rate are taxable | IRAS employer pages (6 Mar and 11 Aug 2026) |
| Mileage | Business mileage in a private car is not taxable; **no prescribed per-km rate, it must be reasonable**; private mileage is taxable | IRAS car and car-related benefits (7 Aug 2026) |
| IR8A | Only taxable items are reported | IRAS |
| Input GST on mileage | Not claimable: no tax invoice, and motor-car expenses are disallowed (reg. 27) | IRAS GST motor vehicles (30 Sep 2026) |
| CPF | Reimbursements for official purposes, not above the actual spend, are not wages | CPF Board (7 Jun 2024) |

## 5. Templates and project revenue

| Item | Finding | Source |
|---|---|---|
| Typst | v0.15.1 (17 Jul 2026); tagged PDFs by default and PDF/UA-1 since 0.14; PDF/UA-1 with PDF/A-2a in 0.15; `sys.inputs` values are strings (pass JSON as a file); file access read-only under `--root`; **no network except `@preview` package downloads**, so ship packages with the app; no official Go binding (CLI or the Rust crate) | typst.app; GitHub |
| SFRS(I) 15 | Over time when a ¶35 test is met; the cost-to-cost input method (B18), excluding costs that do not show progress (B19) | AASB 15 text (identical) |
| SFRS for Small Entities | 2024 edition §23.14 and §23.22 (percentage of completion); the third edition applies from 1 Jan 2027 and follows IFRS for SMEs third edition's simplified five-step model (sec) | ACRA (18 Aug 2025); ICAEW (sec) |

## 6. Cloud facts for scale to zero and portability

| Item | Finding | Source (date) |
|---|---|---|
| Cloud Tasks | HTTP dispatch deadline 15 s–30 min (default 10 min); `scheduleTime` up to 30 days ahead; named tasks deduplicated up to 24 hours; OIDC to Cloud Run with Run Invoker | Google Cloud docs (30 Sep–9 Oct 2026) |
| Cloud Run | Request timeout up to 60 min; scales from zero only on a request; request-based billing throttles CPU outside requests; jobs run up to 7 days; **worker pools are billed even when idle** | Google Cloud docs (9 Oct 2026) |
| Cloud Scheduler | 5,000 jobs per region; $0.10 per job a month; 3 free per billing account | Google Cloud docs |
| Cloud SQL | No idle auto-stop; a manual stop still charges for storage and the IP | Google Cloud docs (9 Oct 2026) |
| AWS | EventBridge Scheduler (one-time and cron, 60 s precision); Lambda maximum 15 min; SQS delay maximum 15 min; ECS services can go to 0, RunTask scales to zero by nature; **App Runner closed to new customers**; RDS and Aurora PostgreSQL 16–17 support pg_cron, pgaudit, pg_partman, pgcrypto; Aurora serverless pauses at 0 capacity (resume about 15 s) but skips pg_cron while paused; S3 Object Lock; KMS and Secrets Manager in ap-southeast-1 | AWS docs |
| Azure | Container Apps scale to zero, **HTTP ingress timeout 240 s**; Container Apps jobs (manual, cron in UTC, event); Service Bus scheduled messages (no recurrence); Flexible Server has the four extensions (via `shared_preload_libraries`), stops only manually and restarts after 7 days; Blob immutable storage; Southeast Asia region in Singapore | Microsoft Learn (2025–2026) |
| Claude by cloud | Vertex AI: only Claude **Sonnet 4.6** on the `asia-southeast1` regional endpoint; Opus 5.5, Sonnet 5.5 and Haiku 5.5 are US, EU or global. Bedrock ap-southeast-1: only Claude Sonnet 5 in-region; newer models via the global profile. Microsoft Foundry: Global or US Data Zone only | Google, AWS and Anthropic docs (9 Oct 2026) |
| Identity | Ory Kratos and Hydra, Keycloak: Apache-2.0; Zitadel: AGPL-3.0 since v3 (2025); AWS Cognito; Microsoft Entra External ID (Azure AD B2C closed to new customers on 1 May 2025) | GitHub; AWS; Microsoft |

## Not verified

- **DBS:** Singapore RAPID fees, eligibility, security model and push vs poll; the current Xero
  or QuickBooks feed cost; whether DBS opens feeds to other software; BAI2 or camt.053 support.
- **OCBC and Aspire:** OCBC's account-information API; Aspire API access for account holders.
- **Payment providers:** Stripe GST on processing fees and the payout timing for Singapore;
  HitPay GST on fees and tax invoices.
- **Ticketing:** ServiceNow auth; Atlassian Connect end of support.
- **Tax:** the current-year IR8A notes; CPF on per-km mileage rates.
- **Accounting standards:** the third-edition Section 23 text of SFRS for Small Entities.
- **Cloud and AI:** the Cloud Tasks maximum task size (the docs conflict); retry defaults;
  whether Claude Opus 4.6 has an `asia-southeast1` endpoint (ADR-T2, option A).
