// Package authz is authorisation in the API (SEC-02): one policy table maps every operation to the
// capability of PRD-001 Appendix D it belongs to, the right it needs and the fields it masks. The
// handler wrapper refuses an operation without a policy, and the matrix test (QA-SEC-03) is
// generated from the same table, checked against Appendix D as the PRD states it. Row-level security
// in the database is the second, independent half (§11.4).
package authz

// Role is a role a person holds in an organisation (§7.1, §6.2), or one of the two principals the
// matrix also names: the AI acting for a user, and the Super admin of the installation.
type Role string

// The roles of §7.1 ("What each role sees"), in its order.
const (
	Employee         Role = "employee"
	LineManager      Role = "line_manager" // a relationship (§6.2), not a role: an Employee's rights here
	Approver         Role = "approver"
	Bookkeeper       Role = "bookkeeper"
	ARSpecialist     Role = "ar_specialist"
	APSpecialist     Role = "ap_specialist"
	PaymentReleaser  Role = "payment_releaser"
	PayrollAdmin     Role = "payroll_administrator"
	Accountant       Role = "accountant"
	Controller       Role = "controller"
	Owner            Role = "owner"
	Administrator    Role = "administrator"
	Auditor          Role = "auditor"
	TaxAgent         Role = "tax_agent"
	BoardViewer      Role = "board_viewer"
	CompanySecretary Role = "company_secretary" // the Board viewer's rights here (Appendix D, rev 0.6)
	DPO              Role = "dpo"
	HROfficer        Role = "hr_officer" // Nemura only: no rights in Fyniq
)

// AI is the assistant or an agent acting on behalf of a user (Appendix D's last column). It never
// exceeds the user's rights: its right is the lesser of the user's and the AI column's.
const AI Role = "ai"

// SuperAdmin is the installation role outside the matrix (FR-ADM-06/07): it sees an organisation's
// data only as a member, so it holds no right in any organisation through this role.
const SuperAdmin Role = "super_admin"

// Role facts the matrix test checks against the PRD: Label is the role's name in §7.1, Column the
// Appendix D column whose rights it holds ("" for none).
type roleFacts struct {
	Label  string
	Column string
}

// Roles are the §7.1 roles, each with the Appendix D column it takes its rights from.
var Roles = map[Role]roleFacts{
	Employee:         {"Employee", "Employee"},
	LineManager:      {"Line manager (relationship, §6.2)", "Employee"},
	Approver:         {"Approver: budget holder or project manager", "Approver"},
	Bookkeeper:       {"Bookkeeper", "Bookkeeper"},
	ARSpecialist:     {"AR specialist", "AR"},
	APSpecialist:     {"AP specialist", "AP"},
	PaymentReleaser:  {"Payment releaser", "Releaser"},
	PayrollAdmin:     {"Payroll administrator", "Payroll"},
	Accountant:       {"Accountant", "Accountant"},
	Controller:       {"Controller", "Controller"},
	Owner:            {"Owner", "Owner"},
	Administrator:    {"Administrator", "Admin"},
	Auditor:          {"Auditor", "Auditor"},
	TaxAgent:         {"Tax agent", "Tax agent"},
	BoardViewer:      {"Board viewer", "Board"},
	CompanySecretary: {"Company secretary", "Board"},
	DPO:              {"DPO", "DPO"},
	HROfficer:        {"HR officer (Nemura only)", ""},
}

// Principals are every role the matrix test runs: the §7.1 roles, the AI and the Super admin.
func Principals() []Role {
	out := []Role{Employee, LineManager, Approver, Bookkeeper, ARSpecialist, APSpecialist, PaymentReleaser,
		PayrollAdmin, Accountant, Controller, Owner, Administrator, Auditor, TaxAgent, BoardViewer, CompanySecretary,
		DPO, HROfficer, AI, SuperAdmin}
	return out
}

// Column returns the Appendix D column a role takes its rights from ("" for none).
func Column(r Role) string {
	switch r {
	case AI:
		return "AI"
	case SuperAdmin:
		return ""
	}
	return Roles[r].Column
}
