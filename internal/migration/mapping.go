package migration

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	billdoc "github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/chart"
	"github.com/cognizhi/fyniq/internal/domain/dimension"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// MappingFormat names the mapping's format: the Accountant's sign-off names its SHA-256 (design D4).
const MappingFormat = "fyniq-migration-mapping@1"

// CodeMapping is a mapping that does not fit Fyniq's set-up: an account of another type, a GST code the sg-gst pack
// does not have, a dimension value's code out of format, a budget line or bank rule without an account.
const CodeMapping = "MIGRATION-11"

// CodeNotSignable is a mapping that cannot be signed yet: of a workbook that is not the latest valid one.
const CodeNotSignable = "MIGRATION-12"

// The mapping's actions: map onto what Fyniq has, or create it with the set-up (M2, design D4).
const (
	Existing = "existing"
	Create   = "create"
)

// moved are the workbook's account codes the standard chart moved (internal/domain/chart/standard-chart.csv, source
// "workbook-moved"): the workbook's 7040, disposals, is Fyniq's 7050, as 7040 is Fyniq's unrealised FX.
var moved = map[string]string{"7040": "7050"}

// Mapping is the workbook's set-up mapped onto Fyniq's (design D4, §17.1 M2): accounts by code, creating those Fyniq
// lacks from the Accounts sheet; GST codes onto the sg-gst pack's; departments and projects as dimension values (rev
// 0.254); budget lines onto accounts; bank rules in their order.
type Mapping struct {
	Format      string       `json:"format"`
	Accounts    []AccountMap `json:"accounts"`
	GSTCodes    []CodeMap    `json:"gst_codes"`
	Departments []ValueMap   `json:"departments"`
	Projects    []ValueMap   `json:"projects"`
	BudgetLines []LineMap    `json:"budget_lines"`
	BankRules   []RuleMap    `json:"bank_rules"`
	// People are the founders and everyone named on a director's or subscriber's account (rev 0.259), as every line on
	// those accounts names its person (B.7).
	People []PersonMap `json:"people"`
	// Natures are the nature of payment of each supplier resident for tax outside Singapore, which its bills record for
	// the tax agent's withholding decision (FR-AP-19): the workbook keeps none, so the Accountant chooses (rev 0.259).
	Natures []NatureMap `json:"natures"`
}

// NatureMap is a non-resident supplier of the Contacts sheet, the nature of payment its bills record, and the tax
// agent's decision on withholding for it, which its bills post with (FR-AP-19): the Accountant enters each.
type NatureMap struct {
	Contact     string `json:"contact"`
	Row         int    `json:"row"`
	Country     string `json:"country"`
	Nature      string `json:"nature"`
	Outcome     string `json:"outcome"`
	RatePercent string `json:"rate_percent,omitempty"`
	TaxAgent    string `json:"tax_agent"`
}

// PersonMap is a person the workbook names and the contact they become: a director, a shareholder, or both.
type PersonMap struct {
	Workbook    string `json:"workbook"`
	Sheet       string `json:"sheet"`
	Row         int    `json:"row"`
	Contact     string `json:"contact"`
	Director    bool   `json:"director,omitempty"`
	Shareholder bool   `json:"shareholder,omitempty"`
}

// AccountMap is a workbook account and the Fyniq account it maps onto: one Fyniq has, or one created from the
// workbook's row.
type AccountMap struct {
	Workbook     string `json:"workbook"`
	Name         string `json:"name"`
	WorkbookType string `json:"workbook_type"`
	Row          int    `json:"row"`
	Fyniq        string `json:"fyniq"`
	Action       string `json:"action"`
	Type         string `json:"type"`
	ReportGroup  string `json:"report_group,omitempty"`
	CashFlowLine string `json:"cash_flow_line,omitempty"`
	Function     string `json:"function,omitempty"`
	MoneyAccount bool   `json:"money_account,omitempty"`
	DefaultGST   string `json:"default_gst_code,omitempty"`
	// Role is the system role of the Fyniq account it maps onto (FR-COA-03), and HasOpening whether the workbook gives
	// it an opening balance; a director's or subscriber's account with one names whose it is (rev 0.259).
	Role          string `json:"role,omitempty"`
	HasOpening    bool   `json:"has_opening,omitempty"`
	OpeningPerson string `json:"opening_person,omitempty"`
}

