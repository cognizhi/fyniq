package authz

import (
	"fmt"
	"slices"
	"strings"
)

// Right is a letter of Appendix D's legend.
type Right byte

// The rights. View is satisfied by any right on the capability (one cannot prepare, approve, post
// or release what one cannot see); Masked views with the policy's fields masked.
const (
	Create  Right = 'C' // create or prepare
	Approve Right = 'A'
	Post    Right = 'P' // post or execute
	Release Right = 'R' // release money
	View    Right = 'V'
	Masked  Right = 'M' // view masked
)

// Capability is a row of Appendix D, named exactly as the PRD names it.
type Capability string

// The capabilities: every row of Appendix D, in its order, named exactly as the PRD names it (users-roles 1.1).
const (
	UsersAndRoles       Capability = "Users, roles, team mode"
	ApprovalPolicies    Capability = "Approval policies, AI catalogue"
	OrganisationProfile Capability = "Organisation profile, financial year, defaults (rev 0.29)"
	AccountingBasis     Capability = "Accounting framework and policies (rev 0.62)"
	StatutoryParameters Capability = "Statutory parameters"
	ChartOfAccounts     Capability = "Chart of accounts, dimensions"
	ContactsAndItems    Capability = "Contacts and items (rev 0.33)"
	BankTransactions    Capability = "Bank transactions, categorise"
	BankReconciliation  Capability = "Bank reconciliation sign-off"
	Invoices            Capability = "Invoices, credit notes"
	Bills               Capability = "Bills, vendor credits"
	VendorBankDetails   Capability = "Vendor bank details"
	PaymentBatches      Capability = "Payment batches"
	ExpenseClaims       Capability = "Expense claims"
	DirectorAccounts    Capability = "Director accounts, settlements, share issues (rev 0.3)"
	CapTable            Capability = "Cap table, holders, investors, equity transactions (rev 0.3)"
	Evidence            Capability = "Evidence: add, supersede (rev 0.3)"
	KnowledgeBase       Capability = "Knowledge base: compiled pages and index (rev 0.4)"
	AICompanySecretary  Capability = "AI company secretary: enable, schedule, runs and drafts (rev 0.6)" // #nosec G101 -- a permission row's name, not a credential
	Integrations        Capability = "Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture (rev 0.6)"
	Announcements       Capability = "Announcements to the organisation (rev 0.6)"
	OwnNotifications    Capability = "Own notifications and preferences (rev 0.6)"
	DocumentTemplates   Capability = "Document templates: edit, publish (rev 0.7)"
	Timesheets          Capability = "Timesheets and project time (rev 0.7)"
	ProjectReports      Capability = "Project reports and cost rates (rev 0.8)"
	BankConnections     Capability = "Bank data, payment-provider and ticketing connections (rev 0.7)"
	Journals            Capability = "Journals"
	PayRuns             Capability = "Pay runs, employee data"
	FixedAssets         Capability = "Fixed assets"
	PeriodClose         Capability = "Period close / reopen"
	GSTReturns          Capability = "GST return, filing"
	Reports             Capability = "Reports, dashboards"
	Budgets             Capability = "Budgets, forecasts"
	AuditTrail          Capability = "Audit trail"
	ComplianceCenter    Capability = "Compliance Center"
	TenantExport        Capability = "Tenant export"
	AIProviders         Capability = "AI providers and model routes: connections, credentials, billing, data-class policy, per-agent routes (rev 0.12, AI-72…AI-75)"
	PeopleAndHR         Capability = "People and HR: the Nemura link and external HR or payroll systems (rev 0.15, FR-PPL-01); imported payroll journals are approved under Journals"
	ConnectedApps       Capability = "Connected apps: enable MCP, allowlist clients, grant integration scopes (rev 0.2)"
	OwnMCPConnection    Capability = "Own MCP connection (opt in, consent, revoke) (rev 0.2)"
)

// Rows are Appendix D's rows in its order.
var Rows = []Capability{UsersAndRoles, ApprovalPolicies, OrganisationProfile, AccountingBasis, StatutoryParameters, ChartOfAccounts, ContactsAndItems, BankTransactions, BankReconciliation, Invoices, Bills, VendorBankDetails, PaymentBatches, ExpenseClaims, DirectorAccounts, CapTable, Evidence, KnowledgeBase, AICompanySecretary, Integrations, Announcements, OwnNotifications, DocumentTemplates, Timesheets, ProjectReports, BankConnections, Journals, PayRuns, FixedAssets, PeriodClose, GSTReturns, Reports, Budgets, AuditTrail, ComplianceCenter, TenantExport, AIProviders, PeopleAndHR, ConnectedApps, OwnMCPConnection}

// The capabilities outside Appendix D.
const (
	// Membership is not an Appendix D row: the caller's organisation (its name and base currency),
	// which every screen shows, is readable by every member of it. AI acts for a member; the Super
	// admin and the HR officer (Nemura only) are not members through those roles.
	Membership Capability = "(membership)"
	// Setup is first-run setup (FR-ADM-06): not an Appendix D row either. Its status is public; completing
	// it needs the one-time setup code and a person signed in, never the AI.
	Setup Capability = "(installation setup)"
	// SuperAdmins are the installation's Super admins and their one-time codes (FR-ADM-06/07/09): not
	// an Appendix D row. Who may act is the database's record of Super admins, step-up and the codes;
	// never the AI.
	SuperAdmins Capability = "(Super admins)"
	// Session is the caller's own sign-in (SEC-05): not an Appendix D row either. The providers to sign
	// in with are public; a session reads only itself, and the AI has none.
	Session Capability = "(your session)"
	// Joining is redeeming an invitation to an organisation (FR-ADM-01): not an Appendix D row either. The
	// person has no organisation yet; the invitation's code decides, and the AI never joins.
	Joining Capability = "(joining by invitation)"
	// Approvals are the approval engine's requests (FR-APR-01…05, FM-1): not an Appendix D row either. Each
	// request's own policy decides who sees and signs it, by the Appendix D row of the object it changes
	// (ApprovalRows): viewing needs a right on that row, signing its A. The operations let in a person who holds
	// the right needed on one of those rows, and never the AI, which never approves or rejects (§9.6).
	Approvals Capability = "(approvals)"
)

// ApprovalRows are the Appendix D rows of the objects the approval engine covers: organisation settings under dual
// control, and members' roles and invitations after the founder bootstrap (rev 0.36); approval policies (rev 0.40);
// activating statutory pack versions (rev 0.41); reopening a closed period (rev 0.51); posting a journal above the
// self-approval threshold, and voiding one (rev 0.53); posting a bill above the threshold, and voiding one (p1-purchases).
var ApprovalRows = []Capability{OrganisationProfile, UsersAndRoles, ApprovalPolicies, StatutoryParameters, PeriodClose, Journals, Bills}

// Cell is one cell of Appendix D: the rights it grants and its qualifier, such as "limit".
type Cell struct {
	Rights    string // e.g. "CP" for "C/P (limit)"; "" for "—", "Proposal" and other words
	Qualifier string // the words after the letters, e.g. "limit"; or the whole cell when it has no letters
}

