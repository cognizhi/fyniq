package compliance

import (
	"strings"
	"testing"

	registry "github.com/cognizhi/fyniq/compliance"
)

// A check that runs in CI passes in a release, linking to the release asset that holds its evidence, and is not
// assessed in a development build, which says why (design D3).
// req: CMP-01
func TestCIChecksReadTheReleaseThePlatformRuns(t *testing.T) {
	controls, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	assets, err := registry.LoadReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	const repo = "https://github.com/cognizhi/fyniq"
	release, dev := Release{Tag: "v1.2.0", Repository: repo}, Release{Repository: repo}
	n := 0
	for _, c := range controls {
		for _, ch := range c.Checks {
			if !ch.InCI() {
				continue
			}
			n++
			got := release.CI(ch, assets)
			if got.Status != Pass || !strings.HasPrefix(got.Evidence, repo+"/releases/download/v1.2.0/") ||
				!strings.HasSuffix(got.Evidence, ".zip") || !strings.Contains(got.Detail, "v1.2.0") {
				t.Errorf("%s (%s) %s in a release: %+v", c.ID, ch.Frequency, ch.How, got)
			}
			if got := dev.CI(ch, assets); got.Status != NotAssessed || got.Evidence != "" || !strings.Contains(got.Detail, "development build") {
				t.Errorf("%s (%s) %s in a development build: %+v", c.ID, ch.Frequency, ch.How, got)
			}
		}
	}
	if n < 20 {
		t.Fatalf("only %d checks run in CI", n)
	}
	for how, want := range map[string]string{"make test": "test-report.zip", "test TestAuthorisationMatrix": "test-report.zip",
		"make scan": "security-scan-reports.zip", "make images": "image-reports.zip", "make differential": "ci-logs.zip",
		"ci Repository settings and merge audit (CTL-CHG-01)": "control-ctl-chg-01.zip"} {
		got := release.CI(registry.Check{Frequency: "CI", Status: "running", How: how}, assets)
		if got.Evidence != repo+"/releases/download/v1.2.0/"+want {
			t.Errorf("%s: evidence %q, want the release's %s", how, got.Evidence, want)
		}
	}
	// Only a release tag is a release: a version stamped by any other build, or no repository, is a development build.
	for _, r := range []Release{{Tag: "0.0.0-dev", Repository: repo}, {Tag: "v1.2.0-rc.1", Repository: repo}, {Tag: "v1.2.0"},
		{Tag: "v1.2.0/../../x", Repository: repo}} {
		if got := r.CI(registry.Check{Frequency: "CI", Status: "running", How: "make test"}, assets); got.Status != NotAssessed {
			t.Errorf("%+v: %+v, want not assessed", r, got)
		}
	}
}
