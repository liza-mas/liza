package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers"
)

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

// TestInstallScriptRefusesWindowsRelease covers the refusal itself and the fact
// that it is visible. detect_platform's stdout is consumed by the caller's
// command substitution, so a message printed there never reaches the process
// output this test reads.
func TestInstallScriptRefusesWindowsRelease(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uname reports MINGW/MSYS/CYGWIN only when bash runs on Windows")
	}

	out, err := runInstallScript(t, nil)
	if err == nil {
		t.Fatalf("install.sh succeeded, want a refusal:\n%s", out)
	}
	for _, want := range []string{
		"does not install Windows releases",
		"install.ps1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("install.sh output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Downloading from") {
		t.Fatalf("install.sh reached the download before refusing:\n%s", out)
	}
}

func TestPowerShellInstallerChecksLatestTagWithoutStrictPropertyAccess(t *testing.T) {
	repoRoot := findRepoRootForInstallScript(t)
	script, err := os.ReadFile(filepath.Join(repoRoot, "install.ps1"))
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	content := string(script)
	if !strings.Contains(content, "$release.PSObject.Properties['tag_name']") {
		t.Fatal("install.ps1 must inspect the tag_name property before reading its value under strict mode")
	}
	if strings.Contains(content, "$release.tag_name") {
		t.Fatal("install.ps1 directly accesses tag_name, which throws before the friendly error when the property is absent")
	}
}

func runInstallScriptHelp(t *testing.T, env ...string) (string, error) {
	t.Helper()
	return runInstallScript(t, []string{"--help"}, env...)
}

func runInstallScript(t *testing.T, args []string, env ...string) (string, error) {
	t.Helper()
	bashPath := testhelpers.ResolveBashForScripts(t)
	repoRoot := findRepoRootForInstallScript(t)
	// Bash (Git for Windows) treats backslashes as escape characters, so a
	// Windows path like C:\Users\...\install.sh gets mangled. Pass the script
	// path with forward slashes, which bash accepts on every platform.
	scriptPath := filepath.ToSlash(filepath.Join(repoRoot, "install.sh"))
	cmd := exec.Command(bashPath, append([]string{scriptPath}, args...)...)
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
