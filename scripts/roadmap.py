#!/usr/bin/env python3
"""The P1–P3 build plan's tables (docs/prd/plans/p1-p3-build-plan-2026-10-16.md).

The plan assigns every requirement in `openspec/specs/` to exactly one OpenSpec change, and places
every designed screen after P0 with the change that owns most of its requirements. The changes, the
capability defaults and the exceptions by ID or title are the data below; the plan's change tables,
coverage table and counts are generated from them into the marked regions of the plan
(`<!-- roadmap:NAME -->` … `<!-- /roadmap -->`). The prose around them is edited by hand. Standard
library only.

    python3 scripts/roadmap.py check   # exit 1 on an unplaced requirement or screen, or a stale region (make roadmap-check)
    python3 scripts/roadmap.py write   # rewrite the marked regions (make roadmap)

A requirement added to the PRD lands in its capability's default change; a P2 or P3 row, or a row
that belongs elsewhere, needs its ID in LATER or BY_ID, and the check names it until it has one.
"""
import collections
import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
SPECS = ROOT / "openspec/specs"
INVENTORY = ROOT / "docs/design/screens/inventory.json"
PLAN = ROOT / "docs/prd/plans/p1-p3-build-plan-2026-10-16.md"
REGION = re.compile(r"<!-- roadmap:([A-Za-z0-9 _-]+?) -->(.*?)<!-- /roadmap -->", re.S)
EMPTY_OK = {"cloud-first-deployment"}  # holds P0's cloud tasks, not requirements of its own

# ---------------------------------------------------------------- changes
# (id, track, wave, title, depends on, local-first note, outside dependency, reviewer, launch proposal)
C = []


def ch(cid, track, wave, title, deps, local, outside, reviewer, launch):
    C.append(dict(id=cid, track=track, wave=wave, title=title, deps=deps, local=local,
                  outside=outside, reviewer=reviewer, launch=launch))


