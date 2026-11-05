package dimension

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func codes(p []Problem) string {
	var out []string
	for _, x := range p {
		out = append(out, x.Code+" "+x.Field)
	}
	return strings.Join(out, "; ")
}

// FR-DIM-01: Department and Project once each, and up to 8 custom dimensions, each taking the first free place, one out
// of use keeping its own; every dimension named once, whatever the case.
// req: FR-DIM-01
func TestDimensionsAreDepartmentProjectAndUpToEightCustom(t *testing.T) {
	dept := storage.Dimension{ID: uuid.New(), Kind: storage.DimensionDepartment, Name: "Department", Active: true}
	grant := storage.Dimension{ID: uuid.New(), Kind: storage.DimensionCustom, Place: 1, Name: "Grant"}
	region := storage.Dimension{ID: uuid.New(), Kind: storage.DimensionCustom, Place: 3, Name: "Region", Active: true}
	have := []storage.Dimension{dept, grant, region}

	if n, p := New(storage.DimensionProject, "  Project ", have); len(p) > 0 || n.Name != "Project" || n.Place != 0 {
		t.Errorf("Project: %+v %s", n, codes(p))
	}
	if n, p := New(storage.DimensionCustom, "Product line", have); len(p) > 0 || n.Place != 2 {
		t.Errorf("a custom dimension takes place 2, as Grant out of use keeps place 1: %+v %s", n, codes(p))
	}
	if _, p := New(storage.DimensionDepartment, "Teams", have); codes(p) != "DIM-01 kind" {
		t.Errorf("a second Department: %s", codes(p))
	}
	if _, p := New("cost-centre", "", have); codes(p) != "DIM-01 kind; DIM-02 name" {
		t.Errorf("an unknown kind with no name: %s", codes(p))
	}
	if _, p := New(storage.DimensionCustom, "region", have); codes(p) != "DIM-02 name" {
		t.Errorf("Region again, in another case: %s", codes(p))
	}
	if _, p := New(storage.DimensionCustom, strings.Repeat("x", MaxName+1), have); codes(p) != "DIM-02 name" {
		t.Errorf("a name too long: %s", codes(p))
	}

	full := []storage.Dimension{dept}
	for place := 1; place <= MaxCustom; place++ {
		full = append(full, storage.Dimension{ID: uuid.New(), Kind: storage.DimensionCustom, Place: place, Name: strings.Repeat("c", place)})
	}
	if _, p := New(storage.DimensionCustom, "Ninth", full); codes(p) != "DIM-01 kind" {
		t.Errorf("a ninth custom dimension: %s", codes(p))
	}

	if p := Change(region, storage.DimensionChange{Name: "REGION", Active: false}, have); len(p) > 0 {
		t.Errorf("Region renamed in its own name's other case: %s", codes(p))
	}
	if p := Change(region, storage.DimensionChange{Name: "grant", Active: true}, have); codes(p) != "DIM-02 name" {
		t.Errorf("Region renamed to Grant: %s", codes(p))
	}
}

// FR-DIM-01: a value belongs to a dimension in use, with a code unique in it and a name; a change keeps its code.
// req: FR-DIM-01
func TestAValueHasACodeUniqueInItsDimension(t *testing.T) {
	dept := storage.Dimension{ID: uuid.New(), Kind: storage.DimensionDepartment, Name: "Department", Active: true}
	eng := storage.DimensionValue{ID: uuid.New(), DimensionID: dept.ID, Code: "ENG", Name: "Engineering", Active: true}
	sales := storage.DimensionValue{ID: uuid.New(), DimensionID: dept.ID, Code: "SALES", Name: "Sales"}
	have := []storage.DimensionValue{eng, sales}

	if p := NewValue(storage.NewDimensionValue{DimensionID: dept.ID, Code: "OPS", Name: "Operations"}, &dept, have); len(p) > 0 {
		t.Errorf("Operations: %s", codes(p))
	}
	if p := NewValue(storage.NewDimensionValue{DimensionID: dept.ID, Code: "eng", Name: "Engineering"}, &dept, have); codes(p) != "DIM-03 code; DIM-02 name" {
		t.Errorf("ENG again: %s", codes(p))
	}
	for _, bad := range []string{"", "-ENG", "R&D", "with space", strings.Repeat("A", 31)} {
		if p := NewValue(storage.NewDimensionValue{DimensionID: dept.ID, Code: bad, Name: "x"}, &dept, have); codes(p) != "DIM-03 code" {
			t.Errorf("code %q: %s", bad, codes(p))
		}
	}
	for _, good := range []string{"P-2026-01", "EDG.2026", "A/B", "x_1", strings.Repeat("A", 30)} {
		if p := NewValue(storage.NewDimensionValue{DimensionID: dept.ID, Code: good, Name: good}, &dept, have); len(p) > 0 {
			t.Errorf("code %q: %s", good, codes(p))
		}
	}
	if p := NewValue(storage.NewDimensionValue{Code: "X", Name: "x"}, nil, nil); codes(p) != "DIM-04 dimension_id" {
		t.Errorf("no such dimension: %s", codes(p))
	}
	out := dept
	out.Active = false
	if p := NewValue(storage.NewDimensionValue{DimensionID: dept.ID, Code: "X", Name: "x"}, &out, nil); codes(p) != "DIM-04 dimension_id" {
		t.Errorf("a dimension out of use: %s", codes(p))
	}
	if p := ChangeValue(sales, storage.DimensionChange{Name: "Sales & marketing", Active: true}, have); len(p) > 0 {
		t.Errorf("Sales renamed: %s", codes(p))
	}
	if p := ChangeValue(sales, storage.DimensionChange{Name: " "}, have); codes(p) != "DIM-02 name" {
		t.Errorf("no name: %s", codes(p))
	}
}
