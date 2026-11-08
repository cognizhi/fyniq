// Package seed is the one sample-company generator (DEV-04). Local seed data, the golden
// fixtures, the in-app sandbox and the AI golden set all come from it, so they never drift
// apart. Every name it writes comes from docs/design/screens/sample-data.json, the synthetic
// sample company the screens show; nothing comes from real people or companies (DEV-08).
// Amounts are generated as integer cents and written as decimal strings (golden rule 1).
package seed

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
	"time"
)

// Schema names the output format.
const Schema = "fyniq-sample-company@1"

// Vocabulary is the part of sample-data.json the generator draws from.
// Placement is the report group and cash-flow line the statements place an account by (FR-COA-02).
type Placement struct{ ReportGroup, CashFlowLine string }

// ExtraPlacements are the sample company's balance-sheet accounts the standard chart does not hold, placed as the
// standard chart places their kind: without them its balance sheet would not balance, each in no section (design D3
// of p1-reports). Its other extra accounts are expenses, which the profit and loss always places.
var ExtraPlacements = map[string]Placement{
	"1350": {"Other current assets", "Other current assets"}, // due from related companies
	"2050": {"Payables & accruals", "Payables & accruals"},   // accrued expenses
	"2115": {"Payroll liabilities", "Payroll liabilities"},   // EPF payable
	"2116": {"Payroll liabilities", "Payroll liabilities"},   // SOCSO and EIS payable
	"2350": {"Payables & accruals", "Payables & accruals"},   // due to related companies
}

type Vocabulary struct {
	Accounts map[string][]string `json:"accounts"` // code → [name, aliases…]
	Contacts map[string][]string `json:"contacts"` // name → [aliases…]
	People   map[string]struct {
		Role string `json:"role"`
		ID   string `json:"id"` // masked identifier tail, already synthetic
	} `json:"people"`
}

// LoadVocabulary reads sample-data.json.
func LoadVocabulary(path string) (*Vocabulary, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the sample data at %s: %w (run from the repository root or pass --vocab)", path, err)
	}
	var v Vocabulary
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("%s is not valid sample data: %w", path, err)
	}
	if len(v.Accounts) == 0 || len(v.Contacts) == 0 || len(v.People) == 0 {
		return nil, fmt.Errorf("%s has no accounts, contacts or people", path)
	}
	return &v, nil
}

// Company is the generated sample company.
type Company struct {
	Schema       string    `json:"schema"`
	Seed         uint64    `json:"seed"`
	AsOf         string    `json:"as_of"`
	Organisation Org       `json:"organisation"`
	Accounts     []Account `json:"accounts"`
	Contacts     []Contact `json:"contacts"`
	People       []Person  `json:"people"`
	Journals     []Journal `json:"journals"`
}

// Org is the organisation itself.
type Org struct {
	Name         string `json:"name"`
	BaseCurrency string `json:"base_currency"`
}

// Account is one ledger account.
type Account struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Role string `json:"role,omitempty"` // "share-capital" for 3010 and 3020 (INV-13); a director's account's for 2120 and 2500
}

// ShareCapital are the default chart's share-capital accounts. Only the register of share issues and
// opening balances post to them, never a manual journal (INV-13, PRD-001 rev 0.24), so the sample
// company's journals leave them out.
var ShareCapital = map[string]bool{"3010": true, "3020": true}

// Instruments are the default chart's accounts that hold each SAFE's or note's money under its instrument: 2700
// Convertible notes / SAFE (FR-EQ-18, FY292). The sample company records no SAFE, so its journals leave it out.
var Instruments = map[string]bool{"2700": true}

// DirectorAccounts are the default chart's director's accounts, 2500 Loan from director / shareholder and 2120 Deferred
// remuneration payable, with their roles: every line on one names its director and where its amount came from
// (FR-EQ-01, B.7; PRD-001 rev 0.167).
var DirectorAccounts = map[string]string{"2120": "deferred-remuneration", "2500": "director-loan"}

// Contact is a customer or vendor.
type Contact struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Short string `json:"short,omitempty"`
}

// Person is someone at the sample company.
type Person struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	IDTail   string `json:"id_tail,omitempty"`
	Director bool   `json:"director,omitempty"` // a director and shareholder, who holds a director account (FR-EQ-01)
}