F1, N1, CG, F2, N2, F3 = "P1 Fyniq", "P1 Nemura", "Cloud gate", "P2", "P2 Nemura", "P3"
# Wave 1: foundations every module needs
ch("p1-organisation-chart", F1, 1, "Organisation profile, financial year, GST status, defaults, feature switches, logo and brand; the chart of accounts and dimensions; contacts with vendor bank details as high-risk data; items; the logical data model", ["P0"], "—", "—", "Accountant (chart)", "Launch")
ch("p1-public-api", F1, 1, "The /v1 conventions claimed as requirements (versioning, problem details, idempotency, concurrency, lists, long and bulk operations, preview before commit, rate limits), machine-readable contracts and the TypeScript SDK; every later change adds its operations here first", ["P0"], "—", "—", "—", "Launch")
ch("p1-statutory-packs", F1, 1, "Effective-dated statutory packs with source URLs and pinned tests (GST rates and codes, CPF, SDL, SHG, FWL, deadlines), the tax point, GST rounding, dual approval before activation, and the §14.2 obligations they serve", ["p1-organisation-chart"], "—", "—", "Tax agent and accountant (#5)", "Launch")
ch("p1-users-roles", F1, 1, "Invitations, the finance roles and combinations of Appendix D, MFA with TOTP and passkey step-up, sessions, external users, OAuth clients, Super admin scope and succession, account recovery; the permission matrix of Appendix D, closed row by row as each module arrives", ["P0"], "Local sign-in page; Identity Platform through its fake", "—", "Security (#4), co-founder (#6)", "Launch")
ch("p1-approval-engine", F1, 1, "Approval policies per object type, signed approval records, step-up, expiry, approval-gated policy changes; the team modes and the founder-mode rules FM-1…9 with thresholds and break-glass", ["p1-users-roles"], "—", "—", "Co-founder (#6)", "Launch")
ch("p1-app-shell", F1, 1, "Sidebar and workspaces by role, command palette, global search, Inbox, drill-down, side peeks, grids, record pages, comments, live updates, the clipboard rules, help registry, tooltips, user guide, first-run walkthrough, the FR-UX-41 banner, Basic and Expert modes with the feature registry, themes, accessibility, devices and motion", ["p1-users-roles"], "—", "—", "—", "Launch")
ch("p1-documents-evidence", F1, 1, "Attachments on any record, retention-locked storage, issued documents kept with their versions, evidence rules and defaults, evidence integrity, safe files, two-phase upload, evidence search and export, evidence through the API", ["p1-organisation-chart"], "Local blob directory and fake GCS; the retention lock itself is proved at the cloud gate", "—", "—", "Launch")
ch("p1-templates-email", F1, 1, "Document templates as open question 44 decided (starter gallery, theme, locked IRAS blocks, immutable versions), the render service, outbound email through ZeptoMail with the Resend failover, inbound capture through Resend, the notification centre, preferences, banners and one event stream", ["p1-documents-evidence"], "Mailpit; the ZeptoMail and Resend adapters against fakes", "A sending domain; ZeptoMail and Resend accounts", "—", "Launch")
# Wave 2: the ledger and documents
ch("p1-general-ledger-close", F1, 2, "Manual journals in full, recurring and auto-reversing journals, schedules, opening balances, period close and lock, year-end, the GL report, flux analysis, the scheduler and system postings, versioned posting rules, the remaining invariants and validation rules; the posting conventions", ["p1-organisation-chart", "p1-statutory-packs", "p1-approval-engine"], "—", "The accountant confirms the framework (Q31)", "Accountant", "Launch")
ch("p1-multi-currency-fx", F1, 2, "FX rates and the rate cascade, realised and unrealised FX, GST on foreign-currency documents, foreign-currency bank accounts, month-end revaluation with automatic reversal", ["p1-general-ledger-close"], "Rates from a file or fixture; the live rate source is configured at the cloud gate", "The accountant confirms the tolerances (Q32)", "Accountant", "Launch")
ch("p1-sales", F1, 2, "Quotes and orders, conversion, invoices, credit notes and refunds, tax-invoice particulars, branded PDFs and sending, recurring invoices, reminders and statements, deposits, allocations, write-offs, AR ageing; their posting rules", ["p1-general-ledger-close", "p1-templates-email"], "—", "—", "Accountant", "Launch")
ch("p1-purchases", F1, 2, "Purchase orders, bills, vendor credits, recurring bills, track and pay, AP ageing, input-tax eligibility, non-resident payments; their posting rules", ["p1-general-ledger-close", "p1-documents-evidence"], "—", "—", "Accountant; co-founder for payee changes (#6)", "Launch")
ch("p1-expense-claims", F1, 2, "Expense claims, My claims, mileage, employee claims processing, the expense policy, company-card imports and cash advances; their posting rules", ["p1-purchases"], "—", "—", "—", "Launch (owner, 26 Oct 2026)")
ch("p1-fixed-assets", F1, 2, "Asset register, depreciation by the scheduler, disposals", ["p1-purchases"], "—", "—", "Accountant", "Launch")
ch("p1-projects", F1, 2, "Projects, timesheets, time entry, cost and billing rates, approval, costs, budgets, billing methods and project reports (feature-switched)", ["p1-sales", "p1-purchases"], "—", "—", "—", "After go-live (owner, 26 Oct 2026: displaced by expense claims and payment files)")
# Wave 3: money in and out
ch("p1-connector-framework", F1, 3, "The integration framework, connector manifests, lifecycle and rules (FR-INT-20, 25…27)", ["P0"], "Every connector is tested against a fake of its service", "—", "Security (#4)", "Launch")
ch("p1-bank-import-reconcile", F1, 3, "Money accounts, statement import (CSV, XLSX, OFX, MT940, camt.053), duplicates, rules, categorising, matching, settlement differences, transfers, reconciliation with sign-off, suspense, daily cash, deterministic auto-reconcile, DBS emailed reports and the Wise and Airwallex sync", ["p1-sales", "p1-purchases", "p1-connector-framework"], "Statement files; DBS reports through Mailpit; Wise and Airwallex against fakes", "A redacted DBS IDEAL daily report; the accountant confirms the cross-currency transfer rule (accounts held: DBS, Wise, Airwallex, rev 0.140)", "Co-founder (#6)", "Launch")
ch("p1-payments-out", F1, 3, "Payment batches as bank files with dual-controlled release and remittance advice", ["p1-bank-import-reconcile", "p1-approval-engine"], "Files only", "—", "Co-founder (#6): money movement", "Launch (owner, 26 Oct 2026)")
ch("p1-payment-providers", F1, 3, "Stripe (Model B) and HitPay (Model A's PayNow QR) as read-only connectors: events and postings, refunds and disputes, fees and their GST, payout reconciliation; the provider sale, fee and payout posting rule", ["p1-connector-framework", "p1-sales"], "Stripe and HitPay test-mode fixtures and fakes", "Stripe and HitPay accounts", "Accountant", "After go-live: neither carries a go-live transaction (owner, 26 Oct 2026)")
# Wave 4: tax, equity and payroll postings
ch("p1-gst", F1, 4, "The GST F5 return (recorded filing), IRAS audit listings, the registration monitor, the statutory calendar and GST corrections", ["p1-sales", "p1-purchases", "p1-statutory-packs"], "—", "GST registration (not registered for now, Q2)", "Tax agent (#5)", "Monitor and calendar at launch; F5 at registration (Q2)")
ch("p1-founders-money", F1, 4, "Director accounts, costs paid personally, pre-incorporation costs, founder pay choice, settlement routes, conversion to share capital, waiver, the register of share issues, ACRA steps, presentation and year-end confirmation; their posting rules", ["p1-general-ledger-close", "p1-purchases"], "—", "A board minute on the founders' loans; the accountant confirms (Q15)", "Accountant", "Launch")
ch("p1-equity-register", F1, 4, "Holders, share classes, equity transactions, SAFEs and notes, the option-pool register, the cap table at any date, round modelling, investor records and linked statutory checks; share-issue and conversion posting rules", ["p1-founders-money"], "—", "A lawyer confirms the SAFE's conversion terms (Q33)", "Accountant", "Launch")
ch("p1-payroll-link", F1, 4, "Fyniq's side of payroll: the people-platform contract (FR-PPL), verifying, posting and paying pay runs from Nemura, a provider's journal or manual entry (FR-PAY-15), salary bank details by version, tax-clearance holds, and the payroll posting rules", ["p1-general-ledger-close", "p1-payments-out", "p1-nemura-payroll (the Nemura route only)"], "Nemura and a provider journal file, both local", "The accountant confirms the SDL difference (Q27)", "Accountant (#5)", "After go-live (Q4, Q16)")
# Wave 5: reports, planning, checks and compliance
ch("p1-reports", F1, 5, "Financial statements, periods and comparatives, management reports, exports, reproducible reports, asynchronous exports, accountant exports, the charts and the remaining statement invariants", ["p1-general-ledger-close"], "—", "The accountant confirms the framework (Q31)", "Accountant", "Launch")
ch("p1-dashboard-kpis", F1, 5, "The default Home and Dashboard and the workbook KPI set as versioned definitions", ["p1-reports"], "—", "—", "—", "Launch")
ch("p1-planning", F1, 5, "Budgets, the driver-based forecast and its outputs, the 13-week and short-term cash forecasts, the feasibility calculator, sandboxed forecasts", ["p1-reports"], "—", "—", "—", "Launch")
ch("p1-checks", F1, 5, "The checks engine, books health and the user access review report; the workbook checks (Appendix C.1) and platform checks (C.2), each closed as its module arrives", ["p1-reports"], "—", "—", "Accountant", "Launch")
ch("p1-audit-explorer", F1, 5, "Sensitive values in the trail, period-close snapshots, logged sensitive reads and exports, audit retention, the audit explorer and export, purge and restore that keep the chain", ["p1-general-ledger-close"], "—", "—", "Security (#4)", "Launch")
ch("p1-compliance-center", F1, 5, "The control dashboard, statutory calendar view, attestations, PDPA registers, risk register, policy library; the control rows of Appendix F (F.1, F.3, F.7), each closed when its check runs, and AI governance", ["p1-checks"], "Evidence from local runs; cloud evidence at the cloud gate", "—", "Security (#4)", "Launch")
ch("p1-pdpa-retention", F1, 5, "The DPO, data inventory, notices and consent, access and correction requests, breach timers, transfers, NRIC handling, DPIAs; encryption, application-level encryption, residency, minimisation; retention per data class with crypto-shredding", ["p1-documents-evidence", "p1-audit-explorer"], "—", "Counsel confirms before payroll data is loaded (Q39)", "Security (#4)", "Launch (before payroll data)")
# Wave 6: AI and MCP (security boundary)
ch("p1-ai-providers", F1, 6, "One model port, provider connectors and capabilities, routes per agent, data handling per provider, failover, usage and budgets, qualification, model lists and prices from their sources", ["p1-connector-framework"], "Replay mode offline; live local tests through DEV-09's adapters or an API key", "A developer's own Claude or ChatGPT plan, or an API key, for live local tests (DEV-09)", "Security (#4)", "After go-live")
ch("p1-ai-assistant", F1, 6, "On-behalf-of read tools, exclusions, the semantic layer, result limits, typed facts, guardrails against the OWASP LLM risks, the eval suites, Ask, explain and simulate", ["p1-ai-providers", "p1-reports"], "Evals in replay; live runs with the local AI provider", "—", "Security (#4)", "After go-live")
ch("p1-knowledge-base", F1, 6, "The organisation knowledge base from trusted sources, context assembly, compile, index and lint, trust, versioning and publishing", ["p1-ai-assistant"], "—", "—", "Security (#4)", "After go-live")
ch("p1-ai-proposals", F1, 6, "The proposal principles, the task catalogue of Appendix E, approval of AI work, numeric grounding, idempotent execution through the executor, undo, kill switches and budgets, the red-team cases", ["p1-ai-assistant", "p1-approval-engine"], "—", "—", "Security (#4)", "After go-live")
ch("p1-mcp-first-party", F1, 6, "The MCP server for first-party clients: client classes, off by default, audience-bound tokens with option (b), consent and scopes, transport, the tool surface, limits, audit and detection; the local MCP endpoint; QA-MCP-01", ["p1-ai-proposals"], "Option (b) with a local key; Cloud KMS at the cloud gate", "Security review #4", "Security (#4)", "After go-live")
ch("p1-scheduled-agents", F1, 6, "Agent skills, schedules and triggers, identity and permissions, deterministic first, outputs, the never-list, budgets and failure", ["p1-ai-assistant"], "—", "—", "Security (#4)", "After go-live")
ch("p1-company-secretary", F1, 6, "The corporate profile, ACRA pack, cross-checks, the company secretary workspace and role, document drafts, the named human secretary, and the AI secretary's scheduled checks", ["p1-scheduled-agents", "p1-equity-register"], "—", "A corporate secretarial firm appointed (Q19)", "Accountant", "Decide (Q25)")
ch("p1-document-capture", F1, 6, "Smart document capture (upload, paste, email-in), bill capture and automated bill entry, as drafts a person confirms", ["p1-ai-providers", "p1-purchases", "p1-templates-email"], "Inbound email through Mailpit", "—", "Security (#4)", "After go-live")
# Wave 7: platform, migration and readiness
ch("p1-security-hardening", F1, 7, "Web protections, uploads, outbound requests to configured hosts, registered private endpoints, dependency hygiene, SLSA provenance and reproducible builds, security monitoring, alerting and the incident runbook, release review and recorded versions", ["P0"], "Local logs and alerts; Binary Authorization and cloud posture at the cloud gate", "—", "Security (#4)", "Launch")
ch("p1-operations-dr", F1, 7, "Observability and the scheduler heartbeat, the performance budgets, volume, availability and browsers, cost targets and limits, backup, PITR, DR copy, restore drills and attachment storage", ["P0"], "Performance budgets locally; backups, restores and observability at the cloud gate", "—", "—", "Launch")
ch("p1-workbook-migration", F1, 7, "Workbook import, import reconciliation, generic import and full tenant export; the M1…M6 migration steps rehearsed on the synthetic sample workbook", ["p1-sales", "p1-purchases", "p1-bank-import-reconcile", "p1-founders-money"], "The synthetic sample workbook only: real company data never comes to a laptop (DEV-08)", "The accountant confirms the framework (Q31) and the founders' balances (Q15)", "Accountant", "Launch")
ch("p1-launch-readiness", F1, 7, "The quality gates as requirements (calculation correctness, test gates), the trace gate against the frozen launch subset, the acceptance scenarios of Appendix G and the UAT scripts", ["p1-workbook-migration"], "—", "The launch rows left to decide (Q25)", "—", "Launch")
# Cloud gate
ch("cloud-first-deployment", CG, 8, "Everything held for the cloud: the founder's Google Cloud organisation and the first apply (infra/README.md), P0's staging tasks (exit items 3, 4 and 7), the live adapters (Identity Platform, Cloud Tasks, KMS, Cloud Storage with the lock), Binary Authorization, observability, backups and restore drills, the email domain, Zoho live", ["p1-launch-readiness"], "—", "Google Cloud organisation and billing", "Security (#4)", "Launch")
ch("p1-go-live", CG, 9, "UAT, the migration with real data in production (never on a laptop), the parallel run (M5), go-live and the workbook's archive (M6), the release record", ["cloud-first-deployment"], "—", "Founders, accountant", "Accountant", "Launch")
# Nemura P1
ch("p1-nemura-platform", N1, 1, "Nemura's own image, database, web app and token issuer (ADR-N1), its non-functional rules, inherited controls, need to know, PDPA and records, roles, the permission matrix, and the experience", ["P0"], "Local sign-in page; one Identity Platform tenant per organisation at the cloud gate", "—", "Security (#4)", "After go-live (Nemura Q12)")
ch("p1-nemura-records-packs", N1, 2, "People records, the Singapore pack with verified values, feature switches and the people checks (N.1)", ["p1-nemura-platform", "p1-statutory-packs"], "—", "—", "HR, accountant", "After go-live (Nemura Q12)")
ch("p1-nemura-leave-time", N1, 3, "Leave types, entitlements, the leave ledger, applying and approving, calendars, medical certificates, government-paid leave, flexible-work requests; working patterns, Part IV overtime and hours worked; the month-end leave liability accrual (Nemura Q4)", ["p1-nemura-records-packs"], "—", "The statutory leave minimum verified in the Singapore pack (Nemura Q3)", "HR", "After go-live (Nemura Q12)")
ch("p1-nemura-self-service-letters", N1, 3, "Self-service, letters, templates and KETs issued in two steps, what an issued letter proves, accounts for joiners and offboarding with final pay and IR21", ["p1-nemura-records-packs"], "—", "—", "HR", "After go-live (Nemura Q12)")
ch("p1-nemura-payroll", N1, 4, "Pay runs with the full CPF, SDL, SHG and FWL rules (PRD-001 §8.7 in Nemura, ADR-N2), overrides, year to date, approval, salary deduction limits, wage classes, payslips in Nemura, run and payment states and corrections; employee records; the oracle's half by the oracle session", ["p1-nemura-leave-time", "p1-nemura-self-service-letters"], "—", "Counsel on retention (Q39); two months of parallel comparison (Nemura Q13)", "Accountant, payroll administrator (#5)", "After go-live (Q4, Nemura Q12)")
ch("p1-nemura-fyniq-link", N1, 4, "The cross-app contracts and events, one sign-in and the workspace switch, segregation across both apps; paired with p1-payroll-link", ["p1-nemura-payroll", "p1-users-roles"], "Both apps in one local stack", "—", "Security (#4)", "After go-live (Nemura Q12)")
ch("p1-nemura-assistant-external", N1, 5, "The people assistant (questions and letter drafts, never-list, own pay), Fyniq without Nemura, the payroll file import and one external connector, imported journals as drafts", ["p1-nemura-payroll", "p1-ai-assistant"], "Connector against a fake", "—", "Security (#4)", "After go-live (Nemura Q12)")
# P2
ch("p2-invoicenow", F2, 1, "InvoiceNow (Peppol) sending and receiving, the submission population and the filing gate", ["p1-gst"], "Against an access point's sandbox", "A Peppol access point; Q2", "Tax agent", "—")
ch("p2-bank-feeds-payments", F2, 1, "Bank feeds, bank APIs and host-to-host, DBS RAPID or IDEAL Connect, card feeds, payment links (Model A), online payment, corporate cards", ["p1-bank-import-reconcile", "p1-payment-providers"], "Against sandboxes", "Q24; a licensed feed provider; HitPay", "Co-founder", "—")
ch("p2-tax-filings", F2, 1, "Corporate income tax support, F7 and F8, IRAS API filing, published pack updates, bad-debt relief with GST", ["p1-gst"], "Against IRAS's sandbox", "IRAS API access", "Tax agent", "—")
ch("p2-payroll-outputs", F2, 1, "Payslip documents, the CPF submission file, IR8A and the Auto-Inclusion Scheme (in Nemura where it runs payroll)", ["p1-nemura-payroll"], "—", "CPF Board and IRAS formats", "Payroll administrator", "—")
ch("p2-reporting-planning", F2, 2, "Report builder, board pack, scheduled reports, deferral roll-forward, configurable dashboards, customer counts, anomaly alerts, industry benchmarks, rolling forecast, the funding plan, investor updates, access, obligations and the exit waterfall, shares for services, allocation rules, asset reports", ["p1-reports", "p1-planning", "p1-equity-register"], "—", "—", "Accountant", "—")
ch("p2-customer-portal-subscriptions", F2, 2, "The customer portal, subscription revenue schedules, disputes and promise-to-pay, revenue over time for fixed-fee projects", ["p1-sales", "p1-projects"], "—", "—", "—", "—")
ch("p2-api-third-parties", F2, 2, "Third-party API clients, webhooks and events, the semantic metrics endpoint, the sandbox, Go and Python SDKs, third-party MCP clients, ticketing connectors, Slack and Zoho Cliq", ["p1-public-api", "p1-mcp-first-party"], "—", "Q22", "Security", "—")
ch("p2-mobile-approvals-automation", F2, 2, "The PWA with offline reads and camera capture, approval delegation, escalation, approvals by email and PWA, workflow rules without code, outbound webhooks, soft locks, vendor statements, supplier contracts, the usage view, pinned Basic mode, tours, code-mode templates and the template editor's layout, column chooser, conditional blocks and visual diff (the P2 halves of FR-TPL-01, 02 and 04), the annual-return pack, evidence export", ["p1-app-shell", "p1-approval-engine"], "—", "—", "Co-founder", "—")
ch("p2-ai-expansion", F2, 3, "Approval-quality metrics and more task types, knowledge pages from documents, document AI drafts", ["p1-ai-proposals", "p1-knowledge-base"], "—", "—", "Security", "—")
ch("p2-security-certification", F2, 3, "Penetration testing and DAST; Cyber Trust or ISO 27001 readiness", ["p1-security-hardening"], "—", "Q8", "Security", "—")
ch("p2-nemura-people", N2, 1, "Hiring, performance and KPIs, onboarding plans, letter verification, grievances, the unified HR API and pay documents from external systems, fairness in the assistant", ["p1-nemura-assistant-external"], "—", "—", "HR", "—")
# P3
ch("p3-multi-entity", F3, 1, "Several legal entities, intercompany and consolidation, accountant-practice mode, the entity switcher, consolidated reporting", ["p2-reporting-planning"], "—", "Q1", "Accountant", "—")
ch("p3-saas-platform", F3, 1, "Tenant onboarding at scale, import from other accounting systems, agent-to-agent, outbound agents, third-party connectors, ask from chat, peer benchmarks, price lists, the vault for backups, a portability rehearsal", ["p2-api-third-parties"], "—", "Q1", "Security", "—")
ch("p3-countries-certification", F3, 1, "The ACRA filing pack and country packs, statements in the recorded framework, withholding tax, template languages, the Data Protection Trustmark, SOC 2 and ISO 27001 certification; Nemura's time and attendance and Malaysia pack", ["p2-tax-filings"], "—", "Q8", "Tax agent", "—")

