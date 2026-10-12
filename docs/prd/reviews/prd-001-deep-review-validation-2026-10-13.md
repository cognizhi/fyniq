# PRD-001 deep review: validation and dispositions · 13 Oct 2026

> **Renamed (13 Oct 2026, PRD rev 0.9).** The product called "Cognizhi Finance Hub Cloud" ("FH Cloud",
> "Finance Hub") in this record is now **Fyniq**, and identifiers prefixed `fh` are now `fyniq`
> (for example the `fh-*` skills are `fyniq-*`). Links were updated; the text is otherwise as written.

| | |
|---|---|
| Review validated | [Deep review of 12 Oct 2026](prd-001-deep-review-2026-10-12.md) (F01–F50, O01–O12, the §8 contradiction table, the §11 scenarios) of PRD-001 revision 0.7 (SHA-256 `dea4422d…5990ea`, commit `9d849a7`) |
| Result | PRD-001 revision 0.8 applies every claim that held, in the form recorded below |
| Validator | Claude Code (Opus 5.5), at the founders' request: "validate every claim … do not simply agree", then "enhance the PRD" |
| Method | Each finding split into its claims: PRD text (quoted and checked by line), cited requirement IDs, arithmetic (recomputed), and external facts (the cited page fetched; official sources preferred). PostgreSQL behaviour was tested on a local 18.6 container with no network. Searched the whole PRD for text that already covered a point, and the rev 0.1 review (REV-01…40) for duplicates. Two high-stakes facts were rechecked by hand: Ory's edition matrix and Google's Sonnet 4.6 page |

## 1. Verdict

The review is **sound and worth adopting**. No finding is wrong as a whole. Every finding names a real
gap or contradiction, and the arithmetic reproduces exactly: 493 / 377 / 319 scope rows, 56 `[verify]`
markers, S$0.0333 on S$1 over six months, S$40.75 → S$40, 3% of S$100,000.

Validation still changed a lot of detail:

- **Overstated or wrong in part** (17 findings): the PRD already covered part of the point, or a
  sub-claim does not match the text. Examples: the "24-hour idempotency window" (F21) is the client
  API key, not financial uniqueness; issued reports are already stored with a hash (F26); the PRD
  says the WebAuthn challenge *includes* the hash, not that it *is* the hash (F28); the "AR credit
  balance is rejected" rule does not exist (F22); ENG-40's averaging is neither silent nor
  untraceable (F08); "current evidence resolves" Sonnet 4.6 in Singapore is too strong, because
  Google's own pages disagree on processing (F44).
- **Misread references**: the "§1 phase table" is the front-matter table; NFR-UI-01 does not exist;
  §2.3 is mis-cited for overseas purchases (consulting is something Cognizhi sells); the PDPC link
  returns 404 (the current PDF, revised 29 Apr 2026, says the same thing); EDG closed on
  29 Sep 2026 (EDGE replaced it).
- **Missed by the reviewer** (each applied in rev 0.8): `NaN` passes the money CHECK and a "positive"
  rate CHECK; `FOR KEY SHARE` does not block a close's status update; two MRR definitions; the per-
  employee SDL cap also breaks across runs; CPF splits change EE/ER by S$1; ≤ 5-year retention would
  breach the statutory minimum; the RPO, not only the RTO, fails for a region loss; Opus 4.6
  retires not before 5 Feb 2027, around go-live; Google spend caps exist and must not be put on
  Cloud Run; an SVG logo upload (FR-ORG-09); double dunning on provider-billed invoices; annual
  controller notices that the research note had and the PRD dropped; the IR21 hold covers
  reimbursements; INV-07 omitted the deferral and claims accounts; Appendix D gave the Controller
  unmasked payroll; the proposal executor still said "worker pool".
- **Severity changes**: F02 H → M (a founders' decision record, not a defect); F21 B → H (durable
  uniqueness already exists; key derivation is missing); F29 B → H (the exposure is
  misclassification, not cash); F49 split (H for P1 exports, SVG and Office files; M for P2 offline
  receipts). The rest stand as rated.

Decisions that belong to the founders, the accountant, the tax agent or the DPO are **not** decided
in requirement text. They are open questions 25–45 with recommended defaults (§20), and §17.4 makes
recording those decisions an entry criterion for the P1 build.

## 2. Findings F01–F50

Verdicts: **Valid** (every claim the remedy depends on holds) · **Partly** (a claim that drives the
recommendation is overstated, wrong or already covered) · **Unverified** (none).

