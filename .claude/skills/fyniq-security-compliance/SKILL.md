---
name: fyniq-security-compliance
description: Security and compliance work for Fyniq — controls as code (compliance/controls/*.yaml) with automated checks and evidence, the Compliance Center, OWASP ASVS 5.0 tagging (L2 baseline, L3 on money, approval, auth and logging flows), threat modelling per feature, secrets and log redaction, hardened images and CI supply chain (SLSA provenance, cosign, SBOM, SHA-pinned actions), PDPA processes (DPIA, data inventory, NRIC rules, breach timers, access requests), the incident runbook, and the statutory calendar controls. Use whenever a change touches money movement, personal data, permissions, authentication, OAuth clients, logging, infrastructure, CI workflows or images; when you add or change a control; when asked about ISO 27001, SOC 2, CSA Cyber Essentials or Cyber Trust, PDPA or a data breach; or when someone says "is this secure", "do we need a DPIA", "what evidence do we keep" or "a token leaked".
---

# Security and compliance

Fyniq treats controls the way it treats code: each control is a versioned file with an
automated check that proves it and the evidence the check produces (PRD-001 rev 0.2 §14.1).
Preventive controls block releases; detective controls page their owner. The registry maps every
control to Singapore statute, PDPA, ISO/IEC 27001:2022 Annex A, SOC 2, CSA Cyber Essentials /
Cyber Trust, the IMDA agentic-AI framework and OWASP (Appendix F).

> **Status:** no code exists yet. This skill states the target conventions (§13, §14,
> Appendix F). Commands marked *(P0)* arrive in P0.

## Where things live

| Path | Content |
|---|---|
| `compliance/controls/*.yaml` | One file per control (IDs from Appendix F: `CTL-FIN-*`, `CTL-AUD-*`, `CTL-IAM-*`, `CTL-CHG-*`, `CTL-DP-*`, `CTL-RES-*`, `CTL-AI-*`, `CTL-API-*`, `CTL-MCP-*`, `CTL-STAT-*`) |
| `compliance/checks/` | The automated checks the controls reference (tests, config probes, queries) |
| `compliance/threat-models/` | One threat model per feature that moves money, handles personal data or changes permissions |
| `compliance/policies/` | The policy library (CMP-07): information security, access control, incident response, backup, change, AI use, data protection |
| `compliance/runbooks/` | Incident response (SEC-32) including the PDPA breach procedure |
| `fyniq-audit` project | Retention-locked evidence, audit anchors and log buckets (§12.4) |

## A control entry

```yaml
# compliance/controls/CTL-MCP-01.yaml
id: CTL-MCP-01
title: Agent-class tokens cannot write except by propose
description: >
  Every OAuth client of class `agent` (all MCP clients) can only create proposals.
  The API rejects any other state-changing request from an agent-class token.
owner: founder-tech            # a role, not a person's name
requirements: [AI-34, AI-16, QA-MCP-01]
mappings:
  iso27001: ["5.15", "8.2"]
  soc2: ["CC6.1"]
  agentic_ai: ["bound risks", "human checkpoints"]
  owasp: ["ASI02", "ASI03"]
  asvs: ["V8.2.1"]              # [verify ASVS 5.0 requirement IDs]
type: preventive               # preventive | detective
checks:
  - id: mcp-agent-write-denied
    kind: test                 # test | config | query | attestation
    ref: internal/api/authz/agent_write_test.go
    frequency: CI
  - id: nightly-agent-writes
    kind: query
    ref: compliance/checks/nightly_agent_writes.sql
    frequency: N
    expect: "0 rows"
evidence: [test report, nightly query result]
on_failure: block-release      # block-release | page-owner
```

The Compliance Center (CMP-01) reads these files and the check results to show the status, last
run, evidence link, owner and framework mappings of each control, and raises Inbox tasks for
failures and due attestations (CMP-03).

## OWASP ASVS 5.0 tagging (SEC-20)

- **Level 2 baseline** everywhere; **Level 3** for business logic, authentication, authorisation
  and logging on money and approval flows (payments, payee bank details, approvals, period
  locks, proposals, OAuth and MCP).
- Tag security tests with the ASVS requirement they prove (`// asvs: V8.…` [verify the 5.0
  numbering]), next to the requirement ID (`// req: SEC-…`). CI reports coverage by chapter.
- Use the OWASP Top 10:2025 as the review checklist and the OWASP LLM / Agentic Top 10 for AI
  features (AI-18…25).

## Threat model per feature (SEC-20)

Required when a change moves money, touches personal data, changes permissions or roles, adds an
integration or OAuth client, or exposes a tool to AI. One page in `compliance/threat-models/`:

| Section | Content |
|---|---|
| Assets | Data and actions at stake (e.g. vendor bank details, approval signatures) |
| Actors | Roles, team mode, AI principal, integrations, external attackers |
| Entry points | API operations, events, webhooks, MCP tools, imports, uploads |
| Threats (STRIDE + LLM/agentic) | Spoofing, tampering, repudiation, disclosure, denial, elevation; injection, excessive agency |
| Controls | Requirement and control IDs that mitigate each threat |
| Tests | The tests that prove it (ASVS-tagged) |
| Residual risk | Accepted by whom; entry in the risk register (CMP-06) |

## Secrets, logs and local adapters

| Rule | Requirement |
|---|---|
| Secrets only in Secret Manager (production) or the git-ignored `.env` (local); rotated; never in code, images or logs; secret scanning in CI | SEC-12 |
| Logs never contain secrets, tokens, full NRIC/FIN or bank numbers; redaction is tested | SEC-15, CTL-DP-03 |
| Sensitive fields envelope-encrypted with Cloud KMS and masked by default | SEC-11, CTL-DP-02 |
| Local adapters (software keys, emulators, replayed AI) never reach staging or production; production refuses to start with them | SEC-16, DEV-07, CTL-CHG-09 |
| No standing human write access to production databases; break-glass with approval and session recording | SEC-03 |
| No service-account keys; Workload Identity Federation for CI | SEC-04, CTL-IAM-04 |

## Images and CI supply chain

| Rule | Requirement / control |
|---|---|
| Distroless, non-root, read-only root filesystem, no shell, base pinned by digest, size budget | SEC-27, NFR-IMG-01…07, CTL-CHG-08 |
| SBOM per release; vulnerability scan with no unwaived critical or high findings | SEC-26, NFR-IMG-05 |
| Cosign signature and SLSA provenance; Binary Authorization admits only attested images; deploy by digest | SEC-22, CTL-CHG-04 |
| Third-party actions pinned by commit SHA; `permissions:` per job; no secrets for fork pull requests; release and deploy only from `main` and tags | SEC-28, CTL-CHG-11 |
| Two-person review on protected branches; release pull requests reviewed | CTL-CHG-01, REL-04 |
| Versions and changelog only from release-please and Conventional Commits | CTL-CHG-10 |

## PDPA processes (§14.3)

| Process | What to do | ID |
|---|---|---|
| Data inventory | Each new personal-data field or processing gets an inventory row: what, why, where, who, how long, cross-border flows (including AI model calls) | CMP-PD-02, CTL-DP-05 |
| DPIA | Required for payroll, the AI assistant and any new processing of personal data, before release | CMP-PD-08 |
| NRIC/FIN | Collect only where the law requires (CPF, IRAS); encrypt; mask in the UI; **never** use as an authenticator or a file password | CMP-PD-07, CTL-DP-04 |
| Access and correction | Tracked workflow; respond within 30 days, or say in writing within 30 days when it will take longer | CMP-PD-04 |
| Erasure vs records | Anonymise what records do not need; statutory records retained under the legal-obligation basis; crypto-shredding of per-person keys when retention ends | §11.8, AUD-06 |
| Transfers | Every overseas transfer (ADR-T2 Option B/C, DR copies, sub-processors) assessed and recorded | CMP-PD-06 |
| Breach | See the timers below | CMP-PD-05 |

**Breach timers.** Assess within **30 days** of becoming aware. Notify the PDPC within **3
calendar days** after deciding that a breach is notifiable (significant harm, or ≥ 500
individuals). A leak of a name or NRIC together with salary or bank details is notifiable. As a
data intermediary (P3), notify customers without undue delay.

## Incident response (SEC-32)

1. **Detect and triage**: alert, owner on call, severity. Open an incident record (the clock
   starts for PDPA).
2. **Contain**: kill switches (AI-13; per MCP client, AI-43), revoke tokens and sessions
   (AI-38), rotate secrets, pause webhooks or integrations, block at Cloud Armor.
3. **Preserve evidence**: audit trail, chain verification, Cloud Audit Logs, AI activity log;
   never alter them.
4. **Assess**: personal data involved? Notifiable? Ledger integrity affected? Run the nightly
   verification and chain verification on demand (FR-CTL-05, AUD-03).
5. **Notify** per the timers; record decisions.
6. **Recover**: fixes through the normal pipeline; data corrections only by approved journals
   (never by editing posted rows).
7. **Learn**: root-cause review; a regression test or a new control; update the threat model.

Tabletop exercises twice a year (CTL-DP-06).

## Which controls a change must touch

| Your change | Update or check |
|---|---|
| New posting source, money movement or approval | Threat model; CTL-FIN-*; QA-SEC-03; ASVS L3 tags |
| New personal-data field | Data inventory; masking and encryption tests; DPIA if new processing; CTL-DP-02/03 |
| New role, permission or team-mode rule | Appendix D matrix; CTL-IAM-06; SoD scan (CTL-FIN-07) |
| New API operation, webhook or OAuth client | CTL-API-01/02; **fyniq-api-integrations** |
| New AI read tool, task type or MCP tool | CTL-AI-*; CTL-MCP-*; **fyniq-ai-proposals**, **fyniq-mcp-server** |
| Infrastructure or organisation policy | CTL-CHG-02 (IaC only, drift); CTL-DP-07 (locations); CTL-AUD-03/04 |
| CI workflow or Dockerfile | CTL-CHG-08/09/11; SEC-27/28 |
| Statutory deadline logic | CTL-STAT-*; the statutory calendar (CMP-02) |

## Commands

`make test` *(P0)* runs the preventive checks that are tests (database grants, authorisation
matrix, redaction, MCP and AI controls). `make images` *(P0)* enforces the image rules.
`make ci` *(P0)* runs everything CI runs. Configuration checks (`gcloud`/OpenTofu probes) run in
CI and nightly against the real projects, never from developer machines.

## Commits

`feat(security): …`, `feat(compliance): add CTL-API-02 nightly check`, `fix(security): …`. Never
put secrets, real personal data or incident details that identify people in commit messages.

## Definition of done

- [ ] Threat model written or updated when required; residual risks in the register.
- [ ] Affected controls updated; checks automated where possible; evidence path defined.
- [ ] Security tests tagged with ASVS and requirement IDs; L3 where the flow needs it.
- [ ] No secrets or excluded personal data in code, logs, events, AI traces or commits.
- [ ] Data inventory and DPIA updated for new personal data.
- [ ] Image and workflow rules still green.
