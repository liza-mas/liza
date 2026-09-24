package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/toolresult"
	"github.com/spf13/cobra"
)

func TestCodexHookCLIFailsClosed(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		cmd := newToolResultCodexHookCmd(func(*cobra.Command) (*toolresult.Store, error) {
			return nil, errors.New("private-token-must-not-escape")
		})
		var out bytes.Buffer
		cmd.SetIn(strings.NewReader(`{"hook_event_name":"` + event + `","tool_name":"Bash"}`))
		cmd.SetOut(&out)
		cmd.SetArgs(nil)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "private-token") {
			t.Fatalf("leaked error: %s", out.String())
		}
		expected := `"decision":"block"`
		if event == "PreToolUse" {
			expected = `"permissionDecision":"deny"`
		}
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("did not withhold: %s", out.String())
		}
	}
}

func TestCodexHookCLISmallReadPassThrough(t *testing.T) {
	store, err := toolresult.New(t.TempDir(), toolresult.Config{ThresholdBytes: 2048, DigestBytes: 1024}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd := newToolResultCodexHookCmd(func(*cobra.Command) (*toolresult.Store, error) { return store, nil })
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"read_file","tool_response":"small read"}`))
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	if err = cmd.Execute(); err != nil || out.String() != "{}" {
		t.Fatalf("output=%q err=%v", out.String(), err)
	}
}
