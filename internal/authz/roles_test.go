package authz_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/authz/authztest"
)

// probe asks the policy engine whether a caller holding roles may need a right on a capability, as any operation's
// policy would (the matrix decides, and the operations' own policies are generated from it).
func probe(roles []authz.Role, c authz.Capability, need authz.Right, ai bool) authz.Decision {
	return authz.Decide(authz.Policy{Operation: "probe", Capability: c, Need: need}, roles, ai)
}

func holdsRight(role authz.Role, c authz.Capability, need authz.Right) bool {
	return probe([]authz.Role{role}, c, need, false).Allowed
}

// grant is what a role's rights and limits are checked against: a capability and the letters the role does or does
// not hold on it.
type grant struct {
	c      authz.Capability
	rights string
}

func checkRole(t *testing.T, role authz.Role, holds, never []grant) {
	t.Helper()
	for _, g := range holds {
		for _, r := range g.rights {
			if !holdsRight(role, g.c, authz.Right(r)) {
				t.Errorf("%s should hold %c on %q", role, r, g.c)
			}
		}
	}
	for _, g := range never {
		for _, r := range g.rights {
			if holdsRight(role, g.c, authz.Right(r)) {
				t.Errorf("%s must not hold %c on %q", role, r, g.c)
			}
		}
	}
}

// Every cell of Appendix D, as the PRD states it, against what the policy engine decides for each role and each right:
// the matrix is the single source of rights (users-roles design: Appendix D as data), so each area is proved for every
// role by the same probe the operations' policies make.
// req: access/roles-and-segregation Permission model
// req: access/roles-and-segregation Permission matrix legend
// req: access/roles-and-segregation Permission matrix: Users, roles, team mode
// req: access/roles-and-segregation Permission matrix: Approval policies, AI catalogue
// req: access/roles-and-segregation Permission matrix: Organisation profile, financial year, defaults
// req: access/roles-and-segregation Permission matrix: Accounting framework and policies
// req: access/roles-and-segregation Permission matrix: Contacts and items
// req: access/roles-and-segregation Permission matrix: Statutory parameters
// req: access/roles-and-segregation Permission matrix: Chart of accounts, dimensions
// req: access/roles-and-segregation Permission matrix: Dimension values, to tag a line
// req: access/roles-and-segregation Permission matrix: Bank transactions, categorise
// req: access/roles-and-segregation Permission matrix: Bank reconciliation sign-off
// req: access/roles-and-segregation Permission matrix: Invoices, credit notes
// req: access/roles-and-segregation Permission matrix: Bills, vendor credits
// req: access/roles-and-segregation Permission matrix: Vendor bank details
// req: access/roles-and-segregation Permission matrix: Payment batches
// req: access/roles-and-segregation Permission matrix: Expense claims
// req: access/roles-and-segregation Permission matrix: Director accounts, settlements, share issues
// req: access/roles-and-segregation Permission matrix: Cap table, holders, investors, equity transactions
// req: access/roles-and-segregation Permission matrix: Evidence: add, supersede
// req: access/roles-and-segregation Permission matrix: Knowledge base: compiled pages and index
// req: access/roles-and-segregation Permission matrix: AI company secretary: enable, schedule, runs and drafts
// req: access/roles-and-segregation Permission matrix: Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture
// req: access/roles-and-segregation Permission matrix: Announcements to the organisation
// req: access/roles-and-segregation Permission matrix: Own notifications and preferences
// req: access/roles-and-segregation Permission matrix: Document templates: edit, publish
// req: access/roles-and-segregation Permission matrix: Timesheets and project time
// req: access/roles-and-segregation Permission matrix: Project reports and cost rates
// req: access/roles-and-segregation Permission matrix: Project cost rates, person level
// req: access/roles-and-segregation Permission matrix: Bank data, payment-provider and ticketing connections
// req: access/roles-and-segregation Permission matrix: Journals
// req: access/roles-and-segregation Permission matrix: Pay runs, employee data
// req: access/roles-and-segregation Permission matrix: Fixed assets
// req: access/roles-and-segregation Permission matrix: Period close / reopen
// req: access/roles-and-segregation Permission matrix: GST return, filing
// req: access/roles-and-segregation Permission matrix: Reports, dashboards
// req: access/roles-and-segregation Permission matrix: Budgets, forecasts
// req: access/roles-and-segregation Permission matrix: Audit trail
// req: access/roles-and-segregation Permission matrix: Statutory calendar
// req: access/roles-and-segregation Permission matrix: Checks and books health
// req: access/roles-and-segregation Permission matrix: Tenant export
// req: access/roles-and-segregation Permission matrix: People and HR link
// req: access/roles-and-segregation Permission matrix: AI providers and model routes
// req: access/roles-and-segregation Permission matrix: AI activity log
// req: access/roles-and-segregation Permission matrix: Connected apps: enable MCP, allowlist clients, grant integration scopes
// req: access/roles-and-segregation Permission matrix: Own MCP connection (opt in, consent, revoke)
// asvs: V8.1.1, V8.2.1
func TestEveryAreaGrantsEachRoleExactlyWhatAppendixDSays(t *testing.T) {
	columns, rows := authztest.AppendixD(t)
	letters := []authz.Right{authz.Create, authz.Approve, authz.Post, authz.Release, authz.View}
	for _, area := range authz.Rows {
		prd, ok := rows[string(area)]
		if !ok {
			t.Fatalf("Appendix D has no row %q", area)
		}
		t.Run(string(area)[:min(len(area), 48)], func(t *testing.T) {
			for _, role := range authz.Principals() {
				col := authz.Column(role)
				if col == "" {
					for _, r := range letters {
						if probe([]authz.Role{role}, area, r, false).Allowed && area != authz.Membership {
							t.Errorf("%s has no column in Appendix D but holds %c on the area", role, r)
						}
					}
					continue
				}
				if !slices.Contains(columns, col) {
					t.Fatalf("%s: column %q is not in Appendix D", role, col)
				}
				cell := authztest.ParseCell(prd[col])
				if own, ok := authz.SecretaryCells[area]; ok && role == authz.CompanySecretary {
					cell = own // the Company secretary's own cells (rev 0.6, 0.280, 0.282, 0.285)
				}
				for _, r := range letters {
					allowed, _ := authztest.Allows(cell, r)
					if col == "AI" && cell.Rights == "" && (cell.Qualifier == "Proposal" || cell.Qualifier == "Proposal (draft)") {
						allowed = r == authz.View // rev 0.278: a Proposal cell is a view of the rows and no other right
					}
					if got := probe([]authz.Role{role}, area, r, false).Allowed; got != allowed {
						t.Errorf("%s / %c: the policy engine says %v, Appendix D %q says %v", role, r, got, prd[col], allowed)
					}
				}
			}
		})
	}
}

