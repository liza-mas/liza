package toolchain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureWritesProfileAndEnv(t *testing.T) {
	globalDir := t.TempDir()
	installDir := filepath.Join(t.TempDir(), "bin")

	got, err := Configure(ConfigureOptions{
		Profile:    ProfileBalanced,
		GlobalDir:  globalDir,
		InstallDir: installDir,
	})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}

	if got.Profile != ProfileBalanced {
		t.Fatalf("profile = %q, want balanced", got.Profile)
	}
	if !strings.HasPrefix(got.ToolchainDir, globalDir) {
		t.Fatalf("toolchain dir = %q, want under %q", got.ToolchainDir, globalDir)
	}

	profileData, err := os.ReadFile(got.ProfilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	var profile profileFile
	if err := json.Unmarshal(profileData, &profile); err != nil {
		t.Fatalf("unmarshal profile: %v", err)
	}
	if !contains(profile.SelectedTools, "rtk") || !contains(profile.SelectedTools, "semble") {
		t.Fatalf("profile selected tools = %v", profile.SelectedTools)
	}

	envData, err := os.ReadFile(got.EnvPath)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	env := string(envData)
	if !strings.Contains(env, `export PATH="`+installDir+`:$PATH"`) {
		t.Fatalf("env missing install dir PATH:\n%s", env)
	}
	if !strings.Contains(env, `export LIZA_ENABLE_STACKLIT="1"`) {
		t.Fatalf("env missing stacklit activation:\n%s", env)
	}
	if strings.Contains(env, "\nexport HF_HUB_OFFLINE=\"1\"") {
		t.Fatalf("env should not assert Semble offline readiness before validation:\n%s", env)
	}
}

func TestConfigureShellProfileIsIdempotent(t *testing.T) {
	home := t.TempDir()
	globalDir := t.TempDir()

	first, err := Configure(ConfigureOptions{
		Profile:           ProfileLean,
		GlobalDir:         globalDir,
		InstallDir:        filepath.Join(t.TempDir(), "bin"),
		WriteShellProfile: true,
		HomeDir:           home,
		Shell:             "/bin/zsh",
	})
	if err != nil {
		t.Fatalf("first Configure() error = %v", err)
	}
	if _, err := Configure(ConfigureOptions{
		Profile:           ProfileLean,
		GlobalDir:         globalDir,
		InstallDir:        filepath.Join(t.TempDir(), "bin"),
		WriteShellProfile: true,
		HomeDir:           home,
		Shell:             "/bin/zsh",
	}); err != nil {
		t.Fatalf("second Configure() error = %v", err)
	}
	content, err := os.ReadFile(first.ShellProfilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if count := strings.Count(string(content), "# Liza toolchain"); count != 1 {
		t.Fatalf("shell profile contains %d Liza toolchain blocks, want 1:\n%s", count, content)
	}
}

func TestConfigureShellProfileUsesBashStartupFiles(t *testing.T) {
	home := t.TempDir()
	globalDir := t.TempDir()

	got, err := Configure(ConfigureOptions{
		Profile:           ProfileLean,
		GlobalDir:         globalDir,
		InstallDir:        filepath.Join(t.TempDir(), "bin"),
		WriteShellProfile: true,
		HomeDir:           home,
		Shell:             "/bin/bash",
	})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}

	wantPaths := []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".profile")}
	if strings.Join(got.ShellProfilePaths, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("ShellProfilePaths = %v, want %v", got.ShellProfilePaths, wantPaths)
	}
	if got.ShellProfilePath != wantPaths[0] {
		t.Fatalf("ShellProfilePath = %q, want first bash path %q", got.ShellProfilePath, wantPaths[0])
	}
	for _, path := range wantPaths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(content), got.EnvPath) {
			t.Fatalf("%s missing env source line:\n%s", path, content)
		}
	}
}

func TestConfigureAgentToolsAutoWritesWhenMissing(t *testing.T) {
	globalDir := t.TempDir()

	got, err := Configure(ConfigureOptions{
		Profile:        ProfileLean,
		GlobalDir:      globalDir,
		InstallDir:     filepath.Join(t.TempDir(), "bin"),
		AgentToolsMode: "auto",
	})
	if err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	if got.AgentToolsPath == "" {
		t.Fatal("AgentToolsPath empty, want written path")
	}
	content, err := os.ReadFile(filepath.Join(globalDir, "AGENT_TOOLS.md"))
	if err != nil {
		t.Fatalf("read AGENT_TOOLS.md: %v", err)
	}
	if !strings.Contains(string(content), "# Agent Tools") {
		t.Fatalf("AGENT_TOOLS.md missing embedded content:\n%s", content)
	}
}

func TestConfigureAgentToolsForceBacksUpExistingFile(t *testing.T) {
	globalDir := t.TempDir()
	toolsPath := filepath.Join(globalDir, "AGENT_TOOLS.md")
	if err := os.WriteFile(toolsPath, []byte("custom tools\n"), 0o644); err != nil {
		t.Fatalf("write existing AGENT_TOOLS.md: %v", err)
	}

	if _, err := Configure(ConfigureOptions{
		Profile:        ProfileLean,
		GlobalDir:      globalDir,
		InstallDir:     filepath.Join(t.TempDir(), "bin"),
		AgentToolsMode: "force",
	}); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	backup, err := os.ReadFile(toolsPath + ".bak")
	if err != nil {
		t.Fatalf("read AGENT_TOOLS backup: %v", err)
	}
	if string(backup) != "custom tools\n" {
		t.Fatalf("backup = %q, want original content", backup)
	}
}

func TestConfigureRejectsInvalidAgentToolsModeBeforeWriting(t *testing.T) {
	globalDir := t.TempDir()
	_, err := Configure(ConfigureOptions{
		Profile:        ProfileLean,
		GlobalDir:      globalDir,
		InstallDir:     filepath.Join(t.TempDir(), "bin"),
		AgentToolsMode: "invalid",
	})
	if err == nil {
		t.Fatal("Configure() error = nil, want invalid agent-tools mode")
	}
	if _, statErr := os.Stat(filepath.Join(globalDir, "toolchain", "profile.json")); !os.IsNotExist(statErr) {
		t.Fatalf("profile.json stat err = %v, want not exist", statErr)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
