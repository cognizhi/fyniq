# Design

Decisions marked **[owner to confirm]** are the recommended option, taken while the owner was away (23 Nov 2026 brief); each can be
reversed in a later revision.

## D1. The report definition

A saved report is a definition `fyniq-report@1`, a JSON object: `rows` (each `{id, label, kind}` where kind is `account` with a
`code`, `group` with a statement group key such as `revenue`, or `calc` with a `formula` over earlier row ids), `columns` (`period`,
`prior_period`, `prior_year`, `ytd`, each with a label), `filters` (department and project dimension values, `include_zero`)
and `format` per row (`amount` or `percent`). It runs through the balance service as at a ledger revision (FR-RPT-08), so it reads the
same books as the statements and carries the revision. A formula is `+ - * /` and brackets over row ids and decimal constants; it is
evaluated in exact decimals at the engine's precision; a division by zero shows the cell as not available, never as zero or an
error; a percent row multiplies by 100 and rounds under `RND-PERCENT` (the statements' policy for percentages). No floating point. At most 60 rows and 12 columns. A circular or forward reference is refused with a code that names
the row.

## D2. Who builds, shares and reads

A definition is stored per organisation with an owner, a name and a visibility of `private` or `shared`. A shared definition is
read by holders of the Reports view right who see the whole ledger (the roles that read the statements); a role limited to a part of the
ledger (sales, purchases, payroll, a department, the board pack) never runs a custom report, because a definition can name any account.
Saving a definition needs the right to create under Reports (Appendix D). The definition's version is an ETag.

## D3. Scheduled reports

A schedule names a definition, a cadence (weekly on a weekday, or monthly on a day, in the organisation's time zone), a period rule
(`last_closed_month` or `month_to_date`) and recipients (active members). The daily job `jobs report-schedules` builds each due run
with the organisation's state at that moment and, for each recipient, checks again that the person is an active member who may run
the definition (D2); one who may not is skipped and the Owner sees why. Email carries a message with the report's name and period and a
link to the signed-in page, plus the headline figures only when the Owner has switched "figures in the email" on for the organisation
(off by default, [owner to confirm]); it never attaches a file. Slack is a connector delivery: the channel adapter is built behind the
port with a fake and the live post is `[live-verify]` (Q22 and a workspace token). A failed send is retried by the outbox and then
raised as an Inbox task.

## D4. The board pack

A board pack is one per month: a draft the Controller or Owner builds from the closed month (cover, highlights, P&L, balance sheet,
cash flow, KPIs, runway, commentary), each section holding either figures the system computed (always from the same statements and KPIs,
with the ledger revision) or text a person wrote. Commentary is plain text with an author; the AI's draft (`rpt.draft_commentary`, tier 1)
is a proposal a person accepts into the text (built in `p2-ai-expansion`); the pack builds and publishes without it. Publishing freezes
the version (insert-only; a correction is a new version that supersedes it, the earlier one stays readable) and is itself approved by
a person other than the one who published a draft they wrote only when the organisation has dual control on (the approval engine's
object `board_pack`); the default follows the engine's setting. A PDF is rendered from the published version by the render service
and kept as evidence with its SHA-256.

## D5. Access-controlled links are sign-in pages

The PRD's "access-controlled link" is a link to a page that needs the reader's own sign-in and role: a Board / investor viewer
signs in and the page checks their row-scoped right at read time. There is no tokenised public link and no bearer link in the email
(a widening of the access model, which waits for the owner; the alternative would be a time-limited signed link with a one-time
passcode, which gains convenience for investors without accounts and loses the audit of who read what).

## D6. Investor updates

An update is built from a published pack's period: its KPIs, cash, runway, highlights and the asks (text). The distribution list
is investors with an access record (D7) and the Board role. Sending creates a notice per recipient (email without figures, pointing to
the signed-in page) and a read record when the recipient opens it. An archive lists past updates. The update's text is edited like the
pack's commentary; the sent version is frozen.

## D7. Investor access

An access record links a user (a Board / investor viewer) to a holder (a contact marked investor or shareholder) and holds a flag
`full_cap_table` that only an Owner sets, audited, with a reason, and that ends when the Owner revokes it. By default the viewer reads
the updates, the packs and the holdings of their own holder. An employee with an option grant reads their own grants only, by the same
row scope.

## D8. Investor obligation tasks

Each information right of an investor (FR-EQ-22: quarterly accounts within 45 days, annual accounts, a board pack...) generates the
calendar tasks of the period it covers (the statutory calendar's task mechanism), due at the right's deadline counted from the period end;
delivering the update or pack to the investor closes the task with that version as its evidence.

## D9. Dashboards

A dashboard layout is a list of tiles `{tile, x, y, w, h}` over a catalogue of tile kinds (the KPI tiles of FR-KPI-01, the charts of
FR-VIZ, runway, cash); a layout belongs to a role (an Owner or Administrator sets it) or to a person, and a person's own layout wins over the
role's. A tile shows when it was last updated (the ledger revision's time or the input's) and drills down by the same links as the KPI.
A role's layout shows a tile only to a person whose rights read it: a tile a person may not read is left out, never shown empty.

## D10. Customer counts, anomalies, benchmarks

- Counts (FR-KPI-04): a `CustomerCounter` port. The subscription schedules (FR-AR-15) belong to `p2-customer-portal-subscriptions`, so the adapter built
  here counts customers with a recurring invoice template that was active at the month end, and the subscription adapter joins later;
  a manual input is used only when neither source answers, and says so.
- Anomalies (FR-KPI-06): a daily job compares each expense category's spend this month with its trailing 12 months. It raises an Inbox
  item when the spend exceeds the mean by more than three standard deviations. The test is exact: with `n` months of history, this month's spend `s`,
  their sum `S` and sum of squares `Q`, the sample variance is `(n*Q - S^2) / (n*(n-1))`, and the alert is `n*s > S` together with
  `(n*s - S)^2 * (n-1) > 9 * n * (n*Q - S^2)`: no square root and no floating point.
  Fewer than 6 months of history raises nothing. One alert per category and month.
- Benchmarks (FR-KPI-07): benchmark packs are effective-dated JSON like statutory packs, with `source`, `licence`, `year`, `sample` and
  bands (p25, median, p75) per KPI and SSIC code and size class. **No real figures are shipped**: the mechanism, the loader and the page work, tested
  on a synthetic fixture marked as such; the licences of Singapore Department of Statistics surveys and published SaaS benchmark reports are
  `[verify licences]` and the owner's to clear. A comparison always shows source, year and sample, and is hidden when a pack has no
  band for the organisation's code.

## D11. The deferral roll-forward and the asset reports

The roll-forward reads the deferral schedules by customer and contract (deferred revenue) and by supplier (prepayments): opening, added,
released, reversed and closing per month, with a check that closing equals the ledger balance of 2400 and 1210 (INV-07). The asset
schedule is NBV, accumulated depreciation and the movement (additions, disposals, depreciation) for a period. The capital allowance
schedule applies S19, S19A(1) and S19A(10A) from the pack `sg-capital-allowances` with IRAS's page as its source; an asset the pack cannot
classify is listed apart, never guessed [accountant to confirm]. **Recorded 20 Nov 2026:** the company's method for each asset is an election kept on the asset's own version (a
recommended default, no new table): S19's working life is irrevocable and the system refuses to change it; the S$30,000 cap takes whole assets in purchase order and the rest are
listed apart; amounts are in cents by ALLOC-EVEN `RND-CAPALLOW` (IRAS shows whole dollars) [accountant to confirm].

## D12. The rolling forecast and the funding plan

When a period closes, the forecast's month becomes actual (the figures are read from the closed period, the assumptions that produced
the forecast are kept in the version). The weekly cash projection is stored each week as a snapshot (insert-only); the accuracy view
compares each with actual cash at 4 and 13 weeks later and splits the gap into receipts later than expected, unplanned spend and timing,
by rules written in the design's test. The funding plan is a list of planned rounds (date, amount, instrument) in the forecast; the
cap table of the date gives the dilution through the equity register's round model; a funding plan never posts.

## D13. The Auditor's engagement

An engagement has a name, a period, an auditor (a member with the Auditor role) and an end date. Requests carry a title, an assignee
(a named member), a status (`open`, `answered`, `closed`) and evidence the assignee attaches (the evidence rules). A sample is drawn
from posted ledger lines by the auditor's rule (period, account, a minimum amount, a count) using a stored seed so that the draw is
reproducible; each sample opens its documents read-only. The auditor never writes outside the engagement's requests.

## D14. The exit waterfall

A sandbox: it reads the share classes' recorded preferences and posts nothing. Built for the structures a lawyer or accountant has
confirmed, each with hand-pinned cases and the oracle's half: a non-participating preference of a multiple of the issue price,
a participating preference (uncapped) and seniority between classes, next to the as-converted split, with each holder's better
choice. A capped participating preference, a cumulative dividend and a conversion price adjustment are **not built** until the owner
confirms them with the lawyer. The page is unavailable until a class with a liquidation preference exists.

## D15. Allocation rules and shares for services

- Allocation (FR-DIM-03): a rule names a source account set, a key (headcount from the people projection, fixed percentages, or an imported usage file per customer
  and product) and target dimension values; running it produces a draft journal with a line per target, rounded under `ALLOC-PRORATA` (`ALLOC-EVEN` for an even split) with
  the remainder where the policy puts it, and what the key cannot place stays on an `unallocated` line. It posts only when an approver
  approves, like any journal. An imported file's billed total must equal the supplier's bill it explains, or the import is refused. The
  contribution margin report is a management view that reads allocations marked posted only.
- Shares for services (FR-EQ-13): a grant record with the grant-date fair value (entered with its valuation evidence [verify valuation]),
  vesting dates and the service period. The expense is straight-line over the service period by whole months, posted monthly as Dr
  share-based payment expense, Cr equity (share-based payment reserve) through the engine; on settlement the reserve moves to share capital.
  No tax deduction is claimed for newly issued shares. Settling pay already accrued is a conversion (FR-EQ-06), not this.
  [accountant to confirm] the accounts and the straight-line rule.
