# Design

## Context

Appendix F is the initial registry and names `compliance/controls/` as its home. The Compliance Center
(P1) will read the same files and evidence.

## Goals / Non-Goals

**Goals:**
- Every control in Appendix F is a file, and every automatable P0 control has a check that runs and leaves
  evidence.

**Non-Goals:**
- Certification work (Cyber Trust, ISO 27001, SOC 2), which is P2–P3.

## Decisions

- **YAML files with a JSON Schema.** One file per control ID (`CTL-CHG-01.yaml`), with `check:` naming a
  make target or a job. Validation runs in `make check`, so the registry cannot drift from Appendix F:
  a script compares the IDs with the PRD's Appendix F rows.
- **Evidence.** CI checks upload their reports as workflow artefacts; scheduled checks write to the
  evidence bucket with the run ID. Both are listed per control.
- **Log scrubbing.** A test drives each service with sample data containing synthetic NRIC and bank
  numbers and asserts that none appear in logs (SEC-15).
- **Audit events.** `control.check.failed` from scheduled checks (AUD-01).
- **Ports.** `BlobStore` for evidence.

## Risks / Trade-offs

- [Controls that need a human (quarterly access review)] → the registry records them with a reminder task,
  not a check, until P1's Compliance Center.
