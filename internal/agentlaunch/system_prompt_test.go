package agentlaunch

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

func systemPromptModeProject(t *testing.T, enable bool) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := embedded.WriteGlobalFiles(globalDir, nil); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if enable {
		if _, err := embedded.EnableSystemPromptMode(root, false); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestResolveLaunchPlanInjectsSystemPromptContracts(t *testing.T) {
	root := systemPromptModeProject(t, true)
	promptFile := embedded.SystemPromptFilePath(root, embedded.SystemPromptModeMultiAgent)

	claude, err := ResolveLaunchPlan(LaunchPlanRequest{ToolName: "claude", ProjectRoot: root, AgentID: "coder-1", OutputsDir: "/logs"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-p", "--permission-mode", "auto", "--verbose", "--output-format", "stream-json", "--append-system-prompt-file", promptFile, "--exclude-dynamic-system-prompt-sections"}
	if !slices.Equal(claude.Args, want) || !claude.SystemPromptContracts {
		t.Fatalf("claude args = %v, want %v", claude.Args, want)
	}

	codex, err := ResolveLaunchPlan(LaunchPlanRequest{ToolName: "codex", ProjectRoot: root, AgentID: "coder-2", Model: "gpt-x"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if codex.SystemPromptContracts || strings.Contains(strings.Join(codex.Args, " "), "developer_instructions") {
			t.Fatalf("codex on Windows must keep the forced reads: %v", codex.Args)
		}
		return
	}
	if len(codex.Args) != 6 || codex.Args[0] != "-c" || !slices.Equal(codex.Args[2:], []string{"-m", "gpt-x", "exec", "-"}) {
		t.Fatalf("codex args = %v", codex.Args)
	}
	var parsed struct {
		DeveloperInstructions string `toml:"developer_instructions"`
	}
	if err := toml.Unmarshal([]byte(codex.Args[1]), &parsed); err != nil {
		t.Fatalf("codex -c value is not valid TOML: %v", err)
	}
	content, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.DeveloperInstructions != string(content) {
		t.Fatal("codex developer_instructions does not round-trip the prompt file")
	}
}

func TestResolveLaunchPlanSkipsSystemPromptContracts(t *testing.T) {
	enabled := systemPromptModeProject(t, true)
	disabled := t.TempDir()
	acpx := models.Config{AgentTools: map[string]models.AgentToolConfig{"qwen-acp": acpxTaskScopeTestTool()}}
	for _, tc := range []struct {
		name string
		req  LaunchPlanRequest
	}{
		{"no marker", LaunchPlanRequest{ToolName: "claude", ProjectRoot: disabled, AgentID: "coder-1"}},
		{"pairing", LaunchPlanRequest{ToolName: "claude", ProjectRoot: enabled}},
		{"codex pairing", LaunchPlanRequest{ToolName: "codex", ProjectRoot: enabled}},
		{"other provider", LaunchPlanRequest{ToolName: "opencode", ProjectRoot: enabled, AgentID: "coder-1"}},
		{"acpx", LaunchPlanRequest{ToolName: "qwen-acp", ProjectRoot: enabled, AgentID: "coder-1", RuntimeConfig: acpx}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := ResolveLaunchPlan(tc.req)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(plan.Args, " ")
			if plan.SystemPromptContracts || strings.Contains(joined, "system-prompt") || strings.Contains(joined, "developer_instructions") {
				t.Fatalf("unexpected injection: %v", plan.Args)
			}
		})
	}
	if SystemPromptContractsApply(disabled, "coder-1", "claude", models.Config{}) || !SystemPromptContractsApply(enabled, "coder-1", "claude", models.Config{}) ||
		SystemPromptContractsApply(enabled, "coder-1", "codex", models.Config{}) != (runtime.GOOS != "windows") {
		t.Fatal("SystemPromptContractsApply disagrees with the launch plan")
	}
}

func TestResolveLaunchPlanFailsWhenContractsMissing(t *testing.T) {
	root := systemPromptModeProject(t, true)
	globalDir, _ := paths.GlobalLizaDir()
	if err := os.Remove(filepath.Join(globalDir, "CORE.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveLaunchPlan(LaunchPlanRequest{ToolName: "claude", ProjectRoot: root, AgentID: "coder-1"}); err == nil {
		t.Fatal("expected launch to fail instead of starting an agent with neither contracts nor gate")
	}
}

func TestResolveLaunchPlanRejectsOversizeCodexContracts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("codex is not injected on Windows")
	}
	root := systemPromptModeProject(t, true)
	if err := os.WriteFile(filepath.Join(root, "GUARDRAILS.md"), []byte(strings.Repeat("lesson\n", maxCodexArgBytes/7)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveLaunchPlan(LaunchPlanRequest{ToolName: "codex", ProjectRoot: root, AgentID: "coder-1"}); err == nil || !strings.Contains(err.Error(), "too large for a codex argument") {
		t.Fatalf("err = %v, want the argv size error", err)
	}
	if _, err := ResolveLaunchPlan(LaunchPlanRequest{ToolName: "claude", ProjectRoot: root, AgentID: "coder-1"}); err != nil {
		t.Fatalf("claude reads the file, so size must not block it: %v", err)
	}
}

func TestTOMLBasicStringRoundTrip(t *testing.T) {
	in := "quote \" backslash \\ tab\t cr\r nl\n bell\a del\x7f ünïcode ''' \"\"\""
	var parsed struct{ V string }
	if err := toml.Unmarshal([]byte("V = "+tomlBasicString(in)), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.V != in {
		t.Fatalf("round trip = %q, want %q", parsed.V, in)
	}
}