### Product, scope and governing requirements

| ID | Verdict | What held | Overstated, wrong or already covered | Missed by the reviewer | Applied as |
|---|---|---|---|---|---|
| F01 | Valid (H) | All eight named capabilities are M/P1; §17.2 still says 12–16 weeks; R2 lists each increase. Counts reproduce: 493 / 377 / 319, 56 markers | 26 of the 377 "P1" rows are split phases (351 purely P1) | The front-matter P1 row and §17.2's activity list are stale too | Phase table, §17.2, R2; open question 25; §17.4 |
| F02 | Partly (M) | Xero S$70/95 incl. GST, rising 1 Nov 2026; Zoho S$36/30 and S$54/45 excl. tax; Talenox S$21.80 for five incl. GST; Xero SG payroll only through partner apps | §5.5 already holds these prices; the PRD never concludes that building is cheaper; R6 covers statutory change as a risk | §5.5 mixes GST-inclusive and exclusive prices; the build rationale rests on undecided open question 1 | §5.5 (dated, tax basis stated), §12.6, A3, default 4, G9; open question 26 |
| F03 | Partly (M) | UX-T3 untargeted; close effort mixes minutes and days; 80% counts unedited AI approvals; builders rate their own product | Incident handling is defined (§4.3, QA-REL-01); FR-UX-03 and AI-32 already block bulk tier-2/3 approval | AI-33 (rubber-stamping guard) is S/P2 while proposals ship in P1 | §4.3 rows, UX-T1/T3/T5 |
| F04 | Valid (H) | Stale phase cells (FR-PLN-05, FR-PRJ-04); P0 exits rely on P1 rows (AUD-03, NFR-IMG-05, SEC-22); FR-ADM-08's Zoho test not in §17.3 | A registry is heavier than needed: the tables already are the registry | §7.6 (7 M rows) and §15 (27 rows) had no phase; FR-EQ-22 had a priority in its phase cell; revision history proves the moves were intended | QA-TRACE-01; Phase cell authoritative; columns added to §7.5, §7.6, §15; phase splits; §17.3 item 7; FR-EQ-28 |

### Accounting and statutory correctness

