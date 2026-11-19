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
		Columns:    []string{"person.legal_name", "person.preferred_name", "person.work_email", "person.phone", "person.leaving_reason"},
	},
	{
		Key: "legal-classification", What: "A person's legal classification: contract of service or for service, Employment Act cover and Part IV class, residency and the date residence began, and date of birth",
		Purpose:    "To apply the right statutory rules to the person (leave, hours, retirement and re-employment) and no others",
		Capability: "People records (not restricted)",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "Kept with the employment record: MOM requires employment records for 1 year after the person leaves"},
		Columns:    []string{"person_classification.birth_date", "person_classification.pr_start_date"},
	},
	{
		Key: "restricted-identifiers", What: "A person's NRIC or FIN and salary bank account, each sealed by the key service and kept with a masked form and a keyed fingerprint",
		Purpose:    "To file with the authorities (CPF, IRAS, MOM) and to pay salary: NRIC only for statutory filings, never to sign in",
		Capability: "NRIC or FIN",
		Retention:  Rule{MinMonths: 60, After: "the end of the year it was last used for filing", Reason: "Statutory filings are kept 5 years; the identifier goes with them"},
		Columns:    []string{"person_secret.nric_sealed", "person_secret.nric_masked", "person_secret.nric_fingerprint", "person_secret.bank_sealed", "person_secret.bank_masked", "person_secret.bank_fingerprint"},
	},
	{
		Key: "dependants", What: "A dependant's date of birth for family leave: relationship, citizenship and the employee's declaration",
		Purpose:    "To work out which family leave an employee is entitled to, per scheme and period (NM-PPL-07)",
		Capability: "Dependants (NM-PPL-07)",
		Retention:  Rule{MinMonths: 12, After: "leaving", Reason: "Kept with the employment record while a leave claim may need it"},
		Columns:    []string{"person_dependant.birth_date"},
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
	"organisation.name":            "the name of a company, not of a person",
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
