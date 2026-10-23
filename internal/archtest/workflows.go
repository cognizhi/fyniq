package archtest

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The CI supply-chain rules (SEC-28, CTL-CHG-11), checked on every workflow in .github/workflows:
// third-party actions pinned by commit SHA; no token at the workflow level, and each job's token
// limited to read unless the job is in WriteJobs; a job with a write token never runs for a pull
// request's code; a pull_request_target workflow never checks out code or reads secrets. And a step that pipes its
// command (`make test | tee log`) runs it with `shell: bash`, which sets -o pipefail, or sets pipefail itself: GitHub's
// default, bash -e, takes the pipe's last command's status, so a failure before the pipe would pass.

// WriteJobs are the jobs allowed a write token, and the scopes each may write, with why.
var WriteJobs = map[string]struct {
	Scopes []string
	Why    string
}{
	"auto-approve.yml/approve": {[]string{"pull-requests"}, "approves the owner's development pull requests before production (founder decision, 14 Oct 2026)"},
	"infra.yml/plan":           {[]string{"id-token"}, "plans each environment through Workload Identity Federation, which trusts main and tags only (SEC-04, CTL-CHG-02)"},
	"infra.yml/apply":          {[]string{"id-token"}, "applies one environment through Workload Identity Federation, in a GitHub environment that holds production for approval (§12.4)"},
}

var (
	pinnedAction = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}$`)
	pinnedImage  = regexp.MustCompile(`^docker://[^@]+@sha256:[0-9a-f]{64}$`)
	secretRef    = regexp.MustCompile(`\$\{\{\s*secrets\.([A-Za-z0-9_]+)`)
)

type workflow struct {
	On          yaml.Node      `yaml:"on"`
	Permissions yaml.Node      `yaml:"permissions"`
	Defaults    defaults       `yaml:"defaults"`
	Jobs        map[string]job `yaml:"jobs"`
}

type defaults struct {
	Run struct {
		Shell string `yaml:"shell"`
	} `yaml:"run"`
}

type job struct {
	Permissions yaml.Node `yaml:"permissions"`
	Uses        string    `yaml:"uses"`
	Defaults    defaults  `yaml:"defaults"`
	Steps       []struct {
		Uses  string `yaml:"uses"`
		Run   string `yaml:"run"`
		Shell string `yaml:"shell"`
		Name  string `yaml:"name"`
	} `yaml:"steps"`
}

// pipe finds a shell pipe in a command: a | that is not part of ||.
var pipe = regexp.MustCompile(`(^|[^|])\|([^|]|$)`)

