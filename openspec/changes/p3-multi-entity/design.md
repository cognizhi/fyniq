# Design

Decisions marked **[owner to confirm]** are the recommended option, taken while the owner was away (21 Nov 2026 brief); each can be
reversed in a later revision.

## D1. An entity is an organisation

PRD-001 §11.4 drew `organisation > entity (P3)`. An organisation already is a legal entity in every way FR-ENT-01 lists: its base
currency never changes (ENG-14), it has its own chart, periods and activated packs, its corporate profile holds the UEN, and every
row, key, index and policy is keyed by `organisation_id` (INV-06, SEC-02). So FR-ENT-01 is read as: several legal entities, each an
organisation, linked in a group. Options: (a) **entity = organisation, a group is a link between organisations** (chosen: nothing
in the ledger, the balance service, row-level security or the 500-odd migrations changes; the tenant boundary stays the legal
boundary); (b) an `entity_id` on every tenant table, key, index, foreign key and policy (rejected: the largest and riskiest change
the codebase could take, and it weakens the tenant boundary the invariants rest on). The designs `Consolidation`, `EntitySetup` and
`PracticeDashboard` already show separate organisations and are adapted, not redrawn.

## D2. The link: invitation, two Owners, narrow consent

A **group** has one parent, the organisation that consolidates, and entities. The parent's Owner (step-up) issues a one-time
invitation code (24 hours, stored as a hash in a global registry, consumed once). The entity's Owner (step-up) enters it in the
entity's own organisation. Before accepting, the page lists what the group may read (D3) and what it may not. Accepting writes a
consent row in the entity's tenant (`group_membership`, in its audit trail and its tenant export) and activates the entity row in the
parent's (`group_entity`). **Both rows must be active for any read**; either Owner ends the link with a reason, by updating their own
row, and the next call finds it ended. An organisation belongs to one group at a time, is never its own parent, and a sandbox
organisation, a practice organisation (FR-UX-36) or the installation's organisation is never an entity. Acceptance is the only cross-tenant write
(one security-definer function that creates the entity's row and activates the parent's, nothing else).

