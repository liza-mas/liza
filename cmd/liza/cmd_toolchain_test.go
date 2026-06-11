package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolchainListShowsBalancedTools(t *testing.T) {
	resetRootCmdForTest(t)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"toolchain", "list"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("toolchain list failed: %v", err)
	}
	text := out.String()
	for _, want := range []string{"rtk", "stacklit", "scip-search", "balanced"} {
		if !strings.Contains(text, want) {
			t.Fatalf("toolchain list output missing %q:\n%s", want, text)
		}
	}
}

func TestToolchainInstallDryRunPrintsPlannedCommands(t *testing.T) {
	resetRootCmdForTest(t)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs(toolchainArgs(
		"toolchain", "install",
		"--dry-run",
		"--profile", "lean",
		"--include", "rtk",
		"--exclude", "stacklit",
		"--exclude", "scip-search",
		"--install-dir", filepath.Join(t.TempDir(), "bin"),
	))

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("toolchain install --dry-run failed: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "rtk") {
		t.Fatalf("dry-run output missing rtk:\n%s", text)
	}
	if strings.Contains(text, "[PLANNED] stacklit") {
		t.Fatalf("dry-run output should not include excluded stacklit:\n%s", text)
	}
}

func TestToolchainInstallDryRunManualCapability(t *testing.T) {
	resetRootCmdForTest(t)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs(toolchainArgs(
		"toolchain", "install",
		"--dry-run",
		"--profile", "lean",
		"--include", "postgres-mcp",
		"--exclude", "rtk",
		"--exclude", "stacklit",
		"--exclude", "scip-search",
	))

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("toolchain install manual dry-run failed: %v", err)
	}
	if !strings.Contains(out.String(), "[SKIPPED] postgres-mcp") {
		t.Fatalf("manual capability output missing skipped postgres-mcp:\n%s", out.String())
	}
}

func toolchainArgs(args ...string) []string {
	excluded := map[string]bool{}
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--exclude" {
			excluded[args[i+1]] = true
		}
	}
	for _, id := range []string{"scip-go", "scip-typescript", "scip-python", "rg", "ast-grep", "mdtoc", "mdq", "jq", "yq", "gh", "pre-commit"} {
		if excluded[id] {
			continue
		}
		args = append(args, "--exclude", id)
	}
	return args
}

func TestToolchainConfigureWritesFiles(t *testing.T) {
	resetRootCmdForTest(t)
	globalDir := t.TempDir()
	installDir := filepath.Join(t.TempDir(), "bin")
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{
		"toolchain", "configure",
		"--profile", "lean",
		"--global-dir", globalDir,
		"--install-dir", installDir,
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("toolchain configure failed: %v", err)
	}
	for _, rel := range []string{"toolchain/profile.json", "toolchain/env.sh", "AGENT_TOOLS.md"} {
		if _, err := os.Stat(filepath.Join(globalDir, rel)); err != nil {
			t.Fatalf("%s not written: %v", rel, err)
		}
	}
	env, err := os.ReadFile(filepath.Join(globalDir, "toolchain", "env.sh"))
	if err != nil {
		t.Fatalf("read env.sh: %v", err)
	}
	if !strings.Contains(string(env), installDir) {
		t.Fatalf("env.sh missing install dir:\n%s", env)
	}
}
