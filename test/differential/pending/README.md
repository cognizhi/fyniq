# Pending golden scenarios (QA-ENG-02)

Golden cases for the P1 sources, in the `fyniq-scenario@1` format, waiting for the engine's
`cmd/scenario-run` to read their shapes. Until it does, they live here and not in `golden/`: the Go
gate (`make test`) posts every file in `golden/` through both sides, and the engine would refuse
these as invalid input.

As in `golden/`, only inputs live here. Each figure is worked out by hand in
`oracle/tests/test_pending_golden.py`, which the oracle must match today (`make oracle-test`). When
an engine session teaches `cmd/scenario-run` a source's shapes, it moves that source's file to
`golden/`, adds the source to `FYNIQ_DIFFERENTIAL_SOURCES` in the Makefile's `differential` target,
and settles any disagreement from the PRD (README, "P1 readings"); an engine bug is a failing golden
case, and the oracle never changes to match the engine.

| File | Source | What |
|---|---|---|
| `bank.json` | Bank lines and transfers (`p1-bank-import-reconcile` 4.6) | Input GST out and output GST in under `RND-GST-INCL`; a spread over 3 months with one release still to come at `as_of`; a USD line split in two whose `RND-FX` cent goes to 7030; GST on a USD line from its base amount; a same-currency transfer; a Wise SGD-to-USD conversion with a fee; a part to suspense; a part on a money account and a spread on an asset, both refused |
| `purchases.json` | Bills, vendor credits and payments of bills (`p1-purchases` 10.1) | G4-023's USD bill claiming the SGD GST stated; an SGD bill; a bill spread over 12 months from before its date, three months released on its date; withholding under `RND-WHT`; a vendor credit; G4-023's bill paid from the USD wallet at 1.30 (rev 0.77's figures); a part recorded in full within tolerance; a part with an unapplied amount; a spread on an asset and a payment above what is open, both refused |
| `sales.json` | Invoices, credit notes, receipts, deposits, refunds and write-offs (`p1-sales` 9.1) | A part-payment deposit bearing GST; G4-038's USD invoice, its receipt at 1.32 and its residual written off; GST per subtotal; ENG-42's USD invoice with its 7030 cent; G4-022's spread with its GST in the invoice's month; a zero-rated invoice deducting part of the deposit (`RND-DEPOSIT`); two credit notes linked to one invoice that credit its total together, and a third of 0.01 beyond it refused (rev 0.155); a receipt in full within tolerance; a deposit refund; a receipt above what is open and a spread on an expense, both refused |
| `claims.json` | Expense claims, reimbursements and cash advances (`p1-expense-claims` 9.1) | An advance used up by two claims (one with a receipt and a trip, one owing the rest on 2150); a USD claim reimbursed in SGD at 1.33 with the difference to 7030; trips by motorcycle and car under `RND-MILEAGE`; one reimbursement of two claims; an advance refunded whole; a reimbursement of a paid claim and a trip in USD, both refused |
| `assets.json` | Fixed asset depreciation and disposals (`p1-fixed-assets` 8.1) | Months on their last days under `ALLOC-EVEN` (333.33, 333.34, 333.33 from a later start); an asset bought before the books' start, posting from it; a residual value; a sale with GST on the proceeds received and a loss; a sale invoiced through the disposal clearing account; each disposal ending its asset's months; a second disposal and an asset on an expense account, both refused |
