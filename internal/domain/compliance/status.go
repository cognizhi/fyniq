package compliance

import (
	"fmt"
	"strings"
	"time"

	registry "github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Inputs are what a control's status is worked out from, for one organisation (design D1): the release the platform
// runs, the platform jobs' slots, the organisation's catalogue checks, its verifications' latest runs and its reviews.
type Inputs struct {
	Release Release
	Assets  registry.ReleaseAssets
	Jobs    map[string][]storage.JobSlot  // by job
	Checks  map[string]storage.CheckState // by key
	Runs    map[string]storage.ControlRun // by RunKey
	Reviews map[string]Review             // by kind
	Now     time.Time
	// Installation: the organisation is the installation's own, where the platform's verifications run.
	Installation bool
}

// RunKey names a verification's latest run among Inputs.Runs.
func RunKey(control, frequency, how string) string { return control + " " + frequency + " " + how }

// CheckResult is what one check of a control says.
type CheckResult struct {
	Frequency string
	How       string
	Result
	Detective bool // a job, a catalogue check, a verification or an attestation: it can raise a notice (design D5)
}

// ControlStatus is a control's status for an organisation: the worst of its checks', and each check's.
type ControlStatus struct {
	Control string
	Status  Status
	Detail  string
	Checks  []CheckResult
}

// order is the order of the worst-of rule: fail, overdue, not assessed, pass; not applicable only when every check is.
var order = map[Status]int{Fail: 4, Overdue: 3, NotAssessed: 2, Pass: 1, NotApplicable: 0}

// StatusOf works out a control's status from its checks (design D1).
func StatusOf(c registry.Control, in Inputs) ControlStatus {
	out := ControlStatus{Control: c.ID, Status: NotApplicable}
	worst := -1
	for _, ch := range c.Checks {
		r := CheckResult{Frequency: ch.Frequency, How: ch.How, Result: checkResult(c, ch, in)}
		kind, _ := ch.Kind()
		r.Detective = ch.Status == "running" && (kind == registry.KindJob || kind == registry.KindCheck || kind == registry.KindVerify ||
			kind == registry.KindAttestation)
		out.Checks = append(out.Checks, r)
		if order[r.Status] > order[out.Status] {
			out.Status, worst = r.Status, len(out.Checks)-1
		}
	}
	switch {
	case out.Status == Pass:
		out.Detail = fmt.Sprintf("Each of its %d check(s) passed.", len(out.Checks))
	case worst >= 0:
		out.Detail = out.Checks[worst].Detail
	default:
		out.Detail = "None of its checks applies here."
	}
	return out
}

// checkResult is what one check says, by its kind and status.
func checkResult(c registry.Control, ch registry.Check, in Inputs) Result {
	switch ch.Status {
	case "planned":
		arrives, _, _ := strings.Cut(ch.Arrives, " ")
		return Result{Status: NotAssessed, Detail: "Not built yet: it arrives with " + strings.TrimRight(arrives, ":;,") + "."}
	case "cloud":
		return Result{Status: NotAssessed, Detail: "Runs in the cloud, which this installation does not run yet."}
	case "manual":
		return Result{Status: NotAssessed, Detail: "Checked by hand, outside Fyniq."}
	}
	kind, name := ch.Kind()
	switch kind {
	case registry.KindMake, registry.KindTest, registry.KindCI:
		return in.Release.CI(ch, in.Assets)
	case registry.KindJob:
		o := registry.JobOutcomeOf(in.Jobs[name], ch.Frequency, in.Now)
		switch {
		case o.Failed:
			return Result{Status: Fail, Detail: "Its latest run, " + o.Slot.Format(time.RFC3339) + ", did not succeed: " + orNothing(o.Detail)}
		case o.Never:
			return Result{Status: Overdue, Detail: "The job has never run."}
		case o.Stale:
			return Result{Status: Overdue, Detail: "The job has not run on time: " + o.Problem + "."}
		}
		return Result{Status: Pass, Detail: "Passed in its latest run, " + o.Slot.Format(time.RFC3339) + "."}
	case registry.KindCheck:
		s, ok := in.Checks[name]
		if !ok {
			return Result{Status: NotAssessed, Detail: "The check has not run for this organisation yet."}
		}
		switch s.Status {
		case "ok":
			return Result{Status: Pass, Detail: orNothing(s.Detail)}
		case "warning", "due":
			return Result{Status: Overdue, Detail: orNothing(s.Detail)}
		case "error", "failed":
			return Result{Status: Fail, Detail: orNothing(s.Detail)}
		}
		return Result{Status: NotAssessed, Detail: "The check needs " + orNothing(s.Needs) + "."}
	case registry.KindVerify:
		r, ok := in.Runs[RunKey(c.ID, ch.Frequency, ch.How)]
		switch {
		case Platform(name) && !in.Installation:
			return Result{Status: NotApplicable, Detail: "Checked for the whole installation, in the installation's own organisation."}
		case !ok:
			return Result{Status: NotAssessed, Detail: "The verification has not run for this organisation yet."}
		case in.Now.Sub(r.RanAt) > 26*time.Hour:
			return Result{Status: Overdue, Detail: "The verification has not run since " + r.RanAt.Format(time.RFC3339) + "."}
		case !r.OK:
			return Result{Status: Fail, Detail: r.Detail}
		}
		return Result{Status: Pass, Detail: r.Detail}
	case registry.KindAttestation:
		rv, ok := in.Reviews[name]
		switch {
		case !ok:
			return Result{Status: NotAssessed, Detail: "The review has not been read for this organisation yet."}
		case rv.Status == ReviewOverdue:
			return Result{Status: Overdue, Detail: rv.Detail}
		case rv.Status == ReviewNotApplicable:
			return Result{Status: NotApplicable, Detail: rv.Detail}
		}
		return Result{Status: Pass, Detail: rv.Detail}
	}
	return Result{Status: NotAssessed, Detail: "Nothing checks it here."}
}

func orNothing(s string) string {
	if s == "" {
		return "nothing more is known"
	}
	return s
}

// Failing reports whether a status is one a notice is about: fail or overdue.
func Failing(s Status) bool { return s == Fail || s == Overdue }

// Notify reports whether a control whose status was before, and is now after, newly fails through a detective check, so
// its owner is told once (design D5). A CI check never notifies: a failing gate blocks the merge and the release.
func Notify(before Status, after ControlStatus) bool {
	if !Failing(after.Status) || Failing(before) {
		return false
	}
	for _, c := range after.Checks {
		if c.Detective && Failing(c.Status) {
			return true
		}
	}
	return false
}