// req: access/roles-and-segregation Owner role
// asvs: V8.2.1
func TestOwnerRightsAndDualControl(t *testing.T) {
	// The Owner does everything the Administrator can: on each row the Administrator prepares, the Owner holds C or the
	// approving right, and every such approval is dual-controlled through the approval engine (its Appendix D cell says
	// "+ second approver" or the row is an approval-engine object).
	for _, area := range authz.Rows {
		admin := authz.Grants[area]["Admin"]
		if admin.Rights == "" {
			continue
		}
		if strings.Contains(admin.Rights, "C") && !holdsRight(authz.Owner, area, authz.Create) && !holdsRight(authz.Owner, area, authz.Approve) {
			t.Errorf("the Administrator holds %q on %q but the Owner holds neither C nor A", admin.Rights, area)
		}
		if !holdsRight(authz.Owner, area, authz.View) {
			t.Errorf("the Administrator views %q but the Owner does not", area)
		}
		// What the Owner approves on an Administrator's row is dual-controlled: an approval-engine object, or its cell says
		// "second approver".
		owner := authz.Grants[area]["Owner"]
		if strings.Contains(admin.Rights, "C") && strings.Contains(owner.Rights, "A") && !slices.Contains(authz.ApprovalRows, area) &&
			!strings.Contains(owner.Qualifier, "second approver") && area != authz.AICompanySecretary && area != authz.Announcements && area != authz.KnowledgeBase {
			t.Errorf("the Owner approves on %q, which is neither an approval-engine row nor marked '+ second approver'", area)
		}
	}
	checkRole(t, authz.Owner,
		[]grant{{authz.UsersAndRoles, "A"}, {authz.TenantExport, "A"}, {authz.PeriodClose, "A"}, {authz.AIProviders, "A"}, {authz.ConnectedApps, "A"}},
		[]grant{{authz.Journals, "CP"}, {authz.PaymentBatches, "R"}, {authz.BankTransactions, "CP"}})
	if !slices.Contains(authz.ApprovalRows, authz.TenantExport) || !slices.Contains(authz.ApprovalRows, authz.UsersAndRoles) {
		t.Error("the Owner's export and role changes must go through the approval engine")
	}
	// "no other Standard-mode restriction applies to the Owner" apart from the roles' conflicts: the Owner alone is exempt.
	if c := authz.RoleConflicts([]authz.Role{authz.Owner, authz.Accountant}); len(c) != 0 {
		t.Errorf("the Owner role is exempt from SOD-6, got %v", c)
	}
}

