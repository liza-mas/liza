package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestClaudeToolResultSettings(t *testing.T) {
	settings, err := ClaudeToolResultSettings("/tmp/engine's binary", "/tmp/project with spaces")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(settings), &value); err != nil {
		t.Fatal(err)
	}
	hooks := value["hooks"].(map[string]any)
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		entry := hooks[event].([]any)[0].(map[string]any)
		command := entry["hooks"].([]any)[0].(map[string]any)["command"].(string)
		if !strings.Contains(command, "claude-hook") || !strings.Contains(command, "'\"'\"'") || !strings.Contains(command, "tool-results'") {
			t.Fatalf("unsafe command: %s", command)
		}
	}
	if strings.Contains(settings, "permissionDecision") || strings.Contains(settings, "bypass") {
		t.Fatal("settings must not elevate permissions")
	}
}

func TestClaudeBuildRunCommandInjectsBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell stub")
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\necho '2.1.267 (Claude Code)'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	d := &CLIAgent{}
	cmd, cleanup, err := d.buildRunCommand(context.Background(), LLMAgentRunRequest{BackendName: "claude", ProjectRoot: t.TempDir(), Prompt: "Read source"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(cmd.Args) < 3 || cmd.Args[1] != "--settings" || !strings.Contains(cmd.Args[2], "claude-hook") {
		t.Fatalf("missing automatic boundary: %v", cmd.Args)
	}
}

func TestClaudeVersionAndHookActivationGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell stub")
	}
	for _, version := range []string{"2.1.226", "2.1.267", "3.0.0", "unknown"} {
		t.Run(version, func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "claude")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' '"+version+" (Claude Code)'\n"), 0755); err != nil {
				t.Fatal(err)
			}
			err := validateClaudeToolResultBoundary(context.Background(), binary, root, nil)
			allowed := version == "2.1.267" || version == "3.0.0"
			if allowed != (err == nil) {
				t.Fatalf("version %s error=%v", version, err)
			}
			if allowed {
				if err := os.Mkdir(filepath.Join(root, ".claude"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(`{"disableAllHooks":true}`), 0600); err != nil {
					t.Fatal(err)
				}
				if err := validateClaudeToolResultBoundary(context.Background(), binary, root, nil); err == nil {
					t.Fatal("disabled hooks accepted")
				}
			}
		})
	}
	for _, arg := range []string{"--bare", "--settings", "--settings={}"} {
		if err := validateClaudeToolResultBoundary(context.Background(), "not-invoked", t.TempDir(), []string{arg}); err == nil {
			t.Fatalf("unsafe argument %s accepted", arg)
		}
	}
}
