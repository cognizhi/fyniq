# Converting PRD rows into OpenSpec requirements

## The source file: `openspec/sources/<prd>.json`

```json
{
  "doc": "PRD-001",
  "prd": "docs/prd/prd-001-fyniq-platform.md",
  "rules": [ {"match": "FR-AP-1[4-8]", "capability": "purchases/expense-claims"},
             {"section": "13.1", "capability": "security/identity-and-access"} ],
  "sections": { "10.2": "ledger/posting-rules" },
  "excluded": { "FR-PRJ-04": "Superseded by FR-PRJ-10 (rev 0.8)" },
  "scenario_patterns": ["G4-\\d{3}", "UX-T\\d+"],
  "check_sections": ["C.1", "C.2"],
  "task_sections": ["E"]
}
```

- **Anchors** are what the extractor finds in table rows: requirement IDs (`FR-GL-08`,
  `NFR-PERF-01`, `INV-03`, `CTL-FIN-02`), named policies (`RND-CPF`, `ALLOC-EVEN`), numbered checks
  in `check_sections` (`C.2 #44`), task types in `task_sections` (`bank.categorise_lines`) and
  acceptance scenarios matching `scenario_patterns` (`G4-002`, `UX-T6`).
- **Rules** map anchors to capabilities; the first match wins. A scenario with no rule goes to the
  capability of the first requirement its Trace column names, under that requirement.
- **Sections** are ID-less tables or text that state behaviour (posting rules, a permission matrix,
  pinned figures, use cases). Their capability must cite them (`PRD-001 §10.2`) and turn their
  normative content into requirements without an anchor.
- **Excluded** anchors are withdrawn or superseded; they appear in no spec.
- `prdspec.py stamp` writes `<prd>.lock.json` (row hashes). Never edit the lock by hand.

## Capabilities

`<domain>/<capability>`, kebab-case, named for a durable behaviour (`banking/bank-transactions`),
never for a piece of work. One PRD section usually maps to one capability; split a section only at
a real seam (claims vs bills). Purpose: one or two sentences (≥ 50 characters) saying what the
capability is for, ending "Derived from PRD-001 §8.4" (all the sections it covers).

## A main spec

```markdown
# banking/bank-transactions Specification

## Purpose
How bank, card, wallet and cash lines get into Fyniq and become categorised, matched and reconciled
records. Derived from PRD-001 §8.4 and Appendix J.3.

## Requirements

### Requirement: FR-BNK-04 Duplicate detection
The system SHALL detect a bank line that duplicates one already imported or fed, including a
transfer already recorded from the other account, using a fingerprint of the line.

Source: PRD-001 §8.4 · Priority M · Phase P1

#### Scenario: Fingerprint fields
- **WHEN** a line is imported
- **THEN** its fingerprint is the account, booking date, amount, currency, the bank's transaction reference where given, the counterparty, and the line's running balance or position in its statement

#### Scenario: Identical lines in one statement are both kept
- **GIVEN** two identical lines in the same statement
- **WHEN** the statement is imported
- **THEN** both lines are kept
```

## The rules for each requirement

1. **Header**: `### Requirement: <anchor> <title>`. The title is the row's bold lead-in if it has
   one, else a 2–6 word summary. Headers are permanent: future deltas match them by text.
2. **Description**: what the system SHALL or MUST do, in one or two sentences, faithful to the row,
   with no invented behaviour and no repository rules the row does not state. Uppercase SHALL/MUST.
3. **Source line**: exactly what `prdspec.py show <anchor>` prints after "Source line:", e.g.
   `Source: PRD-001 §8.4 · Priority M · Phase P1`. Compound Priority or Phase cells are copied
   verbatim. Nothing else between the description and the first scenario.
4. **≤ 500 characters** from the header to the first scenario, Source line included: aim for
   ≤ 450. The pattern that works: the description names *what* is kept or done ("The system SHALL
   record …"), and every clause that states *behaviour* becomes its own scenario.
5. **Scenarios**: `#### Scenario: <name>` (four `#`), then `- **GIVEN**` (optional),
   `- **WHEN**`, `- **THEN**`, `- **AND**` (optional). Each is testable. Every clause of the row
   lands in the description or a scenario: nothing is lost. Long lists go in one THEN line.
6. **Acceptance scenarios** the packet lists under a requirement become
   `#### Scenario: G4-002 <short title>`, with the row's Scenario as GIVEN/WHEN and its Required
   outcome as THEN, pinned figures exactly as written.
7. **Revision markers**: drop `(rev 0.N)` markers but keep the clause they introduce; drop text
   marked *Superseded*; keep `[verify]` and `[proposed]` exactly; keep cross-references such as
   `(§10.6)`, `(ENG-45)`, `(open question 31)`.
8. **One-line rows** still need a real scenario: state the observable outcome ("WHEN the Owner
   opens Settings › Data THEN the data region and retention settings are shown read-only").
9. **Non-functional rows** (`NFR-…` with Area/Target columns): the description states the target
   ("The system SHALL serve interactive reads at p95 ≤ 300 ms server time"); the measurement and
   load conditions become scenarios.
10. **Controls** (`CTL-…` with Mapping/Automated check/Freq./Evidence): the description is the
    control; scenarios cover the automated check, its frequency and the evidence kept; the mapping
    (ISO, SOC 2 …) goes in the description if it fits, else a scenario "THEN it maps to …".
11. **Checks** (`C.2 #44`): header `### Requirement: C.2 #44 <check>`; the description says when
    the check fires and its severity in words ("cannot post", "please review"); a scenario per
    trigger. Severity symbols become words.
12. **Policies** (`RND-CPF`): header `### Requirement: RND-CPF <name>`; the formula in a scenario
    with a worked example from the PRD's pinned figures (G.2, G.3) where one exists.
13. **Task types** (`bank.categorise_lines`): header `### Requirement: bank.categorise_lines <task>`;
    the description says what the AI may propose; scenarios cover the tier, the domain command it
    maps to, each limit and the approver.
14. **Derived requirements** (from a cited section, no anchor): header without an ID
    (`### Requirement: Posting a bank transaction, money in`), the same format, and the Source line
    `Source: PRD-001 §10.2` (the section only). Keep them few and faithful; a large table becomes
    one requirement per row or per group of rows that share a behaviour.

## Process for one capability

```bash
python3 scripts/prdspec.py packet <capability>          # the rows to convert, with Source lines
python3 scripts/prdspec.py show <anchor>                # one row in full
# write openspec/specs/<capability>/spec.md
openspec validate <capability> --type spec --strict     # 0 warnings
python3 scripts/prdspec.py coverage --summary           # no error for this capability's anchors
```

Stamp only after review: `python3 scripts/prdspec.py stamp <anchors…>` (or `--all` once every
capability is converted and checked).

## Large PRDs

Convert in the main session, a few capabilities per sitting, and stamp each batch after review.
Do not fan out to subagents: each builds its own context and the main session's prompt cache
expires while it waits (AGENTS.md "Working as an AI agent"). A subagent is fine only for a
self-contained task under about five minutes, such as finding every row that names one account.

## Reading the result

```bash
openspec list --specs                                   # capabilities and requirement counts
openspec show ledger/invariants --type spec              # one capability in full
openspec show ledger/invariants --type spec --json -r 3  # one requirement as JSON
python3 scripts/prdspec.py show FR-GL-08                # the PRD row behind a requirement
```
