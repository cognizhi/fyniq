//go:build local

package local

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/runtime"
)

// CLIAgent is a model provider on the developer's own plan (PRD-001 DEV-09; p1-ai-providers design D12): the
// developer's `claude` or `codex` command-line agent, run as a subprocess for local live mode on the synthetic sample
// company only (DEV-08). It is in the local build alone and refuses to start unless FYNIQ_ENV=local. Fyniq never reads,
// copies or logs the agent's sign-in: the agent reads its own store, and the subprocess gets the developer's home and
// the agent's own variables, never Fyniq's. It names its model from the agent's own configuration, not a list endpoint,
// and declares what it lacks (AI-71): a request that needs a missing capability is refused, so such an eval runs on
// replay or an API key. Its answers are recorded as cassettes (Recorder), so CI never calls it. Each use follows the
// provider's terms at the time [verify: whether a developer's subscription sign-in may serve local work this way].
type CLIAgent struct {
	Agent string // AgentClaude or AgentCodex
	Model string // from the agent's own configuration
	// run runs the agent with args and stdin in dir, with env, and returns its standard output.
	run func(ctx context.Context, name string, args []string, stdin []byte, env []string, dir string) ([]byte, error)
}

// The command-line agents DEV-09 allows.
const (
	AgentClaude = "claude"
	AgentCodex  = "codex"
)

// AgentEnv names the command-line agent live mode runs on: FYNIQ_AI_AGENT=claude or codex.
const AgentEnv = "FYNIQ_AI_AGENT"

// LiveAgent is Models' live provider when FYNIQ_AI_AGENT names a command-line agent, and nil when it names none.
func LiveAgent() func() (ports.ModelProvider, error) {
	agent := os.Getenv(AgentEnv)
	if agent == "" {
		return nil
	}
	return func() (ports.ModelProvider, error) { return NewCLIAgent(agent) }
}

var _ ports.ModelProvider = (*CLIAgent)(nil)

// NewCLIAgent returns the adapter for agent, its model read from the agent's configuration.
func NewCLIAgent(agent string) (*CLIAgent, error) {
	env, err := runtime.EnvFromOS()
	if err != nil {
		return nil, err
	}
	if env != runtime.EnvLocal {
		return nil, fmt.Errorf("the %s command-line agent runs only in local mode (FYNIQ_ENV=local), never in %s (DEV-09)", agent, env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	var model string
	switch agent {
	case AgentClaude:
		model, err = claudeModel(home)
	case AgentCodex:
		model, err = codexModel(home)
	default:
		return nil, fmt.Errorf("%q is not a command-line agent DEV-09 allows: use %s or %s", agent, AgentClaude, AgentCodex)
	}
	if err != nil {
		return nil, err
	}
	return &CLIAgent{Agent: agent, Model: model, run: runAgent}, nil
}

// claudeModel is the model Claude Code is configured with: ANTHROPIC_MODEL, else "model" in its user settings.
func claudeModel(home string) (string, error) {
	if m := os.Getenv("ANTHROPIC_MODEL"); m != "" {
		return m, nil
	}
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(home, ".claude")
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json")) // #nosec G304 G703 -- the agent's own settings file in the developer's own configuration directory, for its model alone
	if err == nil {
		var s struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(b, &s) == nil && s.Model != "" {
			return s.Model, nil
		}
	}
	return "", errors.New(`name a model for Claude Code: set ANTHROPIC_MODEL, or "model" in its settings.json`)
}

// codexModel is the model Codex is configured with: the top-level model in its config.toml.
func codexModel(home string) (string, error) {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".codex")
	}
	f, err := os.Open(filepath.Join(dir, "config.toml")) // #nosec G304 G703 -- the agent's own configuration file in the developer's own directory, for its model alone
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(line, "[") {
				break // the top-level keys come before the first table
			}
			if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "model" {
				if m := strings.Trim(strings.TrimSpace(v), `"'`); m != "" {
					return m, nil
				}
			}
		}
	}
	return "", errors.New("name a model for Codex: set model in its config.toml")
}

