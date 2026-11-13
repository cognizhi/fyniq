package migration

import (
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/chart"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/packs"
)

// standardTarget is a new organisation's set-up: the standard chart it loads, the sg-gst pack this release carries,
// no departments or projects yet.
func standardTarget(t *testing.T) Target {
	t.Helper()
	tg := Target{Accounts: map[string]TargetAccount{}, Standard: true, GST: map[string]bool{}, Departments: map[string]string{}, Projects: map[string]string{}}
	std, err := chart.Standard()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range std {
		tg.Accounts[a.Code] = TargetAccount{Type: a.Type, ReportGroup: a.ReportGroup, Role: a.Role}
	}
	all, err := pack.LoadAll(packs.FS)
	if err != nil {
		t.Fatal(err)
	}
	var gst pack.Version
	for _, v := range all {
		if v.Pack == "sg-gst" && (v.Year > gst.Year || v.Year == gst.Year && v.N > gst.N) {
			gst = v
		}
	}
	for _, row := range gst.Tables["codes"].Rows {
		tg.GST[row[0]] = true
	}
	return tg
}

func problemsSay(ps []Problem) string {
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(p.Sheet + " " + p.Column + ": " + p.Message + "\n")
	}
	return b.String()
}

// The sample maps onto a new organisation's set-up with nothing to fix (§17.1 M2, design D4): every account onto the
// standard chart's by its code, the workbook's 7040 onto Fyniq's 7050; each GST code onto the pack's; the departments
// and the records' projects as dimension values to create; each budget line onto its report group's first account; the
// bank rules in their order.
func TestTheSampleMapsOntoANewOrganisation(t *testing.T) {
	// req: data/migration Set-up import and mapping review (M2)
	// req: FR-MIG-01
	w := read(t, sample(t))
	tg := standardTarget(t)
	m := w.Propose(tg, nil)
	// The two suppliers resident in the US name no nature of payment: the Accountant chooses each (FR-AP-19, rev 0.259).
	if ps := m.Check(tg); len(ps) != 2 || !strings.Contains(problemsSay(ps), "CloudNimbus Hosting is resident for tax in US") ||
		!strings.Contains(problemsSay(ps), "NeuralAPI Labs is resident for tax in US") {
		t.Fatalf("the sample's mapping:\n%s", problemsSay(ps))
	}
	m, _ = m.Apply(Change{Natures: []NatureMap{
		{Contact: "CloudNimbus Hosting", Nature: "services_outside_singapore", Outcome: "not_applicable", TaxAgent: "Tally & Co Accounting"},
		{Contact: "NeuralAPI Labs", Nature: "services_outside_singapore", Outcome: "not_applicable", TaxAgent: "Tally & Co Accounting"}}})
	if ps := m.Check(tg); len(ps) != 0 {
		t.Fatalf("the sample's mapping with its natures:\n%s", problemsSay(ps))
	}
	// The founders, and the director the 2500 repayment names, as one contact: whose the loan's opening balance is.
	people := map[string]PersonMap{}
	for _, p := range m.People {
		people[p.Workbook] = p
	}
	if people["Alex Tan"].Contact != "Alex Tan" || !people["Alex Tan"].Director || people["Priya Nair"].Sheet != "Funding" ||
		people["Alex Tan (CEO)"].Contact != "Alex Tan" || people["Alex Tan (CEO)"].Row != 127 {
		t.Errorf("the people: %+v", m.People)
	}
	byCode := map[string]AccountMap{}
	for _, a := range m.Accounts {
		byCode[a.Workbook] = a
		if a.Action != Existing {
			t.Errorf("account %s: %+v", a.Workbook, a)
		}
	}
	if len(m.Accounts) != 83 || byCode["7040"].Fyniq != "7050" || byCode["1010"].Fyniq != "1010" || byCode["5010"].Type != "expense" ||
		byCode["2500"].Role != "director-loan" || !byCode["2500"].HasOpening || byCode["2500"].OpeningPerson != "Alex Tan" {
		t.Errorf("the accounts: %d, 7040 %+v", len(m.Accounts), byCode["7040"])
	}
	if len(m.GSTCodes) != 12 || m.GSTCodes[0] != (CodeMap{Workbook: "SR", Row: 6, Fyniq: "SR"}) {
		t.Errorf("the GST codes: %+v", m.GSTCodes)
	}
	if len(m.Departments) != 5 || m.Departments[0] != (ValueMap{Workbook: "Product & Engineering", Code: "PRODUCT-ENGINEERING", Action: Create}) {
		t.Errorf("the departments: %+v", m.Departments)
	}
	for _, p := range m.Projects {
		if p.Action != Create || p.Code != slug(p.Workbook) {
			t.Errorf("project %+v", p)
		}
	}
	if len(m.Projects) == 0 {
		t.Error("no projects")
	}
	lines := map[string]string{}
	for _, l := range m.BudgetLines {
		lines[l.Line] = l.Account
	}
	if len(m.BudgetLines) != 22 || lines["Subscription revenue"] != "4010" || lines["Salaries & staff costs"] != "6011" || lines["FX & rounding"] != "7030" {
		t.Errorf("the budget lines: %v", lines)
	}
	if len(m.BankRules) != 29 || m.BankRules[0] != (RuleMap{Order: 1, Contains: "STRIPE", Account: "1050"}) {
		t.Errorf("the bank rules: %+v", m.BankRules[:2])
	}

	// A changed choice is checked; one the chart cannot take is an error, as is a key the mapping does not hold.
	next, ps := m.Apply(Change{Accounts: []AccountMap{{Workbook: "4030", Fyniq: "4020", Action: Existing, Type: "revenue"}},
		BudgetLines: []LineMap{{Line: "Other revenue", Account: "4020"}}})
	if len(ps) != 0 || len(next.Check(tg)) != 0 {
		t.Fatalf("a good change: %v\n%s", ps, problemsSay(next.Check(tg)))
	}
	if a := next.Accounts[0]; a.Name == "" || a.Row == 0 {
		t.Errorf("a change keeps the workbook's facts: %+v", a)
	}
	bad, ps := m.Apply(Change{Accounts: []AccountMap{{Workbook: "4030", Fyniq: "1010", Action: Existing}}, BudgetLines: []LineMap{{Line: "Other revenue", Account: "1020"}},
		GSTCodes: []CodeMap{{Workbook: "SR", Fyniq: "XX"}}, Projects: []ValueMap{{Workbook: "Nope", Code: "N", Action: Create}}})
	if len(ps) != 1 || !strings.Contains(ps[0].Message, `project "Nope"`) {
		t.Errorf("an unknown key: %+v", ps)
	}
	if say := problemsSay(bad.Check(tg)); !strings.Contains(say, "account 4030 is revenue in the workbook; Fyniq's 1010 is asset") ||
		!strings.Contains(say, `goes to 1020, a asset account`) || !strings.Contains(say, `"XX", which the sg-gst pack does not have`) {
		t.Errorf("a bad change:\n%s", say)
	}

	// An account the chart lacks is created from the workbook's row, and checked as any new account is.
	mine := Target{Accounts: map[string]TargetAccount{}, GST: tg.GST, Departments: map[string]string{"product & engineering": "ENG"}, Projects: map[string]string{}}
	for code, x := range tg.Accounts {
		if code != "6230" {
			mine.Accounts[code] = x
		}
	}
	own := w.Propose(mine, &Mapping{Natures: m.Natures})
	for _, a := range own.Accounts {
		if a.Workbook == "6230" && (a.Action != Create || a.Type != string(storage.Expense) || a.ReportGroup != "Technology & software") {
			t.Errorf("an account to create: %+v", a)
		}
	}
	// A corrected workbook proposes the mapping again, keeping the choices of the mapping before it.
	for _, a := range w.Propose(tg, &next).Accounts {
		if a.Workbook == "4030" && a.Fyniq != "4020" {
			t.Errorf("a person's choice did not stand: %+v", a)
		}
	}
	if own.Departments[0] != (ValueMap{Workbook: "Product & Engineering", Code: "ENG", Action: Existing}) {
		t.Errorf("a department Fyniq has: %+v", own.Departments[0])
	}
	if ps := own.Check(mine); len(ps) != 0 {
		t.Errorf("creating 6230:\n%s", problemsSay(ps))
	}
	if _, a, err := own.Sealed(); err != nil || len(a) != 64 {
		t.Errorf("sealed: %v", err)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{"Product & Engineering": "PRODUCT-ENGINEERING", "  ": "X", "Harbourline AI": "HARBOURLINE-AI",
		"A very long project name that goes on and on": "A-VERY-LONG-PROJECT-NAME-THAT"} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
	vs := values([]string{"A b", "A-B"}, nil, nil)
	if vs[0].Code != "A-B" || vs[1].Code != "A-B-2" {
		t.Errorf("unique codes: %+v", vs)
	}
}
