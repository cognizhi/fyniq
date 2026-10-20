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
var Rows = []Capability{UsersAndRoles, ApprovalPolicies, OrganisationProfile, StatutoryParameters, ChartOfAccounts, ContactsAndItems, BankTransactions, BankReconciliation, Invoices, Bills, VendorBankDetails, PaymentBatches, ExpenseClaims, DirectorAccounts, CapTable, Evidence, KnowledgeBase, AICompanySecretary, Integrations, Announcements, OwnNotifications, DocumentTemplates, Timesheets, ProjectReports, BankConnections, Journals, PayRuns, FixedAssets, PeriodClose, GSTReturns, Reports, Budgets, AuditTrail, ComplianceCenter, TenantExport, AIProviders, PeopleAndHR, ConnectedApps, OwnMCPConnection}

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
// activating statutory pack versions (rev 0.41).
var ApprovalRows = []Capability{OrganisationProfile, UsersAndRoles, ApprovalPolicies, StatutoryParameters}

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
	GetJournal  = Policy{Operation: "getJournal", Capability: Journals, Need: View}
	PostJournal = Policy{Operation: "postJournal", Capability: Journals, Need: Post,
		Note: "The Accountant's amount limit and SOD-3 arrive with the approval engine (P1)."}
	ReverseJournal = Policy{Operation: "reverseJournal", Capability: Journals, Need: Post,
		Note: "A reversal voids a posted journal; FM-1's second person arrives with the approval engine (P1)."}
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
	RevokeRole, AcceptMemberInvitation, SendTestEmail, GetEmailMessage, ListItems, GetItem, CreateItem, UpdateItem, GetAccount, CreateJournal, BulkCreateJournals, GetJournal, PostJournal, ReverseJournal,
	AddJournalEvidence, ListJournalEvidence, GetJournalEvidence, RemoveJournalEvidence,
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
		if p.Capability == Membership {
			rights = "V"
		} else {
			rights = Grants[p.Capability][col].Rights
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
