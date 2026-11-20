# Design

## D1. The CPF EZPay (FTP) file

Source: CPF Board, "CPF EZPay (FTP) File Specifications (effective from 16 January 2025)", last updated Jan 2025,
`cpf.gov.sg/content/dam/web/employer/making-cpf-contributions/documents/CPFEZPayFTPSpecifications.pdf`, retrieved 20 Nov 2026.
Fixed 150-byte records: an Employer Header (record type space), one Contribution Summary record per payment code
(`01` CPF, `02` MBMF, `03` SINDA, `04` CDAC, `05` ECF, `07` penalty interest, `10` Community Chest, `11` SDL), a Contribution Detail
record per person per fund (employee account number, amount `9(10)V99`, ordinary and additional wages `9(8)V99`, status E/L/N/O,
name `X(66)`), and a Trailer (record count, sum of summaries `9(13)V99`). The file name is
`<CSN><Month Paid><Advice Code>.DTL`. Rounding: the total CPF payable by the employer is rounded to the nearest dollar, the part
recoverable from wages is rounded down to the dollar (the engine's `RND-CPF` already produces these). The layout is built to the
page's column table, and the page's own sample file is parsed back in a test.
Not guessed: the Payment Type of the employer's CSN (`PTE`, `AMS` and `VCT` are the Board's examples; the employer enters
it, as it is on their EZPay account), and the employment status code (`E/L/N/O` from the run's joiner and leaver inputs).

## D2. IR8A and the AIS-API 2.0 payload

Source: IRAS, "API Services Interface Specifications: AIS-API 2.0", version 2.6 of 11 Sep 2026 (`file.go.gov.sg/aisapi2-specs.pdf`) and its
"List of Data Items and Validation Rules" version 2.5 (`go.gov.sg/aisapi2-dataitems`), retrieved 20 Nov 2026; the IRAS page
(`iras.gov.sg ... auto-inclusion-scheme-(ais)-for-employment-income/technical-file-format-specifications`) states that all AIS
submissions via API use AIS-API 2.0. The payload: `orgAndSubmissionInfo`, `employeePersonalParticulars[]` each with `ir8a`
(and `appendix8A`, `appendix8B` when they apply). Built: the organisation block, the personal particulars, the IR8A items C1 to C31 from
the year's paid runs, and the checks of the items' rules (totals equal the sum of their parts, dates, indicators). Not built: Appendix 8A
(benefits in kind: accommodation, ...) and 8B (the share schemes): Nemura holds no data for them; a person with such items gets a
`needs_manual_appendix` mark and the file counts `totalNumberOfApp8A: 0` only when none applies [accountant to confirm].
Salary, bonus, director's fees and allowances come from the runs' wage items by class; the amounts are whole dollars (Number(9)),
rounded half away from zero per IRAS's practice [accountant to confirm].

## D3. The submission port

`SubmitEmploymentIncome(payload, validateOnly, bypassWarning)` returns the response IRAS defines: `returnCode` 10, 20, 30, 40, the
`acknowledgementCode`, and the `info` object. The local stub applies the data-item rules and returns the spec's sample success
and error payloads. The real adapter needs a CorpPass OAuth 2.1 client, an APEX API key and an IP allow-list: `[live-verify]`.
Submission is chunked at 2,000 records per payload.

## D4. Exports and authority

CPF files and the AIS payload are exports with a written-once manifest and the SHA-256 of the exact bytes (as the journal and the bank
payment file are), made by the Payroll administrator, with a fresh passkey (they carry NRICs, as the payment file carries accounts).
An export is never proof of submission: the person records the Board's receipt or IRAS's acknowledgement code with evidence.
