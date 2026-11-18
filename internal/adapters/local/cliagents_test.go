//go:build local

package local

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports"
)

// agentRun stands in for the developer's agent: it keeps what it was given and answers out.
type agentRun struct {
	name  string
	args  []string
	stdin string
	env   []string
	dir   string
	out   string
}

func (a *agentRun) run(_ context.Context, name string, args []string, stdin []byte, env []string, dir string) ([]byte, error) {
	a.name, a.args, a.stdin, a.env, a.dir = name, args, string(stdin), env, dir
	return []byte(a.out), nil
}

func textRequest(model string) ports.ModelRequest {
	return ports.ModelRequest{Model: model, System: "Answer from the sample company's figures.", MaxTokens: 512,
		Messages: []ports.ModelMessage{
			{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "What was revenue in October?"}}},
			{Role: ports.RoleAssistant, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "5000.00."}}},
			{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "And September?"}}},
		}}
}

// DEV-09: the developer's own claude or codex command-line agent answers in local mode only, named from the agent's
// own configuration; the subprocess runs in an empty directory with no tools, no MCP servers and no saved session,
// gets the prompt on standard input and never Fyniq's variables; each answer is read with its usage and the model it
// reports; a request needing what a command line lacks, or another model, is refused with what to do; live mode records
// it as a cassette for the next replay.
// req: DEV-09
func TestDevelopersOwnAgentAnswersInLocalModeOnly(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))

	t.Setenv("FYNIQ_ENV", "ci")
	if _, err := NewCLIAgent(AgentClaude); err == nil || !strings.Contains(err.Error(), "FYNIQ_ENV=local") {
		t.Fatalf("outside local mode: %v", err)
	}
	t.Setenv("FYNIQ_ENV", "local")
	if _, err := NewCLIAgent("gemini"); err == nil {
		t.Error("an agent DEV-09 does not allow was accepted")
	}
	if _, err := NewCLIAgent(AgentClaude); err == nil || !strings.Contains(err.Error(), "settings.json") {
		t.Errorf("no model configured: %v", err)
	}
	for path, body := range map[string]string{
		"claude/settings.json": `{"model": "claude-test-1", "permissions": {}}`,
		"codex/config.toml":    "approval_policy = \"never\"\nmodel = \"gpt-test-1\"\n\n[profiles.fast]\nmodel = \"gpt-other\"\n",
	} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(home, path)), 0o700)
		if err := os.WriteFile(filepath.Join(home, path), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FYNIQ_DATABASE_URL", "postgres://fyniq@db/fyniq")
	t.Setenv("ANTHROPIC_BASE_URL", "https://gateway.example")

	claude, err := NewCLIAgent(AgentClaude)
	if err != nil || claude.Model != "claude-test-1" {
		t.Fatalf("claude: %v %+v", err, claude)
	}
	fake := &agentRun{out: `{"type":"result","subtype":"success","is_error":false,"result":"Revenue in September was 4200.00.",` +
		`"usage":{"input_tokens":40,"output_tokens":9,"cache_read_input_tokens":12,"cache_creation_input_tokens":3},` +
		`"modelUsage":{"claude-test-1-20261001":{"inputTokens":40}}}`}
	claude.run = fake.run
	resp, err := claude.Call(ctx, textRequest("claude-test-1"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Parts[0].Text != "Revenue in September was 4200.00." || resp.Model != "claude-test-1-20261001" ||
		resp.Usage != (ports.ModelUsage{Input: 40, Output: 9, CacheRead: 12, CacheWrite: 3}) {
		t.Errorf("claude's answer: %+v", resp)
	}
	want := []string{"-p", "--output-format", "json", "--model", "claude-test-1", "--tools", "", "--strict-mcp-config",
		"--no-session-persistence", "--system-prompt", "Answer from the sample company's figures."}
	if fake.name != "claude" || !slices.Equal(fake.args, want) {
		t.Errorf("claude's command: %s %q", fake.name, fake.args)
	}
	if !strings.Contains(fake.stdin, "User:\nWhat was revenue in October?") || !strings.Contains(fake.stdin, "Assistant:\n5000.00.") ||
		strings.Contains(fake.stdin, "System:") {
		t.Errorf("claude's prompt: %q", fake.stdin)
	}
	if entries, _ := os.ReadDir(fake.dir); fake.dir == "" || len(entries) != 0 {
		t.Errorf("the agent ran in %q", fake.dir)
	}
	if _, err := os.Stat(fake.dir); !os.IsNotExist(err) {
		t.Errorf("the agent's directory was left behind: %v", err)
	}
	if slices.ContainsFunc(fake.env, func(kv string) bool { return strings.HasPrefix(kv, "FYNIQ_") }) ||
		!slices.Contains(fake.env, "ANTHROPIC_BASE_URL=https://gateway.example") || !slices.Contains(fake.env, "HOME="+home) {
		t.Errorf("the agent's environment: %q", fake.env)
	}

	codex, err := NewCLIAgent(AgentCodex)
	if err != nil || codex.Model != "gpt-test-1" {
		t.Fatalf("codex: %v %+v", err, codex)
	}
	fake = &agentRun{out: `{"type":"thread.started","thread_id":"t1"}` + "\n" + `{"type":"turn.started"}` + "\n" +
		`{"type":"item.completed","item":{"id":"i0","type":"reasoning","text":"thinking"}}` + "\n" +
		`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"Revenue in September was 4200.00."}}` + "\n" +
		`{"type":"turn.completed","usage":{"input_tokens":50,"cached_input_tokens":20,"output_tokens":8,"reasoning_output_tokens":5}}` + "\n"}
	codex.run = fake.run
	resp, err = codex.Call(ctx, textRequest("gpt-test-1"))
	if err != nil || resp.Parts[0].Text != "Revenue in September was 4200.00." || resp.Model != "gpt-test-1" ||
		resp.Usage != (ports.ModelUsage{Input: 50, Output: 8, CacheRead: 20, Reasoning: 5}) {
		t.Errorf("codex's answer: %v %+v", err, resp)
	}
	if fake.name != "codex" || !slices.Equal(fake.args[:3], []string{"exec", "--json", "-m"}) || !slices.Contains(fake.args, "read-only") ||
		!slices.Contains(fake.args, "--ephemeral") || fake.args[len(fake.args)-1] != "-" || !strings.HasPrefix(fake.stdin, "System:\nAnswer from") {
		t.Errorf("codex's command: %q, prompt %q", fake.args, fake.stdin)
	}
	for out, msg := range map[string]string{
		`{"type":"turn.failed","error":{"message":"usage limit reached"}}`: "usage limit reached",
		`{"type":"turn.started"}`: "did not finish",
	} {
		fake.out = out
		if _, err := codex.Call(ctx, textRequest("gpt-test-1")); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("codex %s: %v", out, err)
		}
	}

	// What a command line lacks, and another model, are refused before the agent runs.
	needs := textRequest("claude-test-1")
	needs.Tools = []ports.ModelTool{{Name: "reports.figure"}}
	needs.Output = &ports.ModelSchema{Name: "answer"}
	needs.Messages[0].Parts = append(needs.Messages[0].Parts, ports.ModelPart{Kind: ports.PartImage, MediaType: "image/png"})
	claude.run = func(context.Context, string, []string, []byte, []string, string) ([]byte, error) {
		t.Fatal("the agent ran for a request it cannot serve")
		return nil, nil
	}
	if _, err := claude.Call(ctx, needs); err == nil || !strings.Contains(err.Error(), "custom tool definitions") ||
		!strings.Contains(err.Error(), "a strict output schema") || !strings.Contains(err.Error(), "image input") ||
		!strings.Contains(err.Error(), "replay") {
		t.Errorf("a request it lacks the means for: %v", err)
	}
	if _, err := claude.Call(ctx, textRequest("claude-other")); err == nil || !strings.Contains(err.Error(), "claude-test-1") {
		t.Errorf("another model: %v", err)
	}
	if caps := claude.Capabilities(); caps.Tools || caps.StrictOutput || caps.Caching || caps.Images || caps.PDF {
		t.Errorf("declared capabilities: %+v", caps)
	}
	if page, _ := claude.ListModels(ctx, ""); len(page.Models) != 1 || page.Models[0].ID != "claude-test-1" {
		t.Errorf("its model list: %+v", page)
	}

	t.Setenv(AgentEnv, "")
	if LiveAgent() != nil {
		t.Error("live mode picked an agent no one named")
	}
	t.Setenv(AgentEnv, AgentCodex)
	if live := LiveAgent(); live == nil {
		t.Error("FYNIQ_AI_AGENT=codex picked no agent")
	} else if p, err := live(); err != nil || p.(*CLIAgent).Agent != AgentCodex {
		t.Errorf("FYNIQ_AI_AGENT=codex: %v", err)
	}

	// Live mode through the agent records each answer for replay.
	dir := t.TempDir()
	claude.run = (&agentRun{out: `{"subtype":"success","result":"4200.00","usage":{"input_tokens":1,"output_tokens":1}}`}).run
	t.Setenv(LiveEnv, "1")
	p, err := Models(dir, func() (ports.ModelProvider, error) { return claude, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Call(ctx, textRequest("claude-test-1")); err != nil {
		t.Fatal(err)
	}
	t.Setenv(LiveEnv, "0")
	r, _ := Models(dir, nil)
	if got, err := r.Call(ctx, textRequest("claude-test-1")); err != nil || got.Parts[0].Text != "4200.00" {
		t.Errorf("the replayed answer: %v %+v", err, got)
	}
}