| ID | Verdict | What held | Overstated, wrong or already covered | Missed by the reviewer | Applied as |
|---|---|---|---|---|---|
| F05 | Valid (B) | §4.3 compares with the *unrounded* value, so 0.20 vs 0.1667 breaks "< S$0.01"; worst case (n−1)²/n cents (S$0.58 at 60 months); QA-MIG-01 fails on the wage-700 row and on revaluation | — | Month-end balances drift too; the golden fixtures divide evenly, so tests never saw it; paid payroll history must keep its paid amounts | `ALLOC-EVEN` → cumulative rounding (old rule kept as superseded; accountant confirms); migration difference register (FR-MIG-02, QA-MIG-01, M4); §4.3; QA-ENG-03 |
| F06 | Valid (B) | P1 applies framework-dependent policies (FR-EQ-06/18, FR-PSP-03) while FR-RPT-11 is C/P3 [verify]; ACRA: third edition for periods from 1 Jan 2027 | "Statutory statements in P1": P1 has management statements | The decision is needed by opening balances, not P3 | FR-ORG-10; FR-RPT-11; open question 31 (SFRS for Small Entities eligibility kept [verify]) |
| F07 | Valid (B) | IRAS earlier-of rule; four different date words in the PRD; FR-AR-09 deposits without GST | Spreads already keep GST out (B.1/B.2) | Input-tax claim date may follow recording (e-Tax Guide fn 9); the cash-basis [verify] can be retired (Cash Accounting Scheme) | ENG-45; ENG-01, B conventions, §2.2, §10.2, FR-AR-09, FR-ORG-03 |
| F08 | Partly (B) | IRAS: claim the SGD GST on the supplier's invoice or the import permit; ENG-42 and B.2 recompute it | ENG-40 averages only as the third fallback, stores its source and is flagged (C.1 #9) | SGD bills with per-line rounding have the same defect; no GST rate-source rule | ENG-42 (sales and purchases separated), B.2, ENG-40, FR-DOC-12 |
| F09 | Valid (B) | IRAS accepts simplified tax invoices (≤ S$1,000) and employee-as-agent purchases; FR-EQ-02, B.7, UC-EXP-01 deny them | §8.22 prose already had the agency exception; the rows dropped it | `docs/user-guide/founders-money.md` repeats the rule (follow-up below) | FR-AP-20; FR-EQ-02, B.7, §8.22, UC-EXP-01, FR-DOC-12 |
| F10 | Valid (B when applicable) | e-Tax Guide ¶6.3/6.4 (non-network parties still submitted), ¶7.9 (aggregation limits), ¶12.14–12.15 (outage, contingency) | "Acceptance": IRAS's ID proves **receipt** | The F5 gate used the wrong proof (Access Point acknowledgement); Model B invoices must be submitted | FR-TAX-10, FR-TAX-11; §14.2, CTL-STAT-01, FR-AR-12, FR-PSP-03 |
| F11 | Valid (B when GST used) | IRAS next-F5 concession (S$3,000 and 5% of Box 4, not Box 12); F7 supersedes; Box 8 = Box 6 − Box 7; "boxes = control movement" ignores payments and refunds | — | Five-year limit; repay input tax on bills unpaid 12 months | FR-TAX-12; FR-TAX-02/03; INV-07 roll-forward; §14.2; C.2 #46–48 |
| F12 | Valid (B; cash effect under S$1) | CPF Board: add up, round the total down; S$40.75 → S$40; the G.2 March seed is S$70.75 → S$70 | — | The per-employee S$11.25 cap breaks across runs in a month | FR-PAY-03, `RND-SDL`, B.3, G.2 note; open question 27 |
| F13 | Partly (H, Confirmed) | Rounding is on the calendar-month total; two runs give EE/ER 600/512 instead of 601/511 | "Prior-employer YTD": the AW ceiling is per employer | The PRD implicitly forbids two pay records per employee-month, so off-cycle runs have no path; M3 imports by FY, not calendar year | B.3 "employee-month" row; `RND-CPF`; FR-PAY-14 (AW true-up and PR elections kept [verify]) |
| F14 | Valid (B when paid) | MOM: payslips with payment or within 3 working days, and with outstanding salary on termination; IRAS: withhold **all** monies from awareness of cessation; MOM deduction limits | — | Reimbursements are in the IR21 hold; §14.2 and FR-PAY-10 contradicted each other | FR-PAY-15 (go-live gate), FR-PAY-16 (deductions), FR-PAY-08/10, FR-AP-17/18, §14.2, C.2 #44–45; open question 28 |
| F15 | Partly (H for WHT; M for reverse charge) | IRAS distinguishes work done in Singapore from remote services, and copyright rights from copyrighted articles | §2.3 mis-cited; the PRD assumes nothing about full input-tax recovery — it lacks the applicability rule | Before registration, overseas vendors charge 9% GST that is a cost | FR-AP-19; FR-TAX-01; open question 29 |
| F16 | Valid (H input; M Excel) | Reproduced on PostgreSQL 18.6: 1.230001 stored as 1.2300, passes the CHECK; Excel keeps 15 significant digits | CSV is already exact; QA-RPT-03 already re-reads XLSX | `NaN` passes the money CHECK and `rate > 0`; ENG-11 said "significant decimal places" where `NUMERIC(20,10)` gives decimal places | ENG-44; ENG-10/11, §11.3, FR-RPT-05, QA-RPT-03, QA-ENG-05; open question 30 |
| F17 | Valid (B) | B.2 "(or marked fully paid)" has no cap or approval and goes to 7030; G.3 pins a forced 120.00 | — | An overpayment within 3% is booked as income; the only approved write-off route was P2 | FR-BNK-08, B.1, B.2, §10.2; FR-AR-17; C.2 #49; open question 32; migrated forced-paid items kept |
| F18 | Valid (B, fails closed) | INV-05 rejects a zero-USD, non-zero-SGD line; no base-only rule | The reversal method already exists | FR-GL-04 omitted 2500, 2120, 1250 and 2700 balances; posting revaluation breaks parity | INV-05 exception; FR-GL-04; §10.2; G.3 note |
| F19 | Partly (B when Model B live) | UC-PSP-01 posts from the charge: no AR leg, no unpaid or failed path | "Straight-line is not universal": normal for plain subscriptions, and FR-PRJ-11 covers projects | No GST leg in UC-PSP-01 | FR-PSP-07; UC-PSP-01; revenue policy in FR-ORG-10 and open question 31 |
| F20 | Valid (B when live) | §10.2's rev 0.1 payout row debits fees again; Stripe's itemised report needs automatic payouts | "Provider-wide zero": FR-PSP-06 already checks per payout | Clearing must be per provider and currency; how Stripe and HitPay charge GST on fees is unverified | §10.2 row superseded; FR-PSP-05/06; J.4; UC-PSP-04; C.2 #50 |
| F21 | Partly (H) | Stripe: different events can describe one object; no ordering guarantee | The 24-hour window is the client API key; §11.4 already has a durable UNIQUE key; mirrored transfers are covered | The natural key is Stripe's balance-transaction ID | FR-PSP-01 posting identity; §11.4 transport key vs posting identity; FR-BNK-04/16; C.2 #51 |
| F22 | Partly (H) | The invoice chain mixes independent states; no allocation, refund or deposit rules | No PRD rule rejects AR credits (QA-ENG-03 allows them) | INV-07 said "open documents" only | FR-AR-03, FR-AP-02 states; FR-AR-18; INV-07; §10.2 refund row |
| F23 | Valid (B when used) | No account for cash before allotment (it falls into 2999); no SAFE form or denominator | Option-pool timing already in FR-EQ-21 | Under post-money terms the G.3 SAFE gives 52,631 shares, not 52,083; the G.3 cap-plus-discount form is not a standard YC form | B.7 and §10.2 share application monies; FR-EQ-17/18; G.3 second fixture; open question 33 |
| F24 | Valid (H) | ENG-04 says "every … KPI … reads the ledger"; several KPIs use other sources | The semantic layer (AI-03) plus FR-KPI-03 already is the measure registry | Two MRR definitions; undefined states exist only in the workbook PRD | ENG-04, FR-KPI-02/03, principle 1, J.4; open question 34 |
| F25 | Valid (H) | "Direct costs" undefined, so tagged salary counts twice; a published cost-rate formula recovers a salary (65.54 × 173.33) | — | No payroll-cost account flag; the cost-rate formula differs between FR-PRJ-02, FR-PRJ-06 and G.3 | FR-PRJ-12, FR-PRJ-06/02/01, FR-COA-02 flag, Appendix D row |
| F26 | Partly (B for the guarantee) | "High-water mark" defined nowhere; `now()`, `uuidv7()` and sequences come before commit | Presentation half: FR-DOC-04, REL-05, FR-TPL-04 and account versions exist | The report cache would serve a stale report after a rename | ENG-46 (ledger revision); ENG-03, FR-RPT-08, §11.4, §11.6, §11.7, AI-65; glossary |
| F27 | Valid (B) | No isolation level, lock order or retry rule; INV-03 is a bare trigger | Account deactivation is low-consequence; approval expiry belongs to F28 | `FOR KEY SHARE` does not block the close; CTL-FIN-03 is only detective | ENG-47; INV-03, FR-GL-07, AUD-04, QA-ENG-05 race tests |

