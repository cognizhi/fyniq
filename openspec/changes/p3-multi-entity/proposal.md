# Proposal

## Why

Fyniq is one organisation per company. When the founders add a subsidiary (the designs already show Cognizhi Sdn. Bhd. in
MYR beside Cognizhi Pte Ltd in SGD), each needs its own books, and the group needs one set of figures: intercompany balances
eliminated, the subsidiary translated into the group's currency, and every difference shown, never hidden. An accounting firm
that keeps the books of many client companies needs one place to see what needs attention in each.

## What Changes

- **A legal entity is an organisation; a group is organisations linked by consent** (FR-ENT-01, FR-ENT-04). The PRD's
  "entity inside an organisation" is superseded: an organisation already has its own base currency, chart, periods and packs, and
  every row is its own (INV-06). The parent's Owner issues a one-time invitation; the entity's Owner accepts it after the page says
  exactly what the group may read. The link is read-only, narrow (the figures of the entity's accounts, and intercompany offers
  addressed to it), recorded in both audit trails and ended by either side at any time. Screen `entity-setup`.
- **Consolidation** (FR-ENT-02, FR-ENT-05, FR-ENT-06, FR-RPT-12): the parent maps each entity's accounts to its own chart, enters
  closing and average translation rates, and runs a consolidation: each entity's figures translated into the group currency
  (`RND-TRANSLATE`, the translation reserve as the balancing figure), intercompany balances and recharges eliminated pair by
  pair, every unmatched difference shown. The run is frozen in the parent's books. The engine has an independent oracle half
  and differential scenarios. Screen `consolidation`.
- **Intercompany** (FR-GL-13): an entity posts its side as an ordinary journal and records it as an intercompany transaction
  with the counterparty; the counterparty sees the offer, and posts its mirror as a draft in its own books.
- **Accountant-firm mode** (FR-ENT-03, FR-ENT-07): a firm is an organisation in firm mode; a client's Owner accepts the firm's
  invitation; the practice dashboard shows, for each client where the person already holds a role, a few counts without money.
  Screen `practice-dashboard`.
- **The switcher and "all entities"** (FR-UX-18): the organisation switcher groups a group's entities and links to the
  consolidated view.

## Capabilities

### Modified Capabilities
- `organisation/multi-entity` (FR-ENT-01 to 07), `ledger/general-ledger` (FR-GL-13), `reporting/reports` (FR-RPT-12),
  `experience/navigation` (FR-UX-18), `ledger/money-and-rounding` (RND-TRANSLATE), `access/roles-and-segregation` (two Appendix D rows)

## Impact

New tables in 001350 to 001449 (the invitation registry is global; every other table is a tenant table with row-level security).
Three new security-definer functions form the only cross-tenant surface, all read-only except the acceptance that creates both
sides' rows once: each has a narrow, fixed column list, `EXECUTE` for the API role only and an attack test. A new rounding policy
and a new calculation with an oracle half. New operations with authorisation-matrix rows. No tool, scope or grant for the AI or MCP
surface: a group's figures are never read by AI read tools. Outside needs: none to build; an accountant's confirmation of the
translation method and of the scope (wholly owned only) is `[accountant to confirm]`.