// req: access/roles-and-segregation Administrator role
// asvs: V8.2.1
func TestAdministratorManagesButNeverPostsOrApproves(t *testing.T) {
	checkRole(t, authz.Administrator,
		[]grant{{authz.UsersAndRoles, "C"}, {authz.ApprovalPolicies, "C"}, {authz.Integrations, "C"}, {authz.OrganisationProfile, "C"}, {authz.ConnectedApps, "C"}},
		nil)
	for _, c := range authz.TransactionRows {
		checkRole(t, authz.Administrator, nil, []grant{{c, "APR"}})
	}
	checkRole(t, authz.Administrator, nil, []grant{{authz.Journals, "CAPR"}})
	if c := authz.RoleConflicts([]authz.Role{authz.Administrator, authz.Bookkeeper}); len(c) == 0 {
		t.Error("an Administrator who also transacts is a SOD-6 conflict in Standard mode")
	}
}

// req: access/roles-and-segregation Finance controller role
// asvs: V8.2.1
func TestControllerApprovesNeverPreparesOrReleases(t *testing.T) {
	checkRole(t, authz.Controller,
		[]grant{{authz.Journals, "A"}, {authz.Bills, "A"}, {authz.PaymentBatches, "A"}, {authz.StatutoryParameters, "A"}, {authz.BankReconciliation, "A"}, {authz.PeriodClose, "CA"}},
		[]grant{{authz.PaymentBatches, "CR"}, {authz.Journals, "CP"}})
	// A close or a reopening waits for a second approver: Period close is an approval-engine object (FM-1, SOD-7).
	if !slices.Contains(authz.ApprovalRows, authz.PeriodClose) {
		t.Error("closing and reopening a period must go through the approval engine")
	}
}

// req: access/roles-and-segregation Accountant role
// asvs: V8.2.1
func TestAccountantPostsWithinLimitsAndNeverApprovesOrCloses(t *testing.T) {
	checkRole(t, authz.Accountant,
		[]grant{{authz.Journals, "CP"}, {authz.BankReconciliation, "C"}, {authz.FixedAssets, "CP"}, {authz.GSTReturns, "C"}, {authz.Budgets, "C"}},
		[]grant{{authz.Journals, "A"}, {authz.PeriodClose, "AP"}, {authz.PaymentBatches, "R"}})
	if q := authz.Grants[authz.Journals]["Accountant"].Qualifier; q != "limit" {
		t.Errorf("the Accountant's journals are within a limit, got the qualifier %q", q)
	}
}

// req: access/roles-and-segregation Bookkeeper role
// asvs: V8.2.1
func TestBookkeeperRecordsAndNeverApprovesPostsJournalsOrChangesBankDetails(t *testing.T) {
	checkRole(t, authz.Bookkeeper,
		[]grant{{authz.BankTransactions, "CP"}, {authz.Invoices, "C"}, {authz.Bills, "C"}, {authz.Evidence, "C"}},
		[]grant{{authz.Journals, "CAPR"}, {authz.VendorBankDetails, "CAPR"}, {authz.Invoices, "AP"}, {authz.Bills, "AP"}, {authz.PaymentBatches, "CAPR"}})
	for _, c := range authz.TransactionRows {
		checkRole(t, authz.Bookkeeper, nil, []grant{{c, "A"}})
	}
}

// req: access/roles-and-segregation AR specialist role
// asvs: V8.2.1
func TestARSpecialistHandlesSalesAndNeverApprovesCredit(t *testing.T) {
	checkRole(t, authz.ARSpecialist,
		[]grant{{authz.Invoices, "CP"}, {authz.ContactsAndItems, "C"}},
		[]grant{{authz.Invoices, "A"}, {authz.Journals, "CAPR"}, {authz.Bills, "CAPR"}, {authz.BankReconciliation, "CAPR"}})
}

