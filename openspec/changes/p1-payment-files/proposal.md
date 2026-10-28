# Proposal

## Why

`p1-payments-out` releases a payment batch as a **payment list**: a sealed CSV that someone keys into the bank
(FR-BNK-12, rev 0.156), because no bank had been chosen. Keying every payment again is slow, and a wrong amount or
reference shows only when the statement is matched. On 28 Oct 2026 the owner decided not to choose one bank but to
build each bank's own file, and let each money account say which file it pays with (PRD-001 rev 0.157).

## What Changes

- **The owner's decisions of 28 Oct 2026, taken interactively (rev 0.157):**
  - **every bank's own file**, chosen by a **setting on the money account** a batch pays from:
    - the **payment list**, the default, for any bank;
    - **Wise's batch payment file** (CSV, Wise's template for paying to bank accounts);
    - **DBS IDEAL's UFF** ("DBS UFF Version 2.2");
  - each file is built **when its specification is in hand**, and offered only for an account at that provider.
    Changing the setting is audited and changes no file already released;
  - **Airwallex takes no file:** its new batches must be the .xlsx downloaded from the account, with hidden
    validation data and fixed headers. Airwallex accounts pay batches through Airwallex's payout API when API
    initiation is built (P2), and give the payment list until then.
- **What a batch pays is unchanged** (rev 0.154): SGD from an SGD money account to Singapore bank accounts, with
  dual-controlled approval, release after step-up, posting on release and matching on import.
- **What waits for the owner:**
  - Wise's help centre names only the common columns (Name, sourceCurrency, targetCurrency, Amount, amountCurrency,
    Payment reference). The SGD bank columns come from a **blank template** that the owner downloads from Wise's
    template builder (SGD, to bank accounts);
  - DBS's specification sits behind the IDEAL login. The owner sets IDEAL up on the DBS account and shares "DBS UFF
    Version 2.2".

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None: FR-BNK-12's rev 0.157 scenarios are already in the main spec `banking/bank-transactions`, converted with the
PRD revision. This change builds them (`skip_specs: true`).

## Impact

- `db/migrations/`: the money account's payment file setting.
- `internal/domain/paymentfile/`: a writer per format beside `payment-list@1`. Release picks the writer from the
  batch's money account and records its name, as it does today.
- `internal/api/`: the setting on the money account, and its audit event. The release's `writer` gains the new
  names.
- `apps/web/`: the money account's Payment file setting, and the batch page's download label.
- `docs/user-guide/` and `kb/`: how to set the file and upload it to the bank.
- Screens: `add-bank-account`, `bank-accounts` (the setting) and `payment-batch` (the download label).
- Not here: Airwallex's payout API, and any payment API (FR-BNK-12, P2).
