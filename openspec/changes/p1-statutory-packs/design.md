# Design

## Context

There are no packs yet. P0's money and rounding package implements the named rounding policies
(`RND-*`), and its tests pin them. The PRD keeps `[verify]` tags on values that still need a primary
source (verification sweep of 14 Oct 2026). The oracle session owns the Python oracle, and the GST
computations get the oracle's half in `p1-gst`, not here.

## Goals / Non-Goals

**Goals:**
- A statutory value used anywhere has exactly one source: an active pack version, chosen by date.

**Non-Goals:**
- GST returns, F5 and InvoiceNow (`p1-gst`, P2).
- CPF computation (Nemura's payroll).
- CIT computation (P2).

## Decisions

- **Packs are files in git, activated in the database.**
  - Each version is a directory, `packs/sg-gst@2026.1/`, holding:
    - `pack.toml` with the metadata;
    - data tables as CSV;
    - `pinned/`, the statutory test cases.
  - Loading a version copies it into global read-only tables; the migrator role writes them.
  - Activation is an organisation's tenant row, so an organisation chooses when to adopt a version,
    under approval.
  - Alternative considered: packs edited in the UI. Rejected because the source URL and the pinned
    tests belong in review (CTL-CHG-01), and the UI only views and activates.
- **Selection is a pure function.** `pack.At(name, date)` returns the version in force. The tax point
  is computed first, by the rule named in the `sg-gst` pack version; the version is then chosen at the
  tax point. Both are pure functions with table tests, and they never call the clock.
- **Activation goes through the approval engine.** It is a high-risk action (SOD-10, FM-1). Until
  `p1-approval-engine` lands, activation is refused ~~unless the organisation is the local sample
  company, which is seeded active~~. (20 Oct 2026, PRD rev 0.37, provisional: the sample company is
  refused too. Nothing marks an organisation as the sample company, and seeding a two-person approval
  would fake SOD-10; no role may insert an activation until the engine's executor gets the grant.)
- **Layout (20 Oct 2026, rev 0.37):** `packs/<pack>/<year>.<n>/` (the skill's nesting) with
  `id = "<pack>@<year>.<n>"` inside, rather than a directory named `sg-gst@2026.1`. Rows of every table
  name their source ids; pinned cases are worked from the sources, not from the tables.
- **`[verify]` blocks activation.** The loader fails a pack version whose metadata marks any value
  unverified, so an unconfirmed rate cannot reach a posting.
- **Rounding stays in the money package.** RND-GST-EXCL and RND-GST-INCL are named policies there;
  the pack chooses only per line or per subtotal (an organisation setting, FR-ORG-04).
- **Ports and adapters (ADR-T8):** storage only.
- **Audit events (AUD-01):** pack loaded, activation proposed, activation approved or refused, and
  version superseded.

## Risks / Trade-offs

- [The official pages change their wording or URLs] → Each pack's source URL is checked by a
  scheduled link check later (wave 5 compliance). Pinned tests keep the values honest meanwhile.
- [The tax agent rules differently at GST registration (question 29)] → The tax-point and
  reverse-charge rules are named and versioned, so a ruling is a new pack version, not a code change.

## The rows of PRD-001 §14.2 and the change that closes each (task 5.1; 20 Nov 2026)

`compliance/singapore-statutory` holds the 17 requirements the unnumbered rows of §14.2 give. None is a PRD row with an
ID, so the launch gate reports them (never blocks); a test closes one with a tag `// req: compliance/singapore-statutory
<requirement> [<scenario>]`. Each row is mapped below to the change that builds what it asks, with where it stands.

| Row (requirement) | Closed by | Where it stands |
|---|---|---|
| Record keeping and retention | `p1-documents-evidence` (retention, legal hold, FR-DOC-02), `p1-pdpa-retention` (the schedule, CMP-PD-02), **this change** (the periods) | Built. The default of 7 years after the financial year's end covers the Companies Act's, income tax's and GST's periods for every financial-year start (`TestTheDefaultRetentionCoversEveryStatutoryPeriod`, which tags the periods and IRAS's examples); the file store keeps a lock and a legal hold (`porttest.BlobRetention`, `TestAContractIsKeptUntilItsTransactionsComplete`); every ledger line is kept in its own currency and in the base (ENG-10 to ENG-14, ENG-40, `p1-multi-currency-fx`); the nightly records check is CTL-AUD-03. |
| IRAS audit files and listings | `p1-gst` task 7.3 (FR-TAX-04); `p1-reports` for the general ledger export (FR-RPT-10) | The ledger export is built; the sales and purchase listings come at GST registration, after go-live (section 7 of `p1-gst`). No IAF and no ASR+ is built, as the row says. |
| GST rate and registration threshold | **this change** (`sg-gst`, ENG-31) and `p1-gst` (the registration monitor) | Built: `sg-gst@2026.3` holds the rates and the threshold with their sources and pinned cases; the monitor and its alert are `p1-gst` sections 2 to 5. |
| GST returns | `p1-gst` task 7.2 (FR-TAX-02, FR-TAX-03) | After go-live, at GST registration (the owner's decision); the calendar's F5 items wait for it. |
| Tax invoices | `p1-sales` (FR-AR-03), the document templates (FR-TPL, the IRAS particulars check), this change (RND-GST-EXCL, RND-GST-INCL) | Built: the particulars IRAS requires are checked on every document (`TestParticularsAreFoundInTheDocument`), GST rounds per line or per subtotal as set (`rounding_test.go`, the differential), a foreign-currency invoice shows SGD. |
| GST bad-debt relief | `p1-sales` (a bad-debt write-off is tagged for the relief review, `internal/api/writeoffs.go`); the review is FR-AR-11 (P2), `p2-tax-filings` | Tagged only; the claim queue is P2. |
| InvoiceNow (Peppol) | `p2-invoicenow` | P2. |
| Corporate income tax | `p2-tax-filings` | P2; `sg-acra` and `sg-deadlines` carry the dates the calendar shows meanwhile. |
| ACRA annual filings | `p1-company-secretary` (the calendar, the ACRA extract, the filing record), this change (`sg-deadlines`) | Built for the dates and records; the XBRL classifier and filing pack are P2 (`p2-tax-filings`). |
| Share issues and directors' balances | `p1-equity-register`, `p1-founders-money` | Built: the prior-approval step, the allotment records and the director accounts' settlements. |
| Company secretary and corporate changes | `p1-company-secretary` | Built. |
| CPF contributions | this change (`sg-cpf` data), the payroll calculator in Nemura | The data is built; the calculation runs in payroll, after go-live (the owner's decision). |
| SDL, SHG and levies | this change (`sg-sdl`, `sg-shg` data), payroll | As CPF. |
| MOM employment records | Nemura (`p1-nemura-records-packs`, PRD-002 NM-PAY) | After go-live with payroll. |
| IR8A, AIS and IR21 | `p2-payroll-outputs` (files), PRD-001 C.2 #44 (the IR21 hold) | After go-live. |
| Electronic signatures on approvals | `p1-approval-engine` | Built: tagged in `approvals_test.go` and `approval_attack_test.go`. |
| MAS guidelines as reference only | **this change** | Closed: no framework or control of the registry names a MAS guideline (`TestMASGuidelinesAreReferenceOnly`); revisiting when the company serves financial institutions is a decision for the owner, with no code to prove. |

What remains open is each module's own: nothing in this table waits on a task of `p1-statutory-packs`.
