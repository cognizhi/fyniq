# PRD-001: findings from the OpenSpec conversion · 13 Oct 2026

| | |
|---|---|
| Source | PRD-001 revision 0.10, converted into 81 main specs under `openspec/specs/` (1,142 requirements, 1,005 locked anchors) and 158 screen designs |
| Found by | Claude Code (Opus 5.5) while converting each PRD row into requirements and scenarios, and while designing the screens |
| Status | **Resolved in PRD revision 0.11** (13 Oct 2026), every change tagged (rev 0.11); the specs were updated and stamped. OC-62 needed no PRD change. See "Resolution" at the end. The items marked **2nd** change the AI write path, data residency or approvals and need a second human reviewer before merge |

Findings are contradictions, gaps or ambiguities that a careful reader of the PRD would trip over.
The specs follow the PRD as written; where the PRD contradicts itself, the spec quotes both sides
or follows the row the requirement ID belongs to. IDs `OC-nn` are for this record only.

## Highest impact

| ID | Finding | Why it matters |
|---|---|---|
| OC-01 | **§G.2 pay examples:** for the age ≤ 55 rows, net ≠ gross − employee CPF (6,077 vs 6,080; 5,595 vs 5,600), and there is no SHG column; SHG is missing elsewhere even though FR-PAY-04 requires it | The golden examples are test fixtures (QA-ENG-02); they must be internally consistent |
| OC-02 | **SEC-13** puts backups and DR copies in a second region; **DR-03, DR-04, NFR-DR-01** (OQ 37) say no copy outside `asia-southeast1` at P1 | Data residency and the DR design depend on which holds |
| OC-03 | **§8.22** routes table posts a share conversion to Cr 3010; **FR-EQ-06** and B.7 say "3010 or 3020" | Which equity account a director-loan conversion credits |
| OC-04 | **AI tier approvers:** §9.6 says Bookkeeper+ approves tier 1 and Accountant or Controller tier 2; Appendix E names Record owner, Requesting user, Employee, Controller+, AR+, AP+ | The approval rule for every AI proposal |
| OC-05 | **AI kill switch:** AI-13 says "instantly"; AI-43 and AI-38 say "within 60 seconds" | A testable security control needs one number |
| OC-06 | **Never-list vs catalogue:** the never-list bans "approving anything", yet `ar.post_invoice` is "Post (approve) a draft invoice" | Golden rule 5; the red-team suite tests the never-list |
| OC-07 | **Violet:** §7.4 reserves violet for AI content; the §7.5 data palette includes violet (`--chart-7`) | A chart series could be mistaken for AI content |
| OC-08 | **FR-UX-27** sets defaults for a "Founder" role that §6.2 does not define | Roles drive permissions and Basic/Expert defaults |
| OC-09 | **Appendix D vs segregation of duties:** Controller C/A on statutory parameters vs SOD-10; Owner A on users and roles plus period close, GST and vendor bank details vs SOD-6 (mitigated only by dual control) | The RACI and the SoD rules must agree |
| OC-10 | **AI preparer:** AI-10 says "the AI is the preparer"; rev 0.8 says "the human who asked is the preparer" | Who may approve an AI-prepared item |

## Controls and checks

| ID | Finding |
|---|---|
| OC-11 | FR-CTL-01 says "every workbook check (37)", but C.1 #34 is retired, so 36 are live |
| OC-12 | C.1 #8 and #22 are both ✖ and ⚠ with no stated split |
| OC-13 | C.2 #20 uses "S$X", an undefined threshold |
| OC-14 | FM-8 (unreviewed break-glass ✖) and FR-CS-06 (sole director as secretary ✖) are checks without a C.2 number |
| OC-15 | C.2 #52 is P2, but Appendix C has no phase column |
| OC-16 | CTL-FIN-02 and CTL-AUD-05 give frequency "CI, N" without naming the nightly check |
| OC-17 | CTL-DP-06 frequency "E, semi-annual" does not name the event |
| OC-18 | CTL-STAT-03 "IR21 before cessation" is looser than §14.2 (at least one month before) and G4-007 |
| OC-19 | QA-ENG-01 and QA-ENG-06 gates do not say what they block |
| OC-20 | CTL-FIN-11/12 say "reconcile" without saying to what (except the 3010 + 3020 equation) |
| OC-21 | QA-RPT-03 "written as text": both values, or only the one over 15 digits? |

