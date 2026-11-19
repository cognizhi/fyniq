package authz

// Projects and time (FR-PRJ-01, FR-PRJ-05, FR-PRJ-07; p1-projects). A project is a value of the Project dimension, so its
// record is kept under the dimensions' row, "Chart of accounts, dimensions" (C), and read under "Timesheets and project
// time": everyone who holds a right there sees the projects, an Employee's "C (own)" and the Approver's "A (project
// manager)" over the projects they are a member of, which the handlers narrow to (design D4). Naming a manager gives no
// right: the Approver or Controller role does.
var (
	ListProjects   = Policy{Operation: "listProjects", Capability: Timesheets, Need: View}
	GetProject     = Policy{Operation: "getProject", Capability: Timesheets, Need: View}
	CreateProject  = Policy{Operation: "createProject", Capability: ChartOfAccounts, Need: Create}
	ReplaceProject = Policy{Operation: "replaceProject", Capability: ChartOfAccounts, Need: Create}
	// ListProjectPeople are the members a project can name, for those who keep projects: "Users, roles, team mode" is not
	// theirs to read.
	ListProjectPeople  = Policy{Operation: "listProjectPeople", Capability: ChartOfAccounts, Need: Create}
	ListProjectTasks   = Policy{Operation: "listProjectTasks", Capability: Timesheets, Need: View}
	CreateProjectTask  = Policy{Operation: "createProjectTask", Capability: ChartOfAccounts, Need: Create}
	ReplaceProjectTask = Policy{Operation: "replaceProjectTask", Capability: ChartOfAccounts, Need: Create}
)

// Time (FR-PRJ-02, FR-PRJ-05, FR-PRJ-07): a person's own week, timer and corrections are the Employee's "C (own)"; the
// submissions are read by those who hold a right on "Timesheets and project time", each narrowed to what they may see, and
// decided by A (the Approver for the projects they manage, the Controller for any, never their own: design D4).
var (
	GetTimesheet               = Policy{Operation: "getTimesheet", Capability: Timesheets, Need: View}
	SaveTimesheet              = Policy{Operation: "saveTimesheet", Capability: Timesheets, Need: Create}
	SubmitTimesheet            = Policy{Operation: "submitTimesheet", Capability: Timesheets, Need: Create}
	GetTimer                   = Policy{Operation: "getTimer", Capability: Timesheets, Need: View}
	StartTimer                 = Policy{Operation: "startTimer", Capability: Timesheets, Need: Create}
	StopTimer                  = Policy{Operation: "stopTimer", Capability: Timesheets, Need: Create}
	DropTimer                  = Policy{Operation: "dropTimer", Capability: Timesheets, Need: Create}
	ReverseTimeEntry           = Policy{Operation: "reverseTimeEntry", Capability: Timesheets, Need: Create}
	ListTimesheetSubmissions   = Policy{Operation: "listTimesheetSubmissions", Capability: Timesheets, Need: View}
	GetTimesheetSubmission     = Policy{Operation: "getTimesheetSubmission", Capability: Timesheets, Need: View}
	ApproveTimesheetSubmission = Policy{Operation: "approveTimesheetSubmission", Capability: Timesheets, Need: Approve}
	ReturnTimesheetSubmission  = Policy{Operation: "returnTimesheetSubmission", Capability: Timesheets, Need: Approve}
)

// Rates (FR-PRJ-06; decisions A and C, 19 Nov 2026): a person's cost rate is payroll data, entered and read by the Owner and the
// Payroll administrator only, through its own row; billing rates are kept by those who keep projects and read by those who read
// projects; time at cost is read under "Project reports and cost rates", at the viewer's tier (design D5).
var (
	ListCostRates      = Policy{Operation: "listCostRates", Capability: ProjectCostRates, Need: View}
	SetCostRate        = Policy{Operation: "setCostRate", Capability: ProjectCostRates, Need: Create}
	ListBillingRates   = Policy{Operation: "listBillingRates", Capability: Timesheets, Need: View}
	SetBillingRate     = Policy{Operation: "setBillingRate", Capability: ChartOfAccounts, Need: Create}
	GetProjectTimeCost = Policy{Operation: "getProjectTimeCost", Capability: ProjectReports, Need: View}
)

// Project reports (FR-PRJ-12): profitability per project, utilisation and realisation, read under "Project reports and cost rates"
// at the viewer's tier (design D5, D6), with the entries behind a figure.
var (
	GetProjectReport       = Policy{Operation: "getProjectReport", Capability: ProjectReports, Need: View}
	ListProjectTimeEntries = Policy{Operation: "listProjectTimeEntries", Capability: ProjectReports, Need: View}
)

// Billing a project's work (FR-PRJ-10): the unbilled view is read under "Project reports and cost rates" and shows hours and billing rates,
// never a cost rate; the draft invoice is made through the ordinary invoice command, so it needs the right to create invoices as well.
var (
	GetProjectUnbilled  = Policy{Operation: "getProjectUnbilled", Capability: ProjectReports, Need: View}
	DraftProjectInvoice = Policy{Operation: "draftProjectInvoice", Capability: Invoices, Need: Create, Parent: ProjectReports}
)
