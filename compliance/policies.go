package compliance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// PolicyKeys are the policies of the library (PRD-001 CMP-07), in the order the library lists them.
var PolicyKeys = []string{"information-security", "acceptable-use", "access-control", "incident-response", "backup",
	"change-management", "ai-use", "data-protection"}

// NoticeKeys are the privacy notices the library also holds (PRD-001 CMP-PD-03, rev 0.244; p1-pdpa-retention design
// D2): the members' and the contacts'. They are drafted, published and versioned as the policies are.
var NoticeKeys = []string{"notice-members", "notice-contacts"}

// LibraryKeys are every key of the library: the policies, then the notices.
var LibraryKeys = append(append([]string{}, PolicyKeys...), NoticeKeys...)

// Acknowledged reports whether every member acknowledges each published version of key: each policy, and the members'
// notice. The contacts' notice is given to contacts, in what Fyniq sends them, and no member acknowledges it.
func Acknowledged(key string) bool { return key != "notice-contacts" }

// IsNotice reports whether key is a privacy notice rather than a policy.
func IsNotice(key string) bool { return strings.HasPrefix(key, "notice-") }

// PolicySource is an official source a draft was written from, with the day it was read.
type PolicySource struct {
	Title     string `yaml:"title"`
	URL       string `yaml:"url"`
	Retrieved string `yaml:"retrieved"`
}

// PolicyDraft is Fyniq's starting draft of a policy (design D8): its front matter, and its text as Markdown with its
// SHA-256. The Owner or the DPO adopts it as it is, or edits it, and publishes a version.
type PolicyDraft struct {
	Key      string         `yaml:"key"`
	Title    string         `yaml:"title"`
	Draft    string         `yaml:"draft"` // fyniq-policy-draft@1
	Sources  []PolicySource `yaml:"sources"`
	Controls []string       `yaml:"controls"`
	Body     string         `yaml:"-"`
	SHA256   string         `yaml:"-"`
}

// PolicyDrafts are Fyniq's starting drafts of the policies, in the order of PolicyKeys.
func PolicyDrafts() ([]PolicyDraft, error) { return drafts("policies", PolicyKeys) }

// NoticeDrafts are Fyniq's starting drafts of the privacy notices (fyniq-notice-draft@1), in the order of NoticeKeys.
func NoticeDrafts() ([]PolicyDraft, error) { return drafts("notices", NoticeKeys) }

// DPIAKeys are the data protection impact assessments Fyniq drafts (PRD-001 CMP-PD-08, rev 0.244; p1-pdpa-retention
// design D4): the payroll link's and the AI assistant's, each due once its feature is switched on.
var DPIAKeys = []string{"payroll", "ai-assistant"}

// DPIAFeatures are the feature switches that make each of Fyniq's DPIAs due.
var DPIAFeatures = map[string]string{"payroll": "features.payroll", "ai-assistant": "features.ai_assistant"}

// DPIADrafts are Fyniq's starting drafts of the DPIAs (fyniq-dpia-draft@1), in the order of DPIAKeys.
func DPIADrafts() ([]PolicyDraft, error) { return drafts("dpias", DPIAKeys) }

// LibraryDrafts are every starting draft of the library, in the order of LibraryKeys.
func LibraryDrafts() ([]PolicyDraft, error) {
	ps, err := PolicyDrafts()
	if err != nil {
		return nil, err
	}
	ns, err := NoticeDrafts()
	return append(ps, ns...), err
}

func drafts(dir string, keys []string) ([]PolicyDraft, error) {
	var out []PolicyDraft
	err := fs.WalkDir(files, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(b)
		rest, ok := strings.CutPrefix(text, "---\n")
		head, body, found := strings.Cut(rest, "\n---\n")
		if !ok || !found {
			return fmt.Errorf("%s has no front matter", path)
		}
		var p PolicyDraft
		if err := yaml.Unmarshal([]byte(head), &p); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		p.Body = strings.TrimLeft(body, "\n")
		sum := sha256.Sum256([]byte(p.Body))
		p.SHA256 = hex.EncodeToString(sum[:])
		out = append(out, p)
		return nil
	})
	order := map[string]int{}
	for i, k := range keys {
		order[k] = i
	}
	sort.Slice(out, func(i, j int) bool { return order[out[i].Key] < order[out[j].Key] })
	return out, err
}
