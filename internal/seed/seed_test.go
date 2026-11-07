package seed

import (
	"bytes"
	"encoding/json"
	"math/big"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

func vocab(t *testing.T) *Vocabulary {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	v, err := LoadVocabulary(filepath.Join(filepath.Dir(file), "..", "..", "docs", "design", "screens", "sample-data.json"))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func render(t *testing.T, c Company) []byte {
	t.Helper()
	b, err := json.MarshalIndent(c, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// req: DEV-04
func TestSameSeedSameCompany(t *testing.T) {
	v := vocab(t)
	a, b := render(t, Generate(v, 42, 25)), render(t, Generate(v, 42, 25))
	if !bytes.Equal(a, b) {
		t.Fatal("two runs with seed 42 differ")
	}
	if bytes.Equal(a, render(t, Generate(v, 43, 25))) {
		t.Fatal("seeds 42 and 43 gave the same company")
	}
}

// req: DEV-04
func TestJournalsBalance(t *testing.T) {
	for _, j := range Generate(vocab(t), 7, 50).Journals {
		var dr, cr big.Rat
		for _, l := range j.Lines {
			d, _ := new(big.Rat).SetString(l.Debit)
			c, _ := new(big.Rat).SetString(l.Credit)
			dr.Add(&dr, d)
			cr.Add(&cr, c)
		}
		if dr.Cmp(&cr) != 0 {
			t.Fatalf("%s does not balance: %s ≠ %s", j.ID, dr.FloatString(2), cr.FloatString(2))
		}
	}
}

// req: DEV-08
func TestOnlySyntheticVocabulary(t *testing.T) {
	v := vocab(t)
	allowed := map[string]bool{Schema: true, OrganisationName: true, "SGD": true, AsOf.Format("2006-01-02T15:04:05Z07:00"): true,
		// The format's account roles (INV-13, FR-EQ-01) and the origins of a director's account's lines: not names.
		"share-capital": true, "director-loan": true, "deferred-remuneration": true, "lent": true, "pay": true, "paid_out": true}
	for code, names := range v.Accounts {
		allowed[code] = true
		for _, n := range names {
			allowed[n] = true
		}
	}
	for name, aliases := range v.Contacts {
		allowed[name] = true
		for _, a := range aliases {
			allowed[a] = true
		}
	}
	for name, p := range v.People {
		allowed[name], allowed[p.Role], allowed[p.ID] = true, true, true
	}
	generated := regexp.MustCompile(`^(C|P)\d{4}$|^JNL-\d{4}$|^2026-\d{2}-\d{2}$|^\d+\.\d{2}$`)

	var walk func(path string, x any)
	walk = func(path string, x any) {
		switch x := x.(type) {
		case map[string]any:
			for k, val := range x {
				walk(path+"."+k, val)
			}
		case []any:
			for _, val := range x {
				walk(path, val)
			}
		case string:
			if !allowed[x] && !generated.MatchString(x) {
				t.Errorf("%s = %q is neither in sample-data.json nor a generated ID, date or amount", path, x)
			}
		}
	}
	var out any
	if err := json.Unmarshal(render(t, Generate(v, 1, 30)), &out); err != nil {
		t.Fatal(err)
	}
	walk("company", out)
}

// The default chart marks 3010 and 3020 as share capital, and no sample journal posts to them: share
// capital changes only through the register of share issues or opening balances (INV-13, rev 0.24). Nor does one post
// to 2700, whose every line names a SAFE or note the sample company does not record (FR-EQ-18).
// req: INV-13
// req: FR-EQ-18
// req: DEV-04
func TestTheSampleJournalsLeaveShareCapitalAlone(t *testing.T) {
	c := Generate(vocab(t), 7, 400)
	marked := map[string]bool{}
	for _, a := range c.Accounts {
		if a.Role == "share-capital" {
			marked[a.Code] = true
		}
	}
	if !marked["3010"] || !marked["3020"] || len(marked) != 2 {
		t.Fatalf("share-capital accounts: %v; want 3010 and 3020", marked)
	}
	for _, j := range c.Journals {
		for _, l := range j.Lines {
			if marked[l.Account] || l.Account == "2700" {
				t.Fatalf("%s posts to %s", j.ID, l.Account)
			}
		}
	}
}

// Every line on a director's account names a director of the sample company and an origin, and no other line does
// (FR-EQ-01, B.7; PRD-001 rev 0.167).
// req: FR-EQ-01
func TestTheSampleDirectorLinesNameTheirDirector(t *testing.T) {
	c := Generate(vocab(t), 7, 200)
	directors := map[string]bool{}
	for _, p := range c.People {
		if p.Director {
			directors[p.ID] = true
		}
	}
	if len(directors) != 2 {
		t.Fatalf("the sample company has %d directors, not its two founders", len(directors))
	}
	seen := 0
	for _, j := range c.Journals {
		for _, l := range j.Lines {
			switch {
			case DirectorAccounts[l.Account] != "" && (!directors[l.Person] || l.Origin == ""):
				t.Errorf("%s: the line on %s names %q with origin %q", j.ID, l.Account, l.Person, l.Origin)
			case DirectorAccounts[l.Account] == "" && (l.Person != "" || l.Origin != ""):
				t.Errorf("%s: the line on %s names a person or origin", j.ID, l.Account)
			case DirectorAccounts[l.Account] != "":
				seen++
			}
		}
	}
	if seen == 0 {
		t.Fatal("no sample journal posts to a director's account, so the test proves nothing")
	}
}