// agentEnv is the subprocess's environment: what any program needs, and the agent's own variables; never Fyniq's
// (FYNIQ_*, the database, the connectors' secrets).
func agentEnv(environ []string) []string {
	keep := map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "TMPDIR": true,
		"LANG": true, "LC_ALL": true, "TERM": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true}
	var out []string
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		if keep[k] || strings.HasPrefix(k, "CLAUDE_") || strings.HasPrefix(k, "ANTHROPIC_") || strings.HasPrefix(k, "CODEX_") ||
			strings.HasPrefix(k, "OPENAI_") {
			out = append(out, kv)
		}
	}
	return out
}

func runAgent(ctx context.Context, name string, args []string, stdin []byte, env []string, dir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- name is claude or codex; args are flags and the model, the prompt goes on stdin
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Env = env
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// The agent's own words, which name a missing sign-in or a spent plan; never Fyniq's data.
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("the %s command-line agent failed: %w: %s", name, err, msg)
	}
	return out, nil
}

// Capabilities implements ports.ModelProvider: text in and out, nothing it cannot prove through a command line.
func (c *CLIAgent) Capabilities() ports.ModelCapabilities { return ports.ModelCapabilities{} }

// lacks names what r needs that a command-line agent lacks.
func (c *CLIAgent) lacks(r ports.ModelRequest) error {
	var missing []string
	if len(r.Tools) > 0 {
		missing = append(missing, "custom tool definitions")
	}
	if r.Output != nil {
		missing = append(missing, "a strict output schema")
	}
	if r.Cache {
		missing = append(missing, "cache control")
	}
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			switch p.Kind {
			case ports.PartText:
			case ports.PartImage:
				missing = append(missing, "image input")
			case ports.PartDocument:
				missing = append(missing, "PDF input")
			default:
				missing = append(missing, "tool calls")
			}
		}
	}
	if r.Model != c.Model {
		return fmt.Errorf("the %s command-line agent is configured with %s, not %s: change its configuration or the route", c.Agent, c.Model, r.Model)
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return fmt.Errorf("the %s command-line agent lacks %s (DEV-09): run this on replay or with an API key", c.Agent, strings.Join(slices.Compact(missing), ", "))
	}
	return nil
}

// transcript is the conversation as one prompt, with the system prompt first when the agent has no flag for it.
func transcript(r ports.ModelRequest, withSystem bool) []byte {
	var b strings.Builder
	if withSystem && r.System != "" {
		b.WriteString("System:\n" + r.System + "\n\n")
	}
	for i, m := range r.Messages {
		if len(r.Messages) > 1 || withSystem {
			role := "User"
			if m.Role == ports.RoleAssistant {
				role = "Assistant"
			}
			b.WriteString(role + ":\n")
		}
		for _, p := range m.Parts {
			b.WriteString(p.Text)
		}
		if i < len(r.Messages)-1 {
			b.WriteString("\n\n")
		}
	}
	return []byte(b.String())
}

// Call implements ports.ModelProvider.
func (c *CLIAgent) Call(ctx context.Context, r ports.ModelRequest) (ports.ModelResponse, error) {
	if err := c.lacks(r); err != nil {
		return ports.ModelResponse{}, err
	}
	dir, err := os.MkdirTemp("", "fyniq-agent-")
	if err != nil {
		return ports.ModelResponse{}, err
	}
	defer os.RemoveAll(dir)
	env := agentEnv(os.Environ())
	if c.Agent == AgentClaude {
		// No tools, no MCP servers, no saved session; the request's own system prompt instead of Claude Code's.
		args := []string{"-p", "--output-format", "json", "--model", c.Model, "--tools", "", "--strict-mcp-config", "--no-session-persistence"}
		if r.System != "" {
			args = append(args, "--system-prompt", r.System)
		}
		out, err := c.run(ctx, AgentClaude, args, transcript(r, false), env, dir)
		if err != nil {
			return ports.ModelResponse{}, err
		}
		return claudeAnswer(out, c.Model)
	}
	// Read-only, in an empty directory, nothing saved; Codex has no system prompt flag, so it leads the transcript.
	args := []string{"exec", "--json", "-m", c.Model, "--sandbox", "read-only", "--skip-git-repo-check", "--ephemeral", "-C", dir, "-"}
	out, err := c.run(ctx, AgentCodex, args, transcript(r, true), env, dir)
	if err != nil {
		return ports.ModelResponse{}, err
	}
	return codexAnswer(out, c.Model)
}