// CodeMap is a workbook GST code and the sg-gst code it maps onto.
type CodeMap struct {
	Workbook string `json:"workbook"`
	Row      int    `json:"row"`
	Fyniq    string `json:"fyniq"`
}

// ValueMap is a workbook department or project and the dimension value it maps onto, by code.
type ValueMap struct {
	Workbook string `json:"workbook"`
	Code     string `json:"code"`
	Action   string `json:"action"`
}

// LineMap is a budget's P&L line and the account its amounts go to.
type LineMap struct {
	Line    string `json:"line"`
	Account string `json:"account"`
}

// RuleMap is a bank rule, in its order: the text it matches, and the Fyniq account and GST code it gives.
type RuleMap struct {
	Order    int    `json:"order"`
	Contains string `json:"contains"`
	Account  string `json:"account"`
	GST      string `json:"gst,omitempty"`
}

// TargetAccount is an account of Fyniq's chart the mapping maps onto.
type TargetAccount struct {
	Type        storage.AccountType
	ReportGroup string
	Role        storage.AccountRole
}

// personal says whether an account of a role takes a person on each line: a director's account or share application
// monies (B.7).
func personal(role string) bool {
	return role == ledger.RoleDirectorLoan || role == ledger.RoleDeferredRemuneration || role == ledger.RoleShareApplicationMonies
}

// Target is Fyniq's set-up the mapping maps onto: the organisation's chart, or the standard chart it loads with the
// set-up when it has none; the sg-gst pack's codes; the departments and projects it has, by lower-cased name.
type Target struct {
	Accounts    map[string]TargetAccount
	Standard    bool // the chart is the standard chart, loaded with the set-up
	GST         map[string]bool
	Departments map[string]string
	Projects    map[string]string
}

// TargetOf is the organisation's set-up as the mapping reads it, with the sg-gst version in force on on.
func TargetOf(ctx context.Context, u storage.UnitOfWork, on time.Time) (Target, error) {
	t := Target{Accounts: map[string]TargetAccount{}, GST: map[string]bool{}, Departments: map[string]string{}, Projects: map[string]string{}}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return t, err
	}
	for _, a := range accounts {
		if !a.Group {
			t.Accounts[a.Code] = TargetAccount{Type: a.Type, ReportGroup: a.ReportGroup, Role: a.Role}
		}
	}
	if len(t.Accounts) == 0 {
		std, err := chart.Standard()
		if err != nil {
			return t, err
		}
		t.Standard = true
		for _, a := range std {
			if !a.Group {
				t.Accounts[a.Code] = TargetAccount{Type: a.Type, ReportGroup: a.ReportGroup, Role: a.Role}
			}
		}
	}
	v, err := gst.Version(ctx, u, on)
	if err != nil {
		return t, err
	}
	for _, row := range v.Tables["codes"].Rows {
		if c, ok := pack.Code(v, row[0]); ok {
			t.GST[c.Code] = true
		}
	}
	dims, err := u.Dimensions().List(ctx)
	if err != nil {
		return t, err
	}
	for _, d := range dims {
		into := map[string]map[string]string{storage.DimensionDepartment: t.Departments, storage.DimensionProject: t.Projects}[d.Kind]
		if into == nil {
			continue
		}
		values, err := u.Dimensions().Values(ctx, &d.ID)
		if err != nil {
			return t, err
		}
		for _, x := range values {
			into[strings.ToLower(x.Name)] = x.Code
		}
	}
	return t, nil
}

