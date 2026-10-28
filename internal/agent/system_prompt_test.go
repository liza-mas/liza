package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
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

func TestSystemPromptContractsEnvFollowsInjection(t *testing.T) {
	name := brand.EnvName("SYSTEM_PROMPT_CONTRACTS")
	base := []string{"PATH=/bin", name + "=1"} // inherited from an injected parent session
	if got := systemPromptContractsEnv(base, false); !slices.Equal(got, []string{"PATH=/bin"}) {
		t.Fatalf("non-injected env = %v, want the inherited bypass removed", got)
	}
	if got := systemPromptContractsEnv(base[:1], true); !slices.Equal(got, []string{"PATH=/bin", name + "=1"}) {
		t.Fatalf("injected env = %v", got)
	}
}

func TestCLIAgentInteractiveSetsSystemPromptContractsEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI shell script test requires /bin/sh")
	}
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("enabled=%v", enabled), func(t *testing.T) {
			root := systemPromptModeProject(t, enabled)
			binDir := t.TempDir()
			name := brand.EnvName("SYSTEM_PROMPT_CONTRACTS")
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s|%%s' \"$%s\" \"$*\" > env.txt\n", name)
			if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir)
			t.Setenv(name, "1") // the parent is itself an injected session
			code, err := NewCLIAgent("").RunInteractive(context.Background(), LLMAgentInteractiveRequest{
				BackendName: "claude", AgentID: "coder-1", ProjectRoot: root, LaunchGate: immediateLaunchGate,
			})
			if err != nil || code != 0 {
				t.Fatalf("RunInteractive exit=%d err=%v", code, err)
			}
			got, err := os.ReadFile(filepath.Join(root, "env.txt"))
			injected := strings.Contains(string(got), "--append-system-prompt-file")
			if err != nil || injected != enabled || strings.HasPrefix(string(got), "1|") != enabled {
				t.Fatalf("interactive launch env|args = %q err=%v; want bypass and injection both %v", got, err, enabled)
			}
		})
	}
}

// An injected parent (a pairing session started with the bypass, or a lane
// agent running the engine) must not open the gate for sessions it starts:
// ACPX, validation and non-injected CLI launches all take this environment.
func TestResolveLaunchEnvironmentDropsInheritedSystemPromptBypass(t *testing.T) {
	name := brand.EnvName("SYSTEM_PROMPT_CONTRACTS")
	t.Setenv(name, "1")
	for _, frozen := range [][]string{nil, {"PATH=/bin", name + "=1"}} {
		env, err := resolveLaunchEnvironment(agentlaunch.LaunchPlan{ToolName: "qwen-acp"}, t.TempDir(), "coder-1", "gen-1", frozen)
		if err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(env, func(entry string) bool { return strings.HasPrefix(entry, name+"=") }) {
			t.Fatalf("frozen=%v: env keeps the inherited bypass", frozen != nil)
		}
	}
}

func TestCLIAgentRunSetsSystemPromptContractsEnv(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("enabled=%v", enabled), func(t *testing.T) {
			root := systemPromptModeProject(t, enabled)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			name := brand.EnvName("SYSTEM_PROMPT_CONTRACTS")
			cmd, cleanup, err := NewCLIAgent("").buildRunCommand(context.Background(), LLMAgentRunRequest{
				BackendName: "custom-codex",
				AgentID:     "coder-1",
				ProjectRoot: root,
				Environment: []string{name + "=1"},
				RuntimeConfig: models.Config{AgentTools: map[string]models.AgentToolConfig{
					"custom-codex": {
						Backend:     string(providers.BackendCLI),
						ProviderKey: "codex",
						Executable:  executable,
						RunArgs:     []string{"--version"},
					},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			injected := len(cmd.Args) > 2 && cmd.Args[1] == "-c" && strings.HasPrefix(cmd.Args[2], "developer_instructions=")
			bypass := slices.Contains(cmd.Env, name+"=1")
			want := enabled && runtime.GOOS != "windows"
			if injected != want || bypass != want {
				t.Fatalf("injected=%v bypass=%v; want both %v", injected, bypass, want)
			}
		})
	}
}
