//go:build local

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
)

// G4-103 and ENG-33's update marking: while the organisation has not accepted a statutory pack's update (a later
// version in force that it has not activated), the checks whose figures read that pack are not assessed, labelled with
// the version they would be based on, never an all clear; once it is activated they are assessed again.
// req: G4-103
// req: ENG-33
func TestAPendingPackUpdateLeavesItsChecksNotAssessed(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Owner, authz.Controller)
	weiling := f.as(f.weiling, authz.Owner, authz.Controller)
	activate := func(version string) {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/statutory-packs/"+version+"/activate", ahkow, nil, "If-Match", `"0"`)
		if a := f.decide(weiling, r.str("id"), "approve", hashOf(r)); a.Status != http.StatusOK || a.str("status") != "approved" {
			t.Fatalf("activating %s: %d %s / %d %s", version, r.Status, r.Raw, a.Status, a.Raw)
		}
	}
	before := e.checked("gst.registration-threshold")
	if d, _ := before.Body["detail"].(string); strings.Contains(d, "update pending") {
		t.Fatalf("an update pending before any activation: %s", before.Raw)
	}

	activate("sg-gst@2026.1") // sg-gst@2026.2 and 2026.3, which correct it, are loaded and not activated
	c := e.checked("gst.registration-threshold")
	wantStatus(t, c, "not_assessed")
	if d, _ := c.Body["detail"].(string); d != "Not assessed: based on sg-gst@2026.1; update pending: sg-gst@2026.3 is in force and not activated" {
		t.Errorf("the check while sg-gst's update is pending: %s", c.Raw)
	}
	if other := e.checked("compliance.deadlines"); strings.Contains(other.Raw, "update pending") {
		t.Errorf("a check that reads no sg-gst is marked: %s", other.Raw)
	}

	activate("sg-gst@2026.3")
	if after := e.checked("gst.registration-threshold"); strings.Contains(after.Raw, "update pending") {
		t.Errorf("still pending once the update is activated: %s", after.Raw)
	}
}
