package embedded

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCopilotHookOutputFormat shells out to the shipped deny hooks with a
// "copilot" positional argument and asserts they emit Copilot's documented
// top-level decision fields instead of the Claude/Codex hookSpecificOutput
// wrapper.
func TestCopilotHookOutputFormat(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	tmpDir := t.TempDir()

	cases := []struct {
		name    string
		content []byte
		payload string
	}{
		{
			name:    "git-guard denies destructive git",
			content: gitGuardHookContent,
			payload: `{"tool_name":"Bash","tool_input":{"command":"git push --force"}}`,
		},
		{
			name:    "rtk-guard denies rtk proxy",
			content: rtkGuardHookContent,
			payload: `{"tool_name":"Bash","tool_input":{"command":"rtk proxy git status"}}`,
		},
		{
			name:    "worktree-path-guard denies duplicated segment",
			content: worktreePathGuardHookContent,
			payload: `{"tool_name":"Read","tool_input":{"file_path":"/p/.worktrees/task-1/task-1/foo.go"}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hookPath := filepath.Join(tmpDir, "hook.sh")
			if err := os.WriteFile(hookPath, tc.content, 0755); err != nil {
				t.Fatalf("write hook: %v", err)
			}

			cmd := exec.Command("bash", hookPath, "copilot")
			// git-guard only guards agent-driven commands; pairing sessions pass through.
			cmd.Env = append(os.Environ(), "LIZA_AGENT_ID=test-agent")
			cmd.Stdin = strings.NewReader(tc.payload)
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stdout
			if err := cmd.Run(); err != nil {
				t.Fatalf("hook exited non-zero: %v\n%s", err, stdout.String())
			}

			out := stdout.String()
			if strings.Contains(out, "hookSpecificOutput") {
				t.Errorf("copilot mode must not emit hookSpecificOutput wrapper: %s", out)
			}
			var decision map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &decision); err != nil {
				t.Fatalf("copilot mode output is not a JSON object: %v\n%s", err, out)
			}
			if decision["permissionDecision"] != "deny" {
				t.Errorf("permissionDecision = %#v, want deny. Output: %s", decision["permissionDecision"], out)
			}
			if reason, _ := decision["permissionDecisionReason"].(string); reason == "" {
				t.Errorf("deny output missing permissionDecisionReason. Output: %s", out)
			}
		})
	}
}