// Grants are Appendix D's cells, by row and column, copied from the PRD: a cell's leading letters are its rights and the
// rest its qualifier. The matrix test parses Appendix D and fails when a cell here differs from it, or a row is missing or
// extra. A capability without a row grants nothing to anyone.
var Grants = map[Capability]map[string]Cell{
	UsersAndRoles: {
		"Owner": {"A", ""}, "Admin": {"C", ""}, "Controller": {"V", ""}, "Accountant": {}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {}, "Board": {}, "DPO": {"V", ""}, "AI": {},
	},
	ApprovalPolicies: {
		"Owner": {"A", ""}, "Admin": {"C", ""}, "Controller": {"A", ""}, "Accountant": {"V", ""}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {}, "Board": {}, "DPO": {"V", ""}, "AI": {},
	},
	OrganisationProfile: {
		"Owner": {"A", ""}, "Admin": {"C", ""}, "Controller": {"C", ""}, "Accountant": {"V", ""}, "Bookkeeper": {"V", ""}, "AR": {"V", ""}, "AP": {"V", ""}, "Payroll": {"V", ""}, "Releaser": {"V", ""}, "Approver": {"V", ""}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {"V", ""}, "DPO": {"V", ""}, "AI": {"V", ""},
	},
	AccountingBasis: {
		"Owner": {"C", ""}, "Admin": {"V", ""}, "Controller": {"C", ""}, "Accountant": {"CA", ""}, "Bookkeeper": {"V", ""}, "AR": {"V", ""}, "AP": {"V", ""}, "Payroll": {"V", ""}, "Releaser": {"V", ""}, "Approver": {"V", ""}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {"V", ""}, "DPO": {"V", ""}, "AI": {"V", ""},
	},
	StatutoryParameters: {
		"Owner": {"A", ""}, "Admin": {}, "Controller": {"CA", ""}, "Accountant": {"C", ""}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {"V", ""}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"C", ""}, "Board": {}, "DPO": {}, "AI": {"V", ""},
	},
	ChartOfAccounts: {
		"Owner": {"A", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"C", ""}, "Bookkeeper": {"V", ""}, "AR": {"V", ""}, "AP": {"V", ""}, "Payroll": {"V", ""}, "Releaser": {"V", ""}, "Approver": {"V", ""}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"C", ""}, "Board": {}, "DPO": {}, "AI": {"V", ""},
	},
	ContactsAndItems: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"C", ""}, "Accountant": {"C", ""}, "Bookkeeper": {"C", ""}, "AR": {"C", ""}, "AP": {"C", ""}, "Payroll": {}, "Releaser": {"V", ""}, "Approver": {"V", ""}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {}, "DPO": {"V", ""}, "AI": {"V", ""},
	},
	BankTransactions: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"CP", ""}, "Bookkeeper": {"CP", ""}, "AR": {"V", ""}, "AP": {"V", ""}, "Payroll": {}, "Releaser": {"V", ""}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {}, "DPO": {}, "AI": {"", "Proposal"},
	},
	BankReconciliation: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"C", ""}, "Bookkeeper": {"C", ""}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {}, "DPO": {}, "AI": {},
	},
	Invoices: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"CP", ""}, "Bookkeeper": {"C", ""}, "AR": {"CP", ""}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {"A", "limit"}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {}, "DPO": {}, "AI": {"", "Proposal"},
	},
	Bills: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"CP", ""}, "Bookkeeper": {"C", ""}, "AR": {}, "AP": {"C", ""}, "Payroll": {}, "Releaser": {}, "Approver": {"A", "limit"}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {}, "DPO": {}, "AI": {"", "Proposal"},
	},
	VendorBankDetails: {
		"Owner": {"A", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"C", ""}, "Bookkeeper": {}, "AR": {}, "AP": {"C", ""}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"M", ""}, "Tax agent": {"M", ""}, "Board": {}, "DPO": {}, "AI": {},
	},
	PaymentBatches: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"C", ""}, "Bookkeeper": {}, "AR": {}, "AP": {"C", ""}, "Payroll": {"C", "salaries"}, "Releaser": {"R", ""}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {}, "DPO": {}, "AI": {},
	},
	ExpenseClaims: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"V", ""}, "Bookkeeper": {"V", ""}, "AR": {}, "AP": {"V", ""}, "Payroll": {}, "Releaser": {}, "Approver": {"A", ""}, "Employee": {"C", "own"}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {}, "DPO": {}, "AI": {"", "Proposal (draft)"},
	},
	DirectorAccounts: {
		"Owner": {"A", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"CP", ""}, "Bookkeeper": {"V", ""}, "AR": {}, "AP": {}, "Payroll": {"V", "deferred pay"}, "Releaser": {"R", "repayments"}, "Approver": {}, "Employee": {"V", "own"}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {"V", "share issues"}, "DPO": {}, "AI": {"", "Ask only"},
	},
	CapTable: {
		"Owner": {"A", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"CP", ""}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {"V", "own grants"}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {"V", "own holding; all if granted"}, "DPO": {"M", ""}, "AI": {"", "Scenarios only, if `fyniq.read.equity` granted"},
	},
	Evidence: {
		"Owner": {"C", ""}, "Admin": {}, "Controller": {"C", ""}, "Accountant": {"C", ""}, "Bookkeeper": {"C", ""}, "AR": {"C", ""}, "AP": {"C", ""}, "Payroll": {"C", "payroll"}, "Releaser": {"C", ""}, "Approver": {"C", ""}, "Employee": {"C", "own"}, "Auditor": {"V", ""}, "Tax agent": {"C", ""}, "Board": {}, "DPO": {"V", ""}, "AI": {"", "Proposal (`doc.attach`)"},
	},
	KnowledgeBase: {
		"Owner": {"VC", "edit, publish"}, "Admin": {"VC", "edit, publish"}, "Controller": {}, "Accountant": {}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {}, "Board": {}, "DPO": {}, "AI": {"", "Reads, filtered to the user (AI-54)"},
	},
	AICompanySecretary: {
		"Owner": {"A", "+ second approver (enable); C (schedule)"}, "Admin": {"C", "schedule"}, "Controller": {"V", ""}, "Accountant": {"V", ""}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {"V", "summaries"}, "DPO": {}, "AI": {"", "Runs as the agent principal (AI-63)"},
	},
	Integrations: {
		"Owner": {"A", "+ second approver"}, "Admin": {"C", ""}, "Controller": {"V", ""}, "Accountant": {}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {}, "Board": {}, "DPO": {"V", ""}, "AI": {},
	},
	Announcements: {
		"Owner": {"CA", ""}, "Admin": {"C", ""}, "Controller": {}, "Accountant": {}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {}, "Board": {}, "DPO": {}, "AI": {},
	},
	OwnNotifications: {
		"Owner": {"C", ""}, "Admin": {"C", ""}, "Controller": {"C", ""}, "Accountant": {"C", ""}, "Bookkeeper": {"C", ""}, "AR": {"C", ""}, "AP": {"C", ""}, "Payroll": {"C", ""}, "Releaser": {"C", ""}, "Approver": {"C", ""}, "Employee": {"C", ""}, "Auditor": {"C", ""}, "Tax agent": {"C", ""}, "Board": {"C", ""}, "DPO": {"C", ""}, "AI": {},
	},
	DocumentTemplates: {
		"Owner": {"A", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"C", ""}, "Bookkeeper": {}, "AR": {"C", ""}, "AP": {"C", ""}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {}, "Board": {}, "DPO": {}, "AI": {},
	},
	Timesheets: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"V", ""}, "Bookkeeper": {}, "AR": {"V", ""}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {"A", "project manager"}, "Employee": {"C", "own"}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {}, "DPO": {}, "AI": {"", "Proposal (draft invoice)"},
	},
	ProjectReports: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"V", "role-average rates"}, "Accountant": {"V", "role-average rates"}, "Bookkeeper": {}, "AR": {"V", "revenue, hours"}, "AP": {}, "Payroll": {"V", ""}, "Releaser": {}, "Approver": {"V", "own projects; role-average rates"}, "Employee": {}, "Auditor": {"V", "role-average rates"}, "Tax agent": {"V", "role-average rates"}, "Board": {}, "DPO": {}, "AI": {"", "Same as the user; person-level rates only with payroll access (FR-PAY-13, AI-02)"},
	},
	BankConnections: {
		"Owner": {"A", "+ second approver"}, "Admin": {"C", ""}, "Controller": {"V", ""}, "Accountant": {}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {}, "Board": {}, "DPO": {"V", ""}, "AI": {},
	},
	Journals: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"CP", "limit"}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"C", ""}, "Board": {}, "DPO": {}, "AI": {"", "Proposal"},
	},
	PayRuns: {
		"Owner": {"A", ""}, "Admin": {}, "Controller": {"M", "(rev 0.8) V if granted, FR-PAY-13"}, "Accountant": {"M", ""}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {"C", ""}, "Releaser": {"R", "salaries"}, "Approver": {}, "Employee": {"V", "own"}, "Auditor": {"M", ""}, "Tax agent": {"M", ""}, "Board": {}, "DPO": {"M", ""}, "AI": {"", "Only if enabled (AI-02)"},
	},
	FixedAssets: {
		"Owner": {"V", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"CP", ""}, "Bookkeeper": {"V", ""}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {}, "DPO": {}, "AI": {},
	},
	PeriodClose: {
		"Owner": {"A", ""}, "Admin": {}, "Controller": {"CA", ""}, "Accountant": {"C", ""}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {}, "DPO": {}, "AI": {},
	},
	GSTReturns: {
		"Owner": {"A", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"C", ""}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"C", ""}, "Board": {}, "DPO": {}, "AI": {"", "Ask only"},
	},
	Reports: {
		"Owner": {"V", ""}, "Admin": {"V", ""}, "Controller": {"V", ""}, "Accountant": {"V", ""}, "Bookkeeper": {"V", ""}, "AR": {"V", "sales"}, "AP": {"V", "purchases"}, "Payroll": {"V", "payroll"}, "Releaser": {"V", ""}, "Approver": {"V", "dept"}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {"V", "board pack"}, "DPO": {"V", ""}, "AI": {"V", ""},
	},
	Budgets: {
		"Owner": {"A", ""}, "Admin": {}, "Controller": {"A", ""}, "Accountant": {"C", ""}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {"C", "dept"}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {"V", ""}, "DPO": {}, "AI": {"", "Proposal (scenario)"},
	},
	AuditTrail: {
		"Owner": {"V", ""}, "Admin": {"V", ""}, "Controller": {"V", ""}, "Accountant": {"V", ""}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {"V", ""}, "Board": {}, "DPO": {"V", ""}, "AI": {},
	},
	ComplianceCenter: {
		"Owner": {"V", ""}, "Admin": {"V", ""}, "Controller": {"V", ""}, "Accountant": {"V", ""}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {}, "Board": {}, "DPO": {"CA", ""}, "AI": {},
	},
	TenantExport: {
		"Owner": {"A", "+ second approver"}, "Admin": {"C", ""}, "Controller": {}, "Accountant": {}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {}, "Tax agent": {}, "Board": {}, "DPO": {"V", ""}, "AI": {},
	},
	AIProviders: {
		"Owner": {"A", "+ second approver"}, "Admin": {"C", ""}, "Controller": {"V", ""}, "Accountant": {}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {}, "Board": {}, "DPO": {"V", ""}, "AI": {},
	},
	PeopleAndHR: {
		"Owner": {"A", "+ second approver"}, "Admin": {"C", ""}, "Controller": {"V", ""}, "Accountant": {"V", ""}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {"V", ""}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {}, "Board": {}, "DPO": {"V", ""}, "AI": {},
	},
	ConnectedApps: {
		"Owner": {"A", "+ second approver"}, "Admin": {"C", ""}, "Controller": {"V", ""}, "Accountant": {}, "Bookkeeper": {}, "AR": {}, "AP": {}, "Payroll": {}, "Releaser": {}, "Approver": {}, "Employee": {}, "Auditor": {"V", ""}, "Tax agent": {}, "Board": {}, "DPO": {"V", ""}, "AI": {},
	},
	OwnMCPConnection: {
		"Owner": {"C", ""}, "Admin": {"C", ""}, "Controller": {"C", ""}, "Accountant": {"C", ""}, "Bookkeeper": {"C", ""}, "AR": {"C", ""}, "AP": {"C", ""}, "Payroll": {"C", ""}, "Releaser": {"C", ""}, "Approver": {"C", ""}, "Employee": {}, "Auditor": {}, "Tax agent": {"C", ""}, "Board": {}, "DPO": {"C", ""}, "AI": {},
	},
}

