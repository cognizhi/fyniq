> **Record copy (13 Oct 2026).** This is the external deep review as delivered on 12 Oct 2026, copied
> into the repository so the PRD can link to it. Only the link to the PRD on the next lines was
> made relative; the text is otherwise unchanged. Its findings were validated claim by claim and
> applied in PRD revision 0.8: see the [validation record](prd-001-deep-review-validation-2026-10-13.md),
> which records what held, what was overstated or wrong, and what each finding changed.
>
> **Renamed (13 Oct 2026, PRD rev 0.9).** The product this review calls "FH" is now **Fyniq**, and the
> PRD file is now `prd-001-fyniq-platform.md`; links were updated.

# Cognizhi Finance Hub Cloud: deep PRD review and enhancement baseline

**Review date:** 12 October 2026  
**Document reviewed:** [PRD-001](../prd-001-fyniq-platform.md), revision 0.7, 3,920 lines  
**Status:** Independent review baseline; recommendations have not been adopted  
**Repository commit:** 9d849a77c484001b2c5571c979c5188b5a3afe9c  
**PRD SHA-256 at review:** dea4422df0760bdfd079f2680382d08a0885c8e1c13992e7f66298fdbd5990ea  
**Output location:** Temporary file outside the repository. The PRD was not edited.

## 1. Assessment

The PRD has a sound accounting safety direction: exact decimals, immutable posted history, database constraints, independent calculation tests, human approvals and traceable evidence. PostgreSQL is a defensible choice. The distinction between deterministic automation and AI proposals is also worth preserving.

The document is **not yet a reliable implementation baseline**. Its main weakness is the interaction between requirements: later additions alter the launch scope, legal obligations, accounting assumptions and operating model without consistently updating the earlier contracts. Several promises cannot simultaneously hold as written. Some important accounting cases remain underspecified even though the happy-path arithmetic is unusually detailed.

The highest-value enhancement is to make the existing product smaller, internally consistent and explicit about applicability, failure and evidence. Adding more features should follow that work.

### Decisions that deserve attention first

| Decision or correction | Findings | Why it comes first |
|---|---|---|
| Freeze a capacity-backed launch scope and compare build, buy and hybrid options | F01–F04 | The delivery estimate no longer corresponds to the requirements |
| Replace absolute workbook parity with an approved difference register | F05 | The platform deliberately corrects workbook calculations |
| Choose the accounting framework and define tax-point, FX and input-tax rules | F06–F11 | Balanced entries can still report the wrong revenue or tax |
| Complete payroll obligations and employer-level calculations | F12–F14 | Calculations alone are insufficient to run payroll legally |
| Separate write-offs, FX and payment execution; complete provider accounting | F17–F22 | Current shortcuts can hide losses or duplicate revenue |
| Bind approvals to dependencies and preserve human maker–checker separation | F28–F31 | An approved payload can still produce an unapproved effect |
| Define tenant constraints, retention, deletion and audit continuity together | F33–F35 | RLS and WORM do not settle all integrity and privacy questions |
| Reconcile the regional-outage promise with the actual recovery design | F36 | Regional HA protects zones, not a whole-region outage |
| Prove asynchronous execution, local builds and identity capabilities | F37–F42 | These are foundational feasibility questions |
| Strengthen AI evidence and completeness, then prioritise business-specific features | F45–F50; O01–O12 | A correct-looking answer is not necessarily a correct conclusion |

## 2. Method, confidence and limits

Reviewed §1–§21 and Appendices A–J, the earlier revision-0.1 review and its applied resolutions, the supporting research notes, AGENTS.md, the design-system master, and the repository accounting, statutory, security, local-development and quality-gate skills. Existing resolved issues are not presented as new omissions.

Research used Singapore regulators, official standards and protocol documentation, database/cloud documentation, and vendors' own documentation. A vendor page establishes a published capability; it does **not** prove its quality, Singapore availability, contractual suitability or savings. Recommendations and counterexamples below are reviewer analysis unless explicitly attributed. All external pages cited here were retrieved on the review date. Moving documentation should be captured and revalidated before implementation.

The review is of a specification, not a running application. No production configuration, partner agreement, bank report, actual company tax profile or predecessor workbook implementation was validated. The review does not certify compliance or prove that a particular implementation is vulnerable. Conditional findings apply only when the relevant feature or transaction is used.

**Severity**

- **B — blocker:** Resolve before the affected feature handles live books, money or personal data, or before claiming the affected guarantee. This does not mean every B finding blocks unrelated P0 work.
- **H — high:** Resolve before designing or committing delivery of the affected capability.
- **M — medium:** Improve specification, usability or operational maturity; sequence by actual use.

**Evidence class**

- **Confirmed:** Direct PRD contradiction, reproducible arithmetic counterexample, or a directly supported official fact.
- **Design gap:** A necessary contract or failure case is not sufficiently defined. This is not an allegation that the eventual implementation will fail.
- **Validation needed:** Public evidence cannot settle deployment, eligibility, legal interpretation or implementation behaviour.

Finding IDs F01–F50 and opportunity IDs O01–O12 belong to this report. They are not newly adopted PRD requirements.

## 3. Findings: product, scope and governing requirements

### F01 — P1 is no longer a bounded workbook-replacement release

**H · Confirmed · References:** §4.1, §5.5, §17.2, §18, R2; revision history 0.3–0.7. **Owner:** Founders/product lead.