Not chosen: dual approval by the approval engine for the link (the approval objects are per organisation and a link spans two; each
side's Owner already signs for their own organisation, with step-up). A second approver on each side is an easy addition.
Both Owners can be the same person (founders): they switch organisation and sign twice, two audit events.

## D3. The cross-tenant reads

Two security-definer functions, owned by the migrator, fixed `search_path`, `EXECUTE` for the API role only. Each takes the caller from
`fyniq.current_org()` (never from a parameter), checks that both rows of the link are active, switches `app.org_id` to read and restores it
on every exit (as `create_practice_organisation` does), and returns fixed columns:

1. `group_entity_figures(entity, through_month)` for the parent: per account of the entity, per month through `through_month`, the net base
   amount (debit positive) with the account's code, name and type; the entity's base currency, its ledger revision, and each period's state
   (open, soft-locked, closed). Nothing else: no document, contact, employee, bank, evidence, comment, audit or AI data, no dimension.
2. `group_intercompany_offers()` for an entity: the intercompany transactions of linked entities that name the caller as counterparty and are
   offered: pair ID, sender, date, amount, currency, kind, a system-made reference. **No free text**: another tenant's untrusted words never
   land in a draft.

Every call writes an audit event in both organisations' trails (the API does it in the same transaction, as the practice flow does).
The accountant firm's dashboard needs no function (D10).

## D4. Frozen runs

The parent reads the entities' figures once, at run time, and freezes them in its own `consolidation_run` rows (the figures as read, the
rates used, the entities' ledger revisions and period states). A run is insert-only; a correction is a new run. A run is read by holders of the
whole-ledger Reports right and by nobody else: never by the AI read tools, the semantic layer or MCP (the entity's AI exclusions do not
travel with the figures). **After the link ends** the runs stay as the parent's record of what it was shown while consent lasted, marked
"link ended", never refreshed. Options: keep (chosen); delete the entity's lines on the entity's request (a data-protection decision with
retention consequences; wait for the owner).

## D5. Account mapping

The parent maps each entity account to an account of its own chart by code (suggested when the codes are equal, entered otherwise); a mapping
may name a counterparty entity (the account holds an intercompany balance with it). An account with a balance and no mapping blocks the
run, named. The mapping belongs to the parent; the entity cannot change it.

## D6. Rates

The group has its own table of translation rates, per month and currency: a **closing** and an **average** rate (group currency per one unit
of the entity's currency, 10 decimals under `RND-RATE`), entered by the parent's accountant with a source note, insert-only (a newer entry
corrects an older one; all are kept). The entity's own FX rates are never borrowed. A month with no rate blocks the run.

## D7. Translation method `[accountant to confirm]`

SFRS(I) 1-21 follows IAS 21 ¶39 [verify: the standard's text is not on the pages the build could read; ASC and IFRS Foundation pages]:
assets and liabilities at the closing rate; income and expenses at the rates of the transaction dates, for which an average rate for the month
is a practical approximation (¶40); equity at historical rates; the exchange differences into other comprehensive income as a translation
reserve. Built: the balance of an asset or liability account at the closing rate of the run's last month; each month's movement of a
revenue or expense account at that month's average rate; each month's movement of an equity account at that month's closing rate
(the month's rate stands for the date of the movement); the **translation reserve** is the figure that balances the entity's translated
trial balance to nothing, so the consolidated trial balance foots exactly and the reserve is the only balancing item. The result brought forward
(months before the run's range) is shown on its own. Each translated line is `RND-TRANSLATE`. Not built, `[accountant to confirm]`: hyperinflation,
a net investment's loans, the reserve's recycling on disposal.

## D8. Eliminations, differences and scope

An elimination pair is two group accounts that mirror each other, such as due from and due to, intercompany revenue and expense, or the
parent's investment and the entity's share capital. For two entities A and B, the amount of A's pair account with counterparty B and the amount of
B's mirror account with counterparty A are translated first; the **matched amount** is the smaller of the two in absolute value, eliminated from both;
the remainder stays on the consolidated sheet and the run says which side is larger and by how much. **Nothing is plugged.** Eliminations live in the run
and are never posted to any entity's ledger. Scope `[accountant to confirm]`: wholly-owned entities, full consolidation; non-controlling interests, goodwill,
unrealised profit in stock and a part-year of ownership are not built, and an unmatched investment elimination shows as a difference.

## D9. Intercompany transactions (FR-GL-13)

No new posting rule. An entity records the transaction with an ordinary manual journal (on its intercompany accounts, which the parent's mapping
marks with the counterparty) and links it in `intercompany_transaction` (counterparty, kind, pair ID, date, amount, currency, the journal, state). The
counterparty sees the offer through D3's second function, chooses its own accounts, and posts the mirror as a draft journal through the journal command
and its approvals, recording its half under the same pair ID. A pair is *matched* when both halves exist with the same amount and date, and the
reconciliation lists the others. The elimination at consolidation reads balances, not these records, so a difference shows even if a record is missing.

## D10. Accountant-firm mode (FR-ENT-03, FR-ENT-07)

A **firm** is an organisation whose Owner switched firm mode on. (The practice organisation of FR-UX-36, a place to try a task, is another thing; code and
copy say *firm* and *client*.) The firm's Owner issues a client invitation (the same registry); the client's Owner accepts it, which records an
`engagement` in the client's tenant naming the firm. Per-client access is the ordinary membership and role the client's Owner gives a person (invitation,
role, expiry): the engagement grants nothing by itself. The **practice dashboard** lists the clients where the caller holds an active membership
and an engagement names their firm, and for each reads, through that organisation's own unit of work and with the caller's own rights there, a few
counts with no money (oldest open period, last closed month, checks failing, approvals waiting, next statutory deadline). A client where the caller's role cannot see a count
shows it as not allowed. No security-definer function.

## D11. The switcher

The organisation switcher (p1-app-shell 2.1) lists the caller's own memberships; it gains a group label and an "All entities" row, which opens the
consolidated view, shown only to a caller who is an Owner, Controller, Accountant or Auditor of the parent.

## D12. AI and MCP

No tool, scope or grant is added. A group's figures are not read by AI read tools; the assistant answers about one organisation, as today.

## D13. Superseded designs

`Consolidation.html`, `EntitySetup.html` and `PracticeDashboard.html` are adapted (invitation instead of "create with dual approval", the rate card, the run's
frozen banner, the firm's list). `entity-setup` becomes "Entities of the group".