// Policy is what one operation needs.
type Policy struct {
	Operation  string // the OpenAPI operationId
	Capability Capability
	Need       Right
	// MaskedFields are the response's JSON fields masked for a caller who holds only M.
	MaskedFields []string
	// Note is what the policy does not enforce yet and where it will, stated in the contract.
	Note string
	// Parent is the capability of the record the operation acts under, such as Journals for a journal's
	// evidence: the caller must also be able to view that record (object-level access), so a right on
	// evidence never reaches a record its holder cannot see.
	Parent Capability
	// OwnerMay lets the Owner run it although their cell holds no such right: FR-ADM-01's founder bootstrap
	// (rev 0.35). The decision says so (ByOwner), and the handler decides when it holds.
	OwnerMay bool
	// Public operations need no credential at all; NoOrganisation ones need a principal but no
	// organisation. Only the installation's operations (setup, Super admins) are either (a test keeps
	// it so).
	Public, NoOrganisation bool
	// SingleFactor operations are open to a session that passed no second factor yet: reading the session and
	// stepping it up with a passkey. Every other operation refuses such a session (SEC-01, rev 0.43).
	SingleFactor bool
	// Unqualified operations need the right without a qualifier: a cell such as Reports' "V sales" or "V dept"
	// grants it over part of the books only, which the operation does not narrow to (rev 0.52).
	Unqualified bool
}