// req: access/roles-and-segregation AP specialist role
// asvs: V8.2.1
func TestAPSpecialistPreparesPurchasesAndNeverApprovesOrReleases(t *testing.T) {
	checkRole(t, authz.APSpecialist,
		[]grant{{authz.Bills, "C"}, {authz.PaymentBatches, "C"}, {authz.VendorBankDetails, "C"}},
		[]grant{{authz.Bills, "A"}, {authz.PaymentBatches, "AR"}, {authz.VendorBankDetails, "A"}, {authz.Invoices, "CAPR"}})
	// A change of a vendor's bank details waits for a second approver (SOD-1).
	if !slices.Contains(authz.ApprovalRows, authz.VendorBankDetails) {
		t.Error("a change of vendor bank details must go through the approval engine")
	}
}

// req: access/roles-and-segregation Payroll administrator role
// asvs: V8.2.1
func TestPayrollAdministratorHandlesPayRunsAndNeverApprovesOrReleases(t *testing.T) {
	checkRole(t, authz.PayrollAdmin,
		[]grant{{authz.PayRuns, "C"}, {authz.PaymentBatches, "C"}},
		[]grant{{authz.PayRuns, "AR"}, {authz.PaymentBatches, "AR"}, {authz.Journals, "CAPR"}})
}

// The Payment releaser releases and creates or approves nothing it releases. Not tagged for the role's requirement:
// its text also says the releaser manages bank connections, which Appendix D gives them no right on ("—"; the Owner
// approves and the Administrator prepares), so the PRD's text and its matrix disagree and the owner decides which is
// right (reported in the p1-users-roles hand-over).
// asvs: V8.2.1
func TestPaymentReleaserReleasesAndNeverCreatesOrApproves(t *testing.T) {
	checkRole(t, authz.PaymentReleaser,
		[]grant{{authz.PaymentBatches, "R"}},
		[]grant{{authz.PaymentBatches, "CAP"}, {authz.VendorBankDetails, "CAPR"}, {authz.Bills, "CAPR"}, {authz.Journals, "CAPR"}})
}

// req: access/roles-and-segregation Approver role
// asvs: V8.2.1
func TestApproverApprovesWithinDepartmentAndLimitOnly(t *testing.T) {
	checkRole(t, authz.Approver,
		[]grant{{authz.Bills, "A"}, {authz.Invoices, "A"}, {authz.ExpenseClaims, "A"}, {authz.Reports, "V"}},
		[]grant{{authz.Journals, "CAPR"}, {authz.PaymentBatches, "CAPR"}, {authz.Bills, "CP"}, {authz.UsersAndRoles, "CAPR"}})
	for c, q := range map[authz.Capability]string{authz.Bills: "limit", authz.Invoices: "limit", authz.Reports: "dept", authz.Budgets: "dept"} {
		if got := authz.Grants[c]["Approver"].Qualifier; got != q {
			t.Errorf("the Approver's %q cell is qualified %q, want %q", c, got, q)
		}
	}
}

// req: access/roles-and-segregation Employee role
// asvs: V8.2.1
func TestEmployeeSeesOnlyTheirOwn(t *testing.T) {
	checkRole(t, authz.Employee,
		[]grant{{authz.ExpenseClaims, "C"}, {authz.Evidence, "C"}, {authz.Timesheets, "C"}, {authz.PayRuns, "V"}},
		[]grant{{authz.Journals, "V"}, {authz.Invoices, "V"}, {authz.Bills, "V"}, {authz.BankTransactions, "V"}, {authz.Reports, "V"},
			{authz.ChartOfAccounts, "V"}, {authz.ExpenseClaims, "AP"}, {authz.AuditTrail, "V"}})
	for c, q := range map[authz.Capability]string{authz.ExpenseClaims: "own", authz.Evidence: "own", authz.Timesheets: "own", authz.PayRuns: "own"} {
		if got := authz.Grants[c]["Employee"].Qualifier; got != q {
			t.Errorf("the Employee's %q cell is qualified %q, want %q: only their own", c, got, q)
		}
	}
}

