# Design

## Context

Bills (`bill@1`) and categorised bank lines (`bank_transaction@1`) already debit the asset accounts 1510 to 1540, and
opening balances put assets bought before the books start on them with their accumulated depreciation on 1590. The
scheduler's platform jobs post recognised amounts as real entries up to each organisation's today (ENG-05,
FR-AUT-01), as `bills.releases` does for spreads: a principal of its own, an entry per document, line and month, a
month with no open period waiting with an audit event. `ALLOC-EVEN` (internal/domain/rounding) splits an amount over
months exactly. The close reconciles sub-ledgers with their control accounts (INV-07) for payables, receivables and
deposits, from the documents, through the balance service (ENG-04). The capitalisation threshold is a setting
(`assets.capitalisation_threshold`, S$1,000). See proposal.md for why.

## Goals / Non-Goals

**Goals:**
- An asset register whose figures come from documents and replay exactly (ENG-06), and reconcile with the ledger.
- Depreciation that posts by itself, month by month, and never into a closed period or the future.
- Disposals, partial ones included, that post their gain or loss in one entry.

**Non-Goals:**
- Reducing balance, units of production, revaluation or impairment.
- Asset categories with their own default lives (the asset account is the category in P1).
- Depreciation by day or by half-year conventions: a whole month, from the start month.

## Decisions

### D1. An asset is a document

A registered asset is a `fixed_asset` document (numbered FA-), its content versioned: a change of its life or residual,
or a partial disposal, is its next version. Registering it posts nothing: its cost is already on the asset account,
put there by its source. Its source is one posted bill line or bank-transaction part that debits an asset account, by
document and line (one asset per source line, which the database guards), and its cost is that line's base amount as
its entry posted it; or, for an asset bought before the books start, no source and the cost and accumulated
depreciation the opening balances carry, as at the books start. **Alternatives:** a register table outside the
documents, as claim categories are: rejected, because depreciation entries need a document as their source for the
ledger guards and the replay, and documents give versions and history.

The category is the asset account it sits on (1510 Computer equipment, 1520 Office equipment & furniture, 1530
Renovation & fit-out, 1540 Software & intangible assets): the accounts whose report group is "Property, plant &
equipment" or "Intangible assets" [verify with the accountant's chart review].

### D2. Each version carries what came before it

A version holds its cost, residual value, useful life in months, method (straight-line), depreciation start, and
`carried`: the accumulated depreciation up to its `from` month. The first version of a new asset carries nothing from
its start month; the first version of an asset bought before the books start carries the opening accumulated
depreciation from the books start's month, so months before the books start are never posted (FR-FA-02). A later
version carries what was posted until it, from the first month not posted. The schedule of a version is a pure
function of it: `ALLOC-EVEN`(cost − residual − carried, over the months from `from` to the end of its life), the end
being start + life − 1, or the month before a disposal of the whole. So a change of estimate applies to the months
not yet posted, and a replay needs only the version. **Alternatives:** recomputing the whole life from version 1 and
subtracting what was posted: rejected, because the replay of a month would then depend on later postings.

### D3. Depreciation posts per asset and month, at the month's end

`asset_depreciation@1` posts one entry per asset and month, dated the month's last day, Dr 6610 (the role
`depreciation`) · Cr 1590 (`accumulated-depreciation`), in the base currency; its identity is the asset, its version
and the month, and the database refuses an entry that does not name a posted asset at that version (as FY172 does
for releases). The job `assets.depreciation` (hourly, as `scheduler:assets.depreciation`) posts every month whose last
day has come in the organisation's time zone; registering or changing an asset posts its months already due at once.
A month whose period is closed waits, with the audit event `fixed_asset.depreciation_waiting`, and the next run tries
again; future months show as projections on the asset, never in the ledger (ENG-05). **Alternatives:** one entry a
month for every asset: rejected, because a disposal or a change of one asset would split it, and its replay would
depend on every asset.

### D4. A disposal is a document of its own

An `asset_disposal` document names the asset, its date, the share of the cost it disposes of (all, or an amount), and
its proceeds: money received (a money account in the base currency, the amount with its GST code, for output GST
when the organisation is GST-registered: `RND-GST-INCL`), a sales invoice's line (an issued invoice whose line credits
`assets.disposal_clearing_account`; the invoice charges its own GST), or none (a write-off). It posts
`asset_disposal@1` in the same transaction:

- Dr 1590 with the share's accumulated depreciation through the month before the disposal;
- Dr the money account, or the clearing account, with the proceeds net of GST; Cr 2200 with the output GST;
- Cr the asset account with the share of the cost;
- the difference to `assets.disposal_account` (Dr a loss, Cr a gain).

Every month before the disposal's must be posted first (a disposal is refused while one waits), so the accumulated
depreciation is the ledger's. A partial disposal makes the asset's next version in the same transaction: the cost,
the residual and the carried depreciation reduced by the share, the rest depreciating from the disposal's month. The
disposal of a money sale is matched by bank import as money recorded by hand (`bank_match_posting`, FY228). A
disposal is voided by a reversal on its date or later, while no later month of the asset is posted; in Founder mode
on a second person's approval (`asset_disposal.void`, FM-1), which restores the version before it as the next one.

### D5. Who acts and what is checked

Appendix D's "Fixed assets" row: the Accountant registers, changes and disposes (C/P); the Controller approves a void
(A); the Owner, the Bookkeeper, the Auditor and the Tax agent view (V). Every command is audited in its transaction
(AUD-01): `document.created` and `document.revised` for an asset, the disposal's posting and void,
`fixed_asset.depreciation_waiting`. A bill or a categorised bank line whose line on an asset account is at or above
the capitalisation threshold and is not registered shows ⚠ `ASSET-UNREGISTERED` with the way to register it, and the
register lists such lines under **Not in the register yet**.

### D6. The register against the ledger (INV-07)

As at a day, each asset account's balance from the balance service equals the cost of the assets on it not disposed
of by then, and 1590's balance equals their carried and posted depreciation less what disposals took. The close shows
the difference as a reconciliation like the AP one: ✖ for one no source explains, ⚠ naming the opening balances and
manual journals on those accounts. The C.1 #22 check line follows with `p1-checks`.

### D7. Settings, ports and adapters

The settings `assets.disposal_account` (account code, recommended 7050, dual control) and
`assets.disposal_clearing_account` (account code, unset until the organisation names it, dual control) join the
catalogue, with their guide sections. No new port (ADR-T8): the documents, ledger, accounts, allocations and
banking ports carry it; the migration adds the document types, the asset's source guard, the depreciation guard, the
approval object and the bank-match kind. The job is a platform job of the tick (ADR-T7).

## Risks / Trade-offs

- [The opening accumulated depreciation typed for an old asset differs from what the opening balances put on 1590] →
  the register reconciliation (D6) shows the difference at the close; the opening balances are corrected, not the
  register.
- [A period is closed before its depreciation posted] → the month waits with an audit event and the close's
  reconciliation shows 1590 below the register; reopening the period lets it post.
- [Many small entries: one per asset and month] → a start-up has tens of assets; the entries carry the asset's
  number, and the GL groups by source.
- [The asset accounts are recognised by report group, which a person can change] → an asset needs its account to be
  in one of the two groups when registered; a later change shows in the reconciliation.

## Migration Plan

Additive: new document types, guards and settings; nothing existing changes. Organisations with assets already on
1510 to 1540 register them from their bills, or as opening assets. Rollback: the feature's screens and job are
removed; its documents and entries stay, as all posted history does.

## Open Questions

- Recommended useful lives per asset account (the accountant), to prefill the form; until then the person enters the
  life.