### Approvals, identity and permissions

| ID | Verdict | What held | Overstated, wrong or already covered | Missed by the reviewer | Applied as |
|---|---|---|---|---|---|
| F28 | Partly (B on the payment path) | Only "the payload" is hashed; no random, stored, single-use challenge; no dependency versions; no authority recheck at execution | The PRD says the challenge *includes* the hash; AI proposals already have `base_versions` | TOTP approvals give no cryptographic binding | FR-APR-10 (approval envelope); FR-APR-03/05; §9.5 |
| F29 | Partly (H) | AI-10 lets the requester approve their own tier-2 journal, bypassing SOD-3 and FM-2; bank categorisation and matching post | AI-30 already closes the draft path; FR-BNK-14 already needs an approver for auto-apply; S$1m is misclassification, not cash | §9.6 and Appendix E disagree on tier-1 approvers | AI-10; Appendix E preamble and limits; FR-BNK-14 |
| F30 | Partly (B for recovery and succession) | No recovery or succession path; email-only linking is weaker than OIDC Core §5.7 | Issuer scoping is implied by "one provider per issuer" | "The last Super admin cannot be removed" blocks orderly succession | SEC-35; FR-ADM-09; FR-ADM-07/08; open question 36 |
| F31 | Valid (B) | FM-8 has no action list | Not unconditional: FM-8 is an explicit, time-bound exception | It contradicts CTL-FIN-08 and the Owner row; "break-glass" also names SEC-03's production access | FM-8 lists; Owner row; CTL-FIN-08; SEC-03; open question 35 |
| F32 | Partly (B for payroll) | The report-cache key had no authorisation scope; salary is inferable; AI logs reveal payroll to Compliance | FR-RPT-07 send-time check, AUD-02/05 and AI-54 exist | Appendix D gave the Controller unmasked pay runs, against FR-PAY-13 | SEC-34; §11.7 key; FR-PAY-13; FR-RPT-09; AI-12; Appendix D |
| F33 | Valid (B) | PostgreSQL: FK and unique checks bypass RLS; TRUNCATE is not subject to it; single-column keys | No owner, BYPASSRLS or FORCE gap; TRUNCATE already banned on ledger and audit | Pack definitions are global, activations are tenant rows | §11.4 composite keys, trust boundary, global tables; INV-04; QA-ENG-05 |