## Payroll, projects and ledger

| ID | Finding |
|---|---|
| OC-22 | §G.2 wage-700 note asks to re-derive expected values, but the pinned March total S$70.75 relies on that row's SDL 2.00 |
| OC-23 | §G.2 "Age 60–65, PR" row does not state the PR year, and the rates depend on it |
| OC-24 | §J.2 UC-PRJ-02 still cites the superseded FR-PRJ-04 |
| OC-25 | FR-PAY-10 is P2 but carries the P1 IR21 clearance-hold obligation |
| OC-26 | FR-PAY-16 vs G4-007: do the 25% / 12-month recovery limits apply on termination? [verify] |
| OC-27 | FR-PRJ priorities: M rows 05 and 07 depend on S row 01; 02 (S) overlaps 05 (M); 03 (S) repeats 12 (M) |
| OC-28 | FR-EMP-01 hourly cost override vs FR-PRJ-06 effective-dated cost-rate override: one field or two? |
| OC-29 | FR-PAY-07 names 2110 "CPF & SDL payable", but §10.2 also credits SHG to 2110 |
| OC-30 | FR-FA-01 capitalisation prompt: at the threshold, or above it? |
| OC-31 | ENG-33 text says "(this part is M/P1…)" but its cells say S/P2 (the G4-001 pattern) |
| OC-32 | ENG-30 (⚠ past review date) and C.2 #16 ("older than the latest published version") use different triggers |
| OC-33 | Appendix B.5 gaps: 2700 and share application monies are missing; 7040 is named in §10.2 [confirm] while B.5 leaves the unrealised-FX code unassigned; 5040 appears only in a superseded row; 2120 is "proposed" in B.5 but fixed in §10.2 and B.7 |
| OC-34 | §10.7 intro says "workbook checks become the last five rows", which does not match (INV-12/13 are not workbook checks; CFW_Check maps to no row; BS_Uncl is not listed) |
| OC-35 | A superseded payout row holds a live rule ("fees post once, per transaction, never again at payout"); move it into the rev 0.7 payment-provider row |
| OC-36 | B.1 spreads release "from the posting month", B.2 "from the service start month": intended? |
| OC-37 | B.2's bill net leg has no explicit RND-FX |

## Sales, purchases and banking

| ID | Finding |
|---|---|
| OC-38 | FR-AP-04 text says "now P1", but its Phase cell says P2 |
| OC-39 | Duplicates: for receipts FR-AP-15 flags (⚠) while UC-EXP-01 blocks; for bills FR-AP-04 flags while FR-AP-12 blocks |
| OC-40 | FR-AR-16's status list mixes delivery and settlement states with quote acceptance |
| OC-41 | Undefined terms: FR-AR-07 "month-end date handling"; FR-AR-05 "within 30 days" (of what?); FR-AR-11 "12 months passed" (from when?) |
| OC-42 | FR-BNK-03 still allows a "licensed aggregator"; FR-BNK-15 lists none |
| OC-43 | Rules that exist only in Appendix J, with no requirement ID: a claim over its category limit needs an extra approver (UC-EXP-01); a late trip needs finance approval (UC-EXP-02); a bank report from an unexpected sender is quarantined (UC-BNK-01); the "likely paid" alert (UC-BNK-02); an unknown provider event is parked ⚠ (UC-PSP-01) |

## Integrations, documents and data