// The policies of the P0 endpoints. A cell's qualifier is carried, not enforced: the amount limit
// of "C/P (limit)" and the segregation of duties and Founder-mode rules (SOD-3, FM-1) come from the
// approval engine (FR-APR, P1). AI cells without letters ("Proposal") grant nothing through the API:
// the AI's only write path is a proposal (golden rule 5), which arrives in P1.
var (
	GetOrganisation = Policy{Operation: "getOrganisation", Capability: Membership, Need: View}
	ListAccounts    = Policy{Operation: "listAccounts", Capability: ChartOfAccounts, Need: View}
	GetAccount      = Policy{Operation: "getAccount", Capability: ChartOfAccounts, Need: View}
	CreateAccount   = Policy{Operation: "createAccount", Capability: ChartOfAccounts, Need: Create,
		Note: "The Owner and the Controller approve chart changes; the approval itself arrives with the approval engine (P1)."}
	UpdateAccount = Policy{Operation: "updateAccount", Capability: ChartOfAccounts, Need: Create,
		Note: "The Owner and the Controller approve chart changes; the approval itself arrives with the approval engine (P1)."}
	SendTestEmail          = Policy{Operation: "sendTestEmail", Capability: Integrations, Need: Create}
	GetEmailMessage        = Policy{Operation: "getEmailMessage", Capability: Integrations, Need: View}
	ListMembers            = Policy{Operation: "listMembers", Capability: UsersAndRoles, Need: View}
	ListMemberInvitations  = Policy{Operation: "listMemberInvitations", Capability: UsersAndRoles, Need: View}
	CreateMemberInvitation = Policy{Operation: "createMemberInvitation", Capability: UsersAndRoles, Need: Create, OwnerMay: true,
		Note: "Role changes fall under FM-1: until a second member joins, the Owner acts alone (FR-ADM-01, rev 0.35); after that they wait for another Owner to approve them, and the request needs step-up (rev 0.36)."}
	WithdrawMemberInvitation = Policy{Operation: "withdrawMemberInvitation", Capability: UsersAndRoles, Need: Create, OwnerMay: true}
	AssignRole               = Policy{Operation: "assignRole", Capability: UsersAndRoles, Need: Create, OwnerMay: true,
		Note: "Role changes fall under FM-1: until a second member joins, the Owner acts alone (FR-ADM-01, rev 0.35); after that they wait for another Owner to approve them, and the request needs step-up (rev 0.36)."}
	RevokeRole = Policy{Operation: "revokeRole", Capability: UsersAndRoles, Need: Create, OwnerMay: true,
		Note: "Role changes fall under FM-1: until a second member joins, the Owner acts alone (FR-ADM-01, rev 0.35); after that they wait for another Owner to approve them, and the request needs step-up (rev 0.36)."}
	AcceptMemberInvitation = Policy{Operation: "acceptMemberInvitation", Capability: Joining, Need: Create, NoOrganisation: true,
		Note: "The invitation's code decides; a person signed in, never the AI or an OAuth client."}
	ListContacts      = Policy{Operation: "listContacts", Capability: ContactsAndItems, Need: View}
	GetContact        = Policy{Operation: "getContact", Capability: ContactsAndItems, Need: View}
	CreateContact     = Policy{Operation: "createContact", Capability: ContactsAndItems, Need: Create}
	UpdateContact     = Policy{Operation: "updateContact", Capability: ContactsAndItems, Need: Create}
	ListItems         = Policy{Operation: "listItems", Capability: ContactsAndItems, Need: View}
	GetItem           = Policy{Operation: "getItem", Capability: ContactsAndItems, Need: View}
	CreateItem        = Policy{Operation: "createItem", Capability: ContactsAndItems, Need: Create}
	UpdateItem        = Policy{Operation: "updateItem", Capability: ContactsAndItems, Need: Create}
	LoadStandardChart = Policy{Operation: "loadStandardChart", Capability: ChartOfAccounts, Need: Create,
		Note: "The Owner and the Controller approve chart changes; the approval itself arrives with the approval engine (P1)."}
	CreateJournal             = Policy{Operation: "createJournal", Capability: Journals, Need: Create}
	BulkCreateJournals        = Policy{Operation: "bulkCreateJournals", Capability: Journals, Need: Create}
	GetOrganisationProfile    = Policy{Operation: "getOrganisationProfile", Capability: OrganisationProfile, Need: View}
	UpdateOrganisationProfile = Policy{Operation: "updateOrganisationProfile", Capability: OrganisationProfile, Need: Create,
		Note: "The Owner approves changes to the profile; the approval itself arrives with the approval engine (P1)."}
	ListGSTRegistrations = Policy{Operation: "listGSTRegistrations", Capability: OrganisationProfile, Need: View}
	ListSettings         = Policy{Operation: "listSettings", Capability: OrganisationProfile, Need: View}
	GetSetting           = Policy{Operation: "getSetting", Capability: OrganisationProfile, Need: View}
	UpdateSetting        = Policy{Operation: "updateSetting", Capability: OrganisationProfile, Need: Create,
		Note: "A setting under dual control (FM-1) waits for another member who holds A to approve it: the request needs step-up (rev 0.36)."}
	ListSequences  = Policy{Operation: "listSequences", Capability: OrganisationProfile, Need: View}
	GetSequence    = Policy{Operation: "getSequence", Capability: OrganisationProfile, Need: View}
	UpdateSequence = Policy{Operation: "updateSequence", Capability: OrganisationProfile, Need: Create,
		Note: "The Owner approves changes to the defaults; the approval itself arrives with the approval engine (P1)."}
	GetGSTRegistration    = Policy{Operation: "getGSTRegistration", Capability: OrganisationProfile, Need: View}
	GetGSTStatus          = Policy{Operation: "getGSTStatus", Capability: OrganisationProfile, Need: View}
	CreateGSTRegistration = Policy{Operation: "createGSTRegistration", Capability: OrganisationProfile, Need: Create,
		Note: "The Owner approves GST registration changes; the approval itself arrives with the approval engine (P1)."}
	UpdateGSTRegistration = Policy{Operation: "updateGSTRegistration", Capability: OrganisationProfile, Need: Create,
		Note: "The Owner approves GST registration changes; the approval itself arrives with the approval engine (P1)."}
	GetJournal   = Policy{Operation: "getJournal", Capability: Journals, Need: View}
	ListJournals = Policy{Operation: "listJournals", Capability: Journals, Need: View}
	// The accounting framework and policies (FR-ORG-10; the founders' decision of rev 0.62): the Owner, the Controller and the
	// Accountant record them (C); a member who holds the Accountant role confirms or approves each version (A), never its
	// writer, which the domain and the database refuse.
	GetAccounting              = Policy{Operation: "getAccounting", Capability: AccountingBasis, Need: View}
	RecordAccountingFramework  = Policy{Operation: "recordAccountingFramework", Capability: AccountingBasis, Need: Create}
	ConfirmAccountingFramework = Policy{Operation: "confirmAccountingFramework", Capability: AccountingBasis, Need: Approve}
	RecordAccountingPolicy     = Policy{Operation: "recordAccountingPolicy", Capability: AccountingBasis, Need: Create}
	ApproveAccountingPolicy    = Policy{Operation: "approveAccountingPolicy", Capability: AccountingBasis, Need: Approve}
	// The automatic reversals that wait are a view of the journals (FR-GL-02; rev 0.58).
	ListWaitingReversals = Policy{Operation: "listWaitingReversals", Capability: Journals, Need: View}
	// Recurring journals are read as journals are; keeping one is preparing the drafts it makes (C), which a person posts
	// under the usual approvals (FR-GL-02; PRD-001 rev 0.58, rev 0.60).
	ListRecurringJournals   = Policy{Operation: "listRecurringJournals", Capability: Journals, Need: View}
	GetRecurringJournal     = Policy{Operation: "getRecurringJournal", Capability: Journals, Need: View}
	CreateRecurringJournal  = Policy{Operation: "createRecurringJournal", Capability: Journals, Need: Create}
	ReplaceRecurringJournal = Policy{Operation: "replaceRecurringJournal", Capability: Journals, Need: Create}
	PauseRecurringJournal   = Policy{Operation: "pauseRecurringJournal", Capability: Journals, Need: Create}
	ResumeRecurringJournal  = Policy{Operation: "resumeRecurringJournal", Capability: Journals, Need: Create}
	// The opening balances are kept as a journal is: prepared under C on Journals and posted under P (FR-GL-06; PRD-001 rev
	// 0.49, provisional). Changing them once posted is high-risk (FM-1) and arrives with a second person's approval.
	GetOpeningBalances     = Policy{Operation: "getOpeningBalances", Capability: Journals, Need: View}
	ReplaceOpeningBalances = Policy{Operation: "replaceOpeningBalances", Capability: Journals, Need: Create}
	PostOpeningBalances    = Policy{Operation: "postOpeningBalances", Capability: Journals, Need: Post}
	// Changing posted opening balances is a high-risk action in every team mode (FM-1; rev 0.66): whoever posts journals
	// asks, and another person who holds A on Journals approves it through the approval engine.
	RequestOpeningBalancesEdit = Policy{Operation: "requestOpeningBalancesEdit", Capability: Journals, Need: Post,
		Note: "A person stepped up in the last 5 minutes; the approval engine has another person who holds A approve it (FM-1)."}
	// Asking for approval to post or to void a journal is what posting it would be (P); another person who holds A approves
	// it through the approval engine (FM-1, FM-2, SOD-3; rev 0.53).
	RequestJournalPost     = Policy{Operation: "requestJournalPost", Capability: Journals, Need: Post}
	RequestJournalReversal = Policy{Operation: "requestJournalReversal", Capability: Journals, Need: Post}
	// Editing a posted journal posts its reversal and its new version, which is posting (P); asking for approval to edit
	// it is the same right (FR-GL-12, FM-1; rev 0.54).
	EditJournal        = Policy{Operation: "editJournal", Capability: Journals, Need: Post}
	RequestJournalEdit = Policy{Operation: "requestJournalEdit", Capability: Journals, Need: Post}
	// Changing a draft is preparing it, as saving one is (FR-GL-01).
	ReplaceJournal = Policy{Operation: "replaceJournal", Capability: Journals, Need: Create}
	// Periods are read by whoever holds a right on the close (FR-ORG-02, FR-GL-08); opening a year is preparing the
	// close's calendar, so it needs C (the Accountant and the Controller; PRD-001 rev 0.47, provisional).
	ListPeriods       = Policy{Operation: "listPeriods", Capability: PeriodClose, Need: View}
	GetPeriod         = Policy{Operation: "getPeriod", Capability: PeriodClose, Need: View}
	OpenFinancialYear = Policy{Operation: "openFinancialYear", Capability: PeriodClose, Need: Create}
	// The close is two acts by two people (FR-GL-07, FM-4; rev 0.50): the preparer runs the checks under C, another
	// person attests and closes under A, never the AI.
	// The general ledger report reads every line of the accounts asked for, so it needs an unqualified view of the
	// reports (rev 0.52).
	GetGeneralLedger = Policy{Operation: "getGeneralLedger", Capability: Reports, Need: View, Unqualified: true,
		Note: "A view of the reports limited to sales, purchases, payroll, a department or the board pack is refused."}
	GetPeriodClose = Policy{Operation: "getPeriodClose", Capability: PeriodClose, Need: View}
	RunCloseChecks = Policy{Operation: "runCloseChecks", Capability: PeriodClose, Need: Create}
	ClosePeriod    = Policy{Operation: "closePeriod", Capability: PeriodClose, Need: Approve,
		Note: "A person stepped up in the last 5 minutes, who did not run the checks (FM-4)."}
	// Flux analysis (FR-GL-14; rev 0.65): the variances are read with the close (V); their explanations are written by
	// the members who run the checks (C), never the AI, whose draft waits for the AI catalogue.
	ListPeriodVariances = Policy{Operation: "listPeriodVariances", Capability: PeriodClose, Need: View}
	ExplainVariance     = Policy{Operation: "explainVariance", Capability: PeriodClose, Need: Create}
	// The close checklist (FR-GL-07; the founders' decisions of 22 Oct 2026, rev 0.68): read with the close (V); its
	// template is changed, and its tasks marked, by the members who run the checks (C: the Accountant, the Controller).
	GetCloseChecklistTemplate     = Policy{Operation: "getCloseChecklistTemplate", Capability: PeriodClose, Need: View}
	ReplaceCloseChecklistTemplate = Policy{Operation: "replaceCloseChecklistTemplate", Capability: PeriodClose, Need: Create}
	GetPeriodChecklist            = Policy{Operation: "getPeriodChecklist", Capability: PeriodClose, Need: View}
	MarkChecklistTask             = Policy{Operation: "markChecklistTask", Capability: PeriodClose, Need: Create}
	// Year end (FR-GL-10; rev 0.69) is read with the close (V): it is computed, so there is nothing to write.
	GetYearEnd = Policy{Operation: "getYearEnd", Capability: PeriodClose, Need: View}
	// Exchange rates (FR-GL-05, ENG-40; rev 0.70) are month-end work: read with the close (V), entered and withdrawn by the
	// members who run its checks (C: the Accountant, the Controller). Postings take them through the cascade, whoever posts.
	ListFxRates       = Policy{Operation: "listFxRates", Capability: PeriodClose, Need: View}
	GetFxRate         = Policy{Operation: "getFxRate", Capability: PeriodClose, Need: View}
	ListFeedRates     = Policy{Operation: "listFeedRates", Capability: PeriodClose, Need: View}
	LookupFxRate      = Policy{Operation: "lookupFxRate", Capability: PeriodClose, Need: View}
	CreateFxRate      = Policy{Operation: "createFxRate", Capability: PeriodClose, Need: Create}
	BulkCreateFxRates = Policy{Operation: "bulkCreateFxRates", Capability: PeriodClose, Need: Create}
	WithdrawFxRate    = Policy{Operation: "withdrawFxRate", Capability: PeriodClose, Need: Create}
	// The month-end revaluation (FR-GL-04; rev 0.73) is close work too: previewed with the close (V), run by the members
	// who run its checks (C).
	GetFxRevaluation  = Policy{Operation: "getFxRevaluation", Capability: PeriodClose, Need: View}
	PostFxRevaluation = Policy{Operation: "postFxRevaluation", Capability: PeriodClose, Need: Create}
	// The money accounts' balances in other currencies (ENG-43; rev 0.74) are read with the rates (V).
	ListForeignMoneyAccounts = Policy{Operation: "listForeignMoneyAccounts", Capability: PeriodClose, Need: View}
	// Asking to reopen is preparing it (C); a second person who holds A approves it through the approval engine (FM-1).
	RequestPeriodReopen = Policy{Operation: "requestPeriodReopen", Capability: PeriodClose, Need: Create,
		Note: "A person stepped up in the last 5 minutes; the approval engine has another person who holds A approve it (FM-1)."}
	PostJournal = Policy{Operation: "postJournal", Capability: Journals, Need: Post,
		Note: "The Accountant's amount limit and SOD-3 arrive with the approval engine (P1)."}
	ReverseJournal = Policy{Operation: "reverseJournal", Capability: Journals, Need: Post,
		Note: "A reversal voids a posted journal; FM-1's second person arrives with the approval engine (P1)."}
	// Bills (FR-AP-02; p1-purchases): read under V, prepared under C (the Bookkeeper, the AP specialist, the Accountant),
	// posted and voided under P (the Accountant), as journals are; asking for approval to post or void is posting (P),
	// and another person who holds A approves it through the approval engine (FM-1, FM-2, SOD-3).
	ListBills       = Policy{Operation: "listBills", Capability: Bills, Need: View}
	GetBill         = Policy{Operation: "getBill", Capability: Bills, Need: View}
	CreateBill      = Policy{Operation: "createBill", Capability: Bills, Need: Create}
	ReplaceBill     = Policy{Operation: "replaceBill", Capability: Bills, Need: Create}
	PostBill        = Policy{Operation: "postBill", Capability: Bills, Need: Post}
	VoidBill        = Policy{Operation: "voidBill", Capability: Bills, Need: Post}
	RequestBillPost = Policy{Operation: "requestBillPost", Capability: Bills, Need: Post}
	RequestBillVoid = Policy{Operation: "requestBillVoid", Capability: Bills, Need: Post}
	EditBill        = Policy{Operation: "editBill", Capability: Bills, Need: Post}
	// The GST codes a bill's line takes are read with the bills (V).
	GetPurchaseGSTCodes = Policy{Operation: "getPurchaseGSTCodes", Capability: Bills, Need: View}
	RequestBillEdit     = Policy{Operation: "requestBillEdit", Capability: Bills, Need: Post}
	// Payments of bills recorded by hand (FR-AP-02; the owner's decision of 22 Oct 2026): the money has left already, so
	// recording one posts (P on "Bills, vendor credits"), as posting a bill does; releasing money is "Payment batches",
	// which arrives with bank import. Voiding one is FM-1's void of a posted document: posting (P), and in Founder
	// mode asked for and approved by another person who holds A.
	ListBillPayments       = Policy{Operation: "listBillPayments", Capability: Bills, Need: View}
	GetBillPayment         = Policy{Operation: "getBillPayment", Capability: Bills, Need: View}
	RecordBillPayment      = Policy{Operation: "recordBillPayment", Capability: Bills, Need: Post}
	VoidBillPayment        = Policy{Operation: "voidBillPayment", Capability: Bills, Need: Post}
	RequestBillPaymentVoid = Policy{Operation: "requestBillPaymentVoid", Capability: Bills, Need: Post}
	// AP ageing and the bills to pay (FR-AP-06) are read under V on "Bills, vendor credits", as the bills are.
	GetPayables = Policy{Operation: "getPayables", Capability: Bills, Need: View}
	// Applying a supplier credit to a bill (FR-AP-03, FR-AR-18) may post the realised FX, so it is posting (P); voiding one is
	// FM-1's void of a posted document, asked for and approved by another person who holds A in Founder mode.
	GetCreditApplication         = Policy{Operation: "getCreditApplication", Capability: Bills, Need: View}
	ApplyCredit                  = Policy{Operation: "applyCredit", Capability: Bills, Need: Post}
	VoidCreditApplication        = Policy{Operation: "voidCreditApplication", Capability: Bills, Need: Post}
	RequestCreditApplicationVoid = Policy{Operation: "requestCreditApplicationVoid", Capability: Bills, Need: Post}
	// Recording a refund from a supplier (FR-AP-03) posts (P), as recording a payment does; voiding one is FM-1's void.
	GetSupplierRefund         = Policy{Operation: "getSupplierRefund", Capability: Bills, Need: View}
	RecordSupplierRefund      = Policy{Operation: "recordSupplierRefund", Capability: Bills, Need: Post}
	VoidSupplierRefund        = Policy{Operation: "voidSupplierRefund", Capability: Bills, Need: Post}
	RequestSupplierRefundVoid = Policy{Operation: "requestSupplierRefundVoid", Capability: Bills, Need: Post}
	// A journal's evidence needs the evidence right and the right to view the journal (FR-DOC-05, FR-DOC-11).
	// The AI's cell is a proposal (doc.attach): an AI or MCP client never uploads, lists or reads evidence here.
	AddJournalEvidence = Policy{Operation: "addJournalEvidence", Capability: Evidence, Need: Create, Parent: Journals,
		Note: "Needs the right to view the journal as well. AI and MCP clients only propose attaching a document (doc.attach), " +
			"which a person approves; the evidence rules per activity (FR-DOC-06) arrive later in P1."}
	ListJournalEvidence = Policy{Operation: "listJournalEvidence", Capability: Evidence, Need: View, Parent: Journals,
		Note: "Needs the right to view the journal as well."}
	GetJournalEvidence = Policy{Operation: "getJournalEvidence", Capability: Evidence, Need: View, Parent: Journals,
		Note: "Needs the right to view the journal as well."}
	RemoveJournalEvidence = Policy{Operation: "removeJournalEvidence", Capability: Evidence, Need: Create, Parent: Journals,
		Note: "Needs the right to view the journal as well; only while the journal is a draft (FR-DOC-07)."}
)

