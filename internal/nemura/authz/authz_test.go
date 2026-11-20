package authz

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// prdRows reads Appendix B's table from PRD-002, as the rows of its cells.
func prdRows(t *testing.T) [][]string {
	t.Helper()
	b, err := os.ReadFile("../../../docs/prd/prd-002-nemura-people.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	from, to := strings.Index(s, "## Appendix B"), strings.Index(s, "## Appendix C")
	if from < 0 || to < from {
		t.Fatal("Appendix B not found in PRD-002")
	}
	var rows [][]string
	for _, l := range strings.Split(s[from:to], "\n") {
		if strings.HasPrefix(l, "|") && !strings.HasPrefix(l, "|---") {
			var cells []string
			for _, c := range strings.Split(strings.Trim(strings.TrimSpace(l), "|"), " | ") {
				cells = append(cells, strings.TrimSpace(c))
			}
			rows = append(rows, cells)
		}
	}
	return rows
}

// The matrix is Appendix B: every row, every cell and the order, and the roles are its columns.
// req: people/experience Permission matrix
func TestTheMatrixIsAppendixB(t *testing.T) {
	prd := prdRows(t)
	if len(prd) < 10 {
		t.Fatalf("read %d rows of Appendix B", len(prd))
	}
	if len(prd[0]) != RoleCount+1 || prd[0][0] != "Capability" {
		t.Fatalf("Appendix B's header: %v", prd[0])
	}
	for i, h := range Headers {
		if prd[0][i+1] != h {
			t.Errorf("column %d is %q in the PRD and %q here", i+1, prd[0][i+1], h)
		}
	}
	if len(Rows) != len(prd)-1 {
		t.Fatalf("%d rows here, %d in the PRD", len(Rows), len(prd)-1)
	}
	for i, r := range Rows {
		p := prd[i+1]
		if r.Capability != p[0] {
			t.Errorf("row %d: %q here, %q in the PRD", i+1, r.Capability, p[0])
		}
		for c := range r.Cells {
			if r.Cells[c] != p[c+1] {
				t.Errorf("%s, %s: %q here, %q in the PRD", p[0], Headers[c], r.Cells[c], p[c+1])
			}
		}
	}
}

// Closed by default: a role or a capability the matrix does not name holds nothing, and nobody holds a right the PRD does not give.
// req: people/experience Permission matrix
func TestAnUnknownRoleOrCapabilityHoldsNothing(t *testing.T) {
	if len(Rights("superuser", "Pay, pay history, pay runs")) != 0 || len(Rights(Owner, "Invented capability")) != 0 {
		t.Fatal("an unknown role or capability holds rights")
	}
	for _, right := range []Right{Create, Approve, Sign, View, Masked, Own} {
		if Can("", "", right) {
			t.Errorf("an empty role holds %c", right)
		}
	}
}

// The scenarios of the permission-matrix requirement, each read off the table.
// req: people/experience Permission matrix
func TestTheScenariosOfThePermissionMatrix(t *testing.T) {
	const own = "Own profile, balances, payslips, letters, requests"
	// An employee acts only on their own profile, balances, payslips and requests, and sees the directory.
	if !Can(Employee, own, Own) || !Can(Employee, "Directory and org chart", View) || Can(Employee, "Pay, pay history, pay runs", View) || Can(Employee, "People records (not restricted)", View) {
		t.Error("employee")
	}
	// The AI never exceeds the asking person's rights and only drafts letters, posts and summaries.
	for _, c := range []string{"Pay, pay history, pay runs", "Roles (NM-ROLE-05)", "NRIC or FIN", "Salary bank account", "Fyniq link and external systems"} {
		if Can(AI, c, Create) || Can(AI, c, Approve) || Can(AI, c, Sign) || Can(AI, c, View) {
			t.Errorf("the AI holds a right on %s", c)
		}
	}
	if !Can(AI, "Letters and templates", Create) || Can(AI, "Letters and templates", Sign) || Can(AI, "Letters and templates", Approve) {
		t.Error("the AI drafts letters and signs none")
	}
	// The Payroll administrator sees the NRIC for filings and the bank details, and no medical or disciplinary record.
	if !Can(PayrollAdmin, "NRIC or FIN", View) || !Can(PayrollAdmin, "Salary bank account", View) || len(Rights(PayrollAdmin, "Medical certificates and notes")) != 0 || len(Rights(PayrollAdmin, "Disciplinary records")) != 0 {
		t.Error("payroll administrator")
	}
	// An interviewer sees only candidates assigned to them; a line manager never sees pay.
	if !strings.Contains(cellOf(Interviewer, "Hiring (P2)"), "assigned candidates only") || Can(LineManager, "Pay, pay history, pay runs", View) {
		t.Error("interviewer or line manager")
	}
	// Hiring: the HR administrator and the Owner create requisitions and only the Owner approves (rev 0.69); every member reads company and team goals.
	if !Can(HRAdmin, "Hiring (P2)", Create) || !Can(Owner, "Hiring (P2)", Create) || !Can(Owner, "Hiring (P2)", Approve) || Can(HRAdmin, "Hiring (P2)", Approve) || Can(HROfficer, "Hiring (P2)", Create) || !Can(Employee, "Performance (P2)", View) {
		t.Error("hiring or goals rights")
	}
	// The Administrator manages roles but cannot grant payroll, signing or Owner rights; only the Owner approves role changes.
	if !Can(Administrator, "Roles (NM-ROLE-05)", Create) || !strings.Contains(cellOf(Administrator, "Roles (NM-ROLE-05)"), "not payroll, signing or Owner rights") ||
		Can(Administrator, "Roles (NM-ROLE-05)", Approve) || !Can(Owner, "Roles (NM-ROLE-05)", Approve) {
		t.Error("administrator")
	}
	// Restricted fields are masked for the roles that may see only their shape.
	if !Can(HROfficer, "NRIC or FIN", Masked) || Can(HROfficer, "NRIC or FIN", View) {
		t.Error("masked NRIC for the HR officer")
	}
	// The own-record column of the first row holds for every role that has an account.
	for _, r := range Roles {
		if !Can(r, own, Own) {
			t.Errorf("%s has no own record", r)
		}
	}
}

func cellOf(role Role, capability string) string {
	for i, r := range Roles {
		if r != role {
			continue
		}
		for _, row := range Rows {
			if row.Capability == capability {
				return row.Cells[i]
			}
		}
	}
	return ""
}

// Parse reads the rights out of every cell of the table without losing one: each cell's rights are exactly its legend letters
// outside brackets, and a dash is nothing.
func TestParseReadsEveryCell(t *testing.T) {
	for _, row := range Rows {
		for i, cell := range row.Cells {
			outside := regexp.MustCompile(`\([^)]*\)`).ReplaceAllString(cell, "")
			words := regexp.MustCompile(`\b[CASVMO]\b`).FindAllString(outside, -1)
			set := Parse(cell)
			for _, w := range words {
				if !set[Right(w[0])] {
					t.Errorf("%s, %s: %q lost %s", row.Capability, Roles[i], cell, w)
				}
			}
			if cell == "—" && len(set) != 0 {
				t.Errorf("%s, %s: a dash holds %v", row.Capability, Roles[i], set)
			}
		}
	}
}