// req: access/roles-and-segregation Line manager is a relationship, not a role
// asvs: V8.2.1
func TestLineManagerHoldsNothingBeyondTheEmployeesRights(t *testing.T) {
	if authz.Column(authz.LineManager) != authz.Column(authz.Employee) {
		t.Fatal("a line manager is an Employee: no rights of their own")
	}
	for _, area := range authz.Rows {
		for _, r := range []authz.Right{authz.Create, authz.Approve, authz.Post, authz.Release, authz.View} {
			if holdsRight(authz.LineManager, area, r) != holdsRight(authz.Employee, area, r) {
				t.Errorf("a line manager and an Employee differ on %c of %q", r, area)
			}
		}
	}
}

// req: access/roles-and-segregation Auditor role
// asvs: V8.2.1, V8.2.3
func TestAuditorReadsEverythingAndChangesNothing(t *testing.T) {
	for _, area := range authz.Rows {
		for _, r := range []authz.Right{authz.Approve, authz.Post, authz.Release} {
			if holdsRight(authz.Auditor, area, r) {
				t.Errorf("the Auditor holds %c on %q", r, area)
			}
		}
		if area != authz.OwnNotifications && area != authz.OwnDataRequests && area != authz.OwnMCPConnection && holdsRight(authz.Auditor, area, authz.Create) {
			t.Errorf("the Auditor may create on %q", area)
		}
	}
	for _, c := range []authz.Capability{authz.Journals, authz.Invoices, authz.Bills, authz.AuditTrail, authz.Evidence, authz.Reports} {
		if !holdsRight(authz.Auditor, c, authz.View) {
			t.Errorf("the Auditor must read %q", c)
		}
	}
	// Payroll personal data and vendor bank details are masked unless explicitly granted.
	for _, c := range []authz.Capability{authz.PayRuns, authz.VendorBankDetails} {
		d := probe([]authz.Role{authz.Auditor}, c, authz.View, false)
		if !d.Allowed || d.Right != authz.Masked {
			t.Errorf("the Auditor sees %q masked only, got %+v", c, d)
		}
	}
}

// req: access/roles-and-segregation Tax agent role
// asvs: V8.2.1
func TestTaxAgentWorksTheTaxPapersAndNeverAdministersUsers(t *testing.T) {
	checkRole(t, authz.TaxAgent,
		[]grant{{authz.GSTReturns, "C"}, {authz.StatutoryParameters, "C"}, {authz.Journals, "C"}, {authz.StatutoryCalendar, "C"}},
		[]grant{{authz.UsersAndRoles, "CAPRV"}, {authz.ApprovalPolicies, "CAPR"}, {authz.Journals, "AP"}, {authz.PaymentBatches, "CAPR"}, {authz.GSTReturns, "AP"}})
}

// req: access/roles-and-segregation Board viewer role
// asvs: V8.2.1
func TestBoardViewerSeesTheBoardPackNotTheTransactions(t *testing.T) {
	checkRole(t, authz.BoardViewer,
		[]grant{{authz.Reports, "V"}, {authz.Budgets, "V"}, {authz.CapTable, "V"}},
		[]grant{{authz.Journals, "V"}, {authz.Invoices, "V"}, {authz.Bills, "V"}, {authz.BankTransactions, "V"}, {authz.PayRuns, "V"}, {authz.AuditTrail, "V"}, {authz.ExpenseClaims, "V"}})
	for c, q := range map[authz.Capability]string{authz.Reports: "board pack", authz.CapTable: "own holding; all if granted"} {
		if got := authz.Grants[c]["Board"].Qualifier; got != q {
			t.Errorf("the Board viewer's %q cell is qualified %q, want %q", c, got, q)
		}
	}
}

// req: access/roles-and-segregation Company secretary role
// req: access/roles-and-segregation Company secretary in the permission matrix
// asvs: V8.2.1
func TestCompanySecretaryHoldsTheBoardViewersRightsAndTheirOwnCells(t *testing.T) {
	// The Board viewer's rights: every cell the Board column holds, the Company secretary holds.
	for _, area := range authz.Rows {
		for _, r := range []authz.Right{authz.Create, authz.Approve, authz.View} {
			if holdsRight(authz.BoardViewer, area, r) && !holdsRight(authz.CompanySecretary, area, r) {
				t.Errorf("the Company secretary lacks the Board viewer's %c on %q", r, area)
			}
		}
	}
	checkRole(t, authz.CompanySecretary,
		[]grant{{authz.CorporateProfile, "C"}, {authz.Evidence, "C"}, {authz.CapTable, "V"}, {authz.AICompanySecretary, "V"}, {authz.StatutoryCalendar, "C"}},
		[]grant{{authz.Journals, "V"}, {authz.Invoices, "V"}, {authz.BankTransactions, "V"}, {authz.PayRuns, "V"}, {authz.VendorBankDetails, "V"}, {authz.PaymentBatches, "V"}, {authz.CapTable, "CAP"}})
	if q := authz.SecretaryCells[authz.StatutoryCalendar].Qualifier; q != authz.CalendarCorporate {
		t.Errorf("the calendar's C reaches the corporate items alone, got %q", q)
	}
}

