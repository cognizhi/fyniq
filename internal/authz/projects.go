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