const approvalNote = "Each approval's own policy decides: the Appendix D row of the object it changes. Never the AI (§9.6)."

var (
	ListApprovals = Policy{Operation: "listApprovals", Capability: Approvals, Need: View,
		Note: approvalNote + " Lists the requests the caller made or holds a right on the object of."}
	GetApproval = Policy{Operation: "getApproval", Capability: Approvals, Need: View,
		Note: approvalNote + " Shows a request to its requester and to whoever holds a right on its object."}
	ApproveApproval = Policy{Operation: "approveApproval", Capability: Approvals, Need: Approve,
		Note: approvalNote + " Needs A on the object's row, a person signed in who passed step-up in the last 5 minutes, and someone other than the requester (FM-1)."}
	RejectApproval = Policy{Operation: "rejectApproval", Capability: Approvals, Need: Approve,
		Note: approvalNote + " Needs A on the object's row, a person signed in who passed step-up in the last 5 minutes, and someone other than the requester."}
	ListReviewLog = Policy{Operation: "listReviewLog", Capability: Approvals, Need: View,
		Note: approvalNote + " Lists the high-risk actions the caller may see, by the row of what each changed (FM-3)."}
	ReviewLogEntries = Policy{Operation: "reviewLogEntries", Capability: Approvals, Need: Approve,
		Note: approvalNote + " Needs A on the row of what each entry changed, a person signed in, and never the one who acted (FM-3)."}
	ChallengeApproval = Policy{Operation: "challengeApproval", Capability: Approvals, Need: Approve,
		Note: approvalNote + " A challenge for signing the request with a passkey: needs A on the object's row and a person signed in to Fyniq in the browser (FR-APR-03)."}
	WithdrawApproval = Policy{Operation: "withdrawApproval", Capability: Approvals, Need: View,
		Note: approvalNote + " Only the person who asked for the change withdraws it, signed in to Fyniq (rev 0.40)."}
)

