# Build, hybrid or buy: the comparison question 26 asks for

PRD-001 open question 26 and default 26 (rev 0.25): P1 build starts once this page is signed. **Signed for build on 19 Oct 2026** (signature block below). It compares,
over **24 months (Nov 2026 – Oct 2028)**:

- **A · Buy:** Xero Premium as the ledger, and Talenox for payroll once someone is paid;
- **B · Build:** Fyniq as planned, plus Nemura's payroll after go-live (default 4);
- **C · Hybrid:** Xero and Talenox keep the statutory ledger and payroll, and Fyniq's evidence, planning
  and proposal layers are built around them through Xero's API.

Prices are the day's list prices (17 Oct 2026), in S$ **including GST**: Cognizhi is not GST-registered
(question 2), so the GST is a cost. Sources and workings follow the signature block. "est." marks this
draft's estimates; replace them with your own figures where you have better ones.

| Over 24 months | A · Buy | B · Build | C · Hybrid |
|---|---|---|---|
| Founder build time | Setting up Xero and moving the workbook's balances and open items: 20–40 h est. | **Your budget, below.** The PRD, the designs and P0's local build took six calendar days of AI-assisted sessions (12–17 Oct 2026). 1,243 requirements and 199 screens remain: 12 times P0's 106 requirements | About 60% of B est. (evidence, planning, the AI layer and the Xero sync), plus A's 20–40 h |
| Founder maintenance time | About 1 h a month: users, bank feeds, app updates (≈ 24 h) | 4–8 h a month after go-live: dependency and image updates, scan findings, restore drills, incidents (≈ 72–144 h est.) | As B (≈ 72–144 h est.), plus Xero's API changes, such as its 2026 scope and pricing changes |
| Accountant involvement | Year-end and returns, as today; accountants usually know Xero | As A, plus review of the posting rules and policies and UAT (§17.4 item 3, A3): 10–20 accountant hours est., at a fee to obtain | As A, plus a check that proposals post correctly through the API (≈ 5 h est.) |
| Yearly statutory updates | The vendors' job (GST in Xero, CPF and the rest in Talenox) | The founders': effective-dated packs from official sources (§10.6), 10–20 h a year est. (≈ 20–40 h) | The vendors', as A |
| Security operations | The vendors' (Xero and Talenox); the founders keep MFA and user reviews | The founders': the CI scans already built (free tools), patching and monitoring, all in the maintenance hours above. The annual penetration test applies only before a SaaS launch (SEC-25, P2), at a price to obtain | The founders' for the layers, as in B, plus keeping a Xero token that can write |
| Licences, cloud and tools (cash) | Xero Premium S$2,400; Talenox S$260–460 more if someone is paid from month 13 | Cloud from month 7 S$6,700–10,700; one Claude Max plan S$3,300–6,700; AG Grid S$1,400 | Xero S$2,400 (Talenox as A); the layers' cloud S$4,400–8,500 est.; Claude Max S$3,300–6,700; Xero API S$0 on the free tier, but journal-level reads start at A$1,445 a month |
| **Cash total** | **≈ S$2,400** | **≈ S$11,400–18,800** | **≈ S$10,100–17,600** |
| Support | Included in the subscriptions | None: the founders are the support (key-person risk) | The vendors' for the ledger; the founders' for the layers |
| Exit cost | History stays in Xero's format; reports and CSV export; leaving takes 20–40 h est.; Xero has raised its prices in 2025 and in 2026 | The code (MIT licence) and the data (PostgreSQL) are Cognizhi's; moving to Xero later costs A's 20–40 h, and the build is then sunk | Both: A's exit, plus rewriting the layers |

**On cost alone, buy wins.** Build costs about S$9,000–16,400 more cash over 24 months, before any
founder time, and every hour adds to the gap. For scale, 10 hours a week for 24 months is about 1,040 hours. Hybrid costs
almost as much cash as build and most of its hours, and it gives up both reasons to build, listed next.

**What only a build delivers:**
1. **The objective of question 1.** Cognizhi's books come first and the multi-tenant design is kept, so
   offering the product to others can be decided after six months of live use. Buying leaves nothing to
   offer.
2. **The control model.**
   - The AI holds no write credential. Every AI change is a catalogue proposal that a named person
     approves, and it can be undone with an audit record (AI-31).
   - The database enforces balanced entries, immutable posted history, locked periods and tenant
     isolation (INV-01…13).
   - Two independent implementations agree to the cent (QA-ENG-02).
   - Every important activity carries evidence (§8.16).
   - Xero's official MCP server has write tools on by default (§5.3), and Xero's softer lock level
     still lets advisers post into a locked period (§5.2).
3. **What one product carries:** the workbook's statuses and checks, founders' money and the equity
   register, and Nemura behind the same sign-in.

