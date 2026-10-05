package main

import (
	"bytes"
	"encoding/json"
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
