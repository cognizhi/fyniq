# The launch's gaps

The first report of the trace gate (QA-TRACE-01; `p1-launch-readiness` task 1.3), on 16 Nov 2026. `make trace` writes the
live report to `.data/trace/report.json`; this page says how each gap closes. It is kept by hand, and its counts are
those of the day it was written.

## Counts

On main, the launch subset (`docs/prd/plans/launch-subset.json`) holds 37 changes and 854 requirements:

| | Requirements and scenarios |
|---|---|
| Covered by a tagged test | 597 |
| Excepted (`docs/prd/plans/launch-exceptions.json`) | 34 |
| Reported, not blocking: S and C rows, and the specs' prose requirements with no ID | 166 |
| Untested, blocking | 127 |

Once the stacked chain (#643 to #739) merges, 81 stay untested. The chain's tests cover 46.

## How each closes

### When the chain merges (46)

- `p1-ai-providers`: AI-12, CTL-AI-05, CMP-AI-02, CMP-AI-03, C.2 #55.
- `p1-ai-assistant`: AI-01 to AI-05, AI-18 to AI-25, AI-68, AI-69, QA-AI-01 to QA-AI-03, QA-AI-07, CTL-AI-06.
- `p1-scheduled-agents`: AI-61 to AI-65, AI-67, CTL-AI-09, C.2 #36, G4-076.
- `p1-company-secretary`: FR-CS-01, FR-CS-02, FR-CS-04, FR-CS-06, FR-CS-08, C.2 #32 to C.2 #35, C.2 #54, G4-102,
  G4-110.

### Tagged where a test already proves it (task 2, 48)

Each is read against its test before it is tagged (design D3). One that turns out not to be built moves to its change.

- `p1-public-api`: FR-INT-02 (client classes).
- `p1-users-roles`: SEC-07, G4-061.
- `p1-statutory-packs`: G4-018, G4-019, G4-020, G4-021 (the tax point's pinned cases).
- `p1-approval-engine`: G4-056, G4-057.
- `p1-documents-evidence`: G4-068, G4-069.
- `p1-templates-email`: G4-073.
- `p1-general-ledger-close`: G4-017, G4-053.
- `p1-multi-currency-fx`: G4-023, G4-025.
- `p1-sales`: G4-045, G4-054.
- `p1-purchases`: G4-024.
- `p1-connector-framework`: G4-096.
- `p1-bank-import-reconcile`: G4-038, G4-042, G4-059.
- `p1-equity-register`: C.2 #26, C.2 #27, C.2 #28, G4-043, G4-044.
- `p1-reports`: FR-VIZ-04, FR-VIZ-05, FR-VIZ-07, G4-052, G4-065, G4-098.
- `p1-dashboard-kpis`: G4-046.
- `p1-planning`: G4-108.
- `p1-checks`: C.1 #34, C.2 #49, G4-036, G4-037, G4-105.
- `p1-ai-providers`: G4-088.
- `p1-scheduled-agents`: G4-103.
- `p1-security-hardening`: SEC-26, G4-097.
- `p1-operations-dr`: G4-085.
- `p1-workbook-migration`: G4-016.
- `p1-launch-readiness`: G4-013 (the database's NaN refusal).

### Built in this change (tasks 3 to 7, 16)

QA-ENG-07 with the 13 unnumbered sets, QA-RPT-03, QA-RPT-04, QA-PROD-01, QA-REL-01, QA-MCP-01 (the shell's part),
QA-IMG-01, QA-UI-01, QA-UI-02, QA-UI-03, QA-UI-06 with G4-104, QA-UI-07, QA-UI-08, QA-SEC-05 and QA-E2E-01.

### In their own change (17)

- `p1-app-shell` (14): NFR-A11Y-03, NFR-A11Y-04, NFR-A11Y-05, NFR-PERF-UI-01, FR-UX-06, FR-UX-08, FR-UX-11, FR-UX-14,
  FR-UX-25, FR-UX-30, FR-UX-32, FR-UX-34 (the first-run tour, which QA-UI-06 waits for), FR-UX-37, G4-077.
- `p1-ai-assistant` (2): QA-AI-06, the knowledge evals, since the launch's assistant reads the knowledge base; G4-087,
  the pre-go-live check of the model IDs, proved in the dev cloud with `p1-go-live`.
- `p1-approval-engine` (1): FM-6, the AI's advisory review in Founder mode. It launches, by the owner's decision of 16
  Nov 2026, taken interactively: a scheduled agent like the company secretary reviews the week's journals, vendor bank
  changes and payments, and writes advisory findings into the weekly review, never approving. It is built on the
  scheduled agents of the chain, once they merge.
  - *Not chosen:* after go-live with `p1-ai-proposals`, or deterministic rule checks at launch with the AI later.

## The exceptions

- **After go-live (10):**
  - the GST F5's part of `p1-gst`, at registration (FR-TAX-01 to FR-TAX-04, FR-TAX-12, C.2 #46, C.2 #47, G4-011, G4-035);
  - SOD-4, with payroll;
  - QA-API-04, with Nemura.
  Each rests on rev 0.25.
- **Proved in the dev cloud (24):**
  - the controls whose checks run in the cloud (CTL-IAM-03, CTL-IAM-04, CTL-DP-01, CTL-RES-01, CTL-AUD-04, CTL-RES-03,
    CTL-CHG-04, CTL-CHG-05);
  - DR-01, DR-03, NFR-DR-02, NFR-PERF-06, NFR-AVAIL-01 and SEC-30;
  - the costs (NFR-COST-01 to NFR-COST-04, G4-078, G4-086);
  - the drills (G4-072, G4-074, G4-075).
  Each names its runbook or control file.
