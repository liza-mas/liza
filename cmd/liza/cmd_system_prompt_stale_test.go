package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/paths"
)

func TestSystemPromptStalePrintsChangedSources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	globalDir, err := paths.GlobalLizaDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := embedded.WriteGlobalFiles(globalDir, nil); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	// resetRootCmdForTest discards the command's output; capture it here.
	run := func() (string, error) {
		resetRootCmdForTest(t)
		var out bytes.Buffer
		rootCmd.SetOut(&out)
		rootCmd.SetArgs([]string{"system-prompt-stale", "--project-root", root})
		err := rootCmd.Execute()
		return out.String(), err
	}
	guardrails := filepath.Join(root, "GUARDRAILS.md")
	if err := os.WriteFile(guardrails, []byte("# guardrails\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if out, err := run(); err != nil || out != "" {
		t.Fatalf("without a marker: out=%q err=%v, want empty", out, err)
	}
	if _, err := embedded.EnableSystemPromptMode(root, false); err != nil {
		t.Fatal(err)
	}
	if out, err := run(); err != nil || out != "" {
		t.Fatalf("fresh build: out=%q err=%v, want empty", out, err)
	}
	if err := os.WriteFile(guardrails, []byte("# guardrails\n\nnew lesson\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := run(); err != nil || out != "changed GUARDRAILS.md\n" {
		t.Fatalf("after a GUARDRAILS.md edit: out=%q err=%v, want %q", out, err, "changed GUARDRAILS.md\n")
	}
	if err := os.Remove(guardrails); err != nil {
		t.Fatal(err)
	}
	if out, err := run(); err != nil || out != "removed GUARDRAILS.md\n" {
		t.Fatalf("after deleting GUARDRAILS.md: out=%q err=%v, want %q", out, err, "removed GUARDRAILS.md\n")
	}
}
