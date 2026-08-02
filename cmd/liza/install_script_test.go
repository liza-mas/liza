package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// resolveBashForScripts returns the path to a bash that can execute Windows
// filesystem paths, preferring Git Bash over WSL's system32\bash.exe.
//
// WSL bash cannot access Windows paths (C:/Users/... -> "No such file or
// directory"; it needs /mnt/c/...). Git for Windows installs bash.exe under
// %LOCALAPPDATA%\Programs\Git\bin or %ProgramFiles%\Git\bin, but it is often
// not on PATH ahead of system32. Tests that exec a script by its Windows path
// need the Git Bash binary specifically.
func resolveBashForScripts(t *testing.T) string {
	t.Helper()
	// Prefer an explicit Git Bash location if present.
	for _, candidate := range []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "bin", "bash.exe"),
	} {
		if candidate != "" {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	// Fall back to whatever bash is on PATH (may be WSL bash on Windows, which
	// cannot run Windows-path scripts — callers that need path access should
	// skip if this is the only option and it is WSL).
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	t.Skip("bash not available")
	return ""
}

func TestInstallScriptAcceptsDerivedBrandInputsInHelp(t *testing.T) {
	out, err := runInstallScriptHelp(t,
		"BRAND_NAME_LOWER=acme-agent",
		"BRAND_REPO=acme/agent",
	)
	if err != nil {
		t.Fatalf("install.sh --help failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Acme Agent Installation Script",
		"https://raw.githubusercontent.com/acme/agent/main/install.sh",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("install.sh --help missing %q:\n%s", want, out)
		}
	}
}

func TestInstallScriptRejectsInvalidBrandInputs(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{name: "lower", env: "BRAND_NAME_LOWER=Acme", want: "BRAND_NAME_LOWER invalid"},
		{name: "repo", env: "BRAND_REPO=https://github.com/acme/agent", want: "BRAND_REPO invalid"},
		{name: "global dir", env: "BRAND_GLOBAL_DIRNAME=../acme", want: "BRAND_GLOBAL_DIRNAME invalid"},
		{name: "source dir", env: "BRAND_SOURCE_DIR_NAME=../acme", want: "BRAND_SOURCE_DIR_NAME invalid"},
		{name: "release url", env: "BRAND_RELEASE_BASE_URL=http://example.com/acme", want: "BRAND_RELEASE_BASE_URL invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runInstallScriptHelp(t, tt.env)
			if err == nil {
				t.Fatalf("install.sh --help succeeded, want validation failure\n%s", out)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("install.sh output missing %q:\n%s", tt.want, out)
			}
		})
	}
}

func runInstallScriptHelp(t *testing.T, env ...string) (string, error) {
	t.Helper()
	bashPath := resolveBashForScripts(t)
	repoRoot := findRepoRootForInstallScript(t)
	// Bash (Git for Windows) treats backslashes as escape characters, so a
	// Windows path like C:\Users\...\install.sh gets mangled. Pass the script
	// path with forward slashes, which bash accepts on every platform.
	scriptPath := filepath.ToSlash(filepath.Join(repoRoot, "install.sh"))
	cmd := exec.Command(bashPath, scriptPath, "--help")
	cmd.Env = append(os.Environ(), "INSTALL_DIR="+t.TempDir())
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func findRepoRootForInstallScript(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}