// typeOf is Fyniq's account type for the workbook's: Cost of Sales, Other Expense and Tax are expenses and Other
// Income revenue, their report groups keeping the distinction (FR-COA-02).
func typeOf(workbook string) storage.AccountType {
	switch workbook {
	case "Asset":
		return storage.Asset
	case "Liability":
		return storage.Liability
	case "Equity":
		return storage.Equity
	case "Revenue", "Other Income":
		return storage.Revenue
	case "Expense", "Cost of Sales", "Other Expense", "Tax":
		return storage.Expense
	}
	return ""
}

// slug is a dimension value's code made from its name: letters and digits in capitals, anything else one hyphen, at
// most 30 characters.
func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToUpper(name) {
		switch {
		case r >= 'A' && r <= 'Z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if len(s) > 30 {
		s = strings.TrimRight(s[:30], "-")
	}
	if s == "" {
		s = "X"
	}
	return s
}

// values maps names onto dimension values: one the organisation has by its name, else one to create with a code
// made from the name, unique among the others.
func values(names []string, have map[string]string, previous []ValueMap) []ValueMap {
	before := map[string]ValueMap{}
	for _, v := range previous {
		before[v.Workbook] = v
	}
	taken := map[string]bool{}
	for _, c := range have {
		taken[strings.ToUpper(c)] = true
	}
	var out []ValueMap
	for _, n := range names {
		if v, ok := before[n]; ok {
			out = append(out, v)
			taken[strings.ToUpper(v.Code)] = true
			continue
		}
		if c, ok := have[strings.ToLower(n)]; ok {
			out = append(out, ValueMap{Workbook: n, Code: c, Action: Existing})
			continue
		}
		code := slug(n)
		for i := 2; taken[code]; i++ {
			code = fmt.Sprintf("%s-%d", strings.TrimRight(slug(n)[:min(len(slug(n)), 27)], "-"), i)
		}
		taken[code] = true
		out = append(out, ValueMap{Workbook: n, Code: code, Action: Create})
	}
	return out
}