// cmpOr is the first of its values that is not empty.
func cmpOr(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// triggers returns the workflow's events, and the branches of its push event ("tags" for tags).
func triggers(on yaml.Node) (events []string, pushRefs []string) {
	switch on.Kind {
	case yaml.ScalarNode:
		events = []string{on.Value}
	case yaml.SequenceNode:
		for _, n := range on.Content {
			events = append(events, n.Value)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(on.Content); i += 2 {
			name, body := on.Content[i].Value, on.Content[i+1]
			events = append(events, name)
			if name != "push" || body.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(body.Content); j += 2 {
				switch body.Content[j].Value {
				case "branches":
					for _, b := range body.Content[j+1].Content {
						pushRefs = append(pushRefs, b.Value)
					}
				case "tags":
					pushRefs = append(pushRefs, "tags")
				}
			}
		}
	}
	return events, pushRefs
}

// writeScopes returns the scopes a permissions block grants write on; all is true for write-all.
func writeScopes(p yaml.Node) (scopes []string, all bool) {
	switch p.Kind {
	case yaml.ScalarNode:
		return nil, p.Value == "write-all"
	case yaml.MappingNode:
		for i := 0; i+1 < len(p.Content); i += 2 {
			if p.Content[i+1].Value == "write" {
				scopes = append(scopes, p.Content[i].Value)
			}
		}
	}
	sort.Strings(scopes)
	return scopes, false
}

// WorkflowViolations applies the rules to one workflow file.
func WorkflowViolations(name string, src []byte) []string {
	var w workflow
	if err := yaml.Unmarshal(src, &w); err != nil {
		return []string{fmt.Sprintf("%s: not YAML: %v", name, err)}
	}
	var out []string
	bad := func(format string, args ...any) { out = append(out, name+": "+fmt.Sprintf(format, args...)) }
	events, pushRefs := triggers(w.On)
	if !(w.Permissions.Kind == yaml.MappingNode && len(w.Permissions.Content) == 0) {
		bad("the workflow-level token must be `permissions: {}`: grant each job only what it needs")
	}
	prTarget := slices.Contains(events, "pull_request_target")
	forPRCode := slices.Contains(events, "pull_request") || slices.Contains(events, "workflow_run") || slices.Contains(events, "issue_comment")
	for _, ref := range pushRefs {
		if ref != "main" && ref != "tags" {
			forPRCode = true // a push to any other branch runs unreviewed code
		}
	}
	for _, id := range slices.Sorted(maps.Keys(w.Jobs)) {
		j := w.Jobs[id]
		key := name + "/" + id
		if j.Permissions.Kind == 0 {
			bad("job %s has no permissions block: declare the token it needs (often none: `permissions: {}`)", id)
		}
		scopes, all := writeScopes(j.Permissions)
		if all {
			bad("job %s asks for write-all", id)
		}
		if len(scopes) > 0 {
			allowed, ok := WriteJobs[key]
			for _, s := range scopes {
				if !ok || !slices.Contains(allowed.Scopes, s) {
					bad("job %s writes %s, which archtest.WriteJobs does not allow it (SEC-28: least privilege)", id, s)
				}
			}
			if forPRCode {
				bad("job %s has a write token but its workflow runs for pull requests or unreviewed branches", id)
			}
		}
		uses := []string{j.Uses}
		for i, s := range j.Steps {
			uses = append(uses, s.Uses)
			shell := cmpOr(s.Shell, j.Defaults.Run.Shell, w.Defaults.Run.Shell)
			if pipe.MatchString(s.Run) && shell != "bash" && !strings.Contains(s.Run, "pipefail") {
				bad("job %s step %d (%s) pipes its command without `shell: bash`: the default bash -e ignores a failure before the pipe", id, i+1, cmpOr(s.Name, s.Run))
			}
		}
		for _, u := range uses {
			switch {
			case u == "" || strings.HasPrefix(u, "./"):
			case strings.HasPrefix(u, "docker://"):
				if !pinnedImage.MatchString(u) {
					bad("job %s runs the image %s without a digest", id, u)
				}
			case !pinnedAction.MatchString(u):
				bad("job %s uses %s: pin third-party actions by their full commit SHA (SEC-28)", id, u)
			}
			if prTarget && strings.HasPrefix(u, "actions/checkout@") {
				bad("job %s checks out code in a pull_request_target workflow, which runs with the base repository's token", id)
			}
		}
	}
	if prTarget {
		for _, m := range secretRef.FindAllStringSubmatch(string(src), -1) {
			if m[1] != "GITHUB_TOKEN" {
				bad("reads secrets.%s in a pull_request_target workflow", m[1])
			}
		}
	}
	return out
}

// ReleaseAppTokenViolations checks release-please.yml against REL-06: release-please writes with an
// installation token of the organisation's GitHub App, narrowed to the scopes it needs, and its job's
// GITHUB_TOKEN gets nothing. GitHub starts no workflow for a push made with GITHUB_TOKEN, so without
// the App token CI never runs on the release pull request.
func ReleaseAppTokenViolations(src []byte) []string {
	type step struct {
		ID   string            `yaml:"id"`
		Uses string            `yaml:"uses"`
		With map[string]string `yaml:"with"`
	}
	var w struct {
		Jobs map[string]struct {
			Permissions yaml.Node `yaml:"permissions"`
			Steps       []step    `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(src, &w); err != nil {
		return []string{fmt.Sprintf("not YAML: %v", err)}
	}
	j, ok := w.Jobs["release-please"]
	if !ok {
		return []string{"no release-please job"}
	}
	var out []string
	if !(j.Permissions.Kind == yaml.MappingNode && len(j.Permissions.Content) == 0) {
		out = append(out, "the release-please job's GITHUB_TOKEN must be `permissions: {}`: it writes with the App token")
	}
	wantScopes := []string{"permission-contents", "permission-issues", "permission-pull-requests"}
	tokenOf := map[string]bool{}
	for _, s := range j.Steps {
		if !strings.HasPrefix(s.Uses, "actions/create-github-app-token@") {
			continue
		}
		var scopes []string
		for k, v := range s.With {
			if strings.HasPrefix(k, "permission-") {
				scopes = append(scopes, k+"="+v)
			}
		}
		sort.Strings(scopes)
		want := make([]string, len(wantScopes))
		for i, k := range wantScopes {
			want[i] = k + "=write"
		}
		if !slices.Equal(scopes, want) {
			out = append(out, fmt.Sprintf("the App token step %q grants %v; it must grant exactly %v", s.ID, scopes, want))
		}
		if s.With["owner"] != "" || s.With["repositories"] != "" {
			out = append(out, fmt.Sprintf("the App token step %q must cover this repository only (no owner or repositories)", s.ID))
		}
		if s.ID != "" {
			tokenOf["${{ steps."+s.ID+".outputs.token }}"] = true
		}
	}
	if len(tokenOf) == 0 {
		out = append(out, "no actions/create-github-app-token step with an id: release-please needs the App's token (REL-06)")
	}
	used := false
	for _, s := range j.Steps {
		if strings.HasPrefix(s.Uses, "googleapis/release-please-action@") {
			used = true
			if !tokenOf[s.With["token"]] {
				out = append(out, fmt.Sprintf("release-please runs with token %q, not the App token step's output (REL-06)", s.With["token"]))
			}
		}
	}
	if !used {
		out = append(out, "the release-please job does not run googleapis/release-please-action")
	}
	return out
}
