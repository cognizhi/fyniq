package pack_test

import (
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/packs"
)

func version(t *testing.T, dir string) pack.Version {
	t.Helper()
	v, err := pack.Load(packs.FS, dir)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The update from sg-gst@2026.2 to 2026.3 adds one thing, the registration threshold, to the limits table (p1-gst 2.1), and changes nothing else: its rows are the old ones plus
// that one, and the same version compared with itself differs in nothing.
// req: ENG-33
func TestTheDiffOfTwoGSTVersionsIsTheThresholdAndNothingElse(t *testing.T) {
	a, b := version(t, "sg-gst/2026.2"), version(t, "sg-gst/2026.3")
	d := pack.Diff(a, b)
	if len(d) != 1 || d[0].Table != "limits" || d[0].Kind != pack.ChangeAdded || d[0].Key != "registration_threshold" || len(d[0].Now) == 0 || len(d[0].Was) != 0 {
		t.Fatalf("the differences are %+v", d)
	}
	if same := pack.Diff(b, b); len(same) != 0 {
		t.Errorf("a version differs from itself: %+v", same)
	}
	rev := pack.Diff(b, a)
	if len(rev) != 1 || rev[0].Kind != pack.ChangeRemoved || rev[0].Key != "registration_threshold" {
		t.Errorf("the other way round: %+v", rev)
	}
}

// A value that moved is a change listing the row that went and the row that came, and a table only one version holds is all added.
// req: ENG-33
func TestTheDiffShowsAChangedValueAndAWholeNewTable(t *testing.T) {
	a, b := version(t, "sg-cpf/2026.1"), version(t, "sg-cpf/2026.2")
	d := pack.Diff(a, b)
	if len(d) == 0 {
		t.Fatal("sg-cpf@2026.2 differs from 2026.1 in nothing")
	}
	for _, c := range d {
		if c.Kind == pack.ChangeChanged && (len(c.Was) == 0 || len(c.Now) == 0) {
			t.Errorf("a change without both sides: %+v", c)
		}
	}
	old := version(t, "sg-gst/2026.1")
	all := pack.Diff(old, version(t, "sg-gst/2026.3"))
	var added bool
	for _, c := range all {
		if c.Table == "limits" && c.Kind == pack.ChangeAdded {
			added = true
		}
	}
	if !added {
		t.Errorf("the limits table, new since 2026.1, is not all added: %+v", all)
	}
}