var (
	ListApprovalPolicies  = Policy{Operation: "listApprovalPolicies", Capability: ApprovalPolicies, Need: View}
	GetApprovalPolicy     = Policy{Operation: "getApprovalPolicy", Capability: ApprovalPolicies, Need: View}
	ListStatutoryPacks    = Policy{Operation: "listStatutoryPacks", Capability: StatutoryParameters, Need: View}
	GetStatutoryPack      = Policy{Operation: "getStatutoryPack", Capability: StatutoryParameters, Need: View}
	ActivateStatutoryPack = Policy{Operation: "activateStatutoryPack", Capability: StatutoryParameters, Need: Create,
		Note: "Always waits for approval by a member who holds A on the row (the Owner or the Controller), never the proposer (ENG-32, SOD-10); " +
			"needs a person signed in who passed step-up in the last 5 minutes."}
	GetTeamMode    = Policy{Operation: "getTeamMode", Capability: UsersAndRoles, Need: View}
	SwitchTeamMode = Policy{Operation: "switchTeamMode", Capability: UsersAndRoles, Need: Create,
		Note: "Always waits for approval by a member who holds A on the row (an Owner), never the requester (§6.4, FR-ADM-04); " +
			"needs a person signed in who passed step-up in the last 5 minutes."}
	ChangeApprovalPolicy = Policy{Operation: "changeApprovalPolicy", Capability: ApprovalPolicies, Need: Create,
		Note: "Always waits for approval by a member who holds A on the row (the Owner or the Controller), never the requester (FR-APR-09, SOD-8); " +
			"needs a person signed in who passed step-up in the last 5 minutes."}
)

