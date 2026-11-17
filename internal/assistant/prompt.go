package assistant

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"

	contracts "github.com/cognizhi/fyniq/contracts/ai"
)

// Prompt is an agent's system prompt, as one version of contracts/ai/prompts (AI-24, design D10): its text and the hash
// every turn records, so a turn names exactly what the model was told.
type Prompt struct {
	Agent   string
	Version int
	Text    string
	SHA256  string
}

// Name is the prompt as a turn records it: chat@1.
func (p Prompt) Name() string { return p.Agent + "@" + strconv.Itoa(p.Version) }

var promptFile = regexp.MustCompile(`^([a-z][a-z_]{1,30})@([1-9][0-9]{0,3})\.md$`)

// LoadPrompt is the latest version of agent's prompt in fsys's prompts directory.
func LoadPrompt(fsys fs.FS, agent string) (Prompt, error) {
	files, err := fs.Glob(fsys, "prompts/*.md")
	if err != nil {
		return Prompt{}, err
	}
	var best Prompt
	for _, f := range files {
		m := promptFile.FindStringSubmatch(path.Base(f))
		if m == nil {
			return Prompt{}, fmt.Errorf("%s: a prompt's file is <agent>@<version>.md", f)
		}
		v, _ := strconv.Atoi(m[2])
		if m[1] != agent || v <= best.Version {
			continue
		}
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return Prompt{}, err
		}
		sum := sha256.Sum256(b)
		best = Prompt{Agent: agent, Version: v, Text: string(b), SHA256: hex.EncodeToString(sum[:])}
	}
	if best.Version == 0 || best.Text == "" {
		return Prompt{}, fmt.Errorf("no prompt for the agent %s in prompts/", agent)
	}
	return best, nil
}

// DefaultPrompt is agent's latest prompt as the release ships it.
func DefaultPrompt(agent string) (Prompt, error) { return LoadPrompt(contracts.Prompts, agent) }
