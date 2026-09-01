package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/toolchain"
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
	for _, want := range []string{"rtk", "stacklit", "scip-search", "bash-policy", "functional-clusters", "balanced"} {
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

func TestPrintInstallResultAndReturnPrintsFailedSteps(t *testing.T) {
	var out bytes.Buffer
	wantErr := errors.New("toolchain install incomplete: jq:failed")
	err := printInstallResultAndReturn(&out, toolchain.InstallResult{Steps: []toolchain.InstallStep{
		{ToolID: "rtk", Status: toolchain.InstallSkipped, Message: "already installed"},
		{ToolID: "jq", Status: toolchain.InstallFailed, Message: "no supported package manager found"},
	}}, wantErr)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	text := out.String()
	if !strings.Contains(text, "[SKIPPED] rtk") || !strings.Contains(text, "[FAILED] jq") {
		t.Fatalf("install output missing failed step:\n%s", text)
	}
	if !strings.Contains(text, "no supported package manager found") {
		t.Fatalf("install output missing diagnostic message:\n%s", text)
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
	if want := "Run: source " + shellQuote(filepath.Join(globalDir, "toolchain", "env.sh")); !strings.Contains(out.String(), want) {
		t.Fatalf("configure output missing source instruction %q:\n%s", want, out.String())
	}
}

func TestToolchainConfigureWritesPowerShellEnvOnWindows(t *testing.T) {
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

	psEnvPath := filepath.Join(globalDir, "toolchain", "env.ps1")
	_, statErr := os.Stat(psEnvPath)
	if runtime.GOOS != "windows" {
		if statErr == nil {
			t.Fatalf("env.ps1 written on %s, want none", runtime.GOOS)
		}
		if strings.Contains(out.String(), "From PowerShell, run:") {
			t.Fatalf("configure output mentions PowerShell on %s:\n%s", runtime.GOOS, out.String())
		}
		return
	}
	if statErr != nil {
		t.Fatalf("env.ps1 not written: %v", statErr)
	}
	want := `From PowerShell, run: . "` + psEnvPath + `"`
	if !strings.Contains(out.String(), want) {
		t.Fatalf("configure output missing PowerShell instruction %q:\n%s", want, out.String())
	}
	if wantSource := "Run: source " + shellQuote(filepath.Join(globalDir, "toolchain", "env.sh")); !strings.Contains(out.String(), wantSource) {
		t.Fatalf("configure output dropped the Git Bash instruction %q:\n%s", wantSource, out.String())
	}
}

func TestToolchainConfigureRequiresProjectAndAgentsTogether(t *testing.T) {
	resetRootCmdForTest(t)
	rootCmd.SetArgs([]string{
		"toolchain", "configure",
		"--global-dir", t.TempDir(),
		"--project", ".",
	})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("toolchain configure error = nil, want project/agents validation")
	}
	if !strings.Contains(err.Error(), "--project and --agents") {
		t.Fatalf("error = %v", err)
	}
}