var (
	GetSetupStatus = Policy{Operation: "getSetupStatus", Capability: Setup, Need: View, Public: true}
	CompleteSetup  = Policy{Operation: "completeSetup", Capability: Setup, Need: Create, NoOrganisation: true,
		Note: "Needs the one-time setup code and a first-party sign-in; an OAuth client or the AI is refused."}
)

const superAdminNote = "Needs an active Super admin signed in to Fyniq who passed step-up in the last 5 minutes; never an OAuth client or the AI. " +
	"P0 has at most two Super admins; dual control of Super admin changes arrives with the second-person approvals (FR-ADM-07, P1)."

var (
	CreateSuperAdminInvitation = Policy{Operation: "createSuperAdminInvitation", Capability: SuperAdmins, Need: Create, NoOrganisation: true,
		Note: superAdminNote}
	AcceptSuperAdminInvitation = Policy{Operation: "acceptSuperAdminInvitation", Capability: SuperAdmins, Need: Create, NoOrganisation: true,
		Note: "Needs a valid invitation and a person signed in to Fyniq; an OAuth client or the AI is refused."}
	ReplaceRecoveryCodes = Policy{Operation: "replaceSuperAdminRecoveryCodes", Capability: SuperAdmins, Need: Create, NoOrganisation: true,
		Note: superAdminNote}
	ConfirmSuperAdminRecovery = Policy{Operation: "confirmSuperAdminRecovery", Capability: SuperAdmins, Need: Approve, NoOrganisation: true,
		Note: superAdminNote + " The other Super admin confirms; nobody confirms their own recovery."}
	RecoverSuperAdmin = Policy{Operation: "recoverSuperAdmin", Capability: SuperAdmins, Need: Create, NoOrganisation: true, SingleFactor: true,
		Note: "Needs two proofs (SEC-35): a recovery code with the other Super admin's confirmation, or the deployment's two-person recovery; " +
			"and a person signed in to Fyniq, who may have lost their second factor: the two proofs stand in for it. An OAuth client or the AI is refused."}
)

var (
	ListSignInProviders = Policy{Operation: "listSignInProviders", Capability: Session, Need: View, Public: true}
	GetSession          = Policy{Operation: "getSession", Capability: Session, Need: View, NoOrganisation: true, SingleFactor: true,
		Note: "Needs a person signed in to Fyniq; an OAuth client or the AI is refused."}
)

const passkeyNote = "Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-02, SEC-01)."

const deviceNote = "Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused (FR-ADM-03)."

var (
	ListDevices         = Policy{Operation: "listDevices", Capability: Session, Need: View, NoOrganisation: true, Note: deviceNote}
	SignOutDevice       = Policy{Operation: "signOutDevice", Capability: Session, Need: Create, NoOrganisation: true, Note: deviceNote}
	SignOutOtherDevices = Policy{Operation: "signOutOtherDevices", Capability: Session, Need: Create, NoOrganisation: true, Note: deviceNote}
)

var (
	ListPasskeys             = Policy{Operation: "listPasskeys", Capability: Session, Need: View, NoOrganisation: true, Note: passkeyNote}
	StartPasskeyRegistration = Policy{Operation: "startPasskeyRegistration", Capability: Session, Need: Create, NoOrganisation: true,
		Note: passkeyNote + " The session passed a second factor in the last 5 minutes."}
	RegisterPasskey = Policy{Operation: "registerPasskey", Capability: Session, Need: Create, NoOrganisation: true,
		Note: passkeyNote + " The session passed a second factor in the last 5 minutes."}
	RevokePasskey = Policy{Operation: "revokePasskey", Capability: Session, Need: Create, NoOrganisation: true,
		Note: passkeyNote + " The session passed a second factor in the last 5 minutes."}
	StartStepUp = Policy{Operation: "startStepUp", Capability: Session, Need: Create, NoOrganisation: true, SingleFactor: true,
		Note: passkeyNote + " Open to a session that passed no second factor yet: this is how it passes one."}
	StepUp = Policy{Operation: "stepUp", Capability: Session, Need: Create, NoOrganisation: true, SingleFactor: true,
		Note: passkeyNote + " Open to a session that passed no second factor yet: this is how it passes one."}
)

