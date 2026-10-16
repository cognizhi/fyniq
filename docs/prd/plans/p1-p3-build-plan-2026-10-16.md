# After P0 · the build plan for P1, P2 and P3

**Status: draft for the founders (16 Oct 2026).** This plans everything left after P0: P1 for Fyniq and
for Nemura, the cloud gate and go-live, P2 and P3. It follows PRD-001 §17.2, §17.4 and §18 and PRD-002
§22, and the founders' decisions of 16 Oct 2026 (`p0-foundation-2026-10-14.md`): there is no Google Cloud
for some time, so everything is built and tested in local mode first.

The plan assigns **every requirement in `openspec/specs/` to exactly one change**. That is all
1,348 requirements:
- 106 belong to the P0 changes, which built them or hold them for the cloud gate;
- the other 1,242 are spread over 65 changes below;
- all 199 designed screens after P0 sit with the change that owns most of their requirements.

The coverage table at the end lists each change's requirements and screens.

## How to read it

- **One row is one OpenSpec change.** Each change is created with `/opsx:propose` when its work starts,
  with `skip_specs: true` (the requirements are already approved in the main specs). Its tasks name the
  tests (`// req: <ID>`). The P0 plan created all its changes up front; for about sixty changes, tasks
  written months ahead would go stale, so only the next wave's changes are proposed at a time.
- **Tracks and waves.** P1 Fyniq runs in seven waves. A change can start once every change in its
  "After" column is done, whatever its wave. P1 Nemura is a parallel track. The cloud gate and go-live
  follow the P1 build. P2 and P3 come after go-live.
- **Sizes, not durations.** "Reqs" and "Screens" count what each change builds. §17.2 says P1 is
  re-estimated from the team's capacity once the launch subset is frozen, so this plan gives no weeks.
- **Home changes.** Four tables cut across every module:
  - the permission matrix (Appendix D, in `p1-users-roles`);
  - the checks catalogue (Appendix C, in `p1-checks`);
  - the control registry rows (Appendix F, in `p1-compliance-center`);
  - the quality gates (§16.2, in `p1-launch-readiness`).

  Each row is built with the module it guards, and the home change closes the rows that are left.
- **Mixed phases.** A row such as "P1 (import) / P2 (feed)" sits with its P1 change. The P2 change that
  finishes it names it in its "Builds" text.

## Ground rules

Everything in the P0 plan's "Rules for building P0" still holds. In addition:

1. **Local first.** Every change is built and tested in local mode. Each cloud adapter is tested against
   a fake or an emulator, as in P0. What only the cloud can prove goes to `cloud-first-deployment`, and
   the "Local first" column says what that is for each change. Real company data never comes to a laptop
   (DEV-08), so the migration is rehearsed on the synthetic sample workbook.
2. **Contract, then API, then screen** (golden rule 4). A change adds its operations to the contract with
   `make openapi` before the screens use them through the generated client.
3. **Screens as designed.** A change builds the screens listed for it in the coverage table, as designed
   in `docs/design/screens/` (five frames, light and dark). Every feature comes with:
   - its help key and tooltip;
   - a page in `docs/user-guide/`, compiled into `kb/` in the same commit;
   - a tour step, when it is on the first-run path.
4. **Two implementations.** These changes carry posting rules from §10.2 or Appendix B, and each needs the
   oracle's half:
   - Fyniq: `p1-general-ledger-close`, `p1-multi-currency-fx`, `p1-sales`, `p1-purchases`,
     `p1-expense-claims`, `p1-fixed-assets`, `p1-bank-import-reconcile` and `p1-payment-providers`;
   - also `p1-gst`, `p1-founders-money`, `p1-equity-register` and `p1-payroll-link`;
   - Nemura: `p1-nemura-payroll`.

   The oracle session writes that half from the rules, never from the engine (QA-ENG-01). The engine's
   half lands first and the differential gate closes the change.
5. **Statutory values** come from official sources, as effective-dated packs (`fyniq-sg-statutory`).
   A `[verify]` tag stays until a primary source confirms the value.
6. **AI and MCP are security boundaries.** Wave 6 runs the red-team suites (QA-AI-03, QA-MCP-01) before
   anything is enabled. Nothing is enabled until an Owner switches it on with dual control.
7. **The PRD is the source.** When building shows a requirement is wrong, stop and change the PRD first.
   The PRD session makes that change.
8. **Second human reviewer.** The "Reviewer" column says where a person should look. Before production
   the founders waived the review, so pull requests merge after automatic approval. The rule returns
   before the first production deploy.

## Where P1 entry stands (§17.4)

| Criterion | Today | What is needed |
|---|---|---|
| 1 · Every question needed by P1 planning or build has a recorded decision | None recorded: 27 of PRD-001's open questions and Nemura's 13 | The founders answer the decision sheet below. A default counts only once someone accepts it |
| 2 · The launch subset is frozen (open question 25) | Proposed below, not frozen | The founders freeze it. The first task of `p1-launch-readiness`, done early in wave 1 ahead of the rest of that change, records the subset in the repository and makes the trace gate (QA-TRACE-01) check it. Progress is then measured against the subset from the start |
| 3 · Accountant and tax agent reviews | No longer a gate (rev 0.22): pages under review show the FR-UX-41 banner | — |