// req: access/roles-and-segregation Compliance officer role
// asvs: V8.2.1
func TestComplianceOfficerHandlesComplianceNeverTransactions(t *testing.T) {
	checkRole(t, authz.DPO,
		[]grant{{authz.ComplianceCenter, "CA"}, {authz.DataProtection, "CA"}, {authz.PolicyLibrary, "CA"}, {authz.AuditTrail, "V"}},
		[]grant{{authz.Journals, "CAPR"}, {authz.Invoices, "CAPR"}, {authz.Bills, "CAPR"}, {authz.PaymentBatches, "CAPR"}, {authz.BankTransactions, "CAPR"}})
}

// req: access/roles-and-segregation Super admin role
// req: access/roles-and-segregation Super admin outside the permission matrix
// asvs: V8.2.1, V8.3.1
func TestSuperAdminHoldsNothingInAnOrganisationThroughTheRole(t *testing.T) {
	if authz.Column(authz.SuperAdmin) != "" {
		t.Fatal("the Super admin has no Appendix D column: an installation role outside the matrix")
	}
	for _, area := range authz.Rows {
		for _, r := range []authz.Right{authz.Create, authz.Approve, authz.Post, authz.Release, authz.View} {
			if holdsRight(authz.SuperAdmin, area, r) {
				t.Errorf("the Super admin holds %c on %q without being a member", r, area)
			}
		}
	}
	if d := authz.Decide(authz.GetOrganisation, []authz.Role{authz.SuperAdmin}, false); d.Allowed {
		t.Errorf("a Super admin who is not a member reads no organisation: %+v", d)
	}
	// The installation's operations are theirs and refuse the AI.
	for _, op := range []string{"listSuperAdmins"} {
		if p, ok := authz.Policies[op]; ok && authz.Decide(p, []authz.Role{authz.Owner}, true).Allowed {
			t.Errorf("%s admits the AI", op)
		}
	}
}

// req: access/roles-and-segregation AI assistant principal
// asvs: V8.2.1, V4.2.1
func TestAIReadsWithinTheDelegatingUsersRightsAndWritesNothing(t *testing.T) {
	// Reads: the AI's right is the lesser of the user's and its own column's, over every row and every user role.
	for _, area := range authz.Rows {
		for _, role := range authz.Principals() {
			if role == authz.AI || role == authz.SuperAdmin || authz.Column(role) == "" {
				continue
			}
			user := probe([]authz.Role{role}, area, authz.View, false)
			ai := probe([]authz.Role{role}, area, authz.View, true)
			if ai.Allowed && !user.Allowed {
				t.Errorf("the AI views %q for a %s, who may not", area, role)
			}
			// It never writes: no create, approve, post or release for any user, whatever the user may do.
			for _, r := range []authz.Right{authz.Create, authz.Approve, authz.Post, authz.Release} {
				if probe([]authz.Role{role}, area, r, true).Allowed {
					t.Errorf("the AI holds %c on %q for a %s", r, area, role)
				}
			}
		}
	}
	// It never approves or rejects (the never-list, §9.6) and holds no session or installation right.
	for _, op := range []authz.Policy{authz.ApproveApproval, authz.RejectApproval, authz.ChallengeApproval} {
		if d := authz.Decide(op, []authz.Role{authz.Owner}, true); d.Allowed {
			t.Errorf("%s admits the AI for an Owner", op.Operation)
		}
	}
	// Its proposals come from the catalogue only: the AI column's Proposal cells grant a view and nothing else.
	for _, area := range authz.Rows {
		cell := authz.Grants[area]["AI"]
		if cell.Qualifier == "Proposal" || cell.Qualifier == "Proposal (draft)" {
			if cell.Rights != "" {
				t.Errorf("%q: a Proposal cell carries no right letters, got %q", area, cell.Rights)
			}
		}
	}
}
