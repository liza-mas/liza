package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/toolresult"
	"github.com/spf13/cobra"
)

func TestClaudeToolResultHookCLI(t *testing.T) {
	s, err := toolresult.New(filepath.Join(t.TempDir(), "store"), toolresult.Config{ThresholdBytes: 2048, DigestBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd := newToolResultClaudeCmd(func(*cobra.Command) (*toolresult.Store, error) { return s, nil })
	payload, _ := json.Marshal(toolresult.ClaudeHookInput{Event: "PostToolUse", Tool: "Read", Response: map[string]any{"type": "text", "file": map[string]any{"filePath": "/repo/a.go", "content": strings.Repeat("test output\n", 400)}}})
	var out bytes.Buffer
	cmd.SetIn(bytes.NewReader(payload))
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "updatedToolOutput") || !strings.Contains(out.String(), "artifact_id") || out.Len() > 2048 {
		t.Fatalf("unexpected hook response: %s", out.String())
	}
}

func TestClaudeBashControlledModesPreservePermissionInput(t *testing.T) {
	for _, mode := range []string{"", "default", "acceptEdits", "plan", "manual", "auto", "dontAsk", "futureMode"} {
		for _, command := range []string{"git status --short", "trap 'printf original' EXIT"} {
			t.Run(mode+"/"+command, func(t *testing.T) {
				storeCalls := 0
				cmd := newToolResultClaudeCmd(func(*cobra.Command) (*toolresult.Store, error) {
					storeCalls++
					return nil, errors.New("capture store unavailable")
				})
				input := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": command, "timeout": 10000, "run_in_background": true}}
				if mode != "" {
					input["permission_mode"] = mode
				}
				payload, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				var out bytes.Buffer
				cmd.SetIn(bytes.NewReader(payload))
				cmd.SetOut(&out)
				cmd.SetArgs(nil)
				if err := cmd.Execute(); err != nil {
					t.Fatal(err)
				}
				if strings.TrimSpace(out.String()) != "{}" || storeCalls != 0 {
					t.Fatalf("controlled mode must preserve native permission input without capture dependencies: store calls=%d response=%s", storeCalls, out.String())
				}
			})
		}
	}
}

func TestClaudeBashBypassRetainsCapture(t *testing.T) {
	for _, sample := range []struct{ name, command string }{
		{"capture", "git status --short"},
		{"reserved", "trap 'printf original' EXIT"},
		{"store_failure", "git status --short"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			s, err := toolresult.New(filepath.Join(t.TempDir(), "store"), toolresult.Config{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			cmd := newToolResultClaudeCmd(func(*cobra.Command) (*toolresult.Store, error) {
				if sample.name == "store_failure" {
					return nil, errors.New("capture store unavailable")
				}
				return s, nil
			})
			payload, err := json.Marshal(toolresult.ClaudeHookInput{Event: "PreToolUse", Tool: "Bash", PermissionMode: "bypassPermissions", Input: map[string]any{"command": sample.command, "timeout": 10000, "run_in_background": true}})
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			cmd.SetIn(bytes.NewReader(payload))
			cmd.SetOut(&out)
			cmd.SetArgs(nil)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			var response map[string]any
			if err := json.Unmarshal(out.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if sample.name == "store_failure" {
				if response["continue"] != false {
					t.Fatalf("unavailable bypass capture must stop: %s", out.String())
				}
				return
			}
			hook, ok := response["hookSpecificOutput"].(map[string]any)
			if !ok || hook["hookEventName"] != "PreToolUse" {
				t.Fatalf("missing pre-tool response: %s", out.String())
			}
			if sample.name == "reserved" {
				if hook["permissionDecision"] != "deny" || !strings.Contains(out.String(), "reserved") {
					t.Fatalf("reserved bypass shell state must be denied: %s", out.String())
				}
				return
			}
			input, ok := hook["updatedInput"].(map[string]any)
			if !ok || input["command"] == sample.command || input["timeout"] != float64(10000) || input["run_in_background"] != true || hook["permissionDecision"] != nil {
				t.Fatalf("bypass must capture while retaining invocation fields without granting permission: %s", out.String())
			}
		})
	}
}
