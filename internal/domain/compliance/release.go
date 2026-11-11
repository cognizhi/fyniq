// Package compliance works out what the controls of the registry (compliance/, PRD-001 Appendix F) say for an
// organisation: each check's latest result, and from them each control's status on the Compliance Center (CMP-01;
// p1-compliance-center design D1 and D3).
package compliance

import (
	"net/url"
	"regexp"

	registry "github.com/cognizhi/fyniq/compliance"
)

// Status is what a check, or a control, says (CMP-01).
type Status string

// The statuses of CMP-01.
const (
	Pass          Status = "pass"
	Fail          Status = "fail"
	Overdue       Status = "overdue"
	NotApplicable Status = "not_applicable"
	NotAssessed   Status = "not_assessed" // what is missing is named
)

// Result is what a check's latest run says: its status, in words what passed, failed or is missing, and a link to its
// evidence where there is one.
type Result struct {
	Status   Status
	Detail   string
	Evidence string
}

// Release is the release the platform runs: its tag, which only the release image build stamps (version.Release), and
// the repository whose releases hold their evidence. A build without a tag is a development build.
type Release struct {
	Tag        string
	Repository string
}

// tag is a release tag as release-please writes it (include-v-in-tag): anything else is not a release.
var tag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// CI is the result of a check that runs in CI (design D3). No release ships with a failing gate: its evidence job
// attaches the tagged commit's CI evidence to the GitHub release only when that run succeeded. So in a release the
// check passed, with the asset that holds its evidence; a development build carries no release's evidence, and the
// check is not assessed. The dashboard links to the release and never calls GitHub.
func (r Release) CI(c registry.Check, assets registry.ReleaseAssets) Result {
	if !tag.MatchString(r.Tag) || r.Repository == "" {
		return Result{Status: NotAssessed,
			Detail: "A development build: its checks passed in CI, but it carries no release's evidence."}
	}
	return Result{Status: Pass,
		Detail:   "Passed in CI on the commit of release " + r.Tag + ", whose evidence the release holds.",
		Evidence: r.Repository + "/releases/download/" + url.PathEscape(r.Tag) + "/" + url.PathEscape(assets.Asset(c))}
}
