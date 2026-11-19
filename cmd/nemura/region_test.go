package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The release flavour refuses a region outside Singapore and takes Singapore's name, case aside, or none; the local flavour also takes "local".
// A service that is told to run elsewhere does not start (NM-NFR-01).
// req: NM-NFR-01
func TestARegionOutsideSingaporeIsRefused(t *testing.T) {
	env := func(v string) func(string) string { return func(string) string { return v } }
	for _, tc := range []struct {
		flavour, region string
		ok              bool
	}{
		{"release", "", true}, {"release", "asia-southeast1", true}, {"release", " ASIA-SOUTHEAST1 ", true},
		{"release", "us-central1", false}, {"release", "europe-west1", false}, {"release", "asia-southeast2", false}, {"release", "local", false},
		{"local", "local", true}, {"local", "", true}, {"local", "us-central1", false},
	} {
		err := checkRegion(env(tc.region), tc.flavour)
		if (err == nil) != tc.ok || (err != nil && !strings.Contains(err.Error(), "asia-southeast1")) {
			t.Errorf("%s %q: %v", tc.flavour, tc.region, err)
		}
	}
}

// The binary itself refuses to start in another region, with the stable code, before it reads a database.
// req: NM-NFR-01
func TestTheBinaryDoesNotStartOutsideSingapore(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := build(t)
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "NEMURA_ROLE=api", "NEMURA_REGION=us-central1", "NEMURA_DATABASE_URL=postgres://nemura_api:x@127.0.0.1:1/x")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "outside Singapore") || !strings.Contains(string(out), "NEMURA-REGION-01") {
		t.Errorf("the service started in another region: %v %s", err, out)
	}
}