// claudeAnswer reads `claude -p --output-format json`: the result, its usage, and the models it used
// [verify: the fields of Claude Code's JSON result].
func claudeAnswer(out []byte, model string) (ports.ModelResponse, error) {
	var res struct {
		Subtype string `json:"subtype"`
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
		Usage   struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return ports.ModelResponse{}, errors.New("the claude command-line agent's answer cannot be read")
	}
	if res.IsError || (res.Subtype != "" && res.Subtype != "success") {
		return ports.ModelResponse{}, fmt.Errorf("the claude command-line agent stopped: %s", res.Subtype)
	}
	reported := model
	if len(res.ModelUsage) == 1 {
		for m := range res.ModelUsage {
			reported = m
		}
	}
	return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: res.Result}}, StopReason: ports.StopEnd,
		Usage: ports.ModelUsage{Input: res.Usage.Input, Output: res.Usage.Output, CacheRead: res.Usage.CacheRead, CacheWrite: res.Usage.CacheWrite},
		Model: reported}, nil
}

// codexAnswer reads `codex exec --json`: its agent messages and the turn's usage [verify: the events of Codex's JSONL].
func codexAnswer(out []byte, model string) (ports.ModelResponse, error) {
	var text []string
	var usage ports.ModelUsage
	done := false
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 1<<16), 8<<20)
	for sc.Scan() {
		var e struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Usage struct {
				Input     int64 `json:"input_tokens"`
				Cached    int64 `json:"cached_input_tokens"`
				Output    int64 `json:"output_tokens"`
				Reasoning int64 `json:"reasoning_output_tokens"`
			} `json:"usage"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch e.Type {
		case "item.completed":
			if e.Item.Type == "agent_message" {
				text = append(text, e.Item.Text)
			}
		case "turn.completed":
			done = true
			usage = ports.ModelUsage{Input: e.Usage.Input, Output: e.Usage.Output, CacheRead: e.Usage.Cached, Reasoning: e.Usage.Reasoning}
		case "turn.failed":
			return ports.ModelResponse{}, fmt.Errorf("the codex command-line agent stopped: %s", e.Error.Message)
		case "error":
			return ports.ModelResponse{}, fmt.Errorf("the codex command-line agent stopped: %s", e.Message)
		}
	}
	if !done {
		return ports.ModelResponse{}, errors.New("the codex command-line agent did not finish its turn")
	}
	return ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: strings.Join(text, "\n\n")}}, StopReason: ports.StopEnd,
		Usage: usage, Model: model}, nil
}

// Stream implements ports.ModelProvider: the whole answer as one event, since streaming is not among what it proves.
func (c *CLIAgent) Stream(ctx context.Context, r ports.ModelRequest, each func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	resp, err := c.Call(ctx, r)
	if err != nil {
		return resp, err
	}
	for i := range resp.Parts {
		if err := each(ports.ModelEvent{Part: &resp.Parts[i]}); err != nil {
			return ports.ModelResponse{}, err
		}
	}
	return resp, nil
}

// ListModels implements ports.ModelProvider: the one model the agent is configured with.
func (c *CLIAgent) ListModels(_ context.Context, _ string) (ports.ModelPage, error) {
	return ports.ModelPage{Models: []ports.ListedModel{{ID: c.Model, Name: c.Agent + " command-line agent"}}}, nil
}
