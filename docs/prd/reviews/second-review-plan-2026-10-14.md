# Second review plan: PRD-001 revisions 0.16–0.19 and PRD-002 revisions 0.2–0.5

AGENTS.md ("Safety") says that anything touching money movement, payee bank details, approvals, the
audit trail, tenancy or the AI/MCP write path needs a second human reviewer, whoever wrote it. The
revisions of 13–14 Oct 2026 were written with an AI agent at the founder's request and are marked
**Second reviewer required** in both revision histories. This plan records how that review happens.

## Decisions (founder, 14 Oct 2026)

| Question | Decision | Why |
|---|---|---|
| Who reviews | **Split by area**: the external accountant / tax agent reviews statutory values and payroll; the co-founder (the other director) reviews roles, approvals and payments; a security reviewer (fractional CTO or consultant) reviews the AI/MCP surface, identity and tenancy | No single reviewer covers tax law, money controls and OAuth security; this matches the PRD's own Reviewers line |
| How | **One pull request per area**, with the checklist below in its description; merging needs the reviewer's approval. Branch protection on `main` (CTL-CHG-01) is switched on when the review PRs open | The PRD requires an audit trail of changes (CTL-CHG-01, REL-01); comments sit on the exact lines |
| When | **Before P0 work starts on each area**, in this order: statutory and payroll; roles, approvals and payments; AI/MCP and identity (before the MCP server is built) | Nothing is built on an unreviewed rule, and the spec is cheapest to change before code exists |
| Until then | The research and decisions of 14 Oct 2026 are committed to `main`; the review PRs come later | Founder's choice, so the work is saved now |

Decisions taken on the same day that the reviewers confirm rather than make:

- **Payslips are never in Fyniq.** Without Nemura, payslips and KETs come from the payroll provider or a
  documented manual process, recorded under FR-PAY-15; FR-PAY-08 is superseded for Fyniq (PRD-001 rev 0.19).
- **Outstanding advance at final pay**: recover only what the employee agreed to in writing and show
  the rest as an open advance (FR-PAY-16, G4-007), until the accountant rules on the instalment limits.
- **Leave liability**: a monthly draft accrual of carried-forward leave, with the accountant's
  materiality floor (FR-PPL-07, NM-LV-09).
- **Framework**: SFRS for Small Entities, the current edition until the first year beginning on or
  after 1 Jan 2027 and the third edition from then (open question 31), subject to the accountant's
  confirmation of eligibility.

## Area 1 · Statutory values and payroll (accountant / tax agent)

Read with the [verification sweep](../research/verify-sweep-2026-10-14.md), which gives each source.

- [ ] CPF: rates by age band for 2026 and 2027, rounding, OW and AW ceilings, AW refunds, SPR rates (PRD-001 §14.2, FR-PAY-02, FR-PAY-14, RND-CPF)
- [ ] Wage classes by facts, including commission (FR-PAY-17); deductions, reimbursements and disbursement (FR-PAY-18, B.3)
- [ ] SDL (SWDA for foreign-only employers) and the self-help group tables; the G.2 hand-pinned cases, including the re-derived April SHG value
- [ ] Salary deductions and final pay (FR-PAY-16, PRD-002 NM-PAY-08); **decide** whether the 12-month / 25% instalment limits stop an advance being recovered from final pay
- [ ] IR21 scope and the clearance hold (FR-PAY-10, NM-INT-08); **decide** whether director-balance repayments are held
- [ ] GST: time of supply and deposits (ENG-45, G4-018), claim periods, registration transitions (G4-020), error correction (FR-TAX-12), InvoiceNow population (FR-TAX-10)
- [ ] Input tax on bills in an employee's or director's name (FR-AP-20); **decide** the case of a claim kept as a director's loan
- [ ] Withholding tax (FR-AP-11, FR-AP-19)
- [ ] Founders' money (§8.22): conversion and waiver (Advance Ruling Summary 15/2026 binds only its applicant); **decide** the taxability of waiving amounts deducted earlier, founders' costs from before incorporation, and CPF on salary deferred by agreement
- [ ] Share-based payment: **decide** the valuation basis for shares for services, conversions and grants (FR-EQ-04, FR-EQ-13, FR-EQ-17, FR-EQ-18)
- [ ] Framework and policies (FR-ORG-10, open question 31): **confirm** SFRS for Small Entities and the revenue policy per Model B case (FR-AR-15, FR-PSP-07)
- [ ] Leave liability policy and materiality floor (FR-PPL-07, NM-LV-09)
- [ ] The Singapore pack (PRD-002 Appendix A, 22 topics), government-paid leave schemes (NM-LV-14) and KETs (NM-LTR-06)

## Area 2 · Roles, approvals and payments (co-founder)

- [ ] What each role sees (PRD-001 §7.1, 18 roles) against Appendix D; the "Workspaces by role" canvas
- [ ] Every workspace cut to the person's rights (FR-UX-40) and the personal workspace (FR-UX-39); the line manager as a relationship, not a role (§6.2)
- [ ] Department payroll cost shown only as a total covering at least 3 people besides the viewer (FR-PAY-13)
- [ ] Approvals across Fyniq and Nemura: the signed approval, posting and corrections (FR-PPL-09…11, NM-PAY-09…10)
- [ ] Salary payment batches: lines from Nemura, approval and release in Fyniq, holds checked at release, bank-detail versions (FR-PPL-10, FR-PPL-13, FR-PPL-14, FR-BNK-12)
- [ ] Payment release and segregation of duties (§6.4, SOD rules, FM-5)
- [ ] Joiner invitations as an Administrator right (FR-ADM-01, NM-ONB-02)

## Area 3 · AI/MCP, identity and tenancy (security reviewer)

- [ ] Model providers, routes, data classes and egress (AI-70…AI-88, SEC-37); the never-list (§9.6)
- [ ] Data residency (ADR-T2): Opus 4.6 confirmed in Singapore on all three Google pages; Sonnet 4.6 on two of three; the 5.x models have no Singapore endpoint
- [ ] MCP (§9.7) against the 2026-07-28 specification: `iss` checks, credentials keyed by issuer, dynamic client registration deprecated; Hydra has no RFC 8707 or Client ID Metadata Document support, so **decide** open question 40 (token exchange: Ory Enterprise Licence, the JWT-bearer grant, or another server such as Keycloak)
- [ ] Tokens: audience binding, DPoP where clients support it, revocation within 60 seconds (AI-36, AI-38)
- [ ] Sign-in: the Zoho issuer per data centre (FR-ADM-08, §17.3 item 7); passkeys implemented in the API (Identity Platform has none)
- [ ] NRIC handling while PDPC's guidance is under review (CMP-PD-07); NRIC never used for authentication after 31 Dec 2026
- [ ] One sign-in across Fyniq and Nemura and the Finance | People switch (FR-PPL-02, NM-INT-*); tenancy on every row (golden rule 8)
- [ ] The people assistant's data path and own-pay explanations (PRD-002 NM-AI-*, AI-83)

## Each review PR

- Title: a Conventional Commit, for example `docs(prd): second review of statutory values and payroll`.
- Description: this area's checklist, with every **decide** item answered in the PR, and the PRD
  revision rows it covers.
- Outcome: the reviewer approves, or requests changes; any change goes into the PRD with a new
  `(rev 0.N)` tag, and the specs are updated and stamped in the same PR.