// Journal is a balanced manual journal, the first posting source (p0-money-posting-engine).
type Journal struct {
	ID    string `json:"id"`
	Date  string `json:"date"`
	Lines []Line `json:"lines"`
}

// Line is one journal line; exactly one of Debit and Credit is non-zero.
type Line struct {
	Account string `json:"account"`
	Debit   string `json:"debit"`
	Credit  string `json:"credit"`
	// On a director's account only: the director it belongs to (a Person's ID) and where the amount came from (FR-EQ-01).
	Person string `json:"person,omitempty"`
	Origin string `json:"origin,omitempty"`
}

// OrganisationName is the sample company's name, as on every screen.
const OrganisationName = "Cognizhi Pte Ltd"

// AsOf is the moment every screen shows (sample-data.json "clock").
var AsOf = time.Date(2026, 10, 13, 9, 12, 0, 0, time.FixedZone("SGT", 8*3600))

// Generate builds the sample company for seed. The same seed always gives the same company.
func Generate(v *Vocabulary, seed uint64, journals int) Company {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	c := Company{Schema: Schema, Seed: seed, AsOf: AsOf.Format(time.RFC3339),
		Organisation: Org{Name: OrganisationName, BaseCurrency: "SGD"}}

	var codes []string // the accounts a manual journal may post to
	for _, code := range sortedKeys(v.Accounts) {
		a := Account{Code: code, Name: v.Accounts[code][0]}
		switch {
		case ShareCapital[code]:
			a.Role = "share-capital"
		case Instruments[code]: // in the chart, in no journal
		default:
			a.Role = DirectorAccounts[code]
			codes = append(codes, code)
		}
		c.Accounts = append(c.Accounts, a)
	}
	for i, name := range sortedKeys(v.Contacts) {
		ct := Contact{ID: fmt.Sprintf("C%04d", i+1), Name: name}
		if len(v.Contacts[name]) > 0 {
			ct.Short = v.Contacts[name][0]
		}
		c.Contacts = append(c.Contacts, ct)
	}
	people := make([]string, 0, len(v.People))
	for n := range v.People {
		people = append(people, n)
	}
	sort.Strings(people)
	var directors []string // the people a line on a director's account names, in turn
	for i, n := range people {
		p := Person{ID: fmt.Sprintf("P%04d", i+1), Name: n, Role: v.People[n].Role, IDTail: v.People[n].ID,
			Director: strings.Contains(v.People[n].Role, "director")}
		if p.Director {
			directors = append(directors, p.ID)
		}
		c.People = append(c.People, p)
	}

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range journals {
		day := start.AddDate(0, 0, r.IntN(270)) // January to September 2026
		n := 2 + r.IntN(2)                      // two or three lines
		var lines []Line
		var total int64
		for range n - 1 {
			cents := int64(100 + r.IntN(500_000_00)) // S$1.00 to S$500,000.99
			total += cents
			lines = append(lines, Line{Account: codes[r.IntN(len(codes))], Debit: decimal(cents), Credit: "0.00"})
		}
		lines = append(lines, Line{Account: codes[r.IntN(len(codes))], Debit: "0.00", Credit: decimal(total)})
		c.Journals = append(c.Journals, Journal{ID: fmt.Sprintf("JNL-%04d", i+1), Date: day.Format("2006-01-02"), Lines: lines})
	}
	sort.SliceStable(c.Journals, func(i, j int) bool { return c.Journals[i].Date < c.Journals[j].Date })
	turn := 0
	for i := range c.Journals {
		c.Journals[i].ID = fmt.Sprintf("JNL-%04d", i+1)
		// A line on a director's account names a director in turn, and its origin by its side and account: drawing no
		// random number, so the rest of the company is as it was before director's accounts named their person.
		for k, l := range c.Journals[i].Lines {
			if DirectorAccounts[l.Account] == "" || len(directors) == 0 {
				continue
			}
			l.Person = directors[turn%len(directors)]
			turn++
			switch {
			case l.Debit != "0.00":
				l.Origin = "paid_out"
			case l.Account == "2120":
				l.Origin = "pay"
			default:
				l.Origin = "lent"
			}
			c.Journals[i].Lines[k] = l
		}
	}
	return c
}

// decimal formats integer cents as a decimal string with two places.
func decimal(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
