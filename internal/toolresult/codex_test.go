package toolresult

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCodexHookShellRewritePreservesArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX shell")
	}
	store := testStore(t)
	runner := filepath.Join(t.TempDir(), "runner ' literal")
	if err := os.WriteFile(runner, []byte("#!/bin/sh\nprintf '%s\\0' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	command := "printf '%s' \"$HOME\"; # trailing quote '\nexit 7"
	input, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "permission_mode": "bypassPermissions", "session_id": "session-test", "tool_input": map[string]any{"command": command, "custom": 42}})
	data, err := CodexHook(input, runner, store)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Specific struct {
			Decision string `json:"permissionDecision"`
			Input    struct {
				Command string `json:"command"`
				Custom  int    `json:"custom"`
			} `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Specific.Decision != "allow" || result.Specific.Input.Custom != 42 {
		t.Fatalf("rewrite: %s", data)
	}
	output, err := exec.Command("/bin/sh", "-c", result.Specific.Input.Command).Output()
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(string(output), "\x00")
	if len(args) < 5 || args[len(args)-2] != command || args[len(args)-3] != "-c" || args[len(args)-4] != "/bin/sh" {
		t.Fatalf("shell argv changed: %#v", args)
	}
	if !strings.Contains(string(output), "session-test") || !strings.Contains(string(output), store.path) {
		t.Fatalf("missing stable metadata: %#v", args)
	}
}

func TestCodexHookNeverElevatesConstrainedSession(t *testing.T) {
	for _, mode := range []string{"", "default", "acceptEdits", "dontAsk", "plan"} {
		input, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "permission_mode": mode, "tool_input": map[string]any{"command": "echo hello"}})
		data, err := CodexHook(input, "/engine", testStore(t))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"permissionDecision":"deny"`) || strings.Contains(string(data), "updatedInput") {
			t.Fatalf("elevated %q: %s", mode, data)
		}
	}
}

// The hook must accept exactly the permission mode the managed configuration
// renders (approval_policy="never" reports bypassPermissions) and reject every
// other mode: those sessions are either more constrained (an allow rewrite
// would elevate them) or outside the managed baseline posture.
func TestCodexHookAcceptsOnlyManagedConfigurationMode(t *testing.T) {
	runner := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(runner, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"bypassPermissions", "default", "", "readOnly", "fullAccess"} {
		input, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "permission_mode": mode, "session_id": "session-test", "tool_input": map[string]any{"command": "echo ok"}})
		data, err := CodexHook(input, runner, testStore(t))
		if err != nil {
			t.Fatal(err)
		}
		if mode == "bypassPermissions" {
			if !strings.Contains(string(data), `"permissionDecision":"allow"`) || !strings.Contains(string(data), "updatedInput") {
				t.Fatalf("managed mode rejected: %s", data)
			}
			continue
		}
		if !strings.Contains(string(data), `"permissionDecision":"deny"`) || strings.Contains(string(data), "updatedInput") {
			t.Fatalf("mode %q must not be rewritten: %s", mode, data)
		}
	}
}

func TestCodexHookPostFullMCPResultAndDedup(t *testing.T) {
	store := testStore(t)
	response := map[string]any{"content": []any{map[string]any{"type": "text", "text": strings.Repeat("archive-row: terminal stdout\n", 4000) + "fake-known-secret"}}, "isError": true}
	input, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "mcp__fixture__read", "session_id": "session-test", "tool_input": map[string]any{"path": "logs/coder.txt"}, "tool_response": response})
	for i := 0; i < 2; i++ {
		data, err := CodexHook(input, "/engine", store, Result{TaskID: "DEV-778", AgentID: "coder-1", Role: "coder"})
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Decision string `json:"decision"`
			Reason   string `json:"reason"`
		}
		if err = json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		if out.Decision != "block" || out.Reason == "" || len(out.Reason) > 1024 || strings.Contains(out.Reason, "fake-known-secret") {
			t.Fatalf("unsafe post result: %s", data)
		}
		digest := parseDigest(t, out.Reason)
		if digest.Duplicate != (i == 1) {
			t.Fatalf("duplicate=%v round=%d", digest.Duplicate, i)
		}
	}
}

func TestCodexHookSmallReadUnchangedAndShellPostSkipped(t *testing.T) {
	for _, tool := range []string{"read_file", "Bash"} {
		input, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": tool, "tool_response": "small normal read\n"})
		data, err := CodexHook(input, "/engine", testStore(t))
		if err != nil || string(data) != "{}" {
			t.Fatalf("small changed: %s %v", data, err)
		}
	}
}

func TestCodexHookTelemetryMetadata(t *testing.T) {
	store := testStore(t)
	input := []byte(`{"hook_event_name":"PostToolUse","tool_name":"read_file","session_id":"native-session","tool_response":"small read"}`)
	if _, err := CodexHook(input, "/engine", store, Result{TaskID: "DEV-778", AgentID: "coder-1", Role: "coder", SessionID: "must-not-override-native"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(store.path, "events"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(store.path, "events", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"task_id":"DEV-778"`) && strings.Contains(string(data), `"agent_id":"coder-1"`) && strings.Contains(string(data), `"role":"coder"`) && strings.Contains(string(data), `"session_id":"native-session"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("native post telemetry omitted task/agent/role/session")
	}
}
