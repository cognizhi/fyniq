# Page override · Inbox and proposal review

Overrides MASTER.md for Inbox, approvals and AI proposals (PRD §7.2 FR-UX-03, §6.5, §9.5).

## Layout
- **Two panes**: the list (left, 40%) and the detail (right, 60%). Below 1024 px the detail opens
  as a full page with a back link that restores the list's scroll position.
- List rows: type icon, title, counterparty, amount (tabular, right-aligned), status chip, age.
  AI proposals carry the AI badge and a 3 px `--ai-border` left rule.
- Group headers by type ("Approvals 4", "AI proposals 3", "Cannot post 2", "To review 6",
  "Deadlines 1"), each collapsible.

## Keyboard (Expert mode; can be turned off or remapped)
J / K move · E open · A **opens** the approve confirmation · R reject (requires a reason) ·
? shortcut sheet. Focus never lands on Approve by default.

## Proposal review (detail pane)
Order, top to bottom:
1. Header: AI badge, task type name, risk tier chip, "proposed by AI for <requester> · 2 h ago",
   expiry.
2. **Plain summary** (one or two sentences, generated from the payload, not free model text).
3. **Items** (batch proposals): checkboxes per item, all selected by default only for tier 1;
   the selection re-hashes the payload (AI-07).
4. **Ledger impact**: debit / credit / account table with balanced totals (Expert), or the plain
   summary sentence (Basic), plus "balances after" for affected accounts.
5. **Diff** for changed master data (before → after, per field).
6. **Evidence**: links to the records and documents the AI relied on; the rationale in
   `--ai-subtle`.
7. **What could go wrong** (tier 2–3).
8. Actions bar (sticky bottom, never covers focus): Reject (secondary, left) · Approve
   (primary, right). Approve needs step-up (a passkey prompt) for tier 2–3, and the button names
   the action ("Approve 12 categorisations").

## Empty state
"Nothing waiting for you." Show the last 3 decisions with an "undo" link where AI-31 allows it.
