package gitbash

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestResolveWindowsPrefersKnownGitInstall(t *testing.T) {
	localRoot := filepath.Join("C:", "Users", "agent", "AppData", "Local")
	programRoot := filepath.Join("C:", "Program Files")
	want := filepath.Join(localRoot, "Programs", "Git", "bin", "bash.exe")

	got, err := resolve(
		"windows",
		mapEnv(map[string]string{"LOCALAPPDATA": localRoot, "ProgramFiles": programRoot}),
		func(path string) bool { return path == want },
		mapLookPath(map[string]string{"bash": filepath.Join("C:", "Windows", "System32", "bash.exe")}),
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != want {
		t.Fatalf("resolve = %q, want %q", got, want)
	}
}

func TestResolveWindowsFindsBashBesideGitCmd(t *testing.T) {
	gitPath := filepath.Join("D:", "Tools", "Git", "cmd", "git.exe")
	want := filepath.Join("D:", "Tools", "Git", "bin", "bash.exe")

	got, err := resolve(
		"windows",
		mapEnv(nil),
		func(path string) bool { return path == want },
		mapLookPath(map[string]string{"git": gitPath}),
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != want {
		t.Fatalf("resolve = %q, want %q", got, want)
	}
}

func TestResolveWindowsRejectsWSLLauncher(t *testing.T) {
	windowsRoot := filepath.Join("C:", "Windows")

	_, err := resolve(
		"windows",
		mapEnv(map[string]string{"SystemRoot": windowsRoot}),
		func(string) bool { return false },
		mapLookPath(map[string]string{"bash": filepath.Join(windowsRoot, "System32", "bash.exe")}),
	)
	if !errors.Is(err, errNotFound) {
		t.Fatalf("resolve error = %v, want errNotFound", err)
	}
}

func TestResolveWindowsAcceptsNonSystemPathBash(t *testing.T) {
	want := filepath.Join("D:", "PortableGit", "bin", "bash.exe")

	got, err := resolve(
		"windows",
		mapEnv(nil),
		func(string) bool { return false },
		mapLookPath(map[string]string{"bash": want}),
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != want {
		t.Fatalf("resolve = %q, want %q", got, want)
	}
}

func TestResolveUnixUsesPath(t *testing.T) {
	want := "/opt/bin/bash"

	got, err := resolve("linux", mapEnv(nil), func(string) bool { return false }, mapLookPath(map[string]string{"bash": want}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != want {
		t.Fatalf("resolve = %q, want %q", got, want)
	}
}

func mapEnv(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func mapLookPath(values map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if path := values[name]; path != "" {
			return path, nil
		}
		return "", errors.New("not found")
	}
}