### Data lifecycle, resilience and architecture

| ID | Verdict | What held | Overstated, wrong or already covered | Missed by the reviewer | Applied as |
|---|---|---|---|---|---|
| F34 | Valid (B) | Bucket policies count from upload; a 4 Jan upload loses its lock 361 days before the promise | The app's purge is already FY-based; what lapses is the lock | The 5-year option and multi-year contracts fall *below the statutory minimum*; IRAS's period is 5–6 years, not "about 6"; locked buckets cannot remove draft files | FR-DOC-02 (per-object retain-until), FR-DOC-13 (two-phase upload), DR-07, §11.9, §14.2, NFR-RET-01, CTL-AUD-03, default 6 |
| F35 | Valid (B for design) | "Every posting, forever" vs purge; one key per person cannot be shredded early; backups resurrect wrapped keys; PDPC retention is purpose-based | FR-UX-14 is about user actions | PDPA has no general right to erasure (GDPR framing); §11.8 cited a §20 row that did not exist | L1, FR-UX-14, AUD-06/08, AUD-09, §11.8, §11.9; open questions 38–39 |
| F36 | Valid (B for the promise) | All copies and keys are in `asia-southeast1`; NFR-DR-02 promised 4 hours for a regional outage | — | The RPO fails for the region too; no off-project copy was named | NFR-DR-01/02 per scenario; DR-04, DR-06; open question 37 |
| F37 | Valid (B) | Named-task dedup is 24 hours; Cloud Tasks is at-least-once and deletes exhausted tasks, so the sweep missed them | The executor already re-validates approved work | Restore after > 24 hours re-dispatches executed rows; ZeptoMail `client_reference` prevents nothing | ADR-T7 outbox contract; OIDC caveat; FR-INT-21; §12.3 executor row |
| F38 | Valid (H) | The tick is its own watchdog; burn-rate alerts stay silent with no traffic | Deadlines are found while the tick runs; `sec.create_task` already deduplicates | Nightly verification and anchoring share the blind spot; absence alerts need a series that has reported | ADR-T7 step 5; NFR-OBS-02; CTL-RES-05; AI-64/65 |
| F39 | Valid (H) | Cloud Run bills while a connection is open, up to 60 minutes | NFR-UI-01 does not exist | §12.3 and ADR-T7 point to a cold-start target that did not exist | FR-UX-19; NFR-COST-02; NFR-PERF-06 |
| F40 | Valid (B) | Production builds exclude local adapters and refuse other `FH_ENV` values, yet `make up` and QA-LOCAL-01 run them offline | Replay vs live evals already separated (R17) | `fh-local-dev` repeats the contradiction | ADR-T4 two image flavours; `make up`; DEV-07; QA-LOCAL-01; skill updated |
| F41 | Valid (B for ADR-T3) | Ory's matrix: token exchange and stateless JWT are not in OSS (rechecked by hand); the v26.2.0 source has no RFC 8693 handler | JWT-format tokens, refresh rotation and introspection are in OSS | No Hydra version pinned; opaque tokens with introspection meet the 60-second revocation | ADR-T1, ADR-T3, AI-36, §12.9, §12.6, §17.3 item 6, ADR index; open question 40 |
| F42 | Partly (H) | Cloud Run has no read-only root; huma is code-first; no unit of work in the storage port | Business modules already share the `api` service | The executor "worker pool" row | NFR-IMG-02; §12.4 contracts; NFR-PORT-07; NFR-PORT-04 |
| F43 | Valid (H) | The $250–330 total still includes the warm instance (recomputed ≈ $220–315); scanner, email, Access Point and others unpriced | — | Google spend caps exist and would pause Cloud Run; NFR-COST-02 omitted the load balancer | §12.6 rows; NFR-COST-03/04; default 9 |
| F44 | Partly (H) | §12.12 and the ADR index were stale; both 4.6 models have `asia-southeast1` endpoints (Sonnet page rechecked by hand) | Google's data-residency table confirms Singapore processing for Opus 4.6 but not Sonnet 4.6 | Opus 4.6 retirement not before 5 Feb 2027; default 7's "no transfer assessment" reason ignored Zoho (US) and email | §12.5, Option A, §12.12, ADR index, R13, default 7; research-note correction |

