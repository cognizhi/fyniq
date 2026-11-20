# Tasks

## 1. Decisions and specs
- [x] 1.1 PRD-001 rev 0.480 and PRD-002 rev 0.50 record the decisions; the specs are updated and stamped. `// req: FR-PAY-09`, `// req: FR-PAY-10`
  (21 Nov 2026, rev 0.480 and PRD-002 0.50, 0.51.)
## 2. The CPF file
- [x] 2.1 `payroll.CPFFile` to the Board's layout, with the sample parsed back and hand-worked amounts. `// req: FR-PAY-09`, `// req: NM-PAY-13`
  (Built: `internal/nemura/payroll/cpffile.go`, the Board's sample rebuilt column for column in `TestTheBoardsSampleIsRebuiltColumnForColumn`; the line terminator (CR LF) and the wages the OW and AW columns hold (the CPF wages subject to contribution) are `[verify]` with the Board.)
- [ ] 2.2 Export kind `cpf`, API, matrix rows, approval page button, guide and knowledge-base page. `// req: NM-PAY-13`, `// req: QA-UI-05`

## 3. IR8A and AIS
- [x] 3.1 `payroll.AIS` payload and the data-item checks, with hand-worked years. `// req: FR-PAY-10`, `// req: NM-PAY-14`
  (Built: `ais.go`: IRAS's sample request rebuilt for its first two employees, others income and total income by hand, each data-item rule refused, 2,000-record chunks. Appendix 8A and 8B are the empty objects the specification recommends; a person who needs one is refused.)
- [x] 3.2 The submission port and the local stub with IRAS's sample responses. **[live-verify]** against the sandbox. `// req: NM-PAY-14`
  (Built: `aisapi.go`: `AISSubmitter`, `AISStub` and IRAS's response shapes. **[live-verify]** the real call: CorpPass, APEX and an allow-listed server.)
- [ ] 3.3 Export kind `ais`, API, matrix rows. `// req: NM-PAY-14`

## 4. Left
- IR21 filing (the API's specification is behind IRAS's sign-in) `[verify]`; Appendix 8A and 8B; the employee's IR8A form in My pay.

**Built in slice 2 (not ticked, since their last parts are Left):**
- 2.2 The export kind `cpf`: migration 000048 and 000049, `ExportCPF`, the API's kind `cpf` with a fresh passkey and the employer's payment type, serial and advice code, a test through the service with a sealed NRIC, a person left out, the manifest's SHA-256 and the audit trail holding neither an NRIC nor an amount, and the guide and knowledge-base page. **Left:** the button on the approval page, the Basic and Expert registry entry and a design fragment.
- 3.3 The export kind `ais`: **Left**, since Nemura holds no sex, address or designation of a person (IR8A B6 to B11) and the mapping of pay items to the IR8A boxes (salary, bonus, allowances, director's fees) is the accountant's decision.
