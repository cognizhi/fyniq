// Package datacat is Nemura's registry of data categories (PRD-002 NM-SEC-03, NM-SEC-05; PRD-001 CMP-PD-02): each kind of personal
// data Nemura holds, what it is, why it is held (the PDPA purpose), the right that restricts it, how long it is kept and why, and the
// columns that hold it. A change that adds a record type adds its category in the same commit, and TestTheInventoryIsComplete fails
// when a column whose name looks personal is in no category and not marked not personal with a reason, so a record type cannot land
// without its inventory entry. compliance/nemura-data-inventory.yaml is the same registry for people to read.
package datacat

// Category is one kind of personal data.
type Category struct {
	Key        string   // the key a record type, a retention schedule and an access request use
	What       string   // what it holds, in plain words
	Purpose    string   // why Nemura holds it (the PDPA purpose)
	Capability string   // the row of PRD-002 Appendix B that restricts it; empty when every member sees it (the directory)
	Retention  Rule     // how long it is kept
	Columns    []string // table.column of the schema that holds it
}

// Rule is a retention rule: a minimum kept, after what event, and the legal or business reason. A minimum is not a deletion date:
// other laws, purposes and legal holds may keep a record longer (NM-SEC-04). Min is in months; 0 means no statutory minimum.
type Rule struct {
	MinMonths int
	After     string // the event the period runs from: "leaving", "the record's date", "the request is answered"
	Reason    string
}