IDS = {c["id"] for c in C}

# ---------------------------------------------------------------- assignment
CAP_P1 = {
    "access/approvals": "p1-approval-engine", "access/users-and-sign-in": "p1-users-roles",
    "access/roles-and-segregation": "p1-users-roles", "access/team-modes": "p1-approval-engine",
    "ai/data-access": "p1-ai-assistant", "ai/evaluation": "p1-ai-assistant", "ai/guardrails": "p1-ai-assistant",
    "ai/knowledge-base": "p1-knowledge-base", "ai/mcp-and-agents": "p1-mcp-first-party",
    "ai/model-and-provider": "p1-ai-providers", "ai/model-providers": "p1-ai-providers",
    "ai/proposals": "p1-ai-proposals", "ai/task-catalogue": "p1-ai-proposals", "ai/scheduled-agents": "p1-scheduled-agents",
    "automation/rules-and-schedules": "p1-general-ledger-close", "banking/bank-transactions": "p1-bank-import-reconcile",
    "banking/payment-providers": "p1-payment-providers", "communication/email-and-notifications": "p1-templates-email",
    "compliance/ai-governance": "p1-compliance-center", "compliance/compliance-center": "p1-compliance-center",
    "compliance/pdpa": "p1-pdpa-retention", "compliance/financial-controls": "p1-compliance-center",
    "compliance/technology-controls": "p1-compliance-center", "compliance/ai-and-api-controls": "p1-compliance-center",
    "compliance/singapore-statutory": "p1-statutory-packs",
    "controls/checks": "p1-checks", "controls/platform-checks": "p1-checks",
    "data/audit-trail": "p1-audit-explorer", "data/backup-and-recovery": "p1-operations-dr", "data/migration": "p1-workbook-migration",
    "data/data-model": "p1-organisation-chart",
    "documents/attachments-and-evidence": "p1-documents-evidence", "documents/templates": "p1-templates-email",
    "equity/cap-table-and-investors": "p1-equity-register", "equity/founders-money": "p1-founders-money",
    "experience/accessibility-and-performance": "p1-app-shell", "experience/devices-and-motion": "p1-app-shell",
    "experience/help": "p1-app-shell", "experience/modes": "p1-app-shell", "experience/navigation": "p1-app-shell",
    "experience/visual-design": "p1-app-shell", "experience/visualisation": "p1-reports",
    "governance/company-secretary": "p1-company-secretary", "integrations/connectors": "p1-connector-framework",
    "integrations/people-platform": "p1-payroll-link", "integrations/public-api": "p1-public-api",
    "ledger/chart-of-accounts": "p1-organisation-chart", "ledger/fixed-assets": "p1-fixed-assets",
    "ledger/general-ledger": "p1-general-ledger-close", "ledger/multi-currency": "p1-multi-currency-fx",
    "ledger/posting-engine": "p1-general-ledger-close", "ledger/validation": "p1-general-ledger-close",
    "ledger/invariants": "p1-general-ledger-close", "ledger/posting-rules": "p1-general-ledger-close",
    "ledger/money-and-rounding": "p1-general-ledger-close",
    "master-data/contacts": "p1-organisation-chart", "master-data/items": "p1-organisation-chart",
    "organisation/settings": "p1-organisation-chart", "payroll/employees": "p1-nemura-payroll", "payroll/pay-runs": "p1-nemura-payroll",
    "planning/budgets-and-forecasts": "p1-planning", "projects/time-and-costs": "p1-projects",
    "purchases/expense-claims": "p1-expense-claims", "purchases/payables": "p1-purchases",
    "reporting/dashboards-and-kpis": "p1-dashboard-kpis", "reporting/reports": "p1-reports", "sales/receivables": "p1-sales",
    "security/application-security": "p1-security-hardening", "security/data-protection": "p1-pdpa-retention",
    "security/detection-and-response": "p1-security-hardening", "security/identity-and-access": "p1-users-roles",
    "tax/gst-and-filings": "p1-gst", "tax/statutory-parameters": "p1-statutory-packs",
    "platform/container-images": "p1-security-hardening", "platform/local-mode": "p1-mcp-first-party",
    "platform/operations": "p1-operations-dr", "platform/performance-and-scale": "p1-operations-dr",
    "platform/portability-and-cost": "p1-operations-dr", "platform/release-and-versioning": "p1-security-hardening",
    "quality/calculation-correctness": "p1-launch-readiness", "quality/test-gates": "p1-launch-readiness",
    "people/platform": "p1-nemura-platform", "people/security": "p1-nemura-platform", "people/experience": "p1-nemura-platform",
    "people/records": "p1-nemura-records-packs", "people/features-and-packs": "p1-nemura-records-packs", "people/checks": "p1-nemura-records-packs",
    "people/leave": "p1-nemura-leave-time", "people/time": "p1-nemura-leave-time",
    "people/self-service": "p1-nemura-self-service-letters", "people/letters": "p1-nemura-self-service-letters",
    "people/onboarding": "p1-nemura-self-service-letters", "people/payroll": "p1-nemura-payroll",
    "people/fyniq-integration": "p1-nemura-fyniq-link", "people/assistant": "p1-nemura-assistant-external",
    "people/external-systems": "p1-nemura-assistant-external",
}
# P2 and P3 rows (and the "As FR-AR-12" rows) by ID
LATER = {
    "p2-invoicenow": "FR-AR-12 FR-AP-05 FR-TAX-10 FR-TAX-11",
    "p2-bank-feeds-payments": "FR-BNK-03 FR-PSP-02 FR-AR-13 FR-AP-08",
    "p2-tax-filings": "FR-TAX-06 ENG-33 FR-AR-11",
    "p2-payroll-outputs": "FR-PAY-08 FR-PAY-09",
    "p2-reporting-planning": "FR-RPT-04 FR-RPT-06 FR-RPT-07 FR-RPT-13 FR-VIZ-09 FR-KPI-04 FR-KPI-05 FR-KPI-06 FR-KPI-07 FR-PLN-04 FR-PLN-07 FR-EQ-23 FR-EQ-24 FR-EQ-28 FR-EQ-29 FR-EQ-13 FR-DIM-03 FR-FA-04",
    "p2-customer-portal-subscriptions": "FR-AR-14 FR-AR-15 FR-PRJ-11",
    "p2-api-third-parties": "FR-INT-04 FR-INT-12 FR-INT-15 FR-INT-18 AI-46 FR-INT-23 FR-INT-24 FR-NTF-04",
    "p2-mobile-approvals-automation": "FR-UX-16 FR-APR-06 FR-APR-07 FR-APR-08 FR-AUT-03 FR-AUT-04 FR-GL-09 FR-AP-10 FR-AP-21 FR-ADM-05 FR-UX-28 FR-UX-35 FR-TPL-06 FR-CS-07 CMP-04",
    "p2-ai-expansion": "AI-33 AI-59",
    "p2-security-certification": "SEC-25",
    "p2-nemura-people": "NM-REC-01 NM-REC-02 NM-REC-03 NM-REC-04 NM-REC-05 NM-REC-06 NM-PRF-01 NM-PRF-02 NM-PRF-03 NM-PRF-04 NM-PRF-05 NM-ONB-01 NM-LTR-05 NM-SEC-06 NM-EXT-04 NM-EXT-05 NM-AI-05 people.draft_job_post people.draft_interview_kit people.draft_review_summary",
    "p3-multi-entity": "FR-ENT-01 FR-ENT-02 FR-ENT-03 FR-GL-13 FR-RPT-12 FR-UX-18",
    "p3-saas-platform": "NFR-SCALE-02 FR-INT-05 FR-INT-19 FR-INT-28 AI-47 FR-NTF-06 FR-KPI-08 FR-ITM-02 DR-05 NFR-PORT-06",
    "p3-countries-certification": "FR-TAX-08 FR-TAX-09 FR-RPT-11 FR-AP-11 FR-TPL-08 CMP-PD-10 NM-TIM-03",
}
BY_ID = {i: c for c, ids in LATER.items() for i in ids.split()}
# P1 rows that belong to another change than their capability's default
BY_ID.update({
    "FR-BNK-12": "p1-payments-out", "FR-DOC-12": "p1-document-capture", "FR-AP-04": "p1-document-capture",
    "FR-AP-12": "p1-document-capture", "FR-GL-04": "p1-multi-currency-fx", "FR-GL-05": "p1-multi-currency-fx",
    "FR-CTL-03": "p1-audit-explorer", "FR-CTL-04": "p1-approval-engine", "FR-CTL-06": "p1-checks",
    "FR-INT-03": "p1-mcp-first-party", "FR-INT-20": "p1-connector-framework", "FR-INT-21": "p1-templates-email",
    "FR-INT-22": "p1-templates-email", "FR-UX-17": "p1-ai-assistant", "FR-UX-22": "p1-app-shell",
    "FR-ORG-05": "p1-statutory-packs", "FR-ORG-10": "p1-general-ledger-close", "CMP-02": "p1-gst",
    "FR-TAX-07": "p1-gst", "FR-PAY-07": "p1-payroll-link", "FR-PAY-11": "p1-payroll-link", "FR-PAY-15": "p1-payroll-link",
    "FR-PAY-10": "p2-payroll-outputs", "FR-EQ-25": "p1-equity-register",
    "AUD-03": "p1-audit-explorer", "NFR-IMG-05": "p1-security-hardening", "SEC-22": "p1-security-hardening",
    "FR-ADM-08": "p1-users-roles", "AI-15": "p1-mcp-first-party", "FR-INT-02": "p1-public-api",
    "RND-RATE": "p1-multi-currency-fx", "RND-GST-EXCL": "p1-statutory-packs", "RND-GST-INCL": "p1-statutory-packs",
    "RND-CPF": "p1-nemura-payroll", "RND-CPF-GRAD": "p1-nemura-payroll", "RND-SDL": "p1-nemura-payroll",
    "RND-SHARE": "p1-equity-register", "RND-MILEAGE": "p1-expense-claims", "RND-DISPLAY": "p1-app-shell",
    "RND-PERCENT": "p1-general-ledger-close",
    "INV-07": "p1-checks", "INV-10": "p1-reports", "INV-11": "p1-reports", "ENG-45": "p1-statutory-packs",
    "ENG-40": "p1-multi-currency-fx", "ENG-41": "p1-multi-currency-fx", "ENG-42": "p1-multi-currency-fx",
    "ENG-43": "p1-multi-currency-fx", "SEC-07": "p1-users-roles", "SEC-35": "p1-users-roles",
    "CMP-05": "p1-pdpa-retention", "AUD-06": "p1-audit-explorer", "AUD-09": "p1-audit-explorer",
    "FR-CT-03": "p1-organisation-chart", "FR-PPL-14": "p1-payroll-link", "FR-PPL-13": "p1-payroll-link",
    "FR-PPL-02": "p1-nemura-fyniq-link", "FR-PPL-03": "p1-nemura-fyniq-link", "FR-PPL-10": "p1-nemura-fyniq-link",
    "FR-PPL-15": "p1-nemura-fyniq-link", "FR-PPL-18": "p1-nemura-fyniq-link", "NM-ONB-02": "p1-nemura-self-service-letters",
    "DEV-09": "p1-ai-providers",
})
# Phase-less rows placed by title
TITLE = [
    ("ledger/posting-rules", r"bank transaction|Transfer between money|bank, card or cash", "p1-bank-import-reconcile"),
    ("ledger/posting-rules", r"invoice|Customer and supplier|refunds and deposits|Bad-debt|Credit documents|Settlement tolerances", "p1-sales"),
    ("ledger/posting-rules", r"bill", "p1-purchases"),
    ("ledger/posting-rules", r"salaries|Payroll per employee|Posting a pay run", "p1-payroll-link"),
    ("ledger/posting-rules", r"Gross pay|CPF, SDL and SHG", "p1-nemura-payroll"),
    ("ledger/posting-rules", r"expense claim|Mileage|Cash advance", "p1-expense-claims"),
    ("ledger/posting-rules", r"FX revaluation", "p1-multi-currency-fx"),
    ("ledger/posting-rules", r"Asset disposal|Depreciation", "p1-fixed-assets"),
    ("ledger/posting-rules", r"[Ff]ounder|Founders'", "p1-founders-money"),
    ("ledger/posting-rules", r"Share issue|allotment|SAFE", "p1-equity-register"),
    ("ledger/posting-rules", r"Payment-provider", "p1-payment-providers"),
    ("ledger/posting-rules", r"GST codes", "p1-gst"),
    ("data/backup-and-recovery", r"^Retention|Personal data in immutable|Shredded keys", "p1-pdpa-retention"),
    ("data/migration", r"Parallel run|Go-live and workbook", "p1-go-live"),
    ("data/audit-trail", r"Database and platform logs", "p1-operations-dr"),
    ("data/audit-trail", r"Analytical copy", "p1-audit-explorer"),
    ("platform/local-mode", r"Local AI", "p1-ai-providers"),
    ("platform/local-mode", r"partner substitutes", "p1-connector-framework"),
    ("platform/release-and-versioning", r"Statutory parameter pack", "p1-statutory-packs"),
    ("platform/release-and-versioning", r"AI catalogue", "p1-ai-proposals"),
    ("platform/release-and-versioning", r"Knowledge base versions", "p1-knowledge-base"),
    ("platform/release-and-versioning", r"Event schema", "p1-public-api"),
    ("platform/portability-and-cost", r"Claude in Singapore", "p1-ai-providers"),
]
# P0-built capability rows with no phase: owned by their P0 change, not planned again
P0_OWNED = {"platform/background-work", "platform/local-mode", "platform/container-images",
            "platform/release-and-versioning", "platform/portability-and-cost", "ledger/money-and-rounding",
            "ledger/invariants", "data/audit-trail"}