**What hybrid cannot enforce.** Approvals and evidence cover only the changes made through Fyniq: Xero's
users, JAX and other apps change the same records directly. The executor holds a Xero token whose reach
is set by Xero's scopes, not by the catalogue and the database. Tracing a figure to its journal lines
needs the Journals endpoint, which starts at the Advanced tier after Xero approves the use case. The free
tier allows 1,000 calls a day.

**Recommendation.** Sign **build** only for objective 1 and the control model, with a time budget you are
willing to spend. If Cognizhi's own books are the only goal, sign **buy**: P1 then becomes moving to Xero,
plus whichever of evidence, planning and proposals is still worth building on top.

## Signature

| | |
|---|---|
| Decision | ☒ Build ☐ Hybrid ☐ Buy |
| The objective only a build meets (question 1) | Keep Fyniq a product that can be offered to other companies after six months of live use, under the control model above |
| Founder time budgeted | 10 hours a week to start, as a trial that the founders adjust as the pace shows; reviewed every 3 months against progress on the launch subset |
| Signed (each founder) | cyyeong, 19 Oct 2026 |
| | Open: the second founder signs here when ready |

---

## Prices and sources (read 17 Oct 2026)

| Item | List price | Tax basis | Source |
|---|---|---|---|
| Xero Premium (multi-currency; Starter and Standard have none) | S$95 a month; **S$99 from 1 Nov 2026** for new and existing subscribers. Standard S$70 → S$77; Starter S$39 → S$42, capped at 20 invoices and 5 bills | Includes GST | [Pricing plans](https://www.xero.com/sg/pricing-plans/), [pricing update](https://www.xero.com/sg/pricing-plans/update/) |
| Talenox Payroll / Suite (payroll, leave, claims) | S$21.80 / S$38.15 a month on annual billing for the first five employees, then S$4.36 / S$7.63 per employee. Monthly billing costs S$27.25 / S$43.60 | Read as including GST: the figures are S$20, S$35, S$4 and S$7 plus 9%. The page no longer says so, and rev 0.8 recorded them as including GST [verify] | [Pricing](https://www.talenox.com/pricing) |
| Xero developer tiers (from 2 Mar 2026) | Starter free: 5 connections, 1,000 calls a day, no App Store listing. Core A$35, Plus A$245, Advanced A$1,445 a month. The Journals endpoint needs Advanced, use-case approval and a security assessment. Custom connections and bespoke single-client integrations stay on their old terms, but neither page says whether Singapore offers custom connections [verify] | A$, before tax | [Developer pricing FAQ](https://developer.xero.com/faq/pricing-and-policy-updates) |
| Claude Max (Claude Code included) | "From US$100" a month (5x); US$200 for 20x from secondary sources [verify] | Tax extra ("Prices shown don't include applicable tax") | [Claude pricing](https://claude.com/pricing) |
| Google Cloud | US$220–315 a month for production and US$45–110 for development and staging at Cognizhi's scale (§12.6, rev 0.8 estimate) | Before GST | PRD-001 §12.6 |
| AG Grid Enterprise | About US$999 per front-end developer, perpetual with one year of updates | Before tax | PRD-001 §12.2 (ADR-T1) |

**Workings.**
- US$1 = S$1.28, the last rate found (Aug 2026); check the rate on the day you sign [verify].
- GST of 9% is added to the US$ prices on the assumption that both vendors charge it to Singapore
  customers [verify].
- **Build cloud:** 18 months × US$265–425 → S$6,700–10,700.
- **Hybrid cloud:** the build's figure less the high-availability database, US$175–340 a month → S$4,400–8,500.
- **Claude Max:** 24 × US$100–200 → S$3,300–6,700. The cost is zero if the plan is already paid for other
  work.
- **Xero:** 24 × S$99 = S$2,376. A 1-person payroll for 12 months is S$262–458.
- **Journal-level reads:** A$1,445 × 24 = A$34,680 before tax.

**Assumptions.**
- The launch subset is built locally in months 1–6 and goes to the cloud in month 7. Each month earlier
  adds US$265–425.
- There is no Google Cloud for some time (founders, 16 Oct 2026). Until the cloud exists, Fyniq cannot
  hold Cognizhi's real books (DEV-08), and the workbook carries on. With no cloud in the 24 months,
  build's cash falls to about S$4,700–8,100 (Claude Max and AG Grid). Buy's advantage is then the date
  the books are ready rather than money. Buy still costs less under either reading.
- Nobody is paid before month 13 (questions 4 and 16).
- Premium is priced because the launch subset includes foreign currency (`p1-multi-currency-fx`).
  Standard would save S$528 if Cognizhi has no foreign-currency transactions.
- List prices only. Xero's first-three-months discount (S$19 a month on Premium) is left out.
- Prices after Nov 2027 are held flat, although Xero raised them in both 2025 and 2026.

**Not counted.** Items that are the same in every option:
- bank fees;
- the accountant's year-end and the tax agent's work;
- the hours spent keeping the books.

Items §12.6 leaves unpriced for build and hybrid:
- email;
- the Peppol Access Point (needed only after GST registration);
- Document AI;
- the independent monitor;
- support plans.
