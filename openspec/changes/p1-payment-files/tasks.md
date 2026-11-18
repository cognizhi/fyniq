# Tasks

## 1. The money account's payment file (FR-BNK-12, rev 0.157)

- [x] 1.1 Migration: `money_account.payment_file` (default `payment-list`, CHECK on the built formats); the
  institution for a bank account picked from a list (design D2); squawk clean; goldens updated.
  - Built: migrations 000430 and 000431 (`money_account.payment_file`, `payment-list` by default, its check validated
    apart, granted to `fyniq_api`); the schema and grants goldens. Decided here: the check names the three files of
    rev 0.157, and the API takes one only once its writer is built (`paymentfile.Built`) and release refuses any other,
    so a writer arrives without a migration (design D1). The institution stays text, and `banking.Institutions` is the
    list the screens pick from; an account's bank is its statement connector's provider, else its institution
    recognised by name or another name it goes by (POSB, TransferWise), in any case and without "Bank", "Ltd" or
    "Singapore", so the free text of existing accounts maps as a picked name does, with no data migration (design D2).
- [x] 1.2 The API: read and change the setting on the money account; refuse a format the provider doesn't take;
  audit the change; release picks its writer from the account. `make openapi` first.
  - Tests: the default is the payment list; a Wise format on a DBS account refused; a batch released before a change
    keeps its file; the change audited. `// req: FR-BNK-12`
  - Built: `payment_file` on createMoneyAccount and replaceMoneyAccount (left out on a change, it stays); the money
    account's `provider` and `payment_files` (each file, whether it is offered and why not); PAYFILE-01 (not this bank's
    file) and PAYFILE-02 (not built yet), naming `body.payment_file`; a change of bank that would leave a file the bank
    cannot read is refused; the change audited with the old and new value (`money_account.changed`); release reads the
    account's file as it writes it and refuses, releasing nothing, a file the bank no longer takes or Fyniq no longer
    writes. `TestAMoneyAccountSaysWhichFileItsBatchesGive` runs it with a stand-in writer for Wise's file, since no
    bank's file is built yet; `TestEachBankTakesItsOwnFile`, `TestAnAccountsBankIsRecognisedByName`.
- [ ] 1.3 The screens `add-bank-account` and `bank-accounts`: the money account's Payment file setting, and the batch page's download label naming the
  file. Help keys; the guide pages `banking.accounts` and `purchases.payment-batches`, compiled into `kb/`.
  Design the screens first (fyniq-openspec Recipe C).

## 2. Wise's batch payment file (waits for the owner's blank Wise template)

- [ ] 2.1 `wise-batch@1` from the blank template (design D3); formula-safe cells; the reference cut to Wise's
  limit.
  - Tests: columns pinned to the template; one payment and many; a name with a comma or a formula. `// req: FR-BNK-12`
- [ ] 2.2 `[live-verify]` One real upload to Wise, from a batch in the check stack, with the owner.

## 3. DBS IDEAL's UFF (waits for "DBS UFF Version 2.2", once IDEAL is set up)

- [ ] 3.1 `dbs-uff@2.2` for FAST and GIRO from the specification (design D4).
  - Tests: each record pinned to the specification's example. `// req: FR-BNK-12`
- [ ] 3.2 `[live-verify]` One test upload to IDEAL with the owner.

## 4. Close

- [ ] 4.1 The threat model gains the new formats; the PR lists what was not verified; archive.
