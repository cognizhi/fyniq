package ledger

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

var (
	dept    = uuid.MustParse("0192f1a0-0000-7000-8000-0000000d1e01")
	project = uuid.MustParse("0192f1a0-0000-7000-8000-0000000d1e02")
	grant   = uuid.MustParse("0192f1a0-0000-7000-8000-0000000d1e03")
	eng     = uuid.MustParse("0192f1a0-0000-7000-8000-0000000d1e0f") // sorts after ops
	ops     = uuid.MustParse("0192f1a0-0000-7000-8000-0000000d1e0a")
	retired = uuid.MustParse("0192f1a0-0000-7000-8000-0000000d1e0b")
	atlas   = uuid.MustParse("0192f1a0-0000-7000-8000-0000000d1e0c")
	edg     = uuid.MustParse("0192f1a0-0000-7000-8000-0000000d1e0d")
)

// withDimensions is settings with Department (ENG, OPS, and RETIRED out of use), Project (ATLAS), Grant out of use
// (EDG), and sales requiring Department and Grant.
func withDimensions() Settings {
	s := settings
	s.Accounts = map[uuid.UUID]Account{}
	for id, a := range settings.Accounts {
		s.Accounts[id] = a
	}
	tagged := sales
	tagged.Requires = []uuid.UUID{dept, grant}
	s.Accounts[sales.ID] = tagged
	s.Dimensions = map[uuid.UUID]Dimension{dept: {Name: "Department", Active: true}, project: {Name: "Project", Active: true}, grant: {Name: "Grant"}}
	s.Values = map[uuid.UUID]DimensionValue{
		eng: {Dimension: dept, Code: "ENG", Active: true}, ops: {Dimension: dept, Code: "OPS", Active: true},
		retired: {Dimension: dept, Code: "RETIRED"}, atlas: {Dimension: project, Code: "ATLAS", Active: true},
		edg: {Dimension: grant, Code: "EDG", Active: true},
	}
	return s
}

func dimensionCodes(err error) string {
	probs, _ := err.(Problems)
	var out []string
	for _, p := range probs {
		out = append(out, fmt.Sprintf("%s %s", p.Code, p.Field))
	}
	return strings.Join(out, "; ")
}

// FR-DIM-01 and FR-DIM-02 (rev 0.171): a journal's line carries its dimension values, held once each in ascending order,
// at most one of each dimension and each in use; a line a person enters on an account that requires a dimension names
// a value of it (DIM-05), a dimension out of use requiring nothing; opening balances post as entered without the
// requirement; a reversal keeps each line's values, so every set nets to zero; replay sees a changed value.
// req: FR-DIM-01
// req: FR-DIM-02
// req: ENG-22
func TestALinesDimensionValuesAreCheckedAndKept(t *testing.T) {
	s := withDimensions()
	j := sale(t, "100.00")
	j.Lines[1].Values = []uuid.UUID{eng, atlas, eng}
	if _, err := PostJournal(j, s); dimensionCodes(err) != "DIM-06 lines[2].dimension_value_ids" {
		t.Errorf("a value twice: %v", err)
	}
	j.Lines[1].Values = []uuid.UUID{eng, atlas}
	e, err := PostJournal(j, s)
	if err != nil {
		t.Fatalf("Department and Project on the sales line: %v", err)
	}
	if !slices.Equal(e.Lines[1].Values, []uuid.UUID{atlas, eng}) { // ATLAS's ID sorts before ENG's
		t.Errorf("the values in ascending order: %v", e.Lines[1].Values)
	}
	if e.Lines[0].Values != nil {
		t.Errorf("the bank line has none: %v", e.Lines[0].Values)
	}

	for name, c := range map[string]struct {
		values []uuid.UUID
		want   string
	}{
		"no department":          {[]uuid.UUID{atlas}, "DIM-05 lines[2].dimension_value_ids"},
		"none at all":            {nil, "DIM-05 lines[2].dimension_value_ids"},
		"two departments":        {[]uuid.UUID{eng, ops}, "DIM-06 lines[2].dimension_value_ids"},
		"a value out of use":     {[]uuid.UUID{retired}, "DIM-06 lines[2].dimension_value_ids"},
		"a dimension out of use": {[]uuid.UUID{eng, edg}, "DIM-06 lines[2].dimension_value_ids"},
		"a value nobody has":     {[]uuid.UUID{eng, uuid.Nil}, "DIM-06 lines[2].dimension_value_ids"},
	} {
		j := sale(t, "100.00")
		j.Lines[1].Values = c.values
		if _, err := PostJournal(j, s); dimensionCodes(err) != c.want {
			t.Errorf("%s: %q, want %q", name, dimensionCodes(err), c.want)
		}
	}
	_, err = PostJournal(sale(t, "100.00"), s)
	if p := err.(Problems); len(p) != 1 || p[0].Message != "account 4010 needs a value of Department on every line" || p[0].Fix != "Choose the line's Department, or move an amount between values with lines on 4010 that net to zero" {
		t.Errorf("DIM-05's words: %+v", p)
	}

	// A journal that moves an amount between values on the account, its lines on it netting to zero, carries the line with
	// no value it moves from (Appendix C.2 #60); one that leaves the account changed is held to the requirement.
	move := Journal{ID: docID, Version: 1, Date: day, Memo: "Department for the September sales", Lines: []JournalLine{
		{AccountID: sales.ID, Amount: amt(t, "250.00", sgd)},
		{AccountID: sales.ID, Amount: amt(t, "-250.00", sgd), Values: []uuid.UUID{ops}},
	}}
	if _, err := PostJournal(move, s); err != nil {
		t.Errorf("moving an amount to Operations: %v", err)
	}
	partly := move
	partly.Lines = []JournalLine{
		{AccountID: sales.ID, Amount: amt(t, "250.00", sgd)},
		{AccountID: sales.ID, Amount: amt(t, "-200.00", sgd), Values: []uuid.UUID{ops}},
		{AccountID: bank.ID, Amount: amt(t, "-50.00", sgd)},
	}
	if _, err := PostJournal(partly, s); dimensionCodes(err) != "DIM-05 lines[1].dimension_value_ids" {
		t.Errorf("a journal that changes the account's total: %v", err)
	}

	// Opening balances are entered without the requirement; their values are still checked.
	open := sale(t, "100.00")
	if _, err := PostOpening(open, s); err != nil {
		t.Errorf("opening balances without a department: %v", err)
	}
	open.Lines[1].Values = []uuid.UUID{retired}
	if _, err := PostOpening(open, s); dimensionCodes(err) != "DIM-06 lines[2].dimension_value_ids" {
		t.Errorf("an opening line with a value out of use: %v", err)
	}
	// A caller that read no dimensions refuses a line that names a value.
	if _, err := PostJournal(j, settings); !strings.Contains(dimensionCodes(err), "DIM-06") {
		t.Errorf("values without the organisation's dimensions: %v", err)
	}

	r, err := Reverse(e, day, "Wrong month")
	if err != nil || !slices.Equal(r.Lines[1].Values, e.Lines[1].Values) || r.Lines[0].Values != nil {
		t.Errorf("the reversal keeps each line's values: %+v %v", r.Lines, err)
	}
	changed := e
	changed.Lines = slices.Clone(e.Lines)
	changed.Lines[1].Values = []uuid.UUID{ops, atlas}
	if d := Differences(e, changed); len(d) != 1 || !strings.Contains(d[0], "line 2 dimension values") {
		t.Errorf("replay sees the changed value: %v", d)
	}
}
