package knowledge

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The pages rendered from an organisation's settings (PRD-001 AI-50; p1-knowledge-base design D6): approval policies, team mode, and
// every setting with its value, whether the organisation changed it, the recommended value and the reason. Rendered with no model, no
// network and no clock: the same input gives the same bytes. The words are Fyniq's own and the values are the organisation's settings
// as they are, so a page cannot say more than the settings do, and each section cites the setting it states.

// Keys of the pages rendered from settings. A person may edit one (it becomes a human-edited version a re-render never overwrites),
// and a conflict is then a finding for a person.
const (
	RenderedTeamMode = "org.team-mode"
	RenderedPolicies = "org.approval-policies"
	RenderedSettings = "org.settings." // + the group's slug
)

// RenderSetting is one setting as the organisation has it today.
type RenderSetting struct {
	Key         string
	Group       string
	Label       string
	Value       string // in force today
	Recommended string
	Reason      string
	Changed     bool // the organisation set it away from the recommended value
	Fixed       bool // set by the platform
}

// RenderPolicy is one object type's approval policy as the organisation has it.
type RenderPolicy struct {
	ObjectType string
	Label      string // what the approval is for, in words
	Approvers  []int  // each step's quorum, in order
	Changed    bool   // the organisation's own version, not the platform's default
}

// RenderInput is everything the render reads.
type RenderInput struct {
	TeamMode string // founder, standard or enterprise
	Settings []RenderSetting
	Policies []RenderPolicy
}

// RenderedSource is a source of a rendered page: a setting, an approval policy or the team mode, with the text the page's numbers are
// checked against.
type RenderedSource struct {
	Kind  string // setting, policy or team_mode
	Ref   string
	Title string
	Text  string
}

// Page is a page ready to be written as a version.
type RenderedPage struct {
	Key     string
	Type    string
	Title   string
	Summary string
	Aliases []string
	Body    string
	Sources []RenderedSource
}

// Cite is the source as "kind:ref", as a page cites it.
func (s RenderedSource) Cite() string { return s.Kind + ":" + s.Ref }

var teamModeWords = map[string]string{
	"founder":    "Founder mode: a small founding team works with fewer second approvers, and the high-risk actions one person takes alone are listed for another founder's weekly review.",
	"standard":   "Standard mode: duties are separated, so a second person approves what is risky and no one approves their own.",
	"enterprise": "Enterprise mode: duties are separated as in Standard mode, with the controls a larger organisation needs.",
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func groupSlug(g string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(g), "-"), "-")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// Render makes the pages: the team mode, the approval policies and one page per group of settings, by key. A group with no setting,
// and a policy with no step, make nothing.
func Render(in RenderInput) []RenderedPage {
	var out []RenderedPage
	mode := in.TeamMode
	if words, ok := teamModeWords[mode]; ok {
		src := RenderedSource{Kind: "team_mode", Ref: "organisation", Title: "Team mode", Text: "Team mode: " + mode + "\n" + words}
		out = append(out, RenderedPage{Key: RenderedTeamMode, Type: "decision", Title: "How strictly duties are separated here",
			Summary: "The organisation's team mode and what it means for who approves.", Aliases: []string{"team mode", "who can approve my own work"},
			Body:    "# How strictly duties are separated here\n\n## Team mode\n\nThe organisation works in " + strings.ToUpper(mode[:1]) + mode[1:] + " mode. " + words + " (src: team_mode:organisation#mode)\n",
			Sources: []RenderedSource{src}})
	}
	if len(in.Policies) > 0 {
		pol := append([]RenderPolicy(nil), in.Policies...)
		sort.Slice(pol, func(i, j int) bool { return pol[i].ObjectType < pol[j].ObjectType })
		var b strings.Builder
		var srcs []RenderedSource
		b.WriteString("# Who approves what\n\n## Approvals\n\n")
		for _, p := range pol {
			if len(p.Approvers) == 0 {
				continue
			}
			var steps []string
			for _, q := range p.Approvers {
				steps = append(steps, plural(q, "approver", "approvers"))
			}
			how := "the platform's default"
			if p.Changed {
				how = "the organisation's own policy"
			}
			text := fmt.Sprintf("%s: %s, %s", p.Label, strings.Join(steps, ", then "), how)
			fmt.Fprintf(&b, "- %s. (src: policy:%s#steps)\n", text, p.ObjectType)
			srcs = append(srcs, RenderedSource{Kind: "policy", Ref: p.ObjectType, Title: "Approval policy: " + p.Label, Text: text})
		}
		if len(srcs) > 0 {
			out = append(out, RenderedPage{Key: RenderedPolicies, Type: "policy", Title: "Who approves what", Summary: "How many people approve each kind of change, by approval policy.",
				Aliases: []string{"approval policy", "who approves"}, Body: b.String(), Sources: srcs})
		}
	}
	groups := map[string][]RenderSetting{}
	for _, s := range in.Settings {
		groups[s.Group] = append(groups[s.Group], s)
	}
	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	sort.Strings(names)
	for _, g := range names {
		var b strings.Builder
		var srcs []RenderedSource
		fmt.Fprintf(&b, "# Settings: %s\n\n", g)
		for _, s := range groups[g] {
			status := "the recommended value"
			if s.Changed {
				status = "changed from the recommended value " + s.Recommended
			}
			if s.Fixed {
				status = "set by the platform"
			}
			text := fmt.Sprintf("%s is %s (%s). %s", s.Label, s.Value, status, s.Reason)
			fmt.Fprintf(&b, "## %s\n\n%s (src: setting:%s#value)\n\n", s.Label, text, s.Key)
			srcs = append(srcs, RenderedSource{Kind: "setting", Ref: s.Key, Title: s.Label, Text: text})
		}
		out = append(out, RenderedPage{Key: RenderedSettings + groupSlug(g), Type: "decision", Title: "Settings: " + g,
			Summary: oneLine("The organisation's "+strings.ToLower(g)+" settings, whether each was changed, and why the recommended value is recommended.", 160),
			Aliases: []string{strings.ToLower(g) + " settings"}, Body: strings.TrimRight(b.String(), "\n") + "\n", Sources: srcs})
	}
	return out
}