### AI, integration safety and assurance

| ID | Verdict | What held | Overstated, wrong or already covered | Missed by the reviewer | Applied as |
|---|---|---|---|---|---|
| F45 | Valid (B for the accuracy claim) | AI-08 checks only that a value appears; it lets "unverified" figures through | — | The skill's "summary from the payload" rule was not in the PRD | AI-68 (typed facts); AI-07/08; QA-AI-01 |
| F46 | Valid (H) | Number lint is a substring test across all sources; "published" forces "trusted"; raw fallback not reader-filtered | The reviewer keeps the no-vector decision (AI-58), as the founders decided | Numbers in code spans are never checked | AI-52/53/54/56; ADR-T6 wording; QA-AI-06 (`kb.py` and `kb/schema.md` follow-up) |
| F47 | Partly (H) | Class labels cannot detect a model behind an `integration` credential; in-catalogue injected proposals untested | FR-INT-02 and AI-34 already admit it for tokens and unclassed clients | No rule on how assistant output is rendered | AI-P2, AI-34, AI-41, AI-69, QA-AI-03 |
| F48 | Valid (H) | FR-INT-27 binds connectors to the AI never-list; three different "disable" timings; no SSRF control | — | Also InvoiceNow transmission and routine email; tasks scheduled up to 30 days ahead | FR-INT-20/26/27; SEC-36; QA-SEC-04 |
| F49 | Partly (H for P1 items) | No CSV-injection, ZIP, XXE or SVG rules | Unicode is already handled (FR-BNK-07); offline receipts are S/P2 | The SVG logo (FR-ORG-09); ASCII minus means neutralising depends on cell type | FR-RPT-05, FR-DOC-07/08, FR-ORG-09, FR-TPL-02, FR-UX-16, QA-SEC-05 |
| F50 | Valid (H) | ACRA: board-appointment and influence tests, annual notices, nominee-shareholder registers (Central RONS since 16 Jun 2025) | Appendix I already discloses its "[summary]" sources; ENG-30 already holds source, date, verifier | The research note had annual notices; ACRA's own pages disagree (5% vs >25%; >25% adopted); the 25% `[verify]` can go | FR-EQ-25, FR-CS-01, §14.2, AI-65 Not assessed, ENG-30/33, QA-AI-07, Appendix I rule; open question 41 |

## 3. Contradiction table (review §8)

| Row | Verdict | Resolution in rev 0.8 |
|---|---|---|
| FR-PLN-05 phase | Valid | Phase cell P1 (rev 0.6 intended it); §18 P2 cell corrected |
| FR-PRJ-04 phase | Valid | Superseded by FR-PRJ-10, phase P1 |
| FR-PSP-03 vs FR-AR-15/FR-KPI-04 | Valid, narrower | Deferred revenue is already P1 (FR-GL-03); MRR and counts stay P2; open question 34 |
| §1, §3.3, §17.2, §18 stale summaries | Partly | §1 proper was fine; front-matter table, §3.3 and §17.2 corrected; Phase cell made authoritative |
| §18 tax row vs FR-TAX-04 (IAF) | Valid | §18, §3.3, §5.3 and Appendix A corrected |
| §4.3 and QA-MIG-01 vs B.3/G.2 | Valid | Migration difference register |
| §4.3 vs `ALLOC-EVEN` | Valid | Cumulative rounding; bound restated |
| ENG-04 vs KPIs, projects, equity | Valid | ENG-04 and FR-KPI-03 |
| FR-GL-08 vs FR-GL-09 | Partly: wording, not a real contradiction | "Closed" (hard lock) vs "soft-locked" named |
| §10.2 payout rows vs FR-PSP-02 | Valid | rev 0.1 row superseded; fee posts once |
| DEV-07 vs `make up`/QA-LOCAL-01 | Valid | Two image flavours |
| §17.3 vs AUD-03/NFR-IMG-05/FR-ADM-08 | Valid | Phase splits; §17.3 item 7 |
| ADR-T2/§12.5 vs §12.12 | Partly | Endpoints confirmed for both; Sonnet 4.6 processing stays [verify] |
| AUD L1, FR-UX-14 vs §11.9 | Valid | L1, FR-UX-14, AUD-06/08 scoped; open question 38 |
| DR-04 vs NFR-DR-02 | Valid (the RPO too) | NFR-DR-01/02 per scenario; open question 37 |
| FM-1 vs FM-8 | Partly: explicit exception, unbounded scope | FM-8 lists; CTL-FIN-08 and Owner row |
| AI-63/65/66 vs `sec.create_task` | Valid | Deterministic findings create tasks through the system principal; model-suggested tasks stay proposals |
| FR-INT-27 vs later bank and tax capabilities | Valid, broader | Never-list binds what an AI may cause, not the transport |
| FR-UX-34 vs tour anatomy | Valid, editorial | Eight named destinations folded into seven, as in the prototype; the "redesign" remedy was not needed |
| QA-UI-06 vs FR-UX-34 | Valid | Only `user_help_state` may be written during the tour |
| §20 defaults | Valid; the aggregator point slightly overstated | Default 3 rewritten; "a default is not a decision"; Q17 marked answered; Q3 needed-by moved to P1 build |