// projects are the projects the workbook's records name, in order of name.
func (w *Workbook) projects() []string {
	seen := map[string]bool{}
	add := func(p string) {
		if p = strings.TrimSpace(p); p != "" {
			seen[p] = true
		}
	}
	for _, x := range w.Transactions {
		add(x.Project)
	}
	for _, ds := range [][]Document{w.Invoices, w.Bills, w.CarryForward.Invoices, w.CarryForward.Bills} {
		for _, d := range ds {
			add(d.Project)
		}
	}
	for _, q := range w.QuotesOrders {
		add(q.Project)
	}
	for _, l := range w.Journal {
		add(l.Project)
	}
	for _, r := range w.Recurring {
		add(r.Project)
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// Propose is the mapping the workbook suggests onto t (design D4). A previous mapping's choices stand for what the
// workbook still holds, so uploading a corrected workbook keeps what a person mapped.
func (w *Workbook) Propose(t Target, previous *Mapping) Mapping {
	if previous == nil {
		previous = &Mapping{}
	}
	m := Mapping{Format: MappingFormat, Accounts: []AccountMap{}, GSTCodes: []CodeMap{}, Departments: []ValueMap{}, Projects: []ValueMap{},
		BudgetLines: []LineMap{}, BankRules: []RuleMap{}, People: []PersonMap{}, Natures: []NatureMap{}}
	accounts := map[string]AccountMap{}
	for _, a := range previous.Accounts {
		accounts[a.Workbook] = a
	}
	for _, a := range w.Accounts {
		e := AccountMap{Workbook: a.Code, Name: a.Name, WorkbookType: a.Type, Row: a.Row, HasOpening: a.Opening != nil && !a.Opening.IsZero()}
		if p, ok := accounts[a.Code]; ok {
			p.Name, p.WorkbookType, p.Row, p.HasOpening, p.Role = e.Name, e.WorkbookType, e.Row, e.HasOpening, ""
			if x, ok := t.Accounts[p.Fyniq]; ok && p.Action == Existing {
				p.Role = string(x.Role)
			}
			m.Accounts = append(m.Accounts, p)
			continue
		}
		code := cmp.Or(moved[a.Code], a.Code)
		if x, ok := t.Accounts[code]; ok {
			e.Fyniq, e.Action, e.Type, e.Role = code, Existing, string(x.Type), string(x.Role)
		} else {
			e.Fyniq, e.Action, e.Type = a.Code, Create, string(typeOf(a.Type))
			e.ReportGroup, e.CashFlowLine, e.Function, e.MoneyAccount, e.DefaultGST = a.ReportGroup, a.CashFlowLine, a.Function, a.Money, codeOf(a.DefaultGST)
		}
		m.Accounts = append(m.Accounts, e)
	}
	codes := map[string]CodeMap{}
	for _, c := range previous.GSTCodes {
		codes[c.Workbook] = c
	}
	for _, c := range w.GSTCodes {
		e := CodeMap{Workbook: c.Code, Row: c.Row, Fyniq: c.Code}
		if p, ok := codes[c.Code]; ok {
			e.Fyniq = p.Fyniq
		}
		m.GSTCodes = append(m.GSTCodes, e)
	}
	var departments []string
	for _, d := range w.Departments {
		departments = append(departments, d.Name)
	}
	m.Departments = append(m.Departments, values(departments, t.Departments, previous.Departments)...)
	m.Projects = append(m.Projects, values(w.projects(), t.Projects, previous.Projects)...)

	lines := map[string]string{}
	for _, l := range previous.BudgetLines {
		lines[l.Line] = l.Account
	}
	groups := map[string]string{} // a report group's first P&L account, by code
	consider := func(code string, typ storage.AccountType, group string) {
		if (typ == storage.Revenue || typ == storage.Expense) && group != "" && (groups[group] == "" || code < groups[group]) {
			groups[group] = code
		}
	}
	for code, x := range t.Accounts {
		consider(code, x.Type, x.ReportGroup)
	}
	for _, a := range m.Accounts {
		if a.Action == Create {
			consider(a.Fyniq, storage.AccountType(a.Type), a.ReportGroup)
		}
	}
	seen := map[string]bool{}
	for _, b := range w.Budget {
		if b.Line == "" || seen[b.Line] {
			continue
		}
		seen[b.Line] = true
		account, ok := lines[b.Line]
		if !ok {
			account = groups[b.Line]
		}
		m.BudgetLines = append(m.BudgetLines, LineMap{Line: b.Line, Account: account})
	}

	fyniq := map[string]string{}
	for _, a := range m.Accounts {
		fyniq[a.Workbook] = a.Fyniq
	}
	rules := map[int]RuleMap{}
	for _, r := range previous.BankRules {
		rules[r.Order] = r
	}
	for i, r := range w.BankRules {
		e := RuleMap{Order: i + 1, Contains: r.Contains, Account: cmp.Or(fyniq[r.Account], r.Account), GST: codeOf(r.GST)}
		if p, ok := rules[e.Order]; ok && p.Contains == e.Contains {
			e.Account, e.GST = p.Account, p.GST
		}
		m.BankRules = append(m.BankRules, e)
	}
	m.People = w.people(m, previous.People)
	natures := map[string]NatureMap{}
	for _, n := range previous.Natures {
		natures[n.Contact] = n
	}
	for _, c := range w.Contacts {
		role := strings.ToLower(c.Role)
		country := countryOf(c.Country)
		if (strings.Contains(role, "supplier") || strings.Contains(role, "vendor") || strings.Contains(role, "both")) && country != "" && country != "SG" {
			name := strings.TrimSpace(c.Name)
			p := natures[name]
			m.Natures = append(m.Natures, NatureMap{Contact: name, Row: c.Row, Country: country, Nature: p.Nature, Outcome: p.Outcome,
				RatePercent: p.RatePercent, TaxAgent: p.TaxAgent})
		}
	}
	return m
}

// personName is the contact a name the workbook gives a person becomes: without a role in brackets, as "Alex Tan
// (CEO)" is Alex Tan.
func personName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndex(name, " ("); i > 0 && strings.HasSuffix(name, ")") {
		name = strings.TrimSpace(name[:i])
	}
	return name
}

// people are the Funding sheet's founders, then everyone named on a row to an account m maps onto a director's or
// subscriber's account, keeping a previous mapping's choices; each director account with an opening balance is
// given the person named on its rows, or the one founder, when there is exactly one (rev 0.259).
func (w *Workbook) people(m Mapping, previous []PersonMap) []PersonMap {
	before := map[string]PersonMap{}
	for _, p := range previous {
		before[p.Workbook] = p
	}
	var out []PersonMap
	index := map[string]int{}
	add := func(p PersonMap) {
		if p.Workbook = strings.TrimSpace(p.Workbook); p.Workbook == "" {
			return
		}
		if i, ok := index[p.Workbook]; ok {
			out[i].Director, out[i].Shareholder = out[i].Director || p.Director, out[i].Shareholder || p.Shareholder
			return
		}
		if b, ok := before[p.Workbook]; ok {
			p.Contact, p.Director, p.Shareholder = b.Contact, b.Director, b.Shareholder
		}
		index[p.Workbook] = len(out)
		out = append(out, p)
	}
	for _, h := range w.Holdings {
		if strings.EqualFold(h.Kind, "Founder") {
			add(PersonMap{Workbook: h.Holder, Sheet: "Funding", Row: h.Row, Contact: personName(h.Holder), Director: true, Shareholder: true})
		}
	}
	role := map[string]string{} // the workbook's account to the role of the Fyniq account it maps onto
	for _, a := range m.Accounts {
		role[a.Workbook] = a.Role
	}
	named := map[string]map[string]bool{} // a personal account's people, by the workbook's account
	name := func(account, person string, o Origin) {
		r := role[codeOf(account)]
		if !personal(r) || strings.TrimSpace(person) == "" {
			return
		}
		add(PersonMap{Workbook: person, Sheet: o.Sheet, Row: o.Row, Contact: personName(person), Director: r != ledger.RoleShareApplicationMonies,
			Shareholder: true})
		if named[codeOf(account)] == nil {
			named[codeOf(account)] = map[string]bool{}
		}
		named[codeOf(account)][personName(person)] = true
	}
	for _, x := range w.Transactions {
		name(x.Category, x.Contact, x.Origin)
	}
	for _, l := range w.Journal {
		name(l.Account, l.Contact, l.Origin)
	}
	var founders []string
	for _, p := range out {
		if p.Director && !slices.Contains(founders, p.Contact) {
			founders = append(founders, p.Contact)
		}
	}
	for i, a := range m.Accounts {
		if !personal(a.Role) || !a.HasOpening || a.OpeningPerson != "" {
			continue
		}
		switch ns := slices.Collect(maps.Keys(named[a.Workbook])); {
		case len(ns) == 1:
			m.Accounts[i].OpeningPerson = ns[0]
		case len(ns) == 0 && len(founders) == 1:
			m.Accounts[i].OpeningPerson = founders[0]
		}
	}
	return out
}

// Check is everything the mapping gets wrong against t (design D4): an error stops the sign-off and the commit.
func (m Mapping) Check(t Target) []Problem {
	var out []Problem
	add := func(sheet string, row int, column, msg, fix string) {
		out = append(out, Problem{Sheet: sheet, Row: row, Column: column, Severity: Error, Code: CodeMapping, Message: msg, Fix: fix})
	}
	created := map[string]storage.AccountType{}
	for _, a := range m.Accounts {
		want := typeOf(a.WorkbookType)
		switch a.Action {
		case Existing:
			x, ok := t.Accounts[a.Fyniq]
			switch {
			case !ok:
				add("Accounts", a.Row, "Code", fmt.Sprintf("account %s maps onto %q, which Fyniq's chart does not have", a.Workbook, a.Fyniq),
					"Map it onto an account the chart has, or create it")
			case want != "" && x.Type != want:
				add("Accounts", a.Row, "Type", fmt.Sprintf("account %s is %s in the workbook; Fyniq's %s is %s", a.Workbook, want, a.Fyniq, x.Type),
					"Map it onto an account of the same type, or create one")
			}
		case Create:
			if _, ok := t.Accounts[a.Fyniq]; ok {
				add("Accounts", a.Row, "Code", fmt.Sprintf("account %s would be created as %s, which Fyniq's chart has already", a.Workbook, a.Fyniq),
					"Map it onto that account, or create it under another code")
				continue
			}
			if _, ok := created[a.Fyniq]; ok {
				add("Accounts", a.Row, "Code", fmt.Sprintf("two accounts would be created as %s", a.Fyniq), "Create each under its own code")
				continue
			}
			created[a.Fyniq] = storage.AccountType(a.Type)
			n := storage.NewAccount{Code: a.Fyniq, Name: a.Name, Type: storage.AccountType(a.Type), AccountAttributes: storage.AccountAttributes{
				ReportGroup: a.ReportGroup, CashFlowLine: a.CashFlowLine, Function: a.Function, MoneyAccount: a.MoneyAccount, DefaultGSTCode: a.DefaultGST}}
			for _, p := range chart.CheckAccount(n) {
				add("Accounts", a.Row, p.Field, fmt.Sprintf("account %s cannot be created: %s", a.Workbook, p.Message), p.Fix)
			}
		default:
			add("Accounts", a.Row, "Code", fmt.Sprintf("account %s has no action", a.Workbook), "Map it onto an account (existing) or create it (create)")
		}
	}
	has := func(code string) (storage.AccountType, bool) {
		if x, ok := t.Accounts[code]; ok {
			return x.Type, true
		}
		typ, ok := created[code]
		return typ, ok
	}
	for _, c := range m.GSTCodes {
		if !t.GST[c.Fyniq] {
			add("Lists", c.Row, "GST code", fmt.Sprintf("GST code %s maps onto %q, which the sg-gst pack does not have", c.Workbook, c.Fyniq),
				"Map it onto one of the pack's codes, such as SR, TX or NA")
		}
	}
	for _, set := range []struct {
		kind, sheet string
		vs          []ValueMap
		have        map[string]string
	}{{storage.DimensionDepartment, "Employees", m.Departments, t.Departments}, {storage.DimensionProject, "Transactions", m.Projects, t.Projects}} {
		kind, sheet, vs, have := set.kind, set.sheet, set.vs, set.have
		codes := map[string]bool{}
		for _, c := range have {
			codes[c] = true
		}
		var others []storage.DimensionValue
		for _, v := range vs {
			switch v.Action {
			case Existing:
				if !codes[v.Code] {
					add(sheet, 0, kind, fmt.Sprintf("the %s %q maps onto %q, which Fyniq does not have", kind, v.Workbook, v.Code),
						"Map it onto a value Fyniq has, or create it")
				}
			case Create:
				dim := &storage.Dimension{Kind: kind, Name: kind, Active: true}
				for _, p := range dimension.NewValue(storage.NewDimensionValue{Code: v.Code, Name: v.Workbook}, dim, others) {
					add(sheet, 0, kind, fmt.Sprintf("the %s %q cannot be created: %s", kind, v.Workbook, p.Message), p.Fix)
				}
				if codes[v.Code] {
					add(sheet, 0, kind, fmt.Sprintf("the %s %q would be created as %s, which Fyniq has already", kind, v.Workbook, v.Code),
						"Map it onto that value, or create it under another code")
				}
				others = append(others, storage.DimensionValue{Code: v.Code, Name: v.Workbook, Active: true})
			default:
				add(sheet, 0, kind, fmt.Sprintf("the %s %q has no action", kind, v.Workbook), "Map it onto a value (existing) or create it (create)")
			}
		}
	}
	for _, l := range m.BudgetLines {
		typ, ok := has(l.Account)
		switch {
		case l.Account == "":
			add("Budget", 0, "P&L line", fmt.Sprintf("the budget line %q goes to no account", l.Line), "Map the line onto a revenue or expense account")
		case !ok:
			add("Budget", 0, "P&L line", fmt.Sprintf("the budget line %q goes to %q, which is not an account", l.Line, l.Account), "Map it onto an account of the chart")
		case typ != storage.Revenue && typ != storage.Expense:
			add("Budget", 0, "P&L line", fmt.Sprintf("the budget line %q goes to %s, a %s account", l.Line, l.Account, typ), "Map it onto a revenue or expense account")
		}
	}
	contacts := map[string]bool{}
	for _, p := range m.People {
		if strings.TrimSpace(p.Contact) == "" || len([]rune(p.Contact)) > 200 {
			add(cmp.Or(p.Sheet, "Funding"), p.Row, "Name", fmt.Sprintf("%q becomes no contact", p.Workbook), "Give the person's legal name, up to 200 characters")
			continue
		}
		contacts[p.Contact] = true
	}
	for _, a := range m.Accounts {
		switch {
		case !personal(a.Role) || !a.HasOpening:
		case a.OpeningPerson == "":
			add("Accounts", a.Row, "Opening balance", fmt.Sprintf("account %s's opening balance is on %s, a %s account, and names no person", a.Workbook, a.Fyniq, a.Role),
				"Name the director or shareholder whose balance it is, one of the mapping's people")
		case !contacts[a.OpeningPerson]:
			add("Accounts", a.Row, "Opening balance", fmt.Sprintf("account %s's opening balance names %q, who is not one of the mapping's people", a.Workbook, a.OpeningPerson),
				"Name one of the mapping's people")
		}
	}
	for _, n := range m.Natures {
		switch {
		case !slices.Contains(billdoc.Natures, n.Nature):
			add("Contacts", n.Row, "Country", fmt.Sprintf("%s is resident for tax in %s, and its bills name no nature of payment", n.Contact, n.Country),
				"Choose the nature of payment its bills are for, such as services performed outside Singapore, for the tax agent's withholding decision (FR-AP-19)")
		case !slices.Contains(billdoc.Outcomes, n.Outcome) || strings.TrimSpace(n.TaxAgent) == "" || (n.Outcome == billdoc.OutcomeNotApplicable) != (n.RatePercent == ""):
			add("Contacts", n.Row, "Country", fmt.Sprintf("%s's bills name no tax agent's decision on withholding", n.Contact),
				"Enter the tax agent's decision: not applicable, or withhold at the rate they advised, with their name (FR-AP-19)")
		}
	}
	for _, r := range m.BankRules {
		if _, ok := has(r.Account); !ok {
			add("Lists", 0, "…use category", fmt.Sprintf("bank rule %d (%q) gives %q, which is not an account", r.Order, r.Contains, r.Account),
				"Map the rule onto an account of the chart")
		}
		if r.GST != "" && !t.GST[r.GST] {
			add("Lists", 0, "GST code (optional)", fmt.Sprintf("bank rule %d gives GST code %q, which the sg-gst pack does not have", r.Order, r.GST),
				"Use one of the pack's codes, or none")
		}
	}
	return out
}

// Change is a person's change to the mapping: each entry replaces the one with its key (the workbook's code or name,
// the budget line, the bank rule's order).
type Change struct {
	Accounts    []AccountMap `json:"accounts,omitempty"`
	GSTCodes    []CodeMap    `json:"gst_codes,omitempty"`
	Departments []ValueMap   `json:"departments,omitempty"`
	Projects    []ValueMap   `json:"projects,omitempty"`
	BudgetLines []LineMap    `json:"budget_lines,omitempty"`
	BankRules   []RuleMap    `json:"bank_rules,omitempty"`
	People      []PersonMap  `json:"people,omitempty"`
	Natures     []NatureMap  `json:"natures,omitempty"`
}

// Apply is the mapping with c applied, or what c names that the mapping does not hold.
func (m Mapping) Apply(c Change) (Mapping, []Problem) {
	var out []Problem
	missing := func(what, key string) {
		out = append(out, Problem{Severity: Error, Code: CodeMapping, Message: fmt.Sprintf("the mapping has no %s %q", what, key),
			Fix: "Change only what the mapping lists"})
	}
	next := Mapping{Format: m.Format, Accounts: slices.Clone(m.Accounts), GSTCodes: slices.Clone(m.GSTCodes), Departments: slices.Clone(m.Departments),
		Projects: slices.Clone(m.Projects), BudgetLines: slices.Clone(m.BudgetLines), BankRules: slices.Clone(m.BankRules), People: slices.Clone(m.People),
		Natures: slices.Clone(m.Natures)}
	for _, a := range c.Accounts {
		i := slices.IndexFunc(next.Accounts, func(x AccountMap) bool { return x.Workbook == a.Workbook })
		if i < 0 {
			missing("account", a.Workbook)
			continue
		}
		was := next.Accounts[i]
		a.Name, a.WorkbookType, a.Row, a.HasOpening = was.Name, was.WorkbookType, was.Row, was.HasOpening
		if a.Action == Existing && a.Fyniq == was.Fyniq {
			a.Role = was.Role
		} else if a.Action == Create {
			a.Role = ""
		}
		next.Accounts[i] = a
	}
	for _, g := range c.GSTCodes {
		i := slices.IndexFunc(next.GSTCodes, func(x CodeMap) bool { return x.Workbook == g.Workbook })
		if i < 0 {
			missing("GST code", g.Workbook)
			continue
		}
		next.GSTCodes[i].Fyniq = g.Fyniq
	}
	for _, set := range []struct {
		what    string
		changes []ValueMap
		into    []ValueMap
	}{{"department", c.Departments, next.Departments}, {"project", c.Projects, next.Projects}} {
		for _, v := range set.changes {
			i := slices.IndexFunc(set.into, func(x ValueMap) bool { return x.Workbook == v.Workbook })
			if i < 0 {
				missing(set.what, v.Workbook)
				continue
			}
			set.into[i] = v
		}
	}
	for _, l := range c.BudgetLines {
		i := slices.IndexFunc(next.BudgetLines, func(x LineMap) bool { return x.Line == l.Line })
		if i < 0 {
			missing("budget line", l.Line)
			continue
		}
		next.BudgetLines[i].Account = l.Account
	}
	for _, n := range c.Natures {
		i := slices.IndexFunc(next.Natures, func(x NatureMap) bool { return x.Contact == n.Contact })
		if i < 0 {
			missing("supplier", n.Contact)
			continue
		}
		next.Natures[i].Nature, next.Natures[i].Outcome, next.Natures[i].RatePercent, next.Natures[i].TaxAgent = n.Nature, n.Outcome, n.RatePercent,
			strings.TrimSpace(n.TaxAgent)
	}
	for _, p := range c.People {
		i := slices.IndexFunc(next.People, func(x PersonMap) bool { return x.Workbook == p.Workbook })
		if i < 0 {
			missing("person", p.Workbook)
			continue
		}
		next.People[i].Contact, next.People[i].Director, next.People[i].Shareholder = strings.TrimSpace(p.Contact), p.Director, p.Shareholder
	}
	for _, r := range c.BankRules {
		i := slices.IndexFunc(next.BankRules, func(x RuleMap) bool { return x.Order == r.Order })
		if i < 0 {
			missing("bank rule", fmt.Sprint(r.Order))
			continue
		}
		next.BankRules[i].Account, next.BankRules[i].GST = r.Account, r.GST
	}
	return next, out
}

// Sealed is the mapping as it is kept, and its SHA-256: what the sign-off names.
func (m Mapping) Sealed() ([]byte, string, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, "", err
	}
	s := sha256.Sum256(b)
	return b, hex.EncodeToString(s[:]), nil
}
