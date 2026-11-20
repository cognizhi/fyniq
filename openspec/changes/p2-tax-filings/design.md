# Design

## D1. The tax-adjusted profit working paper

Sources: IRAS, "Corporate Income Tax Rate, Rebates & Tax Exemption Schemes" (the `sg-cit@2026.1` sources, retrieved 8 Nov 2026). The paper
starts from the financial year's profit before tax from the ledger (the trial balance at the year end), adds back adjustments the
tax agent enters (each: kind `non_deductible` or `capital_allowance_difference` or `other`, an amount, a reason, a document), and
ends at the chargeable income. The exemptions and the rebate are computed from `sg-cit`: the start-up exemption only when the
organisation marks itself qualifying for that YA (a setting by YA with the tax agent's confirmation), else the partial exemption.
Exact decimals; the rounding is the tax agent's, so the paper shows each step unrounded to the cent and rounds the final tax payable
half away from zero to the cent [accountant to confirm]. No account carries an automatic tax tag (the chart has none): adding tags is a
later change.

## D2. The form selector

Form C-S Lite when revenue is at most S$200,000, C-S at most S$5 million, otherwise C (PRD §14.2, IRAS, Aug to Sep 2026). The thresholds
are limits in `sg-cit@2026.2` with their source, quoted, `[verify]` until the page is captured again. The deadlines come from `sg-deadlines`.

## D3. Bad-debt relief

A write-off of an invoice (FR-AR-17) whose GST code is standard-rated joins the queue. Eligible from the later of: 12 months after the
supply date (the invoice's tax point), the date of the write-off. For an insolvent debtor the 12 months do not apply (the claim is
recorded with the insolvency evidence). The claim expires five years after the supply. A claim is recorded with its amount (the GST
included in the written-off part, exact), the F5 period it belongs to and evidence; the posting is Dr the GST input control, Cr the
bad-debt expense account for the GST amount, on the claim's date, through the engine with the oracle's half and a differential
scenario. Not GST-registered: the queue is not shown.

## D4. Pack updates

A published version newer than the organisation's active one yields a task `pack.update_available` with a diff computed by comparing
the two versions' tables row by row (added, removed, changed). Accepting activates it (the P1 mechanism). A published correction to a past
period creates a task `pack.correction_assessment` listing the posted documents whose period the correction covers; it never re-posts.

## D5. The IRAS filing port

`ports.IRASFiling.Submit(kind, period, payload, idempotencyKey)` returns the acknowledgement reference, with the local stub accepting
a well-formed payload and returning a deterministic reference. IRAS lists these APIs on its developer portal (retrieved 20 Nov 2026):
Submission of GST Return (F5), File GST Return (F5 and F8), Edit Past GST Return (F7), Submission of GST Transaction Listings and
Submission of Corporate Tax Records (Form C-S and pre-fill), all CorpPass-authorised. Their payload structure is behind IRAS's
sign-in: **not guessed**; the mapping is `[live-verify]`.

### D3 as built (rev 0.607, 21 Nov 2026; options and what was chosen, `[owner to confirm]`, `[accountant to confirm]`)

- **The GST share.** The posting leg is the output tax the invoice posted to the GST control account (read from its entry, net of what deposits bore) times the amount written off over the amount the invoice made the customer owe, `RND-BDR`, half away from zero, in the base currency; a foreign invoice's share is therefore computed on its base output tax, never on a foreign-currency GST. Options: (a) the invoice's GST in its own currency converted at its rate (two roundings, rejected); (b) the share of the base output tax (chosen: one rounding, additive); (c) the claimer types the amount (rejected: the differential would prove nothing).
- **Several write-offs of one invoice** are claimed one by one; each is capped at what earlier claims left and the write-off that completes the debt takes all that is left, so the claims add up to the output tax exactly.
- **Approval.** The claim posts at once for a person with the right to the GST return (create); the write-off it rests on was approved (SOD-9), and a void is a reversal. Option: a second person approves each claim (tighter, slower; the approval engine's object types would grow). Recommended for the first production deploy: keep one step and review the claims in the monthly checks.
- **Credit notes.** An invoice a credit note credits is refused, because the note adjusted its output tax and the share would be wrong; the accountant claims by hand until a later change nets the notes.
- **Left:** an insolvent debtor's date (the 12 months wait is then skipped), evidence files on the claim, subsequent recovery when the debt is repaid, and invoices with credit notes.