## 4. Opportunities O01–O12 and the deferral list

The review's own rule (do not expand P1) was applied: P1 gets only one-clause amendments to existing
M/P1 rows; everything else is S or C in P2, or an open question.

| ID | Verdict | Note | Applied as |
|---|---|---|---|
| O01 | Partly | Not "new": extends FR-DIM-03 and §3.3; FOCUS is now at 1.4 | FR-DIM-03 widened to S/P2; open question 42 |
| O02 | Partly; premature | No usage pricing in the PRD; Stripe now recommends Metronome for new usage integrations | Open question 42 |
| O03 | Valid | No register existed | FR-AP-21 (S/P2); FR-PLN-09 clause |
| O04 | Partly | External substantiation already exists for bank, clearing and equity | INV-07, FR-CTL-05, C.2 #2 (P1); close evidence row; C.2 #52, FR-RPT-13 (P2) |
| O05 | Valid | Double dunning missed by the reviewer | FR-PSP-03 collection state (P1); FR-AR-08 disputes (P2) |
| O06 | Partly | Bank side already done (FR-BNK-16) | FR-PSP-01 completeness sweep (P1); C.2 #43; FR-INT-27 re-ingestion preview (P2) |
| O07 | Partly | Mostly in FR-PLN-09/02/03 | FR-PLN-03 with and without funding (P1); FR-PLN-04 accuracy (P2) |
| O08 | Partly; premature | FR-DIM-01 already has "Funding source"; EDG closed 29 Sep 2026 | Grant-claim evidence row; open question 43 |
| O09 | Partly | Snapshots and adjusting journals already exist | FM-7 review record |
| O10 | Partly | FR-AR-15 already names contract changes | FR-AR-15 amendments; FR-PRJ-09 change orders (P2) |
| O11 | Valid; premature | Rights recorded but never computed | FR-EQ-16 clause; FR-EQ-29 (C/P2) |
| O12 | Partly | §5.2 promised an "unknown" state that FR-UX-09 never had | Not assessed in FR-UX-09, CMP-01, FR-CS-03, QA-AI-07 (merged with F50) |
| Defer: template editor | Partly | Code mode already P2, but the visual editor and diff are M/P1; a founder request | Open question 44 |
| Defer: multiple providers | Valid | Two email providers, Stripe and HitPay, Wise and Airwallex all P1 | Open question 45 |
| Defer: ticketing, A2A, workflows, marketplace | Already met | All S/P2, C/P2 or P3 | None |
| Defer: peer benchmarking | Partly | Already C/P3; a count alone is not disclosure control | FR-KPI-08 |
| Defer: complex instruments, packs | Partly | Mostly deferred | FR-EQ-16 "recorded, not computed" |
| Defer: certifications | Already met | §14.8 and default 8 | None |
| Waves A–D (review §10) | Compatible with P0–P3 | The PRD gated P0 on §20 answers but had no gate before P1; the review's §9 and §10 disagree on where the P1 minimums go | §17.4 P1 entry criteria |

## 5. Acceptance scenarios (review §11)