def bucket(phase):
    if phase is None:
        return None
    if phase.startswith("As "):
        return "P2"
    if re.match(r"P0\b", phase) and "/" not in phase:
        return "P0"
    if "P1" in phase:
        return "P1"
    return phase.split()[0]


def assign(cap, rid, head, phase):
    b = bucket(phase)
    if b == "P0":
        return "P0"
    if rid in BY_ID:
        return BY_ID[rid]
    if b is None:
        for c, pat, target in TITLE:
            if c == cap and re.search(pat, head):
                return target
        if cap in P0_OWNED and not re.match(r"(RND|INV|ALLOC)-", rid or ""):
            return "P0"
        if cap in P0_OWNED and rid and rid.startswith(("RND", "INV", "ALLOC")):
            # tested in P0 unless listed in BY_ID above
            return "P0"
        return CAP_P1.get(cap)
    if b == "P1":
        return CAP_P1.get(cap)
    return None  # a P2 or P3 row not listed in LATER: an error


def requirements():
    reqs = []
    for p in sorted(SPECS.rglob("spec.md")):
        cap = str(p.parent.relative_to(SPECS))
        for b in re.split(r"^### Requirement: ", p.read_text(encoding="utf-8"), flags=re.M)[1:]:
            head = b.split("\n", 1)[0].strip()
            m = re.match(r"((?:RND|ALLOC)-[A-Z]+(?:-[A-Z]+)*|(?:[A-Z]+-)+[A-Z0-9]*\d+[a-z]?|people\.[a-z_]+|C\.[12] #\d+|N\.1 #\d+)\b", head)
            rid = m.group(1) if m else None
            src = re.search(r"^Source: (.*)$", b, re.M)
            ph = re.search(r"Phase (.+)$", src.group(1)) if src else None
            phase = ph.group(1).strip() if ph else None
            reqs.append(dict(cap=cap, rid=rid, head=head, phase=phase, target=assign(cap, rid, head, phase)))
    return reqs


