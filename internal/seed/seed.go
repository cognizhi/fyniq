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
	"time"
)

// Schema names the output format.
const Schema = "fyniq-sample-company@1"

// Vocabulary is the part of sample-data.json the generator draws from.
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
}

// Contact is a customer or vendor.
type Contact struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Short string `json:"short,omitempty"`
}

// Person is someone at the sample company.
type Person struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	IDTail string `json:"id_tail,omitempty"`
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

	codes := sortedKeys(v.Accounts)
	for _, code := range codes {
		c.Accounts = append(c.Accounts, Account{Code: code, Name: v.Accounts[code][0]})
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
	for i, n := range people {
		c.People = append(c.People, Person{ID: fmt.Sprintf("P%04d", i+1), Name: n, Role: v.People[n].Role, IDTail: v.People[n].ID})
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
	for i := range c.Journals {
		c.Journals[i].ID = fmt.Sprintf("JNL-%04d", i+1)
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