| ID | Finding |
|---|---|
| OC-44 | FR-DOC-03 (S/P2) is "part of FR-DOC-12" (M/P1) |
| OC-45 | FR-DOC-02 "deletions only by retention expiry" vs removable draft files (FR-DOC-07/13 staging reconciles it, but the text does not say so) |
| OC-46 | AUD-06 retention of at least the accounting retention vs the shorter security-log retention of rev 0.8 |
| OC-47 | FR-INT-22 "through a provider's webhook" vs the IMAP polling adapter |
| OC-48 | FR-INT-23 omits Zoho Projects and ServiceNow, which the §8.24 table lists |
| OC-49 | FR-NTF-03 does not give the ARIA role for warning banners |
| OC-50 | FR-INT-02 is P2, but its text says the client-class model exists from P1 |
| OC-51 | FR-INT-04 and FR-NTF-04 both describe Slack |
| OC-52 | Vendor facts stated without `[verify]`: "ZeptoMail renamed Zoho CPaaS (Sep 2026)"; "Resend receiving GA since Nov 2025" |

## AI

| ID | Finding |
|---|---|
| OC-53 | Tier 1 is "reversible, no cash movement" (§9.6), yet in Appendix E categorisation and matching post |
| OC-54 | `recur.create_template` has a limit but no role that approves above it |
| OC-55 | AI-02's exclusions say "never send…", yet are "Owner-configurable": may the Owner relax the never items? |
| OC-56 | AI activity log visibility: AI-P8 says "user and Owner"; AI-12 says "Owner/Compliance" |

## Tax, reporting, equity and governance

| ID | Finding |
|---|---|
| OC-57 | FR-KPI-01 says 16 tiles but names 15 |
| OC-58 | The §8.22 pay table has five choices (including shares instead of pay); FR-EQ-04 records four |
| OC-59 | FR-EQ-11 says exempt-private-company status lives on FR-ORG-01; neither FR-ORG-01 nor FR-CS-01 lists it |
| OC-60 | The §8.22 routes table and FR-EQ-05 still cite FR-PLN-07 for SAFEs and the cap table (now FR-EQ-18/20) |
| OC-61 | G4-101 accepts "✖ or Not assessed"; FR-EQ-25 says Not assessed when rights are not recorded |
| OC-62 | FR-EQ-08 (share-issue register) may belong in the cap-table-and-investors capability |

## Experience and access

| ID | Finding |
|---|---|
| OC-63 | Basic-mode keyboard is "⌘K and ? only", yet ⌘J opens the AI panel and FR-UX-23 has a mode shortcut |
| OC-64 | Appendix D lacks Integration and Company-secretary columns; §D omits the Super admin's "platform health" |
| OC-65 | Feature switches: §7.1 marks Projects and Equity; FR-ORG-06 and §7.8 also switch Payroll |
| OC-66 | Small ambiguities: FR-VIZ-06 direct labels instead of the legend, or with it? FR-UX-20 "plain decimals": drop thousands separators? FM-2: is exactly S$2,000 "below" the limit? §6.2 Owner break-glass: exempt from audit too? |

## Security, compliance and platform

| ID | Finding |
|---|---|
| OC-67 | NFR-COST-02 says only the database, storage, keys and tick are billed while idle, yet lists fixed fees (load balancer, Cloud Armor, secrets, registry, monitoring) |
| OC-68 | §12.12 still says only Sonnet 4.6 has the `asia-southeast1` endpoint, next to the corrected list |
| OC-69 | NFR-OBS-02 notifies the "Owner", but the alert goes to the platform owner first |
| OC-70 | REL-01 lists 10 commit types; AGENTS.md and the commit hook also allow `deps` |
| OC-71 | SEC-27 requires a read-only root filesystem unconditionally; NFR-IMG-02 exempts Cloud Run |
| OC-72 | The local secrets file is `.env` in §12.8 and `.env.local` in §12.12 |
| OC-73 | PostgreSQL versions: NFR-PORT-02 says 17+, the §12.12 extensions say 16–17, §12.8 runs 18 |

## Handled during the conversion