A mechanical scan found **493 unique rows** having one exact requirement ID, a plain M/S/C priority and an explicit phase; **377 mention P1, including 319 marked M**. This excludes conditional priorities, grouped IDs, invariants, controls and requirements in tables without phase columns. It is a conservative scope indicator, not a measure of equal-sized work. There are also 56 occurrences of a “[verify…” marker, including explanatory occurrences.

P1 now includes advanced equity, project billing, capture by email, payment-provider mirroring, bank email ingestion, connector lifecycle, company-secretary agents and a visual template editor, while §17.2 retains 12–16 weeks. R2 still describes strict phasing but then lists each scope increase.

**Recommendation:** Define the company's actual launch transaction types and an explicitly frozen release subset. Estimate capacity, dependencies and review effort for that subset. Preserve requested capabilities in the roadmap; require a budget and displaced scope for each promotion.

**Acceptance:** Every launch requirement has an owner, estimate, dependency, acceptance scenario and reason it must precede go-live. The release forecast derives from available capacity rather than the old phase label.

### F02 — Build versus buy omits the principal cost: building and maintaining the system

**H · Design gap · References:** G9, §5.5, §12.1, §12.6, A3, R11. **Owner:** Founders.

The PRD compares infrastructure with accounting subscriptions, but omits engineering opportunity cost, accounting review, annual payroll/tax changes, security operations, provider certification, licences and support. A low cloud bill does not establish that a custom ledger is economical for one company.

Current commercial reference points support taking the buy/hybrid option seriously. Xero Singapore lists Standard at S$70/month and Premium with multi-currency at S$95/month, including GST, and announces a price increase from 1 November 2026. These are current regular prices, not the temporary introductory discounts. [Xero Singapore plans](https://www.xero.com/sg/pricing-plans/). Zoho Books lists Professional at S$36 monthly or S$30/month billed annually, and Premium at S$54 monthly or S$45/month billed annually. [Zoho Books Singapore plans](https://www.zoho.com/en-sg/books/pricing/). Talenox lists payroll starting at S$21.80/month for five employees on its annual pricing, inclusive of GST; monthly pricing differs. [Talenox Singapore pricing](https://www.talenox.com.sg/pricing/). Xero also documents Singapore payroll through partner apps. [Xero Singapore payroll partners](https://www.xero.com/sg/accounting-software/payroll/). These prices exclude the full integration, review and add-on costs and do not establish equivalent controls.

**Recommendation:** Compare three options: buy ledger/payroll; build the entire platform; buy the statutory ledger/payroll and build the differentiated evidence, planning and proposal experience around controlled integrations. If a proprietary ledger is strategically important, state that objective and fund it explicitly. For payroll, compare an integration now with in-house statutory ownership; employee count alone is a weak decision rule.

**Acceptance:** An approved 24-month cost and risk comparison includes ongoing maintenance, accountant involvement, exit arrangements and the value that cannot be obtained through purchased products.

### F03 — Success measures can reward unsafe or unhelpful behaviour

**M · Design gap · References:** §4.3, UX-T1/T3/T5, QA-AI-01, AI-33. **Owner:** Product lead/accountant.

“Three approvals in 30 seconds” conflicts with meaningful review of evidence and impact when applied to non-routine items. The close targets mix active effort and elapsed reviewed-close time. “80% categorised automatically” mixes deterministic automation with human-approved AI work. Zero calculation defects is an aspiration, not an incident-handling or measurement definition.

**Recommendation:** Separate active time, elapsed time, review quality, exception rate and automatic-posting coverage. Measure wrong suggestions accepted, later corrections and unexplained differences. Define denominator, measurement window and transaction mix. Use usability testing with unfamiliar founders and accountants beyond the builders themselves.

**Acceptance:** A high automation percentage cannot offset a material error, and the approval speed test excludes actions requiring substantive review.

### F04 — Requirement metadata cannot consistently drive the promised phase gate

**H · Confirmed · References:** QA-TRACE-01, §1 phase table, §17.3, §18, FR-PLN-05, FR-PRJ-04, FR-AR-15, FR-KPI-04, FR-ADM-08. **Owner:** Product/engineering lead.

Several rows say a feature moved to P1 while the phase column still says P2. Model B depends on subscriptions and customer counts assigned to P2. P0 exits require audit verification and signed images whose requirement rows say P1. Some M requirements have no phase or use conditional prose priorities that a simple table extractor will miss.

**Recommendation:** Maintain one structured requirement registry with priority, applicability, sub-capability phase, dependencies, acceptance criteria and verification owner. Generate summary tables from it. Keep requirement IDs stable and record supersession explicitly.

**Acceptance:** The registry detects unresolved dependencies and conflicting phases. Referencing an ID in a test establishes coverage only when that test proves its acceptance criteria; a text tag alone is insufficient.

## 4. Findings: accounting and statutory correctness

### F05 — Workbook parity is contradicted by deliberate rule changes and an incorrect rounding bound

**B · Confirmed · References:** §4.3, FR-MIG-02, QA-MIG-01, Appendix B.3, G.2, ENG-41. **Owner:** Accountant/engine lead.

Only ALLOC-EVEN differences are permitted, yet the platform adds CPF low-wage graduation, AW ceilings, PR rules, wage-banded SHG and actual FX revaluation where the workbook used a memo. These can change payroll liabilities, P&L, balances and checks. Appendix G.2 itself acknowledges that the S$700 wage expectation must change.

The claimed spread difference of less than S$0.01 each month is false when all remainder cents go to the last period. For S$1.00 over six periods, the specified allocation is 0.16 five times and 0.20 once; the final period differs from the unrounded 1/6 by about 0.0333.

**Recommendation:** Use an accountant-approved variance register distinguishing defects, deliberate statutory corrections, new postings and allocation changes. Decide whether final-period concentration is wanted or whether largest-remainder allocation better serves the schedules. Treat parity as migration evidence rather than accounting authority: two independent implementations can agree on the same incorrect specification, so official-source fixtures and accountant review remain separate backstops.

**Acceptance:** Import produces a bridge from workbook balances to platform balances by account/month with every permitted difference explained and signed off. Test positive and negative schedules and final-period residuals.

### F06 — Select the accounting framework before automating recognition and classification

**B · Design gap · References:** FR-RPT-11, FR-PRJ-11, FR-EQ-06/13/18, §14.2, §20. **Owner:** External accountant.

The reporting framework remains a P3 “[verify]” issue even though P1 performs recognition, financing classification and statutory statements. Those policies cannot be inferred from an invoice or chosen freely merely to improve a dashboard. FRS, SFRS(I) and SFRS for Small Entities have different applicability and policy requirements.

ACRA confirms that the third edition of SFRS for Small Entities takes effect for annual periods starting on or after 1 January 2027; that transition can intersect the proposed build and go-live. [ACRA accounting-framework guide](https://www.acra.gov.sg/regulations/accounting-standards-financial-reporting-surveillance/accounting-standards/), [ACRA third-edition announcement](https://www.acra.gov.sg/news-events/news-announcements/885/).

**Recommendation:** Record framework, edition, eligibility and approved accounting policies at setup. Use a supported-transaction matrix for revenue, grants, leases, development costs, impairment and financial instruments; unsupported cases go to accountant-reviewed journals.

**Acceptance:** The accountant approves the launch policies and the edition-transition treatment before live posting. “Management statements” and statutory-ready statements have distinct claims.

### F07 — GST tax point is not the same as document date, accounting date or cash date

**B · Design gap · References:** FR-ORG-03, FR-AR-09, ENG-01/31, Appendix B opening convention. **Owner:** Tax agent/engine lead.

ENG-31 correctly names tax point, but Appendix B selects the rate from document date. Deposits, advance receipts, invoices spanning GST registration or rate changes, credit notes and late supplier invoices need explicit dates and eligibility. A service-period spread determines recognition, not automatically the GST period. IRAS describes the general time-of-supply rule as the earlier of invoice issuance or payment, with specific treatment for security deposits and other cases. [IRAS reporting supplies](https://www.iras.gov.sg/taxes/goods-services-tax-%28gst%29/charging-gst-%28output-tax%29/when-to-report-supplies-in-gst-returns/8).

**Recommendation:** Introduce explicit supply/tax-point, document, accounting and payment dates with a tax-policy precedence rule. Define GST registration transitions, taxable advances and corrections from the applicable official guidance. Resolve the cash-basis “[verify]” item before offering that mode.

**Acceptance:** Pinned cases cover advance payment before invoice, registration mid-service, tax rate transition and late-recorded purchase documents. Cash, revenue and GST timelines can differ without forcing one date onto all three.

### F08 — The GST FX rule can overwrite the authoritative supplier tax amount

**B · Confirmed · References:** ENG-40/42, RND-FX, RND-GST-EXCL, Appendix B.2. **Owner:** Tax agent.

The generic approach converts net to SGD and recomputes GST. For a foreign-currency purchase, IRAS says the input tax claim uses the supplier invoice's SGD GST amount, even if the books use another exchange rate. Imports use the permit's SGD amount. Approved tax-rate sources also require consistent use. [IRAS foreign-currency transactions](https://www.iras.gov.sg/taxes/goods-services-tax-%28gst%29/charging-gst-%28output-tax%29/foreign-currency-transactions).

**Recommendation:** Store document currency amounts, accounting FX and source, supplier/permit SGD tax, and tax FX policy separately. Allocate any difference explicitly. Do not silently average conflicting exchange-rate records as ENG-40 currently permits.

**Acceptance:** A purchase with accounting FX different from tax FX claims the invoiced SGD tax and remains balanced. Conflicting rates require a selected authoritative source or an acknowledged correction, not an untraceable average.

### F09 — Company-name-only input-tax rules incorrectly exclude valid simplified invoices and agency expenses

**B · Confirmed · References:** FR-EQ-02, Appendix B.7, UC-EXP-01, FR-DOC-12. **Owner:** Tax agent/product lead.

FR-EQ-02 and B.7 impose a company-addressed invoice rule, and UC-EXP-01 denies taxi GST because the receipt is not addressed to the company. IRAS also permits valid simplified tax invoices and eligible expenses incurred by employees acting as the company's agent. Entitlement depends on the document, business use and other conditions; it cannot be decided from the addressee alone. [IRAS input-tax conditions and employee expenses](https://www.iras.gov.sg/taxes/goods-services-tax-%28gst%29/claiming-gst-%28input-tax%29/conditions-for-claiming-input-tax).

**Recommendation:** Define an eligibility decision tree including simplified invoices, agency evidence, reimbursement, blocked categories and business/private apportionment. Keep tax-agent review for ambiguous claims.

**Acceptance:** Fixtures distinguish valid simplified receipts, employee-as-agent purchases, inadequate receipts, private expenditure and blocked motor-car expenditure. OCR cannot set eligibility solely from a GST total.

### F10 — InvoiceNow needs a tax-data completeness contract, beyond send/receive integration

**B when applicable · Design gap · References:** FR-AR-12, FR-AP-05, FR-PSP-03, §14.2, CTL-STAT-01. **Owner:** Tax agent/integration lead.

§14.2 already recognises all in-scope sales and purchases; that is good. The functional requirements still concentrate on Peppol document transmission. IRAS's population also includes equivalent documents and allows specified aggregation, with exclusions; submission is due by the earlier of filing and the filing deadline. A counterparty being outside Peppol does not by itself remove an in-scope transaction. [IRAS GST InvoiceNow requirement](https://www.iras.gov.sg/taxes/goods-services-tax-%28gst%29/gst-invoicenow-requirement).

**Recommendation:** Map the return population to tax-data submissions, including provider invoices, receipts, credit notes and eligible aggregation. Define Access Point receipt versus downstream acceptance/rejection, corrections and retry status. An AP acknowledgement alone should not be assumed to prove IRAS acceptance without the partner contract.

**Acceptance:** Reconcile every in-scope return item to an accepted submission or justified exclusion. Test non-Peppol purchases, B2C sales, corrections and partner outage. A local filing gate must not make a legal deadline impossible: define escalation and an evidenced authorised contingency.

### F11 — Corrections and late documents must work from the first filed GST period

**B when GST is used · Design gap · References:** FR-TAX-02/03, FR-GL-08/12, §14.2. **Owner:** Tax agent.

P1 locks filed periods, while F7/F8 support is deferred. The PRD does not specify how a late invoice, wrong code, refund or tax error affects the ledger, already-issued F5 and the correction working paper. Reopening a ledger period is not equivalent to amending a tax return.

IRAS provides F7 and a conditional next-F5 concession; its current net-GST criterion is S$3,000, alongside a separate percentage test. These are effective-dated rules, not a reason to overwrite a filed return. [IRAS GST corrections](https://www.iras.gov.sg/taxes/goods-services-tax-%28gst%29/filing-gst/correcting-errors-made-in-gst-return-%28filing-gst-f7%29).

**Recommendation:** Keep immutable filed-return versions and an adjustment queue carrying original tax period and correction disposition. Manual filing is acceptable at launch if the working paper and evidence are complete.

Also specify a control-account roll-forward: opening GST balance + tax activity + approved adjustments − payments/refunds = closing balance. A return's tax boxes cannot simply equal all movements in 2200 when remittances also affect that account, and the non-tax boxes are separate reporting measures. IRAS defines Box 8 from output/input tax and warns against deriving input tax mechanically from taxable purchase value. [IRAS completing GST returns](https://www.iras.gov.sg/taxes/goods-services-tax-%28gst%29/filing-gst/completing-gst-returns).

**Acceptance:** Correcting a filed period preserves the original return and generates the tax-agent-approved adjustment bridge without violating ledger locks.

### F12 — SDL omits employer-month aggregation and rounding

**B · Confirmed · References:** FR-PAY-03/07/09, RND-SDL, B.3, G.2. **Owner:** Payroll accountant.

The PRD calculates and posts per-employee SDL with cents but does not define the employer's remittance rounding. CPF Board requires per-employee calculation, addition across employees, then rounding the total payable down to the dollar. Its example distinguishes S$40.75 from S$40 payable. [CPF Board SDL calculation](https://www.cpf.gov.sg/employer/employer-obligations/skills-development-levy).

**Recommendation:** Separate employee allocation, aggregate liability and remittance, with an approved treatment for the residual. Do not pretend that summing employee liabilities directly yields the submission amount.

**Acceptance:** Multiple employees with fractional SDL reconcile payroll cost, ledger liability, submission and cash paid; supplementary pay runs cannot repeatedly round the same employer-month.

### F13 — CPF needs an employee-month/year state model, not only a rate lookup

**H · Design gap · References:** FR-PAY-01/02/06, RND-CPF/CPF-GRAD, B.3, G.2. **Owner:** Payroll accountant/engine lead.

The rate tables are detailed, but supplementary and off-cycle runs, prior-employer/year-to-date opening data, multiple OW/AW payments, corrections, PR-year transitions, elections for higher contributions and termination adjustments are not fully specified. The low-wage calculation must classify the wage band consistently with the total payable for the month.

CPF Board describes rounding at the calendar-month level and dropping employee-share cents. Reapplying it independently to each pay run is not automatically correct. [CPF Board contribution rounding](https://www.cpf.gov.sg/service/article/are-cpf-contributions-rounded-to-the-nearest-dollar).

**Recommendation:** Define cumulative employee-month computation and delta postings, with explicit AW estimates/reconciliation and approved overrides. Confirm every edge rule against the official tables.

**Acceptance:** Splitting identical monthly wages across two runs produces the same total statutory obligation as one run; importing mid-year payroll preserves ceilings and corrections.

### F14 — Payslips, tax-clearance holds and lawful deductions cannot be postponed after live payroll

**B when employees are paid · Confirmed/design gap · References:** FR-PAY-08/10, FR-AP-17/18, §14.8, §18. **Owner:** Payroll owner.

P1 calculates and pays salaries while required outputs are P2. MOM requires payslips with payment or within three working days, with a specific termination requirement. IRAS tax clearance can require withholding **all monies due** from awareness of impending departure/cessation, broader than a final-salary hold. Advances and personal card spending cannot simply be deducted without the applicable legal basis and limits. [MOM payslips](https://www.mom.gov.sg/employment-practices/salary/itemised-payslips), [IRAS tax clearance](https://www.iras.gov.sg/taxes/individual-income-tax/employers/tax-clearance-for-foreign-spr-employees-%28ir21%29/tax-clearance-for-employees), [MOM allowable deductions](https://www.mom.gov.sg/employment-practices/salary/salary-deductions).

**Recommendation:** Gate payroll activation on compliant payslips/KET arrangements, statutory remittance and applicable income reporting, using a provider or documented manual process if necessary. Make clearance holds apply across relevant salary, claim and other payment paths.

**Acceptance:** An applicable leaver's claim payment cannot bypass a clearance hold. Deduction limits, consent and salary due dates are checked independently of a balanced journal.

### F15 — Withholding-tax applicability is an early AP concern, even if automated filing is later

**H, conditional · Design gap · References:** FR-AP-11 C/P3; §2.3 business model; FR-TAX-01/05. **Owner:** Tax agent/AP owner.

Cognizhi expects overseas software/cloud purchases and consulting. Not every such payment attracts withholding tax: IRAS distinguishes software rights and services performed in Singapore from services provided remotely overseas. That distinction is precisely why an applicability assessment belongs before payment, rather than treating the entire capability as optional P3. [IRAS payments subject to withholding tax](https://www.iras.gov.sg/taxes/withholding-tax/payments-to-non-resident-company/payments-that-are-subject-to-withholding-tax).

**Recommendation:** Launch with supplier residency, payment nature, place of performance, tax-agent decision and an obligation/hold where applicable. Also document reverse-charge applicability for imported services instead of assuming every company is eligible for full input-tax recovery. [IRAS imported services](https://www.iras.gov.sg/taxes/goods-services-tax-%28gst%29/gst-and-digital-economy/local-businesses).

**Acceptance:** A non-resident supplier payment receives an evidenced applicability decision; absent an applicable obligation, ordinary remote SaaS is not indiscriminately blocked or taxed.

### F16 — Decimal storage and spreadsheet exports need an explicit precision boundary

**H · Confirmed/design gap · References:** ENG-10/11, §11.3, FR-RPT-05, QA-RPT-03. **Owner:** Engine/export lead.

PostgreSQL rounds values exceeding a declared NUMERIC scale before storing them. Thus a currency-scale CHECK on NUMERIC(20,4) does not prove that the original input had no excess decimals: an SGD input such as 1.230001 can become 1.2300 before the CHECK. [PostgreSQL numeric coercion](https://www.postgresql.org/docs/17/datatype-numeric.html).

Excel stores/calculates only 15 significant digits, so arbitrary database-scale values plus live spreadsheet formulas cannot satisfy the absolute “no floating point anywhere, including exports” promise. [Microsoft Excel precision](https://support.microsoft.com/en-us/excel/change-formula-recalculation-iteration-or-precision-in-excel).

**Recommendation:** Validate raw decimal strings before coercion, reject invalid scale/range and non-finite values, and specify intermediate precision and overflow policy. Preserve an exact text data sheet/CSV/JSON; define numeric presentation cells and formulas as a convenience with stated bounds.

**Acceptance:** Raw excess-scale input fails rather than silently rounding. Large monetary values survive an exact export/re-import, and totals are not trusted merely because Excel displays two decimals.

### F17 — A 3% settlement tolerance is a material write-off policy in disguise

**B · Confirmed · References:** FR-BNK-08, Appendix B.1/B.2, G.1/G.3. **Owner:** Accountant/controller.

The foreign-currency tolerance is the greater of S$1 and 3% of document total, and B.2 permits “marked fully paid.” On a S$100,000 base-equivalent invoice, 3% is S$3,000. Routing such a difference to one FX/rounding account can conceal short payment, fraud, fees or a write-off.

**Recommendation:** Separate computed realised FX, genuine rounding, authorised discounts, bank/provider fees and bad debt. Use absolute and relative limits plus cumulative period limits and appropriate approvals. “Fully paid” must not authorise arbitrary differences.

**Acceptance:** A material unexplained shortfall remains open and needs a reason-specific approved adjustment. Exact FX can settle outside a rounding cap because it follows the stored currency amounts and rates, not a tolerance shortcut.

### F18 — FX revaluation requires base-only adjustments and historical carrying-value settlement

**B · Design gap · References:** FR-GL-04, ENG-14/41/43, INV-05, §10.2. **Owner:** Engine lead/accountant.

Revaluing a USD monetary balance changes its SGD carrying value without changing USD principal. The invariant requiring each base amount to equal transaction amount × positive FX rate needs an explicit treatment for such base-only adjustments. Otherwise a zero-USD/nonzero-SGD revaluation is rejected, or the transaction balance is changed incorrectly.

**Recommendation:** Define revaluation entry types, eligible monetary items, rate direction, carrying amount, reversal method and realised FX on partial settlement. Limit the invariant's exception to audited adjustment types. Include advances/deposits according to their monetary/non-monetary classification.

**Acceptance:** USD principal remains unchanged after month-end revaluation; partial settlement and next-period reversal do not double-count FX. Revaluation entries pass a specific invariant rather than weakening the general one.

### F19 — Billing, cash collection and revenue recognition are conflated in Model B

**B when Model B is live · Design gap · References:** FR-PSP-03, UC-PSP-01, FR-AR-15, FR-GL-03, FR-PRJ-11. **Owner:** Accountant/integration lead.

UC-PSP-01 starts from a successful charge and credits revenue/deferred revenue. It does not fully describe finalised but unpaid invoices, payment failure, receivables, credit adjustments, or annual service recognised independently of settlement. Straight-line spreading is not a universal contract-revenue rule.

IFRS 15 distinguishes the contract, performance obligations, transaction price, allocation and satisfaction. Its applicability must follow the selected Singapore framework, not be assumed for every entity. [IFRS Foundation revenue-recognition overview](https://www.ifrs.org/issued-standards/list-of-standards/ifrs-15-revenue-from-contracts-with-customers/).

**Recommendation:** Define authoritative events for invoice, receivable, collection, recognition and refund separately. Specify supported subscription/project contract cases, amendments, proration, cancellation and credits; send complex bundles to the accountant.

**Acceptance:** A finalised unpaid invoice, later collection and deferred recognition do not duplicate revenue. A refund of unused service does not reverse revenue already earned without an approved accounting basis.

### F20 — Provider-clearing reconciliation is incomplete and contains a fee double-counting risk

**B when provider integration is live · Confirmed/design gap · References:** FR-PSP-02/05/06, §10.2 payout rows, UC-PSP-04, C.2 #42. **Owner:** Integration lead/accountant.

FR-PSP-02 expenses the fee when the charge occurs, whereas the older payout posting debits fees again. Capturing the monthly fee tax invoice must not recognise the same expense again. A provider-wide clearing balance also need not be zero: unsettled transactions, reserves, refunds, disputes and multiple currencies may remain.

Stripe's itemised payout report is specific to automatic payouts; manual and instant payouts require a different reconciliation approach. [Stripe payout-reconciliation report](https://docs.stripe.com/reports/payout-reconciliation).

**Recommendation:** Use a provider balance-transaction subledger with gross/fee/net, currency, conversion, availability date and settlement attribution. Reconcile allocated payout groups and the overall balance independently. Define fee-tax-invoice matching and suspense for unallocated adjustments.

**Acceptance:** Fees post once. A paid-out group closes while the legitimate unsettled provider balance remains. Manual payout, negative balance, FX, partial refund and later dispute are all supported or explicitly excluded.

### F21 — Event-ID deduplication and bank fingerprints are not sufficient economic-event identity

**B · Design gap · References:** FR-PSP-01, FR-BNK-04/07/16, FR-INT-08, §11.4. **Owner:** Integration/engine lead.

Two legitimate payments may have the same date, amount and reference. Conversely, different provider events can describe the same invoice or payment. A feed/statement overlap and a mirror of the other transfer leg require identities different from HTTP replay keys. The 24-hour API idempotency window is shorter than financial replay/backfill needs.

Stripe does not guarantee webhook order and explicitly discusses duplicates; event timestamps are not an ordering proof. [Stripe webhook ordering and duplication](https://docs.stripe.com/webhooks#event-ordering).

**Recommendation:** Persist raw source identity and parser/API version; maintain unique posting identities per economic action and source object/version. Scope transport keys by organisation, client and operation, separately from durable financial uniqueness. Use “possible duplicate” for ambiguous fingerprints.

**Acceptance:** Replaying days later does not repost a payment. Equal legitimate bank payments are both retained. Out-of-order invoice/charge events, overlapping backfills and mirrored transfers converge to one correct economic record.

### F22 — Subledger lifecycle and document state transitions need a formal contract

**H · Design gap · References:** FR-AR-03/04/09, FR-AP-02/03, FR-BNK-07, INV-07, §10.2. **Owner:** Domain lead/accountant.

The invoice status chain mixes approval, posting, sending, settlement and overdue status, which are partly independent. Deposits, unapplied receipts, standalone credits, refunds, overpayments and allocations across currencies need posting and matching rules. A credit balance cannot simply be rejected because AR should not be negative.

**Recommendation:** Define independent approval/posting/delivery/payment states with legal transitions. Specify allocation records and their reversal, ageing, unapplied balances, credit application, customer/vendor refund and treatment of cross-currency settlements. No netting customer and supplier obligations without an explicit permitted offset.

**Acceptance:** Part-payment, standalone credit, overpayment, refund and reversed allocation reconcile with the control accounts and statements. Sending failure cannot unpost an invoice; becoming overdue cannot change its accounting.

### F23 — Equity transactions require instrument-specific and legally meaningful event rules

**B when equity automation is used · Design gap · References:** FR-EQ-06/17/18/20/21/26, INV-13, B.7, G.3. **Owner:** Accountant/company secretary.

P1 structured rights exceed the simple SAFE example. The PRD lacks explicit pre-money/post-money conventions, capitalisation denominators, option-pool treatment and note-interest conversion rules. YC's post-money SAFE documentation illustrates why instrument form matters; its commercial description does not determine Singapore accounting classification. [YC SAFE documents and guide](https://www.ycombinator.com/safe).

The cash-share-issue rule Dr Bank/Cr share capital on legal effectiveness also needs a case for cash received earlier. Reversing an economic/legal share transaction cannot automatically undo ACRA's register.

**Recommendation:** Limit automated calculations to named supported instrument templates with terms and independent fixtures. Record accountant-approved classification by instrument. Use subscription money pending allotment where applicable; separate correction of erroneous mirrored data from a new legal transaction.

**Acceptance:** Cash received before allotment reconciles to the bank before share capital becomes effective. Unsupported instrument terms block automatic conversion. A mistaken transfer is corrected with evidence of legal status rather than inventing a legal reversal.

### F24 — “Every KPI reads the ledger” is incompatible with operational metrics

**H · Confirmed/design gap · References:** ENG-04, FR-KPI-02/04/07, FR-PLN-09, FR-PRJ-12, FR-EQ-20. **Owner:** Reporting/product lead.

MRR, churn, customer counts, CAC, utilisation, cap-table ownership and benchmarks are not all ledger measures. “No report reimplements posting” is valuable; “every KPI is obtained from balances” is an inaccurate source-of-truth contract. The PRD also promises workbook KPI definitions without reproducing enough of them to verify this specification independently.

**Recommendation:** Create a measure registry declaring source, definition/version, date semantics, currency, precision, access scope and lineage. Distinguish booked revenue, billed amounts, collected cash and contractual recurring value. Define no-data/undefined states for zero denominators, negative equity, non-burning runway and unstable IRR.

**Acceptance:** MRR can differ from recognised revenue with a visible explanation. A metric shows unavailable when its non-ledger data is missing, rather than fabricating zero. Forecasts and benchmarks remain clearly distinguished from actual books.

### F25 — Project profitability can double-count payroll and reveal restricted salary data

**H · Design gap · References:** FR-PRJ-06/08/12, FR-PAY-13, FR-DIM-02, Appendix D. **Owner:** Project/reporting lead.

FR-PRJ-12 subtracts time at cost and direct costs, but salaries may already be in project-tagged ledger direct costs. Adding time costs again would overstate expense. Cost rates derived from payroll can also disclose salaries to project managers who cannot view payroll. “Available hours” needs a definition despite leave management being P3.

**Recommendation:** Define management-cost versus posted-cost bases and prevent overlaps. State standard hours, availability, overtime and rate-effective dates. Restrict payroll-derived person-level rates or provide appropriately aggregated rates. Specify project-manager membership rather than relying only on a generic Approver role.

**Acceptance:** Assigning salary expense to a project does not double its cost. Users without salary rights cannot reverse-engineer individual compensation from person-level profitability or cost-rate endpoints.

### F26 — Reproducible reports require a commit-consistent snapshot and versioned presentation inputs

**B for the reproducibility guarantee · Design gap · References:** FR-RPT-08, ENG-06, §11.6/11.7, FR-TPL-04. **Owner:** Data/reporting lead.

A ledger “high-water mark” is not fully defined. UUIDv7 ordering or timestamps do not inherently establish commit order. A transaction that began earlier can commit after a report was issued. Account names/grouping, dimensions, tax packs, report formulas, templates and FX policy can also change the reported view.

**Recommendation:** Define a commit-consistent per-organisation ledger revision and an atomic read snapshot. Freeze the versions of mappings, measures, packs, presentation and filters. Store issued export bytes where byte identity is required; deterministic recalculation of figures is a different promise.

**Acceptance:** Two concurrent commits straddling report generation cannot later change that report's result. Renaming or regrouping an account does not change an already-issued report unless the user deliberately requests a restated presentation.

### F27 — Period locks and financial allocations need race-condition specifications

**B · Design gap · References:** INV-01/03/04/07, ENG-02, FR-GL-07/08, QA-ENG-05. **Owner:** Database/domain lead.

A trigger that reads “period open” can race with another transaction closing it unless their locking protocol guarantees an order. Equivalent races occur between payment allocation and another allocation, account deactivation and posting, schedule processing and close, or approval expiry and execution.

**Recommendation:** Document transaction isolation, lock acquisition order, retry behaviour and a shared commit barrier for posting versus close. Checks and attestation must refer to the same version being closed. Period-reopen snapshots require a similarly explicit sequence.

**Acceptance:** Barrier-controlled tests using independent connections prove close-versus-post, allocation-versus-allocation and approval-versus-change behaviour. At most one incompatible operation commits; the loser receives a stable actionable error.

## 5. Findings: approvals, identity and permissions

### F28 — Approvals must bind the effective transaction and its dependencies

**B · Design gap · References:** FR-APR-02/03/05, AI-P5, AI-11, proposal base_versions, FR-CT-03. **Owner:** Security/domain lead.

Hashing command JSON is insufficient when the command refers to mutable bank details, policy, pack, account mapping or schedule. The same payload could produce another impact after approval. It is also unclear how partial selection, quorum and policy changes affect the frozen proposal.

**Recommendation:** Define canonical serialisation and an approval envelope covering operation, organisation, selected items, amounts/currencies, beneficiary version, rule/pack/policy versions and impact/dependency versions. Revalidate current authority and compare the envelope within the execution transaction.

WebAuthn assertions sign authenticator data and a hash of client data; safely binding a transaction requires a fresh challenge containing a nonce and the envelope digest, plus origin/RP/user-verification checks. A bare hash reused as a challenge is insufficient. [W3C WebAuthn specification](https://www.w3.org/TR/webauthn-3/). OWASP also distinguishes sign-in from transaction authorisation and requires meaningful transaction acknowledgement. [OWASP transaction authorisation](https://cheatsheetseries.owasp.org/cheatsheets/Transaction_Authorization_Cheat_Sheet.html).

**Acceptance:** Changing a vendor bank account or relevant pack invalidates approval. Replaying an assertion for another organisation, proposal or selected subset fails.

### F29 — AI origin must not erase the human preparer's conflict; tier 1 needs effect-based limits

**B · Design gap · References:** AI-10/30/32, Appendix E, SOD-3/4/8, FR-BNK-14. **Owner:** Controller/security lead.

AI-10 permits the requester to approve because “the AI is the preparer.” Without preserving business-maker identity, a human could ask AI to prepare a journal to evade a no-self-approval rule. AI-30 helps, but the two approval stages must be enforced independently.

Appendix E also puts bank categorisation, settlement matching and rule editing in tier 1. They may change the ledger, invoke write-off tolerances, or create a rule later promoted to deterministic auto-apply. One hundred S$10,000 lines can expose S$1m despite the routine label.

**Recommendation:** Preserve requester/beneficiary/preparer attribution for business SoD; classify risk from actual effects and aggregate exposure. Separate creating an AI-suggested rule from enabling auto-apply, with its own policy approval and observed results.

**Acceptance:** “Ask AI then approve” cannot bypass journal/payroll/business SoD. Promoting a rule that causes future postings cannot silently become a standing approval of AI suggestions.

### F30 — MFA recovery, identity linking and owner succession are missing critical journeys

**B · Design gap · References:** SEC-01/05, FR-ADM-02/03/06/08, §12.2.1. **Owner:** Identity/security lead.

The PRD specifies mandatory MFA but not recovery after loss of a device, provider outage, compromised recovery contact, or departure of the last Owner. It says users are identified by sub but does not consistently state issuer + sub, and “accounts link only on a verified email” is weaker than proving control of both existing and new identities.

NIST treats account recovery as a separate, risk-assessed process with notifications and authenticator lifecycle controls. It is a useful design reference, not a claim that this Singapore product must be NIST-certified. [NIST SP 800-63B account recovery](https://pages.nist.gov/800-63-4/sp800-63b.html#account-recovery).

**Recommendation:** Require recent authentication to link providers; preserve issuer-qualified subject IDs; define recovery evidence, delay/notification, session revocation and restricted actions during recovery. Define secure ownership and Super-admin succession.

**Acceptance:** A verified email from another provider cannot silently take over an existing finance account. Loss of one authenticator is recoverable without bypassing high-risk financial controls.

### F31 — Break-glass conflicts with unconditional dual control and needs explicit boundaries

**B · Confirmed/design gap · References:** FM-1/8/9, SOD-6/8/10, FR-ADM-07, AI-09, CTL-FIN-08. **Owner:** Founders/security reviewer.

FM-1 requires a second person on high-risk actions, while FM-8 permits Owner break-glass for a single available founder. The permitted actions, exclusions, beneficiary conflicts, policy changes and independent evidence are unspecified. A single founder can also deadlock when all approvers are beneficiaries or unavailable.

**Recommendation:** Define a finite emergency-action list and constraints. State which actions still need another person and which can exceptionally proceed with a recorded override. Never implicitly allow an emergency policy edit to remove its own restrictions. Provide an external emergency reviewer/authority where suitable.

**Acceptance:** An emergency operation is conspicuous, expires and reaches an independently accessible review record. It cannot alter audit history or turn on AI automatic approval. Own-balance settlements remain governed by an explicit beneficiary-conflict rule.

### F32 — Permissions must survive aggregation, caches, exports and every delivery channel

**B · Design gap · References:** FR-PAY-13, AI-01/02/12, Appendix D, FR-RPT-07/09, FR-DOC-09, §11.7. **Owner:** Security/reporting lead.

Field masking does not stop salary leakage through a one-person department total, journals, project rates, search snippets, document previews or AI logs. The report-cache key lists organisation and report inputs but not permission scope. Signed links remain usable until expiry even after rights are revoked, unless there is an additional control.

**Recommendation:** Define effective data scopes for derived results and shared evidence. Include authorisation context or prove safe cache filtering. Recheck recipients at delivery/download; define link expiry, revocation, email visibility and investor-sharing policy. Sensitive AI traces need their own access rules.

**Acceptance:** A user cannot retrieve another role's cached report, use SSE to learn a hidden record, infer payroll via disallowed breakdowns, or fetch an expired/revoked sensitive download. Reader access changes also invalidate KB index caches.

### F33 — RLS needs tenant-aware foreign keys and an honest trust boundary

**B · Design gap · References:** INV-04/06, §11.4, SEC-02, QA-ENG-05. **Owner:** Database/security lead.

Transaction-scoped tenant context fixes pooled-connection leakage, already addressed by REV-23. It does not ensure every reference joins within one tenant: foreign keys and uniqueness checks bypass RLS. [PostgreSQL row-security documentation](https://www.postgresql.org/docs/current/ddl-rowsecurity.html).

**Recommendation:** Use tenant-aware keys/references such as organisation + record ID throughout dependent tables and audit/evidence links. Document that an API-controlled tenant setting protects against accidental query mistakes; an application role that can set arbitrary tenant context does not independently resist compromise of that role. Scope tables by role where stronger separation is promised.

The “every table has organisation_id” rule also needs explicit treatment of platform users, memberships, currency definitions, global packs, connector manifests and installation audit events.

**Acceptance:** Cross-tenant FK insertion fails even when the referenced UUID exists. Grants exclude TRUNCATE, owner and bypass privileges. Global tables have an explicit access model and cannot become an undocumented tenant escape.

## 6. Findings: data lifecycle, resilience and architecture

### F34 — WORM retention must start from the required legal date, and uploads need a two-phase lifecycle

**B · Confirmed/design gap · References:** FR-DOC-02/07/08, NFR-RET-01, §11.9, CTL-AUD-03. **Owner:** Data/security lead.

A seven-year bucket policy retains by object age, not seven years after the related FY end. A document uploaded near the start of the FY may therefore become deletable too early for the PRD's own default. Cloud Storage supports per-object retain-until dates; mutable object metadata is not itself protected by retention. [Google Bucket Lock](https://docs.cloud.google.com/storage/docs/bucket-lock), [Google Object Retention Lock](https://docs.cloud.google.com/storage/docs/object-lock).

**Recommendation:** Compute retention expiry from the record's obligations, FY/YA and legal holds. Seal the classification and manifest in audited records. Keep draft/quarantine objects out of irreversible long-term retention until scanning and classification complete.

Object storage upload and the PostgreSQL transaction cannot be one atomic transaction. Specify staged upload, scan, immutable generation/hash registration, commit, retry and orphan cleanup.

**Acceptance:** A January upload for a December FY stays retained through the promised post-FY period. Failed scan/database commit cannot leave an accessible unregistered attachment, nor cause unnecessary seven-year retention of a rejected upload.

### F35 — Purge, immutable audit history and crypto-shredding are not one coherent design yet

**B for lifecycle guarantees · Confirmed/design gap · References:** AUD-06/08, §11.5 L1, §11.8/11.9, FR-UX-14, FR-DOC-07. **Owner:** DPO/data lead.

L1 says every posting is retained forever; §11.9 specifies expiry and purge; FR-UX-14 says nothing is hard-deleted. Removing an audit-chain prefix requires retained boundary proofs, or integrity verification breaks. Per-person key destruction must also account for backups, retained evidence and other records still requiring the same person's data.

PDPA retention limitation is purpose-based, not an unlimited exception for anything labelled “audit.” The PRD should avoid importing a generic GDPR erasure model. [PDPC retention guidance](https://www.pdpc.gov.sg/guidelines-and-consultation/2020/03/advisory-guidelines-on-key-concepts-in-the-personal-data-protection-act).

**Recommendation:** Define approved archival/purge authority, retained manifests and chain boundaries, key scope and destruction proof, legal holds, and restore-time deletion tombstones. Distinguish encrypted personal content from required accounting amounts and metadata. A PITR restore behind an already-published audit anchor also needs a recovery protocol: preserve the earlier committed history/branch evidence, assess missing events, reconcile external effects and record the recovery without silently resetting the chain.

**Acceptance:** Restoring an old backup cannot resurrect data that should remain inaccessible. Remaining audit segments can still be verified offline. Destruction of one person's expired data does not make current statutory records unreadable.

### F36 — A regional-outage RTO of four hours is not supported by Singapore-only HA and backups

**B for the advertised recovery guarantee · Confirmed · References:** DR-01/03/04, NFR-DR-01/02, §11.8. **Owner:** Infrastructure lead/founders.

Cloud SQL HA is regional. When that region is unavailable, the database is unavailable; cross-region recovery requires an available copy elsewhere. A Singapore bucket and HA in another Singapore zone do not establish recovery from loss of the whole region. [Google Cloud SQL disaster recovery](https://docs.cloud.google.com/sql/docs/postgres/intro-to-cloud-sql-disaster-recovery).

**Recommendation:** Choose explicitly between Singapore-only residency with a narrower recovery promise, or an assessed alternate-region recovery path. Separate database corruption, zone failure, region failure, credential/project loss and key loss. Include documents, keys, identity, DNS, provider configuration and audit evidence.

**Acceptance:** A timed drill proves the advertised RPO/RTO for the named failure scenario, including integrity and payment/outbox reconciliation. A database restore alone does not count as full platform recovery.

### F37 — At-least-once work requires durable completion and external-side-effect reconciliation

**B · Design gap · References:** ADR-T7, FR-INT-08/12/21, AI-11/62, §12.3. **Owner:** Worker/integration lead.

Named-task deduplication is time-limited, and Cloud Tasks can execute more than once. [Google Cloud Tasks semantics](https://docs.cloud.google.com/tasks/docs/dual-overview), [Cloud Tasks duplicate execution](https://docs.cloud.google.com/tasks/docs/common-pitfalls).

Launching a Cloud Run job successfully does not prove that the job completed, and an email provider may accept a message before the worker loses its response. Failover can then send the same message twice. An outbox key does not make a provider without idempotency support exactly-once.

**Recommendation:** Define durable outbox states, leases/fencing, attempts, business keys, retry/backoff limits, dead letters, reconciliation and job-execution linkage. Represent unknown external outcomes explicitly.

An OIDC token proves service-account identity and audience, not a unique queue by itself; enforce enqueuer/IAM restrictions and validate the persisted approved work. [Cloud Tasks HTTP-target authentication](https://docs.cloud.google.com/tasks/docs/creating-http-target-tasks).

**Acceptance:** Crash after provider acceptance, task replay after 24 hours, duplicate job start and database restore all avoid duplicate financial posting and produce explainable delivery outcomes.

### F38 — The Scheduler tick cannot independently detect its own absence

**H · Design gap · References:** AI-67, ADR-T7 step 5, C.2 #12/#36, SEC-30/31, NFR-OBS-01. **Owner:** Operations lead.

The tick is also the watchdog. If Scheduler, the API, credentials or the database fail, it cannot write its own late-run warning. “No model call when nothing changed” also needs the clock, effective pack changes and approaching deadlines treated as inputs.

**Recommendation:** Add an independent heartbeat/deadline monitor outside the affected processing path, with explicit on-call owner and escalation. Define catch-up, skipped runs, concurrency and “exact time” tolerance for schedules. Distinguish deterministic checks from stochastic explanatory text in reproducibility claims.

**Acceptance:** Disabling the tick generates an alert through a separate path. A deadline becomes due without a data mutation and is still detected. Repeated runs do not flood the Inbox with identical findings.

### F39 — SSE and scale-to-zero promises need a shared cost and consistency design

**H · Confirmed/design gap · References:** FR-UX-19, ADR-T7, NFR-COST-02, NFR-PERF-01/UI-01, §12.6. **Owner:** Web/infrastructure lead.

Long-lived streaming requests remain active even if a user stops interacting. Google documents timeouts, reconnect requirements and active billing for streaming/WebSocket services; SSE cost should be measured under the chosen configuration rather than assumed free. [Cloud Run streaming guidance](https://docs.cloud.google.com/run/docs/triggering/websockets).

**Recommendation:** Decide whether open tabs are considered active use. Specify connection lifetime, background-tab behaviour, reconnect/resume cursor, multi-instance fan-out and permission checks. Compare bounded polling with streaming at the small-company scale. Measure cold-start sign-in, API, Hydra, model and document-scan paths.

**Acceptance:** Reconnect after a missed event reloads correct state without duplicates or leaking restricted records. The idle-cost definition allows intentional monitoring traffic and reflects real measured billing.

### F40 — Local “production images” cannot also contain adapters excluded from production binaries

**B for the local-mode contract · Confirmed · References:** DEV-07, SEC-16, make up contract, QA-LOCAL-01, QA-IMG-01, §12.8. **Owner:** Build/local-development lead.

Local adapters are compiled only into non-production builds, production binaries permit only staging/production configuration, yet make up and credential-free CI are required to run the production images against emulators and software keys. Those requirements conflict unless a specific shared runtime strategy is defined.

**Recommendation:** Specify either production-equivalent images compiled with a local build target and reviewed differences, or production-safe ports that can use local protocol-compatible endpoints without an insecure production bypass. Describe what exactly the local gate proves.

Replay AI tests also cannot prove live model quality; local gates, live synthetic eval gates and partner/staging conformance gates must remain separate.

**Acceptance:** A clean offline boot uses a named, valid build mode. Production cannot activate software keys or mocks. CI clearly identifies which cloud behaviours need staging evidence rather than claiming complete offline equivalence.

### F41 — Hydra capability assumptions must be verified against the chosen version and edition

**B for ADR-T3 acceptance · Confirmed public evidence/validation needed · References:** ADR-T3, AI-36/38, SEC-07, §12.6. **Owner:** Identity lead/founders.

The PRD assumes inexpensive Apache-licensed Hydra with JWT access tokens, resource audiences, token exchange and rotation. Ory's current edition matrix places **token exchange and stateless JWT access tokens in OEL/Network rather than OSS**. Its general FAQ describes the combined product capabilities. This is a procurement/version distinction, not proof that every historical OSS build lacked those features. [Ory Hydra edition matrix](https://www.ory.com/hydra).

**Recommendation:** Pin version, licence and deployment edition, and prove the exact required flows end to end. Price any commercial dependency. JWT validation must also implement a revocation strategy if “revoked within 60 seconds” is promised; signature verification alone cannot detect a role change.

The RFC 9728 resource metadata endpoint and RFC 8707 audience semantics solve different problems. Publishing metadata cannot substitute for a token server's missing audience support. [MCP authorisation specification](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization).

**Acceptance:** A real allowlisted client exercises consent, audience, exchange, refresh reuse, expiry and revocation against the selected edition before identity architecture is accepted.

### F42 — Deployment and port conformance need a capability matrix, not only equivalent service names

**H · Design gap · References:** ADR-T1/T4/T8, NFR-IMG-02/06, NFR-PORT-01…06, §12.4. **Owner:** Architecture/infrastructure lead.

Compose read-only roots, tmpfs and capabilities are not automatically the same controls exposed by Cloud Run. Google documents Cloud Run's writable in-memory filesystem; the PRD needs to say how the desired root-filesystem restriction is implemented or what alternative control is verified. [Cloud Run runtime contract](https://docs.cloud.google.com/run/docs/container-contract).

Likewise, naming GCP/AWS/Azure equivalents does not establish identical queues, identity, WORM or scheduling semantics. Repository interfaces also need an explicit unit of work so document, ledger, cache, audit and outbox remain in one database transaction.

**Recommendation:** Define a deployable modular architecture with service/credential boundaries justified by risk. Keep AI/executor separation; consider sharing deployment units for ordinary business modules. Record adapter capability differences and conformance failures. Choose one API-contract generation direction: the OpenAPI-first contract in §12.4 must govern any huma generation.

**Acceptance:** Every required control has an actual deployment configuration or tested alternative; unsupported portability claims stay conditional. Repository operations cannot accidentally commit independently.

### F43 — The operating-cost budget is incomplete and budgets are not spending limits

**H · Design gap · References:** §12.6, NFR-COST-01/02, R9/R11/R15. **Owner:** Founders/operations lead.

The cost table mixes the former warm API with min-instances-zero language and omits or under-specifies malware-scanner operation, email, Access Point fees, static egress, independent monitoring, full evidence-hash scans, live AI evals, restores and identity edition costs. “Everything else scales to zero” does not eliminate fixed edge/security costs or intentional scheduled compute.

**Recommendation:** Build a workload-based estimate with request duration, open-tab streaming, scanning, storage growth, audit/log volume, model input/output/cache mix and partner quotes. Include engineering/support effort separately. Define alerts versus enforceable AI quotas, and safe prioritisation when a quota is reached.

Technology choices should be justified by representative measurements and operational skill. A framework's 0.x version or a benchmark on another serverless platform is insufficient evidence that it is unsuitable here.

**Acceptance:** A staging month or representative load run reconciles predicted and observed costs. AI budget exhaustion cannot stop statutory deadlines or authorised core accounting operations.

### F44 — Current evidence resolves the Opus-region contradiction, but does not settle every residency claim

**H · Confirmed · References:** AI-26, ADR-T2 index, §12.5, §12.12 hard tie 2, supporting bank/payments research. **Owner:** AI/infrastructure lead/DPO.

§12.5 says Opus 4.6 and Sonnet 4.6 are in Singapore, but §12.12 says only Sonnet 4.6. Google's retrieved model-specific pages, updated 9 October 2026, list **asia-southeast1 availability and ML processing for both**. The later Sonnet-only restriction should not govern implementation. [Google Opus 4.6 details](https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/partner-models/claude/opus-4-6), [Google Sonnet 4.6 details](https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/partner-models/claude/sonnet-4-6).

**Recommendation:** Use model-specific availability, processing and contractual evidence. Check quota, retirement and actual invocation at deployment. Treat endpoint location, prompt-cache location, diagnostic logging and subprocessors separately. “Singapore-only model” does not make the email, identity or entire platform data path Singapore-only.

**Acceptance:** Configuration and data-flow documentation agree with a captured current source and synthetic regional test. Residency wording states which data and processing it covers.

## 7. Findings: AI, integration safety and assurance

### F45 — Matching numbers to tool output does not prove the answer's meaning

**B for numeric-accuracy claims · Design gap · References:** AI-08/21, QA-AI-01, AI-P6, AI-49. **Owner:** AI/reporting lead.

An answer can cite the correct S$100,000 from the wrong customer, currency or period and pass a numeric-membership check. The same problem affects unit changes, signs, percentages and comparison baselines. Counts, dates and statutory numbers also need treatment distinct from ledger amounts.

**Recommendation:** Return typed fact IDs containing measure, dimensions, scope, date, currency, ledger revision and lineage. Bind financial assertions to those facts and calculator provenance. Verify claim support as well as numerical appearance. For missing evidence, return an explicit unknown/refusal rather than an actionable financial figure labelled “unverified.”

**Acceptance:** Tests reject a correct number attributed to the wrong entity, metric or period. Partial tool failure cannot produce a complete answer. Multi-step calculations and a reused number from earlier turns retain explicit provenance.

### F46 — Knowledge compilation needs semantic and override controls; literal-number lint is only a smoke test

**H · Design gap · References:** AI-50…60, QA-AI-06, ADR-T6, FR-ORG-08. **Owner:** Knowledge/AI lead.

A claim can cite an existing page and repeat its numbers while reversing an exception or missing an effective date. Human overlays may also contradict live settings, newer packs or published policy. Approval of a document does not make its embedded instructions trusted.

**Recommendation:** Define claim-level source spans, authority/precedence, effective dates, stale fail-closed behaviour for statutory answers and escalation of conflicting overlays. Recheck scope when assembling index, pages and raw fallback. Include negation, exceptions, conflicting dates and revoked access in evals.

**Keep the founders' no-vector decision.** Full-text/index retrieval is a reasonable current design, but “compilation always fixes retrieval better” is an unproven general assertion. Measure paraphrase, synonym, multilingual and long-index retrieval recall under the chosen design; failures should trigger an accountable improvement process.

**Acceptance:** A policy contradicting an approved setting is flagged before publication. A stale tax guide cannot override the current applicable pack. Passing numeric lint alone cannot approve a semantically incorrect page.

### F47 — Client labels and human approval do not eliminate agent or disclosure risk

**H · Design gap · References:** AI-18/34/40/47, FR-INT-02, SEC-31. **Owner:** Security/AI lead.

The API can enforce the permissions assigned to an agent-class token; it cannot prove that an integration-class token is never used by AI. A human can also approve injected content, while read-only results can already disclose information. External MCP clients can write their own misleading prose; FH controls its tools and first-party answers, not every external model's narration.

OWASP describes prompt injection as a persistent risk without fool-proof prevention and recommends controls beyond model instructions. [OWASP prompt-injection risk](https://genai.owasp.org/llmrisk/llm01-prompt-injection/), [OWASP prevention guidance](https://cheatsheetseries.owasp.org/cheatsheets/LLM_Prompt_Injection_Prevention_Cheat_Sheet.html).

**Recommendation:** State the trust boundary honestly. Keep least privilege, execution checks, data minimisation and egress policy outside the model. Assess integration clients as potential automated actors. Test result exfiltration, hostile links/markup, accumulated disclosure and approval manipulation as well as excluded tool calls.

**Acceptance:** Hard authorisation/egress controls hold under arbitrary model output. External-agent numeric guarantees are limited to structured tool facts, with their provenance supplied to clients.

### F48 — Connector lifecycle, failover and revocation semantics are underspecified

**H · Confirmed/design gap · References:** FR-INT-20/25…28, FR-INT-21, AI-38, FR-BNK-12, FR-TAX-03. **Owner:** Integration/security lead.

The connector-wide “never-list” wording can conflict with legitimate human-approved sending, later payment initiation or filing connectors. “Disable stops traffic at once” also conflicts with asynchronous work already queued or executing and revocation taking up to 60 seconds.

**Recommendation:** Define capability-specific human, system and AI authorities; do not apply the AI never-list indiscriminately to all integrations. Specify disable versus pause versus revoke/uninstall, queued-work disposition, in-flight outcomes, secret rotation, webhook cleanup, parser migrations and rate-limit/circuit-breaker behaviour.

User-configured OIDC discovery, webhooks and test-connection URLs require SSRF controls, including private/metadata addresses, redirects and DNS changes. [OWASP SSRF prevention](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html).

**Acceptance:** A disabled connector cannot commit new work using stale enablement state. Unknown provider outcomes are reconciled before failover. Testing a connector cannot access metadata credentials or internal admin endpoints.

### F49 — Files, exports and offline receipts need format-specific security and integrity rules

**H · Design gap · References:** FR-DOC-07/08/12, FR-TPL-01/06, FR-RPT-05, FR-UX-07/16/20, SEC-24. **Owner:** Web/security/export lead.

ClamAV and extension allowlists do not fully address decompression bombs, XML external entities, active PDF/SVG content, misleading previews or hostile filenames. Imported text can also become a spreadsheet formula on export; plain decimal money handling does not protect customer names or notes. [OWASP CSV injection](https://community.owasp.org/attacks/CSV_Injection).

**Recommendation:** Define MIME sniffing, decompressed/resource limits, sanitised previews, original-versus-derived hashes, SVG handling, XML parser restrictions and template isolation. Preserve safe exact data exports without executing text. Define offline receipt storage, logout deletion, shared-device behaviour, permission recheck on sync and duplicate upload handling.

Unicode normalisation should support matching while retaining original references; distinct legal document identifiers must not be silently merged.

**Acceptance:** A formula-like vendor name exports safely, large archive content is bounded, a template cannot fetch a secret, and logout removes queued sensitive receipt access. Metadata stripping leaves an auditable original/derivative relationship.

### F50 — Statutory completeness and source governance are weaker than the “all clear” promise

**H · Confirmed/design gap · References:** ENG-30…33, FR-CS-01/02/03, FR-EQ-25, AI-65/67, QA-AI-07, Appendix I. **Owner:** Tax agent/company secretary/product lead.

The controller check focuses on a >25% share/vote crossing, but ACRA also identifies control through board appointment rights and significant influence. Its guidance requires annual notices; nominee-shareholder register coverage is not explicit alongside nominee directors. A cap-table percentage check alone cannot establish statutory completeness. [ACRA controller identification and annual notices](https://www.acra.gov.sg/manage/companies/legal-requirements-common-offences/maintaining-local-companys-information-registers/rorc/registrable-controllers/), [ACRA RORC/ROND/RONS guidance](https://www.acra.gov.sg/regulations/practice-directions-registrars-interpretations-guidance/rorc-rond-rons-guidance/).

Appendix I also includes search summaries and imprecise source references. Packs need verified applicability, source sections, effective dates and a review SLA, with safe behaviour when a mandatory update is unaccepted or a retrospective correction is published. Evidence hashes prove byte integrity, not that a screenshot, bank statement or vendor confirmation is authentic; define source assurance and reviewer responsibility separately from retention.

**Recommendation:** Add “unknown/not assessed/missing inputs” to obligation outcomes. Capture authoritative source/version and reviewer decisions; distinguish binding law, guidance, contractual obligations and product policy. Keep human confirmation for ownership/control conclusions and future-law commencement.

**Acceptance:** Missing share-rights data or an outdated ACRA extract prevents an unqualified “all clear.” Every mandatory legal rule has an applicability scenario, boundary fixture, owner and captured primary source before activation.

## 8. Contradictions and stale summaries to reconcile

This is a correction checklist, not additional findings. Some entries are editorial; others change implementation.

| Location | Conflict or stale statement | Governing decision needed |
|---|---|---|
| FR-PLN-05 | Prose says P1; phase column and part of §18 say P2 | One phase for the shared short-term/13-week engine |
| FR-PRJ-04 | Prose says P1 with FR-PRJ-10; phase still P2 | Define actual launch billing capability |
| FR-PSP-03 vs FR-AR-15/FR-KPI-04 | Model B M/P1 depends on subscription data assigned P2 | Promote a minimal dependency or defer Model B |
| §1, §3.3, §17.2, §18 | New P1 capture/bank/provider capabilities coexist with older P2 summaries | Generate summaries from the registry |
| §18 tax row vs FR-TAX-04 | Roadmap still mentions IAF although operative requirement makes it conditional | Audit listings now; IAF only on verified need |
| §4.3 and QA-MIG-01 vs B.3/G.2 | Only allocation differences allowed despite statutory corrections | Signed migration difference register |
| §4.3 vs ALLOC-EVEN | Claimed <S$0.01 monthly difference is false for final residuals | Correct bound or allocation policy |
| ENG-04 vs KPIs/projects/equity | Ledger-only source rule contradicts operational-source measures | Measure-source registry |
| FR-GL-08 vs FR-GL-09 | No person can post in a locked period versus adviser exception | Distinguish hard close from a soft/adviser lock |
| §10.2 payout rows vs FR-PSP-02 | Fee at charge versus fee again at payout | One fee-recognition event |
| DEV-07 vs make up/QA-LOCAL-01 | Production build excludes local adapters but offline boot uses them | Explicit local image/build contract |
| §17.3 vs AUD-03/NFR-IMG-05/FR-ADM-08 | P0 exit work assigned P1, with additional Zoho P0 test not listed in exits | Complete P0 acceptance inventory |
| ADR-T2/§12.5 vs §12.12 | Opus in Singapore versus Sonnet only | Current model-specific evidence supports both 4.6 models |
| AUD L1, FR-UX-14 vs §11.9 | Forever/no hard deletion versus expiry/purge | Retention and archival contract |
| DR-04 vs NFR-DR-02 | Singapore-only recovery copies versus four-hour regional-outage RTO | Narrow promise or alternate-region recovery |
| FM-1 vs FM-8 | Mandatory second person versus single-person break-glass | Explicit exception boundaries |
| AI-63/65/66 vs Appendix E sec.create_task | Agents create tasks during a run versus task creation needing a human proposal approval | Decide deterministic task creation authority separately from AI-authored drafts |
| FR-INT-27 vs later bank/tax capabilities | All connectors bound to AI never-list versus human-approved payment/filing transport | Capability/principal-specific authority |
| FR-UX-34 vs tour anatomy | “At most 7” steps versus a list containing more distinct destinations | Design a measured role-specific tour; do not blindly enumerate |
| QA-UI-06 vs FR-UX-34 | “No API write during tour” versus stored tour progress | Ban financial/domain writes; explicitly permit tour-state persistence |
| §20 defaults | Recommended decisions remain phrased as unresolved questions; stale bank-aggregator default | Record accepted/rejected decisions, not infer acceptance from a default |

## 9. Researched feature opportunities

These are proposed enhancements, not a request to expand P1 indiscriminately. “Extension” means the PRD already has a related feature but omits the proposed depth. The rankings reflect Cognizhi's stated AI software, subscriptions and consulting business; they are judgement, not a validated customer-demand study.

| ID | Opportunity | New or extension | Suggested timing | Effort | Primary outcome |
|---|---|---|---|---|---|
| O01 | AI/cloud cost and contribution margin by product/customer | New depth beyond forecast cost percentages | Early P2; minimal import sooner if costs material | Medium | Detect unprofitable customers/models |
| O02 | Usage-to-billing completeness reconciliation | Extension of provider mirroring | With usage-based revenue | Medium | Find missing/duplicated usage charges |
| O03 | Contract renewals and committed-spend register | New | Early P2 | Small–medium | Avoid unwanted renewals and cash surprises |
| O04 | Balance-sheet substantiation and close package | Extension of close/evidence | Minimum at P1 close; fuller P2 | Medium | Prove completeness, not just balance |
| O05 | Collection case management and recovery visibility | Extension of AR reminders | P2 | Small–medium | Resolve disputed/failed collections |
| O06 | Source completeness and integration repair workspace | Extension of connector health | Minimal P1 for live connectors | Medium | Make outages and backfills reviewable |
| O07 | Liquidity forecast with commitments and uncertainty | Extension of cash forecast | P2 after deterministic forecast works | Medium | Make cash decisions under uncertainty |
| O08 | Grant/project claim and restricted-funding evidence | New | When a grant is actually used | Small–medium | Prevent unsupported/late claims |
| O09 | Accountant adjustment and review working papers | Extension of time-boxed accountant access | P1 minimum/P2 enhanced | Small–medium | Reduce review cycles without weakening controls |
| O10 | Contract change orders and revenue schedule register | Extension of projects/subscriptions | With the supported revenue model | Medium | Connect legal terms, billing and recognition |
| O11 | Cap-table assumption and exit-waterfall review | Extension of round modelling | P2 or when fundraising | Medium–large | Explain economic rights, not only percentages |
| O12 | Compliance confidence/completeness ledger | Extension of secretary/control status | Minimal P1 | Small–medium | Prevent a false “all clear” |

### O01 — AI and cloud unit economics

Forecasting hosting and AI/API expense as percentages of revenue is insufficient for an AI business with variable inference costs. Import model/API/cloud usage and invoices; associate costs with product, project and customer using approved allocation rules. Show contribution margin after model, infrastructure and provider fees, with allocated versus unallocated cost and estimated versus invoiced amounts.

FOCUS provides vendor-neutral technology billing fields and invoice-reconciling billed costs. It is an appropriate normalization reference; it does not turn every provider usage estimate into a ledger liability. [FOCUS overview](https://focus.finops.org/what-is-focus/), [FOCUS billed-cost definition](https://focus.finops.org/docs/specification/v1-0/columns/billed-cost/).

**Minimum version:** Monthly cost CSV plus customer/product allocation and an unallocated bucket; no per-token data in the GL. **Proof of value:** Explain total imported cost back to an invoice and identify customers below a chosen margin. Measure unallocated cost and time to identify overspend.

### O02 — Usage-to-billing reconciliation

Where subscriptions include usage, compare product usage, rated provider records and final invoice lines. Track missing customer mappings, late events, duplicate usage, credits and differences between billing and recognised revenue. Keep the provider as billing system of record if Model B is chosen.

Stripe documents usage recording/aggregation and a separate higher-volume ingest model. That supports buying the rating/collection layer rather than rebuilding it inside the ledger. [Stripe usage-recording documentation](https://docs.stripe.com/billing/subscriptions/usage-based/recording-usage).

**Minimum version:** A reconciliation report for one billing metric with bounded date windows, event identity and exceptions; read-only provider import. **Proof of value:** A planted missing/duplicate event is identified before invoice close. Enable only if Cognizhi actually charges for usage.

### O03 — Renewal and committed-spend management

Record contract owner, recurring amount/currency, renewal date, cancellation notice deadline, minimum commitment, price changes and linked POs/bills. Forecast future committed cash separately from posted payables. Notify before the notice deadline, which may precede the renewal date substantially.

Ramp documents contracts linked to POs, expansion requests and renewal reminders. The pattern is useful; vendor claims of savings are not adopted here. [Ramp contracts and renewals](https://support.ramp.com/contracts-renewals/).

**Minimum version:** Manual contract register and reminders, with evidence. **Proof of value:** Every material vendor commitment has an owner and action-by date; cash forecasts include non-cancellable commitments without posting unearned liabilities.

### O04 — Balance-sheet substantiation and completeness

The close checklist should provide an account-by-account support package: opening/movement/closing balance, independent supporting schedule, reconciling items, ageing, owner and reviewer. Add unbilled-service accrual review from contracts/POs and subsequent bills. A perfectly balanced TB can omit an expense entirely.

BlackLine describes reconciliation as comparing GL balances against independent systems or supporting documents; that is the useful pattern, distinct from recomputing the same ledger twice. [BlackLine account reconciliation](https://www.blackline.com/resources/glossaries/account-reconciliation/).

**Minimum version:** Required supports for bank, provider clearing, AR/AP, tax, payroll, deferrals and director accounts. **Proof of value:** A planted missing accrual and stale reconciling item prevent an unsupported close. Preserve the preparer/reviewer evidence already required.

### O05 — Collections and revenue recovery

Extend reminder emails with a promise-to-pay date, disputed amount/reason, case owner, customer communication, agreed schedule and escalation. Mirror failed provider collections, next retry and terminal status rather than treating the subscription as simply unpaid.

Stripe documents retry schedules and failed-payment recovery; the platform should display those outcomes and avoid a second conflicting charge/retry engine. [Stripe revenue-recovery documentation](https://docs.stripe.com/billing/revenue-recovery/smart-retries).

**Minimum version:** Collection case linked to the invoice, with manual promise-to-pay and read-only provider recovery status. **Proof of value:** Measure resolution time and overdue disputed versus undisputed balances; do not count repeated retries as revenue.

### O06 — Source completeness and repair workspace

“Connector healthy” is not “all financial data received.” Expose complete-through date, source cursor, expected versus observed counts/balances, gaps, quarantined items, parser version and replay history. Provide controlled backfill/reparse with preview and a reconciliation bridge.

This follows from the existing bank continuity requirement and provider event limitations, rather than inventing another integration framework. The first provider's unordered/retried events make a repair path essential; F21/F37 supply the evidence and failure model.

**Minimum version:** One workspace for the connectors actually enabled at launch. **Proof of value:** A deliberately missing source interval is visible, repaired without duplicate postings, and independently reconciled. A green API ping cannot clear a financial gap.

### O07 — Decision-oriented liquidity forecasting

Extend open AR/AP forecasts with committed contracts, restricted cash, tax reserves, expected payment ranges and explicit funding assumptions. Show deterministic scenarios first; use probabilistic estimates only when sufficient payment history exists and label the uncertainty.

This is an inferred extension of FR-PLN-09 and O03. It should reuse the existing daily/weekly engine, not create a second forecast system.

**Minimum version:** Expected/best/delayed dates, minimum cash and action thresholds; a proposed financing receipt is separate from confirmed cash. **Proof of value:** Back-test forecast error at 4 and 13 weeks and explain the causes. “Runway” states whether it excludes restricted funds and assumes successful fundraising.

### O08 — Grant and restricted-funding evidence

Link a grant/award to its offer letter, qualifying period, deliverables, eligible expense categories, claim deadline, payments and required review. Prevent an expense from being claimed twice across funding sources; surface unsupported or out-of-period cost.

Enterprise Singapore's EDG guidance describes reimbursement claims backed by deliverables and cost documentation, with claim timing tied to the project. Exact obligations should follow the award letter and current scheme, not a generic hard-coded grant rule. [Enterprise Singapore EDG claims](https://www.enterprisesg.gov.sg/financial-support/enterprise-development-grant/).

**Minimum version:** A funding-source dimension plus an evidence checklist and reconciled claim working paper. **Proof of value:** Every claimed amount traces to eligible, paid/evidenced cost and the applicable award. Do not assume Cognizhi qualifies for any particular scheme.

### O09 — Accountant working papers and adjustment review

Add a review workspace containing an immutable period snapshot, issue list, proposed adjusting journals, tax/GST differences, management responses, evidence and signed resolution. Preserve versions of the review pack when books are reopened.

This is an inferred workflow extension of FR-RPT-10, FM-7 and FR-GL-07; most underlying capabilities already exist. It should not become a general document-management product.

**Minimum version:** A period-linked review checklist and adjusting-journal proposals through the existing approval path. **Proof of value:** Measure time from export to accountant sign-off and count unresolved issues; an accountant cannot bypass controls through an uploaded adjustment file.

### O10 — Contract changes and revenue schedules

Connect contract/SOW, milestones, acceptance evidence, billing schedule, amendments and recognition schedule. A change order should explain revised project budget and recognition basis. Keep billed, collected and recognised amounts side by side.

IFRS 15's contract/performance-obligation distinction supports the need for this separation where that framework applies; F06/F19 establish the applicability constraint. The feature can initially support only the accountant-approved recurring-service and consulting cases.

**Minimum version:** Structured service dates and approved contract/amendment links, with a deferred-revenue movement report. **Proof of value:** An annual subscription amended mid-term remains reconciled across invoice, cash and recognition without retroactively rewriting prior close.

### O11 — Economic rights and exit waterfall

The cap table gives ownership percentages but not the cash outcome of liquidation preferences, participation, seniority and conversion elections already stored in FR-EQ-16. A sandbox waterfall can expose whether ordinary ownership percentages tell a misleading story.

This is an inferred enhancement from the PRD's rights model. Implement only documented, supported instruments with legal/accountant review; do not market a universal legal interpretation engine.

**Minimum version:** Assumption sheet plus waterfall for one preference structure; compare as-converted and preference outcomes. **Proof of value:** Independent hand-pinned cases reconcile proceeds and demonstrate boundaries. Defer until the company has relevant financing terms.

### O12 — Compliance completeness and confidence

For each obligation, record applicability, rule/pack version, required inputs, latest evidence, source freshness, owner and decision. “Unknown” is first-class. Separate an upcoming deadline, a missed filing, evidence not yet uploaded and an obligation that does not apply.

This extends FR-CS-03 and CMP-01 rather than adding another AI agent. ACRA's controller guidance illustrates why percentages alone are insufficient; F50 identifies the governing evidence.

**Minimum version:** A deterministic obligation checklist with explicit missing inputs; AI explains its result. **Proof of value:** An incomplete company profile cannot receive a false all-clear, even if the model is unavailable.

### Features to defer or integrate unless actual demand justifies them

- Advanced visual/code template editing: begin with compliant branded templates and fixed fields.
- Multiple interchangeable providers at launch: support the chosen provider well and retain ports for later adapters.
- Ticketing, A2A, generic workflow builders and connector marketplaces: use existing team tools until transaction volume creates a measured need.
- Peer benchmarking: licensing, cohort comparability and reidentification need more than a ten-company cell threshold.
- Complex cap instruments, anti-dilution and worldwide statutory packs: support a narrow verified set and refer the rest for review.
- Certifications before a customer or assurance objective requires them: preserve foundational controls, but distinguish readiness from independent certification.

## 10. Recommended enhancement sequence

### Wave A — Make the baseline coherent before design freeze

1. Decide own-books versus funded product investment; complete the build/buy/hybrid comparison.
2. Record actual GST, payroll, banking, provider-billing, financing and accounting-framework applicability.
3. Replace inconsistent phase summaries with one requirement registry and a dependency-checked launch subset.
4. Resolve parity, SDL, tax FX, input-tax eligibility, settlement and provider-posting rules with the accountant/tax agent.
5. Decide residency/recovery promises, retention boundaries, identity edition and local-image contract.

**Exit evidence:** Signed decisions, launch requirement matrix, supported-transaction catalogue and migration-difference register. This is specification work, not a mandate to implement all fifty findings.

### Wave B — Prove the high-risk foundations

Build focused feasibility experiments for:

| Experiment | Evidence that should result | Relevant findings |
|---|---|---|
| Identity and real MCP client | Selected edition/version; issuer-qualified account linking; MFA/recovery; audience/exchange/revocation | F28–F31, F41 |
| Concurrent ledger/period close | Real PostgreSQL tests demonstrating isolation, same-tenant references and race outcomes | F18, F26/F27, F33 |
| Outbox crash/replay | Durable completion, external unknown outcomes, duplicate execution and restore recovery | F21, F37/F38 |
| Local boot and deployment hardening | Clear image/build modes; actual cloud capability matrix; credential-free smoke evidence | F40/F42 |
| Retention/audit restore | FY-based retain-until, purge boundary proofs, key lifecycle and no resurrection | F34/F35 |
| Recovery | Timed scenario-specific recovery of data, keys, files, auth and operations | F36 |
| Supported provider lifecycle | Finalised unpaid invoice, charge, fees, tax invoice, refund/dispute and payout bridge | F19/F20 |

**Exit evidence:** Passing experiments plus explicit unresolved limitations. Do not treat a mocked success as proof of partner or cloud behaviour.

### Wave C — Deliver the narrow books-and-controls release

Implement the actual transaction types, immutable posting, imports, AR/AP, required payroll/tax arrangements, essential reports, bank/provider reconciliations, evidence, approvals, help and accountant review. Include statutory features when applicable, using controlled manual procedures if automation is deferred.

Keep a clearly labelled sample organisation and full offline test loop. Start AI with read tools and a small set of bounded proposals after the underlying workflows work reliably.

**Exit evidence:** Accountant-reviewed parallel run, explained differences, evidence-backed close, legal-output readiness for enabled modules and practiced recovery.

### Wave D — Add measured business value

Prioritise O01/O03/O04/O06/O09/O12 according to observed cost, close and review pain. Add O02/O10 with actual usage billing/contract changes; O05/O07 with collection and cash needs; O08/O11 when grants/fundraising create demand. Revisit SaaS expansion with actual support and assurance costs.

## 11. Additional acceptance scenarios for the next PRD revision

These proposed scenarios close the important evidence gaps. They are not tests added in this review.

| Scenario | Required outcome | Trace |
|---|---|---|
| S$700 payroll versus legacy workbook | Correct statutory result with a signed migration bridge | F05/F13 |
| S$1 spread across six periods | Exact sum and disclosed residual policy; no false per-period bound | F05 |
| Multiple pay runs with fractional SDL | Employer-month payable and residual reconcile once | F12/F13 |
| FC purchase with supplier SGD GST different from book FX | Claim invoiced SGD tax; balanced books and explicit difference | F08 |
| Valid simplified receipt/employee-agent purchase | Eligibility determined by conditions rather than missing company name alone | F09 |
| GST correction after filing/ledger close | Original filed return preserved; approved correction working paper | F11 |
| Non-Peppol purchase and provider B2C sale | In-scope tax-data completeness established or justified exclusion | F10 |
| Non-citizen leaver with pending claim and advance | Clearance applicability and lawful hold/deduction across payment paths | F14 |
| Short payment outside a minor rounding limit | Residual remains open unless a reason-specific adjustment is approved | F17 |
| Revalue and partially settle USD AR | Foreign principal unchanged; carrying value and realised/unrealised FX reconcile | F18 |
| Finalised unpaid subscription invoice, then retry success | One receivable/revenue basis, one settlement, no duplicated recognition | F19/F21 |
| Provider fee tax invoice after transaction fees | No duplicate fee expense; valid tax amount and clearing reconciliation | F20 |
| Same bank date/amount/reference on two legitimate payments | Both retained; ambiguity reviewed | F21 |
| Cash before legal share allotment | Bank receipt exists before share capital takes effect | F23 |
| Backdated entry racing with issued report | Frozen report remains reproducible | F26 |
| Close racing with posting | One consistent serial outcome, no posting into the completed close | F27 |
| Vendor bank details change after batch approval | Approval invalidated or execution blocked for re-approval | F28 |
| Requester asks AI for own high-value journal | Business SoD still requires an eligible independent approver | F29 |
| Lost founder authenticator and failed sign-in provider | Controlled recovery without unaudited finance bypass | F30/F31 |
| Cross-tenant reference plus restricted cached report | FK/action rejected; no cached or side-channel disclosure | F32/F33 |
| Upload at FY start and failed upload/DB commit | Correct retain-until; safe quarantine/orphan lifecycle | F34 |
| Restore after expiry/key destruction | No personal-data resurrection; remaining audit proofs verify | F35 |
| Entire Singapore region unavailable | Advertised recovery scenario demonstrated or promise explicitly narrowed | F36 |
| Crash after email/provider acceptance | Unknown outcome reconciled; no duplicate financial posting | F37 |
| Scheduler tick disappears | Independent alert reaches an owner | F38 |
| Correct figure attributed to wrong month/customer | AI assertion fails semantic grounding | F45 |
| Stale statutory guide or missing controller-rights data | Unknown/needs-review; no unqualified all-clear | F46/F50 |
| Formula-like note, hostile SVG and offline logout | Safe export/preview and removal of local sensitive access | F49 |

## 12. Evidence quality and research follow-ups

### Findings supported by direct official corrections or counterexamples

F05 (parity and arithmetic), F08 (tax FX), F09 (input-tax eligibility), F12 (SDL), F14 (required payroll workflows), F16 (precision), F17 (tolerance exposure), F36 (regional recovery), F40 (local-build contradiction), F41 (published identity edition matrix), F44 (model regions), and F50 (controller completeness) have especially strong baseline evidence. Their remedies still need an implementation decision; supporting evidence does not imply a universal legal conclusion.

### Matters not settled by this review

- Actual Cognizhi framework, tax registration, staff/employment-pass status, instrument terms and transaction population.
- Singapore DBS report samples, API contract, pricing, security model and permissions; an India onboarding form is not a Singapore agreement.
- Access Point data-submission scope, acceptance semantics, outage/correction procedure, production eligibility and fees.
- Exact deployed Hydra version/edition, licensing rights and client support; a product matrix and a real conformance test are both needed.
- CPF/SHG year packs and every PR election/correction edge case; this review verified selected rules, not the entire statutory calculator.
- Singapore accounting-standard paragraphs behind geographic/licensed access. ACRA explicitly documents access restrictions; a secondary or equivalent-country standard is not a substitute for confirming applicable Singapore text.
- Commercial processor fee-GST particulars and the company's actual invoices, provider reserves, reporting mode and supported currencies.
- Security of any implementation, cloud configuration or live deployment; no penetration or code audit was performed.
- Competitor-wide claims such as “no vendor offers this,” “no undo anywhere,” or roadmap features being live in Singapore. Replace universal negatives with bounded observations and dated feature evidence.

### Source governance for subsequent enhancements

For every externally governed rule or capability, keep the primary URL, source title/section, retrieval date, effective/commencement date, version/edition/region, applicability conditions, captured evidence or hash where permitted, verifier and approval. Record whether the evidence is a law, official guidance, contractual term, vendor capability or reviewer inference.

Use official source text and examples to derive pinned fixtures. Treat old search summaries as research leads, not implementation authority. A pack update must not silently change historical postings; a retrospective correction needs an assessed adjustment workflow.

Keep this report's finding IDs stable in the enhancement backlog. For each adopted item record the resulting requirement IDs, decision owner, acceptance evidence and disposition: adopt, adapt, defer, reject with rationale, or validate. Rejected recommendations should remain visible so the baseline does not lose the reasoning.

### Reproducible scope inventory

The scope counts in F01 use rows with exactly four Markdown table cells: one exact FR/AI/ENG/SEC/AUD/DR/DEV/REL/CMP/QA/NFR requirement ID; requirement text; plain M, S or C; and explicit phase. Count a row towards P1 if its phase cell contains P1. This yields 493 unique IDs, 377 P1 IDs and 319 M/P1 IDs. Conditional priorities and other table shapes are deliberately excluded. The marker count searches case-insensitively for “[verify” and counts 56 occurrences, not 56 distinct unresolved legal questions.

This is a transparent indicator of specification breadth. It is not a replacement for the proposed registry, nor proof that each row is a separate development task.

## 13. Review completion

The report contains fifty traceable findings, twelve prioritised feature opportunities, a contradiction checklist, implementation-decision sequencing and additional acceptance scenarios. External links support the adjacent attributed claims; the remainder is explicit specification analysis or a proposed remedy.

The reviewed PRD was preserved. No repository files were changed and no application tests were run; the only artifact created is this temporary report. The repository is pre-P0, so runtime and deployment guarantees remain unproven.
