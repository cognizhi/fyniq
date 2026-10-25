# Tasks

Each slice is one pull request, merged when green, and passes every gate at its own commit (`make openapi` clean,
`make images` when it adds a top-level package). Tests carry `// req:` tags (QA-TRACE-01); every new ✖ or ⚠
message is triggered by a fixture (QA-RPT-02). Money is exact decimals everywhere (ENG-10). The threat model comes
before any code that parses bank files or touches connector secrets (SEC-20).

## 1. Before code

- [x] 1.1 The threat model `docs/security/threat-models/banking.md`:
  - assets: statements and bank lines, the postings they make, bank connector keys, the bank-ingest mail;
  - abuse cases: a forged or replayed bank report, a malformed or oversized file (zip bomb, deep XML, a huge
    CSV), a forged or replayed webhook, a real line dropped as a duplicate, auto-reconcile driven to the wrong
    document or beyond its caps, a reconciliation signed by its preparer, a cross-tenant read, a leaked bank key;
  - each mapped to its control and the test that proves it; the connector threat model's T12 and its open item
    rewritten for the secrets project (design D12).
  - Verified by review in the pull request, before the first code slice merges. Refs `// req: SEC-20`, FR-BNK-15,
    FR-INT-22, SEC-36.
  (25 Oct 2026: T1 to T17, each mapped to its control and the task whose tests prove it; the connector threat
  model's T12 and open item now name the secrets project)

## 2. The store

- [x] 2.1 Money accounts, statements and lines (design D1, D2):
  - migrations for `money_account` (one per chart money account), `money_account_source` (effective-dated),
    `statement_mapping`, `bank_statement`, `bank_line` and `bank_gap`, each with `organisation_id`,
    `FORCE ROW LEVEL SECURITY`, a guard trigger (facts immutable, status forward only) with its `FY` codes, grants
    and audit in the same transaction; a validating migration for every `NOT VALID` constraint;
  - the storage port and the PostgreSQL adapter, sqlc regenerated, the schema and grants goldens updated.
  - Tests: lifecycle and attack tests (change a line's amount, rewind a status, read another organisation's lines,
    delete a statement) in `internal/adapters/postgres`. `// req: FR-BNK-01`, `// req: FR-BNK-04`, `// req: INV-06`,
    `// req: QA-ENG-05`
  (25 Oct 2026: migration 000144, `FY216`–`FY221`; `storage.Banking` and its adapter; no `NOT VALID` constraint, so no
  validating migration. A statement's import event stands for its lines, which arrive only in its transaction; each
  later status change is audited. Proved by `TestBankingStoreKeepsWhatTheBankSaid` and the NaN attacks)
- [x] 2.2 Rules, matches, transfers, signals, auto-reconciliations and reconciliations (design D5 to D10):
  - migrations for `bank_rule`, `bank_match`, `bank_transfer`, `bank_signal`, `auto_reconciliation` and
    `bank_reconciliation`, with the same tenancy, guards, grants and audit; a signed-off reconciliation is
    immutable and locked with its period.
  - Tests: attack tests for each table (re-sign a reconciliation, undo an auto-reconciliation in a closed period,
    match one line twice). `// req: FR-BNK-10`, `// req: FR-BNK-14`, `// req: QA-ENG-05`
  (25 Oct 2026: migration 000145, `FY222`–`FY226`; `storage.BankMatching` and its adapter. An auto-reconciliation is a
  `bank_match` whose basis is the system's (exact, transfer or rule) rather than a table of its own, so a line's one
  live match covers both and the Auto-reconciled list reads the matches. The `bank_rule` approval object joins the
  engine with task 5.2. Proved by `TestBankMatchingKeepsWhatItRecorded` and the NaN attacks)

## 3. Importing statements

- [x] 3.1 `internal/bankfile` (design D3): CSV and XLSX through a saved mapping (signed amount, or in and out
  columns), OFX/QFX, camt.053 and MT940 (SWIFT and SAP), into the one bank data model, with exact decimals, a
  parser version per format and limits on size, lines and fields; the XLSX reader on `archive/zip` and
  `encoding/xml` with the evidence upload's zip limits.
  - Tests: golden fixtures per format (synthetic statements), a Go fuzz test per parser, refusals naming the line
    and field. `// req: FR-BNK-02`, `// req: FR-BNK-15`
  (25 Oct 2026: one model of statements and lines; `BANKFILE-01`–`06`; the package joins the architecture test's money
  packages, so no binary floating point: an XLSX number is its stored decimal text, read exactly, and an inexact one is
  refused with a fix. Fuzzing found two panics in the OFX tokeniser (an empty tag; `strings.ToUpper` changing the
  length of invalid UTF-8), fixed and kept as regression seeds. DBS's MT940 SAP layout stays `[verify]` against a
  sample. Proved by `TestEachFormatReadsIntoOneModel`, `TestRefusalsNameTheLineAndField`, `TestMappingsReadFromTheirJSON`
  and `FuzzMT940`, `FuzzCAMT053`, `FuzzOFX`, `FuzzCSV`, `FuzzXLSX`)
- [x] 3.2 The import (design D2): statements for several accounts at once, each file an untrusted upload checked and
  scanned and kept with its statement; a preview of what will be added, flagged as possible duplicate and dropped on
  proof; the continuity check with gaps recorded; the account's last sync and complete-to date.
  - Tests: several accounts in one import; a gap flagged, never filled; G4-042 (two S$500.00 receipts kept, a
    re-import and an overlapping source add nothing, a look-alike without proof marked possible duplicate); a
    transfer already recorded from the other account recognised. `// req: FR-BNK-02`, `// req: FR-BNK-04`,
    `// req: FR-BNK-16`
  (25 Oct 2026: `banking.Import` in the caller's unit of work, so the API's preview rolls it back; statements matched
  to their money accounts by identifier and currency (`BANK-01`–`05`); running balances derived from the opening or
  closing balance where a format gives none; a line dropped only when the bank's reference or the running balance
  proves it held, otherwise flagged; a transfer's other side named for auto-reconcile (task 5.1) to complete;
  `FreshnessOf` for the last import and the complete-to date. The upload itself, checked and scanned and kept as the
  statement's file, comes with its API operation (task 9.1). Proved by `TestStatementsForSeveralAccountsGoToTheirOwn`,
  `TestABreakInTheBalanceChainIsAGap`, `TestDuplicatesAreDroppedOnlyOnProof`, `TestATransfersOtherSideIsRecognised`)

## 4. Posting, rules, matching, transfers and suspense

- [x] 4.1 The bank posting rule set (design D4): money in and out with the GST extracted by `RND-GST-INCL` when
  registered at the line's date and the code carries GST; a spread to 1210 or 2400 released by `ALLOC-EVEN` from the
  posting month; posted through the engine in the unit of work of the line's status (ENG-02), with replay. As built:
  the source is a `bank_transaction` document (D4, rev 0.141), its spread released by the job `bank.releases`.
  - Tests: the legs of Appendix B.1 for money in, money out and a spread, hand-worked; replay byte for byte.
    `// req: ledger/posting-rules Posting a bank transaction, money in`, `… money out`, `… Spreading a bank
    transaction over N months`, `… Amounts of a bank, card or cash transaction`
- [x] 4.2 Bank rules (FR-BNK-05): conditions (description contains or regex with a time limit, amount range,
  account, counterparty) set the category, contact, GST code, dimensions and reference, in their order; a back-test
  against history reports how many lines a rule would have matched.
  - Tests: ordering, each condition, the back-test count, a regex that cannot run away. `// req: FR-BNK-05`
  - As built: the regex runs on RE2 (linear in the text) with a 200-character pattern; the amount range is of the
    line's size with an optional direction and currency. Dimensions in the action wait for FR-DIM (task 4.7), as on
    bill and invoice lines.
- [x] 4.3 Matching (design D5): suggestions by reference (exact, then normalised) and by amount and contact;
  accepting creates the receipt or bill payment through their commands with the line as source; a receipt or payment
  recorded by hand is matched, never duplicated; one to many, many to one, partial and over payments; realised FX and
  the settlement tolerances as built.
  - Tests: each matching order; a hand-recorded receipt matched without a second posting; G4-038 (USD invoice paid
    short stays part-paid; the SGD bill's S$120.00 stays open). `// req: FR-BNK-07`, `// req: FR-BNK-08`,
    `// req: FR-AR-18`
- [x] 4.4 Transfers (design D6): one record, matched on both accounts, in transit with one side; across currencies
  each leg at its day's rate, a reported fee to the bank charges setting (default 6460), the rest to 7030, no rate
  refused (FX-01).
  - Tests: same-currency transfer recorded once; in transit; a Wise SGD-to-USD conversion with a fee, hand-worked.
    `// req: FR-BNK-09`, `// req: ledger/posting-rules Transfer between money accounts`
- [x] 4.5 Suspense (design D7): an unmatched line posts to 2999 with ⚠; classifying it reverses and reposts.
  - Tests: post to suspense, classify, the 2999 balance back to zero, history intact. `// req: FR-BNK-11`
  - As built: `BankTransactions.Suspend` posts the whole line to the account with the role suspense; the close's ⚠
    while 2999 is not at nothing is task 6.2.
- [ ] 4.6 The independent oracle: the bank line, spread and transfer rules (with D6's cross-currency rule once the
  accountant confirms it) from Appendix B.1, with scenario shapes and golden cases, in a session of its own (never
  from a build session).
  - Verified by `make oracle-test` and `make differential` agreeing to the cent. `// req: QA-ENG-01`,
    `// req: QA-ENG-02`

- [ ] 4.7 Dimensions on a bank transaction's parts and in a rule's action, once FR-DIM is built
  (`p1-organisation-chart` task 3.3), as on bill and invoice lines.
  - Tests: a rule's dimensions carried to the posting; a value required by its account refused when missing.
    `// req: FR-BNK-05`, `// req: FR-DIM-02`

## 5. Auto-reconcile

- [x] 5.1 The auto-reconcile job (design D8): after each import or sync, as the system principal, a line matching
  exactly one open document, a transfer matched on both accounts, a line hit by an approved auto-apply rule; within
  the per-line and per-rule-per-day caps (organisation settings with recommended defaults); each recorded with what
  did it; undo while the period is open; a sample in the weekly review.
  - Tests: each kind of match; an ambiguous line left in the queue; a cap reached; undo, and undo refused once the
    period is closed. `// req: FR-BNK-14`, `// req: FR-AUT-02`
- [x] 5.2 Auto-apply as an approval (design D8): the approval object `bank_rule` with operation
  `bank_rule.auto_apply`, showing the back-test and the rule's hits since it was created; nothing auto-reconciled
  by a rule before approval; repeated coding (five times by default) offered as a rule, never applied by itself; the
  rule's AI-proposal flag kept for G4-059.
  - Tests: approval through the engine (FM-1); a rule's lines wait until approval; the repeated-coding offer.
    `// req: FR-BNK-14`, `// req: FR-APR-01`
- [x] 5.3 Reconcile now (the owner's decision of 26 Oct 2026, rev 0.143): in P1, lines that arrive by feed wait in
  the queue, because the worker that imports them has no posting right; a person's Reconcile now runs auto-reconcile
  over the waiting lines (one account's or all) as the system principal, for the person who asked, within the same
  caps; a preview says what it would do. Feeds reconcile by themselves in P2, under a role of their own (§17.5).
  - Tests: a preview that posts nothing; a transfer's arriving side reconciled; a second run finds nothing; the
    permission row. `// req: FR-BNK-14`, `// req: banking/bank-transactions Lines that arrive by feed wait for
    Reconcile now`

## 6. Reconciliation, the close and cash

- [x] 6.1 Reconciliation (design D9): per account and period, the statement balance, the reconciled book balance
  from the balance service, the unreconciled items, the difference and the ledger revision seen; prepared by one
  person, signed off by another with step-up; locked with the period.
  - Tests: sign-off by the preparer refused (FM-5); a difference shown; locked after the close.
    `// req: FR-BNK-10`, `// req: FR-GL-07`
- [x] 6.2 The close's bank checks (design D9): ✖ a money account with activity and no signed-off reconciliation at
  the revision seen; ⚠ suspense not zero, a money account below zero at month end, a reconciliation not matching,
  stale data, unreconciled lines older than 30 days, unresolved possible duplicates.
  - Tests: one fixture per message. `// req: FR-GL-07`, `// req: FR-BNK-11`, `// req: FR-BNK-16`, Appendix C.1 #30
    to #32 and C.2 #8, #9, #43, #51
- [x] 6.3 The daily cash position (FR-BNK-13): every money account and currency at the latest rates, from the balance
  service, with its date and the rate source of each currency.
  - Tests: a multi-currency position against hand-worked totals. `// req: FR-BNK-13`

## 7. DBS by email

- [x] 7.1 The bank-ingest address (design D10): a third capture address kind, one per organisation, named by an
  Administrator; its known senders only the bank addresses each money account registers, with DMARC passing; any
  other sender quarantined.
  - Tests: a report from the registered sender accepted; one from another sender quarantined; a forged sender
    without DMARC quarantined. `// req: FR-INT-22`, `// req: banking/bank-transactions Daily DBS sync without a
    bank API`
- [x] 7.2 The daily report and the tie-out (design D10): an accepted report imported through its account's format
  and mapping, with the continuity and duplicate checks, its lines waiting for Reconcile now (rev 0.143, task 5.3);
  the monthly MT940 export or e-statement
  ties out the closing balance. Before the parser is pinned, confirm the format with a redacted sample of Cognizhi's
  DBS IDEAL report (`[verify]` until then).
  - Tests: a synthetic report through Mailpit end to end on the local stack; a missing day shown as a gap.
    `// req: FR-BNK-17`, `// req: banking/bank-transactions Daily DBS sync without a bank API`
  - As built: `internal/bankingest` (design D10's note); the end-to-end test drives Mailpit's webhook, the fetch and the
    worker's import in Go; a run on the local stack with Mailpit itself is not automated. The DBS IDEAL format stays
    `[verify]`: the import reads whichever of CSV, XLSX or MT940 the account's source names.
- [x] 7.3 Incoming-funds alerts (design D10): a pending signal matched to an open invoice by amount and customer;
  "likely paid" on the invoice and on Home; nothing posted; the signal closed by the statement line or expired. The
  signals' API operations (listed, and dismissed by a person) come with them, as task 9.1 left them.
  - Tests: UC-BNK-02's S$10,900 from "ACME ROBOTICS" to INV-0138, then the statement line reconciling it; the
    invoice's status and the ledger unchanged by the alert. `// req: FR-BNK-16`,
    `// req: banking/bank-transactions Incoming-funds alert speeds up matching`
  - As built (PRD rev 0.145, the owner's decision of 26 Oct 2026): a message at the bank-ingest address with no
    attachment is read as an alert (`banking.ParseAlert`; the DBS IDEAL wording stays `[verify]`); its likely invoice is
    the one open invoice of its amount and currency whose customer the payer names; the match that settles the invoice
    closes the signal in its transaction (`journal.MatchInvoices`, `journal.MatchRecorded`); the daily job
    `bank.signals-expiry` expires the rest after `bank.signal_expiry_days` (7, under dual control); `listBankSignals`,
    `dismissBankSignal` (status `dismissed`, migrations 000158/000159) and `likely_paid` on the invoice. Home's list
    reads `listBankSignals?status=pending`; Home's inventory row had the hint in the violet AI card and now lists it on its
    own, neutral, since a signal is not AI content (golden rule 11). At S$10,900 the statement line is over the
    auto-reconcile line limit (S$500), so a person matches it in the test, as the default limits require.

## 8. Bank connectors

- [x] 8.1 The secrets project (design D12; the owner's decision): OpenTofu creates a secrets project per platform
  environment; the API's account manages secrets there only, the API and worker read there; `connector.Organisations`
  uses it (`FYNIQ_ORG_SECRETS_PROJECT`); local mode unchanged.
  - Tests: the environment module's `tofu test` and the infra rules (no create right on any other project);
    `make infra-check`. `// req: SEC-12`, `// req: SEC-04`
  - As built: the founder creates the secrets projects with the others (projects are not OpenTofu's, `infra/README.md`);
    OpenTofu configures them (`org_secrets.tf`) and gives each `tofu-<env>` account its own. The API makes an
    organisation's secret the first time it is typed, in Singapore under the environment's secrets key
    (`FYNIQ_ORG_SECRETS_KEY`); `gcp.SplitSecrets` sends every `org-` name there. Not applied: no cloud account yet.
- [x] 8.2 The bank-data port and sync (design D11): accounts, balances and transactions since a cursor; the sync job
  per enabled connector on the tick through the egress guard, writing statements and lines and its cursor in one unit
  of work, the lines waiting for Reconcile now (rev 0.143, task 5.3); the webhook route that only prompts an early sync (404 for an unknown, disabled or uninstalled connector);
  bank-data connector steps checked against Appendix D's "Bank data… connections" row and its approval policy.
  - Tests: a sync from a fake; a webhook prompting one; a webhook body never trusted for data; the permission row.
    `// req: FR-BNK-15`, `// req: FR-BNK-16`, `// req: FR-INT-27`
  - As built: `ports.BankData`, `connector.Binding.BankData` and `VerifyWebhook`, `internal/bankfeed` (the sync and the
    webhook receiver), `bank_feed` and the `connector_route` registry (migration 000155), the platform job `bank.sync`
    (rev 0.144). The provider account is the money account's identifier.
- [x] 8.3 Wise (design D11): manifest and adapter (organisation scope, bank-data, read-only token), balance mapping to
  money accounts, statements and transactions, `balances#update` verified with RSA-SHA256; `contracts/partners/wise.yaml`
  mocked by Prism in the `partners` profile; the providers' facts `[verify]`d against Wise's pages before the
  manifest is pinned.
  - Tests: contract tests against the fake and the partner document; a forged signature refused.
    `// req: FR-BNK-18`, `// req: banking/bank-transactions Wise or Airwallex account syncs itself`
  - As built: `connectors/wise` and `internal/adapters/wise`; the facts checked against Wise's pages on 26 Oct 2026 are
    in design D11's note. Still `[verify]`: a read-only token (Wise documents none, so the adapter sends GET only) and
    where Wise processes what it is sent (the manifest says GB). `contracts/partners/wise.yaml` is an extract of the
    published document, not the whole 1.8 MB; Prism serving it was not run here.
- [x] 8.4 Airwallex (design D11): manifest and adapter (client ID and API key, scoped where offered), balances and
  transactions, webhooks verified by HMAC-SHA256 over `x-timestamp` and the raw body within five minutes;
  `contracts/partners/airwallex.yaml` mocked by Prism; facts `[verify]`d against Airwallex's pages.
  - Tests: contract tests; a forged and a stale signature refused. `// req: FR-BNK-18`
  - As built: `connectors/airwallex` and `internal/adapters/airwallex`; facts in design D11's note. Still `[verify]`:
    where Airwallex processes what it is sent (the manifest says SG). `contracts/partners/airwallex.yaml` is written
    from the reference pages (no published OpenAPI document); Prism serving it was not run here.
- [x] 8.5 The source switch (design D13; the owner's decision): a money account moves from emailed reports to an API
  connector from a date without a gap; the e-statement tie-out stays; the bank agreements are evidence on the account.
  - Tests: a switch to a fake API connector with the chain unbroken. `// req: FR-BNK-17`,
    `// req: banking/bank-transactions Moving DBS to RAPID`, `// req: FR-DOC-05`
  - As built: a connector's first pull starts on its source's first day (`ports.BankData.Transactions` takes it), so
    the switch carries the chain on; agreements are files on the account's `bank_agreement` document (migrations
    000156/000157), attached under C on "Bank data… connections" and committed to the locked store at once.

## 9. The API

- [x] 9.1 The Banking operations (design D14), contract first: money accounts and their sources, imports with
  preview (the statement file an untrusted upload, checked and scanned as evidence uploads are, and kept as the
  statement's file), lines and the queue, rules with the back-test, matches, transfers, signals, auto-reconciliations and undo,
  reconciliations and sign-off, the cash position; `Idempotency-Key`, `If-Match`, `Prefer: return=preview`, RFC 9457
  `BANK-nn` codes; matrix rows for the three Appendix D rows; `make openapi` and the web client regenerated.
  - Tests: the API tests per operation and the authorisation matrix (QA-SEC-03). `// req: FR-INT-01`,
    `// req: QA-SEC-03`
  - As built: 38 operations in three slices (money accounts, mappings, imports and the queue; line actions and
    transfers; rules, the Auto-reconciled list, reconciliations and cash). The signals' operations moved to task 7.3,
    where signals are built. One undo (`unreconcileBankLine`) serves every match, the Auto-reconciled list included.

## 10. The screens

- [x] 10.1 `bank-accounts` (`/banking`) and `add-bank-account` (`/banking/accounts/new`), with the Banking navigation
  item: freshness and what needs categorising; the wizard's kinds of source (file, emailed reports, Wise or Airwallex
  through the connector).
  - Component tests with axe; help keys and registry entries. `// req: FR-BNK-01`, `// req: FR-BNK-13`,
    `// req: FR-BNK-15`
  - As built: `apps/web/src/screens/banking/` with the guide page `docs/user-guide/banking.md` (key
    `banking.accounts`) compiled into `kb/pages/`. Freshness shows complete to, the last statement and gaps (⚠ in
    both modes); "stale" and "sync failed" wait for check C.2 #43, which no API reports yet. The email source names the
    bank-ingest address when it has none. Setting a source is the Administrator's (C on "Bank data… connections"); a
    Bookkeeper's or Accountant's account takes statement files. Links to the pages of 10.2 to 10.5 come with them.
- [x] 10.2 `bank-account` (`/banking/accounts/$id`) and `statement-import` (`/banking/import`): lines with status,
  gaps and duplicates; the import's preview for several accounts.
  - Component tests with axe. `// req: FR-BNK-02`, `// req: FR-BNK-04`, `// req: FR-BNK-16`
  - As built: the register shows each statement's continuity, the gaps (resolved with a reason), the lines with their
    status, a possible duplicate compared side by side and kept or excluded with a reason, the account's pending
    incoming-funds alerts apart (dismissed with a reason) and its transfers in transit. The import takes several
    files, each with its format, account and mapping, previews each, imports each once with its own key, and saves a
    column mapping. PDF statements stay P2.
- [x] 10.3 `categorise-basic` and `categorise-expert` (`/banking/categorise`): one line at a time in Basic, the grid
  in Expert; accept, edit, split, transfer, match and bulk accept.
  - Component tests with axe, and the same request for the same action in both modes (QA-UI-04).
    `// req: FR-BNK-06`, `// req: FR-BNK-07`, `// req: FR-UX-24`
  - As built: one card at a time with its suggestions (accept, change the category, skip) in both modes, Reconcile
    now, and in Expert the grid view (bulk accept of the selection, each line in its own request), split, transfer
    (both sides or in transit), match to chosen invoices or bills, and suspense. The grid is a table: range
    selection, paste and fill-down (FR-UX-05, FR-UX-07) come with the app shell's grid (`p1-app-shell`); AI
    suggestions and their tier-1 proposal arrive with the AI assistant; the rules side-peek comes with 10.4.
- [ ] 10.4 `bank-rules` (`/banking/rules`) and `auto-reconciled` (`/banking/auto-reconciled`): rules with the
  back-test and the auto-apply request; what was reconciled, why, and undo.
  - Component tests with axe. `// req: FR-BNK-05`, `// req: FR-BNK-14`
- [ ] 10.5 `bank-reconciliation` (`/banking/reconciliations/$account/$period`): balances, items, difference,
  prepare and sign-off.
  - Component tests with axe. `// req: FR-BNK-10`
- [ ] 10.6 The user-guide pages for banking (keys for every screen's help), compiled into `kb/pages/`, with
  `make kb-index` and `make kb-lint` clean, and the screens checked in the dev stack against the real API.
  `// req: QA-UI-05`

## 11. Finish

- [ ] 11.1 The banking threat model's abuse cases each have their test, and the security checklist is updated;
  `make check`, `make test`, `make scan`, `make infra-check` and `make images` pass; a conventional commit with
  upgrade notes (the secrets project before the first organisation connector).
  - Not verified here: a live sync with Wise, Airwallex and DBS in staging, which needs their sandboxes and accounts.

## Workflow follow-up

- `/opsx:verify`, then `/opsx:archive p1-bank-import-reconcile --skip-specs` once every task is done.
- Before 4.6: the accountant confirms D6's cross-currency transfer rule.
- Before 7.2 is pinned: a redacted sample of Cognizhi's DBS IDEAL daily account report.
