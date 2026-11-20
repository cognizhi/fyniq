package authz

// Rows is PRD-002 Appendix B, the permission matrix, transcribed once and in the PRD's order: one row per capability, one cell per
// role in the order of Roles, each cell as the PRD writes it. TestTheMatrixIsAppendixB fails when a cell, a row or the order
// differs from the PRD, so a PRD change that is not carried over fails `make test`.
var Rows = []Row{
	{"Own profile, balances, payslips, letters, requests", [RoleCount]string{"O", "O", "O", "O", "O", "O", "O", "O", "O", "O (as the asking person)"}},
	{"Own file beyond routine release (NM-SEC-07)", [RoleCount]string{"C (request)", "C (request)", "C (handles)", "A", "—", "—", "—", "—", "V", "—"}},
	{"Erasure of a former employee's data (NM-SEC-08)", [RoleCount]string{"—", "—", "—", "C/A", "—", "—", "—", "—", "C/A", "—"}},
	{"Grievances (NM-SEC-06)", [RoleCount]string{"C (own)", "—", "—", "C/A (when assigned)", "—", "—", "—", "—", "C/A (when assigned)", "—"}},
	{"Directory and org chart", [RoleCount]string{"V", "V", "V", "V", "V", "V", "V", "V", "V", "V"}},
	{"People records (not restricted)", [RoleCount]string{"—", "V (reports)", "C", "C/A", "V", "—", "—", "—", "A", "V (within the asking person's rights)"}},
	{"NRIC or FIN", [RoleCount]string{"—", "—", "M", "V", "V (statutory filings)", "—", "—", "—", "V", "—"}},
	{"Salary bank account", [RoleCount]string{"C (propose own)", "—", "M", "A (not own)", "V; A (not own)", "—", "—", "—", "V; A", "—"}},
	{"Medical certificates and notes", [RoleCount]string{"—", "—", "V", "V", "—", "—", "—", "—", "M", "—"}},
	{"Disciplinary records", [RoleCount]string{"—", "—", "C", "C/A", "—", "—", "—", "—", "V", "—"}},
	{"Dependants (NM-PPL-07)", [RoleCount]string{"C (own)", "—", "V", "A", "V (where pay depends on it)", "—", "—", "—", "M", "—"}},
	{"Pay, pay history, pay runs", [RoleCount]string{"—", "—", "—", "—", "C", "—", "—", "—", "A", "—"}},
	{"Leave requests", [RoleCount]string{"C (own)", "A (reports)", "A", "A", "V", "—", "—", "—", "A", "—"}},
	{"Letters and templates", [RoleCount]string{"C (request)", "—", "C", "A (templates)", "—", "—", "—", "—", "S (or a delegated signatory, NM-LTR-04)", "C (draft)"}},
	{"Features, packs, policies", [RoleCount]string{"—", "—", "V", "C", "V", "—", "—", "V", "A", "—"}},
	{"Roles (NM-ROLE-05)", [RoleCount]string{"—", "—", "—", "C (not payroll, signing or Owner rights)", "—", "—", "—", "C (not payroll, signing or Owner rights)", "A", "—"}},
	{"Fyniq link and external systems", [RoleCount]string{"—", "—", "—", "C", "V", "—", "—", "C", "A (+ second approver)", "—"}},
	{"Hiring (P2)", [RoleCount]string{"—", "V (own roles)", "V", "V", "—", "C", "V (assigned candidates only)", "—", "A (offers)", "C (drafts)"}},
	{"Performance (P2)", [RoleCount]string{"O", "C (reports)", "V", "C", "—", "—", "—", "—", "A", "C (drafts)"}},
}