def ids_text(items):
    named = [i for i in items if re.match(r"(?:[A-Z]+-)+|people\.|C\.|N\.", i)]
    other = len(items) - len(named)
    s = ", ".join(named)
    if other:
        s += (", " if s else "") + f"{other} unnumbered"
    return s


def build():
    """Returns the regions, the errors, and how many requirements and screens were placed."""
    errors = []
    reqs = requirements()
    for r in reqs:
        if r["target"] is None or (r["target"] not in IDS and r["target"] != "P0"):
            errors.append(f"{r['cap']}: {r['rid'] or r['head']} (phase {r['phase']}) has no change: "
                          "add its ID to LATER or BY_ID, or its capability to CAP_P1")
    known = {r["rid"] for r in reqs}
    errors += [f"BY_ID names {k}, which no spec has" for k in BY_ID if k not in known]
    owner = {r["rid"]: r["target"] for r in reqs if r["rid"]}
    screens = collections.defaultdict(list)
    for s in json.loads(INVENTORY.read_text(encoding="utf-8"))["screens"]:
        if s["phase"] == "P0":
            continue
        votes = collections.Counter(owner.get(i) for i in s.get("requirements", []) if owner.get(i) not in (None, "P0"))
        if not votes:
            errors.append(f"screen {s['id']} names no requirement planned after P0")
            continue
        screens[votes.most_common(1)[0][0]].append(s["id"])
    count = collections.Counter(r["target"] for r in reqs)
    errors += [f"change {c['id']} has no requirement" for c in C if count[c["id"]] == 0 and c["id"] not in EMPTY_OK]
    caps = collections.defaultdict(lambda: collections.defaultdict(list))
    for r in reqs:
        caps[r["target"]][r["cap"]].append(r["rid"] or r["head"])

    placed = sum(len(v) for v in screens.values())
    regions = {"summary": f"""

That is all {len(reqs):,} requirements:
- {count['P0']} belong to the P0 changes, which built them or hold them for the cloud gate;
- the other {len(reqs) - count['P0']:,} are spread over {len(C)} changes below;
- all {placed} designed screens after P0 sit with the change that owns most of their requirements.

"""}
    for track in [F1, N1, CG, F2, N2, F3]:
        lines = ["| # | Change | Builds | Reqs | Screens | After | Local first | Outside dependency | Reviewer | Launch |",
                 "|---|---|---|---|---|---|---|---|---|---|"]
        for c in (c for c in C if c["track"] == track):
            deps = ", ".join(d if d == "P0" else re.sub(r"^(\S+)", r"`\1`", d) for d in c["deps"])
            lines.append(f"| {c['wave']} | `{c['id']}` | {c['title']} | {count[c['id']]} | {len(screens[c['id']])} | {deps} | "
                         f"{c['local']} | {c['outside']} | {c['reviewer']} | {c['launch']} |")
        regions["track " + track] = "\n\n" + "\n".join(lines) + "\n\n"
    lines = ["| Change | Capability: requirements |", "|---|---|"]
    for c in C:
        parts = [f"`{cap}`: {ids_text(v)}" for cap, v in sorted(caps[c["id"]].items())]
        sc = ", ".join(sorted(screens[c["id"]]))
        lines.append(f"| `{c['id']}` | {'; '.join(parts) or '—'}{'<br>Screens: ' + sc if sc else ''} |")
    regions["coverage"] = "\n\n" + "\n".join(lines) + "\n\n"
    return regions, errors, (len(reqs), placed)