By the PRD, P1 build starts once criteria 1 and 2 hold. The first wave needs only a few of the
decisions (questions 5, 20, 35, 44 and 45, and Nemura's 2, 10 and 11), so answering those first lets
wave 1 start while the rest are settled.

## The launch subset (proposed)

Open question 25's default puts these in the launch subset:
- R2's original line: workbook parity (Appendix A), with approvals, audit, checks and controls;
- the rev 0.3 items: founders' money, the equity register, evidence and help;
- whatever Cognizhi's actual go-live transactions need.

Every other P1 row is "P1 after go-live". Applied change by change, the "Launch (proposed)" column reads:

- **Launch:**
  - 29 changes: the foundations, the ledger and its documents, banking, founders' money and the
    equity register, reports, KPIs and planning;
  - checks, the audit explorer, compliance and PDPA;
  - hardening, operations, the migration and readiness.
- **Decide:**
  - claims, payment files, the company secretary, payroll and Nemura;
  - the connector framework and payment providers, which depend on which providers Cognizhi uses
    (questions 23 and 45);
  - GST: the monitor and the calendar are in the launch subset, and the F5 return follows registration
    (question 2).
- **After go-live:** the AI and MCP changes. They are P1 by §17.2, but R2's line does not include them,
  and they are the riskiest surface.

With question 16's default (no founder salary), payroll and Nemura need not go live with Fyniq. Each
promotion into the subset names the work it displaces (§17.4).

## Decision sheet

These questions are open in PRD-001 §20 and PRD-002 §23. Each line gives the question and its
recommended default, which the founders can accept or replace. Defaults are shortened here, and the PRD
has the full text.

**Before the plan is final**
- **Q1:** Is Fyniq only for Cognizhi's books, or a future SaaS? Default: own books first, keeping the
  multi-tenant design, and decide on SaaS after six months of live use.
- **Q2:** GST status and date; does InvoiceNow apply? Default: not registered until the monitor or the
  tax agent says so, and register voluntarily only once InvoiceNow works.
- **Q4:** Payroll in-house or through a provider? Default: in-house while there are fewer than 10
  employees.
- **Q12:** MCP timing? Default: first-party clients in P1 and third parties in P2, off until an Owner
  enables it.
- **Q25:** The launch subset? Default: as proposed above.
- **Q26:** Build, buy or hybrid over 24 months? Default: build, once a one-page comparison of the three is
  recorded and signed.
- **Q44:** Template editor in P1? Default: the starter gallery, theme and texts, the locked IRAS blocks,
  immutable versions and email templates. Drag-and-drop layout, the column chooser, conditional blocks,
  the visual diff and code mode move to P2. If that is accepted, the PRD session moves those halves of
  FR-TPL-01/02/04 to P2.
- **Q45:** One adapter per capability at go-live? Default: yes. That means one email provider plus SMTP,
  inbound capture, Stripe, API connectors only for the EMI accounts held, and Google and Zoho sign-in.
  The email failover and HitPay move to P2.
- **Nemura Q1:** Does payroll move to Nemura at P1? Default: yes; Nemura runs §8.7 and Fyniq posts.
- **Nemura Q8:** Is Nemura sold on its own, and at what price? Default: decide before P1 build.
- **Nemura Q12:** Which Nemura rows go live with Fyniq? Default: decide with Q25.

**Wave 1**
- **Q5:** Founder-mode approval threshold? Default: S$2,000 self-approval per document, with dual approval
  above it and always for the FM-1 list.
- **Q20:** Email providers and data centres? Default: ZeptoMail as primary (US data centre), Resend as
  failover and for receiving capture addresses.
- **Q35:** Break-glass lists, cap and reviewer? Default: rev 0.8's lists, S$20,000 per break-glass, and the
  external accountant as reviewer of last resort.
- **Nemura Q2:** One Identity Platform tenant per organisation? Default: yes; a linked organisation shares
  Fyniq's.
- **Nemura Q10:** One passkey relying party for both apps? Default: yes, under one registrable domain.
- **Nemura Q11:** Nemura's own token issuer? Default: its own instance of the same server build.

**Wave 2**
- **Q15:** Founders' existing balances? Default: interest-free loans repayable on demand, converted to
  shares at the first priced round.
- **Q31:** Accounting framework? Default: SFRS for Small Entities, if the accountant confirms the company
  qualifies.
- **Q32:** Settlement tolerances? Default: S$1.00, or for foreign currency the greater of S$1.00 and 3%.
  Capped at S$50 a document and S$200 a month, subject to the accountant's confirmation.

**Wave 3**
- **Q3:** Which banks and providers? Default: statement files for every account, DBS's emailed reports,
  and the Wise and Airwallex APIs where those accounts exist.
- **Q23:** Customer payments? Default: Stripe Billing for self-serve subscriptions (Model B), with
  read-only connectors in P1.

**Wave 4**
- **Q16:** Founder pay from go-live? Default: no salary, agreed in writing and reviewed each year. A
  founder on an Employment Pass must still be paid the declared salary.
- **Q27:** Where the SDL rounding difference goes? Default: to 7030 when the remittance posts.
- **Q28:** Any paid employee before P2's payroll outputs? Default: keep the P1 gate. On 14 Oct 2026 the
  founders already decided that payslips come from Nemura or the provider, never from Fyniq.
- **Q33:** Which SAFE and note forms? Default: YC's Singapore post-money SAFE as the first template
  `[verify]`.
- **Nemura Q3:** Leave year? Default: calendar year, prorated.
- **Nemura Q4:** Leave liability accrual? Default: off; the accounting policy decides.
- **Nemura Q5:** Letter signatures? Default: a passkey signature with a verification link.
- **Nemura Q13:** First payroll month? Default: native, after two months of parallel comparison.

**Wave 5**
- **Q34:** Which MRR to show? Default: contracted once subscription data exists, booked until then.
- **Q38:** After retention ends? Default: keep the ledger and its audit for life, purge documents and
  shred personal values.
- **Q39:** Counsel on PDPA retention? Default: proceed with §11.8; counsel confirms before payroll data is
  loaded.

**Wave 6**
- **Q7:** AI data residency? Default: Singapore-only models at go-live.
- **Q18:** Publishing organisation knowledge? Default: self-approval with the weekly review in Founder
  mode.
- **Q19:** Who is the company secretary? Default: a corporate secretarial firm.
- **Q41:** Who handles registrable controllers? Default: the named company secretary, with the AI only
  reminding and drafting.
- **Q46:** Model providers at go-live? Default: Agent Platform in Singapore for every agent.
- **Nemura Q7:** First external payroll connector? Default: the file import first.
- **L1 (new, from local first):** Which AI provider serves AI work in local mode until the cloud exists?
  Q7 and Q46's defaults need Google Cloud. The options:
  - replay only, so no live model calls until the cloud gate;
  - a direct provider API, used with the synthetic sample company only;
  - a local model.

  This plan does not choose. `p1-ai-providers` cannot finish its live tests without an answer.

**Go-live**
- **Q8:** Which certifications first? Default: CSA Cyber Essentials, then Cyber Trust.
- **Q14:** Basic or Expert by role? Default: founders and Owners in Basic, accountants in Expert, confirmed
  by UX tests.
- **Q37:** Losing the Singapore region? Default: say plainly that we wait for the region.

**P2 planning**
- **Q22:** Slack or Zoho Cliq? Default: neither until P2.
- **Q24:** When should DBS move to RAPID? Default: when reconciling by emailed reports costs over about 15
  minutes a day.
- **Q42:** Usage pricing? Default: none at go-live.
- **Nemura Q6:** Is there a MyCareersFuture API? Default: manual with evidence.
- **Nemura Q9:** A grievance channel? Default: build it in P2.

**When the event happens**
- **Q29:** At GST registration: reverse charge? Default: not while all supplies are taxable.
- **Q43:** Before the first grant application: a grant module? Default: none; use the Funding source
  dimension and the evidence rule.

## P1 · Fyniq

**Wave 1 · Foundations.** These are what every module needs:
- the organisation and its chart;
- the API's conventions, claimed as requirements;
- statutory packs;
- users and roles, and the approval engine;
- the app shell;
- documents and evidence, templates and email.

**Wave 2 · The ledger and its documents.** The general ledger and close, then FX, sales, purchases,
claims, fixed assets and projects.

**Wave 3 · Money in and out.** Connectors, bank import and reconciliation, payment files and payment
providers.

**Wave 4 · Tax, equity and payroll postings.**

**Wave 5 · Reports, planning, checks and compliance.**

**Wave 6 · AI and MCP.** This wave is the security boundary.

**Wave 7 · Hardening, migration and readiness.**

| # | Change | Builds | Reqs | Screens | After | Local first | Outside dependency | Reviewer | Launch (proposed) |
|---|---|---|---|---|---|---|---|---|---|
| 1 | `p1-organisation-chart` | Organisation profile, financial year, GST status, defaults, feature switches, logo and brand; the chart of accounts and dimensions; contacts with vendor bank details as high-risk data; items; the logical data model | 34 | 6 | P0 | — | — | Accountant (chart) | Launch |
| 1 | `p1-public-api` | The /v1 conventions claimed as requirements (versioning, problem details, idempotency, concurrency, lists, long and bulk operations, preview before commit, rate limits), machine-readable contracts and the TypeScript SDK; every later change adds its operations here first | 13 | 0 | P0 | — | — | — | Launch |
| 1 | `p1-statutory-packs` | Effective-dated statutory packs with source URLs and pinned tests (GST rates and codes, CPF, SDL, SHG, FWL, deadlines), the tax point, GST rounding, dual approval before activation, and the §14.2 obligations they serve | 25 | 1 | `p1-organisation-chart` | — | — | Tax agent and accountant (#5) | Launch |
| 1 | `p1-users-roles` | Invitations, the finance roles and combinations of Appendix D, MFA with TOTP and passkey step-up, sessions, external users, OAuth clients, Super admin scope and succession, account recovery; the permission matrix of Appendix D, closed row by row as each module arrives | 86 | 5 | P0 | Local sign-in page; Identity Platform through its fake | — | Security (#4), co-founder (#6) | Launch |
| 1 | `p1-approval-engine` | Approval policies per object type, signed approval records, step-up, expiry, approval-gated policy changes; the team modes and the founder-mode rules FM-1…9 with thresholds and break-glass | 21 | 7 | `p1-users-roles` | — | Q5, Q35 | Co-founder (#6) | Launch |
| 1 | `p1-app-shell` | Sidebar and workspaces by role, command palette, global search, Inbox, drill-down, side peeks, grids, record pages, comments, live updates, the clipboard rules, help registry, tooltips, user guide, first-run walkthrough, the FR-UX-41 banner, Basic and Expert modes with the feature registry, themes, accessibility, devices and motion | 83 | 18 | `p1-users-roles` | — | — | — | Launch |
| 1 | `p1-documents-evidence` | Attachments on any record, retention-locked storage, issued documents kept with their versions, evidence rules and defaults, evidence integrity, safe files, two-phase upload, evidence search and export, evidence through the API | 16 | 2 | `p1-organisation-chart` | Local blob directory and fake GCS; the retention lock itself is proved at the cloud gate | — | — | Launch |
| 1 | `p1-templates-email` | Document templates as open question 44 recommends (starter gallery, theme, locked IRAS blocks, immutable versions), the render service, outbound and inbound email behind adapters, the notification centre, preferences, banners and one event stream | 14 | 6 | `p1-documents-evidence` | Mailpit; the chosen email provider's adapter against a fake | Q20, Q44; a sending domain | — | Launch |
| 2 | `p1-general-ledger-close` | Manual journals in full, recurring and auto-reversing journals, schedules, opening balances, period close and lock, year-end, the GL report, flux analysis, the scheduler and system postings, versioned posting rules, the remaining invariants and validation rules; the posting conventions | 24 | 4 | `p1-organisation-chart`, `p1-statutory-packs`, `p1-approval-engine` | — | Q31 (framework, for opening balances) | Accountant | Launch |
| 2 | `p1-multi-currency-fx` | FX rates and the rate cascade, realised and unrealised FX, GST on foreign-currency documents, foreign-currency bank accounts, month-end revaluation with automatic reversal | 8 | 1 | `p1-general-ledger-close` | Rates from a file or fixture; the live rate source is configured at the cloud gate | Q32 | Accountant | Launch |
| 2 | `p1-sales` | Quotes and orders, conversion, invoices, credit notes and refunds, tax-invoice particulars, branded PDFs and sending, recurring invoices, reminders and statements, deposits, allocations, write-offs, AR ageing; their posting rules | 22 | 7 | `p1-general-ledger-close`, `p1-templates-email` | — | — | Accountant | Launch |
| 2 | `p1-purchases` | Purchase orders, bills, vendor credits, recurring bills, track and pay, AP ageing, input-tax eligibility, non-resident payments; their posting rules | 10 | 4 | `p1-general-ledger-close`, `p1-documents-evidence` | — | — | Accountant; co-founder for payee changes (#6) | Launch |
| 2 | `p1-expense-claims` | Expense claims, My claims, mileage, employee claims processing, the expense policy, company-card imports and cash advances; their posting rules | 14 | 6 | `p1-purchases` | — | — | — | Decide (Q25) |
| 2 | `p1-fixed-assets` | Asset register, depreciation by the scheduler, disposals | 5 | 1 | `p1-purchases` | — | — | Accountant | Launch |
| 2 | `p1-projects` | Projects, timesheets, time entry, cost and billing rates, approval, costs, budgets, billing methods and project reports (feature-switched) | 13 | 5 | `p1-sales`, `p1-purchases` | — | — | — | Launch |
| 3 | `p1-connector-framework` | The integration framework, connector manifests, lifecycle and rules (FR-INT-20, 25…27) | 5 | 2 | P0 | Every connector is tested against a fake of its service | Q45 | Security (#4) | Decide (Q45) |
| 3 | `p1-bank-import-reconcile` | Money accounts, statement import (CSV, XLSX, OFX, MT940, camt.053), duplicates, rules, categorising, matching, settlement differences, transfers, reconciliation with sign-off, suspense, daily cash, deterministic auto-reconcile, DBS emailed reports and the Wise and Airwallex sync | 25 | 9 | `p1-sales`, `p1-purchases`, `p1-connector-framework` | Statement files; DBS reports through Mailpit; Wise and Airwallex against fakes | Q3; which accounts Cognizhi holds | Co-founder (#6) | Launch |
| 3 | `p1-payments-out` | Payment batches as bank files with dual-controlled release and remittance advice | 1 | 1 | `p1-bank-import-reconcile`, `p1-approval-engine` | Files only | — | Co-founder (#6): money movement | Decide (Q25) |
| 3 | `p1-payment-providers` | Stripe as the Model B connector (read): events and postings, refunds and disputes, fees and their GST, payout reconciliation; the provider sale, fee and payout posting rule | 9 | 2 | `p1-connector-framework`, `p1-sales` | Stripe test-mode fixtures and a fake | Q23; a Stripe account | Accountant | Decide (Q23) |
| 4 | `p1-gst` | The GST F5 return (recorded filing), IRAS audit listings, the registration monitor, the statutory calendar and GST corrections | 9 | 4 | `p1-sales`, `p1-purchases`, `p1-statutory-packs` | — | Q2 | Tax agent (#5) | Monitor and calendar at launch; F5 at registration (Q2) |
| 4 | `p1-founders-money` | Director accounts, costs paid personally, pre-incorporation costs, founder pay choice, settlement routes, conversion to share capital, waiver, the register of share issues, ACRA steps, presentation and year-end confirmation; their posting rules | 22 | 5 | `p1-general-ledger-close`, `p1-purchases` | — | Q15, Q16 | Accountant | Launch |
| 4 | `p1-equity-register` | Holders, share classes, equity transactions, SAFEs and notes, the option-pool register, the cap table at any date, round modelling, investor records and linked statutory checks; share-issue and conversion posting rules | 15 | 8 | `p1-founders-money` | — | Q33 | Accountant | Launch |
| 4 | `p1-payroll-link` | Fyniq's side of payroll: the people-platform contract (FR-PPL), verifying, posting and paying pay runs from Nemura, a provider's journal or manual entry (FR-PAY-15), salary bank details by version, tax-clearance holds, and the payroll posting rules | 20 | 5 | `p1-general-ledger-close`, `p1-payments-out`, `p1-nemura-payroll` (the Nemura route only) | Nemura and a provider journal file, both local | Q4, Q16, Q27, Q28 | Accountant (#5) | Decide (Q16) |
| 5 | `p1-reports` | Financial statements, periods and comparatives, management reports, exports, reproducible reports, asynchronous exports, accountant exports, the charts and the remaining statement invariants | 18 | 4 | `p1-general-ledger-close` | — | Q31 | Accountant | Launch |
| 5 | `p1-dashboard-kpis` | The default Home and Dashboard and the workbook KPI set as versioned definitions | 3 | 2 | `p1-reports` | — | Q34 | — | Launch |
| 5 | `p1-planning` | Budgets, the driver-based forecast and its outputs, the 13-week and short-term cash forecasts, the feasibility calculator, sandboxed forecasts | 7 | 4 | `p1-reports` | — | — | — | Launch |
| 5 | `p1-checks` | The checks engine, books health and the user access review report; the workbook checks (Appendix C.1) and platform checks (C.2), each closed as its module arrives | 97 | 1 | `p1-reports` | — | — | Accountant | Launch |
| 5 | `p1-audit-explorer` | Sensitive values in the trail, period-close snapshots, logged sensitive reads and exports, audit retention, the audit explorer and export, purge and restore that keep the chain | 9 | 1 | `p1-general-ledger-close` | — | Q38 | Security (#4) | Launch |
| 5 | `p1-compliance-center` | The control dashboard, statutory calendar view, attestations, PDPA registers, risk register, policy library; the control rows of Appendix F (F.1, F.3, F.7), each closed when its check runs, and AI governance | 79 | 3 | `p1-checks` | Evidence from local runs; cloud evidence at the cloud gate | Q8 | Security (#4) | Launch |
| 5 | `p1-pdpa-retention` | The DPO, data inventory, notices and consent, access and correction requests, breach timers, transfers, NRIC handling, DPIAs; encryption, application-level encryption, residency, minimisation; retention per data class with crypto-shredding | 21 | 2 | `p1-documents-evidence`, `p1-audit-explorer` | — | Q38, Q39 (counsel) | Security (#4) | Launch (before payroll data) |
| 6 | `p1-ai-providers` | One model port, provider connectors and capabilities, routes per agent, data handling per provider, failover, usage and budgets, qualification, model lists and prices from their sources | 24 | 8 | `p1-connector-framework` | Replay mode offline; live mode with the provider chosen for local work | Q7, Q46, and the local AI provider (decision L1) | Security (#4) | After go-live (proposed) |
| 6 | `p1-ai-assistant` | On-behalf-of read tools, exclusions, the semantic layer, result limits, typed facts, guardrails against the OWASP LLM risks, the eval suites, Ask, explain and simulate | 24 | 1 | `p1-ai-providers`, `p1-reports` | Evals in replay; live runs with the local AI provider | L1 | Security (#4) | After go-live (proposed) |
| 6 | `p1-knowledge-base` | The organisation knowledge base from trusted sources, context assembly, compile, index and lint, trust, versioning and publishing | 15 | 1 | `p1-ai-assistant` | — | Q18 | Security (#4) | After go-live (proposed) |
| 6 | `p1-ai-proposals` | The proposal principles, the task catalogue of Appendix E, approval of AI work, numeric grounding, idempotent execution through the executor, undo, kill switches and budgets, the red-team cases | 53 | 2 | `p1-ai-assistant`, `p1-approval-engine` | — | — | Security (#4) | After go-live (proposed) |
| 6 | `p1-mcp-first-party` | The MCP server for first-party clients: client classes, off by default, audience-bound tokens with option (b), consent and scopes, transport, the tool surface, limits, audit and detection; the local MCP endpoint; QA-MCP-01 | 18 | 2 | `p1-ai-proposals` | Option (b) with a local key; Cloud KMS at the cloud gate | Q12; security review #4 | Security (#4) | After go-live (proposed) |
| 6 | `p1-scheduled-agents` | Agent skills, schedules and triggers, identity and permissions, deterministic first, outputs, the never-list, budgets and failure | 9 | 2 | `p1-ai-assistant` | — | — | Security (#4) | After go-live (proposed) |
| 6 | `p1-company-secretary` | The corporate profile, ACRA pack, cross-checks, the company secretary workspace and role, document drafts, the named human secretary, and the AI secretary's scheduled checks | 7 | 1 | `p1-scheduled-agents`, `p1-equity-register` | — | Q19, Q41 | Accountant | Decide (Q25) |
| 6 | `p1-document-capture` | Smart document capture (upload, paste, email-in), bill capture and automated bill entry, as drafts a person confirms | 3 | 1 | `p1-ai-providers`, `p1-purchases`, `p1-templates-email` | Inbound email through Mailpit | L1 | Security (#4) | After go-live (proposed) |
| 7 | `p1-security-hardening` | Web protections, uploads, outbound requests to configured hosts, registered private endpoints, dependency hygiene, SLSA provenance and reproducible builds, security monitoring, alerting and the incident runbook, release review and recorded versions | 14 | 0 | P0 | Local logs and alerts; Binary Authorization and cloud posture at the cloud gate | — | Security (#4) | Launch |
| 7 | `p1-operations-dr` | Observability and the scheduler heartbeat, the performance budgets, volume, availability and browsers, cost targets and limits, backup, PITR, DR copy, restore drills and attachment storage | 26 | 1 | P0 | Performance budgets locally; backups, restores and observability at the cloud gate | Q37 | — | Launch |
| 7 | `p1-workbook-migration` | Workbook import, import reconciliation, generic import and full tenant export; the M1…M6 migration steps rehearsed on the synthetic sample workbook | 8 | 2 | `p1-sales`, `p1-purchases`, `p1-bank-import-reconcile`, `p1-founders-money` | The synthetic sample workbook only: real company data never comes to a laptop (DEV-08) | Q15, Q31 | Accountant | Launch |
| 7 | `p1-launch-readiness` | The quality gates as requirements (calculation correctness, test gates), the trace gate against the frozen launch subset, the acceptance scenarios of Appendix G and the UAT scripts | 50 | 0 | `p1-workbook-migration` | — | Q25 frozen | — | Launch |

## P1 · Nemura

Nemura is optional and has its own image, database, web app and sign-in (ADR-N1). Its track can start
after Fyniq's wave 1, because it reuses the users and roles, the statutory pack mechanism and the shell
patterns. Whether it goes live with Fyniq is Nemura's question 12. With no founder salary and no
employees, Nemura can follow Fyniq's launch.

| # | Change | Builds | Reqs | Screens | After | Local first | Outside dependency | Reviewer | Launch (proposed) |
|---|---|---|---|---|---|---|---|---|---|
| 1 | `p1-nemura-platform` | Nemura's own image, database, web app and token issuer (ADR-N1), its non-functional rules, inherited controls, need to know, PDPA and records, roles, the permission matrix, and the experience | 30 | 3 | P0 | Local sign-in page; one Identity Platform tenant per organisation at the cloud gate | Nemura Q2, Q11 | Security (#4) | Decide (Nemura Q12) |
| 2 | `p1-nemura-records-packs` | People records, the Singapore pack with verified values, feature switches and the people checks (N.1) | 37 | 5 | `p1-nemura-platform`, `p1-statutory-packs` | — | — | HR, accountant | Decide (Nemura Q12) |
| 3 | `p1-nemura-leave-time` | Leave types, entitlements, the leave ledger, applying and approving, calendars, medical certificates, government-paid leave, flexible-work requests; working patterns, Part IV overtime and hours worked | 17 | 6 | `p1-nemura-records-packs` | — | Nemura Q3, Q4 | HR | Decide (Nemura Q12) |
| 3 | `p1-nemura-self-service-letters` | Self-service, letters, templates and KETs issued in two steps, what an issued letter proves, accounts for joiners and offboarding with final pay and IR21 | 14 | 7 | `p1-nemura-records-packs` | — | Nemura Q5 | HR | Decide (Nemura Q12) |
| 4 | `p1-nemura-payroll` | Pay runs with the full CPF, SDL, SHG and FWL rules (PRD-001 §8.7 in Nemura, ADR-N2), overrides, year to date, approval, salary deduction limits, wage classes, payslips in Nemura, run and payment states and corrections; employee records; the oracle's half by the oracle session | 30 | 5 | `p1-nemura-leave-time`, `p1-nemura-self-service-letters` | — | Q4, Q16, Q27, Q28, Q39; Nemura Q1, Q13 | Accountant, payroll administrator (#5) | Decide (Q16) |
| 4 | `p1-nemura-fyniq-link` | The cross-app contracts and events, one sign-in and the workspace switch, segregation across both apps; paired with p1-payroll-link | 19 | 1 | `p1-nemura-payroll`, `p1-users-roles` | Both apps in one local stack | Nemura Q10 | Security (#4) | Decide (Nemura Q12) |
| 5 | `p1-nemura-assistant-external` | The people assistant (questions and letter drafts, never-list, own pay), Fyniq without Nemura, the payroll file import and one external connector, imported journals as drafts | 10 | 3 | `p1-nemura-payroll`, `p1-ai-assistant` | Connector against a fake | Nemura Q7 | Security (#4) | Decide (Nemura Q12) |

## The cloud gate and go-live

`cloud-first-deployment` collects everything only Google Cloud can prove. It owns no requirement of its
own: each row stays with its change and is closed here.

- **The first apply.** `infra/README.md`, starting with the founder's organisation, billing account and
  projects.
- **P0's held tasks:**
  - audit 2.3: anchors in the locked bucket;
  - images 2.3 and 3.2: keyless signing into Artifact Registry and deployment by digest;
  - outbox 1.2, 3.2 and 3.3: Cloud Tasks and scale to zero;
  - identity 3.2: a live Zoho sign-in;
  - compliance 2.1: Binary Authorization's check for CTL-CHG-04;
  - every infra task.
- **P1's cloud halves:**
  - Identity Platform, KMS (including the MCP delegation key) and Cloud Storage with its lock;
  - the email domain;
  - observability, backups and restore drills;
  - cloud posture monitoring.

`p1-go-live` then runs UAT, the migration with real data in production, the parallel run (M5), go-live
and the workbook's archive (M6), and writes the release record. Until the cloud exists, the P1 build is
complete when every launch change passes in local mode.

| # | Change | Builds | Reqs | Screens | After | Local first | Outside dependency | Reviewer | Launch (proposed) |
|---|---|---|---|---|---|---|---|---|---|
| 8 | `cloud-first-deployment` | Everything held for the cloud: the founder's Google Cloud organisation and the first apply (infra/README.md), P0's staging tasks (exit items 3, 4 and 7), the live adapters (Identity Platform, Cloud Tasks, KMS, Cloud Storage with the lock), Binary Authorization, observability, backups and restore drills, the email domain, Zoho live | 0 | 0 | `p1-launch-readiness` | — | Google Cloud organisation and billing | Security (#4) | Launch |
| 9 | `p1-go-live` | UAT, the migration with real data in production (never on a laptop), the parallel run (M5), go-live and the workbook's archive (M6), the release record | 2 | 0 | `cloud-first-deployment` | — | Founders, accountant | Accountant | Launch |

## P2 · Run the company

P2 starts after go-live. Most of it needs outside access:
- a Peppol access point;
- IRAS's APIs;
- a bank feed provider or DBS RAPID;
- HitPay.

Each change is planned against its sandbox, like the P1 connectors.

| # | Change | Builds | Reqs | Screens | After | Local first | Outside dependency | Reviewer | Launch (proposed) |
|---|---|---|---|---|---|---|---|---|---|
| 1 | `p2-invoicenow` | InvoiceNow (Peppol) sending and receiving, the submission population and the filing gate | 4 | 1 | `p1-gst` | Against an access point's sandbox | A Peppol access point; Q2 | Tax agent | — |
| 1 | `p2-bank-feeds-payments` | Bank feeds, bank APIs and host-to-host, DBS RAPID or IDEAL Connect, card feeds, payment links (Model A), online payment, corporate cards | 4 | 0 | `p1-bank-import-reconcile`, `p1-payment-providers` | Against sandboxes | Q24; a licensed feed provider; HitPay | Co-founder | — |
| 1 | `p2-tax-filings` | Corporate income tax support, F7 and F8, IRAS API filing, published pack updates, bad-debt relief with GST | 3 | 2 | `p1-gst` | Against IRAS's sandbox | IRAS API access | Tax agent | — |
| 1 | `p2-payroll-outputs` | Payslip documents, the CPF submission file, IR8A and the Auto-Inclusion Scheme (in Nemura where it runs payroll) | 3 | 1 | `p1-nemura-payroll` | — | CPF Board and IRAS formats | Payroll administrator | — |
| 2 | `p2-reporting-planning` | Report builder, board pack, scheduled reports, deferral roll-forward, configurable dashboards, customer counts, anomaly alerts, industry benchmarks, rolling forecast, the funding plan, investor updates, access, obligations and the exit waterfall, shares for services, allocation rules, asset reports | 18 | 7 | `p1-reports`, `p1-planning`, `p1-equity-register` | — | — | Accountant | — |
| 2 | `p2-customer-portal-subscriptions` | The customer portal, subscription revenue schedules, disputes and promise-to-pay, revenue over time for fixed-fee projects | 3 | 1 | `p1-sales`, `p1-projects` | — | — | — | — |
| 2 | `p2-api-third-parties` | Third-party API clients, webhooks and events, the semantic metrics endpoint, the sandbox, Go and Python SDKs, third-party MCP clients, ticketing connectors, Slack and Zoho Cliq | 8 | 2 | `p1-public-api`, `p1-mcp-first-party` | — | Q22 | Security | — |
| 2 | `p2-mobile-approvals-automation` | The PWA with offline reads and camera capture, approval delegation, escalation, approvals by email and PWA, workflow rules without code, outbound webhooks, soft locks, vendor statements, supplier contracts, the usage view, pinned Basic mode, tours, code-mode templates, the annual-return pack, evidence export | 15 | 1 | `p1-app-shell`, `p1-approval-engine` | — | — | Co-founder | — |
| 3 | `p2-ai-expansion` | Approval-quality metrics and more task types, knowledge pages from documents, document AI drafts | 2 | 0 | `p1-ai-proposals`, `p1-knowledge-base` | — | — | Security | — |
| 3 | `p2-security-certification` | Penetration testing and DAST; Cyber Trust or ISO 27001 readiness | 1 | 0 | `p1-security-hardening` | — | Q8 | Security | — |

| # | Change | Builds | Reqs | Screens | After | Local first | Outside dependency | Reviewer | Launch (proposed) |
|---|---|---|---|---|---|---|---|---|---|
| 1 | `p2-nemura-people` | Hiring, performance and KPIs, onboarding plans, letter verification, grievances, the unified HR API and pay documents from external systems, leave accrual, fairness in the assistant | 20 | 3 | `p1-nemura-assistant-external` | — | Nemura Q6, Q9 | HR | — |

## P3 · Scale and SaaS

P3 waits for question 1: whether Fyniq becomes a product for other companies.

| # | Change | Builds | Reqs | Screens | After | Local first | Outside dependency | Reviewer | Launch (proposed) |
|---|---|---|---|---|---|---|---|---|---|
| 1 | `p3-multi-entity` | Several legal entities, intercompany and consolidation, accountant-practice mode, the entity switcher, consolidated reporting | 6 | 3 | `p2-reporting-planning` | — | Q1 | Accountant | — |
| 1 | `p3-saas-platform` | Tenant onboarding at scale, import from other accounting systems, agent-to-agent, outbound agents, third-party connectors, ask from chat, peer benchmarks, price lists, the vault for backups, a portability rehearsal | 10 | 0 | `p2-api-third-parties` | — | Q1 | Security | — |
| 1 | `p3-countries-certification` | The ACRA filing pack and country packs, statements in the recorded framework, withholding tax, template languages, the Data Protection Trustmark, SOC 2 and ISO 27001 certification; Nemura's time and attendance and Malaysia pack | 7 | 1 | `p2-tax-filings` | — | Q8 | Tax agent | — |

## Appendix · Coverage

Each change's requirements by capability, and the screens it builds (IDs from
`docs/design/screens/inventory.json`). "Unnumbered" counts requirements whose heading has no ID, such
as a posting rule or a use case; `openspec show <capability> --type spec` lists them.

| Change | Capability: requirements |
|---|---|
| `p1-organisation-chart` | `data/data-model`: 14 unnumbered; `ledger/chart-of-accounts`: FR-COA-01, FR-COA-02, FR-COA-03, FR-COA-04, FR-COA-05, FR-DIM-01, FR-DIM-02; `master-data/contacts`: FR-CT-01, FR-CT-02, FR-CT-03, FR-CT-04; `master-data/items`: FR-ITM-01; `organisation/settings`: FR-ORG-01, FR-ORG-02, FR-ORG-03, FR-ORG-04, FR-ORG-06, FR-ORG-07, FR-ORG-08, FR-ORG-09<br>Screens: chart-of-accounts, contact-record, contacts-list, items, logo-brand, organisation-settings |
| `p1-public-api` | `integrations/public-api`: FR-INT-01, FR-INT-02, FR-INT-06, FR-INT-07, FR-INT-08, FR-INT-09, FR-INT-10, FR-INT-11, FR-INT-13, FR-INT-14, FR-INT-16, FR-INT-17; `platform/release-and-versioning`: 1 unnumbered |
| `p1-statutory-packs` | `compliance/singapore-statutory`: 17 unnumbered; `ledger/money-and-rounding`: RND-GST-EXCL, RND-GST-INCL; `organisation/settings`: FR-ORG-05; `platform/release-and-versioning`: 1 unnumbered; `tax/statutory-parameters`: ENG-30, ENG-31, ENG-32, ENG-45<br>Screens: statutory-packs |
| `p1-users-roles` | `access/roles-and-segregation`: SOD-1, SOD-2, SOD-3, SOD-4, SOD-5, SOD-6, SOD-7, SOD-8, SOD-9, SOD-10, 62 unnumbered; `access/users-and-sign-in`: FR-ADM-01, FR-ADM-02, FR-ADM-03, FR-ADM-04, FR-ADM-07, FR-ADM-08, FR-ADM-09; `security/identity-and-access`: SEC-01, SEC-34, SEC-05, SEC-06, SEC-07, SEC-08, SEC-35<br>Screens: account-recovery, auditor-workspace, mfa-enrolment, super-admin-console, users-roles |
| `p1-approval-engine` | `access/approvals`: FR-APR-01, FR-APR-02, FR-APR-03, FR-APR-04, FR-APR-05, FR-APR-09, FR-APR-10; `access/team-modes`: FM-1, FM-2, FM-3, FM-4, FM-5, FM-6, FM-7, FM-8, FM-9, 4 unnumbered; `controls/checks`: FR-CTL-04<br>Screens: accountant-review, approval-confirmation, approval-policies, bank-details-change, payment-batch, personal-approvals, team-mode |
| `p1-app-shell` | `experience/accessibility-and-performance`: NFR-A11Y-01 WCAG 2.2 level AA, NFR-A11Y-02 Keyboard, focus and screen-reader labels, NFR-A11Y-03 Focus not obscured, NFR-A11Y-04 Dragging alternatives, NFR-A11Y-05 Accessible authentication, NFR-L10N-01 Localisation, NFR-PERF-UI-01, NFR-PERF-UI-02, NFR-PERF-UI-03; `experience/devices-and-motion`: FR-UX-37, FR-UX-38, 3 unnumbered; `experience/help`: FR-UX-30, FR-UX-31, FR-UX-32, FR-UX-33, FR-UX-34, FR-UX-36, FR-UX-41; `experience/modes`: FR-UX-23, FR-UX-24, FR-UX-25, FR-UX-26, FR-UX-27, FR-UX-29, 13 unnumbered; `experience/navigation`: FR-UX-01, FR-UX-02, FR-UX-03, FR-UX-04, FR-UX-05, FR-UX-06, FR-UX-07, FR-UX-08, FR-UX-09, FR-UX-10, FR-UX-11, FR-UX-12, FR-UX-13, FR-UX-14, FR-UX-15, FR-UX-19, FR-UX-20, FR-UX-21, FR-UX-22, FR-UX-39, FR-UX-40, 8 unnumbered; `experience/visual-design`: 13 unnumbered; `ledger/money-and-rounding`: RND-DISPLAY<br>Screens: app-shell, appearance-motion, command-palette, department-home, edit-conflict, first-run-tour, global-search, guided-setup, help-guide, inbox, invoices-list, journal-editor, my-shares, not-part-of-access, profile-preferences, shortcut-sheet, trace-panel, void-confirmation |
| `p1-documents-evidence` | `documents/attachments-and-evidence`: FR-DOC-01, FR-DOC-02, FR-DOC-04, FR-DOC-05, FR-DOC-06, FR-DOC-07, FR-DOC-08, FR-DOC-13, FR-DOC-09, FR-DOC-10, FR-DOC-11, 5 unnumbered<br>Screens: evidence-panel, evidence-search |
| `p1-templates-email` | `communication/email-and-notifications`: FR-INT-21, FR-INT-22, FR-NTF-01, FR-NTF-02, FR-NTF-03, FR-NTF-05, 2 unnumbered; `documents/templates`: FR-TPL-01, FR-TPL-02, FR-TPL-03, FR-TPL-04, FR-TPL-05, FR-TPL-07<br>Screens: announcements, banners, document-templates, notification-centre, notification-preferences, template-editor |
| `p1-general-ledger-close` | `automation/rules-and-schedules`: FR-AUT-01, FR-AUT-02; `ledger/general-ledger`: FR-GL-01, FR-GL-02, FR-GL-03, FR-GL-06, FR-GL-07, FR-GL-08, FR-GL-10, FR-GL-11, FR-GL-12, FR-GL-14; `ledger/posting-engine`: ENG-05, ENG-06; `ledger/posting-rules`: 6 unnumbered; `ledger/validation`: ENG-20, ENG-21, ENG-23; `organisation/settings`: FR-ORG-10<br>Screens: journals, opening-balances, period-close, schedules |
| `p1-multi-currency-fx` | `ledger/general-ledger`: FR-GL-04, FR-GL-05; `ledger/money-and-rounding`: RND-RATE; `ledger/multi-currency`: ENG-40, ENG-41, ENG-42, ENG-43; `ledger/posting-rules`: 1 unnumbered<br>Screens: fx-revaluation |
| `p1-sales` | `ledger/posting-rules`: 9 unnumbered; `sales/receivables`: FR-AR-01, FR-AR-02, FR-AR-03, FR-AR-04, FR-AR-05, FR-AR-06, FR-AR-07, FR-AR-08, FR-AR-09, FR-AR-18, FR-AR-10, FR-AR-17, FR-AR-16<br>Screens: customer-document-view, invoice-editor, invoice-record, quotes-list, record-payment, recurring-documents, statements-reminders |
| `p1-purchases` | `ledger/posting-rules`: 2 unnumbered; `purchases/payables`: FR-AP-01, FR-AP-02, FR-AP-03, FR-AP-06, FR-AP-09, FR-AP-19, FR-AP-13, FR-AP-20<br>Screens: bill-editor, bill-record, bills-board, purchase-orders |
| `p1-expense-claims` | `ledger/money-and-rounding`: RND-MILEAGE; `ledger/posting-rules`: 3 unnumbered; `purchases/expense-claims`: FR-AP-07, FR-AP-14, FR-AP-15, FR-AP-16, FR-AP-17, FR-AP-18, 4 unnumbered<br>Screens: claim-editor, claims-review, expense-policy, mileage-claim, my-claims, personal-claims |
| `p1-fixed-assets` | `ledger/fixed-assets`: FR-FA-01, FR-FA-02, FR-FA-03; `ledger/posting-rules`: 2 unnumbered<br>Screens: fixed-assets |
| `p1-projects` | `projects/time-and-costs`: FR-PRJ-01, FR-PRJ-02, FR-PRJ-05, FR-PRJ-06, FR-PRJ-07, FR-PRJ-08, FR-PRJ-09, FR-PRJ-10, FR-PRJ-12, 4 unnumbered<br>Screens: personal-time, project-record, project-reports, projects, timesheet |
| `p1-connector-framework` | `integrations/connectors`: FR-INT-20, FR-INT-25, FR-INT-26, FR-INT-27; `platform/local-mode`: 1 unnumbered<br>Screens: connector-catalogue, connector-detail |
| `p1-bank-import-reconcile` | `banking/bank-transactions`: FR-BNK-01, FR-BNK-02, FR-BNK-04, FR-BNK-05, FR-BNK-06, FR-BNK-07, FR-BNK-08, FR-BNK-09, FR-BNK-10, FR-BNK-11, FR-BNK-13, FR-BNK-14, FR-BNK-15, FR-BNK-16, FR-BNK-17, FR-BNK-18, 4 unnumbered; `ledger/posting-rules`: 5 unnumbered<br>Screens: add-bank-account, auto-reconciled, bank-account, bank-accounts, bank-reconciliation, bank-rules, categorise-basic, categorise-expert, statement-import |
| `p1-payments-out` | `banking/bank-transactions`: FR-BNK-12<br>Screens: payment-batches |
| `p1-payment-providers` | `banking/payment-providers`: FR-PSP-01, FR-PSP-03, FR-PSP-07, FR-PSP-04, FR-PSP-05, FR-PSP-06, 2 unnumbered; `ledger/posting-rules`: 1 unnumbered<br>Screens: provider-payouts, subscriptions |
| `p1-gst` | `compliance/compliance-center`: CMP-02; `ledger/posting-rules`: 1 unnumbered; `tax/gst-and-filings`: FR-TAX-01, FR-TAX-02, FR-TAX-03, FR-TAX-04, FR-TAX-05, FR-TAX-12, FR-TAX-07<br>Screens: gst-adjustments, gst-return, gst-returns, statutory-calendar |
| `p1-founders-money` | `equity/founders-money`: FR-EQ-01, FR-EQ-02, FR-EQ-03, FR-EQ-04, FR-EQ-05, FR-EQ-06, FR-EQ-07, FR-EQ-08, FR-EQ-09, FR-EQ-10, FR-EQ-11, FR-EQ-12, FR-EQ-14, 2 unnumbered; `ledger/posting-rules`: 7 unnumbered<br>Screens: director-accounts, director-settlement, equity-register, money-i-put-in, pre-incorporation-costs |
| `p1-equity-register` | `equity/cap-table-and-investors`: FR-EQ-15, FR-EQ-16, FR-EQ-17, FR-EQ-18, FR-EQ-19, FR-EQ-20, FR-EQ-21, FR-EQ-22, FR-EQ-25, FR-EQ-26, FR-EQ-27; `ledger/money-and-rounding`: RND-SHARE; `ledger/posting-rules`: 3 unnumbered<br>Screens: cap-table, equity-transaction, holder-record, holders, instruments, option-pool, round-simulator, share-classes |
| `p1-payroll-link` | `integrations/people-platform`: FR-PPL-01, FR-PPL-04, FR-PPL-05, FR-PPL-06, FR-PPL-07, FR-PPL-08, FR-PPL-09, FR-PPL-11, FR-PPL-12, FR-PPL-13, FR-PPL-14, FR-PPL-16, FR-PPL-17, FR-PPL-19; `ledger/posting-rules`: 3 unnumbered; `payroll/pay-runs`: FR-PAY-07, FR-PAY-11, FR-PAY-15<br>Screens: employee-record, pay-run, payroll-overview, people-hr-settings, salary-batch |
| `p1-reports` | `experience/visualisation`: FR-VIZ-01, FR-VIZ-02, FR-VIZ-03, FR-VIZ-04, FR-VIZ-05, FR-VIZ-06, FR-VIZ-07, FR-VIZ-08, 1 unnumbered; `ledger/invariants`: INV-10, INV-11; `reporting/reports`: FR-RPT-01, FR-RPT-02, FR-RPT-03, FR-RPT-05, FR-RPT-08, FR-RPT-09, FR-RPT-10<br>Screens: export-dialog, financial-statement, general-ledger, reports-index |
| `p1-dashboard-kpis` | `reporting/dashboards-and-kpis`: FR-KPI-01, FR-KPI-02, FR-KPI-03<br>Screens: home, kpis |
| `p1-planning` | `planning/budgets-and-forecasts`: FR-PLN-01, FR-PLN-02, FR-PLN-03, FR-PLN-05, FR-PLN-09, FR-PLN-06, FR-PLN-08<br>Screens: budgets, cash-flow-forecast, feasibility, forecast |
| `p1-checks` | `controls/checks`: FR-CTL-01, FR-CTL-02, FR-CTL-05, FR-CTL-06, C.1 #1, C.1 #2, C.1 #3, C.1 #4, C.1 #5, C.1 #6, C.1 #7, C.1 #8, C.1 #9, C.1 #10, C.1 #11, C.1 #12, C.1 #13, C.1 #14, C.1 #15, C.1 #16, C.1 #17, C.1 #18, C.1 #19, C.1 #20, C.1 #21, C.1 #22, C.1 #23, C.1 #24, C.1 #25, C.1 #26, C.1 #27, C.1 #28, C.1 #29, C.1 #30, C.1 #31, C.1 #32, C.1 #33, C.1 #34, C.1 #35, C.1 #36, C.1 #37; `controls/platform-checks`: C.2 #1, C.2 #2, C.2 #3, C.2 #4, C.2 #5, C.2 #6, C.2 #7, C.2 #8, C.2 #9, C.2 #10, C.2 #11, C.2 #12, C.2 #13, C.2 #14, C.2 #15, C.2 #16, C.2 #17, C.2 #18, C.2 #19, C.2 #20, C.2 #21, C.2 #22, C.2 #23, C.2 #24, C.2 #25, C.2 #26, C.2 #27, C.2 #28, C.2 #29, C.2 #30, C.2 #31, C.2 #32, C.2 #33, C.2 #34, C.2 #35, C.2 #36, C.2 #37, C.2 #38, C.2 #39, C.2 #40, C.2 #41, C.2 #42, C.2 #43, C.2 #44, C.2 #45, C.2 #46, C.2 #47, C.2 #48, C.2 #49, C.2 #50, C.2 #51, C.2 #52, C.2 #53, C.2 #54, C.2 #55; `ledger/invariants`: INV-07<br>Screens: checks |
| `p1-audit-explorer` | `controls/checks`: FR-CTL-03; `data/audit-trail`: AUD-02, AUD-03, AUD-04, AUD-05, AUD-06, AUD-07, AUD-09, 1 unnumbered<br>Screens: audit-trail |
| `p1-compliance-center` | `compliance/ai-and-api-controls`: CTL-AI-01, CTL-AI-02, CTL-AI-03, CTL-AI-04, CTL-AI-05, CTL-AI-06, CTL-AI-07, CTL-AI-08, CTL-AI-09, CTL-API-01, CTL-API-02, CTL-MCP-01, CTL-MCP-02, CTL-MCP-03, CTL-MCP-04; `compliance/ai-governance`: CMP-AI-01, CMP-AI-02, CMP-AI-03; `compliance/compliance-center`: CMP-01, CMP-03, CMP-06, CMP-07, 5 unnumbered; `compliance/financial-controls`: CTL-FIN-01, CTL-FIN-02, CTL-FIN-03, CTL-FIN-04, CTL-FIN-05, CTL-FIN-06, CTL-FIN-07, CTL-FIN-08, CTL-FIN-09, CTL-FIN-10, CTL-FIN-11, CTL-FIN-12, CTL-AUD-01, CTL-AUD-02, CTL-AUD-03, CTL-AUD-04, CTL-AUD-05, CTL-AUD-06, CTL-STAT-01, CTL-STAT-02, CTL-STAT-03, CTL-STAT-04; `compliance/technology-controls`: CTL-IAM-01, CTL-IAM-02, CTL-IAM-03, CTL-IAM-04, CTL-IAM-05, CTL-IAM-06, CTL-IAM-07, CTL-CHG-01, CTL-CHG-02, CTL-CHG-03, CTL-CHG-04, CTL-CHG-05, CTL-CHG-06, CTL-CHG-07, CTL-CHG-08, CTL-CHG-09, CTL-CHG-10, CTL-CHG-11, CTL-DP-01, CTL-DP-02, CTL-DP-03, CTL-DP-04, CTL-DP-05, CTL-DP-06, CTL-DP-07, CTL-RES-01, CTL-RES-02, CTL-RES-03, CTL-RES-04, CTL-RES-05<br>Screens: attestations, compliance-center, risk-policies |
| `p1-pdpa-retention` | `compliance/compliance-center`: CMP-05; `compliance/pdpa`: CMP-PD-01, CMP-PD-02, CMP-PD-03, CMP-PD-04, CMP-PD-05, CMP-PD-06, CMP-PD-07, CMP-PD-08, CMP-PD-09; `data/backup-and-recovery`: 7 unnumbered; `security/data-protection`: SEC-10, SEC-11, SEC-13, SEC-14<br>Screens: data-protection, privacy-notices |
| `p1-ai-providers` | `ai/model-and-provider`: AI-26, AI-27, AI-28; `ai/model-providers`: AI-70, AI-71, AI-72, AI-73, AI-74, AI-75, AI-76, AI-77, AI-78, AI-79, AI-80, AI-81, AI-82, AI-83, AI-84, AI-85, AI-86, AI-87, AI-88; `platform/local-mode`: 1 unnumbered; `platform/portability-and-cost`: 1 unnumbered<br>Screens: ai-activity-log, ai-model-routes, ai-provider-connection, ai-providers, ai-route-editor, ai-settings, ai-usage, my-ai-plan |
| `p1-ai-assistant` | `ai/data-access`: AI-01, AI-02, AI-03, AI-04, AI-68, AI-05; `ai/evaluation`: QA-AI-01, QA-AI-02, QA-AI-03, QA-AI-04, QA-AI-05, QA-AI-06, QA-AI-07; `ai/guardrails`: AI-18, AI-19, AI-20, AI-21, AI-22, AI-23, AI-24, AI-25, AI-69, 1 unnumbered; `experience/navigation`: FR-UX-17<br>Screens: ai-assistant-panel |
| `p1-knowledge-base` | `ai/knowledge-base`: AI-48, AI-49, AI-50, AI-51, AI-52, AI-53, AI-54, AI-55, AI-56, AI-57, AI-58, AI-60, 2 unnumbered; `platform/release-and-versioning`: 1 unnumbered<br>Screens: knowledge-base |
| `p1-ai-proposals` | `ai/proposals`: AI-P1, AI-P2, AI-P3, AI-P4, AI-P5, AI-P6, AI-P7, AI-P8, AI-06, AI-07, AI-08, AI-09, AI-10, AI-11, AI-12, AI-13, AI-14, AI-30, AI-31, AI-32, 6 unnumbered; `ai/task-catalogue`: 26 unnumbered; `platform/release-and-versioning`: 1 unnumbered<br>Screens: ai-proposal-review, ai-tasks |
| `p1-mcp-first-party` | `ai/mcp-and-agents`: AI-15, AI-16, AI-17, AI-34, AI-35, AI-36, AI-37, AI-38, AI-39, AI-40, AI-41, AI-42, AI-43, AI-44, AI-45, 1 unnumbered; `integrations/public-api`: FR-INT-03; `platform/local-mode`: DEV-06<br>Screens: connected-apps, oauth-consent |
| `p1-scheduled-agents` | `ai/scheduled-agents`: AI-61, AI-62, AI-63, AI-64, AI-65, AI-66, AI-67, 2 unnumbered<br>Screens: agent-run-report, company-secretary |
| `p1-company-secretary` | `governance/company-secretary`: FR-CS-01, FR-CS-02, FR-CS-03, FR-CS-04, FR-CS-05, FR-CS-06, FR-CS-08<br>Screens: corporate-profile |
| `p1-document-capture` | `documents/attachments-and-evidence`: FR-DOC-12; `purchases/payables`: FR-AP-04, FR-AP-12<br>Screens: document-inbox |
| `p1-security-hardening` | `platform/container-images`: NFR-IMG-05, NFR-IMG-07; `platform/release-and-versioning`: REL-04, REL-05; `security/application-security`: SEC-22, SEC-23, SEC-24, SEC-36, SEC-37, SEC-26; `security/detection-and-response`: SEC-30, SEC-31, SEC-32, SEC-33 |
| `p1-operations-dr` | `data/audit-trail`: 1 unnumbered; `data/backup-and-recovery`: DR-01, DR-02, DR-03, DR-04, DR-06, DR-07; `platform/operations`: NFR-DR-01, NFR-DR-02, NFR-DR-03, NFR-RET-01, NFR-OBS-01, NFR-OBS-02; `platform/performance-and-scale`: NFR-PERF-01, NFR-PERF-06, NFR-PERF-02, NFR-PERF-03, NFR-PERF-04, NFR-PERF-05, NFR-SCALE-01, NFR-AVAIL-01, NFR-BROWSER-01; `platform/portability-and-cost`: NFR-COST-02, NFR-COST-01, NFR-COST-03, NFR-COST-04<br>Screens: operations-tray |
| `p1-workbook-migration` | `data/migration`: FR-MIG-01, FR-MIG-02, FR-MIG-03, FR-MIG-04, 4 unnumbered<br>Screens: data-export, workbook-import |
| `p1-launch-readiness` | `quality/calculation-correctness`: QA-ENG-01, QA-ENG-02, QA-ENG-03, QA-ENG-04, QA-ENG-05, QA-ENG-06, QA-ENG-07, QA-ENG-08, QA-RPT-01, QA-RPT-02, QA-RPT-03, QA-RPT-04, QA-MIG-01, QA-PROD-01, QA-REL-01, 13 unnumbered; `quality/test-gates`: QA-API-01, QA-API-02, QA-API-03, QA-API-04, QA-MCP-01, QA-LOCAL-01, QA-IMG-01, QA-UI-01, QA-UI-02, QA-UI-03, QA-UI-04, QA-UI-05, QA-UI-06, QA-UI-07, QA-UI-08, QA-UI-09, QA-TRACE-01, QA-SEC-03, QA-SEC-04, QA-SEC-05, QA-E2E-01 End-to-end journeys, QA-PERF-01 |
| `cloud-first-deployment` | — |
| `p1-go-live` | `data/migration`: 2 unnumbered |
| `p1-nemura-platform` | `people/experience`: NM-ROLE-01, NM-ROLE-02, NM-ROLE-03, NM-ROLE-04, NM-ROLE-05, NM-UX-01, NM-UX-02, NM-UX-03, NM-UX-04, NM-UX-05, NM-UX-06, 1 unnumbered; `people/platform`: NM-ARC-01, NM-ARC-02, NM-ARC-03, NM-ARC-04, NM-ARC-05, NM-ARC-06, NM-NFR-01, NM-NFR-02, NM-NFR-03, NM-NFR-04, NM-NFR-05, 1 unnumbered; `people/security`: NM-SEC-01, NM-SEC-02, NM-SEC-03, NM-SEC-04, NM-SEC-05, NM-SEC-07<br>Screens: nm-access-request, nm-my-pay, nm-sign-in |
| `p1-nemura-records-packs` | `people/checks`: N.1 #1, N.1 #2, N.1 #3, N.1 #4, N.1 #5, N.1 #6, N.1 #7, N.1 #8, N.1 #9, N.1 #10, N.1 #11, N.1 #12, N.1 #13, N.1 #14, N.1 #15; `people/features-and-packs`: NM-FEAT-01, NM-FEAT-02, NM-FEAT-03, NM-FEAT-04, NM-FEAT-05, NM-PACK-01, NM-PACK-02, NM-PACK-03, NM-PACK-04, NM-PACK-05, NM-PACK-06, NM-PACK-07, NM-PACK-08, 1 unnumbered; `people/records`: NM-PPL-01, NM-PPL-02, NM-PPL-03, NM-PPL-04, NM-PPL-05, NM-PPL-06, NM-PPL-07, NM-PPL-08<br>Screens: nm-country-pack, nm-features, nm-leave-policy, nm-people, nm-person |
| `p1-nemura-leave-time` | `people/leave`: NM-LV-01, NM-LV-02, NM-LV-03, NM-LV-04, NM-LV-05, NM-LV-06, NM-LV-07, NM-LV-08, NM-LV-09, NM-LV-10, NM-LV-11, NM-LV-12, NM-LV-13, NM-LV-14; `people/time`: NM-TIM-01, NM-TIM-02, NM-TIM-04<br>Screens: nm-apply-leave, nm-govt-claims, nm-my-leave, nm-people-home, nm-report-sick, nm-team-leave |
| `p1-nemura-self-service-letters` | `people/letters`: NM-LTR-01, NM-LTR-02, NM-LTR-03, NM-LTR-04, NM-LTR-06, NM-LTR-07; `people/onboarding`: NM-ONB-02, NM-ONB-03; `people/self-service`: NM-ESS-01, NM-ESS-02, NM-ESS-03, NM-ESS-04, NM-ESS-05, NM-ESS-06<br>Screens: nm-leaver-home, nm-letter-template, nm-letters, nm-my-home, nm-my-requests, nm-onboarding, nm-request-letter |
| `p1-nemura-payroll` | `ledger/money-and-rounding`: RND-CPF, RND-CPF-GRAD, RND-SDL; `ledger/posting-rules`: 2 unnumbered; `payroll/employees`: FR-EMP-01, FR-EMP-02; `payroll/pay-runs`: FR-PAY-01, FR-PAY-02, FR-PAY-03, FR-PAY-04, FR-PAY-05, FR-PAY-06, FR-PAY-14, FR-PAY-16, FR-PAY-17, FR-PAY-18, FR-PAY-13, 1 unnumbered; `people/payroll`: NM-PAY-01, NM-PAY-02, NM-PAY-03, NM-PAY-04, NM-PAY-05, NM-PAY-06, NM-PAY-07, NM-PAY-08, NM-PAY-09, NM-PAY-10, NM-PAY-11<br>Screens: employees, nm-checks, nm-payroll-hr, nm-payrun-approval, nm-payslip |
| `p1-nemura-fyniq-link` | `integrations/people-platform`: FR-PPL-02, FR-PPL-03, FR-PPL-10, FR-PPL-15, FR-PPL-18; `people/fyniq-integration`: NM-INT-01, NM-INT-02, NM-INT-03, NM-INT-04, NM-INT-05, NM-INT-06, NM-INT-07, NM-INT-08, NM-INT-09, NM-INT-10, NM-INT-11, NM-INT-12, NM-INT-13, 1 unnumbered<br>Screens: nm-fyniq-link |
| `p1-nemura-assistant-external` | `people/assistant`: NM-AI-01, NM-AI-02, NM-AI-03, NM-AI-04, NM-AI-06, people.draft_letter, people.draft_fwa_reply; `people/external-systems`: NM-EXT-01, NM-EXT-02, NM-EXT-03<br>Screens: hr-import, nm-ask, nm-assistant |
| `p2-invoicenow` | `purchases/payables`: FR-AP-05; `sales/receivables`: FR-AR-12; `tax/gst-and-filings`: FR-TAX-10, FR-TAX-11<br>Screens: invoicenow-submissions |
| `p2-bank-feeds-payments` | `banking/bank-transactions`: FR-BNK-03; `banking/payment-providers`: FR-PSP-02; `purchases/expense-claims`: FR-AP-08; `sales/receivables`: FR-AR-13 |
| `p2-tax-filings` | `sales/receivables`: FR-AR-11; `tax/gst-and-filings`: FR-TAX-06; `tax/statutory-parameters`: ENG-33<br>Screens: bad-debt-relief, filing-packs |
| `p2-payroll-outputs` | `payroll/pay-runs`: FR-PAY-08, FR-PAY-09, FR-PAY-10<br>Screens: ir21-clearance |
| `p2-reporting-planning` | `equity/cap-table-and-investors`: FR-EQ-28, FR-EQ-23, FR-EQ-24, FR-EQ-29; `equity/founders-money`: FR-EQ-13; `experience/visualisation`: FR-VIZ-09; `ledger/chart-of-accounts`: FR-DIM-03; `ledger/fixed-assets`: FR-FA-04; `planning/budgets-and-forecasts`: FR-PLN-04, FR-PLN-07; `reporting/dashboards-and-kpis`: FR-KPI-04, FR-KPI-05, FR-KPI-06, FR-KPI-07; `reporting/reports`: FR-RPT-04, FR-RPT-06, FR-RPT-07, FR-RPT-13<br>Screens: board-pack, board-portal, dashboards, exit-waterfall, investor-updates, management-report, report-builder |
| `p2-customer-portal-subscriptions` | `projects/time-and-costs`: FR-PRJ-11; `sales/receivables`: FR-AR-14, FR-AR-15<br>Screens: customer-portal |
| `p2-api-third-parties` | `ai/mcp-and-agents`: AI-46; `communication/email-and-notifications`: FR-NTF-04; `integrations/connectors`: FR-INT-23, FR-INT-24; `integrations/public-api`: FR-INT-04, FR-INT-12, FR-INT-15, FR-INT-18<br>Screens: routing-rules, webhooks |
| `p2-mobile-approvals-automation` | `access/approvals`: FR-APR-06, FR-APR-07, FR-APR-08; `access/users-and-sign-in`: FR-ADM-05; `automation/rules-and-schedules`: FR-AUT-03, FR-AUT-04; `compliance/compliance-center`: CMP-04; `documents/templates`: FR-TPL-06; `experience/help`: FR-UX-35; `experience/modes`: FR-UX-28; `experience/navigation`: FR-UX-16; `governance/company-secretary`: FR-CS-07; `ledger/general-ledger`: FR-GL-09; `purchases/payables`: FR-AP-10, FR-AP-21<br>Screens: supplier-contracts |
| `p2-ai-expansion` | `ai/knowledge-base`: AI-59; `ai/proposals`: AI-33 |
| `p2-security-certification` | `security/application-security`: SEC-25 |
| `p2-nemura-people` | `people/assistant`: NM-AI-05, people.draft_job_post, people.draft_interview_kit, people.draft_review_summary; `people/external-systems`: NM-EXT-04, NM-EXT-05; `people/letters`: NM-LTR-05; `people/onboarding`: NM-ONB-01; `people/performance`: NM-PRF-01, NM-PRF-02, NM-PRF-03, NM-PRF-04, NM-PRF-05; `people/recruitment`: NM-REC-01, NM-REC-02, NM-REC-03, NM-REC-04, NM-REC-05, NM-REC-06; `people/security`: NM-SEC-06<br>Screens: nm-goals, nm-hiring, nm-verify-letter |
| `p3-multi-entity` | `experience/navigation`: FR-UX-18; `ledger/general-ledger`: FR-GL-13; `organisation/multi-entity`: FR-ENT-01, FR-ENT-02, FR-ENT-03; `reporting/reports`: FR-RPT-12<br>Screens: consolidation, entity-setup, practice-dashboard |
| `p3-saas-platform` | `ai/mcp-and-agents`: AI-47; `communication/email-and-notifications`: FR-NTF-06; `data/backup-and-recovery`: DR-05; `integrations/connectors`: FR-INT-28; `integrations/public-api`: FR-INT-05, FR-INT-19; `master-data/items`: FR-ITM-02; `platform/performance-and-scale`: NFR-SCALE-02; `platform/portability-and-cost`: NFR-PORT-06; `reporting/dashboards-and-kpis`: FR-KPI-08 |
| `p3-countries-certification` | `compliance/pdpa`: CMP-PD-10; `documents/templates`: FR-TPL-08; `people/time`: NM-TIM-03; `purchases/payables`: FR-AP-11; `reporting/reports`: FR-RPT-11; `tax/gst-and-filings`: FR-TAX-08, FR-TAX-09<br>Screens: statement-notes |
