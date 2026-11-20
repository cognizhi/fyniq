package pack_test

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/packs"
)

// The pack loads, and every pinned case, worked by hand from IRAS's e-Tax Guide, passes.
// req: FR-TAX-10
// req: ENG-30
// req: QA-ENG-08
func TestTheInvoiceNowPackLoadsAndItsPinnedCasesPass(t *testing.T) {
	v, err := pack.Load(packs.FS, "sg-invoicenow/2026.1")
	if err != nil {
		t.Fatal(err)
	}
	rs := pack.RunPinned(v)
	if len(rs) != 9 {
		t.Fatalf("%d pinned cases ran, want 9", len(rs))
	}
	for _, r := range rs {
		if !r.Pass {
			t.Errorf("%s %s: got %q, want %q (%s)", r.Table, r.Case, r.Got, r.Want, r.Why)
		}
	}
}

// Every code of the newest sg-gst version has a treatment in sg-invoicenow, so a code added to the GST pack cannot reach
// the gate unclassified, and a purchase excluded or not reported has a recorded reason.
// req: FR-TAX-10
func TestEveryGSTCodeHasATreatmentForTheRequirement(t *testing.T) {
	gst, err := pack.Load(packs.FS, "sg-gst/2026.3")
	if err != nil {
		t.Fatal(err)
	}
	in, err := pack.Load(packs.FS, "sg-invoicenow/2026.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range gst.Tables["codes"].Rows {
		c, ok := pack.InvoiceNow(in, r[0])
		if !ok {
			t.Errorf("the GST code %s has no row in sg-invoicenow", r[0])
			continue
		}
		if c.Direction != r[1] {
			t.Errorf("%s is a %s in sg-gst and a %s in sg-invoicenow", r[0], r[1], c.Direction)
		}
		// a code the return reports (it has a value box) is submitted, excluded or chosen; one it does not is not reported
		reported := r[3] != ""
		if reported && (c.Scope == pack.ScopeNotReported || c.Scope == pack.ScopeNone) && r[0] != "OS" {
			t.Errorf("%s is reported in box %s but sg-invoicenow says %s", r[0], r[3], c.Scope)
		}
		if !reported && c.Scope == pack.ScopeSubmit {
			t.Errorf("%s is not reported in the return but sg-invoicenow submits it", r[0])
		}
	}
}

// A group's start day, and the phases listed in order from the earliest.
// req: FR-AR-12
func TestThePhasesStartOnIRASDays(t *testing.T) {
	in, err := pack.Load(packs.FS, "sg-invoicenow/2026.1")
	if err != nil {
		t.Fatal(err)
	}
	ps := pack.InvoiceNowPhases(in)
	if len(ps) != 7 || !ps[1].From.Equal(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)) || !ps[6].From.Equal(time.Date(2031, 4, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("the phases are %+v", ps)
	}
	if rs := pack.InvoiceNowReasons(in); len(rs) != 8 || rs[0][0] != "reverse_charge" {
		t.Fatalf("the reasons are %v", rs)
	}
}