- Fixed in rev 0.10: §7.4 motion timing (150–200 ms) now matches FR-UX-38 (120–240 ms); FR-UX-37's
  44 px touch target and FR-UX-30's 24 px ⓘ are reconciled (24 px visual, 44 px hit area).
- FR-AP-07/08 (expense claims) are specified under `purchases/expense-claims`, not payables.
- §11.8 crypto-shredding, §14.1 compliance-as-code, §9.3, §9.7 (MCP revision kept `[verify]`),
  §9.8, §7.5, §10.4 and the Appendix B conventions are converted and cited.

## Resolution (PRD revision 0.11, 13 Oct 2026)

| ID | What changed | Review |
|---|---|---|
| OC-01 | §G.2 gains an SHG column; net = gross − employee CPF − SHG (S$3.00 CDAC in March; S$5.00 in April, fund and band [verify]) |  |
| OC-02 | SEC-13: backups and the DR export stay in Singapore at P1 (DR-03, DR-04, OQ 37); "a second region" superseded | 2nd |
| OC-03 | §8.22: Cr 3010 or 3020, the account of the class allotted |  |
| OC-04 | §9.6: approvers are set per task type in Appendix E; tier 1 may post reversible entries | 2nd |
| OC-05 | AI-13: new requests refused at once; tokens, streams and queued calls stop within 60 seconds | 2nd |
| OC-06 | Never-list: the AI never approves or rejects; `ar.post_invoice` and `ap.post_bill` are approved by someone other than the requester | 2nd |
| OC-07 | §7.5: the violet series is a data colour (seventh), never the `--ai` tokens; AI content is marked by its badge, never by hue (also MASTER.md §10) |  |
| OC-08 | FR-UX-27: "Founder" removed; founders are Owners |  |
| OC-09 | Appendix D: C/A means prepare or approve, never both for one item; the Owner holds both sides of SOD-6 only under dual control |  |
| OC-10 | AI-10: "(the AI is the preparer)" superseded; the rev 0.8 rule (the human who asked is the preparer) governs | 2nd |
| OC-11 | FR-CTL-01: 36 live workbook checks |  |
| OC-12 | C.1 #8 and #22: ✖ and ⚠ split stated |  |
| OC-13 | C.2 #20 rewritten without "S$X" |  |
| OC-14 | New C.2 #53 (unreviewed break-glass) and #54 (company secretary missing, or a sole director as secretary) |  |
| OC-15 | Appendix C.2: every check runs from P1 unless its row names a later phase |  |
| OC-16 | CTL-FIN-02 and CTL-AUD-05 name their nightly checks |  |
| OC-17 | CTL-DP-06 names its event |  |
| OC-18 | CTL-STAT-03: IR21 at least one month before cessation |  |
| OC-19 | QA-ENG-01 and QA-ENG-06 say what they block |  |
| OC-20 | CTL-FIN-11/12 say what they reconcile to |  |
| OC-21 | QA-RPT-03 clarified |  |
| OC-22 | §G.2: only the CPF and net columns of the wage-700 row are re-derived; SDL S$2.00 stands (minimum [verify]) |  |
| OC-23 | §G.2: "SPR from the third year" (full rates) [verify] |  |
| OC-24 | §J.2 UC-PRJ-02: FR-PRJ-04 removed |  |
| OC-25 | FR-PAY-10 Phase "P2 / P1 (IR21 hold)" |  |
| OC-26 | FR-PAY-16: not settled for final pay [verify with the accountant]; until then no recovery beyond written consent, and the open advance is shown |  |
| OC-27 | FR-PRJ-01/02 raised to M; FR-PRJ-03 superseded |  |
| OC-28 | FR-EMP-01: one field, the effective-dated override of FR-PRJ-06 |  |
| OC-29 | FR-PAY-07: 2110 also holds SHG |  |
| OC-30 | FR-FA-01: at or above the threshold |  |
| OC-31 | ENG-33 cells: "S / M (update marking)", "P2 / P1 (update marking)" |  |
| OC-32 | C.2 #16 has both triggers |  |
| OC-33 | B.5 rows for 2700, share application monies, 7040, 5040, 2120 [confirm in the CoA review] |  |
| OC-34 | §10.7 intro maps the workbook checks correctly |  |
| OC-35 | Payout rule moved into the rev 0.7 payment-provider row |  |
| OC-36 | B.1: by design (a bank line has no service period) |  |
| OC-37 | B.2: the bill net leg uses `RND-FX` |  |
| OC-38 | FR-AP-04 Phase P1 |  |
| OC-39 | Duplicate receipts and bills are blocked (✖) until a person records why |  |
| OC-40 | FR-AR-16: delivery and document status shown separately |  |
| OC-41 | FR-AR-05 (time of supply), FR-AR-07 (29th–31st), FR-AR-11 (date of supply) defined |  |
| OC-42 | FR-BNK-03: "licensed aggregator" superseded |  |
| OC-43 | Rules moved into FR-AP-15, FR-INT-22, FR-BNK-16 and FR-PSP-01 |  |
| OC-44 | FR-DOC-03 superseded (part of FR-DOC-12) |  |
| OC-45 | FR-DOC-02 applies to committed files; draft files stay removable |  |
| OC-46 | AUD-06 reconciled with the security-log retention |  |
| OC-47 | FR-INT-22: webhook or IMAP polling |  |
| OC-48 | FR-INT-23 adds Zoho Projects and, in P3, ServiceNow |  |
| OC-49 | FR-NTF-03: info and warning banners use `role="status"` |  |
| OC-50 | FR-INT-02 Phase "P2 / P1 (client classes)" |  |
| OC-51 | FR-INT-04 points Slack to FR-NTF-04 |  |
| OC-52 | [verify] added to the ZeptoMail rename and Resend GA |  |
| OC-53 | §9.6 tier 1 definition: reversible, no cash movement; may post reversible entries | 2nd |
| OC-54 | `recur.create_template`: Controller approves above the limit | 2nd |
| OC-55 | AI-02: the never-send items cannot be removed; the Owner may only add exclusions | 2nd |
| OC-56 | AI-P8: the Owner and Compliance see all, as AI-12 states |  |
| OC-57 | FR-KPI-01: revenue vs budget and cost vs budget are two tiles |  |
| OC-58 | FR-EQ-04: fifth choice, shares instead of pay [verify the valuation basis] |  |
| OC-59 | Exempt-private-company status lives on FR-CS-01 |  |
| OC-60 | §8.22 and FR-EQ-05 cite FR-EQ-18/20 |  |
| OC-61 | G4-101: Not assessed |  |
| OC-62 | No PRD change: capability placement only |  |
| OC-63 | §7.8: Basic adds ⌘J and the mode shortcut |  |
| OC-64 | Appendix D notes: integration clients, Super admin platform health |  |
| OC-65 | §7.1: Payroll is feature-switched |  |
| OC-66 | FR-VIZ-06 (labels and legend), FR-UX-20 (plain decimals), FM-2 (at or below), §6.2 (break-glass always audited) |  |
| OC-67 | NFR-COST-02: only the listed fixed-fee items are billed while idle |  |
| OC-68 | §12.12: the "only Sonnet 4.6" line superseded |  |
| OC-69 | NFR-OBS-02: the platform owner first, the Owner by escalation |  |
| OC-70 | REL-01 adds `deps` |  |
| OC-71 | SEC-27: read-only root where the platform supports it; NFR-IMG-02 controls on Cloud Run |  |
| OC-72 | §12.12: `.env` |  |
| OC-73 | PostgreSQL 17–18 [verify 18 on each cloud] |  |

Revision 0.12 (same day, founder request) added §9.9.1 (AI-70…AI-79: model providers, billing
modes and per-agent routes), C.2 #55 and open question 46; it also needs a second reviewer (AI
surface). Sample-data clashes in the screens were fixed against `docs/design/screens/sample-data.json`,
which `build.py check` now enforces.