// Policies are every policy, by operation. An operation without one is refused.
var Policies = index(GetOrganisation, GetOrganisationProfile, UpdateOrganisationProfile, ListGSTRegistrations, GetGSTRegistration,
	GetGSTStatus, CreateGSTRegistration, UpdateGSTRegistration, ListSequences, GetSequence, UpdateSequence, ListSettings, GetSetting, UpdateSetting, ListAccounts, LoadStandardChart, CreateAccount, UpdateAccount, ListContacts, GetContact,
	CreateContact, UpdateContact, ListMembers, ListMemberInvitations, CreateMemberInvitation, WithdrawMemberInvitation, AssignRole,
	RevokeRole, AcceptMemberInvitation, SendTestEmail, GetEmailMessage, ListItems, GetItem, CreateItem, UpdateItem, GetAccount, CreateJournal, BulkCreateJournals, GetJournal, ListJournals, ListWaitingReversals, GetAccounting, RecordAccountingFramework, ConfirmAccountingFramework, RecordAccountingPolicy, ApproveAccountingPolicy, ListRecurringJournals, GetRecurringJournal, CreateRecurringJournal, ReplaceRecurringJournal, PauseRecurringJournal, ResumeRecurringJournal, ReplaceJournal, PostJournal, ReverseJournal, RequestJournalPost, RequestJournalReversal, EditJournal, RequestJournalEdit,
	GetOpeningBalances, ReplaceOpeningBalances, PostOpeningBalances, RequestOpeningBalancesEdit,
	ListPeriods, GetPeriod, OpenFinancialYear, GetGeneralLedger, GetPeriodClose, RunCloseChecks, ClosePeriod, RequestPeriodReopen,
	ListPeriodVariances, ExplainVariance, GetCloseChecklistTemplate, ReplaceCloseChecklistTemplate, GetPeriodChecklist, MarkChecklistTask, GetYearEnd,
	ListFxRates, GetFxRate, ListFeedRates, LookupFxRate, CreateFxRate, BulkCreateFxRates, WithdrawFxRate, GetFxRevaluation, PostFxRevaluation, ListForeignMoneyAccounts,
	AddJournalEvidence, ListJournalEvidence, GetJournalEvidence, RemoveJournalEvidence,
	ListBills, GetBill, CreateBill, ReplaceBill, PostBill, VoidBill, RequestBillPost, RequestBillVoid, EditBill, RequestBillEdit, GetPurchaseGSTCodes,
	ListBillPayments, GetBillPayment, RecordBillPayment, VoidBillPayment, RequestBillPaymentVoid, GetPayables,
	GetCreditApplication, ApplyCredit, VoidCreditApplication, RequestCreditApplicationVoid,
	GetSupplierRefund, RecordSupplierRefund, VoidSupplierRefund, RequestSupplierRefundVoid,
	GetSetupStatus, CompleteSetup, CreateSuperAdminInvitation, AcceptSuperAdminInvitation, ReplaceRecoveryCodes,
	ConfirmSuperAdminRecovery, RecoverSuperAdmin, ListSignInProviders, GetSession, ListApprovals, GetApproval, ApproveApproval, RejectApproval,
	WithdrawApproval, ChallengeApproval, ListReviewLog, ReviewLogEntries, ListApprovalPolicies, GetApprovalPolicy, ChangeApprovalPolicy, ListStatutoryPacks, GetStatutoryPack, ActivateStatutoryPack,
	GetTeamMode, SwitchTeamMode, ListPasskeys, StartPasskeyRegistration, RegisterPasskey, RevokePasskey, StartStepUp, StepUp,
	ListDevices, SignOutDevice, SignOutOtherDevices)

func index(ps ...Policy) map[string]Policy {
	out := make(map[string]Policy, len(ps))
	for _, p := range ps {
		if _, dup := out[p.Operation]; dup {
			panic("authz: two policies for " + p.Operation)
		}
		out[p.Operation] = p
	}
	return out
}

// Decision is the outcome of an authorisation check.
type Decision struct {
	Allowed bool
	Masked  []string // fields to mask in the response
	Right   Right    // the right that allowed it
	Reason  string   // why it was refused, for the security log
	// ByOwner is set when only the policy's OwnerMay allowed it: the handler checks the founder bootstrap.
	ByOwner bool
}

// Decide checks whether a caller holding roles may run the operation of p. When ai is true the
// caller is the AI acting for a user holding roles: it gets the lesser of the user's right and the
// AI column's.
func Decide(p Policy, roles []Role, ai bool) Decision {
	if p.Operation == "" {
		return Decision{Reason: "the operation has no policy"}
	}
	if p.Capability == Setup || p.Capability == SuperAdmins || p.Capability == Session || p.Capability == Joining { // the codes, the database and a person's sign-in decide
		if ai {
			return Decision{Reason: "the AI cannot set up the installation, change its Super admins or hold a session"}
		}
		return Decision{Allowed: true, Right: p.Need}
	}
	if p.Capability == Approvals {
		if ai {
			return Decision{Reason: "the AI never sees, approves or rejects an approval (§9.6)"}
		}
		for _, row := range ApprovalRows {
			q := p
			q.Capability = row
			if r, ok := holds(q, roles); ok {
				return Decision{Allowed: true, Right: r}
			}
		}
		return Decision{Reason: fmt.Sprintf("these roles hold no %q right on the rows approvals cover", string(p.Need))}
	}
	best, ok := holds(p, roles)
	if ok && ai {
		var aiRight Right
		if aiRight, ok = holds(p, []Role{AI}); ok && rank(aiRight) < rank(best) {
			best = aiRight
		}
	}
	if ok && p.Parent != "" { // the record it acts under must be visible too
		parent := Policy{Operation: p.Operation, Capability: p.Parent, Need: View}
		var seen Right
		if seen, ok = holds(parent, roles); ok && ai {
			_, ok = holds(parent, []Role{AI})
		}
		if ok && seen == Masked {
			ok = false // a masked view of the record is not enough to act on what is under it
		}
		if !ok {
			return Decision{Reason: fmt.Sprintf("these roles cannot view the %q record it is under", p.Parent)}
		}
	}
	if !ok && p.OwnerMay && !ai && slices.Contains(roles, Owner) {
		return Decision{Allowed: true, Right: p.Need, ByOwner: true}
	}
	if !ok {
		who := "these roles"
		if ai {
			who = "the AI acting for these roles"
		}
		return Decision{Reason: fmt.Sprintf("%s hold no %q right on %q", who, string(p.Need), p.Capability)}
	}
	d := Decision{Allowed: true, Right: best}
	if best == Masked {
		d.Masked = slices.Clone(p.MaskedFields)
	}
	return d
}

// rank orders the ways of satisfying View: a full view is better than a masked one.
func rank(r Right) int {
	if r == Masked {
		return 0
	}
	return 1
}

// holds returns the best right among roles that satisfies p.Need.
func holds(p Policy, roles []Role) (Right, bool) {
	var best Right
	found := false
	for _, r := range roles {
		col := Column(r)
		if col == "" {
			continue
		}
		var rights string
		switch cell := Grants[p.Capability][col]; {
		case p.Capability == Membership:
			rights = "V"
		case p.Unqualified && cell.Qualifier != "":
			continue
		default:
			rights = cell.Rights
		}
		for i := 0; i < len(rights); i++ {
			got, ok := satisfies(Right(rights[i]), p.Need)
			if ok && (!found || rank(got) > rank(best)) {
				best, found = got, true
			}
		}
	}
	return best, found
}

// satisfies reports whether holding have meets need, and as which right.
func satisfies(have, need Right) (Right, bool) {
	if need == View {
		if have == Masked {
			return Masked, true
		}
		return View, strings.ContainsRune("CAPRV", rune(have))
	}
	return have, have == need
}
