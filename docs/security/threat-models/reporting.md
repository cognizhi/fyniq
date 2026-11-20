# Threat model: the report builder, board packs, investor updates, dashboards and spend alerts

| | |
|---|---|
| Feature and requirement IDs | Report builder (FR-RPT-04), scheduled reports (FR-RPT-07), board pack (FR-RPT-06), investor updates and the Board portal (FR-EQ-23, FR-EQ-24), configurable dashboards (FR-VIZ-09, FR-KPI-05), spend anomaly alerts (FR-KPI-06); `p2-reporting-planning` (PRD-001 rev 0.540 to 0.550) |
| Author, reviewer, date | Claude Code for the owner, 23 Nov 2026; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Report definitions (confidential business data: they name accounts, not figures); a report's figures, a published board pack and the investor update (confidential business data: statements and KPIs); recipients (user IDs of members); alerts (confidential business data) |
| Who acts | Members who read the whole ledger's reports (run and save reports); an Owner, Controller or Accountant (build a pack), an Owner or Controller (publish and send); an Owner or Administrator (a role's dashboard); a Board viewer or investor (reads published packs and updates sent to them); the worker (sends scheduled reports and raises alerts); the AI holds no write here |

## 1. What we are building

A person arranges rows, columns and calculated rows into a saved report; a schedule tells chosen members, by a notification, that a
report is ready. A pack freezes the month's statements and KPIs and a person's texts; an update is built from a published pack and sent
once. A Board viewer signs in and reads what was published or sent to them. A person or role arranges KPI tiles. The worker raises an
alert when a closed month's category spend is three standard deviations above its trend.

Trust boundaries: member ↔ API (every read and write is checked against the member's roles); worker ↔ database (its grants are
column-limited); notification ↔ email (the one place something leaves the application).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Report definition → books | Information disclosure: a role limited to part of the ledger (sales, a department, the board pack) builds a report that names any account | A reader sees figures beyond their rights | Medium |
| T2 | Shared report | Elevation: sharing a report shows its figures to people who could not read them | The same | Medium |
| T3 | Formula | Tampering and denial of service: a formula with deep brackets, huge numbers or a division by zero; a float rounding that changes a figure | A wrong figure, or a stalled request | Medium |
| T4 | Schedule → email | Information disclosure: figures in an email, or sent to someone who has lost the right or left | Confidential figures leave the application | Medium |
| T5 | Schedule | Tampering: a schedule names recipients who may not read the report, or a private report of another person | The same | Low |
| T6 | Board pack | Tampering: a published pack changed after the board read it, or a correction that hides the first version | A board decides on figures that changed | Medium |
| T7 | Board pack and update | Information disclosure: a draft, or an update sent to others, read by a Board viewer; a link that opens without sign-in | Figures read before they are final or by the wrong person | Medium |
| T8 | Roles | Elevation: a Bookkeeper or the AI publishes or sends; a person other than the maker changes a private report | Unreviewed figures sent to investors | Low |
| T9 | Dashboard layout | Information disclosure: a layout names tiles the viewer may not read | The tile shows figures it should not | Low |
| T10 | Alerts | Tampering: an alert changed or raised by the API; an alert raised twice, or for a month that is not closed | Noise, or hidden spend | Low |
| T11 | Database | Information disclosure: one organisation reads another's reports, packs or alerts | Cross-tenant disclosure | Low |

## 3. Mitigations, and what proves each

| # | Mitigation | Test |
|---|---|---|
| T1 | The builder and every saved report need an unqualified view of the reports; a qualified role is refused (403) | `TestOnlyReadersOfTheWholeLedgerUseTheBuilder`, the matrix rows |
| T2 | A report is run under the runner's own rights at that moment; a private report is its owner's alone (not found to others) | `TestAReportIsSavedRunAndChangedThroughTheAPI` |
| T3 | The grammar is `+ - * /` over row IDs and numbers, at most 8 levels of brackets, 60 rows and 12 columns; exact decimals; a division by zero is not available, never zero | `TestACalculatedRowRoundsOnceHalfAwayFromZeroAndNeverDividesByZero`, `TestADefinitionThatBreaksARuleIsRefusedWithItsCode` |
| T4 | The notification names the report and its month and carries no figure and no file; each recipient is checked again at send time and a skipped one is recorded with why | `TestADueReportIsSentOnlyToThoseWhoStillMayReadIt` |
| T5 | Recipients must be active members with an unqualified view; a private report goes to its owner only (RDEF-11) | `TestAReportIsScheduledOnlyForPeopleWhoMayReadIt` |
| T6 | The database refuses any change to a published pack and any delete; a correction is the next version, which supersedes it | `TestABoardPackIsFrozenWhenPublishedAndAnUpdateGoesOnceToItsRecipients` |
| T7 | A reader limited to the board pack sees published packs and the updates sent to them only; there is no tokenised link: the page needs the reader's own sign-in | `TestABoardPackIsFrozenWhenPublishedAndReadAfterSignIn`, `TestAnInvestorUpdateGoesOnceToThoseItIsSentTo` |
| T8 | The Report definitions row is C for five roles, never the AI; the handlers narrow building to the Owner, Controller and Accountant and publishing and sending to the Owner and Controller (BPACK-01) | the matrix rows, `TestABoardPackIsFrozenWhenPublishedAndReadAfterSignIn` |
| T9 | A layout names KPIs of the catalogue and holds no figure; the KPIs' own right decides what shows | `TestADashboardIsTheTilesAPersonOrARoleArranged` |
| T10 | Only the worker holds INSERT on `kpi_alert`, once per category and month; the database refuses any change; only a closed month is looked at | `TestAnAlertIsRaisedOnceByTheWorkerAndALayoutKeepsItsVersions`, `TestASpendAnomalyIsRaisedOnceForTheClosedMonth` |
| T11 | Row-level security on every table | the adapter tests above |

## 4. Residual risk and what is not built

- The full cap table's grant to a Board viewer (FR-EQ-24) is not built: it would widen a read and waits for the owner.
- A published pack's PDF and an approval step on publishing are not built.
- Slack delivery of a scheduled report is `[live-verify]`: no channel posts without the Slack connection.