// Categories are the kinds of personal data in Nemura's database today. A later change adds its own.
var Categories = []Category{
	{
		Key: "members", What: "Members' display names, the people each organisation's Nemura knows, and their roles and reporting lines",
		Purpose:   "To let people sign in and do their work, to name who did what, and to route approvals and leave to their manager",
		Retention: Rule{After: "leaving", Reason: "Kept while a member and for the audit trail's retention afterwards; a name in an audit event stays with it"},
		Columns:   []string{"member.display_name"},
	},
	{
		Key: "sign-in-identities", What: "Each person's name at the identity provider: its issuer, subject and tenant, and the invitations, recovery codes and sessions that let them in (kept as hashes), with the names of the two operators who issued a recovery",
		Purpose:   "To recognise a person who signs in, never by email alone, and to keep them signed in until they sign out or are deactivated",
		Retention: Rule{After: "leaving", Reason: "Kept while a member; a deactivated member's identity is switched off and kept for the audit trail"},
		Columns:   []string{"identity_directory.subject", "passkey.name", "owner_recovery.operators"},
	},
	{
		Key: "people-records", What: "A person's legal and preferred name, work email, phone, start and leaving, with their job, manager and department history",
		Purpose:    "To run employment: who works here, in what role and under whom, and to meet the employer's record-keeping duties (Employment Act)",
		Capability: "People records (not restricted)",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "MOM: employment records of a former employee are kept for 1 year after they leave"},
		Columns:    []string{"person.legal_name", "person.preferred_name", "person.work_email", "person.phone", "person.leaving_reason", "join_task.note"},
	},
	{
		Key: "legal-classification", What: "A person's legal classification: contract of service or for service, Employment Act cover and Part IV class, residency and the date residence began, and date of birth",
		Purpose:    "To apply the right statutory rules to the person (leave, hours, retirement and re-employment) and no others",
		Capability: "People records (not restricted)",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "Kept with the employment record: MOM requires employment records for 1 year after the person leaves"},
		Columns:    []string{"person_classification.birth_date", "person_classification.pr_start_date"},
	},
	{
		Key: "restricted-identifiers", What: "A person's NRIC or FIN and salary bank account, each sealed by the key service and kept with a masked form and a keyed fingerprint, and the particulars the IR8A asks of them (sex, address and the designation of a non-resident director or seaman), sealed as one document",
		Purpose:    "To file with the authorities (CPF, IRAS, MOM) and to pay salary: NRIC only for statutory filings, never to sign in",
		Capability: "NRIC or FIN",
		Retention:  Rule{MinMonths: 60, After: "the end of the year it was last used for filing", Reason: "Statutory filings are kept 5 years; the identifier goes with them"},
		Columns:    []string{"person_secret.nric_sealed", "person_secret.nric_masked", "person_secret.nric_fingerprint", "person_secret.bank_sealed", "person_secret.bank_masked", "person_secret.bank_fingerprint", "person_secret.ir8a_sealed"},
	},
	{
		Key: "dependants", What: "A dependant's date of birth for family leave: relationship, citizenship and the employee's declaration",
		Purpose:    "To work out which family leave an employee is entitled to, per scheme and period (NM-PPL-07)",
		Capability: "Dependants (NM-PPL-07)",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "Kept with the employment record while a leave claim may need it"},
		Columns:    []string{"person_dependant.birth_date"},
	},
	{
		Key: "leave-records", What: "A person's working pattern, their leave ledger (each grant, use, reservation and correction by type and year, with its note) their leave requests, with the person's note and the approver's reason, and their government-paid leave with the claims filed for it and what the portal answered, their hours worked on special days and what leave gives their pay",
		Purpose:    "To keep an exact balance of leave, to show how it was worked out, and to meet the employer's record-keeping duties for leave (Employment Act)",
		Capability: "Leave records (own and reports)",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "MOM: employment records, leave included, of a former employee are kept for 1 year after they leave"},
		Columns:    []string{"leave_ledger.note", "leave_request.note", "leave_request.decision_reason", "gov_claim_event.note", "overtime_entry.note", "overtime_entry.decision_reason", "pay_input.note"},
	},
	{
		Key: "sick-leave", What: "A person's notice that they were sick: the days, whether outpatient or hospitalisation, their note, and a pointer to the medical certificate (a restricted document) with who issued it and who verified it",
		Purpose:    "To record sick leave against the statutory ceiling, to check the certificate (Employment Act) and to pay what is due",
		Capability: "Medical certificates and notes",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "MOM: employment records, sick leave included, of a former employee are kept for 1 year after they leave"},
		Columns:    []string{"sick_notification.note", "sick_notification.issuer"},
	},
	{
		Key: "flexible-work", What: "A person's formal request for a flexible work arrangement, their reason, and the employer's written reply with the grounds for a refusal",
		Purpose:    "To answer a request in writing within the time the Tripartite Guidelines give, and to keep the written reply the guidelines ask the employer to keep",
		Capability: "Leave requests",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "Kept with the employment record: the written reply is evidence the employer followed the guidelines"},
		Columns:    []string{"flex_request.reason", "flex_request.outcome", "flex_request.written_reply"},
	},
	{
		Key: "own-details", What: "A person's own contact details (personal email, home address, postal code), up to three emergency contacts (name, relationship, phone), their requests to change a legal name, residency or NRIC or FIN with the note and the evidence, and their questions to the people team with the answers",
		Purpose:    "To reach a person and their family in an emergency, to keep the record true on the person's own word and evidence, and to answer what they ask",
		Capability: "Own profile, balances, payslips, letters, requests",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "Kept with the employment record: MOM requires employment records for 1 year after the person leaves"},
		Columns: []string{"person_contact.personal_email", "person_contact.address", "person_contact.postal_code", "emergency_contact.contact_name", "emergency_contact.relationship", "emergency_contact.contact_phone",
			"details_change.new_legal_name", "details_change.note", "details_change.decision_reason", "hr_question.question", "hr_question.reply"},
	},
	{
		Key: "pay-records", What: "A person's pay: their pay group, basic monthly salary, self-help group fund, allowances and fixed deductions, the one-off items of a month (bonuses, commissions), a permanent resident's joint election, the year to date of CPF wages, each pay run's lines, overrides and reasons, and each person's itemised payslips (the twelve items MOM requires, with the PDF) and the state of each payment (what was paid, when, and its instruction)",
		Purpose:    "To work out each person's pay, CPF, SDL and self-help group fund exactly and to show where every figure came from (Employment Act; CPF Act)",
		Capability: "Pay, pay history, pay runs",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "Kept with the employment record: MOM requires employment records for 1 year after the person leaves"},
		Columns: []string{"person_pay.basic_monthly", "person_pay.shg_fund", "person_pay.pay_group_id", "person_pay_item.pay_id", "person_pay_item.name", "person_pay_item.amount", "pay_one_off.name", "pay_one_off.amount", "pay_one_off.note",
			"cpf_ytd.ow", "cpf_ytd.aw_subject", "cpf_ytd.aw_paid",
			"pay_run.pay_group_id", "pay_run_version.reason", "pay_run_override.reason", "pay_run_override.value", "pay_run_line.gross", "pay_run_line.cpf_employee", "pay_run_line.cpf_employer", "pay_run_line.net", "pay_run_line.items", "payslip.data", "payslip.blob_key", "payslip.pay_date", "pay_payment_event.amount", "pay_payment_event.paid_on", "pay_payment_event.instruction_id", "pay_payment_event.note", "tax_hold.reason", "tax_hold.effective_from", "pay_export.total"},
	},
	{
		Key: "key-employment-terms", What: "A person's key employment terms: main duties, notice period, medical benefits and a fixed-term contract's end, and the pay terms (salary period, basic salary, fixed allowances and deductions, overtime period, other pay)",
		Purpose:    "To give the person the key employment terms in writing (Employment Act) and to fill the letter from the record, not from memory",
		Capability: "Pay, pay history, pay runs",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "Kept with the employment record: MOM requires employment records for 1 year after the person leaves"},
		Columns: []string{"person_terms.main_duties", "person_terms.notice_period", "person_terms.medical_benefits", "person_terms.salary_period", "person_terms.basic_salary", "person_terms.fixed_allowances",
			"person_terms.fixed_deductions", "person_terms.overtime_period", "person_terms.other_pay"},
	},
	{
		Key: "people-assistant", What: "What the people assistant drafted for a request (the fields of a letter, the words of a reply), the text a person approved, their reason for not using a draft, and the assistant's own reasoning for it",
		Purpose:    "To let the people team edit, approve or refuse a draft and to apply exactly the text that was approved (NM-AI-02, NM-AI-04)",
		Capability: "Letters and templates",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "Kept with the letter or the reply it became: an approved draft is evidence of what the company stated about the person"},
		Columns:    []string{"ai_proposal.payload", "ai_proposal.final_payload", "ai_draft.payload", "ai_proposal.rationale", "ai_proposal.decision_reason"},
	},
	{
		Key: "letters", What: "A person's request for a letter and what it is for, the facts filled into it from their record, the issued letter's data, the company's letterhead and the notes on a template",
		Purpose:    "To issue letters the person asked for, exactly and attributably, and to keep the issued letter as evidence of what was said and who signed it",
		Capability: "Letters and templates",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "Kept with the employment record: an issued letter is evidence of what the company stated about the person"},
		Columns:    []string{"letter_request.purpose", "letter_request.data", "letter_request.decision_reason", "letter_issued.data", "letter_template_version.note", "letter_delegation.revoked_reason"},
	},
	{
		Key: "access-requests", What: "A person's request for their own data, the decision for each category of record and the written outcome",
		Purpose:    "To answer a request for access to personal data within 30 days and to show how it was answered (PDPA)",
		Capability: "Own file beyond routine release (NM-SEC-07)",
		Retention:  Rule{After: "the request is answered", Reason: "Kept as the record of how the organisation met its obligation"},
		Columns:    []string{"access_request.outcome", "access_request_item.exception"},
	},
	{
		Key: "legal-holds", What: "A reason a person's or a type's records are kept past their retention",
		Purpose:   "To keep records that a dispute, an inquiry or the law requires, and to say why",
		Retention: Rule{After: "the hold is released", Reason: "Kept as the record of why a deletion did not happen"},
		Columns:   []string{"legal_hold.reason"},
	},
}