All 29 rows trace correctly. Several were split (they combined unrelated tests) or sharpened (the F17
row now says the residual stays open *above* tolerance; the F23 row says where the cash sits; the
F29 row distinguishes Standard from Founder mode). Together with rows drawn from each finding's own
acceptance text, they form Appendix G.4 (110 rows, G4-001…110), each traced to its finding and
requirements.

## 6. Not settled by this validation

Kept as `[verify]` in the PRD, or open questions:

- SFRS for Small Entities eligibility criteria (secondary sources only).
- The part-payment deposit rule (IRAS page returned 404) and the GST transitional rules for
  registration and rate changes.
- The CPF graduated formula, the AW estimate and true-up rule, and the joint full-rate election.
- The IR21 scope for SPRs; whether director balances fall inside the IR21 hold.
- Employment Act coverage for payslips and deductions.
- The withholding-tax filing date.
- How Stripe and HitPay charge GST on their fees; HitPay's list API and collection-state fields.
- Sonnet 4.6 Singapore processing.
- Whether Hydra ignores or rejects `resource`; Client ID Metadata Documents.
- Resend's idempotency window; Cloud Monitoring absence-alert limits.
- Companies Act s199(2) wording (the official statutes site refused the fetch).
- The Regulations behind the 7-day and 2-business-day controller periods.
- EDGE claim deadlines.

## 7. Follow-ups outside the PRD

Updated in the same change, because they restate rules that rev 0.8 corrected:

- the `fh-accounting-domain` skill and its posting-rules reference;
- the `fh-quality-gates`, `fh-bug-fix`, `fh-sg-statutory`, `fh-local-dev` and `fh-mcp-server` skills;
- dated correction lines in `research/bank-payments-cloud-2026-10-12.md` (F44) and
  `research/secretary-xero-integrations-2026-10-12.md` (F50);
- a "Read first" row in `AGENTS.md` for this record.

Left for their own change, each with its required process:

- `docs/user-guide/founders-money.md` (lines 55–56) repeats the company-name-only GST rule. Fix it
  under `fh-knowledge-base`: recompile the affected `kb/` pages, then `make kb-index` and `make kb-lint`.
- `kb/schema.md` rule 8 and `scripts/kb.py`: let an approved page stay untrusted (AI-54); check
  numbers per cited section as whole tokens, including code spans (AI-52).
- `agents/company-secretary/SKILL.md`: checklist row 12 needs all three controller tests, annual
  notices and nominee registers, and deterministic task creation (AI-65). A change to an agent
  skill needs QA-AI-07 and the dual-approved change record (AI-61).
- The `fh-ai-proposals` skill: typed facts (AI-68) and scheduled-agent outputs (AI-65).

## 8. Calls made while applying (for the founders and the accountant to confirm)

These are the validator's choices, not the reviewer's and not yet the founders':

- **`ALLOC-EVEN` changed to cumulative rounding** instead of correcting the §4.3 bound. This makes the
  existing §4.3 promise true: every month within S$0.01 of the workbook, and every month-end balance
  within S$0.005. The old rule is kept as superseded, and the accountant confirms the policy at the
  P1 rule review (A3). If the accountant prefers the old rule, restore it and restate the §4.3 bound
  instead (up to n − 1 cents in the last month).
- **"Not assessed" is a labelled ⚠ variant**, not a new status chip, so MASTER.md and the token set
  do not change. A separate chip would be a design-system change.
- **Recommended defaults supplied** (all settings or open questions, marked to confirm):
  - a settlement write-off cap of S$50 per document and S$200 per month (question 32);
  - a break-glass payment cap of S$20,000 (question 35);
  - S$50,000 in total per bank-categorisation proposal (Appendix E);
  - a 24-hour cooling-off after account recovery (SEC-35);
  - a 24-hour export-link life with 60-second storage URLs (FR-RPT-09);
  - a 3-person threshold for payroll cost groups (FR-PAY-13);
  - a 2% target for corrected AI categorisations (§4.3);
  - a 5-minute hidden-tab limit for live updates (FR-UX-19);
  - a 2-second cold-start target (NFR-PERF-06);
  - a 30-minute heartbeat alert (NFR-OBS-02).
- **New P0 requirements**: ENG-44, ENG-46, ENG-47 and NFR-PORT-07 are P0, because the columns and
  constraints they need are cheap in the first migration and costly after go-live.
- **The rev 0.7 Claude-in-Singapore statement** was corrected on Google's current pages. The Sonnet 4.6
  processing guarantee stays [verify].
