# Tasks

## 1. Envelope and records

- [ ] 1.1 Write the envelope builder per object type, with its declared dependencies, in RFC 8785 canonical JSON with SHA-256. Test `// req: FR-APR-10`: a rebuilt envelope that differs blocks execution
- [ ] 1.2 Signed approval records; the database refuses one without a hash or an MFA claim, and a version trigger voids approvals on edit. Tests `// req: FR-APR-03` (G4-056) and `// req: compliance/singapore-statutory Electronic signatures on approvals`
- [ ] 1.3 Step-up for high-risk approvals, and expiry from the app clock with invalidation on any dependency change. Tests `// req: FR-APR-04` and `// req: FR-APR-05` (G4-055)

## 2. Policies

- [ ] 2.1 Policies per object type, with conditions and ordered steps with a quorum. Test `// req: FR-APR-01`
- [ ] 2.2 Policy changes are approval-gated (SOD-8) and versioned; a pending approval keeps its version, and a change restarts it. Test `// req: FR-APR-09` (G4-057)
- [ ] 2.3 The approval view: document, ledger impact, affected balances and checks, and the diff against the previous version. Test `// req: FR-APR-02`

## 3. Team modes and Founder mode

- [ ] 3.1 Founder, Standard and Enterprise modes, with dual-controlled switching. Tests `// req: access/team-modes <mode> mode` and `… Mode switching`
- [ ] 3.2 FM-1 (two-person rule with step-up for both), FM-2 (self-approval at or below the threshold, flagged) and FM-3 (the weekly review task, escalating after 14 days). Tests `// req: FM-1`, `// req: FM-2` and `// req: FM-3`
- [ ] 3.3 FM-8 break-glass and FM-9 beneficiary exclusion. Tests `// req: FM-8` (G4-062, G4-063) and `// req: FM-9`
- [ ] 3.4 The rules and records for FM-4, FM-5 and FM-7, called by the close (wave 2) and reconciliation (wave 3). FM-6's advisory review arrives with wave 6. Tests `// req: FM-4`, `// req: FM-5` and `// req: FM-7` on the records (G4-109)
- [ ] 3.5 The control check: period lock, approvals, segregation of duties and team mode together. Test `// req: FR-CTL-04`

## 4. Wire the waiting refusals

- [ ] 4.1 Switch vendor bank details (FR-CT-03), account merges (FR-COA-04), pack activation (ENG-32), administrative changes (FR-ADM-04) and Owner changes (FR-ADM-09) from "refused until the engine lands" to approval through it, with each change's test extended

## 5. Screens

- [ ] 5.1 Build the screens as designed: accountant-review, approval-confirmation, approval-policies, bank-details-change, payment-batch (approval part), personal-approvals and team-mode. Add help keys and `docs/user-guide/` pages compiled into `kb/`; the Inbox approve shortcut opens the confirmation and never approves directly (FR-UX-03)
- [ ] 5.2 Finish with `make check` and `make test`