// NotPersonal are columns whose names look personal and are not, with why. A column here needs a reason a reviewer can read.
var NotPersonal = map[string]string{
	"idempotency_key.body":         "the stored answer of a write that cannot carry pay or restricted data, kept 24 hours; an answer that may is never stored",
	"idempotency_key.had_body":     "whether the first answer had a body: a flag",
	"idempotency_key.body_kept":    "whether that body was stored: a flag",
	"api_client_request.name":      "the name an Owner gives an API client, such as Payroll bridge, not a person",
	"link_inbox.payload_sha256":    "the SHA-256 of a message from Fyniq, which holds none of it",
	"link_inbox.outcome":           "the code of what the link did with a message (applied, duplicate or a refusal), not a person's data",
	"link_restore_run.outcome":     "whether Fyniq posted a pay run (posted or not_posted) after a restore, decided by an Owner: a state of a run, not a person's data",
	"link_department.name":         "the name of a department as Fyniq has it, such as Engineering, not of a person",
	"ai_proposal.payload_hash":     "the SHA-256 of a draft's text, which holds none of it",
	"ai_settings.own_pay":          "whether the Owner turned on explaining a person's own pay: a setting of the company, always off until a second approver exists",
	"pay_group.name":               "the name of a pay group, such as Monthly, not of a person",
	"pay_group.pay_day":            "the day of the month a group is paid, not a person's pay",
	"organisation.name":            "the name of a company, not of a person",
	"letter_settings.company_name": "the name of the company on its letterhead, not of a person",
	"letter_settings.address":      "the company's registered address on its letterhead, not a person's",
	"deletion_report.record_type":  "the name of a kind of record",
	"deletion_proof.record_type":   "the name of a kind of record",
	"legal_hold.record_type":       "the name of a kind of record",
	"access_request_item.category": "the key of a category of data, not a person's data",
	"transfer_assessment.country":  "the country data goes to, not a person's country",
	"approval_request.reason":      "why a request was refused, written for the approver; never personal data by design (SEC-15 masks it)",
	"audit_event.action":           "the name of an action",
	"audit_event.body":             "the audit event, whose personal values are masked or sealed (SEC-15, AUD-02)",
	"outbox.last_error":            "an error message for operators, never a person's data",
	"outbox.task_name":             "the queue's name for one delivery of a task: a row id and a count",
	"identity_directory.issuer":    "the identity provider's address, not a person",
	"identity_directory.tenant":    "the identity provider's tenant for the organisation, not a person",
	"api_client.name":              "the name of a system that calls the API (a company or product), not a person",
	"person_job.job_title":         "the title of a role, not a person's data on its own",
	"person_job.department":        "the name of a department",
	"pack_decision.table_name":     "the name of a table of a statutory pack, not a person",
	"pack_decision.reason":         "the company's reason for using a researched value, written for the record; it names no person by design",
	"outbox.payload":               "the task's payload: ids only, never pay or bank details (ADR-N5)",
}

// Of is the category that holds column ("table.column"), or false.
func Of(column string) (Category, bool) {
	for _, c := range Categories {
		for _, col := range c.Columns {
			if col == column {
				return c, true
			}
		}
	}
	return Category{}, false
}

// ByKey is the category with the key.
func ByKey(key string) (Category, bool) {
	for _, c := range Categories {
		if c.Key == key {
			return c, true
		}
	}
	return Category{}, false
}