def render(text, regions):
    seen = set()

    def fill(m):
        name = m.group(1)
        if name not in regions:
            raise SystemExit(f"✖ {PLAN.relative_to(ROOT)}: unknown region roadmap:{name}")
        seen.add(name)
        return f"<!-- roadmap:{name} -->{regions[name]}<!-- /roadmap -->"

    out = REGION.sub(fill, text)
    missing = sorted(set(regions) - seen)
    if missing:
        raise SystemExit(f"✖ {PLAN.relative_to(ROOT)}: no region for {', '.join(missing)}")
    return out


def main():
    cmd = sys.argv[1] if len(sys.argv) > 1 else "check"
    if cmd not in ("check", "write"):
        raise SystemExit(__doc__)
    regions, errors, (reqs, placed) = build()
    if errors:
        print("✖ the P1–P3 plan does not place everything:")
        for e in errors:
            print("  " + e)
        sys.exit(1)
    text = PLAN.read_text(encoding="utf-8")
    new = render(text, regions)
    if cmd == "write":
        if new != text:
            PLAN.write_text(new, encoding="utf-8")
        print(f"✔ {PLAN.relative_to(ROOT)} regenerated")
    elif new != text:
        print(f"✖ {PLAN.relative_to(ROOT)} is stale: the specs, the screens or scripts/roadmap.py changed. Run `make roadmap`")
        sys.exit(1)
    else:
        print(f"✔ P1–P3 plan: {reqs:,} requirements and {placed} screens after P0, each placed once, across {len(C)} changes")


if __name__ == "__main__":
    main()
