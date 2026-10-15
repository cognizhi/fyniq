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
// request's code; a pull_request_target workflow never checks out code or reads secrets.

// WriteJobs are the jobs allowed a write token, and the scopes each may write, with why.
var WriteJobs = map[string]struct {
	Scopes []string
	Why    string
}{
	"release-please.yml/release-please": {[]string{"contents", "issues", "pull-requests"}, "release-please opens the release pull request and tags releases (REL-02)"},
	"auto-approve.yml/approve":          {[]string{"pull-requests"}, "approves the owner's development pull requests before production (founder decision, 14 Oct 2026)"},
}

var (
	pinnedAction = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}$`)
	pinnedImage  = regexp.MustCompile(`^docker://[^@]+@sha256:[0-9a-f]{64}$`)
	secretRef    = regexp.MustCompile(`\$\{\{\s*secrets\.([A-Za-z0-9_]+)`)
)

type workflow struct {
	On          yaml.Node      `yaml:"on"`
	Permissions yaml.Node      `yaml:"permissions"`
	Jobs        map[string]job `yaml:"jobs"`
}

type job struct {
	Permissions yaml.Node `yaml:"permissions"`
	Uses        string    `yaml:"uses"`
	Steps       []struct {
		Uses string `yaml:"uses"`
	} `yaml:"steps"`
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
		for _, s := range j.Steps {
			uses = append(uses, s.Uses)
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
